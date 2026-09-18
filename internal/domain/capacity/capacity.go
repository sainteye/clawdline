// Package capacity is the register of every bounded thing this daemon keeps,
// and the rules for what happens when one of them fills (docs/limits.md §4,
// docs/design-decisions.md D27).
//
// It is not called `limits`: that name is already the plan-quota reader in
// internal/adapters/limits, and one name for two concepts is the defect
// docs/design-guidelines.md DG-5 exists to stop.
//
// The register answers four questions for every row, and a row missing one of
// them is not registered (DG-2):
//
//   - how much it may hold (`Limit`, in `Unit`),
//   - what happens when it is full (`AtLimit`, and `Deviation` when today's
//     code does not yet do what the row's class requires),
//   - who finds out (`Told`),
//   - who decides what is let go (`EvictedBy`).
//
// Nothing in this package reads a file, a clock or the environment. The
// adapters measure; the transport runs the beat and records what it saw; this
// package decides what a reading means.
package capacity

import "regexp"

// Class is what kind of data a row holds. It decides which at-limit behaviours
// are allowed (docs/limits.md §4.2).
type Class string

const (
	// Evidence is never evicted: at its limit it refuses new writes and says
	// so on the open health route.
	Evidence Class = "evidence"
	// SecurityAudit is never deleted; it rotates into segments that are kept.
	SecurityAudit Class = "security_audit"
	// Idempotency receipts may go once their retry window has passed.
	Idempotency Class = "idempotency"
	// UserInput is a person's pictures and drops.
	UserInput Class = "user_input"
	// Narrative is long prose that may be summarised and moved.
	Narrative Class = "narrative"
	// Progress is notes and activity, kept in a window with a dropped count.
	Progress Class = "progress"
	// Observation can be read again from its source.
	Observation Class = "observation"
	// Journal is operational history: state transitions, not repetitions.
	Journal Class = "journal"
	// Work is task directories, worktrees and build output.
	Work Class = "work"
	// Cache is anything rebuilt on a miss.
	Cache Class = "cache"
	// DiagnosticLog is the daemon's own log.
	DiagnosticLog Class = "diagnostic_log"
	// Buffer is a live queue between two parts of this process or two
	// machines: what it drops, the other side must hear about.
	Buffer Class = "buffer"
)

// Action is what happens when a row reaches its limit.
type Action string

const (
	Refuse      Action = "refuse"
	EvictOldest Action = "evict_oldest"
	Expire      Action = "expire"
	Rotate      Action = "rotate"
	Summarize   Action = "summarize"
	Coalesce    Action = "coalesce"
	Disconnect  Action = "disconnect"
	// Nothing is refused, removed or rotated: the limit is only reported. No
	// class allows it, so a row that does this carries a Deviation.
	Nothing Action = "none"
)

// allowed is docs/limits.md §4.2's last column. An action outside a class's
// list is a defect in that row, not a choice.
var allowed = map[Class][]Action{
	Evidence:      {Refuse},
	SecurityAudit: {Rotate},
	Idempotency:   {Expire, Refuse},
	UserInput:     {EvictOldest},
	Narrative:     {Summarize},
	Progress:      {EvictOldest},
	Observation:   {EvictOldest},
	Journal:       {EvictOldest, Rotate},
	Work:          {EvictOldest},
	Cache:         {EvictOldest},
	DiagnosticLog: {Rotate},
	Buffer:        {Coalesce, Refuse, Disconnect},
}

// Allowed reports whether a class permits an at-limit behaviour.
func Allowed(c Class, a Action) bool {
	for _, x := range allowed[c] {
		if x == a {
			return true
		}
	}
	return false
}

// KnownClass reports whether c is one of the classes above.
func KnownClass(c Class) bool { _, ok := allowed[c]; return ok }

// Unit is what `Limit` and a reading's `Used` count.
type Unit string

const (
	Bytes Unit = "bytes"
	Rows  Unit = "rows"
)

// Channel is a way a full row reaches somebody.
type Channel string

const (
	// Diagnostics is /v1/diagnostics.capacity, behind this machine's token.
	// Every row is there, always.
	Diagnostics Channel = "diagnostics"
	// Health is the open /v1/health, which says `capacity_exhausted` — never
	// the name or the numbers — when an evidence or security-audit row is
	// exhausted.
	Health Channel = "health"
	// Notice is a notification intent: a `capacity.notify` event in the store
	// on entering critical or full and on recovering, at most one per row per
	// day. It becomes a push in C4.
	Notice Channel = "notice"
	// CloudStatus is /v1/cloud/status.
	CloudStatus Channel = "cloud_status"
	// Log is the daemon's log.
	Log Channel = "log"
)

