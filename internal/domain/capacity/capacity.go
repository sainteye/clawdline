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
	Cache:         {EvictOldest, Expire},
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
	Bytes      Unit = "bytes"
	Characters Unit = "characters"
	Rows       Unit = "rows"
	// Seconds is the age of an observation whose honesty depends on a time
	// horizon. It is a capacity in the literal sense: once filled, the held
	// observation expires and the reader must say it does not know.
	Seconds Unit = "seconds"
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
	// day, and the push it owes (notice.go, C4), recorded in the same
	// transaction and sent after it commits.
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
	WorkOpen                  = "work.open"
	WorkPlanning              = "work.planning"
	WorkAssignments           = "work.assignments"
	WorkDocumentsPerItem      = "work.documents_per_item"
	WorkImagesPerItem         = "work.images_per_item"
	WorkImageBytes            = "work.image_bytes"
	WorkImageBytesPerItem     = "work.image_bytes_per_item"
	WorkImageBytesTotal       = "work.image_bytes_total"
	WorkImageRequestBodyBytes = "work.image_request_body_bytes"
	WorkStepsPerItem          = "work.steps_per_item"
	SessionDirectTodos        = "session.direct_todos"
	SessionTitleRequestBytes  = "session.title_request_bytes"
	SessionTitleCharacters    = "session.title_characters"
	SessionTitleRows          = "session.title_rows"
	SessionTitleAge           = "session.title_age"
	WorkItemTitleBytes        = "work.item_title_bytes"
	WorkItemDescriptionBytes  = "work.item_description_bytes"
	WorkItemUserActionBytes   = "work.item_user_action_bytes"
	WorkCompletionReasonBytes = "work.completion_reason_bytes"
	SessionDirectTodoBytes    = "session.direct_todo_bytes"
	SessionTodoBatchRows      = "session.todo_batch_rows"
	ReportOpenTodoRows        = "session.report_open_todo_rows"
	ReportOpenTodoCharacters  = "session.report_open_todo_characters"
	RunCreatedItems           = "run.created_items"
	WorkRequestBodyBytes      = "work.request_body_bytes"
	// T4: where a person takes part.
	ProposalsOpen = "proposals.open"
	DecisionsOpen = "decisions.open"
	WorkDigests   = "work.digests"
	// C4: the Cloud answers waiting to leave (limits N22).
	CloudSpool      = "cloud.spool"
	CloudSpoolBytes = "cloud.spool_bytes"
	// The same spool per wire channel, and its receipts. The per-channel
	// bound is the one that refuses a Cloud answer in practice; the receipts
	// are the tombstones that used to be charged as though they were queue.
	CloudSpoolChannelBytes = "cloud.spool_channel_bytes"
	CloudSpoolReceipts     = "cloud.spool_receipts"
	// The typed refusals one wire channel may have owed at once: each answers
	// one refused read by its own id, so there may be more than one.
	CloudSpoolRefusals = "cloud.spool_refusals"
	// The reference-image thumbnails drawn for Board cards and to-do rows.
	CacheImageThumbs = "cache.image_thumbs"
	// The slash menu's skills, per working directory and assistant.
	CacheSessionSkills = "cache.session_skills"
	// The plan-window reading of each assistant's account.
	CacheAssistantQuota = "cache.assistant_quota"
	// The text somebody wrote once and presses instead of typing again: how
	// many this machine holds, and how many are in one group of them.
	SnippetsTotal = "snippets.total"
	SnippetsScope = "snippets.scope"
	// The screens the session list reads: how many captures may be in flight
	// at once, and how many screens are held between them.
	ScreensCaptureSlots   = "screens.capture_slots"
	CacheTerminalScreens  = "cache.terminal_screens"
	CacheSessionInventory = "cache.session_inventory"
	// The Project Timeline is a projection that stores nothing, so what is
	// bounded is the read (limits §3.3).
	TimelineEntries = "timeline.entries"
	// When each session last moved: how many records one reading of the
	// machine may read, and what the last reading of each one found.
	SessionsActivityReads = "sessions.activity_reads"
	CacheSessionActivity  = "cache.session_activity"
	// Where each project can be opened, one reading per working directory.
	CacheSessionLinks = "cache.session_links"
	// Directories a person explicitly keeps in the session-start list.
	PlacesRegistered = "places.registered"
	// Provider-native work under a session: how many rows the fleet carries,
	// and the immutable metadata/changing-tail cursors held to make a one-second
	// reading cheap.
	SessionsAgentRows     = "sessions.agent_rows"
	CacheBackgroundAgents = "cache.background_agents"
	// The sentence-to-draft planner: admitted bytes, queued turns and the
	// longest one turn may hold its queue slot.
	IntentRequestBytes     = "intent.request_bytes"
	IntentPlannerQueue     = "intent.planner_queue"
	IntentPlannerSeconds   = "intent.planner_seconds"
	IntentCloudWaitSeconds = "intent.cloud_wait_seconds"
	// A release keeps the previous selector until the restarted daemon and
	// its console prove the new commit. At this deadline it rolls back.
	DeployHealthSeconds = "deploy.health_seconds"
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
// wave 3); C4 the Cloud spool's two, whose refusals only a log heard (§7.2
// wave 5). The rest of the bounded things in this
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
			// Each conversation's title as last read, keyed on the file's size
			// and time (limits N18). A miss reads the transcript's tail again.
			Name: CacheTranscriptTitles, Class: Cache, Unit: Rows,
			Limit: 256, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
		},
		{
			// Provider-native child work carried on one session row. The newest
			// running work wins; the payload says how many rows were omitted.
			Name: SessionsAgentRows, Class: Observation, Unit: Rows,
			Limit: 6, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/subagents.MaximumShown"},
		},
		{
			// Claude sidecars, changing transcript tails and append cursors.
			// Each cache is bounded at this row; Used is the fullest of them.
			Name: CacheBackgroundAgents, Class: Cache, Unit: Rows,
			Limit: 256, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/subagents.MaximumCache"},
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
			Sources:   []string{"internal/adapters/store.WorkOpenLimit", "internal/adapters/store.WorkV2OpenLimit"},
		},
		{
			Name: WorkPlanning, Class: Evidence, Unit: Rows,
			Limit: 1_000, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Projects:  true,
			Sources:   []string{"internal/adapters/store.WorkV2PlanningLimit"},
		},
		{
			Name: WorkAssignments, Class: Evidence, Unit: Rows,
			Limit: 4_000, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/store.WorkV2AssignmentLimit"},
		},
		{
			Name: WorkDocumentsPerItem, Class: Evidence, Unit: Rows,
			Limit: 32, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/store.WorkV2DocumentLimit"},
		},
		{
			Name: WorkImagesPerItem, Class: Evidence, Unit: Rows,
			Limit: 6, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Projects:  true,
			Sources:   []string{"internal/adapters/store.WorkV2ImageLimit"},
		},
		{
			Name: WorkImageBytes, Class: Evidence, Unit: Bytes,
			Limit: 5 << 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Projects:  true,
			Sources:   []string{"internal/transport/http.workV2ImageByteLimit"},
		},
		{
			Name: WorkImageBytesPerItem, Class: Evidence, Unit: Bytes,
			Limit: 15 << 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Projects:  true,
			Sources:   []string{"internal/adapters/store.WorkV2ImageItemLimit"},
		},
		{
			Name: WorkImageBytesTotal, Class: Evidence, Unit: Bytes,
			Limit: 512 << 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/store.WorkV2ImageTotalLimit"},
		},
		{
			Name: WorkImageRequestBodyBytes, Class: Buffer, Unit: Bytes,
			Limit: 18 << 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Projects:  true,
			Sources:   []string{"internal/transport/http.workV2ImageBodyLimit", "internal/app/cloudops.workV2CloudImageBodyLimit"},
		},
		{
			Name: WorkStepsPerItem, Class: Evidence, Unit: Rows,
			Limit: 128, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/store.WorkV2StepLimit"},
		},
		{
			Name: SessionDirectTodos, Class: Evidence, Unit: Rows,
			Limit: 500, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/store.DirectTodoV2Limit"},
		},
		{
			Name: WorkItemTitleBytes, Class: Evidence, Unit: Bytes,
			Limit: 240, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person, Projects: true,
			Sources: []string{"internal/app.workV2TitleLimit"},
		},
		{
			Name: WorkItemDescriptionBytes, Class: Evidence, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person, Projects: true,
			Sources: []string{"internal/app.workV2DescriptionLimit"},
		},
		{
			Name: WorkItemUserActionBytes, Class: Evidence, Unit: Bytes,
			Limit: 8 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person, Projects: true,
			Sources: []string{"internal/app.workV2UserActionLimit"},
		},
		{
			Name: WorkCompletionReasonBytes, Class: Evidence, Unit: Bytes,
			Limit: 8 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person, Projects: true,
			Sources: []string{"internal/app.workV2CompletionReasonLimit"},
		},
		{
			Name: SessionDirectTodoBytes, Class: Evidence, Unit: Bytes,
			Limit: 8 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person,
			Sources: []string{"internal/app.directTodoTextLimit"},
		},
		{
			// The rows one Session may add to its own to-do list in one call.
			// The whole batch is written or none of it is.
			Name: SessionTodoBatchRows, Class: Buffer, Unit: Rows,
			Limit: 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.sessionTodoBatchLimit"},
		},
		{
			// The delivered, unfinished to-dos one turn receipt lists
			// (work-system-v2 §11.2). A row past it is not admitted to the
			// answer, which says so with open_todos_truncated; nothing is
			// removed from the store.
			Name: ReportOpenTodoRows, Class: Buffer, Unit: Rows,
			Limit: 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.reportOpenTodoLimit"},
		},
		{
			// How much of each listed to-do's text that receipt repeats, on
			// one line and ending in an ellipsis when cut; the id names the
			// whole row.
			Name: ReportOpenTodoCharacters, Class: Buffer, Unit: Characters,
			Limit: 120, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.reportOpenTodoTextLimit"},
		},
		{
			// The Board items one person's message may back when a Session
			// creates them on it (work-system-v2 §2, amended 2026-09-25).
			// Counted over every item that run created, open or closed; the
			// sixth is refused run_items_exhausted and nothing is written.
			Name: RunCreatedItems, Class: Buffer, Unit: Rows,
			Limit: 5, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.runItemLimit"},
		},
		{
			Name: WorkRequestBodyBytes, Class: Buffer, Unit: Bytes,
			Limit: 96 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.workV2BodyLimit", "internal/app/cloudops.workV2CloudBodyLimit",
				"cmd/clawdline.todoInputLimit"},
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
			Sources:   []string{"internal/adapters/store.ProposalOpenLimit", "internal/adapters/store.WorkV2ProposalLimit"},
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
		{
			// The Cloud line's outbound spool (limits N22): answers and
			// snapshots sealed for the relay and not yet answered by it. At
			// the limit a new answer is refused and nothing already queued is
			// let go — a queued row is somebody's answer — and the refusal is
			// counted here rather than only logged. So are the rows the spool
			// burns: an older snapshot replaced by a newer one (coalesced), a
			// row too old to send (dropped), and a row written and never
			// answered within its window, whose delivery is unknown (in the
			// note: unknown is neither sent nor dropped).
			//
			// **Answered rows are not in this figure.** They were, and both
			// this row and the byte row below therefore measured a population
			// no reservation is really competing for; the receipts have a row
			// of their own now (CloudSpoolReceipts).
			Name: CloudSpool, Class: Buffer, Unit: Rows,
			Limit: 2_000, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Log},
			EvictedBy: Daemon,
		},
		{
			// The same spool's bytes, its second bound: whichever of the two
			// a new answer would cross refuses it.
			Name: CloudSpoolBytes, Class: Buffer, Unit: Bytes,
			Limit: 16 << 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Log},
			EvictedBy: Daemon,
		},
		{
			// What one wire channel may have owed to it at once, and the
			// bound that refuses a Cloud answer in practice: measured on
			// 2026-09-21, one session's transcript channel was refused 34
			// times over while the two rows above stood at 401/2,000 and
			// 3.09 MB/16 MiB.
			//
			// **Four mebibytes, and where the four comes from.** One channel
			// has to hold the largest single answer that may legally be
			// published on it, or that answer can never be delivered at all;
			// the largest this daemon builds outside a picture is a document,
			// bounded at 2 MiB by internal/adapters/documents.MaximumBytes.
			// It has to hold a second one behind it, because the first is
			// still waiting for its receipt while the next read is answered.
			// Two of them is 4 MiB, which is also exactly the spool's
			// in-flight window (OutboundWindowByteCap): past it a channel
			// would be queueing more than the line can carry, which is a
			// backlog to refuse rather than to keep. For scale, the answers
			// measured on this machine the same day were 151,932 bytes for a
			// full transcript page and 362,457 bytes for the largest of 35
			// project boards.
			//
			// A refusal is admitted past this cap under a reserve of its own
			// (internal/adapters/cloud.spoolRefusalByteLimit), because a
			// channel that cannot say it is full is the silence this row
			// exists to end.
			Name: CloudSpoolChannelBytes, Class: Buffer, Unit: Bytes,
			Limit: 4 << 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Log},
			EvictedBy: Daemon,
		},
		{
			// The spool's tombstones: a row that has been answered, kept for
			// ten minutes so that a second receipt for the same sequence is
			// answered "late, ignored" rather than "no such row" — which is
			// what a receipt for a sequence this machine never sent must
			// mean, and only that.
			//
			// It is an idempotency table and not a queue, which is the whole
			// point of separating it: the payload went at Settle, so a
			// receipt holds no delivery capacity and must not be charged as
			// though it did. Past the cap the oldest receipt expires early
			// and is counted, because from then on a late ack for that
			// sequence reads as a correlation failure.
			Name: CloudSpoolReceipts, Class: Idempotency, Unit: Rows,
			Limit: 4_096, AtLimit: Expire,
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
		},
		{
			// The refusals one wire channel may have owed at once, each at
			// most internal/adapters/cloud.spoolRefusalByteLimit and admitted
			// past that channel's own byte cap. Until 2026-09-25 the rule was
			// one: every carried read waits on its own read id, so the second
			// refused read on a channel got no answer at all and its browser
			// waited out its sixty seconds ("did not fit its channel and the
			// refusal did not either"). Sixty-four is a whole Board's worth
			// of reference images refused at once, and 64 × 4 KiB is 256 KiB
			// a channel at the very worst. Past it a refusal is dropped and
			// the drop is logged with the read's operation.
			Name: CloudSpoolRefusals, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
		},
		{
			// The thumbnails a Board card or a to-do row asks for
			// (`GET /v1/work/v2/images/{id}?size=thumb`), drawn from the
			// stored image on a miss and held by id and digest. A thumbnail
			// of a 1600 × 873 screenshot is tens of kilobytes; eight
			// mebibytes is a couple of hundred of them. Past the limit the
			// thumbnail used longest ago is let go and drawn again when it
			// is next asked for; nothing durable is lost.
			Name: CacheImageThumbs, Class: Cache, Unit: Bytes,
			Limit: 8 << 20, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
		},
		{
			// The skills each session's slash menu offers, one reading per
			// working directory (or Codex rollout) and assistant, served for
			// five minutes as the Swift app's SessionLinksCache.skills serves
			// them. Past the limit the reading used longest ago is let go; a
			// miss walks the skills directories again.
			Name: CacheSessionSkills, Class: Cache, Unit: Rows,
			Limit: 64, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
		},
		{
			// What the providers last said about each account's plan windows
			// (limits N18), one reading per assistant, held for five seconds.
			// The row is here for what it rules out as much as for what it
			// holds: the reading is taken when somebody asks, out of files
			// the providers write anyway, so this cache grows with the number
			// of assistants and with nothing else — not with sessions, not
			// with projects, and never on a beat that would spend quota to
			// ask how much quota is left. Past the limit the reading handed
			// out longest ago is let go, and a miss reads the files again.
			// Two readings is all it has ever held; the limit is headroom,
			// and what the row is for is that nothing makes it grow.
			Name: CacheAssistantQuota, Class: Cache, Unit: Rows,
			Limit: 32, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/limits.quotaCacheLimit"},
		},
		{
			// Every snippet on this machine. Each one is a piece of text a
			// person wrote, so nothing is let go at the limit: one more is
			// refused with 409 `snippet_limit_reached`, in the answer to the
			// sheet that asked, and only a person deletes one. The bytes they
			// hold are bounded per field at the door (internal/domain/snippet)
			// and accumulate on store.db, which has its own row.
			Name: SnippetsTotal, Class: Evidence, Unit: Rows,
			Limit: 100, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Projects:  true,
			Sources:   []string{"internal/adapters/store.SnippetTotalLimit"},
		},
		{
			// One group of them: every project, or one project. The second
			// bound exists because the first is reached by a hundred snippets
			// anywhere, and a list of fifty in one sheet is already longer than
			// anybody scrolls. Refused the same way, and Used is the fullest
			// group, named in the reading.
			Name: SnippetsScope, Class: Evidence, Unit: Rows,
			Limit: 50, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Projects:  true,
			Sources:   []string{"internal/adapters/store.SnippetScopeLimit"},
		},
		{
			// The screen captures the session list may have in flight at once
			// (internal/app/screen_held.go). This is the row that bounds how
			// deep a queue this daemon can put in front of a person's own
			// keystroke: every iTerm2 capture is an AppleScript behind one
			// process-wide Apple Events lock, and the list used to start one
			// per qualifying row per reader — twenty rows times three loops,
			// each waiting for all the ones before it. At this limit a refresh
			// is not started at all; the reader is answered from the held
			// screen and the skip is counted, because the alternative is a
			// queue whose length is the number of rows.
			Name: ScreensCaptureSlots, Class: Buffer, Unit: Rows,
			Limit: 2, AtLimit: Refuse,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.ScreenCaptureLimit"},
		},
		{
			// The last complete per-terminal-source inventory kept across a
			// failed scan (internal/app/inventory_reading.go). At this age it
			// expires: two minutes spans twelve complete iTerm2 list timeouts and
			// matches the held-screen backoff ceiling, while anything older would
			// be a claim about the present rather than a named prior observation.
			Name: CacheSessionInventory, Class: Cache, Unit: Seconds,
			Limit: 120, AtLimit: Expire,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.LastGoodInventoryAgeLimit"},
		},
		{
			// The screens held between those captures, one per session the
			// list has asked about. Past the limit the screen asked about
			// longest ago is let go, and a screen nobody has asked about for
			// two minutes goes with it; a miss is one capture behind the
			// answer, like every other refresh here.
			Name: CacheTerminalScreens, Class: Cache, Unit: Rows,
			Limit: 64, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.ScreenHeldLimit"},
		},
		{
			// One Project's Timeline entries. The Swift app's equivalent
			// refused new writes here and stopped recording for over a day
			// with nobody told (timeline-design §0); this row cannot repeat
			// that, because the Timeline stores nothing — every entry is
			// re-derived from the broker's records on the next read, so the
			// oldest going is a shorter answer and never a lost fact. The
			// count and the limit are both on the wire (timeline-design A4).
			Name: TimelineEntries, Class: Observation, Unit: Rows,
			Limit: 500, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/http.timelineEntryLimit"},
		},
		{
			// How many sessions one reading of the machine may ask an
			// activity time of (session.Activity). Each one is a stat of a
			// file the assistant writes anyway, inside the one producer, so
			// the cost is a stat per row per reading and not a transcript
			// read — but a bound that grows with the number of rows is not a
			// bound, and the one thing a list of sessions must survive is a
			// machine with a great many of them.
			//
			// Past the limit the row is not read and says so: its activity
			// comes back `unread`, which the order puts ahead of every known
			// time inside its state rather than below them. That is the whole
			// point of refusing rather than evicting here — a row left unread
			// must not be mistaken for a row that has been quiet, and it is
			// the reader, not this row, that is told.
			Name: SessionsActivityReads, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.ActivityReadLimit"},
		},
		{
			// What the last reading found about each session's own record:
			// where it is, and what its newest turn said the time was when the
			// file was that size. It is what keeps a list redrawn every two
			// seconds to one stat per row — the record itself is opened only
			// when it has changed. Past the limit the conversation read
			// longest ago is let go, and its next reading opens the file
			// again.
			Name: CacheSessionActivity, Class: Cache, Unit: Rows,
			Limit: 64, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
		},
		{
			// CLAWDLINE_NEXT_DIR/places.json: explicit project directories. Each
			// row is a person's choice, so the daemon never evicts one; a full
			// registry refuses a new path and `clawdline project remove` is its
			// deliberate exit.
			Name: PlacesRegistered, Class: Evidence, Unit: Rows,
			Limit: 512, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Projects:  true,
		},
		{
			Name: "icons.saved", Class: Evidence, Unit: Rows,
			Limit: 512, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person,
			Sources: []string{"internal/domain/icon.MaxIconOverrides"},
		},
		{
			Name: "projectsync.manifest_projects", Class: Buffer, Unit: Rows,
			Limit: 256, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectsync.MaxManifestProjects"},
		},
		{
			Name: "projectsync.project_files", Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectsync.MaxProjectFiles"},
		},
		{
			Name: "projectsync.file_bytes", Class: Buffer, Unit: Bytes,
			Limit: 256 << 10, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectsync.MaxFileBytes"},
		},
		{
			// How much of one checkout's git config GET /v1/places reads to
			// name the repository a place clones (its `repo`). A longer config
			// is not parsed and the place names no repository, which the
			// console says as "this project has no origin" when a schedule is
			// moved to another machine.
			Name: "places.git_config_bytes", Class: Buffer, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projects.MaxGitConfigBytes"},
		},
		{
			Name: "projectsync.path_bytes", Class: Buffer, Unit: Bytes,
			Limit: 512, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectsync.MaxPathBytes"},
		},
		{
			Name: "projectsync.entry_bytes", Class: Buffer, Unit: Bytes,
			Limit: 4 << 20, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectsync.MaxEntryBytes", "internal/app/cloudops.projectSyncCloudBodyLimit"},
		},
		{
			Name: "projectsync.mirrored", Class: Evidence, Unit: Rows,
			Limit: 512, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person,
			Sources: []string{"internal/domain/projectsync.MaxMirrorRecords"},
		},
		{
			Name: "projectsync.clones", Class: Buffer, Unit: Rows,
			Limit: 2, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projectsync.MaxCloneJobs"},
		},
		{
			Name: "icons.side", Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/icon.MaxIconSide"},
		},
		{
			Name: "icons.request_bytes", Class: Buffer, Unit: Bytes,
			Limit: 96 << 10, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/icon.MaxIconRequestBytes"},
		},

		{
			// Where each project can be opened: one reading per working
			// directory, held as the Swift app's SessionLinksCache holds it.
			// A reading costs a `git remote` and a handful of file reads, and
			// the Swift app measured five consecutive walks at 319-390 ms
			// with nothing holding them. Past the limit the directory read
			// longest ago is let go; a miss walks it again, which is why this
			// is a cache and not evidence.
			Name: CacheSessionLinks, Class: Cache, Unit: Rows,
			Limit: 64, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
		},
		{
			// One title write. The body holds one short string; anything larger
			// is refused before it is decoded or copied into the settings file.
			Name: SessionTitleRequestBytes, Class: Buffer, Unit: Bytes,
			Limit: 16 << 10, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
		},
		{
			// A session title is one visible line. Control characters and runs of
			// whitespace are normalized before this character count is applied.
			Name: SessionTitleCharacters, Class: Buffer, Unit: Characters,
			Limit: 200, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
		},
		{
			// Manual session names are user input. The oldest row gives way only
			// after the newer write is admitted, as the retired app did.
			Name: SessionTitleRows, Class: UserInput, Unit: Rows,
			Limit: 200, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
		},
		{
			// A terminal can outlive many conversations. A manual title not seen
			// for ninety days gives way so it cannot name a later occupant.
			Name: SessionTitleAge, Class: UserInput, Unit: Seconds,
			Limit: 90 * 24 * 60 * 60, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
		},
		{
			// One spoken sentence admitted by /v1/intents. A larger body is
			// refused before a model is asked to read it.
			Name: IntentRequestBytes, Class: Buffer, Unit: Bytes,
			Limit: 4 << 10, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.intentLimit",
				"internal/app/cloudops.intentTextLimit"},
		},
		{
			// One planner turn running and one waiting. A third is refused
			// with 429 busy and can try again after the line moves.
			Name: IntentPlannerQueue, Class: Buffer, Unit: Rows,
			Limit: 2, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/http.intentQueueLimit"},
		},
		{
			// A CLI turn past this deadline is stopped and gives its queue
			// slot back. The sender receives plan_failed.
			Name: IntentPlannerSeconds, Class: Buffer, Unit: Seconds,
			Limit: 30, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/http.intentTimeLimit"},
		},
		{
			// The hosted console may be the one admitted waiter: 60 seconds
			// behind the active turn, then 60 for its own two CLI attempts,
			// with ten seconds for relay delivery and refusal handling.
			Name: IntentCloudWaitSeconds, Class: Buffer, Unit: Seconds,
			Limit: 130, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/http.intentCloudWaitLimit"},
		},
		{
			// The unprivileged Linux deploy keeps the previous release selected
			// until the daemon and its console prove the new commit. A full wait
			// restores that previous selector and restarts it.
			Name: DeployHealthSeconds, Class: Buffer, Unit: Seconds,
			Limit: 30, AtLimit: Refuse,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/domain/capacity.DeployHealthLimit"},
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
