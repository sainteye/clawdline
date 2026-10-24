package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/limits"
	"github.com/sainteye/clawdline/internal/adapters/ownrecords"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/coordinator"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/task"
)

// epoch identifies this process. A client that reconnects to a restarted daemon
// must be able to tell that the generation counter started over rather than
// went backwards.
var epoch = time.Now().Unix()

var generation atomic.Int64

// sessions answers the route the console actually reads.
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// The product every stream reads, when it is not older than one tick
	// (producer.go): a page's first read, the Cloud fleet's and the streams'
	// are one build of the machine, not one each. The build keeps the eight
	// seconds this route gave its own.
	var snapshot sessionsSnapshotWire
	if r.URL.Query().Get("parts") == "list" && r.URL.Query().Get("fresh") == "1" {
		// A person's send must check the current execution against a scan
		// begun after the press, not the producer's previous list tick.
		fresh, err := s.lists().sessions.since(time.Now())
		if err != nil {
			writeRefusal(w, http.StatusServiceUnavailable, "execution_check_unavailable", "The current Session list could not be read.")
			return
		}
		snapshot = fresh
	} else {
		snapshot = s.lists().snapshot()
	}
	if look := r.URL.Query().Get("look"); look != "" {
		// A person pressed "read again" on a waiting Session whose question
		// has not come: that Session's screen is captured now rather than
		// served from what is held, and the list is built now rather than
		// taken from a product up to a tick old. The streams read the same
		// product, so the next event carries the question too.
		s.inventory.Held.Pressed(look)
		if fresh, err := s.lists().sessions.since(time.Now()); err == nil {
			snapshot = fresh
		}
	}
	// A shared list snapshot may predate an icon file edit. Recompute only its
	// presentation marks for this response, without changing the producer's rows.
	if err := s.refreshSnapshotIcons(&snapshot); err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "icon_store_unavailable", "Project icons could not be read.")
		return
	}
	if r.URL.Query().Get("parts") == "list" {
		// The Cloud fleet asks once per machine. Reuse the local single-machine
		// snapshot, including the fields used by the original row status line.
		// No transcript, menu or Git data leaves this route.
		type status struct {
			SessionID         string                        `json:"sessionId,omitempty"`
			EpicParent        *contract.EpicSessionParent   `json:"epic_parent,omitempty"`
			Persona           string                        `json:"persona,omitempty"`
			State             contract.SessionState         `json:"state"`
			WorkState         contract.WorkState            `json:"work_state"`
			WorkNote          string                        `json:"work_note,omitempty"`
			WorkProvenance    contract.WorkProvenance       `json:"work_provenance,omitempty"`
			WorkMovedBy       string                        `json:"work_moved_by,omitempty"`
			WorkPersonNeeded  *bool                         `json:"work_person_needed,omitempty"`
			Line              string                        `json:"line,omitempty"`
			BackgroundReading string                        `json:"background_reading,omitempty"`
			BackgroundOmitted int64                         `json:"background_omitted,omitempty"`
			Shells            []contract.SessionShell       `json:"shells,omitempty"`
			HeavyWork         *contract.SessionHeavyWork    `json:"heavy_work,omitempty"`
			Source            *contract.BearingsSource      `json:"source,omitempty"`
			Activity          *contract.SessionActivity     `json:"activity,omitempty"`
			Identity          contract.IdentityBinding      `json:"identity,omitempty"`
			AttentionCount    *int64                        `json:"attention_count,omitempty"`
			Owed              *contract.WorkOwed            `json:"owed,omitempty"`
			Acceptance        *contract.SessionAcceptance   `json:"acceptance,omitempty"`
			Disposition       *contract.WorkDisposition     `json:"disposition,omitempty"`
			Coordination      *contract.SessionCoordination `json:"coordination,omitempty"`
			// The signed ss/ rows carry these two; this list stands in for
			// them while ss/ has a gap, and without them the machine's
			// Clawdfather was drawn as an ordinary Session under its own title.
			MachineScope      bool                         `json:"machine_scope,omitempty"`
			CoordinatorBound  bool                         `json:"coordinator_bound,omitempty"`
			WaitingFor        []contract.SessionWaitTarget `json:"waiting_for,omitempty"`
			WaitingForOmitted int64                        `json:"waiting_for_omitted,omitempty"`
		}
		type presentation struct {
			Assistant           contract.Assistant `json:"assistant,omitempty"`
			ID                  string             `json:"id"`
			Title               string             `json:"title"`
			CWD                 string             `json:"cwd,omitempty"`
			ProjectKey          string             `json:"project_key,omitempty"`
			Icon                *contract.Icon     `json:"icon,omitempty"`
			ExecutionGeneration string             `json:"execution_generation,omitempty"`
			Status              status             `json:"status"`
		}
		rows := make([]presentation, 0, len(snapshot.Sessions))
		for _, row := range snapshot.Sessions {
			rows = append(rows, presentation{Assistant: row.Assistant, ID: row.ID, Title: row.Label, CWD: row.CWD, ProjectKey: row.ProjectKey,
				Icon: row.Icon, ExecutionGeneration: row.ExecutionGeneration,
				Status: status{SessionID: row.SessionID, EpicParent: row.EpicParent, Persona: row.Persona, State: row.State, WorkState: row.WorkState, WorkNote: row.WorkNote,
					WorkProvenance: row.WorkProvenance, WorkMovedBy: row.WorkMovedBy,
					WorkPersonNeeded: row.WorkPersonNeeded, Line: row.Line, Shells: row.Shells, BackgroundReading: row.BackgroundReading, BackgroundOmitted: row.BackgroundOmitted,
					HeavyWork: row.HeavyWork, Source: row.Source, Activity: row.Activity,
					Identity: row.Identity, AttentionCount: row.AttentionCount,
					Owed: row.Owed, Acceptance: row.Acceptance,
					Disposition: row.Disposition, Coordination: row.Coordination,
					MachineScope: row.MachineScope, CoordinatorBound: row.Coordinator != nil,
					WaitingFor: row.WaitingFor, WaitingForOmitted: row.WaitingForOmitted}})
		}
		_ = json.NewEncoder(w).Encode(struct {
			At       int64          `json:"at"`
			Complete bool           `json:"complete"`
			Sessions []presentation `json:"sessions"`
		}{At: snapshot.At, Complete: cloudListComplete(snapshot.Scan), Sessions: rows})
		return
	}
	_ = json.NewEncoder(w).Encode(snapshot)
}

