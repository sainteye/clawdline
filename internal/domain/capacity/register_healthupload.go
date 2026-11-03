package capacity

// healthUploadBounds are the cloud.health_upload_* rows: the daemon's own
// HTTPS uploader of connection-health evidence to Cloud's machine ingest
// (internal/adapters/healthupload, docs/admin-connection-health-contract.md
// §9). The batch and hourly rows mirror Cloud's ingest limits
// (Cloud's own budget table) so the uploader stops
// itself before Cloud refuses it; the rest are this producer's proposals,
// not measured budgets.
func healthUploadBounds() []Entry {
	return []Entry{
		{
			// One request body. Records are taken while the body stays under
			// it; one record that alone would cross it is counted as
			// excluded, never sent truncated.
			Name: "cloud.health_upload_batch_bytes", Class: Buffer, Unit: Bytes,
			Limit: 32 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthupload.MaxBatchBytes"},
		},
		{
			Name: "cloud.health_upload_batch_events", Class: Buffer, Unit: Rows,
			Limit: 100, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthupload.MaxBatchEvents"},
		},
		{
			// Bytes sent per clock hour, every attempt counted: 1 MiB less
			// Cloud's 64 KiB metadata reserve. At the bound the uploader
			// waits for the next hour (quota_local); nothing is dropped.
			Name: "cloud.health_upload_hourly_bytes", Class: Buffer, Unit: Bytes,
			Limit: 1<<20 - 64<<10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthupload.MaxHourlyRoutineBytes", "internal/adapters/healthupload.MaxHourlyBytes"},
		},
		{
			Name: "cloud.health_upload_hourly_requests", Class: Buffer, Unit: Rows,
			Limit: 120, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthupload.MaxHourlyRequests"},
		},
		{
			// Attempts of one batch without a receipt; past it the batch is
			// abandoned and counted (Cloud may or may not hold it).
			Name: "cloud.health_upload_attempts", Class: Idempotency, Unit: Rows,
			Limit: 6, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthupload.MaxAttempts"},
		},
		{
			Name: "cloud.health_upload_retry_seconds", Class: Idempotency, Unit: Seconds,
			Limit: 60 * 60, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthupload.MaxRetrySeconds"},
		},
		{
			// A server's Retry-After is honored up to this.
			Name: "cloud.health_upload_retry_after_seconds", Class: Buffer, Unit: Seconds,
			Limit: 60 * 60, AtLimit: Coalesce,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthupload.MaxRetryAfterSeconds"},
		},
		{
			// One request: dial, TLS, send and answer.
			Name: "cloud.health_upload_attempt_seconds", Class: Buffer, Unit: Seconds,
			Limit: 10, AtLimit: Disconnect,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthupload.MaxAttemptSeconds"},
		},
		{
			// Bytes of an answer read; a longer answer is not a receipt.
			Name: "cloud.health_upload_response_bytes", Class: Buffer, Unit: Bytes,
			Limit: 16 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthupload.MaxResponseBytes"},
		},
		{
			// The progress file: cursor, the one pending body, counters and
			// receipts. A write that would cross it is refused and nothing is
			// sent (state_unwritable).
			Name: "cloud.health_upload_state_bytes", Class: Buffer, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthupload.MaxStateBytes"},
		},
		{
			Name: "cloud.health_upload_receipts", Routine: true, Class: Observation, Unit: Rows,
			Limit: 16, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthupload.MaxReceipts"},
		},
		{
			// Store pages read in one cycle; the rest waits for the next.
			Name: "cloud.health_upload_step_pages", Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthupload.MaxStepPages"},
		},
	}
}
