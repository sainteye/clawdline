package capacity

// antigravityBounds registers the local native observation bridge.
func antigravityBounds() []Entry {
	return []Entry{
		{Name: "antigravity.history_connections", Routine: true, Class: Buffer, Unit: Rows, Limit: 1, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravitysession.MaxHistoryConnections"}},
		{Name: "antigravity.acp_sessions", Class: Buffer, Unit: Rows, Limit: 2, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravitysession.MaxSessions"}},
		{Name: "antigravity.acp_frame_bytes", Class: Buffer, Unit: Bytes, Limit: 1048576, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravitysession.MaxFrameBytes"}},
		{Name: "antigravity.acp_view_bytes", Class: Observation, Unit: Bytes, Limit: 1048576, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravitysession.MaxViewBytes"}},
		{Name: "antigravity.acp_call_seconds", Class: Buffer, Unit: Seconds, Limit: 45, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravitysession.MaxCallTime"}},
		{Name: "antigravity.first_input_slots", Class: Buffer, Unit: Rows, Limit: 512, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravitylaunch.MaxFirstInputs"}},
		{Name: "antigravity.saved_launches", Class: Buffer, Unit: Rows, Limit: 512, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravitylaunch.MaxLaunches"}},
		{Name: "antigravity.context_history", Class: Buffer, Unit: Rows, Limit: 512, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravitylaunch.MaxHistory"}},
		{Name: "antigravity.context_receipts", Class: Buffer, Unit: Rows, Limit: 512, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravitylaunch.MaxReceipts"}},
		{Name: "antigravity.context_index_bytes", Class: Buffer, Unit: Bytes, Limit: 4194304, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravitylaunch.MaxIndexBytes"}},
		{Name: "antigravity.launch_context_bytes", Class: Buffer, Unit: Bytes, Limit: 262144, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravitylaunch.MaxContextBytes"}},
		{Name: "antigravity.hook_connections", Class: Buffer, Unit: Rows, Limit: 8, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravity.MaxHookConnections"}},
		{Name: "antigravity.hook_frame_bytes", Class: Buffer, Unit: Bytes, Limit: 2097152, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravity.MaxHookFrameBytes"}},
		{Name: "antigravity.hook_request_seconds", Class: Buffer, Unit: Seconds, Limit: 2, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravity.MaxHookRequestTime"}},
		{Name: "antigravity.native_process_file_bytes", Class: Buffer, Unit: Bytes, Limit: 1048576, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravity.MaxNativeProcessFileBytes"}},
		{Name: "antigravity.native_tty_entries", Class: Buffer, Unit: Rows, Limit: 4096, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/antigravity.MaxNativeTTYEntries"}},
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
