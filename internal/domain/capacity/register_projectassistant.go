package capacity

import pa "github.com/sainteye/clawdline/internal/domain/projectassistant"

const (
	MaximumBotOwnedDescendants    = 256
	MaximumBotActors              = 64
	MaximumBotProcesses           = 4
	MaximumBotRPCPending          = 32
	MaximumBotPermissions         = 8
	MaximumBotNotifications       = 256
	MaximumBotFrameBytes          = 1048576
	MaximumBotCaptureBytes        = 1048576
	MaximumBotStderrBytes         = 65536
	MaximumBotWriteMilliseconds   = 2000
	MaximumBotCleanupMilliseconds = 5000
	MaximumBotRPCMilliseconds     = 120000
	MaximumBotPromptMilliseconds  = 600000
	MaximumBotSurveyMilliseconds  = 50
	MaximumBotChatGapMilliseconds = 3600000
	MaximumBotRefreshMilliseconds = 3000
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
)

// These rows bound the frozen contract validators. PA-02 owns live queue
// measurement; PA-05 installs those measurements with the disabled module.
// No absent module reports an invented zero queue depth.
func projectAssistantBounds() []Entry {
	return []Entry{
		{Name: BotsRoutineTriggers, Class: Buffer, Unit: Rows, Limit: pa.MaxRoutineTriggers, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineTriggers"}},
		{Name: BotsRoutineTickMilliseconds, Class: Buffer, Unit: Milliseconds, Limit: pa.MaxRoutineTickMilliseconds, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRoutineTickMilliseconds"}},
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
		{Name: BotsInputsPerMessage, Class: Buffer, Unit: Rows, Limit: pa.MaxInputsPerMessage, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Person, Sources: []string{"internal/domain/projectassistant.MaxInputsPerMessage"}},
		{Name: BotsInputsPerBot, Class: Buffer, Unit: Rows, Limit: pa.MaxInputsPerBot, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Person, Sources: []string{"internal/domain/projectassistant.MaxInputsPerBot"}},
		{Name: BotsRosterSubscribers, Class: Buffer, Unit: Rows, Limit: pa.MaxRosterSubscribers, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRosterSubscribers"}},
		{Name: BotsRosterWakeups, Class: Buffer, Unit: Rows, Limit: pa.MaxRosterWakeups, AtLimit: Disconnect, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRosterWakeups", "internal/adapters/projectassistantstore.(*Store).SubscribeRoster:chan(pa.MaxRosterWakeups)"}},
		{Name: BotsRosterLifetime, Class: Buffer, Unit: Seconds, Limit: pa.MaxRosterLifetimeSeconds, AtLimit: Disconnect, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRosterLifetimeSeconds"}},
		{Name: BotsRosterRecheck, Class: Buffer, Unit: Milliseconds, Limit: pa.MaxRosterRecheckMilliseconds, AtLimit: Disconnect, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxRosterRecheckMilliseconds"}},
		{Name: BotsArtifactTitleBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxArtifactTitleBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxArtifactTitleBytes"}},
		{Name: BotsOutputBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxOutputBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxOutputBytes"}},
		{Name: BotsArtifactBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxArtifactBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxArtifactBytes"}},
		{Name: BotsOutputChunkBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxOutputChunkBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxOutputChunkBytes"}},
		{Name: BotsReadViewCursorBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxReadViewCursorBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxReadViewCursorBytes"}},
		{Name: BotsOwnedDescendants, Class: Buffer, Unit: Rows, Limit: MaximumBotOwnedDescendants, AtLimit: Disconnect, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotOwnedDescendants"}},
		{Name: BotsNameRunes, Class: Buffer, Unit: Characters, Limit: pa.MaxNameRunes, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxNameRunes"}},
		{Name: BotsInstructionBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxInstructionBytes, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxInstructionBytes"}},
		{Name: BotsMessageBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxMessageBytes, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxMessageBytes"}},
		{Name: BotsPendingTurns, Class: Buffer, Unit: Rows, Limit: pa.MaxPendingTurns, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxPendingTurns"}},
		{Name: BotsPageRows, Class: Buffer, Unit: Rows, Limit: pa.MaxPageRows, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxPageRows"}},
		{Name: BotsEventRows, Class: Buffer, Unit: Rows, Limit: pa.MaxEventPageRows, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxEventPageRows"}},
		{Name: BotsActors, Class: Buffer, Unit: Rows, Limit: MaximumBotActors, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotActors"}},
		{Name: BotsProcesses, Class: Buffer, Unit: Rows, Limit: MaximumBotProcesses, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotProcesses", "internal/app/projectassistant.NewRuntime:chan(o.Limits.MaxProcesses)"}},
		{Name: BotsRPCPending, Class: Buffer, Unit: Rows, Limit: MaximumBotRPCPending, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotRPCPending", "internal/adapters/projectassistantacp.NewClient:chan(l.RPCPending)"}},
		{Name: BotsPermissions, Class: Buffer, Unit: Rows, Limit: MaximumBotPermissions, AtLimit: Disconnect,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotPermissions"}},
		{Name: BotsNotifications, Class: Buffer, Unit: Rows, Limit: MaximumBotNotifications, AtLimit: Disconnect,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotNotifications", "internal/adapters/projectassistantacp.NewClient:chan(l.Notifications)"}},
		{Name: BotsFrames, Class: Buffer, Unit: Bytes, Limit: MaximumBotFrameBytes, AtLimit: Disconnect,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotFrameBytes"}},
		{Name: BotsCapture, Class: Buffer, Unit: Bytes, Limit: MaximumBotCaptureBytes, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotCaptureBytes"}},
		{Name: BotsStderr, Class: Buffer, Unit: Bytes, Limit: MaximumBotStderrBytes, AtLimit: Coalesce,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotStderrBytes"}},
		{Name: BotsWrite, Class: Buffer, Unit: Milliseconds, Limit: MaximumBotWriteMilliseconds, AtLimit: Disconnect,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotWriteMilliseconds"}},
		{Name: BotsCleanup, Class: Buffer, Unit: Milliseconds, Limit: MaximumBotCleanupMilliseconds, AtLimit: Disconnect,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotCleanupMilliseconds"}},
		{Name: BotsRPCDeadline, Class: Buffer, Unit: Milliseconds, Limit: MaximumBotRPCMilliseconds, AtLimit: Disconnect,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotRPCMilliseconds"}},
		{Name: BotsPrompt, Class: Buffer, Unit: Milliseconds, Limit: MaximumBotPromptMilliseconds, AtLimit: Disconnect,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotPromptMilliseconds"}},
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
		{Name: BotsAttachmentsPerMessage, Class: Buffer, Unit: Rows, Limit: pa.MaxAttachmentsPerMessage, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxAttachmentsPerMessage"}},
		{Name: BotsAttachmentBytes, Class: Buffer, Unit: Bytes, Limit: pa.MaxAttachmentBytes, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/projectassistant.MaxAttachmentBytes"}},
	}
}
