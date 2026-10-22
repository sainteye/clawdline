package capacity

// antigravityBounds registers the local native observation bridge.
func antigravityBounds() []Entry {
	return []Entry{
		{Name: "antigravity.hook_payload_bytes", Class: Buffer, Unit: Bytes, Limit: 1048576, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravity.MaxHookPayloadBytes"}},
		{Name: "antigravity.observation_bytes", Class: Buffer, Unit: Bytes, Limit: 4194304, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravity.MaxObservationBytes"}},
		{Name: "antigravity.live_bindings", Class: Buffer, Unit: Rows, Limit: 512, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravity.MaxLiveBindings"}},
		{Name: "antigravity.managed_launches", Class: Buffer, Unit: Rows, Limit: 512, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravity.MaxLiveBindings"}},
		{Name: "antigravity.ancestry_rows", Class: Buffer, Unit: Rows, Limit: 32, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravity.MaxAncestry"}},
		{Name: "antigravity.state_age_seconds", Class: Cache, Unit: Seconds, Limit: 15, AtLimit: Expire, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravity.MaxObservationAge"}},
		{Name: "antigravity.retention_seconds", Class: Cache, Unit: Seconds, Limit: 86400, AtLimit: Expire, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravity.MaxObservationRetention"}},
		{Name: "antigravity.process_read_seconds", Class: Buffer, Unit: Seconds, Limit: 3, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravity.MaxProcessReadTime"}},
		{Name: "antigravity.hook_config_bytes", Class: Buffer, Unit: Bytes, Limit: 1048576, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravity.MaxHookConfigBytes"}},
		{Name: "antigravity.hook_timeout_seconds", Class: Buffer, Unit: Seconds, Limit: 3, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravity.MaxHookTimeoutSeconds"}},
	}
}
