package capacity

import "time"

// BotServerUnconfirmedNotice is how long a running Bot server's authority may
// stay unreadable before the person is shown that it cannot be confirmed.
// Nothing is stopped at this point; it only changes what is shown.
const BotServerUnconfirmedNotice = BotServerUnconfirmedNoticeMilliseconds * time.Millisecond

const (
	// Reconcile ticks once per second; a few failed reads in a row are normal.
	BotServerUnconfirmedNoticeMilliseconds = 30000
	// Source-local definitions include stopped entries; people remove them.
	MaximumBotServerDefinitions = 64
	// Saved "always allow" Bot-to-Project dispatch rules; people revoke them.
	MaximumBotDispatchRules = 64
	// Independent foreground servers do not occupy the Agent's own slots.
	MaximumBotServerProcesses = 4
	// Complete command/name/directory metadata must fit a human review card.
	MaximumBotServerSpecBytes = 8 << 10
	// A single run retains its latest output, never an unbounded history.
	MaximumBotServerLogBytes = 64 << 10
	// Scoped start receipts are retained until the calling execution ends.
	MaximumBotServerCallerReceipts = 32
	// Read-only listener snapshots do not queue extra OS probes when busy.
	MaximumBotServerProbeReaders = 4
)

func botServerBounds() []Entry {
	return []Entry{
		{Name: "bots.server_definitions", Class: Buffer, Unit: Rows, Limit: MaximumBotServerDefinitions, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Person, Sources: []string{"internal/domain/capacity.MaximumBotServerDefinitions"}},
		{Name: "bots.dispatch_rules", Class: Buffer, Unit: Rows, Limit: MaximumBotDispatchRules, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Person, Sources: []string{"internal/domain/capacity.MaximumBotDispatchRules"}},
		{Name: "bots.server_processes", Class: Buffer, Unit: Rows, Limit: MaximumBotServerProcesses, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotServerProcesses"}},
		{Name: "bots.server_spec_bytes", Class: Buffer, Unit: Bytes, Limit: MaximumBotServerSpecBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Person, Sources: []string{"internal/domain/capacity.MaximumBotServerSpecBytes"}},
		{Name: "bots.server_log_bytes", Class: Observation, Unit: Bytes, Limit: MaximumBotServerLogBytes, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotServerLogBytes"}},
		{Name: "bots.server_caller_receipts", Class: Buffer, Unit: Rows, Limit: MaximumBotServerCallerReceipts, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotServerCallerReceipts"}},
		{Name: "bots.server_unconfirmed_notice_ms", Class: Observation, Unit: Milliseconds, Limit: BotServerUnconfirmedNoticeMilliseconds, AtLimit: Nothing, Deviation: "Notice only; a server whose authority cannot be read keeps running until the person stops or releases it.", Told: []Channel{Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.BotServerUnconfirmedNoticeMilliseconds", "internal/domain/capacity.BotServerUnconfirmedNotice"}},
		{Name: "bots.server_probe_readers", Class: Buffer, Unit: Rows, Limit: MaximumBotServerProbeReaders, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.MaximumBotServerProbeReaders", "internal/adapters/projectassistantacp.package:chan(capacity.MaximumBotServerProbeReaders)", "internal/adapters/projectassistantacp.tcpReadLimit"}},
	}
}
