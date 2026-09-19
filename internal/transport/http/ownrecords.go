package http

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/projects"
	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/contract"
)

// This daemon's own records, converted to the shape the session list's
// projections read (swiftstore/own.go), so that the crown, the task chips,
// the waits, the delivery ticks and the titles come from this daemon's store
// first and the Swift app's is only history beside it (cutover B1).
//
// Every identity below is one this daemon's own evidence gives, and each says
// which evidence. A record that no evidence ties to a live session is still
// carried — it is somebody's task, wait or handoff — but without process
// facts, so the Swift app's six-fact rule matches it to nobody.

// eventSessionDelivered is what the broker records when a root reports its
// turn delivered (orchestrator.ReportSessionDelivery).
const eventSessionDelivered = "session.delivered"

// ownStartTolerance is the Swift app's SessionRegistry.startTolerance, used for
// the same question: was this process running at that moment.
const ownStartTolerance = 5 * time.Second

// errOwnRecords is one of this daemon's own sources that could not be read.
// It is evidence missing, never an empty list (DG-7).
var errOwnRecords = errors.New("this daemon's own records could not all be read")

func secondsOf(t time.Time) swiftstore.Seconds {
	return swiftstore.Seconds(float64(t.UnixNano()) / 1e9)
}

func secondsPtrOf(t time.Time) *swiftstore.Seconds {
	if t.IsZero() {
		return nil
	}
	v := secondsOf(t)
	return &v
}

func stringPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// ownOverlay reads the records and converts them against the sessions on
// screen. err is set when any source failed; what the others said is still
// returned, so the caller can list it under "unknown" (DG-7).
func (s *Server) ownOverlay(ctx context.Context, lives []swiftstore.Live) (swiftstore.Own, error) {
	var own swiftstore.Own
	var failed bool
	byTerminal := make(map[string]swiftstore.Live, len(lives))
	for _, l := range lives {
		byTerminal[l.TerminalID] = l
	}
	terminalOf := conversationTerminals(lives)

	if records, unreadable, err := s.broker.Records(ctx); err != nil || len(unreadable) > 0 {
		// A row that cannot be decoded may be any session's task: the list
		// without it is not the list.
		failed = true
		own.Tasks = ownTasks(records, byTerminal)
	} else {
		own.Tasks = ownTasks(records, byTerminal)
	}
	if waits, err := s.broker.Waits(ctx); err != nil {
		failed = true
	} else {
		own.Waits = ownWaits(waits, terminalOf)
	}
	if deliveries, err := s.ownDeliveries(ctx, lives); err != nil {
		failed = true
	} else {
		own.Deliveries = deliveries
	}
	if handoffs, err := s.broker.Handoffs(ctx); err != nil {
		failed = true
	} else {
		own.Handoffs, own.HandoffLabels = ownHandoffs(handoffs)
	}
	if assignments, err := s.broker.RootAssignments(ctx); err != nil {
		failed = true
	} else {
		own.RootAssignments = ownAssignments(assignments)
	}
	if failed {
		return own, errOwnRecords
	}
	return own, nil
}

// conversationTerminals maps each conversation on screen to its terminal,
// when exactly one terminal holds it. This daemon's coordination plane names
// sessions by conversation (W5); the session list's rows are terminals, and a
// conversation two terminals claim names neither of them.
func conversationTerminals(lives []swiftstore.Live) map[string]string {
	count := map[string]int{}
	out := map[string]string{}
	for _, l := range lives {
		if l.ConversationID == "" {
			continue
		}
		count[l.ConversationID]++
		out[l.ConversationID] = l.TerminalID
	}
	for id, n := range count {
		if n != 1 {
			delete(out, id)
		}
	}
	return out
}