func (s *Server) refreshSnapshotIcons(snapshot *sessionsSnapshotWire) error {
	if s.icons == nil {
		return nil
	}
	if err := s.icons.Refresh(); err != nil {
		return err
	}
	// A copy that stays a list when it is empty: appending nothing to a nil
	// slice gives nil, which went out as `"sessions": null`, and a machine with
	// no sessions then read as one still loading (the header waited forever).
	rows := make([]sessionRowWire, len(snapshot.Sessions))
	copy(rows, snapshot.Sessions)
	for i := range rows {
		rows[i].Icon = wireIcon(s.icons.For(rows[i].CWD))
	}
	snapshot.Sessions = rows
	return nil
}

// The compact Cloud list covers the sources this person enabled. The merged
// local scan remains partial while a source is deliberately turned off, but
// that source is outside the Cloud list's promised set. A failed enabled
// source still makes the list partial. This matches the signed ss/ publisher.
func cloudListComplete(scan contract.Scan) bool {
	if scan.Complete || scan.EmptyAuthoritative {
		return true
	}
	if len(scan.Sources) == 0 {
		return false
	}
	disabled := false
	for _, source := range scan.Sources {
		if source.Complete {
			continue
		}
		if source.Disabled == "" {
			return false
		}
		disabled = true
	}
	return disabled
}

// sessionRowWire is a row as it is sent. It exists for one key the contract
// generator cannot express: `work_person_needed` is optional, and `false` is a
// real answer the console tests for (`=== false`), so it must be a pointer
// rather than the generated bool, whose omitempty would drop the false. The
// outer field shadows the embedded one for encoding/json.
type sessionRowWire struct {
	contract.SessionRow
	processPID   int
	processStart time.Time
	// A successful zero must survive omitempty on the generated integer.
	AttentionCount   *int64 `json:"attention_count,omitempty"`
	WorkPersonNeeded *bool  `json:"work_person_needed,omitempty"`
	// Menu shadows the generated field for the same reason: a multi-select's
	// unticked row is `checked: false`, which the generated bool would drop.
	Menu *menuWire `json:"menu,omitempty"`
	// waiting is the work projection's live tasks behind a broker-decided
	// waiting_session, turned into `waiting_for` once labels are known.
	waiting []*ownrecords.Task
}

// sessionsSnapshotWire is contract.SessionsSnapshot with those rows.
type sessionsSnapshotWire struct {
	At       int64            `json:"at"`
	Scan     contract.Scan    `json:"scan"`
	Sessions []sessionRowWire `json:"sessions"`
}

// sessionsPayload builds the one snapshot both the route and the event stream
// publish. They are the same payload, so they are the same code: two builders
// would drift, and the client would have no way to tell which one it got.
func (s *Server) sessionsPayload(ctx context.Context) sessionsSnapshotWire {
	return s.sessionsPayloadFrom(ctx, s.reading(ctx))
}

// sessionsPayloadFrom is that snapshot built from a reading the caller already
// took, for the broker's lists that need the reading itself beside the rows
// (orchestrator.go): one reading, so a row and the source completeness read
// with it are of the same moment.
func (s *Server) sessionsPayloadFrom(ctx context.Context, inv session.Inventory) sessionsSnapshotWire {
	return s.sessionsPayloadRead(ctx, inv, s.sessionTasks)
}

// sessionLedgerRead reads the broker's tasks one build of the list projects
// from, for the sessions in a reading.
type sessionLedgerRead func(ctx context.Context, sessions []session.Session) ([]orchestrator.Record, []orchestrator.Unreadable, error)

// sessionTasks is the part of the ledger the sessions can be told by
// (Broker.SessionTasks): the list is rebuilt every two seconds, and the whole
// history it used to read three times a build is thousands of finished tasks
// no row projects.
func (s *Server) sessionTasks(ctx context.Context, sessions []session.Session) ([]orchestrator.Record, []orchestrator.Unreadable, error) {
	if s.broker == nil {
		return nil, nil, nil
	}
	terminals := make([]string, 0, len(sessions))
	for _, item := range sessions {
		terminals = append(terminals, item.ID)
	}
	return s.broker.SessionTasks(ctx, terminals)
}

