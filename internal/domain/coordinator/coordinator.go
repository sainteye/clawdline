// Package coordinator is the machine-wide role: one explicitly registered
// session that explains other people's receipts and authors none of them.
//
// The rules are the Swift app's (Coordinator.swift), with one change the
// design asked for (docs/broker-design.md #36, O2): **a session has one name.**
// The Swift routes took `session_id` meaning a terminal id, while every broker
// route took `root.session_id` meaning a conversation id — two fields with the
// same name and opposite values, which is how a landing slot came to be named
// by one and looked up by the other and never delivered once. Here a session
// is its conversation id everywhere; the terminal it happens to be in is a
// location (`TerminalID`), recorded beside it and never called a session.
package coordinator

import (
	"fmt"
	"time"
)

// Scope and Label are the Swift app's constants: there is one role per
// machine, and it is called Clawdfather.
const (
	Scope = "machine"
	Label = "Clawdfather"
	// AliasLimit is how many earlier bindings a record keeps (Coordinator.swift
	// keeps the last 32). They are read only to route a completion notice for a
	// task an earlier binding dispatched.
	AliasLimit = 32
	// StartTolerance is SessionRegistry.startTolerance: two readings of one
	// process's start time may differ by this much and still be one process.
	StartTolerance = 5 * time.Second
)

// Identity is the exact tuple a binding names. A live session is the bound
// one only when every field matches; a reading that matches on some of them is
// a different process that happens to share a terminal or a conversation.
type Identity struct {
	// ConversationID is the session (#36).
	ConversationID string
	// TerminalID is where it is now: a pane or tab id, never a session id.
	TerminalID   string
	Assistant    string
	TTY          string
	PID          int
	ProcessStart time.Time
}

// Complete reports whether the tuple names one process exactly. A session
// whose conversation, process or start time could not be read cannot be
// bound: binding it would name a terminal, not a session.
func (i Identity) Complete() bool {
	return i.ConversationID != "" && i.TerminalID != "" && i.Assistant != "" &&
		i.PID > 0 && !i.ProcessStart.IsZero()
}

// Matches is Coordinator.swift `matches`: every field exact, except the start
// time, which two readings may place up to StartTolerance apart.
func (i Identity) Matches(o Identity) bool {
	if i.ConversationID != o.ConversationID || i.TerminalID != o.TerminalID ||
		i.Assistant != o.Assistant || trimDev(i.TTY) != trimDev(o.TTY) || i.PID != o.PID {
		return false
	}
	if i.ProcessStart.IsZero() || o.ProcessStart.IsZero() {
		return false
	}
	d := i.ProcessStart.Sub(o.ProcessStart)
	if d < 0 {
		d = -d
	}
	return d <= StartTolerance
}

// trimDev compares ttys the way both apps spell them: "/dev/ttys012" and
// "ttys012" are one tty.
func trimDev(t string) string {
	if len(t) > 5 && t[:5] == "/dev/" {
		return t[5:]
	}
	return t
}

// Alias is an earlier binding, kept for routing only.
type Alias struct {
	ConversationID string    `json:"conversation_id"`
	TerminalID     string    `json:"terminal_id"`
	Assistant      string    `json:"assistant"`
	BoundAt        time.Time `json:"bound_at"`
	UnboundAt      time.Time `json:"unbound_at"`
}

// Record is the one durable binding. It is a separate identity ledger rather
// than a flag on a session, because the role outlives any particular process
// and must never be inferred from a label or from whoever was busiest.
type Record struct {
	// ID is the role's own UUID. It survives every rebind.
	ID string
	Identity
	// SessionLabel and CWD are what the bound session was called and where
	// it worked when it was bound; the live row's own are preferred.
	SessionLabel string
	CWD          string
	RegisteredAt time.Time
	ReboundAt    time.Time
	// Generation increments on every rebind, so a stale caller holding an old
	// view cannot move a role that has already moved. It is the only value the
	// compare-and-set is over.
	Generation int64
	Aliases    []Alias
}

// Valid is Coordinator.swift `valid()`: a record whose tuple is incomplete,
// whose generation is below one, or that claims a rebind at generation one is
// not a record this daemon wrote, and is never overwritten on the way past.
func (r Record) Valid() bool {
	if r.ID == "" || !r.Identity.Complete() || r.Generation < 1 || r.RegisteredAt.IsZero() {
		return false
	}
	if r.Generation == 1 && !r.ReboundAt.IsZero() {
		return false
	}
	if r.Generation > 1 && r.ReboundAt.IsZero() {
		return false
	}
	return true
}

// Since is the moment the current binding began.
func (r Record) Since() time.Time {
	if !r.ReboundAt.IsZero() {
		return r.ReboundAt
	}
	return r.RegisteredAt
}

// Liveness is what a current inventory says about the bound process.
//
// `Unknown` is a first-class answer and is never treated as absence. "Not in
// this reading" is also true of a terminal that lost a permission for a moment,
// and converting that into proof of death is how a live coordinator gets
// replaced underneath itself.
type Liveness string

const (
	Online  Liveness = "online"
	Offline Liveness = "offline"
	Unknown Liveness = "unknown"
)

// Reading is the part of a machine reading the role is judged against.
type Reading struct {
	// Sessions are the live assistant sessions, each as an exact tuple.
	Sessions []Identity
	// Complete is whether the reading could see every session the bound one
	// could be — the source the bound terminal belongs to, not the machine
	// (docs/design-decisions.md D05 ③).
	Complete   bool
	ObservedAt time.Time
}