// ownTasks converts the broker's records.
//
// A record's child is tied to a live session by the evidence the broker
// itself acts on — the terminal it opened for the task (watch.go reads the
// child's presence by that id, DG-6) — narrowed by the assistant, and by the
// process having been running between the tab's opening and the task's end,
// so a pane id reused by a later process is not the task's child. Only then
// does the converted task carry that session's process facts.
//
// Closing a session reads three of the task's facts, and each is taken from
// the one record that owns it. A pending landing is not carried: the broker's
// own obligation list already carries it (D01), and a second copy here would
// count it twice. A finished task's result is its record's; one that ended
// without a result ended by the broker's verdict, which is its answer.
func ownTasks(records []orchestrator.Record, byTerminal map[string]swiftstore.Live) []swiftstore.Task {
	out := make([]swiftstore.Task, 0, len(records))
	for _, r := range records {
		t := swiftstore.Task{
			ID:         r.ID,
			State:      string(r.State),
			Kind:       r.Kind,
			Title:      r.Title,
			Assistant:  r.Assistant,
			ProjectDir: r.ProjectDir,
			Created:    secondsOf(r.CreatedAt),
			Model:      stringPtr(r.Model),
			WorkItemID: stringPtr(r.WorkID),
			ScheduleID: stringPtr(r.ScheduleID),
			Permission: r.PermissionMode,
			SpawnedAt:  secondsPtrOf(r.SpawnedAt),
			BriefedAt:  secondsPtrOf(r.AcceptedAt),
			FinishedAt: secondsPtrOf(r.FinishedAt),

			ChildTerminal: stringPtr(r.ChildTerminalID),
			ChildBackend:  stringPtr(r.ChildBackend),
		}
		if r.State.Terminal() {
			at := r.FinishedAt
			if at.IsZero() {
				at = r.CreatedAt
			}
			t.ResultVerifiedAt = secondsPtrOf(at)
		}
		if r.Root != nil {
			t.RootSession = stringPtr(r.Root.SessionID)
			t.RootAssistant = stringPtr(r.Root.Assistant)
			t.RootLabel = stringPtr(r.Root.Label)
		}
		if r.ScheduleTitle != "" && t.RootLabel == nil {
			t.RootLabel = stringPtr(r.ScheduleTitle)
		}
		if l := r.Landing; l != nil {
			switch l.State {
			case orchestrator.LandingLanded:
				// Proved against the target at the time (D17), which is what
				// the Swift app calls a broker-verified target landing.
				origin := "local_target_branch"
				t.Landing = &swiftstore.Landing{
					State: string(l.State), Commit: stringPtr(l.Commit), VerifiedCommit: stringPtr(l.Commit),
					VerifiedTargetCommit: stringPtr(l.TargetCommit), Target: stringPtr(l.Target),
					VerificationOrigin: &origin, LandedAt: secondsPtrOf(l.At),
				}
			case orchestrator.LandingAbandoned, orchestrator.LandingNothingToLand:
				t.Landing = &swiftstore.Landing{State: string(l.State), Target: stringPtr(l.Target),
					LandedAt: secondsPtrOf(l.At)}
			}
		}
		if live, ok := byTerminal[r.ChildTerminalID]; ok && r.ChildTerminalID != "" && ownChild(r, live) {
			tty, pid, conv := live.TTY, live.PID, live.ConversationID
			start := secondsOf(live.ProcessStart)
			t.ChildTTY, t.ChildPID, t.ChildProcStart, t.ChildSession = &tty, &pid, &start, &conv
			t.TranscriptProven = true
		}
		out = append(out, t)
	}
	return out
}

// ownChild is whether the live session in a record's child terminal is that
// task's child: the same assistant, a fully read process, and a process that
// was running between the tab's opening and the task's end.
func ownChild(r orchestrator.Record, l swiftstore.Live) bool {
	if l.Assistant == "" || l.Assistant != r.Assistant || l.PID == 0 || l.ProcessStart.IsZero() ||
		l.ConversationID == "" {
		return false
	}
	opened := r.SpawnedAt
	if opened.IsZero() {
		opened = r.CreatedAt
	}
	if l.ProcessStart.Before(opened.Add(-ownStartTolerance)) {
		return false
	}
	if !r.FinishedAt.IsZero() && l.ProcessStart.After(r.FinishedAt.Add(ownStartTolerance)) {
		return false
	}
	return true
}

