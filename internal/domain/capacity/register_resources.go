package capacity

import "github.com/sainteye/clawdline/internal/domain/taskresource"

// resourceBounds registers retention, accounting and admission for disposable data.
func resourceBounds() []Entry {
	return []Entry{
		{Name: "resources.scan_files", Class: Buffer, Unit: Rows, Limit: taskresource.FilesLimit, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.FilesLimit"}},
		{Name: "resources.scratch_seconds", Class: Work, Unit: Seconds, Limit: taskresource.ScratchRetentionSecondsLimit, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.ScratchRetentionSecondsLimit"}},
		{Name: "resources.log_seconds", Class: DiagnosticLog, Unit: Seconds, Limit: taskresource.LogRetentionSecondsLimit, AtLimit: Rotate, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.LogRetentionSecondsLimit"}},
		{Name: "resources.cache_bytes", Class: Cache, Unit: Bytes, Limit: taskresource.CacheBytesLimit, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.CacheBytesLimit"}},
		{Name: "resources.cache_total_bytes", Class: Cache, Unit: Bytes, Limit: taskresource.CacheTotalBytesLimit, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.CacheTotalBytesLimit"}},
		{Name: "resources.scratch_total_bytes", Class: Work, Unit: Bytes, Limit: taskresource.ScratchTotalBytesLimit, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.ScratchTotalBytesLimit"}},
		{Name: "resources.disk_reserve_bytes", Class: Buffer, Unit: Bytes, Limit: taskresource.DiskReserveBytesLimit, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.DiskReserveBytesLimit"}},
		{Name: "resources.page_rows", Class: Buffer, Unit: Rows, Limit: taskresource.PageRowsLimit, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.PageRowsLimit"}},
	}
}
