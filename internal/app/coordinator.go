package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/coordinator"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Coordinator is the machine role on this daemon (Clawdfather): registering
// it, moving it after its session is gone, reading where the machine's work
// stands, and saying which live row wears the crown.
//
// It lives in the broker's store (broker-design #7): the role is one row whose
// every write is a compare-and-set on its generation, in the same file as the
// tasks it coordinates, so that nothing written about the role and nothing
// written about the work can disagree about which happened first. The Swift
// app kept it in coordinator.json beside its own lock; this daemon only ever
// read that file, and the crown it drew from it was somebody else's record
// (cutover A7 needs the crown drawn with that reader switched off).
type Coordinator struct {
	Store *store.Store
	// Read is a current reading of the machine's sessions.
	Read func(ctx context.Context) session.Inventory
	// ProcessStart is when a process started, from the kernel.
	ProcessStart func(pid int) time.Time
	// Broker answers the bearings' task, landing, wait and lease counts.
	Broker *orchestrator.Broker
	Clock  func() time.Time
	NewID  func() string
}

func (c *Coordinator) now() time.Time {
	if c.Clock != nil {
		return c.Clock()
	}
	return time.Now()
}

// RoleRefusal is a typed no from the coordinator routes. Extra is placed
// inside the error envelope, as every broker refusal's is.
type RoleRefusal struct {
	Status  int
	Code    string
	Message string
	Extra   map[string]any
}

func (r RoleRefusal) Error() string { return r.Code + ": " + r.Message }

// Seen is one reading of the machine reduced to what the role is judged by.
type Seen struct {
	Inventory session.Inventory
	// Sessions are the live assistant sessions with their exact tuples.
	Sessions map[string]session.Session
	tuples   []coordinator.Identity
}

// seeing takes one reading.
func (c *Coordinator) seeing(ctx context.Context) Seen {
	inv := c.Read(ctx)
	out := Seen{Inventory: inv, Sessions: map[string]session.Session{}}
	for _, s := range inv.Sessions {
		if !s.IsAssistant() {
			continue
		}
		out.Sessions[s.ID] = s
		out.tuples = append(out.tuples, c.identity(s))
	}
	return out
}

// identity is a live session as the exact tuple the role binds.
func (c *Coordinator) identity(s session.Session) coordinator.Identity {
	id := coordinator.Identity{
		ConversationID: s.ConversationID,
		TerminalID:     s.ID,
		Assistant:      string(s.Assistant),
		TTY:            s.TTY,
		PID:            s.PID,
	}
	if c.ProcessStart != nil {
		id.ProcessStart = c.ProcessStart(s.PID)
	}
	return id
}

// reading is the machine reading as the role's rules take it. Complete is the
// completeness of the source that owns the terminal in question, not the AND
// over every source: on a Mac whose iTerm2 cannot be asked that AND is never
// true, and a role bound to a tmux pane would then never be provably offline
// (design-decisions D05 ③).
func (s Seen) reading(backend session.Backend) coordinator.Reading {
	complete := s.Inventory.Complete
	if said, ok := s.Inventory.Sources[string(backend)]; ok && backend != "" {
		complete = said
	}
	return coordinator.Reading{Sessions: s.tuples, Complete: complete, ObservedAt: s.Inventory.ObservedAt}
}

// backendOf is the backend a terminal id belongs to, as the last reading that
// listed it said; empty when no reading here lists it.
func (s Seen) backendOf(terminal string) session.Backend {
	if row, ok := s.Sessions[terminal]; ok {
		return row.Backend
	}
	// A tmux pane id is `%<n>`; the Go inventory lists tmux panes under it.
	if len(terminal) > 1 && terminal[0] == '%' {
		return session.BackendTmux
	}
	return ""
}

// State is the role as one reading sees it.
type State struct {
	Status   store.CoordinatorStatus
	Record   *coordinator.Record
	Liveness coordinator.Liveness
	Seen     Seen
}

