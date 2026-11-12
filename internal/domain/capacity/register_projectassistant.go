package capacity

import pa "github.com/sainteye/clawdline/internal/domain/projectassistant"

// These rows bound the frozen contract validators. PA-02 owns live queue
// measurement; PA-05 installs those measurements with the disabled module.
// No absent module reports an invented zero queue depth.
func projectAssistantBounds() []Entry {
	return []Entry{
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
	}
}
