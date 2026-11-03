package capacity

// Graphify checks are explicitly invoked read-only CLI probes. No daemon cache
// or background scanner owns these buffers; the JSON answer tells the caller
// the work performed and refuses index use when the probe cannot complete.
func graphifyToolBounds() []Entry {
	return []Entry{
		{Name: "graphify.metadata_bytes", Class: Buffer, Unit: Bytes, Limit: 64 << 20, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/graphify.MaxMetadataBytes"}},
		{Name: "graphify.source_file_bytes", Class: Buffer, Unit: Bytes, Limit: 16 << 20, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/graphify.MaxFileBytes"}},
		{Name: "graphify.total_read_bytes", Class: Buffer, Unit: Bytes, Limit: 256 << 20, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/graphify.MaxTotalBytes"}},
		{Name: "graphify.scan_entries", Class: Buffer, Unit: Rows, Limit: 20000, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/graphify.MaxScanEntries"}},
		{Name: "graphify.check_seconds", Class: Buffer, Unit: Seconds, Limit: 5, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/graphify.CheckSecondsLimit"}},
	}
}