// Read answers the stored role and what the machine says about it now.
func (c *Coordinator) State(ctx context.Context) (State, error) {
	rec, status, err := c.Store.Coordinator(ctx)
	if err != nil {
		return State{}, coordinatorStoreRefusal(err)
	}
	seen := c.seeing(ctx)
	st := State{Status: status, Record: rec, Seen: seen, Liveness: coordinator.Unknown}
	if rec != nil {
		st.Liveness = coordinator.LivenessOf(*rec, seen.reading(seen.backendOf(rec.TerminalID)))
	}
	return st, nil
}

// resolve finds the one live session a conversation id names (#36).
func (c *Coordinator) resolve(seen Seen, conversation string) (session.Session, error) {
	if !lowercaseUUID(conversation) {
		return session.Session{}, RoleRefusal{Status: http.StatusConflict, Code: "session_id_is_terminal",
			Message: "A session is named by its conversation id (one lowercase UUID) everywhere on this daemon; " +
				"that value is not one. A terminal id is a place a session is, not the session. Resolve yours with " +
				"GET /v1/orchestrator/whoami?conversation_id=… and send the conversation id.",
			Extra: map[string]any{"value": conversation}}
	}
	var found []session.Session
	for _, s := range seen.Sessions {
		if s.ConversationID == conversation {
			found = append(found, s)
		}
	}
	switch len(found) {
	case 0:
		return session.Session{}, RoleRefusal{Status: http.StatusNotFound, Code: "session_not_found",
			Message: "No live assistant session has that conversation id."}
	case 1:
		return found[0], nil
	}
	return session.Session{}, RoleRefusal{Status: http.StatusConflict, Code: "session_ambiguous",
		Message: "More than one live session claims that conversation id; none was chosen."}
}

// Register binds the role to one live session, or recognises the same
// session registering again (created false). It is never a takeover.
func (c *Coordinator) Register(ctx context.Context, conversation string) (State, bool, error) {
	st, err := c.State(ctx)
	if err != nil {
		return State{}, false, err
	}
	if st.Status == store.CoordinatorCorrupt || st.Status == store.CoordinatorUnsupported {
		return st, false, RoleRefusal{Status: http.StatusConflict, Code: "coordinator_store_invalid",
			Message: "The stored role is not a record this daemon can vouch for (" + string(st.Status) +
				"); it is left as it is for a person to look at, and nothing was registered."}
	}
	live, err := c.resolve(st.Seen, conversation)
	if err != nil && st.Record == nil {
		return st, false, err
	}
	var candidate coordinator.Identity
	if err == nil {
		candidate = c.identity(live)
	}
	id := ""
	if c.NewID != nil {
		id = c.NewID()
	} else {
		id = orchestrator.NewUUID()
	}
	next, created, err := coordinator.Register(st.Record, candidate, st.Seen.reading(live.Backend), id, c.now())
	if err != nil {
		return st, false, c.refusal(err, st)
	}
	if !created {
		return st, false, nil
	}
	next.SessionLabel, next.CWD = live.Label, live.CWD
	payload, _ := json.Marshal(map[string]any{"id": next.ID, "session": next.ConversationID, "terminal": next.TerminalID})
	if err := c.Store.CommitCoordinator(ctx, nil, next,
		[]store.Event{{Kind: "coordinator.registered", Subject: next.ID, Payload: payload}}); err != nil {
		if errors.Is(err, store.ErrCoordinatorExists) {
			return st, false, RoleRefusal{Status: http.StatusConflict, Code: "coordinator_exists",
				Message: "Another session registered the role while this one was being decided."}
		}
		return st, false, coordinatorStoreRefusal(err)
	}
	st.Record, st.Status, st.Liveness = &next, store.CoordinatorReady, coordinator.Online
	return st, true, nil
}

