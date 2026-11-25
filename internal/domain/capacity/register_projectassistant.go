package capacity

import pa "github.com/sainteye/clawdline/internal/domain/projectassistant"

const (
	// One offline Bot recovery input; reject before parsing or creating a target.
	MaximumBotArchiveBytes = 32 << 20
	// CodyNC 9f67eb09, host/src/agent/fallback/carry.rs:13-18 and
	// repeated(None): a bounded recent transcript when no window is known.
	MaximumBotContinuationMessages   = 12
	MaximumBotContinuationQuoteBytes = 2000
	// Trusted ACP category identifiers; reject longer or non-ASCII metadata.
	MaximumBotErrorKindBytes   = 40
	MaximumBotOwnedDescendants = 256
	MaximumBotActors           = 64
	MaximumBotRPCPending       = 32
	MaximumBotFrameBytes       = 1048576
	// Existing process-identity inventory budget; never limits native ACP text.
	MaximumBotProcessCensusBytes = 1048576
	// A macOS census copy that outgrows its size estimate (ENOMEM) is taken
	// again with a fresh, larger estimate at most this many times in all.
	MaximumBotProcessCensusAttempts = 4
	MaximumBotStderrBytes           = 65536
	MaximumBotWriteMilliseconds     = 2000
	MaximumBotCleanupMilliseconds   = 5000
	// A retained Bot re-attempts an unconfirmed Agent cleanup this many times,
	// first after MaximumBotCleanupFirstRetryMilliseconds and doubling: 1+2+4+8+16 s.
	MaximumBotCleanupRetries                = 5
	MaximumBotCleanupFirstRetryMilliseconds = 1000
	// After those retries a retained Bot keeps re-checking its cleanup,
	// doubling the delay from the last fast retry up to this interval, until
	// cleanup is confirmed or Clawdline stops. It never gives up on its own.
	MaximumBotCleanupSlowRetryMilliseconds = 300000
	// Slow re-checks (32, 64, 128, 256 s, then every 300 s: about 23 min)
	// before a retained Bot is released without confirmation, so recovery
	// never waits for an unrelated process to exit.
	MaximumBotCleanupSlowRetries = 8
	// A census gap shorter than this is treated like an ordinary survey
	// interval and is not scanned for strangers. One failed census inside
	// Close spans at most its own deadline plus the grace wait, under
	// 2 x MaximumBotCleanupMilliseconds; longer gaps are scanned.
	MaximumBotUnscannedCensusGapMilliseconds = 10000
	MaximumBotRPCMilliseconds                = 120000
	MaximumBotControlWindowMilliseconds      = 600000
	MaximumBotSurveyMilliseconds             = 50
	MaximumBotChatGapMilliseconds            = 3600000
	MaximumBotRefreshMilliseconds            = 3000
	// Live rows and removal revisions retained by one browser source.
	MaximumBotRosterClientRows = 1000
	// The longest isolated work one Bot dispatch may start.
	MaximumBotDispatchSeconds = 3600
	// Installing the supplied ACP adapter with the person's npm on first use.
	MaximumBotAdapterInstallSeconds = 300
	// One `--version` observation of the person's Node or Claude Code.
	MaximumBotToolProbeMilliseconds = 10000
	MaximumBotToolProbeBytes        = 4096
	// One `codex features list` read, to see whether its memories are on.
	MaximumBotFeatureProbeBytes = 65536
	// The text a permission card shows of one native action: title, command
	// or input and diffs together.
	MaximumBotPermissionActionBytes = 32768
	// One send with its pictures as data: URLs.
	MaximumBotSendBodyBytes = 24 << 20
	// How long the agent may take to say whether a Send now joined its turn
	// (Codync's STEER_ANSWER).
	MaximumBotSteerAnswerMilliseconds = 15000
	// The Project's Claude Code local settings, read to list and revoke the
	// standing rules Always allow and Never leave there.
	MaximumBotRulesFileBytes = 1 << 20
	// Group rooms, CodyNC 9f67eb09 host/src/chat/group.rs and group/rooms.rs.
	MaximumBotGroupRounds            = 3   // MAX_ROUNDS
	MaximumBotGroupReplies           = 10  // Bot-initiated follow-up replies per room turn
	MaximumBotGroupLateTurns         = 3   // rooms.rs MAX_LATE_TURNS
	MaximumBotGroupMemberWaitSeconds = 600 // MEMBER_TIMEOUT; not a retry or permission deadline
	MaximumBotGroupHistoryLines      = 24  // HISTORY_LINES shown to a member
	MaximumBotGroupPeerLineChars     = 2000
	// Live Bots one manager may have created (chat/manage.rs MAX_CREATED).
	MaximumBotManagerCreated = 20
	// CodyNC chat/manage.rs: MAX_CARD_DETAIL (UTF-16 units), APPROVAL_TIMEOUT.
	MaximumBotManagerCardUnits       = 2000
	MaximumBotManagerApprovalSeconds = 270
	// One waiting plan per manager; share the owned RPC concurrency ceiling.
	MaximumBotManagerPending = MaximumBotRPCPending
)

