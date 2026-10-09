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
	Bytes       Unit = "bytes"
	Characters  Unit = "characters"
	Rows        Unit = "rows"
	Multipliers Unit = "multipliers"
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
	AuditSecurity      = "audit.security"
	StoreDB            = "store.db"
	SessionExecutions  = "session.executions"
	SessionNoMovement  = "session.no_movement_seconds"
	BoardReceipts      = "board.receipts"
	CloudRelayQueue    = "cloud.relay_queue"
	StoreReceipts      = "store.receipts"
	CloudPeerPairs     = "cloud.peer_pairs"
	CloudPeerGrants    = "cloud.peer_grants"
	CloudPeerInbox     = "cloud.peer_inbox"
	CloudPeerInboxPage = "cloud.peer_inbox_page"
	CloudPeerOutbox    = "cloud.peer_outbox"
	CloudPeerIngress   = "cloud.peer_ingress"
	CloudPeerBody      = "cloud.peer_body_bytes"
	CloudPeerFrame     = "cloud.peer_frame_bytes"
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
	// The drops the byte cap removed while they were young: the one thing
	// about that cache a person is pushed (limits N16).
	ArtifactsDropsYoung = "artifacts.drops_young"
	// W5: the coordination plane (design-decisions §6 W5).
	LeasesQueue             = "leases.queue"
	CoordinatorAliases      = "coordinator.aliases"
	CoordinatorBindAttempts = "coordinator.bind_attempts"
	WaitsOpen               = "waits.open"
	// T3: the board and the Backlog.
	WorkOpen                  = "work.open"
	WorkListPageRows          = "work.list_page_rows"
	WorkPlanning              = "work.planning"
	WorkAssignments           = "work.assignments"
	WorkDocumentsPerItem      = "work.documents_per_item"
	WorkImagesPerItem         = "work.images_per_item"
	WorkImageBytes            = "work.image_bytes"
	WorkImageBytesPerItem     = "work.image_bytes_per_item"
	WorkImageBytesTotal       = "work.image_bytes_total"
	WorkImageRequestBodyBytes = "work.image_request_body_bytes"
	WorkStepsPerItem          = "work.steps_per_item"
	WorkRootLandingsPerItem   = "work.root_landings_per_item"
	SessionDirectTodos        = "session.direct_todos"
	// The token ledger's work-unit cursors (docs/token-ledger.md "One unit of
	// work", limits N73).
	UsageWorkCursorRows       = "usage.work_cursor_rows"
	UsageWorkCursorQueue      = "usage.work_cursor_queue"
	UsageWorkUnitsInAnswer    = "usage.work_units_per_answer"
	HumanInterventionsOpen    = "session.human_interventions_open"
	HumanInterventionsTotal   = "session.human_interventions_total"
	HumanInterventionsRecent  = "session.human_interventions_recent"
	HumanInterventionTitle    = "session.human_intervention_title_bytes"
	HumanInterventionText     = "session.human_intervention_text_bytes"
	HumanInterventionDetail   = "session.human_intervention_detail_bytes"
	HumanInterventionDocument = "session.human_intervention_document_bytes"
	HumanInterventionDraft    = "session.human_intervention_draft_bytes"
	SettingsRequestBodyBytes  = "settings.request_body_bytes"
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
	RunClaimedItems           = "run.claimed_items"
	EpicChildItems            = "epic.child_items"
	PlanReviewBlockingListed  = "planreview.blocking_listed"
	WorkRequestBodyBytes      = "work.request_body_bytes"
	// Planning and verification gates use the exact stable names approved by
	// the Epic plan. Unlike older rows, these names are one underscore-delimited
	// protocol word because typed refusals and export manifests carry them
	// verbatim.
	WorkGateRoundDetailsPerItem       = "work_gate_round_details_per_item"
	WorkGateRoundDetailsPerStore      = "work_gate_round_details_per_store"
	WorkGateTasksPerRound             = "work_gate_tasks_per_round"
	WorkGateClaimsPerRound            = "work_gate_claims_per_round"
	WorkGateEvidenceStringsPerClaim   = "work_gate_evidence_strings_per_claim"
	WorkGateEvidenceStringBytes       = "work_gate_evidence_string_bytes"
	WorkGateResultBytes               = "work_gate_result_bytes"
	WorkGateEvidenceArtifactsPerTask  = "work_gate_evidence_artifacts_per_task"
	WorkGateEvidenceArtifactBytes     = "work_gate_evidence_artifact_bytes"
	WorkGateEvidenceTotalBytesPerTask = "work_gate_evidence_total_bytes_per_task"
	WorkGateRecentRoundsPerItemRead   = "work_gate_recent_rounds_per_item_read"
	WorkGateDueRowsPerPass            = "work_gate_due_rows_per_pass"
	WorkGateRetryBackoffSeconds       = "work_gate_retry_backoff_seconds"
	WorkGateOwnerOfflineGraceSeconds  = "work_gate_owner_offline_grace_seconds"
	// G6: a Root Assignment or handoff whose opening never finished.
	OpeningStuckSeconds = "orchestrator.opening_stuck_seconds"
	// Whether this machine trails the cloud's latest build (docs/updates.md).
	UpdateRefreshSeconds      = "update.refresh_seconds"
	UpdateFetchTimeoutSeconds = "update.fetch_timeout_seconds"
	UpdateBuildBodyBytes      = "update.build_body_bytes"
	// A release's signed manifest and its signature list (docs/releasing.md).
	ReleaseManifestBytes  = "release.manifest_bytes"
	ReleaseSignatureBytes = "release.signature_bytes"
	// A release install updating itself (docs/updates.md).
	ReleaseCheckIntervalSeconds   = "release.check_interval_seconds"
	ReleaseCheckJitterSeconds     = "release.check_jitter_seconds"
	ReleaseFetchTimeoutSeconds    = "release.fetch_timeout_seconds"
	ReleaseDownloadTimeoutSeconds = "release.download_timeout_seconds"
	ReleaseArtifactBytes          = "release.artifact_bytes"
	ReleaseListBytes              = "release.list_bytes"
	ReleaseArchiveEntries         = "release.archive_entries"
	ReleaseUnpackedBytes          = "release.unpacked_bytes"
	ReleaseSmokeTimeoutSeconds    = "release.smoke_timeout_seconds"
	ReleaseHealthWaitSeconds      = "release.health_wait_seconds"
	ReleasePendingDeadlineSeconds = "release.pending_deadline_seconds"
	ReleaseBootAttempts           = "release.boot_attempts"
	ReleaseSupervisorRuns         = "release.supervisor_runs"
	ReleaseLockStaleSeconds       = "release.lock_stale_seconds"
	ReleaseBackupsKept            = "release.backups_kept"
	ReleasePreviousKept           = "release.previous_releases_kept"
	ReleaseFailedVersions         = "release.failed_versions"
	ReleaseStateFileBytes         = "release.state_file_bytes"
	ReleaseApplyFollowSeconds     = "release.apply_follow_seconds"
	ReleaseAppSwapPollSeconds     = "release.app_swap_poll_seconds"
	ReleaseAutoApplyRetrySeconds  = "release.auto_apply_retry_seconds"
	UpdateApplyBodyBytes          = "update.apply_body_bytes"
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
	ScreensCaptureSlots = "screens.capture_slots"
	// The store's read-only connections for reads made outside a write.
	StoreReadConnections  = "store.read_connections"
	CacheTerminalScreens  = "cache.terminal_screens"
	CacheSessionInventory = "cache.session_inventory"
	// An iTerm2 that stops answering Apple Events writes its own diagnosis:
	// the failures held for it, the files kept, how often one is written,
	// and what each of its steps may take (docs/limits.md N64).
	ITermStallFailures     = "iterm.stall_failures"
	ITermStallSaidBytes    = "iterm.stall_said_bytes"
	ITermStallDiagnoses    = "iterm.stall_diagnoses"
	ITermStallCooldown     = "iterm.stall_cooldown_seconds"
	ITermStallSectionBytes = "iterm.stall_section_bytes"
	ITermStallStepSeconds  = "iterm.stall_step_seconds"
	// How old one source's own answer may be and still vouch for its rows
	// while a slower source holds the refresh.
	CacheSourceAnswer = "cache.source_answer"
	// How long after a scan finished it may still answer a pinned read's
	// execution check instead of the read waiting for a scan of its own.
	CachePinnedRead = "cache.pinned_read"
	// The Git panel's memory of directories git said hold no repository:
	// how long one is believed, and how many are remembered.
	CacheGitNotRepo     = "cache.git_not_repo"
	CacheGitNotRepoRows = "cache.git_not_repo_rows"
	// Minimum age before the machine dashboard refreshes grouped reclaim rows.
	CacheReclaimSummary = "cache.reclaim_summary"
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
	// A Project's shared memory (docs/project-memory.md): how many entries
	// one Project holds, and how large one entry, its description and its
	// name may be.
	MemoryEntries          = "memory.entries"
	MemoryEntryBytes       = "memory.entry_bytes"
	MemoryDescriptionBytes = "memory.description_bytes"
	MemoryNameBytes        = "memory.name_bytes"
	// How many groups one Project's memory holds, and the length of the
	// one line that says when to read each.
	MemoryGroups                = "memory.groups"
	MemoryGroupDescriptionBytes = "memory.group_description_bytes"
	// Which conversations were open in this boot and the one before it, so
	// they can be offered back after a reboot (docs/session-restore.md).
	SessionsRestoreRows    = "sessions.restore_rows"
	SessionsRestoreBoots   = "sessions.restore_boots"
	SessionsRestoreBatch   = "sessions.restore_batch"
	SessionsRestoreSeenAge = "sessions.restore_seen_age"
	SessionsRestoreBeat    = "sessions.restore_heartbeat"
	SessionsRestoreGrace   = "sessions.restore_grace"
	// The Sessions a person archived: closed, recorded, and resumable again
	// (docs/session-archive.md).
	SessionsArchiveRows  = "sessions.archive_rows"
	SessionsArchiveBatch = "sessions.archive_restore_batch"
	// Provider-native work under a session: how many rows the fleet carries,
	// and the immutable metadata/changing-tail cursors held to make a one-second
	// reading cheap.
	SessionsAgentRows     = "sessions.agent_rows"
	CacheBackgroundAgents = "cache.background_agents"
	// SessionsShellOutputBytes is the most of one background command's
	// output GET /v1/sessions/{id}/shells/{shell} sends.
	SessionsShellOutputBytes = "sessions.shell_output_bytes"
	// The sentence-to-draft planner: admitted bytes, queued turns and the
	// longest one turn may hold its queue slot.
	IntentRequestBytes     = "intent.request_bytes"
	IntentPlannerQueue     = "intent.planner_queue"
	IntentPlannerSeconds   = "intent.planner_seconds"
	IntentCloudWaitSeconds = "intent.cloud_wait_seconds"
	IntentStderrBytes      = "intent.stderr_bytes"
	// What one smart-title turn reads of a session: the resolved opening
	// request, the newest later requests that fit, and the latest reply.
	NamingContextBytes = "naming.context_bytes"
	NamingTailEntries  = "naming.tail_entries"
	// What one explicitly requested Board-role classification turn reads: the
	// item kind, title and description plus the closed persona catalog.
	PersonaSuggestionContextBytes = "personas.suggestion_context_bytes"
	// A release keeps the previous selector until the restarted daemon and
	// its console prove the new commit. At this deadline it rolls back.
	DeployHealthSeconds = "deploy.health_seconds"
	// The built-in session personas: how many the catalog holds, and the
	// most one persona's injected text may be (docs/personas.md).
	PersonasCatalog   = "personas.catalog"
	PersonasTextBytes = "personas.text_bytes"
	// Versioned squad definitions and private settings.
	SquadEntities                = "squad.entities"
	SquadSettingsRows            = "squad.settings_rows"
	SquadReceipts                = "squad.receipts"
	SquadBodyBytes               = "squad.body_bytes"
	SquadRequestBytes            = "squad.request_bytes"
	SquadPlaceLookup             = "squad.place_lookup"
	SkillSourceSummaryRunes      = "squad.skill_source_summary_runes"
	SquadLaunchSkillWhenRunes    = "squad.launch_skill_when_runes"
	SquadPackageArchiveBytes     = "squadpackage.archive_bytes"
	SquadPackageRequestBytes     = "squadpackage.request_bytes"
	SquadPackageManifestBytes    = "squadpackage.manifest_bytes"
	SquadPackageFileBytes        = "squadpackage.file_bytes"
	SquadPackageExpandedBytes    = "squadpackage.expanded_bytes"
	SquadPackageEntries          = "squadpackage.entries"
	SquadPackageExpansionRatio   = "squadpackage.expansion_ratio"
	SquadPackagePreviewRows      = "squadpackage.preview_rows"
	SquadPackagePreviewAge       = "squadpackage.preview_age"
	SquadSnapshotBytes           = "squad.snapshot_bytes"
	SquadConsoleActiveSkillBytes = "squad.console_active_skill_bytes"
	SquadRecoveryRows            = "squad.recovery_page_rows"
	SquadEventIDBytes            = "squad.event_id_bytes"
	SquadEventPageRows           = "squad.event_page_rows"
	SquadEventBodyBytes          = "squad.event_body_bytes"
	// The ordinary shells this machine holds open for a person, and what one
	// request may type into one or read back from it (limits N59).
	TerminalCount                     = "terminal.count"
	TerminalInputBytes                = "terminal.input_bytes"
	TerminalPasteBytes                = "terminal.paste_bytes"
	TerminalHistoryLines              = "terminal.history_lines"
	TerminalLane                      = "terminal.lane"
	TerminalViewers                   = "terminal.viewers"
	TerminalStreams                   = "terminal.streams"
	TerminalLeaseSeconds              = "terminal.lease_seconds"
	TerminalGrantsBytes               = "terminal.grants_bytes"
	CloudTerminalRosterRefresh        = "cloud.terminal_roster_refresh_seconds"
	CloudTerminalRosterDeadline       = "cloud.terminal_roster_deadline_seconds"
	CloudTerminalRosterRetry          = "cloud.terminal_roster_retry_seconds"
	CloudTerminalUnverifiedRetire     = "cloud.terminal_unverified_retire_seconds"
	CloudTerminalConnections          = "cloud.terminal_connections"
	CloudTerminalViewerConnections    = "cloud.terminal_viewer_connections"
	CloudTerminalRequestBytes         = "cloud.terminal_request_bytes"
	CloudTerminalReceipts             = "cloud.terminal_receipts"
	CloudTerminalKeySeconds           = "cloud.terminal_key_seconds"
	CloudTerminalIngress              = "cloud.terminal_ingress"
	CloudTerminalListIngress          = "cloud.terminal_list_ingress"
	CloudTerminalRefusals             = "cloud.terminal_refusals"
	CloudTerminalRevocationRetire     = "cloud.terminal_revocation_retire_seconds"
	CloudTerminalFrameHeartbeat       = "cloud.terminal_frame_heartbeat_seconds"
	CloudTerminalEarlyFrames          = "cloud.terminal_early_frames"
	CloudTerminalObservationRows      = "cloud.terminal_observation_rows"
	CloudTerminalListRetry            = "cloud.terminal_list_retries"
	CloudHeaderReadDiagnosticRows     = "cloud.header_read_diagnostic_rows"
	CloudHeaderReadDiagnosticAge      = "cloud.header_read_diagnostic_seconds"
	CloudTerminalUnconfirmed          = "cloud.terminal_unconfirmed_seconds"
	CloudTerminalHistoryReceipt       = "cloud.terminal_history_receipt_bytes"
	CloudTerminalHistoryLine          = "cloud.terminal_history_line_bytes"
	CloudTerminalHistoryCapture       = "cloud.terminal_history_capture_bytes"
	CloudTerminalDirectPeers          = "cloud.terminal_direct_peers"
	CloudTerminalDirectOffers         = "cloud.terminal_direct_offers_per_minute"
	CloudTerminalDirectSDP            = "cloud.terminal_direct_sdp_bytes"
	CloudTerminalDirectCandidates     = "cloud.terminal_direct_candidates"
	CloudTerminalDirectNegotiate      = "cloud.terminal_direct_negotiate_seconds"
	CloudTerminalDirectGather         = "cloud.terminal_direct_gather_seconds"
	CloudTerminalDirectMessage        = "cloud.terminal_direct_message_bytes"
	CloudTerminalDirectChunk          = "cloud.terminal_direct_chunk_seconds"
	CloudTerminalDirectAck            = "cloud.terminal_direct_ack_seconds"
	CloudTerminalDirectProbe          = "cloud.terminal_direct_probe_seconds"
	CloudTerminalDirectProbeUnsettled = "cloud.terminal_direct_probe_unsettled_seconds"
	CloudTerminalSweep                = "cloud.terminal_sweep_seconds"
	CloudTerminalReceiptBusyRetries   = "cloud.terminal_receipt_busy_retries"
	CloudTerminalReceiptBusyRetry     = "cloud.terminal_receipt_busy_retry_seconds"
	CloudTerminalReceiptSeconds       = "cloud.terminal_receipt_seconds"
	TerminalBodyBytes                 = "terminal.body_bytes"
	// What a new tab or pane is typed to start an assistant, and the scripts
	// that hold a line too long to type.
	TerminalLaunchLineBytes = "terminal.launch_line_bytes"
	TerminalLaunchScripts   = "terminal.launch_scripts"
	// The further Board items one dispatch carries beside its work_id.
	DispatchAlsoWorkIDs = "dispatch.also_work_ids"
	// A Session's own close, asked during its turn and carried out when the
	// turn ends: how many may wait at once, how many per terminal, and how
	// long one waits for its turn to end.
	SessionCloseScheduled            = "session.close_scheduled"
	SessionCloseScheduledPerTerminal = "session.close_scheduled_per_terminal"
	SessionCloseScheduledSeconds     = "session.close_scheduled_seconds"
	// What a reassigned in-flight item's handoff pack copies out of the
	// previous owner's worktrees.
	HandoffWorktrees  = "handoff.worktrees"
	HandoffPatchBytes = "handoff.patch_bytes"
	HandoffPackBytes  = "handoff.pack_bytes"
	// A callback: a command the daemon runs for a root and reports on when
	// it exits (orchestrator/callback.go).
	CallbackArgs         = "callback.args"
	CallbackCommandBytes = "callback.command_bytes"
	CallbacksPerRoot     = "callback.per_root"
	CallbacksPerMachine  = "callback.per_machine"
	CallbackOutputBytes  = "callback.output_bytes"
	CallbackTailBytes    = "callback.tail_bytes"
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
	// QuietRecovery says the row's coming back to ok is only written down,
	// never announced: a row whose every notice is about something that
	// already happened has nothing to say when a day passes without it, and
	// an announcement would spend the day's notice the next time it does.
	QuietRecovery bool
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

