package capacity

import pa "github.com/sainteye/clawdline/internal/domain/projectassistant"

func memorySettingsBounds() []Entry {
	return []Entry{
		{Name: "bots.extensions", Class: Buffer, Unit: Rows, Limit: pa.MaxBotExtensions, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Person, Sources: []string{"internal/domain/projectassistant.MaxBotExtensions"}},
		{Name: "bots.login_profiles", Class: Buffer, Unit: Rows, Limit: pa.MaxLoginProfiles, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Person, Sources: []string{"internal/domain/projectassistant.MaxLoginProfiles"}},
		{Name: "bots.config_probes", Class: Buffer, Unit: Rows, Limit: pa.MaxConfigurationProbes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxConfigurationProbes", "internal/adapters/projectassistanthost.New:chan(pa.MaxConfigurationProbes)"}},
		{Name: "bots.config_options", Class: Buffer, Unit: Rows, Limit: pa.MaxConfigurationOptions, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxConfigurationOptions"}},
		{Name: "bots.config_choices", Class: Buffer, Unit: Rows, Limit: pa.MaxConfigurationChoices, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxConfigurationChoices"}},
		{Name: "bots.config_value_bytes", Class: Buffer, Unit: Bytes, Limit: pa.MaxConfigurationValueBytes, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxConfigurationValueBytes"}},
		{Name: "bots.memory_preview", Class: Buffer, Unit: Characters, Limit: pa.MaxMemoryPreviewRunes, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/projectassistant.MaxMemoryPreviewRunes"}},
	}
}
