package swiftstore

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
)

// The projections below are the Swift app's, restated over its records. Each
// names the function it follows; where this port cannot follow it, the comment
// says what differs and why.

func isTerminalState(state string) bool {
	switch state {
	case "success", "failure", "timeout", "cancelled", "spawn_failed":
		return true
	}
	return false
}

// taskMatches is Orchestrator.taskMatchesCurrentSession.
func taskMatches(t Task, l Live) bool {
	if l.Assistant == "" || t.Assistant != l.Assistant {
		return false
	}
	if deref(t.ChildTerminal) != l.TerminalID || t.ChildTTY == nil || !sameTTY(*t.ChildTTY, l.TTY) {
		return false
	}
	if t.ChildPID == nil || *t.ChildPID != l.PID || !sameStart(t.ChildProcStart, l.ProcessStart) {
		return false
	}
	return t.TranscriptProven && t.ChildSession != nil && *t.ChildSession == l.ConversationID && l.ConversationID != ""
}

// OwnTask is Orchestrator.taskForCurrentSession: the one task this exact
// process was opened for, or nil when there is not exactly one.
func (s Snapshot) OwnTask(l Live) *Task {
	var found *Task
	n := 0
	for i := range s.Tasks {
		t := &s.Tasks[i]
		if isTerminalState(t.State) && (t.SessionRoot || t.AttachSession != nil) {
			continue
		}
		if taskMatches(*t, l) {
			found = t
			n++
		}
	}
	if n != 1 {
		return nil
	}
	return found
}

func handoffSource(source *string, l Live) bool {
	if source == nil {
		return false
	}
	return *source == l.TerminalID || (l.ConversationID != "" && *source == l.ConversationID)
}

