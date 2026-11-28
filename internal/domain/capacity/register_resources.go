package capacity

import "github.com/sainteye/clawdline/internal/domain/taskresource"

// MachineDiskLow is the low free-space alert row.
const MachineDiskLow = "resources.disk_low_free_bytes"

// resourceBounds registers retention, accounting and admission for disposable data.
func resourceBounds() []Entry {
	return []Entry{
		{Name: "resources.ready_seconds", Class: Cache, Unit: Seconds, Limit: taskresource.PreparationReadySecondsLimit, AtLimit: Expire, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.PreparationReadySecondsLimit"}},
		{Name: "resources.admission_measurement_attempts", Class: Buffer, Unit: Rows, Limit: taskresource.AdmissionMeasurementAttemptsLimit, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.AdmissionMeasurementAttemptsLimit"}},
		{Name: "resources.scan_files", Class: Buffer, Unit: Rows, Limit: taskresource.FilesLimit, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.FilesLimit"}},
		{Name: "resources.scratch_seconds", Class: Work, Unit: Seconds, Limit: taskresource.ScratchRetentionSecondsLimit, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.ScratchRetentionSecondsLimit"}},
		// A finished task's artifacts/ go once it has been over this long
		// since it ended with its landing settled; result.json, task.json and
		// the landing stay, and the reclaim row says the files expired.
		{Name: ResourceArtifactRetention, Class: Work, Unit: Seconds, Limit: taskresource.ArtifactRetentionSecondsLimit, AtLimit: EvictOldest, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.ArtifactRetentionSecondsLimit"}},
		{Name: "resources.log_seconds", Class: DiagnosticLog, Unit: Seconds, Limit: taskresource.LogRetentionSecondsLimit, AtLimit: Rotate, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.LogRetentionSecondsLimit"}},
		{Name: "resources.cache_bytes", Class: Cache, Unit: Bytes, Limit: taskresource.CacheBytesLimit, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.CacheBytesLimit"}},
		{Name: "resources.cache_total_bytes", Class: Cache, Unit: Bytes, Limit: taskresource.CacheTotalBytesLimit, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.CacheTotalBytesLimit"}},
		{Name: "resources.scratch_total_bytes", Class: Work, Unit: Bytes, Limit: taskresource.ScratchTotalBytesLimit, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.ScratchTotalBytesLimit"}},
		{Name: "resources.disk_reserve_bytes", Class: Buffer, Unit: Bytes, Limit: taskresource.DiskReserveBytesLimit, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.DiskReserveBytesLimit"}},
		// Free space on the daemon's filesystem. Below the limit the person is
		// told once per notice cooldown, with the largest measured sources;
		// coming back above it is only written down.
		{Name: MachineDiskLow, Class: Buffer, Unit: Bytes, Limit: taskresource.LowDiskAlertBytesLimit, AtLimit: Coalesce, Told: []Channel{Diagnostics, Notice}, EvictedBy: Person, QuietRecovery: true, Sources: []string{"internal/domain/taskresource.LowDiskAlertBytesLimit"}},
		{Name: "resources.disk_source_scan_files", Class: Buffer, Unit: Rows, Limit: taskresource.DiskSourceScanFilesLimit, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.DiskSourceScanFilesLimit"}},
		{Name: "resources.page_rows", Class: Buffer, Unit: Rows, Limit: taskresource.PageRowsLimit, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/taskresource.PageRowsLimit"}},
		{Name: ResourceCleanupBatchRows, Class: Buffer, Unit: Rows, Limit: ResourceCleanupBatchRowsLimit, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.ResourceCleanupBatchRowsLimit"}},
		{Name: ResourceCleanupItemSeconds, Class: Buffer, Unit: Seconds, Limit: ResourceCleanupItemSecondsLimit, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.ResourceCleanupItemSecondsLimit"}},
		{Name: ResourceCleanupPlanAttempts, Class: Buffer, Unit: Rows, Limit: ResourceCleanupPlanAttemptsLimit, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.ResourceCleanupPlanAttemptsLimit"}},
		{Name: ResourceCleanupBackoffSeconds, Class: Buffer, Unit: Seconds, Limit: ResourceCleanupBackoffSecondsLimit, AtLimit: Coalesce, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/domain/capacity.ResourceCleanupBackoffSecondsLimit"}},
	}
}
