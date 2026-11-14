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
	// The longest isolated work one Bot dispatch may start.
	MaximumBotDispatchSeconds = 3600
	// Installing the supplied ACP adapter with the person's npm on first use.
	MaximumBotAdapterInstallSeconds = 300
	// One `--version` observation of the person's Node or Claude Code.
	MaximumBotToolProbeMilliseconds = 10000
	MaximumBotToolProbeBytes        = 4096
	// Commits retried after only a reader's read cursor moved the Bot version.
	MaximumBotVersionRetries = 3
)

// These rows bound the frozen contract validators. PA-02 owns live queue
// measurement; PA-05 installs those measurements with the disabled module.
// No absent module reports an invented zero queue depth.
func projectAssistantBounds() []Entry {
	return []Entry{
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
		{Name: BotsVersionRetries, Class: Buffer, Unit: Rows, Limit: MaximumBotVersionRetries, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/capacity.MaximumBotVersionRetries"}},
	}
}