// sessionsPayloadRead is sessionsPayloadFrom with the tasks read by read.
func (s *Server) sessionsPayloadRead(ctx context.Context, inv session.Inventory, read sessionLedgerRead) sessionsSnapshotWire {
	// This daemon's own records get a clock of their own, and not what the
	// terminal scan left of the request's (recordsContext).
	records, releaseRecords := recordsContext(ctx)
	defer releaseRecords()

	// One reading of the broker's tasks for the whole build: what is owed,
	// the callbacks each root runs and the tasks the rows project are all
	// cut from it, so no two of them describe different moments.
	tasks, unreadableTasks, tasksErr := read(records, inv.Sessions)

	// One reading of what is owed, for the whole list, against this same
	// reading of the sessions. Asking per row would ask the same question
	// eight times and let two rows disagree about the same moment. An
	// unreadable broker makes the projection incomplete, not wrong in one
	// place: it is carried as missing evidence rather than as zero.
	var owed []task.Obligation
	owedErr := tasksErr
	if tasksErr == nil && s.broker != nil {
		owed, owedErr = s.broker.OwedOf(tasks, unreadableTasks, inv.Sessions)
	}
	// The record set the projections read: this daemon's own, laid in below.
	// The retired Swift app's store is no longer read (docs/cutover.md §7).
	swift := ownrecords.Snapshot{}
	// And of this daemon's own machine role (W5): when it holds one, the
	// crown is drawn from it and from nothing else — one role, one source.
	// An unreadable role draws no crown.
	role, roleStatus, _ := s.store.Coordinator(records)
	ownRole := roleStatus == store.CoordinatorReady

	// Only assistant sessions are rows. A terminal running an ordinary shell is
	// not a session in this contract — the Swift app carries shells as an
	// attribute of the session that left them running, and publishing them as
	// rows of their own turned eight cards into eighteen, ten of which the
	// console could only describe as unreadable.
	items := make([]session.Session, 0, len(inv.Sessions))
	for _, item := range inv.Sessions {
		if item.IsAssistant() && !item.DiscoveryExcluded {
			items = append(items, item)
		}
	}
	lives := make([]ownrecords.Live, len(items))
	for i, item := range items {
		lives[i] = liveOf(item)
	}
	// A generation belongs to a verified process, not to this list read. A
	// failed source or database write leaves it absent rather than reusing the
	// last value as authority for a new action.
	executionGenerations := s.observeExecutions(records, inv, items, lives)
	matches := identityMatchCounts(items)
	conversationMatches := map[string]int{}
	for _, item := range items {
		if item.ConversationID != "" {
			conversationMatches[item.ConversationID]++
		}
	}
	heavySession, heavyWork, heavyPlaces := s.runningHeavyCallback(records)
	callbacks, callbacksKnown := map[string][]contract.SessionShell{}, true
	if s.broker != nil {
		callbacks, callbacksKnown = callbackShells(tasks, unreadableTasks, tasksErr, heavyPlaces)
	}
	// One database read for the entire fleet. An error leaves the count
	// unknown on every row, rather than turning it into zero.
	attentionCounts, attentionErr := s.store.OpenHumanInterventionCounts(records)
	s.lastScreen.Store(&screenReading{at: time.Now(), rows: onScreen(items)})
	// The assignment actor is the stable source of Epic ancestry. A missing
	// Board reading leaves a Root independent instead of inventing a parent.
	epicParents := map[string]store.EpicRootParent{}
	if s.store != nil {
		byAssistant := map[string][]string{}
		for _, item := range items {
			if item.ConversationID != "" && item.Assistant != "" {
				byAssistant[string(item.Assistant)] = append(byAssistant[string(item.Assistant)], item.ConversationID)
			}
		}
		for assistant, ids := range byAssistant {
			parents, err := s.store.WorkV2EpicRootParentsForSessions(records, assistant, ids)
			if err != nil {
				continue
			}
			for conversation, parent := range parents {
				epicParents[assistant+"\x00"+conversation] = parent
			}
		}
	}

	// One Board reading of what each conversation delivered and the person has
	// not opened. Unreadable is carried to every row as unknown, never as none.
	var pending map[string]store.PendingAcceptance
	pendingErr := errBoardUnreadable
	if s.store != nil {
		ids := make([]string, 0, len(items))
		for _, item := range items {
			if item.ConversationID != "" {
				ids = append(ids, item.ConversationID)
			}
		}
		pending, pendingErr = s.store.WorkV2PendingAcceptanceForSessions(records, ids)
	}

	// This daemon's own tasks, waits, deliveries, handoffs and Feature Roots,
	// in the Swift store's shape, so the one set of rules reads both
	// (ownrecords.go). A source of ours that could not be read is evidence
	// missing for every row, as an unreadable Swift store is.
	own, ownErr := s.ownOverlayOf(records, lives, tasks, unreadableTasks, tasksErr)
	swift = swift.With(own)
	// A delivery is historical. A later message typed by the person starts new
	// work, even when the terminal has already gone idle again. Read the run
	// ledger once for the fleet; sequence numbers order sends and receipts that
	// happened in the same second.
	runSubjects := make([]string, 0, len(items))
	deliverySubjects := make([]string, 0, len(items))
	for _, item := range items {
		runSubjects = append(runSubjects, item.ConversationID)
		deliverySubjects = append(deliverySubjects, item.ID)
	}
	runs, runsErr := s.store.LatestEvents(records, app.EventRunLatest, runSubjects)
	deliveries, deliveriesErr := s.store.LatestEvents(records, eventSessionDelivered, deliverySubjects)
	responsibilities, responsibilityErr := s.store.OpenSessionResponsibilities(records)
	openBySession := make(map[string][]store.SessionResponsibility)
	for _, item := range responsibilities {
		openBySession[item.Session] = append(openBySession[item.Session], item)
	}
	now := time.Now()
	// Names chosen in this daemon live in its own config.json and outrank the
	// retired app's read-only rows. They are laid onto the same projection so
	// the title ladder itself still has one implementation.
	swift = s.withOwnSessionTitles(swift, now)

	// Names first, because a coordination wait names the sessions on either
	// side of it by the label this list gives them.
	labels := make(map[string]string, len(items))
	for i, item := range items {
		project := ""
		if s.icons != nil {
			project = s.icons.Label(item.CWD)
		}
		if project == "" && item.CWD != "" {
			project = filepath.Base(item.CWD)
		}
		labels[item.ID] = rowLabel(item, swift.TitleOf(lives[i], item.CustomTitle, lives), project)
	}
	labelOf := func(id string) string { return labels[id] }

	// One generation for the whole snapshot, taken before the rows are built,
	// so every row's closeability names the same reading it arrived with.
	gen := generation.Add(1)
	rows := make([]sessionRowWire, 0, len(items))
	for i, item := range items {
		row := s.sessionRow(rowInput{
			item:                item,
			live:                lives[i],
			label:               labels[item.ID],
			labelOf:             labelOf,
			matches:             matches[item.ID],
			swift:               swift,
			owed:                owed,
			owedErr:             owedErr,
			ownErr:              ownErr,
			closure:             openBySession[item.ConversationID],
			closureErr:          responsibilityErr,
			inv:                 inv,
			now:                 now,
			generation:          gen,
			executionGeneration: executionGenerations[item.ID],
			role:                role,
			ownRole:             ownRole,
			epicParent:          epicParents[string(item.Assistant)+"\x00"+item.ConversationID],
			acceptance:          rowAcceptance(item.ConversationID, pending, pendingErr),
			latestRun:           runs[item.ConversationID],
			latestRunErr:        runsErr,
			latestDelivery:      deliveries[item.ID],
			latestDeliveryErr:   deliveriesErr,
		})
		// Every list reads what a Session waits for from the row, never from a
		// task collection of its own: the Cloud lists do not have one.
		if len(row.waiting) > 0 {
			row.WaitingFor, row.WaitingForOmitted = s.waitTargets(records, row.waiting, labels, heavyPlaces)
		}
		if attentionErr == nil && item.ConversationID != "" && conversationMatches[item.ConversationID] == 1 &&
			row.Source != nil && row.Source.Freshness == contract.SourceFreshnessCurrent &&
			(item.Binding == session.BindingCommandLine || item.Binding == session.BindingOpenFile ||
				item.Binding == session.BindingLiveTitle || item.Binding == session.BindingRegistry) {
			count := attentionCounts[item.ConversationID]
			row.AttentionCount = &count
		}
		if heavyWork != nil && item.ConversationID == heavySession && conversationMatches[heavySession] == 1 &&
			row.Source != nil && row.Source.Freshness == contract.SourceFreshnessCurrent {
			row.HeavyWork = heavyWork
		}
		row.BackgroundReading = "unavailable"
		if callbacksKnown && item.ConversationID != "" && conversationMatches[item.ConversationID] == 1 && row.Source != nil && row.Source.Freshness == contract.SourceFreshnessCurrent {
			row.BackgroundReading = "complete"
			jobs := callbacks[item.ConversationID]
			if len(jobs) > orchestrator.MaxCallbacksPerRoot {
				row.BackgroundOmitted = int64(len(jobs) - orchestrator.MaxCallbacksPerRoot)
				jobs = jobs[:orchestrator.MaxCallbacksPerRoot]
			}
			row.Shells = append(row.Shells, jobs...)
		}

		if item.State == session.StateWorking && item.Line != "" {
			at := item.Observation.ObservedAt
			if at.IsZero() {
				at = inv.ObservedAt
			}
			row.WorkingSince = s.turns.since(item.ID, item.Line, at)
		}
		rows = append(rows, row)
	}
	s.turns.keep(rows)

	return sessionsSnapshotWire{
		At:       now.Unix(),
		Sessions: rows,
		Scan: contract.Scan{
			Epoch:      epoch,
			Generation: gen,
			Complete:   inv.Complete,
			Notes:      append([]string(nil), inv.Notes...),
			Provenance: inv.Provenance,
			// An empty list is only authoritative when the reading was
			// complete. Saying so here is what stops a client from treating a
			// failed scan as "every session went away".
			// A source turned off was not looked at, so an empty list says
			// nothing about it either.
			EmptyAuthoritative: inv.Complete && len(inv.DisabledSources) == 0 && len(rows) == 0,
			Completed: contract.ScanCompleted{
				Sequence: gen,
				Complete: inv.Complete,
			},
			Source: wireInventoryObservation(inv),
			// Each source's own answer travels beside the AND. A reader
			// deciding whether a session it remembers is really gone needs the
			// answer of the source that would have seen it: the AND is false
			// whenever any source failed, so on its own it would make every
			// absence unprovable and nothing could ever be tombstoned.
			Sources: scanSources(inv.Sources, inv.Gaps, inv.DisabledSources),
		},
	}
}