// Rebind moves the role to one exact live session after a reading proves the
// bound one offline. A compare-and-set on the role id and generation the
// caller read; `rebound` false is the same session asking twice.
func (c *Coordinator) Rebind(ctx context.Context, expectID string, expectGeneration int64, conversation string) (State, bool, error) {
	st, err := c.State(ctx)
	if err != nil {
		return State{}, false, err
	}
	if st.Status == store.CoordinatorCorrupt || st.Status == store.CoordinatorUnsupported {
		return st, false, RoleRefusal{Status: http.StatusConflict, Code: "coordinator_store_invalid",
			Message: "The stored role is not a record this daemon can vouch for; nothing was moved."}
	}
	live, err := c.resolve(st.Seen, conversation)
	if err != nil {
		return st, false, err
	}
	candidate := c.identity(live)
	var backend session.Backend
	if st.Record != nil {
		backend = st.Seen.backendOf(st.Record.TerminalID)
	}
	next, moved, err := coordinator.Rebind(st.Record, expectID, expectGeneration, candidate, st.Seen.reading(backend), c.now())
	if err != nil {
		return st, false, c.refusal(err, st)
	}
	if !moved {
		return st, false, nil
	}
	next.SessionLabel, next.CWD = live.Label, live.CWD
	payload, _ := json.Marshal(map[string]any{"id": next.ID, "generation": next.Generation,
		"from": st.Record.ConversationID, "to": next.ConversationID, "terminal": next.TerminalID})
	err = c.Store.CommitCoordinator(ctx, &store.CoordinatorExpect{ID: expectID, Generation: expectGeneration}, next,
		[]store.Event{{Kind: "coordinator.rebound", Subject: next.ID, Payload: payload}})
	if errors.Is(err, store.ErrConflict) {
		return st, false, RoleRefusal{Status: http.StatusConflict, Code: "coordinator_generation_mismatch",
			Message: "The role moved while this rebind was being decided; read it again."}
	}
	if err != nil {
		return st, false, coordinatorStoreRefusal(err)
	}
	st.Record, st.Liveness = &next, coordinator.Online
	return st, true, nil
}

// refusal carries a domain refusal with the role as it is now, as the Swift
// app's mismatch, online and unknown refusals do.
func (c *Coordinator) refusal(err error, st State) error {
	var ref coordinator.Refusal
	if !errors.As(err, &ref) {
		return err
	}
	out := RoleRefusal{Status: ref.Status, Code: ref.Code, Message: ref.Detail}
	if st.Record != nil {
		out.Extra = map[string]any{"coordinator": map[string]any{
			"id": st.Record.ID, "generation": st.Record.Generation, "status": string(st.Liveness),
			"session_id": st.Record.ConversationID, "terminal_id": st.Record.TerminalID,
		}}
	}
	return out
}

func coordinatorStoreRefusal(err error) error {
	if errors.Is(err, store.ErrBusy) {
		return RoleRefusal{Status: http.StatusServiceUnavailable, Code: "orchestrator_store_busy",
			Message: "Another writer held the store longer than this one waits; nothing was changed. Retry.",
			Extra:   map[string]any{"retry_after": 1}}
	}
	return RoleRefusal{Status: http.StatusInternalServerError, Code: "coordinator_store_failed",
		Message: "The role could not be read or written; nothing was changed.", Extra: map[string]any{"cause": err.Error()}}
}

// Crowned reports whether a live row is the bound session: the exact tuple,
// as Coordinator.sessionProjection asks. The crown carries no freshness — the
// row is there, so the process is.
func (c *Coordinator) Crowned(rec *coordinator.Record, s session.Session) bool {
	if rec == nil || !s.IsAssistant() {
		return false
	}
	return rec.Identity.Matches(c.identity(s))
}