// ownWaits converts the open waits. Their sessions are conversations; each is
// named by its terminal when exactly one on screen holds it, and otherwise by
// the conversation, which no row's id can be mistaken for.
func ownWaits(waits []store.WaitRow, terminalOf map[string]string) []swiftstore.CoordinationWait {
	name := func(conversation string) string {
		if t, ok := terminalOf[conversation]; ok {
			return t
		}
		return conversation
	}
	out := make([]swiftstore.CoordinationWait, 0, len(waits))
	for _, w := range waits {
		cw := swiftstore.CoordinationWait{
			ID: w.ID, Repository: w.Repository, Paths: append([]string(nil), w.Paths...),
			OwnerSessionID: name(w.Owner), ReleaseCondition: w.ReleaseCondition,
			Created: secondsOf(w.CreatedAt),
		}
		for _, waiter := range w.Waiters {
			row := swiftstore.CoordinationWaiter{
				SessionID: name(waiter.Waiter), Reason: waiter.Reason, Created: secondsOf(waiter.CreatedAt),
				RequestDeliveredAt: secondsPtrOf(waiter.RequestDeliveredAt),
			}
			// A waiter who was told of the release, or who stopped waiting,
			// waits no longer; the projection reads both as released.
			if !waiter.Open() {
				done := waiter.ReleaseDeliveredAt
				if done.IsZero() {
					done = waiter.CancelledAt
				}
				row.ReleaseDeliveredAt = secondsPtrOf(done)
			}
			cw.Waiters = append(cw.Waiters, row)
		}
		out = append(out, cw)
	}
	return out
}

// ownDeliveries reads each on-screen terminal's newest `session.delivered`.
//
// The broker recorded it only for a session it could bind — a live assistant
// with a conversation — and wrote down which. So a receipt speaks for the
// session now in that terminal when it is the same conversation of the same
// assistant: the evidence the broker itself required when it accepted it.
func (s *Server) ownDeliveries(ctx context.Context, lives []swiftstore.Live) ([]swiftstore.SessionDelivery, error) {
	terminals := make([]string, 0, len(lives))
	for _, l := range lives {
		terminals = append(terminals, l.TerminalID)
	}
	events, err := s.store.LatestEvents(ctx, eventSessionDelivered, terminals)
	if err != nil {
		return nil, err
	}
	out := []swiftstore.SessionDelivery{}
	for _, l := range lives {
		e, ok := events[l.TerminalID]
		if !ok {
			continue
		}
		var p struct {
			Terminal     string `json:"terminal"`
			Conversation string `json:"conversation"`
			Assistant    string `json:"assistant"`
			Summary      string `json:"summary"`
			At           int64  `json:"at"`
		}
		if json.Unmarshal(e.Payload, &p) != nil || p.Conversation == "" ||
			p.Conversation != l.ConversationID || p.Assistant != l.Assistant {
			continue
		}
		assistant, pid, conv := l.Assistant, l.PID, l.ConversationID
		start := secondsOf(l.ProcessStart)
		out = append(out, swiftstore.SessionDelivery{
			Identity: swiftstore.Identity{TerminalID: l.TerminalID, TTY: l.TTY, Assistant: &assistant,
				PID: &pid, ProcessStart: &start, ConversationID: &conv},
			Summary:    p.Summary,
			ReportedAt: swiftstore.Seconds(p.At),
		})
	}
	return out, nil
}

// ownHandoffs converts the handoffs, and the label each names the session it
// opened with — by the terminal the broker opened for it.
func ownHandoffs(handoffs []orchestrator.Handoff) ([]swiftstore.Handoff, []swiftstore.HandoffLabel) {
	out := make([]swiftstore.Handoff, 0, len(handoffs))
	labels := []swiftstore.HandoffLabel{}
	for _, h := range handoffs {
		out = append(out, swiftstore.Handoff{ID: h.ID, State: h.State, FromSession: stringPtr(h.FromSession),
			Created: swiftstore.Seconds(h.CreatedAt)})
		if h.Opened != nil && h.Opened.TerminalID != "" && h.Title != "" {
			labels = append(labels, swiftstore.HandoffLabel{HandoffID: h.ID, Label: h.Title,
				Identity: swiftstore.RootAssignmentIdentity{TerminalID: h.Opened.TerminalID, Assistant: h.Assistant}})
		}
	}
	return out, labels
}

// ownAssignments converts the Feature Roots that have a terminal: the one the
// broker opened for the executor.
func ownAssignments(assignments []orchestrator.RootAssignment) []swiftstore.RootAssignment {
	out := make([]swiftstore.RootAssignment, 0, len(assignments))
	for _, a := range assignments {
		ra := swiftstore.RootAssignment{ID: a.ID, Label: a.Label, State: a.State}
		if a.Executor != nil && a.Executor.TerminalID != "" {
			ra.Identity = &swiftstore.RootAssignmentIdentity{TerminalID: a.Executor.TerminalID, Assistant: a.Assistant}
		}
		out = append(out, ra)
	}
	return out
}