// Group room rows. Names live here, beside the bounds they name.
const (
	BotsGroupRounds            = "bots.group_rounds"
	BotsSkillAssetFiles        = "bots.skill_asset_files"
	BotsConnectorProcesses     = "bots.connector_processes"
	BotsGroupReplies           = "bots.group_replies"
	BotsGroupLateTurns         = "bots.group_late_turns"
	BotsGroupMemberWaitSeconds = "bots.group_member_wait_seconds"
	BotsGroupHistoryLines      = "bots.group_history_lines"
	BotsGroupPeerLineChars     = "bots.group_peer_line_chars"
	BotsManagerCreated         = "bots.manager_created"
	BotsManagerCardUnits       = "bots.manager_card_units"
	BotsManagerApprovalSeconds = "bots.manager_approval_seconds"
	BotsManagerPending         = "bots.manager_pending"
)

// Group activity and presentation bounds. Membership is defined by the room.
func groupBounds() []Entry {
	return []Entry{
		{Name: BotsDispatchedStripRows, Class: Buffer, Unit: Rows, Limit: MaximumBotDispatchedStripRows, AtLimit: Coalesce, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotDispatchedStripRows"}},
		{Name: BotsCompletionWakeRetrySeconds, Class: Buffer, Unit: Seconds, Limit: pa.MaxCompletionWakeRetrySeconds, AtLimit: Coalesce, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxCompletionWakeRetrySeconds"}},
		{Name: BotsGroupPreviewAvatars, Class: Buffer, Unit: Rows, Limit: MaximumBotGroupPreviewAvatars, AtLimit: Coalesce, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotGroupPreviewAvatars"}},
		{Name: BotsSkillAssetFiles, Class: Buffer, Unit: Rows, Limit: pa.MaxSkillAssetFiles, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxSkillAssetFiles"}},
		{Name: BotsConnectorProcesses, Class: Buffer, Unit: Rows, Limit: pa.MaxConnectorProcesses, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxConnectorProcesses"}},
		{Name: BotsManagerCardUnits, Class: Buffer, Unit: Characters, Limit: MaximumBotManagerCardUnits, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotManagerCardUnits"}},
		{Name: BotsManagerApprovalSeconds, Class: Buffer, Unit: Seconds, Limit: MaximumBotManagerApprovalSeconds, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotManagerApprovalSeconds"}},
		{Name: BotsManagerPending, Class: Buffer, Unit: Rows, Limit: MaximumBotManagerPending, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotManagerPending"}},
		{Name: BotsGroupRounds, Class: Buffer, Unit: Rows, Limit: MaximumBotGroupRounds, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotGroupRounds"}},
		{Name: BotsGroupReplies, Class: Buffer, Unit: Rows, Limit: MaximumBotGroupReplies, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotGroupReplies"}},
		{Name: BotsGroupLateTurns, Class: Buffer, Unit: Rows, Limit: MaximumBotGroupLateTurns, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotGroupLateTurns"}},
		{Name: BotsGroupMemberWaitSeconds, Class: Buffer, Unit: Seconds, Limit: MaximumBotGroupMemberWaitSeconds, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotGroupMemberWaitSeconds"}},
		{Name: BotsGroupHistoryLines, Class: Buffer, Unit: Rows, Limit: MaximumBotGroupHistoryLines, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotGroupHistoryLines"}},
		{Name: BotsGroupPeerLineChars, Class: Buffer, Unit: Characters, Limit: MaximumBotGroupPeerLineChars, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotGroupPeerLineChars"}},
		{Name: BotsManagerCreated, Class: Buffer, Unit: Rows, Limit: MaximumBotManagerCreated, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotManagerCreated"}},
	}
}

// These rows bound the frozen contract validators. PA-02 owns live queue
// measurement; PA-05 installs those measurements with the disabled module.
// No absent module reports an invented zero queue depth.
func projectAssistantBounds() []Entry {
	return append(append(append(append(reactionBounds(), groupBounds()...), botServerBounds()...), append(append(botRoutineHookBounds(), memorySettingsBounds()...), sharedSettingsBounds()...)...), []Entry{
		{Name: "bots.archive_bytes", Class: Buffer, Unit: Bytes, Limit: MaximumBotArchiveBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Person, Sources: []string{"internal/domain/capacity.MaximumBotArchiveBytes"}},
		{Name: BotsContinuationMessages, Class: Buffer, Unit: Rows, Limit: MaximumBotContinuationMessages, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotContinuationMessages"}},
		{Name: BotsContinuationQuoteBytes, Class: Buffer, Unit: Bytes, Limit: MaximumBotContinuationQuoteBytes, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotContinuationQuoteBytes"}},
		{Name: BotsReviewPending, Class: Buffer, Unit: Rows, Limit: pa.MaxCollaborationOutstanding, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxCollaborationOutstanding"}},
		{Name: BotsReviewPerSender, Class: Buffer, Unit: Rows, Limit: pa.MaxCollaborationPerSender, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxCollaborationPerSender"}},
		{Name: BotsReviewHops, Class: Buffer, Unit: Rows, Limit: pa.MaxCollaborationHops, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxCollaborationHops"}},
		{Name: BotsReviewRequest, Class: Buffer, Unit: Bytes, Limit: pa.MaxCollaborationRequestBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxCollaborationRequestBytes"}},
		{Name: BotsReviewReply, Class: Buffer, Unit: Bytes, Limit: pa.MaxCollaborationReplyBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxCollaborationReplyBytes"}},
		// One settings save rebases on a fresh read when a turn or memory write
		// won the commit race; at the bound it is refused and the draft is kept.
		{Name: BotsSettingsRebaseAttempts, Class: Buffer, Unit: Rows, Limit: pa.MaxSettingsRebaseAttempts, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxSettingsRebaseAttempts"}},
		{Name: "bots.peer_notes_per_request", Class: Buffer, Unit: Rows, Limit: pa.MaxPeerNotesPerRequest, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxPeerNotesPerRequest"}},
		{Name: "bots.peer_note_bytes", Class: Buffer, Unit: Bytes, Limit: pa.MaxPeerNoteBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxPeerNoteBytes"}},
		{Name: BotsReviewQuoteRounds, Class: Buffer, Unit: Rows, Limit: pa.MaxCollaborationQuoteRounds, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxCollaborationQuoteRounds"}},
		{Name: BotsReviewQuoteBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxCollaborationQuoteBytes, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxCollaborationQuoteBytes"}},
		{Name: BotsFallbacks, Class: Buffer, Unit: Rows, Limit: pa.MaxFallbackAgents, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxFallbackAgents"}},
		{Name: BotsAgentRestSeconds, Class: Cache, Unit: Seconds, Limit: pa.MaxFallbackCreditsRestSeconds, AtLimit: Expire, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxFallbackCreditsRestSeconds", "internal/domain/projectassistant.MaxFallbackUsageRestSeconds", "internal/domain/projectassistant.MaxFallbackSignedOutRestSeconds", "internal/domain/projectassistant.MaxFallbackBackoffOneSeconds", "internal/domain/projectassistant.MaxFallbackBackoffTwoSeconds", "internal/domain/projectassistant.MaxFallbackBackoffThreeSeconds", "internal/domain/projectassistant.MaxFallbackBackoffMaxSeconds"}},
		{Name: "bots.routine_preview_field_bytes", Class: Buffer, Unit: Bytes, Limit: pa.MaxRoutinePreviewFieldBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutinePreviewFieldBytes"}},
		{Name: "bots.tower_zone_shown", Class: Buffer, Unit: Rows, Limit: pa.MaxTowerZoneShown, AtLimit: Coalesce, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxTowerZoneShown"}},
		{Name: "bots.tower_floors", Class: Buffer, Unit: Rows, Limit: pa.MaxTowerFloors, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxTowerFloors"}},
		{Name: "bots.decision_offers", Class: Buffer, Unit: Rows, Limit: pa.MaxDecisionOffersPerBot, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxDecisionOffersPerBot"}},
		{Name: "bots.decision_suggested_replies", Class: Buffer, Unit: Rows, Limit: pa.MaxDecisionSuggestedReplies, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxDecisionSuggestedReplies"}},
		{Name: "bots.decision_question_bytes", Class: Buffer, Unit: Bytes, Limit: pa.MaxDecisionQuestionBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxDecisionQuestionBytes"}},
		{Name: "bots.decision_answer_bytes", Class: Buffer, Unit: Bytes, Limit: pa.MaxDecisionAnswerBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxDecisionAnswerBytes", "internal/domain/projectassistant.MaxDecisionPromptOverheadBytes"}},
		{Name: "bots.decision_reply_bytes", Class: Buffer, Unit: Bytes, Limit: pa.MaxDecisionReplyBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxDecisionReplyBytes"}},
		{Name: "bots.decision_request_quote_bytes", Class: Buffer, Unit: Bytes, Limit: pa.MaxDecisionRequestQuoteBytes, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxDecisionRequestQuoteBytes"}},
		{Name: "bots.decision_offer_seconds", Class: Cache, Unit: Seconds, Limit: pa.MaxDecisionOfferSeconds, AtLimit: Expire, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxDecisionOfferSeconds"}},
		{Name: "bots.tower_floor_projects", Class: Buffer, Unit: Rows, Limit: pa.MaxTowerFloorProjects, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxTowerFloorProjects"}},
		{Name: "bots.tower_floor_project_bytes", Class: Buffer, Unit: Bytes, Limit: pa.MaxTowerFloorProjectBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxTowerFloorProjectBytes"}},
		{Name: "bots.routine_event_name_bytes", Class: Buffer, Unit: Bytes, Limit: pa.MaxRoutineEventNameBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineEventNameBytes"}},
		{Name: "bots.routine_event_filters", Class: Buffer, Unit: Rows, Limit: pa.MaxRoutineEventFilters, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineEventFilters"}},
		{Name: "bots.routine_filter_key_bytes", Class: Buffer, Unit: Bytes, Limit: pa.MaxRoutineEventFilterKeyBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineEventFilterKeyBytes"}},
		{Name: "bots.routine_filter_value_bytes", Class: Buffer, Unit: Bytes, Limit: pa.MaxRoutineEventFilterValueBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineEventFilterValueBytes"}},
		{Name: "bots.routine_webhook_body_bytes", Class: Buffer, Unit: Bytes, Limit: pa.MaxRoutineWebhookBodyBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineWebhookBodyBytes"}},
		{Name: "bots.routine_delivery_id_bytes", Class: Buffer, Unit: Bytes, Limit: pa.MaxRoutineDeliveryIDBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineDeliveryIDBytes"}},
		{Name: "bots.routine_github_event_bytes", Class: Buffer, Unit: Bytes, Limit: pa.MaxRoutineGitHubEventNameBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineGitHubEventNameBytes"}},
		{Name: "bots.routine_hook_secret_bytes", Class: Buffer, Unit: Bytes, Limit: pa.MaxRoutineWebhookSecretBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineWebhookSecretBytes"}},
		{Name: "bots.follow_up_pending", Class: Buffer, Unit: Rows, Limit: pa.MaxRoutineFollowUps, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineFollowUps"}},
		{Name: "bots.follow_up_seconds", Class: Buffer, Unit: Seconds, Limit: pa.MaxRoutineFollowUpSeconds, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineFollowUpSeconds"}},
		{Name: "bots.follow_up_chain", Class: Buffer, Unit: Rows, Limit: pa.MaxRoutineFollowUpChain, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineFollowUpChain"}},
		{Name: "bots.follow_up_tokens", Class: Buffer, Unit: Tokens, Limit: pa.MaxRoutineFollowUpTokens, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineFollowUpTokens"}},
		{Name: "bots.routine_retry_attempts", Class: Buffer, Unit: Rows, Limit: pa.MaxRoutineRetryAttempts, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineRetryAttempts"}},
		{Name: "bots.routine_retry_delay_seconds", Class: Buffer, Unit: Seconds, Limit: pa.MaxRoutineRetryDelaySeconds, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineRetryDelaySeconds"}},
		{Name: BotsRoutineProgressTokens, Class: Buffer, Unit: Tokens, Limit: pa.MaxRoutineProgressTokens, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineProgressTokens"}},
		{Name: BotsRoutineTriggers, Class: Buffer, Unit: Rows, Limit: pa.MaxRoutineTriggers, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineTriggers"}},
		{Name: BotsErrorKindBytes, Class: Buffer, Unit: Bytes, Limit: MaximumBotErrorKindBytes, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotErrorKindBytes"}},
		{Name: BotsRoutineTickMilliseconds, Class: Buffer, Unit: Milliseconds, Limit: pa.MaxRoutineTickMilliseconds, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineTickMilliseconds"}},
		{Name: "bots.background_threads", Class: Buffer, Unit: Rows, Limit: pa.MaxBackgroundThreads, AtLimit: Coalesce, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxBackgroundThreads"}},
		{Name: "bots.background_waiting", Class: Buffer, Unit: Rows, Limit: pa.MaxBackgroundPending, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxBackgroundPending"}},
		{Name: BotsRoutinePending, Class: Buffer, Unit: Rows, Limit: pa.MaxRoutinePendingPerDefinition, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutinePendingPerDefinition"}},
		{Name: BotsRoutineDefinitions, Class: Evidence, Unit: Rows, Limit: pa.MaxRoutineDefinitions, AtLimit: Refuse, Told: []Channel{Diagnostics, Health, Sender}, EvictedBy: Person, Sources: []string{"internal/domain/projectassistant.MaxRoutineDefinitions"}},
		{Name: BotsRoutineNameBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxRoutineNameBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineNameBytes"}},
		{Name: BotsRoutineInstructionBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxRoutineInstructionBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineInstructionBytes"}},
		{Name: BotsRoutineIntervalSeconds, Class: Buffer, Unit: Seconds, Limit: pa.MaxRoutineIntervalSeconds, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineIntervalSeconds"}},
		{Name: BotsRoutineTimeoutSeconds, Class: Buffer, Unit: Seconds, Limit: pa.MaxRoutineTimeoutSeconds, AtLimit: Disconnect, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineTimeoutSeconds"}},
		{Name: BotsRoutineFinishedRuns, Class: Observation, Unit: Rows, Limit: pa.MaxRoutineFinishedRuns, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineFinishedRuns"}},

		{Name: BotsRosterClientRows, Class: Buffer, Unit: Rows, Limit: MaximumBotRosterClientRows, AtLimit: Disconnect, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotRosterClientRows"}},
		{Name: BotsInputBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxInputBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Person, Sources: []string{"internal/domain/projectassistant.MaxInputBytes"}},
		{Name: BotsInputChunkBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxInputChunkBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Person, Sources: []string{"internal/domain/projectassistant.MaxInputChunkBytes"}},
		{Name: BotsRosterSubscribers, Class: Buffer, Unit: Rows, Limit: pa.MaxRosterSubscribers, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRosterSubscribers"}},
		{Name: BotsRosterWakeups, Class: Buffer, Unit: Rows, Limit: pa.MaxRosterWakeups, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRosterWakeups", "internal/adapters/projectassistantstore.(*Store).SubscribeRoster:chan(pa.MaxRosterWakeups)"}},
		{Name: BotsRosterLifetime, Class: Buffer, Unit: Seconds, Limit: pa.MaxRosterLifetimeSeconds, AtLimit: Disconnect, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRosterLifetimeSeconds"}},
		{Name: BotsRosterRecheck, Class: Buffer, Unit: Milliseconds, Limit: pa.MaxRosterRecheckMilliseconds, AtLimit: Disconnect, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRosterRecheckMilliseconds"}},
		{Name: BotsArtifactTitleBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxArtifactTitleBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxArtifactTitleBytes"}},
		{Name: BotsOutputBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxOutputBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxOutputBytes"}},
		{Name: BotsArtifactBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxArtifactBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxArtifactBytes"}},
		{Name: BotsInlineImageBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxInlineImageBytes, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxInlineImageBytes"}},
		{Name: BotsOutputChunkBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxOutputChunkBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxOutputChunkBytes"}},
		{Name: BotsReadViewCursorBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxReadViewCursorBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxReadViewCursorBytes"}},
		{Name: BotsOwnedDescendants, Class: Buffer, Unit: Rows, Limit: MaximumBotOwnedDescendants, AtLimit: Disconnect, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotOwnedDescendants"}},
		{Name: BotsNameRunes, Class: Buffer, Unit: Characters, Limit: pa.MaxNameRunes, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxNameRunes"}},
		{Name: BotsGroupDescriptionRunes, Class: Buffer, Unit: Characters, Limit: pa.MaxGroupDescriptionRunes, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxGroupDescriptionRunes"}},
		{Name: BotsInstructionBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxInstructionBytes, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxInstructionBytes"}},
		{Name: BotsMessageBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxMessageBytes, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxMessageBytes"}},
		{Name: BotsWorkMessageBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxWorkMessageBytes, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxWorkMessageBytes"}},
		{Name: BotsUserMessageBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxUserMessageBytes, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxUserMessageBytes"}},
		{Name: BotsAutomationBacklog, Class: Buffer, Unit: Rows, Limit: pa.MaxAutomationBacklog, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxAutomationBacklog"}},
		{Name: BotsPageRows, Class: Buffer, Unit: Rows, Limit: pa.MaxPageRows, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxPageRows"}},
		// The console's opening page requests a smaller slice of BotsPageRows.
		// The existing history cursor exposes the remaining rows on demand.
		{Name: "bots.chat_initial_page_rows", Class: Buffer, Unit: Rows, Limit: 20, AtLimit: Coalesce,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon},
		{Name: BotsEventRows, Class: Buffer, Unit: Rows, Limit: pa.MaxEventPageRows, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxEventPageRows"}},
		{Name: BotsActors, Class: Buffer, Unit: Rows, Limit: MaximumBotActors, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotActors"}},
		{Name: BotsRPCPending, Class: Buffer, Unit: Rows, Limit: MaximumBotRPCPending, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotRPCPending", "internal/adapters/projectassistantacp.newClient:chan(l.RPCPending)"}},
		// This still bounds owned MCP HTTP/manifest IO, not native ACP lines.
		{Name: BotsFrames, Class: Buffer, Unit: Bytes, Limit: MaximumBotFrameBytes, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotFrameBytes"}},
		{Name: BotsProcessCensusBytes, Class: Buffer, Unit: Bytes, Limit: MaximumBotProcessCensusBytes, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotProcessCensusBytes"}},
		{Name: BotsProcessCensusAttempts, Class: Buffer, Unit: Rows, Limit: MaximumBotProcessCensusAttempts, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotProcessCensusAttempts"}},
		{Name: BotsStderr, Class: Buffer, Unit: Bytes, Limit: MaximumBotStderrBytes, AtLimit: Coalesce,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotStderrBytes"}},
		{Name: BotsMCPWrite, Class: Buffer, Unit: Milliseconds, Limit: MaximumBotWriteMilliseconds, AtLimit: Disconnect,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotWriteMilliseconds"}},
		{Name: BotsCleanup, Class: Buffer, Unit: Milliseconds, Limit: MaximumBotCleanupMilliseconds, AtLimit: Disconnect,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotCleanupMilliseconds"}},
		{Name: BotsCleanupRetries, Class: Buffer, Unit: Rows, Limit: MaximumBotCleanupRetries, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotCleanupRetries"}},
		{Name: BotsCleanupRetryDelay, Class: Buffer, Unit: Milliseconds, Limit: MaximumBotCleanupFirstRetryMilliseconds, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotCleanupFirstRetryMilliseconds"}},
		{Name: BotsCleanupSlowRetryDelay, Class: Buffer, Unit: Milliseconds, Limit: MaximumBotCleanupSlowRetryMilliseconds, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotCleanupSlowRetryMilliseconds"}},
		{Name: BotsCleanupSlowRetries, Class: Buffer, Unit: Rows, Limit: MaximumBotCleanupSlowRetries, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotCleanupSlowRetries"}},
		{Name: BotsUnscannedCensusGap, Class: Buffer, Unit: Milliseconds, Limit: MaximumBotUnscannedCensusGapMilliseconds, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotUnscannedCensusGapMilliseconds"}},
		{Name: BotsRPCDeadline, Class: Buffer, Unit: Milliseconds, Limit: MaximumBotRPCMilliseconds, AtLimit: Disconnect,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotRPCMilliseconds"}},
		{Name: BotsTurnControlWindow, Class: Buffer, Unit: Milliseconds, Limit: MaximumBotControlWindowMilliseconds, AtLimit: Disconnect,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotControlWindowMilliseconds"}},
		{Name: BotsSurvey, Class: Buffer, Unit: Milliseconds, Limit: MaximumBotSurveyMilliseconds, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotSurveyMilliseconds"}},
		{Name: BotsChatGap, Class: Buffer, Unit: Milliseconds, Limit: MaximumBotChatGapMilliseconds, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotChatGapMilliseconds"}},
		{Name: BotsEventRefresh, Class: Buffer, Unit: Milliseconds, Limit: MaximumBotRefreshMilliseconds, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotRefreshMilliseconds"}},
		{Name: BotsDispatchSeconds, Class: Buffer, Unit: Seconds, Limit: MaximumBotDispatchSeconds, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotDispatchSeconds"}},
		{Name: BotsAdapterInstallSeconds, Class: Buffer, Unit: Seconds, Limit: MaximumBotAdapterInstallSeconds, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotAdapterInstallSeconds"}},
		{Name: BotsToolProbe, Class: Buffer, Unit: Milliseconds, Limit: MaximumBotToolProbeMilliseconds, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotToolProbeMilliseconds"}},
		{Name: BotsToolProbeBytes, Class: Buffer, Unit: Bytes, Limit: MaximumBotToolProbeBytes, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotToolProbeBytes"}},
		{Name: BotsFeatureProbeBytes, Class: Buffer, Unit: Bytes, Limit: MaximumBotFeatureProbeBytes, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotFeatureProbeBytes"}},
		{Name: BotsPermissionActionBytes, Class: Buffer, Unit: Bytes, Limit: MaximumBotPermissionActionBytes, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotPermissionActionBytes"}},
		{Name: BotsSendBodyBytes, Class: Buffer, Unit: Bytes, Limit: MaximumBotSendBodyBytes, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotSendBodyBytes"}},
		{Name: BotsRulesFileBytes, Class: Buffer, Unit: Bytes, Limit: MaximumBotRulesFileBytes, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotRulesFileBytes"}},
		{Name: BotsSteerAnswer, Class: Buffer, Unit: Milliseconds, Limit: MaximumBotSteerAnswerMilliseconds, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotSteerAnswerMilliseconds"}},
		{Name: BotsMemoryEntries, Class: Evidence, Unit: Rows, Limit: pa.MaxMemoryEntries, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person,
			Sources: []string{"internal/domain/projectassistant.MaxMemoryEntries"}},
		{Name: BotsMemorySearchResults, Class: Buffer, Unit: Rows, Limit: pa.MaxMemorySearchResults, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxMemorySearchResults"}},
		{Name: BotsAttachmentBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxAttachmentBytes, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxAttachmentBytes"}},
		{Name: BotsRoomPictures, Class: Buffer, Unit: Rows, Limit: pa.MaxRoomPictures, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxRoomPictures"}},
	}...)
}
