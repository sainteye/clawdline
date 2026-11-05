package capacity

// These shared scan bounds also apply to the standalone read-only CLI probe.
// Its answer reports work performed; reaching a limit refuses freshness.
func codegraphToolBounds() []Entry {
	return []Entry{
		{Name: "codegraph.metadata_bytes", Class: Buffer, Unit: Bytes, Limit: 64 << 20, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.MaxMetadataBytes"}},
		{Name: "codegraph.source_file_bytes", Class: Buffer, Unit: Bytes, Limit: 16 << 20, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.MaxFileBytes"}},
		{Name: "codegraph.total_read_bytes", Class: Buffer, Unit: Bytes, Limit: 256 << 20, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.MaxTotalBytes"}},
		{Name: "codegraph.scan_entries", Class: Buffer, Unit: Rows, Limit: 20000, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.MaxScanEntries"}},
		{Name: "codegraph.check_seconds", Class: Buffer, Unit: Seconds, Limit: 5, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.CheckSecondsLimit"}},
	}
}