// ownTaskLinks puts a task this daemon dispatched where the console looks for
// a task's place: the tab it opened (`child`, the chip and the header) and the
// session that asked for it (`root`, the indent under it). The root's tab is
// found as the Swift app finds one — the one session on screen holding the
// root's conversation — and a scheduled run's root is its schedule's title,
// as the Swift app labels one. Without these a child this daemon dispatched
// stood alone in the list as soon as the Swift store was not there to place it
// (cutover B1).
func ownTaskLinks(row *contract.TaskRow, t orchestrator.Record, screen []swiftstore.OnScreen) {
	if t.ChildTerminalID != "" || t.ChildBackend != "" {
		row.Child = &contract.TaskChild{TerminalID: t.ChildTerminalID, Backend: t.ChildBackend}
	}
	switch {
	case t.Root != nil:
		row.Root = &contract.TaskRoot{SessionID: t.Root.SessionID, Assistant: t.Root.Assistant, Label: t.Root.Label,
			TerminalID: swiftstore.ScreenTerminal(screen, t.Root.Assistant, t.Root.SessionID)}
	case t.ScheduleTitle != "":
		row.Root = &contract.TaskRoot{Label: t.ScheduleTitle}
	}
	if !t.SpawnedAt.IsZero() {
		row.SpawnedAt = t.SpawnedAt.Unix()
	}
	if !t.AcceptedAt.IsZero() {
		row.BriefedAt = t.AcceptedAt.Unix()
	}
	row.Model, row.WorkItemID, row.Permission = t.Model, t.WorkID, t.PermissionMode
	if t.Worktree != nil || t.Isolation == orchestrator.IsolationWorktree {
		row.Isolation = orchestrator.IsolationWorktree
	}
}

// lifecycleEvidence is the task records the Projects page's worktree
// lifecycle reads: the Swift store's, as before, and this daemon's own beside
// them. It is authoritative only when both were read whole — the Swift store
// read, or absent from this machine, and no stored task of ours undecodable —
// because a worktree whose owner could be in the part not read must not be
// offered for removal (DG-7). A Swift store switched off is not read: the
// Swift app's worktrees may belong to records nobody looked at, so the switch
// never makes this reading authoritative. Unknown never authorises a removal.
func (s *Server) lifecycleEvidence() projects.TaskEvidence {
	snap := s.swift.Read()
	out := lifecycleTasks(snap)
	out.Authoritative = (snap.Known && !snap.Stale) || snap.Source == swiftstore.SourceAbsent
	records, unreadable, err := s.broker.Records(context.Background())
	if err != nil || len(unreadable) > 0 {
		out.Authoritative = false
	}
	for _, r := range records {
		row := projects.Task{ID: r.ID, State: string(r.State), Title: r.Title,
			ChildTerminal: r.ChildTerminalID}
		if r.Root != nil {
			row.RootLabel, row.RootSession = r.Root.Label, r.Root.SessionID
		}
		row.Created = timePtr(r.CreatedAt)
		row.BriefedAt, row.SpawnedAt, row.FinishedAt = timePtr(r.AcceptedAt), timePtr(r.SpawnedAt), timePtr(r.FinishedAt)
		if r.Result != nil {
			row.Status = r.Result.Summary
		}
		if r.Landing != nil {
			row.Landing = &projects.TaskLanding{State: string(r.Landing.State), Target: r.Landing.Target,
				Commit: r.Landing.Commit}
		}
		if r.Worktree != nil {
			row.Worktree = &projects.TaskWorktree{Path: r.Worktree.Path, Branch: r.Worktree.Branch,
				Base: r.Worktree.Base}
		}
		out.Tasks = append(out.Tasks, row)
	}
	return out
}

// taskProjectDirs is every directory a task was dispatched for, the Swift
// store's and this daemon's own: the places the lifecycle looks for
// worktrees.
func (s *Server) taskProjectDirs() []string {
	dirs := []string{}
	for _, t := range s.swift.Read().Tasks {
		dirs = append(dirs, t.ProjectDir)
	}
	if records, _, err := s.broker.Records(context.Background()); err == nil {
		for _, r := range records {
			dirs = append(dirs, r.ProjectDir)
		}
	}
	return dirs
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