// changeIdentity is the part of a list that a reader would read differently,
// which is what a stream compares before it sends a frame.
//
// Three things move on every build and say nothing new, and they are the
// paths the Cloud publisher has always left out of its own comparison
// (internal/transport/cloud freshnessOnlySessionRowFields). The working line's
// own clock moves every second a session works ("Thinking… (1m 12s)"): a page
// that has `working_since` draws that clock itself, and one that does not gets
// the line again on the next real change. Each row's closeability names the
// build it came from (`session_generation`), which nothing on a page reads.
// And a current row's reading clock (`observed_at`) moves with every reading:
// a page reads it only as the age of a row that is not current — the retained
// reading's note — so on an unverified row it is still compared. Measured on
// 2026-10-09 over five minutes, the reading clock alone made every one of 150
// frames differ from the last; without it, 80 did.
func changeIdentity(rows []sessionRowWire) ([]byte, error) {
	copied := make([]sessionRowWire, len(rows))
	for i, row := range rows {
		if row.State == contract.SessionStateWorking && row.Line != "" {
			row.Line = session.WithoutElapsed(row.Line)
		}
		row.Closeability.SessionGeneration = 0
		if row.Source != nil && row.Source.Freshness == contract.SourceFreshnessCurrent {
			source := *row.Source
			source.ObservedAt = 0
			row.Source = &source
			row.Closeability.ObservedAt = 0
			row.Closeability.Source.ObservedAt = 0
		}
		copied[i] = row
	}
	return json.Marshal(copied)
}

// workingClockDriftSeconds is how far one turn's start may wander between two
// readings and still be the same turn. The start is worked out as the moment
// the line was read less the clock drawn in it, and both are whole seconds
// read at slightly different moments, so one turn's start reads a second or
// two either way from one reading to the next. A new turn starts from zero,
// which is further from the last one than this.
const workingClockDriftSeconds = 5

// workingClock keeps each working session's turn start steady, so that it
// can be compared: a start that wandered by a second on every reading would be
// a change on every reading, which is what it exists to stop.
//
// A turn is told from the next by its clock going back, not by its start
// moving. The line is the screen as last drawn, and a provider stops redrawing
// its clock while a tool runs: on the running daemon (2026-10-09) one line read
// "12m 18s" for six readings over 30 seconds, so a start worked out from it
// moved five seconds a reading and was a new `working_since` every other one —
// 285 changes in 20 minutes, more row changes than the clock it replaced.
type workingClock struct {
	mu     sync.Mutex
	starts map[string]turnStart
}

// turnStart is one working session's turn as last read: when it began, and
// the clock its line showed then.
type turnStart struct {
	at, elapsed int64
}

// since is when the turn a working line reports began, in unix seconds, or 0
// when the line carries no clock or was not read at a known moment.
func (c *workingClock) since(id, line string, at time.Time) int64 {
	_, _, elapsed, ok := session.ElapsedSpan(line)
	if !ok || at.IsZero() {
		return 0
	}
	start := at.Unix() - int64(elapsed)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.starts == nil {
		c.starts = map[string]turnStart{}
	}
	held, ok := c.starts[id]
	if !ok || int64(elapsed) < held.elapsed {
		// The first reading of a turn, or a clock that went back: a new turn.
		c.starts[id] = turnStart{at: start, elapsed: int64(elapsed)}
		return start
	}
	// The same turn. A line whose clock stood still while it was read again
	// puts the start later than it was, so a start only ever moves earlier,
	// and then only by more than reading jitter.
	if start < held.at-workingClockDriftSeconds {
		held.at = start
	}
	held.elapsed = int64(elapsed)
	c.starts[id] = held
	return held.at
}

// keep forgets every session that is not working in this list. The map holds
// at most one entry per row of the last list, and a session that stopped and
// started again begins a new turn.
func (c *workingClock) keep(rows []sessionRowWire) {
	c.mu.Lock()
	defer c.mu.Unlock()
	working := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.WorkingSince != 0 {
			working[row.ID] = true
		}
	}
	for id := range c.starts {
		if !working[id] {
			delete(c.starts, id)
		}
	}
}

// sourceComplete is whether this reading answers for one row's own source.
//
// A backend no source here speaks for falls back to the whole reading, which
// is what this said for every row before it could tell them apart.
func sourceComplete(inv session.Inventory, backend session.Backend) bool {
	proves, _ := inv.ProvesAbsence(session.SourceFor(backend))
	return proves
}

// A tty-named assistant came only from ps, even if its provisional backend
// is iTerm2. Its evidence must follow ps; an unrelated Apple Event failure
// cannot make the process reading stale (session.OwningSource).
func rowSource(item session.Session) string { return session.OwningSource(item) }

func sourceCompleteRow(inv session.Inventory, item session.Session) bool {
	proves, _ := inv.ProvesAbsence(rowSource(item))
	return proves
}

