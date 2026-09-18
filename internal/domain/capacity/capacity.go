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
	// Sender is whoever sent the thing the row turned away, told in the
	// answer to that very request: a Cloud request refused at a full queue
	// comes back `cloud_ingress_busy` on the channel the viewer is waiting on.
	Sender Channel = "sender"
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
	StoreReceipts   = "store.receipts"
	// C2: the things that had no limit at all (limits §3.3, §7.2 wave 2).
	LogDaemon             = "log.daemon"
	DevicesList           = "devices.list"
	PushSubscriptions     = "push.subscriptions"
	CacheTranscriptUsage  = "cache.transcript_usage"
	CacheTranscriptTitles = "cache.transcript_titles"
	// C3: the quiet failures made loud (limits §3.2, §7.2 wave 3).
	SSEScreenPending   = "sse.screen_pending"
	ArtifactsImages    = "artifacts.images"
	ArtifactsImageSize = "artifacts.image_bytes"
	ArtifactsDrops     = "artifacts.drops"
	// W5: the coordination plane (design-decisions §6 W5).
	LeasesQueue        = "leases.queue"
	CoordinatorAliases = "coordinator.aliases"
	WaitsOpen          = "waits.open"
	// T3: the board and the Backlog.
	WorkOpen = "work.open"
	// T4: where a person takes part.
	ProposalsOpen = "proposals.open"
	DecisionsOpen = "decisions.open"
	WorkDigests   = "work.digests"
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
// C1 registered the four rows docs/limits.md §7.1 names; C2 the five that had
// no limit at all (§7.2 wave 2); C3 the four whose failure was quiet (§7.2
// wave 3). The rest of the bounded things in this
// repository are on the guard's baseline, each with the limits.md row that
// will register it.
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
			// The board commands' (actor, requestId) receipts (limits N24):
			// the store's receipt table, scope `board`, since the board's
			// settings moved into clawdline.sqlite3 (D37, T3). They expire by
			// time, and a full window refuses a new command with 429 rather
			// than evicting a receipt somebody may still retry against.
			Name: BoardReceipts, Class: Idempotency, Unit: Rows,
			Limit: 4_096, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
			Projects:  true,
		},
		{
			// The store's request receipts (D03, W2): per scope, inside each
			// receipt's window. At the limit a new request in that scope is
			// refused with a Retry-After; nothing inside its window is
			// evicted, and an answered receipt past it keeps only its key and
			// digest. Used is the fullest scope.
			Name: StoreReceipts, Class: Idempotency, Unit: Rows,
			Limit: 4_096, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/store.ReceiptLimit"},
		},
		{
			// Decrypted Cloud requests waiting for the bridge (limits N20).
			// At the limit the new request is refused and nothing already
			// taken is let go: the sender is answered 429 cloud_ingress_busy
			// with a retry_after on the channel it is waiting on (C3). Only a
			// refusal that could not itself be sent reaches nobody, and that
			// is counted as dropped.
			Name: CloudRelayQueue, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, CloudStatus, Log, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/cloud.(*Relay).start:chan(r.depth())"},
		},
		{
			// CLAWDLINE_NEXT_DIR/logs/daemon.log (limits N29). At this size it
			// becomes a segment and a new file is begun; the daemon keeps the
			// newest of those segments and deletes the rest, so the whole log
			// is at most the current file and maxSegments more.
			Name: LogDaemon, Class: DiagnosticLog, Unit: Bytes,
			Limit: 10 << 20, AtLimit: Rotate,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/logs.maxSegments"},
		},
		{
			// remote.json's paired devices (limits N13). Each is somebody's
			// access, so only a person lets one go: at the limit a new
			// pairing, password sign-in or browser device is refused with
			// 507 device_list_full, and revoking still works. The read bound
			// is registered here too: the most rows this limit allows,
			// written at their longest, stay far under it (a test holds
			// that), so the file cannot grow past what the daemon reads at
			// startup — the limit speaks first.
			Name: DevicesList, Class: Evidence, Unit: Rows,
			Limit: 512, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Projects:  true,
			Sources:   []string{"internal/adapters/devices.storeLimit"},
		},
		{
			// push/subscriptions.json (limits N14). A person's standing
			// request to be told, one row per device: at the limit a new
			// subscription is refused with 507 subscriptions_full rather
			// than one being dropped, and the read bound is registered for
			// the same reason as the device list's.
			Name: PushSubscriptions, Class: Evidence, Unit: Rows,
			Limit: 128, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Projects:  true,
			Sources:   []string{"internal/adapters/push.subscriptionsLimit"},
		},
		{
			// What each live session's transcript has been counted to, so
			// the usage route reads only what is new (limits N18). A miss
			// reads the transcript again from its start.
			Name: CacheTranscriptUsage, Class: Cache, Unit: Rows,
			Limit: 256, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
		},
		{
			// Each conversation's title as last read, keyed on the file's size
			// and time (limits N18). A miss reads the transcript's tail again.
			Name: CacheTranscriptTitles, Class: Cache, Unit: Rows,
			Limit: 256, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
		},
		{
			// The screens one event stream has been told moved and has not
			// yet written out (limits N19). Each screen holds one frame, its
			// newest: a newer revision replaces a waiting one and is counted
			// as coalesced, so a slow stream is told late and never told
			// wrong. Past this many different screens waiting on one stream
			// the stream is ended, counted as disconnected, and the page's
			// reconnect reads every screen afresh. Used is the most any one
			// stream has waiting now.
			Name: SSEScreenPending, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Disconnect,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
		},
		{
			// Pictures behind <clawdline-image id> (limits N15). Every one
			// was stored to be shown in a reply, so every live one is
			// referenced. The daemon measures after each store, so warn and
			// critical are said before the store is full and before the
			// oldest is let go; a picture let go is counted, logged, and its
			// tombstone says why to whoever asks for it.
			Name: ArtifactsImages, Class: UserInput, Unit: Rows,
			Limit: 64, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice, Log},
			EvictedBy: Daemon,
		},
		{
			// The same pictures' bytes, the store's second bound (limits
			// N15): whichever of the two is reached lets the oldest go.
			Name: ArtifactsImageSize, Class: UserInput, Unit: Bytes,
			Limit: 64 << 20, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice, Log},
			EvictedBy: Daemon,
		},
		{
			// Pictures written out for a terminal program to read, each one
			// already typed into a prompt as a path (limits N16). Measured
			// after each write, like the images, so the warning comes before
			// the oldest file is removed.
			Name: ArtifactsDrops, Class: UserInput, Unit: Rows,
			Limit: 40, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice, Log},
			EvictedBy: Daemon,
		},
		{
			// The askers waiting for one lease — the compile slot, or one
			// checkout's landing (D06 ②, D20). At the limit a new asker is
			// refused with 429 queue_full and a retry_after, in the answer
			// to its own ask; nobody already in line is let go. A waiter that
			// stops asking is passed over at once and forgotten after half an
			// hour. Used is the longest line.
			Name: LeasesQueue, Class: Buffer, Unit: Rows,
			Limit: 32, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app/orchestrator.LeaseQueueLimit"},
		},
		{
			// The machine role's earlier bindings, kept only to route a
			// completion notice for a task an earlier binding dispatched
			// (Coordinator.swift keeps the last 32). A rebind past the limit
			// lets the oldest go; every rebind is also an event in the store,
			// so nothing is lost but the routing.
			Name: CoordinatorAliases, Class: Journal, Unit: Rows,
			Limit: 32, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/domain/coordinator.AliasLimit"},
		},
		{
			// File waits not yet fully released: one session's paths held,
			// others waiting to be let go. The Swift app had no limit. At
			// this one a new wait is refused with 429 waits_full in the
			// answer to the session asking; an open wait is never dropped —
			// only its owner releases it, or its waiters leave.
			Name: WaitsOpen, Class: Buffer, Unit: Rows,
			Limit: 256, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app/orchestrator.WaitsOpenLimit"},
		},
		{
			// Open work items (design-decisions T3): on the board and not
			// closed, or planned in the Backlog. Each is a person's plan or
			// a commitment somebody made, so nothing is let go at the limit:
			// a new item is refused with 507 work_full, and only a person
			// closes or drops one. Every board item has an exit (a landing,
			// the closure queue, three quiet days back to the Backlog); the
			// Backlog's exit is a person, by design (board-redesign §5.3).
			// Moves are append-only and ride on store.db.
			Name: WorkOpen, Class: Evidence, Unit: Rows,
			Limit: 2_000, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Projects:  true,
			Sources:   []string{"internal/adapters/store.WorkOpenLimit"},
		},
		{
			// Proposals waiting for a person's answer — the "to confirm"
			// area (design-decisions T4, board-redesign §4.4). At the limit a
			// new proposal is refused with 429 proposals_full in the answer
			// to the session that made it; none waiting is let go early. The
			// work it was about stays with its to-dos, which is also what an
			// unanswered proposal comes to: each one waiting leaves after
			// seven days with that answer (§10 #4). Answered and expired
			// proposals are a person's answers and ride on store.db.
			Name: ProposalsOpen, Class: Buffer, Unit: Rows,
			Limit: 500, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/store.ProposalOpenLimit"},
		},
		{
			// Decisions a session asked and nobody has answered (T4). At the
			// limit a new one is refused with 429 decisions_full in the answer
			// to the session asking; none open is let go early. Each leaves
			// by its due — at most seven days — with the default it named.
			Name: DecisionsOpen, Class: Buffer, Unit: Rows,
			Limit: 256, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/store.DecisionOpenLimit"},
		},
		{
			// The daily and weekly digests (T4, board-redesign §8), one row
			// each: about two years of them. Past the limit the oldest is let
			// go in the transaction that writes the newest; every fact one
			// summarised is still in moves, proposals and decisions.
			Name: WorkDigests, Class: Journal, Unit: Rows,
			Limit: 800, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/store.DigestKeepLimit"},
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