// Bearings is where the machine's work stands, as the role reads it.
type Bearings struct {
	ObservedAt      time.Time
	Lifecycle       string
	ActiveTasks     int
	PendingLandings []BearingsLanding
	OpenWaits       int
	DeadLetters     int
	HeldLeases      int
	Unknown         []string
	SessionsFresh   string
	TasksFresh      string
	// LandingsFresh is the landing ledger's own word, which is not the task
	// records' word however often the two are read in the same pass. The
	// records read perfectly and still leave "is this work on its target"
	// unanswered whenever nothing on the record says what its target is, and
	// while this field was a copy of TasksFresh the ledger answered `current`
	// about exactly that — a reading that happened, standing in for a fact
	// nobody had checked.
	LandingsFresh string
	WaitsFresh    string
	// UnverifiedLandings is how many pending landings are owed and cannot be
	// checked against a target. It is the evidence behind an `unverified`
	// LandingsFresh, and never a reason to draw the pending count as fewer.
	UnverifiedLandings int
}

// BearingsLanding is one pending landing.
type BearingsLanding struct {
	Task, Title, Obligation string
	Age                     time.Duration
}

// Bearings reads them. A source that could not be read is named in Unknown
// and counted as nothing only there: "I could not read the waits" is never
// "there are no waits".
func (c *Coordinator) Bearings(ctx context.Context, st State) Bearings {
	now := c.now()
	out := Bearings{ObservedAt: now, Lifecycle: Lifecycle(st), SessionsFresh: "current",
		TasksFresh: "current", LandingsFresh: "current", WaitsFresh: "current"}
	if !st.Seen.Inventory.Complete {
		out.SessionsFresh = "stale"
		out.Unknown = append(out.Unknown, "sessions")
	}
	if c.Broker == nil {
		out.TasksFresh, out.LandingsFresh, out.WaitsFresh = "missing", "missing", "missing"
		out.Unknown = append(out.Unknown, "tasks", "landings", "waits", "leases")
		return out
	}
	records, _, err := c.Broker.Records(ctx)
	if err != nil {
		out.TasksFresh, out.LandingsFresh = "missing", "missing"
		out.Unknown = append(out.Unknown, "tasks", "landings")
	} else {
		for _, r := range records {
			if !r.State.Terminal() {
				out.ActiveTasks++
			}
			if r.Landing != nil && r.Landing.State == orchestrator.LandingPending {
				out.PendingLandings = append(out.PendingLandings, BearingsLanding{
					Task: r.ID, Title: r.Title, Obligation: string(c.Broker.Obligation(r)),
					Age: now.Sub(r.FinishedAt),
				})
				if orchestrator.Unverifiable(r.Landing) {
					out.UnverifiedLandings++
				}
			}
			if r.Notice != nil && r.Notice.State == orchestrator.NoticeDeadLetter {
				out.DeadLetters++
			}
		}
	}
	if waits, err := c.Broker.Waits(ctx); err != nil {
		out.WaitsFresh = "missing"
		out.Unknown = append(out.Unknown, "waits")
	} else {
		out.OpenWaits = len(waits)
	}
	if out.LandingsFresh == "current" && out.UnverifiedLandings > 0 {
		out.LandingsFresh = "unverified"
	}
	if leases, err := c.Broker.Leases(ctx); err != nil {
		out.Unknown = append(out.Unknown, "leases")
	} else {
		for _, l := range leases {
			if l.Holder != nil {
				out.HeldLeases++
			}
		}
	}
	return out
}

// Lifecycle is coordinatorMetadata's second word: standby while the bound
// session is online, offline, unknown, or unregistered.
func Lifecycle(st State) string {
	if st.Record == nil {
		return "unregistered"
	}
	switch st.Liveness {
	case coordinator.Online:
		return "standby"
	case coordinator.Offline:
		return "offline"
	}
	return "unknown"
}

func lowercaseUUID(v string) bool {
	if len(v) != 36 {
		return false
	}
	for i, r := range v {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
				return false
			}
		}
	}
	return true
}
