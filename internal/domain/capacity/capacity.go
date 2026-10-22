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

import (
	"regexp"
	"sort"
)

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
	// Milliseconds retains subsecond deadlines without rounding the capacity.
	Milliseconds Unit = "milliseconds"
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
	AuditSecurity     = "audit.security"
	StoreDB           = "store.db"
	StoreEvents       = "store.events"
	SessionExecutions = "session.executions"
	SessionNoMovement = "session.no_movement_seconds"
	// How often a Cloud Session row whose only change is volatile is re-sent.
	CloudSessionRowVolatile = "cloud.session_row_volatile_seconds"
	// How long the Cloud publisher may go without reading its own task list
	// when nothing has told it the list moved.
	CloudTaskListReread = "cloud.task_list_reread_seconds"
	// The last committed execution scan, so an unchanged one opens no write.
	CacheSessionExecutionMemo = "cache.session_execution_memo"
	// Each working directory's Project key, for the session list's rows.
	CacheSessionProjectKeys = "cache.session_project_keys"
	BoardReceipts           = "board.receipts"
	CloudRelayQueue         = "cloud.relay_queue"
	CloudReadQueue          = "cloud.read_queue"
	CloudLaneRefusals       = "cloud.lane_refusals"
	StoreReceipts           = "store.receipts"
	// How long the store keeps what nothing reads any more.
	StoreDoneEffectRetention       = "store.done_effect_retention_seconds"
	StoreReceiptTombstoneRetention = "store.receipt_tombstone_retention_seconds"
	StoreRetentionPruneBatch       = "store.retention_prune_batch_rows"
	CloudPeerPairs                 = "cloud.peer_pairs"
	CloudPeerGrants                = "cloud.peer_grants"
	CloudPeerInbox                 = "cloud.peer_inbox"
	CloudPeerInboxPage             = "cloud.peer_inbox_page"
	CloudPeerOutbox                = "cloud.peer_outbox"
	CloudPeerIngress               = "cloud.peer_ingress"
	CloudPeerBody                  = "cloud.peer_body_bytes"
	CloudPeerFrame                 = "cloud.peer_frame_bytes"
	// What a hosted page may report about its own failures (`diagnostics.events`).
	CloudViewerEventsBatch = "cloud.viewer_events_batch_bytes"
	CloudViewerEventsRows  = "cloud.viewer_events_logged_rows"
	CloudViewerEventsLine  = "cloud.viewer_events_line_bytes"
	CloudSessionRowID      = "cloud.session_row_id_bytes"
	CloudSessionRowAsks    = "cloud.session_row_asks"
	CloudViewerEventsSeen  = "cloud.viewer_events_seen_batches"
	CloudReloadStopSeconds = "cloud.reload_stop_seconds"
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
	UsageWorkCursorRows          = "usage.work_cursor_rows"
	UsageWorkCursorQueue         = "usage.work_cursor_queue"
	UsageWorkUnitsInAnswer       = "usage.work_units_per_answer"
	HumanInterventionsOpen       = "session.human_interventions_open"
	HumanInterventionsTotal      = "session.human_interventions_total"
	HumanInterventionsRecent     = "session.human_interventions_recent"
	HumanInterventionTitle       = "session.human_intervention_title_bytes"
	HumanInterventionText        = "session.human_intervention_text_bytes"
	HumanInterventionDetail      = "session.human_intervention_detail_bytes"
	HumanInterventionDocument    = "session.human_intervention_document_bytes"
	HumanInterventionDraft       = "session.human_intervention_draft_bytes"
	SecretHandoffBytes           = "session.secret_handoff_bytes"
	SecretHandoffPending         = "session.secret_handoff_pending"
	SecretHandoffSeconds         = "session.secret_handoff_seconds"
	SecretHandoffDeliverySeconds = "session.secret_handoff_delivery_seconds"
	SettingsRequestBodyBytes     = "settings.request_body_bytes"
	NotificationRulesRows        = "notification.rules_rows"
	NotificationRuleLabel        = "notification.rule_label_characters"
	NotificationRuleWait         = "notification.rule_wait_seconds"
	SessionTitleRequestBytes     = "session.title_request_bytes"
	SessionTitleCharacters       = "session.title_characters"
	SessionTitleRows             = "session.title_rows"
	SessionTitleAge              = "session.title_age"
	WorkItemTitleBytes           = "work.item_title_bytes"
	WorkItemDescriptionBytes     = "work.item_description_bytes"
	WorkItemUserActionBytes      = "work.item_user_action_bytes"
	WorkCompletionReasonBytes    = "work.completion_reason_bytes"
	SessionDirectTodoBytes       = "session.direct_todo_bytes"
	SessionTodoBatchRows         = "session.todo_batch_rows"
	ReportOpenTodoRows           = "session.report_open_todo_rows"
	ReportOpenTodoCharacters     = "session.report_open_todo_characters"
	RunCreatedItems              = "run.created_items"
	RunClaimedItems              = "run.claimed_items"
	EpicChildItems               = "epic.child_items"
	PlanReviewBlockingListed     = "planreview.blocking_listed"
	WorkRequestBodyBytes         = "work.request_body_bytes"
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
	ReleaseAppRelistSeconds       = "release.app_relist_seconds"
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
	StoreReadConnections                 = "store.read_connections"
	StoreAntigravityProjectConsents      = "store.antigravity_project_consents"
	StoreAntigravityConsentRootBytes     = "store.antigravity_consent_root_bytes"
	StoreAntigravityConsentIdentityBytes = "store.antigravity_consent_identity_bytes"
	CacheTerminalScreens                 = "cache.terminal_screens"
	CacheSessionInventory                = "cache.session_inventory"
	// An iTerm2 that stops answering Apple Events writes its own diagnosis:
	// the failures held for it, the files kept, how often one is written,
	// and what each of its steps may take (docs/limits.md N64).
	ITermStallFailures     = "iterm.stall_failures"
	ITermStallSaidBytes    = "iterm.stall_said_bytes"
	ITermStallDiagnoses    = "iterm.stall_diagnoses"
	ITermStallCooldown     = "iterm.stall_cooldown_seconds"
	ITermStallSectionBytes = "iterm.stall_section_bytes"
	ITermStallStepSeconds  = "iterm.stall_step_seconds"
	// The osascript run kinds counted apart for the hourly line and
	// /v1/diagnostics (the iTerm2 scan experiment, docs/interface.md).
	ITermOsascriptKinds = "iterm.osascript_kinds"
	// A tmux Session is already running while iTerm2 opens its optional viewer.
	ITermViewerOpenSeconds = "iterm.viewer_open_seconds"
	ITermViewerConcurrent  = "iterm.viewer_concurrent"
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
	// One optional whisper.cpp model download. The response is bounded before
	// it can fill the machine, and no partial file becomes a usable model.
	VoiceModelDownloadBytes = "voice.model_download_bytes"
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
	SessionsAgentRows = "sessions.agent_rows"
	// The waited-on tasks one Session row names (transport/http/session_wait_targets.go).
	SessionCodexPastPage  = "session.codex_past_page"
	SessionWaitingFor     = "session.waiting_for"
	CacheBackgroundAgents = "cache.background_agents"
	// SessionsShellOutputBytes is the most of one background command's
	// output GET /v1/sessions/{id}/shells/{shell} sends.
	SessionsShellOutputBytes = "sessions.shell_output_bytes"
	// A finished scheduled run matched to its child's conversation by
	// transcript (adapters/projects/child_transcript.go).
	ScheduleRunTranscriptsProbed = "schedule.run_transcripts_probed"
	ScheduleRunTranscriptHead    = "schedule.run_transcript_head"
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
	// How the console's transcript pane reads (web/console/src/session/
	// transcript-follow.ts) and how the Cloud seam answers it
	// (web/console/src/cloud/relay-reader.ts). They bound requests, not
	// storage: the browser enforces them, and they are registered here so a
	// change to any of them is a change to this register.
	ConsoleTranscriptSafetySeconds    = "console.transcript_safety_seconds"
	ConsoleTranscriptFollowSeconds    = "console.transcript_follow_seconds"
	ConsoleTranscriptFillPages        = "console.transcript_fill_pages"
	ConsoleTranscriptMergedRows       = "console.transcript_merged_rows"
	ConsoleTranscriptBackoffSeconds   = "console.transcript_backoff_seconds"
	ConsoleRelayTranscriptExpectReask = "console.relay_transcript_expect_reask_seconds"
	ConsoleRelayFeatureWait           = "console.relay_feature_wait_seconds"
	ConsoleExecutionRecoveryAttempts  = "console.execution_recovery_attempts"
	ConsoleStatusGapRetry             = "console.status_gap_retry_seconds"
	ConsolePersonaRetry               = "console.persona_retry_seconds"
	ConsoleStatusMarkerRetry          = "console.status_marker_retry_seconds"
	ConsolePinnedListReadTimeout      = "console.pinned_list_read_timeout_seconds"
	// One save of a pairing's permission by the macOS shell
	// (shell/darwin/Pairing.swift saveTimeoutSeconds).
	ConsoleNativePairingSaveSeconds = "console.native_pairing_save_seconds"
	ConsoleStatusMarkerRecoveryWait = "console.status_marker_recovery_wait_seconds"
	ConsoleTranscriptFollowFast     = "console.transcript_follow_fast_seconds"
	ConsoleTranscriptFollowLater    = "console.transcript_follow_later_seconds"
	ConsoleHealthLive               = "console.health_live_seconds"
	ConsoleSessionNotesSafety       = "console.session_notes_safety_seconds"
	ConsoleOpenedRowRestate         = "console.opened_row_restate_seconds"
	ConsoleOpenedRowQuiet           = "console.opened_row_quiet_seconds"
	// This browser's own record of direct attempts per machine, drawn on the
	// machine sheet (web/console/src/cloud/direct-history.ts), and how often
	// the open machine list re-reads (pages/devices/AccountMachines.tsx).
	ConsoleDirectHistoryHours    = "console.direct_history_hours"
	ConsoleDirectHistoryMachines = "console.direct_history_machines"
	ConsoleMachineListPoll       = "console.machine_list_poll_seconds"
	// The ordinary shells this machine holds open for a person, and what one
	// request may type into one or read back from it (limits N59).
	TerminalCount        = "terminal.count"
	TerminalInputBytes   = "terminal.input_bytes"
	TerminalPasteBytes   = "terminal.paste_bytes"
	TerminalHistoryLines = "terminal.history_lines"
	TerminalLane         = "terminal.lane"
	TerminalViewers      = "terminal.viewers"
	TerminalStreams      = "terminal.streams"
	TerminalLeaseSeconds = "terminal.lease_seconds"
	// Every API call this daemon answers, counted by route shape and caller
	// (`/v1/diagnostics.routes`).
	DiagnosticsRouteStatKeys          = "diagnostics.route_stat_keys"
	DiagnosticsRouteLatencySamples    = "diagnostics.route_latency_samples"
	CloudTerminalRosterRefresh        = "cloud.terminal_roster_refresh_seconds"
	CloudTerminalRosterDeadline       = "cloud.terminal_roster_deadline_seconds"
	CloudTerminalRosterRetry          = "cloud.terminal_roster_retry_seconds"
	CloudRosterShortReadAttempts      = "cloud.roster_short_read_attempts"
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
//
// Each area's rows are declared in its own register_<area>.go, listed in
// areas. The rows come back sorted by name, so where a row is declared never
// changes the order /v1/diagnostics and /v1/capacity list them in.
func Register() []Entry {
	var rows []Entry
	for _, a := range areas {
		rows = append(rows, a.bounds()...)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows
}

// Default is a registered row's default limit, and 0 for a name that is not
// registered. Adapters that enforce a row's limit take their default from
// here, so the number has one spelling.
func Default(name string) int64 {
	for _, a := range areas {
		for _, e := range a.bounds() {
			if e.Name == name {
				return e.Limit
			}
		}
	}
	return 0
}