// namePattern is `area.thing`, lower case. The work-gate plan deliberately
// approved exact underscore-delimited protocol names, kept as a narrow second
// form rather than weakening every row's convention.
var (
	namePattern     = regexp.MustCompile(`^[a-z]+(\.[a-z_]+)+$`)
	gateNamePattern = regexp.MustCompile(`^work_gate_[a-z0-9_]+$`)
)

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
			// One durable identity per terminal id on this machine. Full
			// refuses a newly observed id; no generation is silently evicted.
			Name: SessionExecutions, Class: Evidence, Unit: Rows,
			Limit: 4096, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Health, Sender, Log}, EvictedBy: Person,
			Sources: []string{"internal/adapters/store.ExecutionRecordsLimit"},
		},
		{
			// A current record-movement observation stops being recent after
			// this age. The status publisher marks no_movement and tells viewers
			// the threshold; an unknown reading never becomes a quiet one.
			Name: SessionNoMovement, Class: Cache, Unit: Seconds,
			Limit: 1800, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.SessionNoMovementSecondsLimit"},
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
			// Locally pinned machine-to-machine pairs are authorization evidence.
			// No account roster fallback or automatic eviction may free a slot.
			Name: CloudPeerPairs, Class: Evidence, Unit: Rows,
			Limit: 128, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/peerstore.PairLimit"},
		},
		{
			// A target's exact Session scope grants remain until a person removes
			// them; expiry and revocation deny without erasing the evidence.
			Name: CloudPeerGrants, Class: Evidence, Unit: Rows,
			Limit: 512, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/peerstore.GrantLimit"},
		},
		{
			// A received Agent message or handoff is retained as evidence.
			Name: CloudPeerInbox, Class: Evidence, Unit: Rows,
			Limit: 512, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/peerstore.InboxLimit"},
		},
		{
			// One encrypted content read carries one message and its receipt.
			Name: CloudPeerInboxPage, Class: Buffer, Unit: Rows,
			Limit: 1, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/peerstore.InboxPageLimit"},
		},
		{
			Name: CloudPeerBody, Class: Buffer, Unit: Bytes,
			Limit: 512 << 10, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/domain/agenthandoff.MaxBodyBytes"},
		},
		{
			Name: CloudPeerFrame, Class: Buffer, Unit: Bytes,
			Limit: 1 << 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Log, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/cloud.PeerFrameBytesLimit"},
		},
		{
			// A source keeps the request and relay evidence for later checks.
			Name: CloudPeerOutbox, Class: Evidence, Unit: Rows,
			Limit: 512, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/peerstore.OutboxLimit"},
		},
		{
			// Peer frames wait off the socket reader for fresh authorization.
			Name: CloudPeerIngress, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Log, Sender},
			EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.PeerIngressLimit",
				"internal/transport/cloud.PeerAckIngressLimit",
				"internal/transport/cloud.(*Link).runOnce:chan(PeerIngressLimit)",
				"internal/transport/cloud.(*Link).runOnce:chan(PeerAckIngressLimit)"},
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
			// The tail of one background command's output the Shell panel is
			// sent (limits N50). The file is Claude Code's and stays whole:
			// past this only its oldest bytes are left out of the answer,
			// which says `truncated`. Asked of a request, never retained.
			Name: SessionsShellOutputBytes, Class: Observation, Unit: Bytes,
			Limit: 1 << 20, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/transcript.MaxShellOutput"},
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
			// already typed into a prompt as a path (limits N16). The bytes
			// are what limits the cache: files past a week go on their own
			// (the reading's window), except the newest forty, and past this
			// many bytes the oldest go whatever their age. So this row reads
			// full only when the cap is what is removing pictures — which is
			// said in diagnostics and the log, and pushed only when the file
			// it removed was young (artifacts.drops_young).
			Name: ArtifactsDrops, Class: UserInput, Unit: Bytes,
			Limit: 256 << 20, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
			Sources: []string{"internal/adapters/artifacts.MaxDropsBytes",
				"internal/adapters/artifacts.DropsAgeLimit"},
		},
		{
			// Pictures the byte cap of artifacts.drops removed while they
			// were less than a day old, within the last day. Each was typed
			// into a prompt recently enough to be read again after a
			// compaction or a resume, so one is the cache too small for how
			// it is used, and it is pushed; a day without one is written
			// down and not announced. The limit is one: any at all.
			Name: ArtifactsDropsYoung, Class: UserInput, Unit: Rows,
			Limit: 1, AtLimit: EvictOldest,
			Told:          []Channel{Diagnostics, Notice, Log},
			EvictedBy:     Daemon,
			QuietRecovery: true,
			Sources:       []string{"internal/adapters/artifacts.DropsYoungLimit"},
		},
		{
			// The askers waiting for one lease — the compile slot, or one
			// checkout's landing (D06 ②, D20). At the limit a new asker is
			// refused with 429 queue_full and a retry_after, in the answer
			// to its own ask; nobody already in line is let go. A waiter that
			// stops asking is passed over at once and forgotten after half an
			// hour. Used is the longest line.
			Name: LeasesQueue, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
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
			// A bind retries a transient unknown inventory reading a few times,
			// then refuses to replace the old holder until liveness is proved.
			Name: CoordinatorBindAttempts, Class: Buffer, Unit: Rows,
			Limit: 6, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"cmd/clawdline.coordinatorBindAttemptLimit"},
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
			// One page of the current Board. The page is an observation, not
			// retained data: rows after this one stay in the store and the
			// answer names the keyset cursor that reads them next. Keeping the
			// first page small also bounds the documents, images, steps and
			// active-claim reads needed before the Console can paint.
			Name: WorkListPageRows, Class: Observation, Unit: Rows,
			Limit: 24, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.WorkV2ListPageLimit"},
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
		// Every document but the item's one completion_report, which always
		// has its own place on top of these: an item holds at most 32+1.
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
			// The broker's root landing rows one Board item keeps, and the
			// legacy landing copies its read lists (internal/adapters/store/root_landings.go).
			Name: WorkRootLandingsPerItem, Class: Evidence, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/store.WorkV2RootLandingLimit"},
		},
		{
			Name: SessionDirectTodos, Class: Evidence, Unit: Rows,
			Limit: 500, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/store.DirectTodoV2Limit"},
		},
		{
			Name: HumanInterventionsOpen, Class: Evidence, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person,
			Sources: []string{"internal/adapters/store.HumanInterventionsOpenLimit"},
		},
		{
			Name: HumanInterventionsTotal, Class: Progress, Unit: Rows,
			Limit: 2000, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/store.HumanInterventionsTotalLimit"},
		},
		{
			// The work-unit cursors (store/work_cursors.go): a journal of
			// readings, the oldest let go past the limit in the write that
			// adds one. The ledger's own rows keep every session's
			// cumulative totals; what goes is how an old unit was split.
			Name: UsageWorkCursorRows, Class: Journal, Unit: Rows,
			Limit: 50_000, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/store.WorkCursorRowLimit"},
		},
		{
			// Edges posted by the broker and the Board, waiting for the
			// cursor worker. Past the limit an edge is not read: it is
			// recorded cursor_missing, which every report shows.
			Name: UsageWorkCursorQueue, Class: Buffer, Unit: Rows,
			Limit: 1024, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Log}, EvictedBy: Daemon,
			Sources: []string{"internal/app.workCursorQueueLimit"},
		},
		{
			// Units one GET /v1/usage/work-units reads, the most recent
			// first; the answer says truncated past it.
			Name: UsageWorkUnitsInAnswer, Class: Observation, Unit: Rows,
			Limit: 500, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.workUnitAnswerLimit"},
		},
		{
			Name: HumanInterventionsRecent, Class: Cache, Unit: Rows,
			Limit: 5, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/store.HumanInterventionsRecentLimit"},
		},
		{
			Name: HumanInterventionTitle, Class: Buffer, Unit: Bytes,
			Limit: 120, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.humanInterventionTitleLimit"},
		},
		{
			Name: HumanInterventionText, Class: Buffer, Unit: Bytes,
			Limit: 500, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.humanInterventionTextLimit"},
		},
		{
			Name: HumanInterventionDetail, Class: Buffer, Unit: Bytes,
			Limit: 16 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.humanInterventionDetailLimit"},
		},
		{
			Name: HumanInterventionDocument, Class: Buffer, Unit: Bytes,
			Limit: 2 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.humanInterventionDocumentLimit"},
		},
		{
			Name: HumanInterventionDraft, Class: Buffer, Unit: Bytes,
			Limit: 2 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.humanInterventionDraftLimit"},
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
			Sources: []string{"internal/app.workV2DescriptionLimit", "cmd/clawdline.itemTextLimit"},
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
			// The Board items one person's message may have a Session claim
			// on it, or assign to a new Session on it, mirroring
			// run.created_items. Counted over every assignment made on that
			// run, whatever its state; the sixth is refused
			// run_claims_exhausted and nothing is written.
			Name: RunClaimedItems, Class: Buffer, Unit: Rows,
			Limit: 5, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.runClaimLimit"},
		},
		{
			// The child items one Epic's owner Session may break it into
			// (work-system-v2 §6.5). Counted over every child of that Epic,
			// open or closed; the thirty-third is refused epic_children_full
			// and nothing is written.
			Name: EpicChildItems, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/work.EpicChildLimit"},
		},
		{
			// The blocking findings a planning-gate refusal names
			// (*_plan_review_blocking). The rest are counted, "and N more";
			// the whole receipt stays on the review task. The findings past
			// the limit coalesce into that one count.
			Name: PlanReviewBlockingListed, Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Coalesce,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/work.PlanReviewBlockingListLimit"},
		},
		{
			Name: WorkRequestBodyBytes, Class: Buffer, Unit: Bytes,
			Limit: 96 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.workV2BodyLimit", "internal/app/cloudops.workV2CloudBodyLimit",
				"cmd/clawdline.todoInputLimit"},
		},
		{
			// Work v2 owns the detailed rounds retained for one item. At the
			// limit a new round is refused as verification_rounds_full until a
			// person exports eligible closed detail and confirms its digest-
			// bound purge. No detail is silently evicted.
			Name: WorkGateRoundDetailsPerItem, Class: Evidence, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Notice, Health}, EvictedBy: Person, Projects: true,
			Sources: []string{"internal/contract.WorkGateRoundDetailsPerItemLimit"},
		},
		{
			// The Work v2 store owns the global retained-round population. It
			// has the same explicit export-and-confirmed-purge recovery as the
			// per-item row and never removes the oldest row automatically.
			Name: WorkGateRoundDetailsPerStore, Class: Evidence, Unit: Rows,
			Limit: 10_000, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Notice, Health}, EvictedBy: Person, Projects: true,
			Sources: []string{"internal/contract.WorkGateRoundDetailsPerStoreLimit"},
		},
		{
			// The coordinator and broker own one initial gate task and one
			// bounded retry. A third protocol-lineage record is refused and the
			// round becomes a technical escalation.
			Name: WorkGateTasksPerRound, Class: Buffer, Unit: Rows,
			Limit: 2, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateTasksPerRoundLimit"},
		},
		{
			// The broker owns checker-result admission. An oversized claim set
			// is a typed technical failure and no partial result is retained.
			Name: WorkGateClaimsPerRound, Class: Buffer, Unit: Rows,
			Limit: 32, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateClaimsPerRoundLimit"},
		},
		{
			Name: WorkGateEvidenceStringsPerClaim, Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateEvidenceStringsPerClaimLimit"},
		},
		{
			Name: WorkGateEvidenceStringBytes, Class: Buffer, Unit: Bytes,
			Limit: 500, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateEvidenceStringBytesLimit"},
		},
		{
			// Gate-result HTTP admission rejects a body before persistence once
			// it exceeds this byte ceiling.
			Name: WorkGateResultBytes, Class: Buffer, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateResultBytesLimit"},
		},
		{
			// The broker owns streamed artifact admission. Count and byte
			// ceilings reject the incoming upload before it changes durable
			// task evidence; eligible round export/purge is separate recovery.
			Name: WorkGateEvidenceArtifactsPerTask, Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateEvidenceArtifactsPerTaskLimit"},
		},
		{
			Name: WorkGateEvidenceArtifactBytes, Class: Buffer, Unit: Bytes,
			Limit: 2 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateEvidenceArtifactBytesLimit"},
		},
		{
			Name: WorkGateEvidenceTotalBytesPerTask, Class: Buffer, Unit: Bytes,
			Limit: 8 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateEvidenceTotalBytesPerTaskLimit"},
		},
		{
			// A single-item Work v2 read owns this newest-first observation.
			// Older detail stays in the store and aggregates/latest remain in
			// the answer, which says the detail list was truncated.
			Name: WorkGateRecentRoundsPerItemRead, Class: Observation, Unit: Rows,
			Limit: 10, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateRecentRoundsPerItemReadLimit"},
		},
		{
			// The coordinator owns this due-row observation. Rows beyond one
			// pass remain queued and the next supervised pass resumes from a
			// stable cursor; none is removed from durable work.
			Name: WorkGateDueRowsPerPass, Class: Observation, Unit: Rows,
			Limit: 20, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateDueRowsPerPassLimit"},
		},
		{
			// The coordinator owns provider/capacity retry timing. At this age
			// the delay expires and the row becomes due; a provider's larger
			// requested delay is capped here rather than extending forever.
			Name: WorkGateRetryBackoffSeconds, Class: Cache, Unit: Seconds,
			Limit: 300, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateRetryBackoffSecondsLimit"},
		},
		{
			// The coordinator owns the live-parent observation. Once this
			// observation is stale, authority is atomically promoted to the
			// person and one attention notification is owed.
			Name: WorkGateOwnerOfflineGraceSeconds, Class: Observation, Unit: Seconds,
			Limit: 900, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics, Notice}, EvictedBy: Daemon,
			Sources: []string{"internal/contract.WorkGateOwnerOfflineGraceSecondsLimit"},
		},
		{
			// A Root Assignment at terminal_opened, or a handoff at opening,
			// lasts only while its opening request runs, which touches the
			// row at least every 150 seconds. One unchanged for this long was
			// left by a daemon that stopped mid-opening: the beat records it
			// failed with terminal_open_timeout or handoff_open_timeout and
			// an event of that name. Nothing is kept waiting at the limit.
			Name: OpeningStuckSeconds, Class: Observation, Unit: Seconds,
			Limit: 1800, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.OpeningStuckSecondsLimit"},
		},
		{
			// The cloud's BUILD.json, as the update check last read it. At
			// this age the background loop reads it again; the old answer is
			// replaced only by a good new one (docs/updates.md).
			Name: UpdateRefreshSeconds, Class: Observation, Unit: Seconds,
			Limit: 1800, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/updatecheck.RefreshSecondsLimit"},
		},
		{
			// One ask of the cloud's BUILD.json. At the limit the ask is
			// abandoned and its error is shown beside the last good answer.
			Name: UpdateFetchTimeoutSeconds, Class: Observation, Unit: Seconds,
			Limit: 10, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/updatecheck.FetchTimeoutSecondsLimit"},
		},
		{
			// One BUILD.json body, from the cloud or the served dist. A
			// longer one is refused as not a BUILD.json.
			Name: UpdateBuildBodyBytes, Class: Buffer, Unit: Bytes,
			Limit: 4096, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/updatecheck.maxBuildBodyBytes"},
		},
		{
			// One release manifest.json. A longer one is refused as
			// manifest_malformed and nothing of it is followed.
			Name: ReleaseManifestBytes, Class: Buffer, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release.maxManifestBytes"},
		},
		{
			// One manifest.sig.json, the list of signatures over the
			// manifest. A longer one is refused as manifest_signature_invalid.
			Name: ReleaseSignatureBytes, Class: Buffer, Unit: Bytes,
			Limit: 16 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release.maxSignatureBytes"},
		},
		{
			// The signed manifest of a release install's channel, as the release
			// check last read it. At this age (plus up to the jitter) it is read
			// again; the old answer is replaced only by a good new one, or dropped
			// when the release is gone (404).
			Name: ReleaseCheckIntervalSeconds, Class: Observation, Unit: Seconds,
			Limit: 6 * 3600, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.CheckIntervalSecondsLimit"},
		},
		{
			// The most added at random to the release check's period, so machines
			// started together do not ask together.
			Name: ReleaseCheckJitterSeconds, Class: Observation, Unit: Seconds,
			Limit: 1800, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.CheckJitterSecondsLimit"},
		},
		{
			// One read of a release manifest, its signatures or the release list. At
			// the limit the read is abandoned and its error shown beside the last
			// good answer.
			Name: ReleaseFetchTimeoutSeconds, Class: Observation, Unit: Seconds,
			Limit: 30, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.FetchTimeoutSecondsLimit"},
		},
		{
			// One artifact's download. At the limit the download is abandoned, the
			// partial file removed, and the update recorded failed with
			// download_failed; the running release is untouched.
			Name: ReleaseDownloadTimeoutSeconds, Class: Buffer, Unit: Seconds,
			Limit: 900, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.DownloadTimeoutSecondsLimit"},
		},
		{
			// One release artifact. A manifest naming a larger one is refused before
			// anything is downloaded.
			Name: ReleaseArtifactBytes, Class: Buffer, Unit: Bytes,
			Limit: 512 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.maxArtifactBytes"},
		},
		{
			// One answer of the GitHub Releases API, read for the beta channel. A
			// longer one is refused as not a release list.
			Name: ReleaseListBytes, Class: Buffer, Unit: Bytes,
			Limit: 1 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.maxReleaseListBytes"},
		},
		{
			// Entries in one release archive. More is refused as archive_unsafe and
			// nothing of it is kept.
			Name: ReleaseArchiveEntries, Class: Buffer, Unit: Rows,
			Limit: 20000, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.maxArchiveEntries"},
		},
		{
			// What one release archive unpacks to. More is refused as archive_unsafe
			// and nothing of it is kept.
			Name: ReleaseUnpackedBytes, Class: Buffer, Unit: Bytes,
			Limit: 2 << 30, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.maxUnpackedBytes"},
		},
		{
			// The new binary's `version --json` before anything switches to it. At
			// the limit the update is recorded failed with smoke_run_failed.
			Name: ReleaseSmokeTimeoutSeconds, Class: Buffer, Unit: Seconds,
			Limit: 15, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.SmokeTimeoutSecondsLimit"},
		},
		{
			// How long the supervisor waits for a restarted daemon to serve its
			// console and name its commit. At the limit the update is rolled back
			// with health_timeout.
			Name: ReleaseHealthWaitSeconds, Class: Buffer, Unit: Seconds,
			Limit: 60, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.HealthWaitSecondsLimit"},
		},
		{
			// How long an update may stay pending. A new release that starts after
			// it gives the update up (boot_guard) and exits for the previous release
			// to start.
			Name: ReleasePendingDeadlineSeconds, Class: Buffer, Unit: Seconds,
			Limit: 600, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.PendingDeadlineSecondsLimit"},
		},
		{
			// Starts of a new release while its update is pending. At the limit its
			// boot guard rolls the update back.
			Name: ReleaseBootAttempts, Class: Buffer, Unit: Rows,
			Limit: 3, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.BootAttemptsLimit"},
		},
		{
			// Starts of the update supervisor for one update. Past the limit it
			// stops trying the new release and rolls back (supervisor_gave_up).
			Name: ReleaseSupervisorRuns, Class: Buffer, Unit: Rows,
			Limit: 5, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.SupervisorRunsLimit"},
		},
		{
			// The age at which an update.lock left by a process that died is taken
			// over; a younger one refuses a second update with update_in_progress.
			Name: ReleaseLockStaleSeconds, Class: Idempotency, Unit: Seconds,
			Limit: 1800, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.LockStaleSecondsLimit"},
		},
		{
			// How often a daemon with a staged app bundle looks whether the app
			// has quit; a running app keeps its bundle until the next look.
			Name: ReleaseAppSwapPollSeconds, Class: Observation, Unit: Seconds,
			Limit: 60, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.AppSwapPollSecondsLimit"},
		},
		{
			// How often an auto-apply held back by a busy session looks again
			// whether the sessions are idle; it stops once the update starts or
			// nothing is left to apply.
			Name: ReleaseAutoApplyRetrySeconds, Class: Observation, Unit: Seconds,
			Limit: 300, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.AutoApplyRetrySecondsLimit"},
		},
		{
			// Store snapshots taken before an update (VACUUM INTO). The oldest is
			// removed when a new one is written.
			Name: ReleaseBackupsKept, Class: Journal, Unit: Rows,
			Limit: 2, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.BackupsKeptLimit"},
		},
		{
			// Unpacked releases kept besides `current` after a healthy update; older
			// ones are removed. A commit-named source deploy is never removed.
			Name: ReleasePreviousKept, Class: Journal, Unit: Rows,
			Limit: 2, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.PreviousReleasesKeptLimit"},
		},
		{
			// Versions that rolled back, which auto-apply does not try again. The
			// oldest is forgotten first.
			Name: ReleaseFailedVersions, Class: Journal, Unit: Rows,
			Limit: 16, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.FailedVersionsLimit"},
		},
		{
			// One of the updater's own files (status, pending, failed, lock). A
			// longer one is reported unreadable, never read as idle.
			Name: ReleaseStateFileBytes, Class: Buffer, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/release/updater.maxStateFileBytes"},
		},
		{
			// How long `clawdline update --apply` follows an update before it says
			// the outcome could not be read and exits 3; the update itself goes on.
			Name: ReleaseApplyFollowSeconds, Class: Buffer, Unit: Seconds,
			Limit: 1500, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"cmd/clawdline.applyFollowSecondsLimit"},
		},
		{
			// One POST /v1/update/apply body. A longer one is refused as
			// bad_request.
			Name: UpdateApplyBodyBytes, Class: Buffer, Unit: Bytes,
			Limit: 4096, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.updateApplyBodyLimit"},
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
			// The store's read-only connections (internal/adapters/store,
			// sqlite.go openReader). Reads made outside a write use them, so
			// they no longer queue behind the one write connection; past the
			// limit a reader waits for one on its own context and is refused
			// when that context ends. Writes never wait for a reader.
			Name: StoreReadConnections, Class: Buffer, Unit: Rows,
			Limit: 4, AtLimit: Refuse,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/store.ReadConnectionsLimit"},
		},
		{
			// The last complete per-terminal-source inventory kept across a
			// failed scan (internal/app/inventory_reading.go). At this age it
			// expires: two minutes is also the iTerm2 listing and held-screen
			// backoff ceiling, while anything older would be a claim about the
			// present rather than a named prior observation.
			Name: CacheSessionInventory, Class: Cache, Unit: Seconds,
			Limit: 120, AtLimit: Expire,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.LastGoodInventoryAgeLimit"},
		},
		{
			// The newest failed iTerm2 osascript runs, held for the next
			// stall diagnosis (internal/adapters/terminal/stall.go). Past the
			// limit the oldest is let go; every one of them is already a line
			// in the log.
			Name: ITermStallFailures, Class: Observation, Unit: Rows,
			Limit: 32, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/terminal.stallFailuresLimit"},
		},
		{
			// The end of what osascript wrote on stderr that one failure line
			// carries. The end is kept: the Apple Event error number is there.
			Name: ITermStallSaidBytes, Class: Observation, Unit: Bytes,
			Limit: 512, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/terminal.saidLimit"},
		},
		{
			// The stall diagnoses kept in CLAWDLINE_NEXT_DIR/logs. After each
			// one is written the oldest past the limit are removed.
			Name: ITermStallDiagnoses, Class: DiagnosticLog, Unit: Rows,
			Limit: 20, AtLimit: Rotate,
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/terminal.stallDiagnosesLimit"},
		},
		{
			// The least time between two stall diagnoses: one per episode,
			// which has run 3 to 23 minutes. A run of timeouts inside it
			// starts nothing; its failures are still logged one by one.
			Name: ITermStallCooldown, Class: Buffer, Unit: Seconds,
			Limit: 1800, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/terminal.stallCooldownLimit"},
		},
		{
			// The most of one step's output a stall diagnosis carries. A
			// two-second sample of iTerm2 measured 245 KB here; past the
			// limit the rest is not written and the section says how much.
			Name: ITermStallSectionBytes, Class: Buffer, Unit: Bytes,
			Limit: 512 << 10, AtLimit: Refuse,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/terminal.stallSectionLimit"},
		},
		{
			// The longest one step of a stall diagnosis may take. A step past
			// it is abandoned and recorded as timed out; the file goes on.
			Name: ITermStallStepSeconds, Class: Buffer, Unit: Seconds,
			Limit: 30, AtLimit: Refuse,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/terminal.stallStepLimit"},
		},
		{
			// What each source said in the scan behind the held reading, and
			// in the one running now (internal/app/inventory_reading.go). A
			// drawing taken while iTerm2 holds the refresh keeps the rows of a
			// source whose answer is complete, lists exactly those rows and is
			// younger than this. At this age the answer expires and that
			// source's rows read unverified, as every source's did before.
			// Thirty seconds is the scan budget, the longest one refresh runs.
			Name: CacheSourceAnswer, Class: Cache, Unit: Seconds,
			Limit: 30, AtLimit: Expire,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.SourceAnswerAgeLimit"},
		},
		{
			// A pinned Cloud read (transcript, info, git, to-dos) checks that
			// the execution it names is still the one running. A scan that
			// finished less than this ago answers that check; an older one
			// is replaced by a scan taken for the read, as every check was
			// before. Three seconds: a scan started now returns rows observed
			// as it started, so this is at most three seconds older than the
			// fresh answer, against the 8.9 s and 13.0 s medians measured for
			// the slow pinned reads that waited for one. Writes never use it.
			Name: CachePinnedRead, Class: Cache, Unit: Seconds,
			Limit: 3, AtLimit: Expire,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.PinnedReadAgeLimit"},
		},
		{
			// A directory git answered "not a repository" for is answered
			// that way for a minute without running git again
			// (internal/adapters/git/notrepo.go). Only that answer is kept.
			Name: CacheGitNotRepo, Class: Cache, Unit: Seconds,
			Limit: 60, AtLimit: Expire,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/git.NotRepoAgeLimit"},
		},
		{
			// How many such directories are remembered. Past it the one
			// remembered longest ago is let go; a miss is one `git status`.
			Name: CacheGitNotRepoRows, Class: Cache, Unit: Rows,
			Limit: 256, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/git.NotRepoRowsLimit"},
		},
		{
			// The machine dashboard polls every three seconds. Grouped reclaim
			// decisions come from the store at most once per thirty seconds.
			Name: CacheReclaimSummary, Class: Cache, Unit: Seconds,
			Limit: 30, AtLimit: Expire,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/http.reclaimSummaryLimit"},
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
			// CLAWDLINE_NEXT_DIR/places.json: explicit project directories and
			// directories deliberately hidden from the lists. Each
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
			// <state dir>/memory/<repo key>/: one Project's shared memory
			// entries. Each is a lesson somebody recorded, so nothing is let
			// go at the limit: one more is refused with 409 memory_full in
			// the answer to the add, and only a person (or a session they
			// asked) forgets one.
			Name: MemoryEntries, Class: Evidence, Unit: Rows,
			Limit: 512, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/memory.EntryCountLimit"},
		},
		{
			// One entry's body. An add or update past it is refused before
			// anything is written; the request body and the file read are
			// the same bound with room for the other fields.
			Name: MemoryEntryBytes, Class: Buffer, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources: []string{"internal/adapters/memory.EntryBodyLimit", "internal/adapters/memory.entryFileReadLimit",
				"internal/transport/http.memoryRequestBodyLimit"},
		},
		{
			// One entry's one-line description: what the index a launched
			// session is given repeats for every entry.
			Name: MemoryDescriptionBytes, Class: Buffer, Unit: Bytes,
			Limit: 512, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/memory.DescriptionByteLimit"},
		},
		{
			// One entry's name, which is also its file name.
			Name: MemoryNameBytes, Class: Buffer, Unit: Bytes,
			Limit: 64, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/memory.nameByteLimit"},
		},
		{
			// <state dir>/memory/<repo key>/GROUPS.json: the groups an
			// entry may be filed under. The index a launch carries names
			// every group, so the bound is what fits in it with room for
			// resident entries; one more is refused with 409 memory_full.
			Name: MemoryGroups, Class: Evidence, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/memory.GroupCountLimit"},
		},
		{
			// One group's "read this when" line, which the index repeats.
			// The groups file read and a group request body are bounded
			// with room for every group at this length.
			Name: MemoryGroupDescriptionBytes, Class: Buffer, Unit: Bytes,
			Limit: 256, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources: []string{"internal/adapters/memory.GroupDescriptionByteLimit", "internal/adapters/memory.groupsFileReadLimit",
				"internal/transport/http.memoryGroupRequestBodyLimit"},
		},
		{
			// Conversations one boot records. They are read again from the
			// machine on every complete reading; past the limit the ones that
			// moved longest ago are not recorded and the count says so.
			Name: SessionsRestoreRows, Class: Observation, Unit: Rows,
			Limit: 200, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.restoreRowsLimit"},
		},
		{
			// This boot and the one before it. An older boot's rows can no
			// longer be offered, so they go with it.
			Name: SessionsRestoreBoots, Class: Journal, Unit: Rows,
			Limit: 2, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.restoreBootsLimit"},
		},
		{
			// Conversations one restore may name. A longer list is refused
			// whole, and nothing is opened.
			Name: SessionsRestoreBatch, Class: Buffer, Unit: Rows,
			Limit: 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.restoreBatchLimit"},
		},
		{
			// How stale a recorded last_seen may grow while the set of open
			// conversations is unchanged; past it the next complete reading
			// writes the same set again with the new time.
			Name: SessionsRestoreSeenAge, Class: Cache, Unit: Seconds,
			Limit: 300, AtLimit: Expire,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.restoreSeenLimit"},
		},
		{
			// How stale the boot's own last_seen may grow while complete
			// readings keep arriving with nothing changed; past it the next
			// one moves last_seen alone, one row update. It is what the
			// grace line after a reboot is drawn from.
			Name: SessionsRestoreBeat, Class: Cache, Unit: Seconds,
			Limit: 60, AtLimit: Expire,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.restoreBeatLimit"},
		},
		{
			// How long before the previous boot was last seen a conversation
			// may have gone and still be offered back: a shutdown's final
			// wave. One that went earlier expires from the offer; its row
			// stays until the boot does.
			Name: SessionsRestoreGrace, Class: Cache, Unit: Seconds,
			Limit: 180, AtLimit: Expire,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.restoreGraceLimit"},
		},
		{
			// Archived conversations kept. Each is a row a person asked for,
			// so past the limit the one archived longest ago is dropped and
			// the count says so; its transcript stays in the assistant's own
			// history, resumable from the place's past list while it is
			// still on it.
			Name: SessionsArchiveRows, Class: Journal, Unit: Rows,
			Limit: 500, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.archiveRowsLimit"},
		},
		{
			// Conversations one archive restore may name. A longer list is
			// refused whole, and nothing is opened.
			Name: SessionsArchiveBatch, Class: Buffer, Unit: Rows,
			Limit: 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.archiveBatchLimit"},
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
			Name: "projectfiles.list", Class: Buffer, Unit: Rows,
			Limit: 128, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projectfiles.MaxFiles"},
		},
		{
			Name: "projectfiles.scan_entries", Class: Buffer, Unit: Rows,
			Limit: 1024, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projectfiles.MaxScanEntries"},
		},
		{
			Name: "projectfiles.tree_path_bytes", Class: Buffer, Unit: Bytes,
			Limit: 4096, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projectfiles.MaxTreePathBytes"},
		},
		{
			Name: "projectfiles.tree_depth", Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projectfiles.MaxTreeDepth"},
		},
		{
			Name: "projectfiles.file_bytes", Class: Buffer, Unit: Bytes,
			Limit: 128 << 10, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projectfiles.MaxFileBytes"},
		},
		{
			Name: "projectfiles.write_bytes", Class: Buffer, Unit: Bytes,
			Limit: 256 << 10, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projectfiles.MaxWriteBytes"},
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
			// One settings change, whether it came from this machine's own
			// console or the narrow two-model Cloud route. Past the bound the
			// request is refused before it is copied into config.json.
			Name: SettingsRequestBodyBytes, Class: Buffer, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.settingsRequestBodyLimit",
				"internal/app/cloudops.defaultModelsCloudBodyLimit"},
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
			// The end of a failed planner or naming turn's stderr, kept so a
			// CLI's usage-limit refusal can be told apart from any other
			// failure. Older bytes give way to newer ones.
			Name: IntentStderrBytes, Class: Observation, Unit: Bytes,
			Limit: 4 << 10, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/planner.stderrLimit"},
		},
		{
			// One smart-title turn reads the session's resolved opening
			// request, the newest later requests that fit, and an excerpt of
			// the latest reply. Older later requests give way first.
			Name: NamingContextBytes, Class: Observation, Unit: Bytes,
			Limit: 12 << 10, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/http.namingContextLimit"},
		},
		{
			// The newest transcript rows a smart-title turn looks through for
			// later requests and the latest reply. Older rows are not read.
			Name: NamingTailEntries, Class: Observation, Unit: Rows,
			Limit: 400, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/http.namingTailLimit"},
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
		{
			// The personas compiled into this daemon (docs/personas.md). The
			// catalog is embedded, so it cannot grow while the daemon runs: a
			// catalog past the limit does not load, and the persona package's
			// own test refuses it before a build ships. Nothing is let go.
			Name: PersonasCatalog, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/domain/persona.MaxPersonas"},
		},
		{
			// One persona's injected text — the precedence preamble, the body
			// and its source line — which a session reads on every turn. A
			// longer one is refused at load in the same way.
			Name: PersonasTextBytes, Class: Buffer, Unit: Bytes,
			Limit: 8 << 10, AtLimit: Refuse,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/domain/persona.MaxPersonaBytes"},
		},
		{
			// One complete immutable AI squad launch document. A larger
			// launch is refused before an intent or snapshot is persisted.
			Name: SquadSnapshotBytes, Class: Evidence, Unit: Bytes,
			Limit: 1 << 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/store.MaxSquadSnapshotBytes"},
		},
		{
			// Console refuses another enabled skill before the complete launch
			// document can approach its larger, authoritative snapshot limit.
			Name: SquadConsoleActiveSkillBytes, Class: Buffer, Unit: Bytes,
			Limit: 512 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Person,
			Sources: []string{"internal/domain/squad.MaxSquadConsoleActiveSkillBytes"},
		},
		{
			// One recovery query reads this many pending launch intents. Later
			// pages continue from a cursor instead of refusing new Sessions.
			Name: SquadRecoveryRows, Class: Buffer, Unit: Rows,
			Limit: 256, AtLimit: Refuse,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/store.MaxSquadRecoveryRows"},
		},
		{
			// An agent's idempotency key for one reported skill event.
			Name: SquadEventIDBytes, Class: Buffer, Unit: Bytes,
			Limit: 128, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/store.MaxSquadEventIDBytes"},
		},
		{
			// One JSON skill-event report from a local session.
			Name: SquadEventBodyBytes, Class: Buffer, Unit: Bytes,
			Limit: 4 << 10, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/http.maxSquadEventBodyBytes"},
		},
		{
			// A durable skill-event cursor page; more events remain for
			// the next read rather than being dropped from the store.
			Name: SquadEventPageRows, Class: Buffer, Unit: Rows,
			Limit: 100, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/store.MaxSquadEventPageRows"},
		},
		{
			// One explicitly requested Board-role classification reads the
			// closed catalog and as much of the item's title and description as
			// fits beside it. The rest of a long description is not sent.
			Name: PersonaSuggestionContextBytes, Class: Observation, Unit: Bytes,
			Limit: 16 << 10, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/http.personaSuggestionContextLimit"},
		},
		{
			Name: SquadEntities, Class: Evidence, Unit: Rows,
			Limit: 1024, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Health, Sender}, EvictedBy: Person,
			Sources: []string{"internal/domain/squad.MaxSquadEntities"},
		},
		{
			Name: SquadSettingsRows, Class: Evidence, Unit: Rows,
			Limit: 10000, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Health, Sender}, EvictedBy: Person,
			Sources: []string{"internal/domain/squad.MaxSquadSettingsRows"},
		},
		{
			Name: SquadReceipts, Class: Idempotency, Unit: Rows,
			Limit: 10000, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Person,
			Sources: []string{"internal/domain/squad.MaxSquadReceipts"},
		},
		{
			Name: SquadBodyBytes, Class: Buffer, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squad.MaxSquadBodyBytes"},
		},
		{
			Name: SkillSourceSummaryRunes, Class: Observation, Unit: Characters,
			Limit: 240, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxSkillSourceSummaryRunes"},
		},
		{
			Name: SquadLaunchSkillWhenRunes, Class: Observation, Unit: Characters,
			Limit: 240, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/squadfiles.maxSkillWhenRunes"},
		},
		{
			Name: SquadRequestBytes, Class: Buffer, Unit: Bytes,
			Limit: 256 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squad.MaxSquadRequestBytes"},
		},
		{
			Name: SquadPlaceLookup, Class: Observation, Unit: Rows,
			Limit: 1024, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squad.MaxSquadPlaceLookup"},
		},
		{
			Name: SquadPackageArchiveBytes, Class: Buffer, Unit: Bytes,
			Limit: 512 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squadpack.MaxArchiveBytes"},
		},
		{
			Name: SquadPackageRequestBytes, Class: Buffer, Unit: Bytes,
			Limit: 1 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squadpack.MaxRequestBytes"},
		},
		{
			Name: SquadPackageManifestBytes, Class: Buffer, Unit: Bytes,
			Limit: 128 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squadpack.MaxManifestBytes"},
		},
		{
			Name: SquadPackageFileBytes, Class: Buffer, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squadpack.MaxFileBytes"},
		},
		{
			Name: SquadPackageExpandedBytes, Class: Buffer, Unit: Bytes,
			Limit: 4 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squadpack.MaxExpandedBytes"},
		},
		{
			Name: SquadPackageEntries, Class: Buffer, Unit: Rows,
			Limit: 128, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squadpack.MaxEntries"},
		},
		{
			Name: SquadPackageExpansionRatio, Class: Buffer, Unit: Multipliers,
			Limit: 64, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/squadpack.MaxExpansionRatio"},
		},
		{
			Name: SquadPackagePreviewRows, Class: Buffer, Unit: Rows,
			Limit: 1024, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/squadpackages.MaxPreviewRows"},
		},
		{
			Name: SquadPackagePreviewAge, Class: Cache, Unit: Seconds,
			Limit: 15 * 60, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/squadpackages.MaxPreviewAgeSeconds"},
		},
		{
			// Terminals open on this machine's own tmux server (limits N59).
			// Each is a person's running shell, so none is let go at the
			// limit: the next open is refused with terminals_full, and only
			// a person closes one.
			Name: TerminalCount, Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Person,
			Sources: []string{"internal/domain/terminal.MaxTerminals"},
		},
		{
			// One batch of keystrokes. Past it the batch is refused with
			// input_too_large before a byte is typed; the sender splits.
			Name: TerminalInputBytes, Class: Buffer, Unit: Bytes,
			Limit: 4 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/terminal.MaxInputBytes"},
		},
		{
			// One paste, the same way.
			Name: TerminalPasteBytes, Class: Buffer, Unit: Bytes,
			Limit: 1 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/terminal.MaxPasteBytes"},
		},
		{
			// One read of a terminal's history. A larger ask is lowered to
			// this; the older lines stay in the server's own history.
			Name: TerminalHistoryLines, Class: Observation, Unit: Rows,
			Limit: 2000, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/terminal.MaxHistoryLines"},
		},
		{
			// Inputs and pastes being typed into the machine's terminals, and
			// the ones waiting behind them (limits N60). Terminals have lanes
			// of their own and never take one of the Agent lanes' sixteen.
			// The next is refused terminal_busy with nothing typed; the
			// sender retries the same number.
			Name: TerminalLane, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/terminals.LaneLimit"},
		},
		{
			// Streams one terminal serves at once. The next viewer is refused
			// terminal_viewers_full; nobody watching is cut off for it.
			Name: TerminalViewers, Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/terminals.MaxViewers"},
		},
		{
			// Terminal streams the machine serves at once, the same way.
			Name: TerminalStreams, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/terminals.MaxStreams"},
		},
		{
			// How long a control lease lasts unrenewed. Past it the lease is
			// lapsed: its holder's next input is lease_expired, and anybody
			// let in may acquire it.
			Name: TerminalLeaseSeconds, Class: Cache, Unit: Seconds,
			Limit: 30, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/terminals.MaxLeaseSeconds"},
		},
		{
			// Active Cloud terminal authority expires after this many seconds.
			// The next terminal operation waits for one bounded refresh or is
			// refused; ordinary Cloud Session traffic does not depend on it.
			Name: CloudTerminalRosterRefresh, Class: Cache, Unit: Seconds,
			Limit: 2, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalRosterRefreshLimit"},
		},
		{
			// A terminal roster request past this deadline fails closed.
			Name: CloudTerminalRosterDeadline, Class: Cache, Unit: Seconds,
			Limit: 2, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalRosterDeadlineLimit"},
		},
		{
			// A failed terminal roster read is read again on the first check
			// this long after it failed; until then terminal rights stay
			// closed as unverified (terminal_busy), never revoked.
			Name: CloudTerminalRosterRetry, Class: Cache, Unit: Seconds,
			Limit: 1, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalRosterRetrySecondsLimit"},
		},
		{
			// A registered connection whose viewer's authority cannot be
			// verified is paused (no frames, input refused as busy) and retired
			// without a revocation notice after this long.
			Name: CloudTerminalUnverifiedRetire, Class: Cache, Unit: Seconds,
			Limit: 10, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalUnverifiedRetireSecondsLimit"},
		},
		{
			Name: CloudTerminalConnections, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalConnectionsLimit"},
		},
		{
			Name: CloudTerminalViewerConnections, Class: Buffer, Unit: Rows,
			Limit: 2, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalViewerConnectionsLimit"},
		},
		{
			Name: CloudTerminalRequestBytes, Class: Buffer, Unit: Bytes,
			Limit: 6<<20 + 4<<10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalRequestBytesLimit"},
		},
		{
			Name: CloudTerminalHistoryReceipt, Class: Buffer, Unit: Bytes,
			Limit: 8 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalHistoryReceiptBytesLimit"},
		},
		{
			Name: CloudTerminalHistoryLine, Class: Buffer, Unit: Bytes,
			Limit: 4 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalHistoryLineBytesLimit"},
		},
		{
			Name: CloudTerminalHistoryCapture, Class: Buffer, Unit: Bytes,
			Limit: 4 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalHistoryCaptureBytesLimit"},
		},
		{
			// Receipts kept per connection for a re-sent request id; each goes after CloudTerminalReceiptSeconds, and at the limit the oldest goes early so the new request is answered.
			Name: CloudTerminalReceipts, Class: Idempotency, Unit: Rows,
			Limit: 512, AtLimit: EvictOldest,
			Deviation: "limits §4.2 has an idempotency row refuse new requests rather than evict inside its window. Decided 2026-10-07: refusing here refused every key of a key held down past 12.8 s (40 a second against 512 in 15 s), while the bound exists only for memory and the viewer never resends a request id itself (cloud-terminal-wire.md); the oldest receipt, the least likely to be asked for again, goes instead, counted as Evicted and logged as stage receipt_evicted.",
			Told:      []Channel{Diagnostics, Log},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/cloud.CloudTerminalReceiptsLimit"},
		},
		{
			Name: CloudTerminalKeySeconds, Class: Cache, Unit: Seconds,
			Limit: 600, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalKeySecondsLimit"},
		},
		{
			Name: CloudTerminalIngress, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalIngressLimit", "internal/transport/cloud.(*Link).runOnce:chan(CloudTerminalIngressLimit)"},
		},
		{
			Name: CloudTerminalListIngress, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalListIngressLimit", "internal/transport/cloud.(*Link).runOnce:chan(CloudTerminalListIngressLimit)"},
		},
		{
			Name: CloudTerminalRefusals, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Log}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalRefusalsLimit", "internal/transport/cloud.(*Link).runOnce:chan(CloudTerminalRefusalsLimit)"},
		},
		{
			Name: CloudTerminalRevocationRetire, Class: Cache, Unit: Seconds,
			Limit: 3, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalRevocationRetireSecondsLimit"},
		},
		{
			Name: CloudTerminalFrameHeartbeat, Class: Cache, Unit: Seconds,
			Limit: 3, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalFrameHeartbeatSecondsLimit"},
		},
		{
			// A browser keeps only the newest verified complete frame that
			// arrives before its terminal ID is known. This is a Console bound;
			// daemon diagnostics publish the policy but cannot measure tab usage.
			Name: CloudTerminalEarlyFrames, Class: Buffer, Unit: Rows,
			Limit: 1, AtLimit: Coalesce,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// A browser page keeps its newest 128 content-free terminal
			// stages, evicting the oldest, and pins its first 16 failure-like
			// stages so a report shows both the start of a break and the
			// latest state. Its console, and a daemon connection's log, write
			// the first 128 stages of each kind in full and then sample them
			// (every 64th routine stage, every 16th failure stage), so neither
			// goes silent and neither grows without bound. The daemon
			// publishes the policy but cannot measure page usage.
			Name: CloudTerminalObservationRows, Class: Observation, Unit: Rows,
			Limit: 128, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalObservationRowsLimit"},
		},
		{
			// A browser retries a rate-limited terminal list read once after
			// the relay's short authority grant renews. It never retries input.
			Name: CloudTerminalListRetry, Class: Buffer, Unit: Rows,
			Limit: 1, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// A Console tab retains only content-free Session header read
			// stages. The daemon publishes the browser policy but cannot inspect
			// another device's sessionStorage.
			Name: CloudHeaderReadDiagnosticRows, Class: Observation, Unit: Rows,
			Limit: 24, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			Name: CloudHeaderReadDiagnosticAge, Class: Cache, Unit: Seconds,
			Limit: 3600, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			Name: CloudTerminalUnconfirmed, Class: Cache, Unit: Seconds,
			Limit: 15, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalUnconfirmedSecondsLimit"},
		},
		{
			// A machine holds at most this many viewers' direct data channels; a further offer is refused `terminal_busy` and that viewer stays on the relay.
			Name: CloudTerminalDirectPeers, Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectPeersLimit"},
		},
		{
			// Direct offers one viewer may make in a minute; more are refused `terminal_busy`. It also bounds the connectivity checks an offer can point this machine at.
			Name: CloudTerminalDirectOffers, Class: Buffer, Unit: Rows,
			Limit: 6, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectOffersPerMinuteLimit"},
		},
		{
			// One opened offer SDP; larger is refused `terminal_invalid` before it is parsed.
			Name: CloudTerminalDirectSDP, Class: Buffer, Unit: Bytes,
			Limit: 16 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectSDPBytesLimit"},
		},
		{
			// Candidate lines in one offer; more are refused `terminal_invalid`.
			Name: CloudTerminalDirectCandidates, Class: Buffer, Unit: Rows,
			Limit: 32, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectCandidatesLimit"},
		},
		{
			// From the answer to an open data channel; a peer that has not opened by then is closed and the terminal stays on the relay.
			Name: CloudTerminalDirectNegotiate, Class: Cache, Unit: Seconds,
			Limit: 5, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectNegotiateSecondsLimit"},
		},
		{
			// How long the machine gathers its own candidates before answering with those it has.
			Name: CloudTerminalDirectGather, Class: Cache, Unit: Seconds,
			Limit: 3, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectGatherSecondsLimit"},
		},
		{
			// One reassembled data-channel message; a larger one closes the channel, and the terminal falls back to the relay.
			Name: CloudTerminalDirectMessage, Class: Buffer, Unit: Bytes,
			Limit: 9 << 20, AtLimit: Disconnect,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectMessageBytesLimit"},
		},
		{
			// A partly received chunked message held longer than this closes the channel.
			Name: CloudTerminalDirectChunk, Class: Buffer, Unit: Seconds,
			Limit: 2, AtLimit: Disconnect,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectChunkSecondsLimit"},
		},
		{
			// A direct frame not acknowledged by the viewer in this time retires the connection.
			Name: CloudTerminalDirectAck, Class: Cache, Unit: Seconds,
			Limit: 3, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectAckSecondsLimit"},
		},
		{
			// Interval between relay probes of one direct connection; one is in flight at a time.
			Name: CloudTerminalDirectProbe, Class: Cache, Unit: Seconds,
			Limit: 1, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectProbeSecondsLimit"},
		},
		{
			// A relay probe unsettled for this long retires the direct connection, so a relay that stops answering cannot leave a direct terminal unchecked.
			Name: CloudTerminalDirectProbeUnsettled, Class: Cache, Unit: Seconds,
			Limit: 2, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalDirectProbeUnsettledSecondsLimit"},
		},
		{
			// Every Cloud terminal connection is re-checked this often: authority, deadlines, and a direct connection's ack and probe.
			Name: CloudTerminalSweep, Class: Cache, Unit: Seconds,
			Limit: 1, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalSweepSecondsLimit"},
		},
		{
			// A receipt the relay refused with rate_limited (its account terminal budget was spent)
			// is published again this many times before the connection is retired.
			Name: CloudTerminalReceiptBusyRetries, Class: Buffer, Unit: Rows,
			Limit: 3, AtLimit: Disconnect,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalReceiptBusyRetriesLimit"},
		},
		{
			// How long a terminal receipt is kept for its request id: past the viewer's ten-second wait, after which it never sends that id again.
			Name: CloudTerminalReceiptSeconds, Class: Idempotency, Unit: Seconds,
			Limit: 15, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalReceiptSecondsLimit"},
		},
		{
			// The wait before such a receipt is published again; the relay refreshes the budget every two seconds.
			Name: CloudTerminalReceiptBusyRetry, Class: Cache, Unit: Seconds,
			Limit: 2, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.CloudTerminalReceiptBusyRetrySecondsLimit"},
		},
		{
			// The terminal grants file. A larger one is not read, and then
			// grants nobody; a write that would make it larger is refused.
			Name: TerminalGrantsBytes, Class: Buffer, Unit: Bytes,
			Limit: 256 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Person,
			Sources: []string{"internal/adapters/devices.MaxGrantsBytes"},
		},
		{
			// One terminal request's body: a 1 MiB paste JSON-escaped, and
			// the fields beside it. Larger is refused 413 before it is read.
			Name: TerminalBodyBytes, Class: Buffer, Unit: Bytes,
			Limit: 6<<20 + 4<<10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.terminalBodyLimit"},
		},
		{
			// One line typed into a new iTerm2 tab or tmux pane to start an
			// assistant. Its shell has not taken the tty yet, and macOS keeps
			// 1024 bytes of a line in canonical mode: a longer one arrived cut
			// off and started nothing. A line past this is never typed; it is
			// written to a script and the short line that runs it is typed.
			Name: TerminalLaunchLineBytes, Class: Buffer, Unit: Bytes,
			Limit: 512, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/terminal.MaxTypedLaunchBytes"},
		},
		{
			// Those scripts. Each removes itself when its tab runs it; one a
			// tab never ran stays, and the oldest go first.
			Name: TerminalLaunchScripts, Class: Work, Unit: Rows,
			Limit: 64, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/terminal.MaxLaunchScripts"},
		},
		{
			// The items one child carries beside its work_id: one branch and
			// one landing for all of them. A dispatch naming more is refused
			// whole with bad_task, never trimmed, because a dropped item is
			// one whose landing would silently not count.
			Name: DispatchAlsoWorkIDs, Class: Buffer, Unit: Rows,
			Limit: 7, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.MaxAlsoWorkIDs"},
		},
		{
			// Closes Sessions asked for themselves while still working. They
			// live in memory; a restart drops them. One past the cap is
			// refused close_schedule_full and nothing is waiting for it.
			Name: SessionCloseScheduled, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxScheduledCloses"},
		},
		{
			// A repeat for the same terminal is the same request: it answers
			// the same 202 and adds nothing.
			Name: SessionCloseScheduledPerTerminal, Class: Buffer, Unit: Rows,
			Limit: 1, AtLimit: Coalesce,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxScheduledClosesPerTerminal"},
		},
		{
			// A terminal still working this long after its close was asked
			// for has its request dropped, with a session.close_schedule_dropped
			// event; the agent can ask again.
			Name: SessionCloseScheduledSeconds, Class: Cache, Unit: Seconds,
			Limit: 900, AtLimit: Expire,
			Told: []Channel{Diagnostics, Log}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxScheduledCloseWait"},
		},
		{
			// The worktrees one handoff pack reads, the bytes of one
			// worktree's patch and of all the pack's patches. Past each the
			// pack refuses that copy and says "truncated" and what it left
			// to read by hand; the reassignment itself never fails on it.
			Name: HandoffWorktrees, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.MaxHandoffWorktrees"},
		},
		{
			Name: HandoffPatchBytes, Class: Buffer, Unit: Bytes,
			Limit: 8 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.MaxHandoffPatchBytes"},
		},
		{
			Name: HandoffPackBytes, Class: Buffer, Unit: Bytes,
			Limit: 32 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.MaxHandoffPackBytes"},
		},
		{
			// A callback naming more words, or more bytes of them, is refused
			// bad_task and nothing is started.
			Name: CallbackArgs, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.MaxCallbackArgs"},
		},
		{
			Name: CallbackCommandBytes, Class: Buffer, Unit: Bytes,
			Limit: 16 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.CallbackCommandLimit"},
		},
		{
			// Callbacks still running, per root and on this machine. They do
			// not take a child's slot; one past either cap is refused
			// callback_capacity and nothing is started.
			Name: CallbacksPerRoot, Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.MaxCallbacksPerRoot"},
		},
		{
			Name: CallbacksPerMachine, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.MaxCallbacksPerMachine"},
		},
		{
			// A running command's output.log. Past the limit the beat keeps
			// its newest output and says on the first line that earlier
			// output was dropped.
			Name: CallbackOutputBytes, Class: Observation, Unit: Bytes,
			Limit: 8 << 20, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.CallbackLogLimit"},
		},
		{
			// The end of output.log read for a finished callback's verdict;
			// earlier output stays in the file for `task show`'s reader.
			Name: CallbackTailBytes, Class: Observation, Unit: Bytes,
			Limit: 8 << 10, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.CallbackTailLimit"},
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