func hexID(id string) bool {
	if len(id) != 40 && len(id) != 64 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// verifiedTaskLanding is Orchestrator.isBrokerVerifiedTargetLanding.
func verifiedTaskLanding(l *Landing) bool {
	if l == nil || l.State != "landed" || l.LandedAt == nil ||
		deref(l.VerificationOrigin) != "local_target_branch" {
		return false
	}
	commit := deref(l.Commit)
	target := deref(l.VerifiedTargetCommit)
	if commit == "" || deref(l.VerifiedCommit) != commit || target == "" || deref(l.Target) == "" {
		return false
	}
	return hexID(commit) && hexID(target)
}

// verifiedSessionLanding is Orchestrator.isBrokerVerifiedSessionLanding. The
// Swift app also requires the repository path to be canonical; here it must be
// absolute and already clean, which is the same test for a path it wrote.
func verifiedSessionLanding(l *Landing) bool {
	if l == nil || deref(l.VerificationOrigin) != "local_target_branch" || l.LandedAt == nil {
		return false
	}
	repo := deref(l.RepositoryCommonDir)
	if !strings.HasPrefix(repo, "/") || strings.Contains(repo, "//") || strings.Contains(repo, "/./") ||
		strings.Contains(repo, "/../") || (len(repo) > 1 && strings.HasSuffix(repo, "/")) {
		return false
	}
	target := deref(l.Target)
	if target == "" || len([]rune(target)) > 200 {
		return false
	}
	return hexID(deref(l.VerifiedCommit)) && hexID(deref(l.VerifiedTargetCommit))
}

// Work is one row's work projection.
type Work struct {
	State        string
	Provenance   string
	Note         string
	Since        int64
	MovedBy      string
	PersonNeeded *bool
	Owed         *contract.WorkOwed
	Disposition  *contract.WorkDisposition
}

type workInputs struct {
	terminalState       string
	task                *Task
	hasWait             bool
	hasOpenHandoff      bool
	assignmentAbsent    bool
	hasSessionDelivery  bool
	hasSessionLanding   bool
	hasOutstandingChild bool
	selfClaim           string
}

// projectWorkState is Orchestrator.projectSessionWorkState.
func projectWorkState(in workInputs) string {
	switch {
	case in.terminalState == "waiting":
		return "waiting_you"
	case in.hasWait:
		return "waiting_session"
	case in.terminalState == "unknown":
		return "unknown"
	case in.terminalState == "working":
		return "working"
	case in.hasOutstandingChild:
		return "waiting_session"
	}
	if t := in.task; t != nil {
		if t.State == "success" && t.FinishedAt != nil {
			if verifiedTaskLanding(t.Landing) && !in.hasOpenHandoff {
				return "work_complete"
			}
			return "milestone_complete"
		}
		if isTerminalState(t.State) {
			return "unknown"
		}
	}
	if in.hasSessionLanding {
		return "work_complete"
	}
	if in.hasSessionDelivery {
		return "milestone_complete"
	}
	switch in.selfClaim {
	case "ready", "holding", "waiting_session":
		return in.selfClaim
	}
	if in.assignmentAbsent {
		return "ready"
	}
	return "unknown"
}

func (s Snapshot) sessionDelivery(l Live) *SessionDelivery {
	// Keyed by terminal id in the Swift registry, so the last record for a
	// terminal is the one it holds.
	var found *SessionDelivery
	for i := range s.SessionDeliveries {
		if s.SessionDeliveries[i].TerminalID == l.TerminalID {
			found = &s.SessionDeliveries[i]
		}
	}
	if found == nil || !found.Matches(l) {
		return nil
	}
	return found
}

func (s Snapshot) selfState(l Live) *SessionSelfState {
	var found *SessionSelfState
	for i := range s.SessionSelfStates {
		if s.SessionSelfStates[i].TerminalID == l.TerminalID {
			found = &s.SessionSelfStates[i]
		}
	}
	if found == nil || !found.Matches(l) {
		return nil
	}
	return found
}

func (s Snapshot) hasWait(terminalID string) bool {
	for _, w := range s.CoordinationWaits {
		for _, waiter := range w.Waiters {
			if waiter.ReleaseDeliveredAt == nil && (waiter.SessionID == terminalID || w.OwnerSessionID == terminalID) {
				return true
			}
		}
	}
	return false
}

func (s Snapshot) hasOpenHandoff(l Live) bool {
	for _, h := range s.Handoffs {
		if h.State != "delivered" && handoffSource(h.FromSession, l) {
			return true
		}
	}
	return false
}

func dispatchedBy(t Task, own *Task, l Live) bool {
	if own != nil && t.ParentTask != nil && *t.ParentTask == own.ID {
		return true
	}
	if l.ConversationID == "" || t.RootSession == nil || *t.RootSession != l.ConversationID {
		return false
	}
	return t.RootAssistant != nil && *t.RootAssistant == l.Assistant
}

// Work is Orchestrator.sessionWorkProjection.
func (s Snapshot) Work(l Live, terminalState string) Work {
	own := s.OwnTask(l)
	delivery := s.sessionDelivery(l)
	var landing *Landing
	if delivery != nil && verifiedSessionLanding(delivery.Landing) {
		landing = delivery.Landing
	}
	outstanding := false
	for _, t := range s.Tasks {
		if isTerminalState(t.State) {
			continue
		}
		if own != nil && t.ParentTask != nil && *t.ParentTask == own.ID {
			outstanding = true
			break
		}
		if l.ConversationID == "" || t.RootSession == nil || *t.RootSession != l.ConversationID {
			continue
		}
		// Rows written before root_assistant existed can only be Claude's.
		ra := "claude"
		if t.RootAssistant != nil {
			ra = *t.RootAssistant
		}
		if ra == l.Assistant {
			outstanding = true
			break
		}
	}
	self := s.selfState(l)
	var owed *contract.WorkOwed
	if self != nil && self.Owed != nil {
		owed = &contract.WorkOwed{
			Note:         self.Owed.Note,
			Since:        self.Owed.Since.Unix(),
			PersonNeeded: self.Owed.PersonNeeded,
			Provenance:   "self",
			MovedBy:      deref(self.Owed.MovedBy),
		}
	}
	in := workInputs{
		terminalState:       terminalState,
		task:                own,
		hasWait:             s.hasWait(l.TerminalID),
		hasOpenHandoff:      s.hasOpenHandoff(l),
		assignmentAbsent:    l.Assistant == "",
		hasSessionDelivery:  delivery != nil,
		hasSessionLanding:   landing != nil,
		hasOutstandingChild: outstanding,
	}
	withoutClaim := projectWorkState(in)
	if self != nil {
		in.selfClaim = deref(self.Claim)
	}
	state := projectWorkState(in)
	out := Work{State: state, Provenance: "broker", Owed: owed}

	if state != "milestone_complete" && state != "work_complete" {
		if self != nil && self.Claim != nil && state != withoutClaim {
			out.Provenance = "self"
			out.Note = deref(self.Note)
			if self.ClaimReportedAt != nil {
				out.Since = self.ClaimReportedAt.Unix()
			}
			out.MovedBy = deref(self.MovedBy)
			out.PersonNeeded = self.PersonNeeded
		}
		return out
	}
	if delivery != nil && own == nil {
		d := &contract.WorkDisposition{
			Scope:     "session",
			Title:     delivery.Summary,
			Evidence:  "authenticated_session_delivery",
			ReceiptAt: delivery.ReportedAt.Unix(),
		}
		if landing != nil {
			d.Evidence = "broker_verified_target_landing"
			d.Commit = deref(landing.VerifiedCommit)
			d.Target = deref(landing.Target)
			d.TargetCommit = deref(landing.VerifiedTargetCommit)
			d.LandedAt = landing.LandedAt.Unix()
		}
		out.Disposition = d
		return out
	}
	if own == nil {
		return Work{State: "unknown", Provenance: "broker", Owed: owed}
	}
	d := &contract.WorkDisposition{
		Scope:    "task",
		TaskID:   own.ID,
		Title:    own.Title,
		Evidence: "authenticated_task_delivery",
	}
	if state == "work_complete" {
		d.Evidence = "broker_verified_target_landing"
	}
	if own.FinishedAt != nil {
		d.ReceiptAt = own.FinishedAt.Unix()
	}
	if state == "work_complete" && own.Landing != nil {
		d.Commit = deref(own.Landing.VerifiedCommit)
		d.Target = deref(own.Landing.Target)
		d.TargetCommit = deref(own.Landing.VerifiedTargetCommit)
		if own.Landing.LandedAt != nil {
			d.LandedAt = own.Landing.LandedAt.Unix()
		}
	}
	out.Disposition = d
	return out
}

// Coordination is Orchestrator.coordination(forTerminal:), with the labels
// RemoteServer adds. `label` answers for a session id on screen, and "" for one
// that is not.
func (s Snapshot) Coordination(terminalID string, label func(string) string) *contract.SessionCoordination {
	waitingOn := []contract.CoordinationWaitRow{}
	waitedOnBy := []contract.CoordinationWaitRow{}
	for _, w := range s.CoordinationWaits {
		base := contract.CoordinationWaitRow{
			ID:               w.ID,
			Repository:       w.Repository,
			Paths:            append([]string{}, w.Paths...),
			OwnerSessionID:   w.OwnerSessionID,
			ReleaseCondition: w.ReleaseCondition,
			CreatedAt:        w.Created.Unix(),
		}
		for _, waiter := range w.Waiters {
			if waiter.ReleaseDeliveredAt != nil {
				continue
			}
			if waiter.SessionID == terminalID {
				row := base
				row.Reason = waiter.Reason
				row.WaiterCreatedAt = waiter.Created.Unix()
				row.OwnerLabel = label(w.OwnerSessionID)
				waitingOn = append(waitingOn, row)
			}
			if w.OwnerSessionID == terminalID {
				row := base
				row.WaiterSessionID = waiter.SessionID
				row.Reason = waiter.Reason
				row.WaiterLabel = label(waiter.SessionID)
				waitedOnBy = append(waitedOnBy, row)
			}
		}
	}
	if len(waitingOn) == 0 && len(waitedOnBy) == 0 {
		return nil
	}
	sort.SliceStable(waitingOn, func(i, j int) bool {
		if waitingOn[i].CreatedAt != waitingOn[j].CreatedAt {
			return waitingOn[i].CreatedAt < waitingOn[j].CreatedAt
		}
		return waitingOn[i].ID < waitingOn[j].ID
	})
	sort.SliceStable(waitedOnBy, func(i, j int) bool {
		if waitedOnBy[i].CreatedAt != waitedOnBy[j].CreatedAt {
			return waitedOnBy[i].CreatedAt < waitedOnBy[j].CreatedAt
		}
		return waitedOnBy[i].WaiterSessionID < waitedOnBy[j].WaiterSessionID
	})
	state := "has_waiters"
	if len(waitingOn) > 0 {
		state = "waiting_on_session"
	}
	return &contract.SessionCoordination{State: state, WaitingOn: waitingOn, WaitedOnBy: waitedOnBy}
}

// looseMatch is Orchestrator.rootAssignmentIdentityMatches: terminal and
// assistant must agree; the process facts only where they were recorded.
func looseMatch(id RootAssignmentIdentity, l Live) bool {
	if id.TerminalID != l.TerminalID || id.Assistant == "" || id.Assistant != l.Assistant {
		return false
	}
	if id.PID != nil && *id.PID != l.PID {
		return false
	}
	if id.ProcessStart != nil && (l.ProcessStart.IsZero() || id.ProcessStart.Unix() != l.ProcessStart.Unix()) {
		return false
	}
	if id.ConversationID != nil && *id.ConversationID != l.ConversationID {
		return false
	}
	return true
}

func liveAssignment(state string) bool { return state != "failed" && state != "inactive" }

// RootAssignment is Orchestrator.rootAssignmentSessionProjection.
func (s Snapshot) RootAssignment(l Live) *contract.RootAssignmentRecord {
	var found *RootAssignment
	n := 0
	for i := range s.RootAssignments {
		a := &s.RootAssignments[i]
		if a.Identity == nil || !liveAssignment(a.State) || !looseMatch(*a.Identity, l) {
			continue
		}
		found = a
		n++
	}
	if n != 1 {
		return nil
	}
	return &contract.RootAssignmentRecord{
		ID:          found.ID,
		Label:       found.Label,
		State:       found.State,
		Ownership:   "independent_root",
		Explanation: "owns_feature_lifecycle",
	}
}

// coordinatorCommands is Coordinator.swift's fixed table, in its order.
var coordinatorCommands = func() []contract.SessionCoordinatorCommand {
	unrouted := "No route carries a command from this panel into a session yet, so nothing can be sent."
	on := func(kind, effort, basis string) contract.SessionCoordinatorCommand {
		return contract.SessionCoordinatorCommand{Type: kind, Enabled: true, TokenEffort: effort, TokenEffortBasis: basis}
	}
	off := func(kind, reason, effort, basis, why string) contract.SessionCoordinatorCommand {
		return contract.SessionCoordinatorCommand{Type: kind, Enabled: false, Reason: reason, TokenEffort: effort, TokenEffortBasis: basis, Why: why}
	}
	return []contract.SessionCoordinatorCommand{
		on("status_report", "low", "registry_read"),
		on("duplicates_conflicts_ownership", "low", "registry_read"),
		on("landing_closure", "low", "registry_read"),
		on("scope_permissions", "low", "registry_read"),
		off("since_away", "no_return_ledger", "unknown", "unbuilt",
			"This machine does not record a return point yet, so there is nothing to read one against."),
		off("coordinate_work", "no_command_route", "unknown", "unbuilt", unrouted),
		off("dispatch_independent_work", "device_cannot_spawn", "high", "spawns_session",
			"A paired device can never start a session — that separation is deliberate, and this command will not cross it."),
		off("ask_coordinator", "no_command_route", "medium", "single_session_message", unrouted),
		on("deep_status_audit", "high", "session_fanout"),
		off("quiet_watch", "no_command_route", "unknown", "unbuilt", unrouted),
		off("stop", "no_command_route", "low", "broker_only", unrouted),
		off("reconnect", "machine_token_only", "low", "broker_only",
			"Reconnecting needs the machine's own orchestrator token, which a paired device deliberately does not hold."),
	}
}()

// Coordinator is Coordinator.sessionProjection: the Clawdfather record, on
// exactly the row whose live process is the registered one.
func (s Snapshot) CoordinatorFor(l Live) *contract.SessionCoordinator {
	c := s.Coordinator
	if c == nil || l.Assistant == "" || l.PID == 0 {
		return nil
	}
	if c.SessionID != l.TerminalID || c.Assistant != l.Assistant || !sameTTY(c.TTY, l.TTY) ||
		c.PID == nil || *c.PID != l.PID || !sameStart(c.ProcessStart, l.ProcessStart) ||
		deref(c.ConversationID) != l.ConversationID {
		return nil
	}
	return &contract.SessionCoordinator{
		Label:    "Clawdfather",
		Status:   "online",
		Commands: append([]contract.SessionCoordinatorCommand(nil), coordinatorCommands...),
	}
}

// Titles is the two rungs of a session's name the Swift store holds, and the
// automatic one below them.
type Titles struct {
	// Manual is a name typed in Clawdline (Config.sessionTitle).
	Manual string
	// Orchestrator is the task, Feature Root or handoff the tab was opened for
	// (Orchestrator.title(forTerminal:)).
	Orchestrator string
	// Automatic is a model-chosen name for a Claude conversation
	// (Config.automaticSessionTitle), the thread rung for Claude.
	Automatic string
}

// TitleOf answers the store's rungs for one live session. `customTitle` is the
// conversation's current `/rename`, which retires a stored name chosen before
// it; `live` is every session on screen, for the rule that a handoff or Feature
// Root label stops applying once its process is gone.
func (s Snapshot) TitleOf(l Live, customTitle string, live []Live) Titles {
	var out Titles
	if s.TitlesKnown {
		out.Manual = s.manualTitle(l, customTitle)
		if l.ConversationID != "" {
			for _, row := range s.Titles {
				if row.Automatic && deref(row.SessionID) == l.ConversationID {
					out.Automatic = row.Title
				}
			}
		}
	}
	// Over whatever records there are: the Swift store's when it was read,
	// and this daemon's own laid over them (own.go) either way.
	out.Orchestrator = s.orchestratorTitle(l.TerminalID, live)
	return out
}

// manualTitle is Config.sessionTitle(sessionID:terminalID:...).
func (s Snapshot) manualTitle(l Live, customTitle string) string {
	superseded := func(row SessionTitle) bool {
		if row.SeenTranscriptPath == nil {
			return false
		}
		return customTitle != deref(row.SeenCustomTitle)
	}
	if l.ConversationID != "" {
		var hit *SessionTitle
		for i := range s.Titles {
			row := &s.Titles[i]
			if !row.Automatic && deref(row.SessionID) == l.ConversationID {
				hit = row
			}
		}
		if hit != nil {
			if superseded(*hit) {
				return ""
			}
			return hit.Title
		}
	}
	var hit *SessionTitle
	for i := range s.Titles {
		row := &s.Titles[i]
		if row.Automatic || row.TerminalID != l.TerminalID {
			continue
		}
		if l.ConversationID == "" || row.SessionID == nil || *row.SessionID == l.ConversationID {
			hit = row
		}
	}
	if hit == nil {
		return ""
	}
	// Config.sameConversation: both absent is a match; one absent is not.
	switch {
	case hit.StartedAt == nil && l.ProcessStart.IsZero():
	case hit.StartedAt == nil || l.ProcessStart.IsZero():
		return ""
	case !sameStart(hit.StartedAt, l.ProcessStart):
		return ""
	}
	if superseded(*hit) {
		return ""
	}
	return hit.Title
}

// orchestratorTitle is OrchestratorRegistry.rebuildTerminalProjection's title
// half. The Swift app suppresses a handoff or Feature Root label, in memory,
// once no live session matches its recorded identity; this reads that rule
// directly off the sessions on screen. Tasks are applied oldest first so the
// newest record for a reused terminal id wins — the Swift app walks a
// dictionary there, whose order is not defined.
func (s Snapshot) orchestratorTitle(terminalID string, live []Live) string {
	alive := func(id RootAssignmentIdentity) bool {
		for _, l := range live {
			if looseMatch(id, l) {
				return true
			}
		}
		return false
	}
	found := ""
	labels := []string{}
	for _, hl := range s.HandoffLabels {
		if hl.Identity.TerminalID == terminalID && alive(hl.Identity) {
			labels = append(labels, hl.Label)
		}
	}
	if len(labels) == 1 {
		found = labels[0]
	}
	for _, a := range s.RootAssignments {
		if a.Identity == nil || a.Identity.TerminalID != terminalID || !liveAssignment(a.State) || !alive(*a.Identity) {
			continue
		}
		found = a.Label
	}
	for _, t := range tasksOldestFirst(s.Tasks) {
		if deref(t.ChildTerminal) != terminalID || t.AttachSession != nil {
			continue
		}
		title := t.Title
		if t.ScheduleID != nil && !strings.HasPrefix(title, "[Task]") {
			title = "[Task] " + title
		}
		found = title
	}
	return found
}

// CloseInput is what the closeability projection needs besides the store.
type CloseInput struct {
	Live          Live
	TerminalState string
	// Bound is RemoteServer's identityBound: assistant, pid, start time and
	// conversation all known.
	Bound bool
	// Matches is how many sessions on screen carry this exact identity.
	Matches             int
	InventoryComplete   bool
	InventoryObservedAt time.Time
	Now                 time.Time
	Generation          int64
	// Extra are this daemon's own obligations, as Swift's additionalObligations.
	Extra []contract.CloseReason
}

const inventoryMaxAge = 45 * time.Second

func moverSelf() contract.CloseMover {
	return contract.CloseMover{Kind: "session", Self: true}
}
func moverOther(id string) contract.CloseMover {
	return contract.CloseMover{Kind: "session", SessionID: id}
}
func moverPerson() contract.CloseMover {
	return contract.CloseMover{Kind: "person", PersonNeeded: true}
}
func moverTask(id string) contract.CloseMover {
	return contract.CloseMover{Kind: "task", TaskID: id}
}
func moverBroker() contract.CloseMover { return contract.CloseMover{Kind: "broker"} }

func reason(code, kind, subjectKind, subjectID string, m contract.CloseMover) contract.CloseReason {
	return contract.CloseReason{Code: code, Kind: kind, SubjectKind: subjectKind, SubjectID: subjectID, Mover: m}
}

// canBlockRoot is taskCanContributeToRootCloseability.
func canBlockRoot(t Task) bool {
	if !isTerminalState(t.State) {
		return true
	}
	closed := t.Landing != nil && t.Landing.State != "pending"
	if t.ResultVerifiedAt == nil && !t.Summary.Set() && !closed {
		return true
	}
	if t.Landing != nil && t.Landing.State == "pending" {
		return true
	}
	if t.CompletionDelivery != nil && t.CompletionDelivery.State != "acknowledged" {
		return true
	}
	if t.Worktree != nil && t.Worktree.Dirty != nil && *t.Worktree.Dirty && !closed {
		return true
	}
	return len(t.Claims) > 0 && !closed && !sameSet(t.UntouchedClaims, t.Claims)
}

func sameSet(a, b []string) bool {
	as := map[string]bool{}
	for _, x := range a {
		as[x] = true
	}
	bs := map[string]bool{}
	for _, x := range b {
		bs[x] = true
	}
	if len(as) != len(bs) {
		return false
	}
	for x := range as {
		if !bs[x] {
			return false
		}
	}
	return true
}

// candidates is CloseabilityRegistryIndex.candidates: the child-terminal
// bucket, then tasks rooted at this conversation, then children of this
// session's own task — each of the latter two only if it can still block.
func (s Snapshot) candidates(l Live) []Task {
	seen := map[string]bool{}
	out := []Task{}
	add := func(t Task) {
		if !seen[t.ID] {
			seen[t.ID] = true
			out = append(out, t)
		}
	}
	var own []Task
	for _, t := range s.Tasks {
		if deref(t.ChildTerminal) == l.TerminalID {
			add(t)
			if taskMatches(t, l) {
				own = append(own, t)
			}
		}
	}
	if l.ConversationID != "" && l.Assistant != "" {
		for _, t := range s.Tasks {
			if canBlockRoot(t) && t.RootSession != nil && t.RootAssistant != nil &&
				*t.RootSession == l.ConversationID && *t.RootAssistant == l.Assistant {
				add(t)
			}
		}
	}
	if len(own) == 1 {
		for _, t := range s.Tasks {
			if canBlockRoot(t) && t.ParentTask != nil && *t.ParentTask == own[0].ID {
				add(t)
			}
		}
	}
	return tasksOldestFirst(out)
}

// obligations is Orchestrator.closeabilityObligations.
func (s Snapshot) obligations(l Live) []contract.CloseReason {
	out := []contract.CloseReason{}
	tasks := s.candidates(l)
	own := Snapshot{Orchestrator: Orchestrator{Tasks: tasks}}.OwnTask(l)
	if own != nil && !isTerminalState(own.State) {
		out = append(out, reason("own_task_unfinished", "obligation", "task", own.ID, moverSelf()))
	}
	for _, t := range tasks {
		mine := dispatchedBy(t, own, l)
		if !mine {
			continue
		}
		if !isTerminalState(t.State) {
			m := moverTask(t.ID)
			if t.ChildTerminal != nil {
				m = moverOther(*t.ChildTerminal)
			}
			out = append(out, reason("live_descendant_task", "obligation", "task", t.ID, m))
			continue
		}
		closed := t.Landing != nil && t.Landing.State != "pending"
		if t.ResultVerifiedAt == nil && !t.Summary.Set() && !closed {
			out = append(out, reason("task_without_result", "obligation", "task", t.ID, moverSelf()))
		}
		if t.Landing != nil && t.Landing.State == "pending" {
			out = append(out, reason("pending_landing_owned", "obligation", "task", t.ID, moverSelf()))
		}
		if t.CompletionDelivery != nil && t.CompletionDelivery.State != "acknowledged" {
			out = append(out, reason("completion_undelivered", "obligation", "task", t.ID, moverSelf()))
		}
		if t.Worktree != nil && t.Worktree.Dirty != nil && *t.Worktree.Dirty && !closed {
			out = append(out, reason("dirty_isolated_worktree", "obligation", "task", t.ID, moverSelf()))
		}
		if len(t.Claims) > 0 && !closed && !sameSet(t.UntouchedClaims, t.Claims) {
			out = append(out, reason("touched_claims_without_closure", "obligation", "task", t.ID, moverSelf()))
		}
	}
	waits := append([]CoordinationWait(nil), s.CoordinationWaits...)
	sort.SliceStable(waits, func(i, j int) bool { return waits[i].Created < waits[j].Created })
	for _, w := range waits {
		pending := false
		waiting := false
		for _, waiter := range w.Waiters {
			if waiter.ReleaseDeliveredAt == nil {
				pending = true
				if waiter.SessionID == l.TerminalID {
					waiting = true
				}
			}
		}
		if !pending {
			continue
		}
		if w.OwnerSessionID == l.TerminalID {
			out = append(out, reason("coordination_wait_owned", "obligation", "wait", w.ID, moverSelf()))
		} else if waiting {
			out = append(out, reason("coordination_wait_waiting", "obligation", "wait", w.ID, moverOther(w.OwnerSessionID)))
		}
	}
	handoffs := append([]Handoff(nil), s.Handoffs...)
	sort.SliceStable(handoffs, func(i, j int) bool { return handoffs[i].Created < handoffs[j].Created })
	for _, h := range handoffs {
		if h.State != "delivered" && handoffSource(h.FromSession, l) {
			out = append(out, reason("open_handoff", "obligation", "handoff", h.ID, moverSelf()))
		}
	}
	if self := s.selfState(l); self != nil && self.Owed != nil {
		m := moverSelf()
		if self.Owed.PersonNeeded {
			m = moverPerson()
		}
		out = append(out, reason("owed_decision", "obligation", "session", l.TerminalID, m))
	}
	return out
}

func (s Snapshot) attestation(l Live) *ClosureAttestation {
	var found *ClosureAttestation
	for i := range s.ClosureAttestations {
		if s.ClosureAttestations[i].TerminalID == l.TerminalID {
			found = &s.ClosureAttestations[i]
		}
	}
	return found
}

// version is Orchestrator.closeabilityVersion.
func version(l Live, activity, obligation int64, state string) string {
	dash := func(v string) string {
		if v == "" {
			return "-"
		}
		return v
	}
	pid := "-"
	if l.PID != 0 {
		pid = strconv.FormatInt(l.PID, 10)
	}
	start := "-"
	if !l.ProcessStart.IsZero() {
		start = strconv.FormatInt(l.ProcessStart.Unix(), 10)
	}
	fields := []string{"cl1", l.TerminalID, dash(l.Assistant), "/dev/" + strings.TrimPrefix(l.TTY, "/dev/"),
		pid, start, dash(l.ConversationID),
		strconv.FormatInt(activity, 10), strconv.FormatInt(obligation, 10), "-", state}
	sum := sha256.Sum256([]byte(strings.Join(fields, "\x01")))
	return "cl1_" + hex.EncodeToString(sum[:])[:32]
}

func sameMover(a, b contract.CloseMover) bool { return a == b }

// Closeability is Orchestrator.sessionCloseability → projectCloseability.
//
// A store that could not be read at all is one more evidence reason, so the
// answer is `unknown` with whatever else is known listed under it — never a
// short list that reads as complete. A store that is absent, or switched off,
// is not unread: it is known to hold nothing, and this daemon's own records
// laid over it (own.go) are the whole list.
func (s Snapshot) Closeability(in CloseInput) contract.Closeability {
	l := in.Live
	evidence := []contract.CloseReason{}
	if !s.Usable() {
		evidence = append(evidence, reason("swift_store_unreadable", "evidence", "", "", moverBroker()))
	}
	if !in.Bound {
		evidence = append(evidence, reason("session_identity_unbound", "evidence", "", "", moverBroker()))
	}
	age := in.Now.Sub(in.InventoryObservedAt)
	overAge := age < 0 || age > inventoryMaxAge
	freshness := "current"
	switch {
	case in.InventoryObservedAt.IsZero():
		evidence = append(evidence, reason("session_inventory_missing", "evidence", "", "", moverBroker()))
		freshness = "missing"
	case !in.InventoryComplete || overAge:
		evidence = append(evidence, reason("session_inventory_stale", "evidence", "", "", moverBroker()))
		freshness = "stale"
	}
	if in.Matches != 1 {
		evidence = append(evidence, reason("session_identity_ambiguous", "evidence", "session", l.TerminalID, moverBroker()))
	}
	if in.TerminalState == "unknown" {
		evidence = append(evidence, reason("terminal_unreadable", "evidence", "session", l.TerminalID, moverBroker()))
	}

	obligations := []contract.CloseReason{}
	switch in.TerminalState {
	case "working":
		obligations = append(obligations, reason("terminal_working", "obligation", "session", l.TerminalID, moverSelf()))
	case "waiting":
		obligations = append(obligations, reason("terminal_waiting_you", "obligation", "session", l.TerminalID, moverPerson()))
	}
	obligations = append(obligations, s.obligations(l)...)
	obligations = append(obligations, in.Extra...)

	activity := s.Activity[l.TerminalID]
	obligationGen := s.ObligationGeneration
	provenance := []string{"broker"}
	attestations := []contract.CloseReason{}
	var attestationID *string
	att := s.attestation(l)
	matched := att != nil && att.Matches(l)
	if len(evidence) == 0 && len(obligations) == 0 {
		if matched {
			provenance = append(provenance, "self")
			if att.ActivityGeneration == activity && att.ObligationGeneration == obligationGen {
				id := att.ID
				attestationID = &id
			} else {
				attestations = append(attestations, reason("attestation_superseded", "attestation", "attestation", att.ID, moverSelf()))
			}
		} else {
			attestations = append(attestations, reason("attestation_missing", "attestation", "session", l.TerminalID, moverSelf()))
		}
	} else if matched {
		provenance = append(provenance, "self")
	}

	state := "needs_attestation"
	switch {
	case len(evidence) > 0:
		state = "unknown"
	case len(obligations) > 0:
		state = "blocked"
	case attestationID != nil:
		state = "safe"
	}
	reasons := append(append(evidence, obligations...), attestations...)
	var unique []contract.CloseMover
	for _, r := range reasons {
		dup := false
		for _, m := range unique {
			if sameMover(m, r.Mover) {
				dup = true
				break
			}
		}
		if !dup {
			unique = append(unique, r.Mover)
		}
	}
	var mover *contract.CloseMover
	if len(unique) == 1 {
		m := unique[0]
		mover = &m
	}
	return contract.Closeability{
		State:                contract.CloseabilityState(state),
		Reasons:              reasons,
		ObservedAt:           in.Now.Unix(),
		ActivityGeneration:   activity,
		ObligationGeneration: obligationGen,
		Version:              version(l, activity, obligationGen, state),
		Provenance:           provenance,
		Source: contract.CloseSource{
			Provenance:    "session_watch",
			Freshness:     freshness,
			ObservedAt:    in.InventoryObservedAt.Unix(),
			MaxAgeSeconds: int64(inventoryMaxAge / time.Second),
		},
		SessionGeneration: in.Generation,
		AttestationID:     attestationID,
		Mover:             mover,
	}
}