// LivenessOf is coordinatorMetadata's rule. Online is a live row with the
// exact tuple. Offline needs a complete reading taken after the binding began:
// a reading from before the rebind cannot say anything about the process the
// rebind named. Everything else is unknown.
func LivenessOf(r Record, rd Reading) Liveness {
	for _, s := range rd.Sessions {
		if s.Matches(r.Identity) {
			return Online
		}
	}
	if rd.Complete && !rd.ObservedAt.IsZero() && !rd.ObservedAt.Before(r.Since()) {
		return Offline
	}
	return Unknown
}

// Refusal is a typed no. Extra carries what a caller needs to try again (the
// record as it is now), inside the error envelope as the Swift app puts it.
type Refusal struct {
	Status int
	Code   string
	Detail string
}

func (r Refusal) Error() string { return r.Code + ": " + r.Detail }

func refuse(status int, code, detail string) Refusal { return Refusal{status, code, detail} }

// Register creates the binding, or recognises it.
//
// Registration is never a takeover. An existing record keeps its place even
// while liveness is degraded, because the alternative — letting a new claimant
// in whenever the old one cannot be seen — makes the role migrate on a bad
// reading rather than on a decision. `created` is false for the same session
// registering again.
func Register(existing *Record, candidate Identity, rd Reading, id string, now time.Time) (Record, bool, error) {
	if existing != nil {
		if existing.Identity.Matches(candidate) {
			return *existing, false, nil
		}
		return Record{}, false, refuse(409, "coordinator_exists",
			fmt.Sprintf("%s is already bound; registration is never a takeover — use rebind once the bound session is offline", Label))
	}
	if !rd.Complete || rd.ObservedAt.IsZero() || rd.ObservedAt.After(now.Add(time.Second)) {
		return Record{}, false, refuse(409, "coordinator_liveness_unknown",
			"The session reading is incomplete, so nobody can say which session this is; nothing was registered.")
	}
	if !candidate.Complete() {
		return Record{}, false, refuse(409, "session_unbound",
			"That session's process, start time or conversation could not be read, so it cannot be bound as one exact process.")
	}
	return Record{
		ID:           id,
		Identity:     candidate,
		RegisteredAt: now,
		Generation:   1,
	}, true, nil
}

// Rebind moves the role to one exact live process after a reading proves the
// old one offline.
//
// This is reconnect, not takeover: it is how a role survives a terminal
// restarting, and it refuses everything that is not that. The compare-and-swap
// is closed over the record's id and the generation the caller read, so a
// caller working from a stale view cannot move a role that has already moved.
func Rebind(existing *Record, expectID string, expectGeneration int64, candidate Identity, rd Reading, now time.Time) (Record, bool, error) {
	if existing == nil {
		return Record{}, false, refuse(409, "coordinator_not_configured", "There is no role to move; register it first.")
	}
	if existing.ID != expectID {
		return Record{}, false, refuse(409, "coordinator_identity_mismatch",
			"expected_coordinator_id is not the role this machine holds.")
	}
	if existing.Generation != expectGeneration {
		return Record{}, false, refuse(409, "coordinator_generation_mismatch",
			"The role moved since you read it; read it again.")
	}
	if existing.Identity.Matches(candidate) {
		return *existing, false, nil
	}
	switch LivenessOf(*existing, rd) {
	case Online:
		return Record{}, false, refuse(409, "coordinator_online",
			"The bound session is alive; a rebind is reconnect, not replacement.")
	case Unknown:
		return Record{}, false, refuse(409, "coordinator_liveness_unknown",
			"The bound session could not be read, which is not the same as gone.")
	}
	if !candidate.Complete() {
		return Record{}, false, refuse(409, "session_unbound",
			"That session's process, start time or conversation could not be read, so it cannot be bound as one exact process.")
	}
	if !now.After(existing.Since()) {
		return Record{}, false, refuse(409, "coordinator_store_invalid",
			"The clock reads earlier than the current binding began; nothing was moved.")
	}
	next := *existing
	next.Aliases = append(append([]Alias(nil), existing.Aliases...), Alias{
		ConversationID: existing.ConversationID,
		TerminalID:     existing.TerminalID,
		Assistant:      existing.Assistant,
		BoundAt:        existing.Since(),
		UnboundAt:      now,
	})
	if n := len(next.Aliases); n > AliasLimit {
		next.Aliases = next.Aliases[n-AliasLimit:]
	}
	next.Identity = candidate
	next.SessionLabel = ""
	next.CWD = ""
	next.Generation = existing.Generation + 1
	next.ReboundAt = now
	return next, true, nil
}

// DeliveryFor is Coordinator.deliveryBinding: a task dispatched by an earlier
// binding of the role, while it was that binding, is reported to the role's
// current session. Ordinary resolution never reads aliases; this is the one
// place an old conversation id still means something.
func (r Record) DeliveryFor(conversationID, assistant string, created time.Time) (string, bool) {
	if conversationID == "" || conversationID == r.ConversationID {
		return "", false
	}
	for _, a := range r.Aliases {
		if a.ConversationID != conversationID || (assistant != "" && a.Assistant != assistant) {
			continue
		}
		if !created.Before(a.BoundAt) && created.Before(a.UnboundAt) {
			return r.ConversationID, true
		}
	}
	return "", false
}