// Decider is who decides what a full row lets go of.
type Decider string

const (
	// Person: nothing here is let go by the daemon. Only a person removes it.
	Person Decider = "person"
	// Daemon: the daemon lets go by a rule of its own, named in AtLimit.
	Daemon Decider = "daemon"
)

// The names of the rows. Stable: they are keys on the wire and in the
// CLAWDLINE_NEXT_CAPACITY override.
const (
	AuditSecurity   = "audit.security"
	StoreDB         = "store.db"
	BoardReceipts   = "board.receipts"
	CloudRelayQueue = "cloud.relay_queue"
)

// Entry is one row of the register.
type Entry struct {
	Name  string
	Class Class
	Unit  Unit
	// Limit is the default. An override may only lower it (Resolve).
	Limit int64
	// WarnAt is the ratio at which the row leaves `ok`. Zero means 0.8.
	WarnAt float64
	// AtLimit is what the code does today when the row is full.
	AtLimit Action
	// Deviation is non-empty when AtLimit is not what the class requires: it
	// says what the class requires and which decision or wave brings it.
	Deviation string
	Told      []Channel
	EvictedBy Decider
	// Projects says a projection of when the row fills is worth a warning
	// before it does. A queue that fills and drains, or a file that rotates
	// at its limit by design, has no wall to warn about.
	Projects bool
	// Sources are the declarations in this repository that bound this row, as
	// the register guard spells them (guard_test.go). A bounded declaration
	// that is neither a row's source nor on the guard's baseline is a failing
	// test.
	Sources []string
}

// Warn is the row's warn ratio.
func (e Entry) Warn() float64 {
	if e.WarnAt > 0 {
		return e.WarnAt
	}
	return 0.8
}

// namePattern is `area.thing`, lower case.
var namePattern = regexp.MustCompile(`^[a-z]+(\.[a-z_]+)+$`)

// Register is every row this daemon measures. It is a function rather than a
// variable so nobody can edit the table they were handed.
//
// C1 registers the four rows docs/limits.md §7.1 names. The rest of the
// bounded things in this repository are on the guard's baseline, each with the
// limits.md row that will register it.
func Register() []Entry {
	return []Entry{
		{
			// remote-audit.jsonl (limits N12). Rotated into segments at this
			// size; a segment is never deleted by the daemon.
			Name: AuditSecurity, Class: SecurityAudit, Unit: Bytes,
			Limit: 8 << 20, AtLimit: Rotate,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
		},
		{
			// clawdline.sqlite3 with its -wal and -shm. Evidence: the broker's
			// task records and landings are in it.
			Name: StoreDB, Class: Evidence, Unit: Bytes,
			Limit: 1 << 30, AtLimit: Nothing,
			Deviation: "limits §7.1: C1 only measures this row. Its class refuses new writes at the limit (limits §4.4 step 5, 507 storage_exhausted); nothing refuses yet, and ok:false on /v1/health is the only effect of full.",
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Projects:  true,
		},
		{
			// project-board.json's (actor, requestId) receipts (limits N24).
			Name: BoardReceipts, Class: Idempotency, Unit: Rows,
			Limit: 4_096, AtLimit: EvictOldest,
			Deviation: "D03 (W2): receipts expire by time and a full window refuses new commands (429); today the oldest receipt is evicted by count, which can evict one still inside its retry window.",
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
			Projects:  true,
		},
		{
			// Decrypted Cloud requests waiting for the bridge (limits N20).
			Name: CloudRelayQueue, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: EvictOldest,
			Deviation: "limits N20 (C3): a queue of instructions refuses the new request and tells the sender to retry; today the oldest waiting request is dropped and a line is logged.",
			Told:      []Channel{Diagnostics, Notice, CloudStatus, Log},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/cloud.(*Relay).start:chan(r.depth())"},
		},
	}
}

// Default is a registered row's default limit, and 0 for a name that is not
// registered. Adapters that enforce a row's limit take their default from
// here, so the number has one spelling.
func Default(name string) int64 {
	for _, e := range Register() {
		if e.Name == name {
			return e.Limit
		}
	}
	return 0
}