// scanSources orders the per-source answers by name, because this snapshot is
// compared byte for byte against the last one published (the Cloud publisher
// skips a row nobody would read differently) and a map's order is not stable.
//
// A source turned off (disabled) travels as one more entry, incomplete and
// saying why, so a client can say "iTerm2 scanning is off" instead of drawing
// those tabs as nothing.
func scanSources(sources map[string]bool, gaps []session.Gap, disabled map[string]string) []contract.ScanSource {
	if len(sources) == 0 && len(disabled) == 0 {
		return nil
	}
	names := make([]string, 0, len(sources)+len(disabled))
	for name := range sources {
		names = append(names, name)
	}
	for name := range disabled {
		if _, asked := sources[name]; !asked {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	out := make([]contract.ScanSource, 0, len(names))
	for _, name := range names {
		row := contract.ScanSource{Source: name, Complete: sources[name], Gaps: scanGaps(name, gaps)}
		if why, off := disabled[name]; off {
			row.Complete = false
			row.Disabled = contract.ScanSourceDisabled(why)
		}
		out = append(out, row)
	}
	return out
}

// scanGaps is what one source could not read, in the order the source met it.
//
// It travels even when the source is complete again, because a sealed gap is
// still a window on this person's screen that will not open — and until this
// was on the wire the only way to find out was to ask iTerm2 by hand, which is
// how the window that made every row on this machine read `unknown` was found
// in the first place.
func scanGaps(source string, gaps []session.Gap) []contract.ScanGap {
	out := []contract.ScanGap{}
	for _, g := range gaps {
		if g.Source != source {
			continue
		}
		out = append(out, contract.ScanGap{
			Scope:    g.Scope,
			ID:       g.ID,
			Detail:   g.Detail,
			Sealed:   g.Sealed,
			SealedBy: g.SealedBy,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// recordsBudget is what this daemon's own records are given to answer in.
//
// Two seconds, and they are SQLite reads of files on this machine: the whole
// overlay measured in single-digit milliseconds, so this is a bound on a
// pathology, not a schedule.
const recordsBudget = 2 * time.Second

// recordsContext is that budget, and it deliberately does not inherit the
// request's deadline.
//
// **A read that was never given any time did not find nothing; it was not
// asked.** The session list's request carried one eight-second deadline for
// everything, the terminal scan spent all of it, and the record reads that
// follow then ran on a context that was already dead. Every one of them failed
// at once, the response carried `obligation_list_unreadable` and
// `own_records_unreadable`, and the rows lost the broker's task titles and
// fell back to `Clawdline task <id>` — while the titles sat in the database,
// readable, a millisecond away.
//
// So the records are taken off that clock. The cancellation is dropped with
// it, which is the right trade for reads of this daemon's own store: they are
// local, they are bounded here, and a client that hung up costs at most this
// long. What it buys is that "unreadable" in the answer means the store could
// not be read — never that the scan in front of it was slow.
func recordsContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), recordsBudget)
}

// liveOf is a session reduced to the identity the Swift store's records are
// compared with. The start time is asked of the kernel here, once per row per
// reading: it is one sysctl, and a cached one would be the one fact that could
// be wrong about a process that replaced another in the same second.
func liveOf(item session.Session) ownrecords.Live {
	return ownrecords.Live{
		TerminalID:     item.ID,
		TTY:            item.TTY,
		Assistant:      string(item.Assistant),
		PID:            int64(item.PID),
		ProcessStart:   ownrecords.ProcessStart(item.PID),
		ConversationID: item.ConversationID,
	}
}

// identityMatchCounts is SessionClosePolicy.identityMatchCounts: how many rows
// share each row's exact assistant/conversation identity. An unbound peer does
// not weaken a row whose process supplied one exact conversation: open-file,
// command-line and registry bindings are direct evidence about that process,
// not guesses among every anonymous process of the same assistant.
func identityMatchCounts(items []session.Session) map[string]int {
	byConversation := map[string]int{}
	for _, item := range items {
		if item.ConversationID == "" {
			continue
		}
		byConversation[string(item.Assistant)+"\x01"+item.ConversationID]++
	}
	out := make(map[string]int, len(items))
	for _, item := range items {
		switch {
		case item.ConversationID == "":
			out[item.ID] = 1
		default:
			out[item.ID] = byConversation[string(item.Assistant)+"\x01"+item.ConversationID]
		}
	}
	return out
}

// rowLabel chooses a name by the Swift app's rungs, with the two it keeps in
// its store filled from there. A Claude conversation's automatic name is its
// thread rung. A project is the last human name before the terminal
// coordinate: `Codex · project` distinguishes two conversations, while
// `Codex · ttys020` distinguishes only two terminal devices.
func rowLabel(item session.Session, titles ownrecords.Titles, project string) string {
	rungs := item.Rungs
	rungs.Manual = titles.Manual
	rungs.Orchestrator = titles.Orchestrator
	if titles.AgentNamedBoard && item.CustomTitle != "" && rungs.Manual == "" {
		rungs.Orchestrator = ""
		rungs.Conversation = item.CustomTitle
	}
	if rungs.Thread == "" {
		rungs.Thread = titles.Automatic
	}
	if rungs.Manual == "" && rungs.Orchestrator == "" && rungs.Conversation == "" &&
		rungs.Thread == "" && rungs.Handle == "" && project != "" {
		assistant := session.Coordinate(session.Session{Assistant: item.Assistant})
		if assistant != "" {
			return assistant + " · " + project
		}
		return project
	}
	if label := session.PreferredLabel(rungs); label != "" {
		return label
	}
	return item.Label
}

type rowInput struct {
	item                session.Session
	live                ownrecords.Live
	label               string
	labelOf             func(string) string
	matches             int
	swift               ownrecords.Snapshot
	owed                []task.Obligation
	owedErr             error
	ownErr              error // this daemon's own records not all read (ownrecords.go)
	closure             []store.SessionResponsibility
	closureErr          error
	inv                 session.Inventory
	now                 time.Time
	generation          int64
	executionGeneration string
	// role is this daemon's own machine role, and ownRole whether it holds
	// one; see sessionsPayload.
	role       *coordinator.Record
	ownRole    bool
	epicParent store.EpicRootParent
	// acceptance is shown only beside a Session's own delivery receipt.
	acceptance        *contract.SessionAcceptance
	latestRun         store.Event
	latestRunErr      error
	latestDelivery    store.Event
	latestDeliveryErr error
}

// receiptSuperseded compares durable events, not the terminal's current
// activity. A run belongs to this conversation and assistant; a resumed
// conversation may have used another terminal before its current receipt.
func receiptSuperseded(in rowInput, receipt *contract.WorkDisposition) (bool, bool) {
	if in.latestRunErr != nil || in.latestDeliveryErr != nil {
		return false, false
	}
	if in.latestRun.Seq == 0 || receipt == nil || receipt.Scope != "session" {
		return false, true
	}
	var run struct {
		Session   string `json:"session_id"`
		Assistant string `json:"assistant"`
		At        int64  `json:"at"`
	}
	if json.Unmarshal(in.latestRun.Payload, &run) != nil || run.Session != in.live.ConversationID ||
		run.Assistant != in.live.Assistant {
		return false, false
	}
	// The current daemon writes both facts into one event ledger. Its sequence
	// distinguishes the order even when both timestamps share a second. A
	// retired Swift receipt has no sequence; only a strictly later time wins.
	if in.latestDelivery.Seq != 0 {
		var delivered struct {
			Conversation string `json:"conversation"`
			Assistant    string `json:"assistant"`
			At           int64  `json:"at"`
		}
		if json.Unmarshal(in.latestDelivery.Payload, &delivered) != nil {
			return false, false
		}
		if delivered.Conversation == in.live.ConversationID && delivered.Assistant == in.live.Assistant &&
			delivered.At == receipt.ReceiptAt {
			return in.latestRun.Seq > in.latestDelivery.Seq, true
		}
	}
	return run.At > receipt.ReceiptAt, true
}

var errBoardUnreadable = errors.New("board store is not open")

// rowAcceptance is one conversation's answer from the fleet's Board reading.
// A row with no conversation id cannot own an item, which is unknown rather
// than none: its delivery may well have one the reading could not name.
func rowAcceptance(conversation string, pending map[string]store.PendingAcceptance, err error) *contract.SessionAcceptance {
	if err != nil || conversation == "" {
		return &contract.SessionAcceptance{State: "unknown"}
	}
	p, ok := pending[conversation]
	if !ok {
		return &contract.SessionAcceptance{State: "none"}
	}
	return &contract.SessionAcceptance{State: "pending", WorkID: p.WorkID, Title: p.Title,
		Phase: string(p.Phase), Since: p.EnteredAt.Unix(), Count: p.Count}
}

// sessionRow renders one session in the shape the console reads.
//
// Fields that cannot be supported are left at their zero value and the
// contract marks them optional, so they are absent rather than invented.
func (s *Server) sessionRow(in rowInput) sessionRowWire {
	item := in.item
	state := string(item.State)
	var glyph *contract.Icon
	if s.icons != nil {
		glyph = wireIcon(s.icons.For(item.CWD))
	}
	row := contract.SessionRow{
		Icon:     glyph,
		ID:       item.ID,
		Backend:  contract.Backend(item.Backend),
		State:    contract.SessionState(item.State),
		IsClaude: item.Assistant == session.AssistantClaude,
		// Not in the Swift contract: how this row's state was learned. A
		// registry reading and a screen guess are different kinds of fact.
		Evidence:            contract.Evidence(item.Evidence),
		ExecutionGeneration: in.executionGeneration,
		TTY:                 item.TTY,
		Assistant:           contract.Assistant(item.Assistant),
		Label:               in.label,
		Line:                item.Line,
		Source:              wireSessionObservation(item, in.inv),
		CWD:                 item.CWD,
		SessionID:           item.ConversationID,
		// How that id was obtained, or which kind of nothing took its place.
		// A row with no `sessionId` is three different situations, and only
		// one of them — a session that has not written anything yet — is
		// worth waiting out rather than acting on.
		Identity:      contract.IdentityBinding(item.Binding),
		Shells:        wireShells(item.Shells),
		Agents:        wireAgents(item.Agents),
		AgentsReading: wireAgentReading(item.AgentReading),
		// When this row last moved, from this daemon: the list's order no
		// longer depends on what any one browser happened to have watched.
		Activity: wireActivity(item.Activity),
		// Read back from the process's command line; absent for none.
		Persona:      item.Persona,
		MachineScope: projects.IsMachineWorkspace(s.cfg.Dir, item.CWD),
	}
	// The Project this row would be accepted into, by the claim's own rule:
	// a subdirectory or a linked worktree of a repository is that repository.
	if key, ok := s.projectKeys.key(item.CWD); ok {
		row.ProjectKey = key
	}
	out := sessionRowWire{Menu: wireMenu(item), processPID: item.PID, processStart: in.live.ProcessStart}
	// Only a waiting row says whether its screen was read: it is what tells
	// the card a terminal that is not answering from a dialog nothing
	// recognised. Elsewhere it would flicker with the terminal's health and
	// republish every row for nothing a reader looks at.
	if item.State == session.StateWaiting {
		row.ScreenReading = contract.SessionScreenReading(item.Screen)
	}

	// When this daemon's own records were read, the Swift app's rules run
	// over them. Otherwise the daemon's own projection answers with the
	// evidence marked missing.
	if in.ownErr == nil {
		work := in.swift.Work(in.live, state)
		// This daemon's own peer waits are an input the Swift store does not
		// hold; they rank where the Swift app's coordination waits do.
		if work.State != "waiting_you" && ownWaitingOnPeer(item, in.owed) {
			work = ownrecords.Work{State: "waiting_session", Provenance: "broker", Owed: work.Owed}
		}
		row.WorkState = contract.WorkState(work.State)
		row.WorkProvenance = contract.WorkProvenance(work.Provenance)
		row.WorkNote = work.Note
		row.WorkSince = work.Since
		row.WorkMovedBy = work.MovedBy
		out.WorkPersonNeeded = work.PersonNeeded
		out.waiting = work.Waiting
		row.Owed = work.Owed
		row.Disposition = work.Disposition
		if work.Disposition != nil && work.Disposition.Scope == "session" {
			superseded, known := receiptSuperseded(in, work.Disposition)
			if !known {
				row.WorkState = contract.WorkStateUnknown
				row.Disposition = nil
			} else if superseded {
				row.WorkState = contract.WorkStateReady
				row.Disposition = nil
			}
		}
	} else {
		// Without its own records there is no reading of the task, the
		// delivery or the declaration behind a state, and the honest
		// projection is the daemon's own with that evidence marked missing.
		row.WorkState = contract.WorkState(workState(item, in.owed, in.ownErr))
		row.WorkProvenance = contract.WorkProvenanceBroker
	}
	// A Feature Root or a wait that was read is a fact whatever else could
	// not be: they are drawn from the records there are, the daemon's own
	// among them.
	// Board items waiting for the person ride on a Session's own report: a
	// task's delivery is accepted on its task, and a row with no report has
	// nothing it is saying is ready.
	if row.Disposition != nil && row.Disposition.Scope == "session" {
		row.Acceptance = in.acceptance
	}
	row.RootAssignment = in.swift.RootAssignment(in.live)
	if in.epicParent.OwnerSession != "" && in.epicParent.EpicID != "" {
		row.EpicParent = &contract.EpicSessionParent{
			OwnerSessionID: in.epicParent.OwnerSession,
			EpicID:         in.epicParent.EpicID,
		}
	}
	row.Coordination = in.swift.Coordination(item.ID, in.labelOf)

	// One role, one source: this daemon's own. Without one, nobody wears the
	// crown; the retired Swift app's coordinator record is no longer read.
	if in.ownRole {
		row.Coordinator = ownCrown(in.role, in.live)
	}

	extra := ownCloseReasons(item, in.owed, in.owedErr)
	for _, open := range in.closure {
		extra = append(extra, contract.CloseReason{
			Code: open.Code, Kind: "obligation", SubjectKind: "work", SubjectID: open.ID,
			Mover: contract.CloseMover{Kind: "session", Self: true},
		})
	}
	if in.closureErr != nil {
		extra = append(extra, contract.CloseReason{
			Code: "session_work_unreadable", Kind: "evidence",
			Mover: contract.CloseMover{Kind: "broker"},
		})
	}
	if in.ownErr != nil {
		extra = append(extra, contract.CloseReason{
			Code: "own_records_unreadable", Kind: "evidence",
			Mover: contract.CloseMover{Kind: "broker"},
		})
	}

	row.Closeability = in.swift.Closeability(ownrecords.CloseInput{
		Live:          in.live,
		TerminalState: state,
		Bound: in.live.Assistant != "" && in.live.PID != 0 &&
			!in.live.ProcessStart.IsZero() && (in.live.ConversationID != "" ||
			item.Assistant == session.AssistantCodex && item.Binding == session.BindingNoRecord),
		Matches: in.matches,
		// The completeness that decides whether this row's reading is stale is
		// the completeness of the source that lists this row, not the AND over
		// every source (D05 ③). With the AND here, one iTerm2 window that will
		// not describe itself made every row on this machine read `unknown`
		// with `session_inventory_stale` — including ten tmux panes iTerm2 has
		// never listed and could not be hiding — and nothing on this Mac could
		// be closed for as long as that window was open.
		InventoryComplete:   sourceCompleteRow(in.inv, item),
		InventoryObservedAt: in.inv.ObservedAt,
		Now:                 in.now,
		Generation:          in.generation,
		Extra:               extra,
	})
	out.SessionRow = row
	return out
}

// wireInventoryObservation and wireSessionObservation put the cache's answer
// on the wire in the same four-word vocabulary the rest of the daemon uses.
// Hand-built inventories in older tests carry no Observation; their existing
// complete flag remains the fallback rather than turning every fixture into a
// missing reading.
func wireInventoryObservation(inv session.Inventory) *contract.BearingsSource {
	obs := inv.Observation
	if obs.Freshness == "" {
		obs = session.Observation{ObservedAt: inv.ObservedAt, Provenance: inv.Provenance}
		if inv.Complete {
			obs.Freshness = session.FreshnessCurrent
		} else {
			obs.Freshness = session.FreshnessMissing
		}
	}
	return wireObservation(obs)
}

func wireSessionObservation(item session.Session, inv session.Inventory) *contract.BearingsSource {
	obs := item.Observation
	if obs.Freshness == "" {
		obs = session.Observation{
			ObservedAt: inv.ObservedAt,
			Provenance: rowSource(item),
		}
		if sourceCompleteRow(inv, item) {
			obs.Freshness = session.FreshnessCurrent
		} else {
			obs.Freshness = session.FreshnessMissing
		}
	}
	return wireObservation(obs)
}

func wireObservation(obs session.Observation) *contract.BearingsSource {
	at := int64(0)
	if !obs.ObservedAt.IsZero() {
		at = obs.ObservedAt.Unix()
	}
	return &contract.BearingsSource{
		ObservedAt: at,
		Provenance: obs.Provenance,
		Freshness:  contract.SourceFreshness(obs.Freshness),
	}
}

// ownWaitingOnPeer is this daemon's own record of a session parked on another.
func ownWaitingOnPeer(item session.Session, owed []task.Obligation) bool {
	for _, o := range owed {
		if o.Mover.Kind == task.MoverOtherSession && o.Subject == item.ID {
			return true
		}
	}
	return false
}

// ownCloseReasons carries this daemon's own obligations — the broker's pending
// landings (D01) — into the closeability projection beside the Swift store's,
// as Swift's `additionalObligations`. An unreadable list is evidence, so the
// row reads unknown rather than short.
func ownCloseReasons(item session.Session, owed []task.Obligation, err error) []contract.CloseReason {
	if err != nil {
		return []contract.CloseReason{{
			Code: "obligation_list_unreadable", Kind: "evidence",
			Mover: contract.CloseMover{Kind: "broker"},
		}}
	}
	c := task.Closeability(item.ID, owed, nil)
	out := make([]contract.CloseReason, 0, len(c.Reasons))
	for _, r := range c.Reasons {
		out = append(out, contract.CloseReason{
			Kind:        "obligation",
			Code:        string(r.Kind),
			SubjectID:   r.Mover.ID,
			SubjectKind: string(r.Mover.Kind),
			Mover:       wireCloseMover(r.Mover, item.ID),
		})
	}
	return out
}

// wireActivity carries when a row last moved across to the wire.
//
// An unreadable time is sent as `known: false` with its reason, never as a
// zero or an old one: `at` is absent unless there is an answer, so a client
// cannot accidentally read "nobody could tell" as "quiet since 1970". The
// order it is owed is written into the contract beside it.
func wireActivity(a session.Activity) *contract.SessionActivity {
	out := &contract.SessionActivity{
		Evidence: contract.Evidence(a.Evidence),
		Detail:   a.Detail,
	}
	if a.Known() {
		out.Known = true
		out.At = a.At.Unix()
		return out
	}
	out.UnknownReason = contract.ActivityUnknownReason(a.Unknown())
	return out
}

// wireShells carries a session's background commands across. None is nil, so
// the key is absent as the Swift app leaves it.
func wireShells(shells []session.Shell) []contract.SessionShell {
	if len(shells) == 0 {
		return nil
	}
	out := make([]contract.SessionShell, 0, len(shells))
	for _, sh := range shells {
		out = append(out, contract.SessionShell{
			ID:      sh.ID,
			At:      sh.At.Unix(),
			Command: sh.Command,
			What:    sh.What,
			Doing:   sh.Doing,
		})
	}
	return out
}

func wireAgents(agents []session.Agent) []contract.SessionAgent {
	if len(agents) == 0 {
		return nil
	}
	out := make([]contract.SessionAgent, 0, len(agents))
	for _, agent := range agents {
		var at int64
		if !agent.At.IsZero() {
			at = agent.At.Unix()
		}
		out = append(out, contract.SessionAgent{
			ID: agent.ID, What: agent.What, Type: agent.Type, Model: agent.Model,
			Depth: int64(agent.Depth), State: contract.SessionAgentState(agent.State), At: at,
			Doing: agent.Doing, Result: agent.Result, Tokens: agent.Tokens,
			Tools: agent.Tools, Seconds: agent.Seconds,
		})
	}
	return out
}

func wireAgentReading(reading session.AgentReading) *contract.SessionAgentReading {
	if reading.State == "" {
		return nil
	}
	return &contract.SessionAgentReading{
		State:     contract.SessionAgentReadState(reading.State),
		Reason:    contract.SessionAgentUnknownReason(reading.Reason),
		Truncated: int64(reading.Truncated),
	}
}

// wireIcon carries a mark across to the wire in the shape the console draws.
func wireIcon(g icon.Grid) *contract.Icon {
	if len(g.Cells) == 0 && g.SVG == "" {
		return nil
	}
	cells := g.Cells
	if cells == nil {
		cells = [][]*string{}
	}
	return &contract.Icon{Accent: g.Accent, Cells: cells, Svg: g.SVG}
}

// wireMover is the one place a mover crosses from the domain to the wire. It is
// a function rather than a literal in three handlers because a mover that means
// something different in the obligation list than in a close reason is exactly
// the drift this contract exists to prevent.
func wireMover(m task.Mover) contract.Mover {
	return contract.Mover{Kind: contract.MoverKind(m.Kind), ID: m.ID}
}

// workState is this daemon's own projection, used only when its own records
// cannot be read: it gathers this session's axes and hands them to the
// projection.
func workState(item session.Session, owed []task.Obligation, owedErr error) task.WorkState {
	in := task.WorkInputs{
		AskedOnScreen:          item.State == session.StateWaiting,
		EvidenceMissing:        owedErr != nil || item.State == session.StateUnknown,
		Active:                 item.State == session.StateWorking,
		PromptWithoutAssistant: !item.IsAssistant() && item.State == session.StateIdle,
	}
	for _, o := range owed {
		switch {
		case o.Mover.Kind == task.MoverOtherSession && o.Subject == item.ID:
			in.WaitingOnPeer = true
		case o.Mover.Kind == task.MoverTask && o.Subject != "":
			// A task obligation belongs to whoever dispatched it; without that
			// link recorded yet this cannot claim the session, so it does not.
		}
	}
	return task.ProjectWorkState(in)
}

// wireCloseMover says who clears one reason, as the Swift app's
// CloseabilityMover.wire spells it.
//
// `self` is the distinction that matters to a reader: a thing this session has
// to do is a thing they can do now, and a thing another session has to do is
// only something to wait for — so the other session is named.
func wireCloseMover(m task.Mover, subject string) contract.CloseMover {
	switch m.Kind {
	case task.MoverPerson:
		return contract.CloseMover{Kind: "person", PersonNeeded: true}
	case task.MoverBroker:
		return contract.CloseMover{Kind: "broker"}
	case task.MoverTask:
		return contract.CloseMover{Kind: "task", TaskID: m.ID}
	}
	if m.ID == subject {
		return contract.CloseMover{Kind: "session", Self: true}
	}
	return contract.CloseMover{Kind: "session", SessionID: m.ID}
}

// The plan-window reader lives beside the Server rather than in it so this
// process-wide, short-lived cache adds no field to server.go.
var (
	limitsOnce   sync.Once
	limitsReader *limits.Reader
)

// quotaReader is the one plan-window reader: a session's `/info` and the
// assistants route read the same five-second reading of each account.
func quotaReader() *limits.Reader {
	limitsOnce.Do(func() {
		home, _ := os.UserHomeDir()
		limitsReader = limits.NewReader(home, nil)
	})
	return limitsReader
}

// wireWindows is a reading's windows as the wire spells them, in both places
// a window is sent.
func wireWindows(windows []limits.Window) []contract.SessionLimitWindow {
	out := make([]contract.SessionLimitWindow, 0, len(windows))
	for _, w := range windows {
		row := contract.SessionLimitWindow{Name: w.Name, Hit: w.Hit}
		if w.UsedPercent != nil {
			row.UsedPercent = *w.UsedPercent
		}
		if w.ResetsAt != nil {
			row.ResetsAt = *w.ResetsAt
		}
		out = append(out, row)
	}
	return out
}

// sessionLimits is what the Swift app's `/info` puts under `limits`: the
// account-level plan windows of the session's assistant
// (`AssistantQuota.machineLimits`), stamped with the moment they were read. A
// session with no assistant has none and no stamp.
//
// The windows come with the age of the record they were read from, the line
// that age is judged against, and — when there are none — which of the four
// kinds of nothing that was. A status line that draws a percentage without
// them draws a number from three hours ago as a number from now.
func (s *Server) sessionLimits(assistant session.Assistant, now time.Time) *contract.SessionLimits {
	out := &contract.SessionLimits{Windows: []contract.SessionLimitWindow{}}
	if assistant != session.AssistantClaude && assistant != session.AssistantCodex {
		return out
	}
	read := quotaReader().Machine(string(assistant), now)
	out.Windows = append(out.Windows, wireWindows(read.Windows)...)
	if read.At != nil {
		out.At = *read.At
		out.AgeSeconds = ageSeconds(*read.At, now)
	}
	if read.FreshFor > 0 {
		fresh := read.FreshFor
		out.FreshForSeconds = &fresh
	}
	out.Stale = read.Stale
	out.Detail = read.Detail
	out.UnknownReason = contract.AssistantUnknownReason(read.Reason)
	out.ReadAtMs = now.UnixMilli()
	return out
}

// ageSeconds is how old a provider record is now, never negative: a record
// stamped in the future is nought seconds old, not a negative age.
func ageSeconds(observedAt int64, now time.Time) *int64 {
	age := now.Unix() - observedAt
	if age < 0 {
		age = 0
	}
	return &age
}
