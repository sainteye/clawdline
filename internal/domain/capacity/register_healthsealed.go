package capacity

// healthSealedBounds are the cloud.health_sealed_* rows: the machine side of
// Cloud's owner-sealed diagnostic attachments (internal/adapters/healthsealed,
// Cloud's docs/connection-health-contract.md §13). The ciphertext, request,
// revision and life rows mirror Cloud's SEALED_LIMITS so the machine stops
// itself before Cloud refuses it; the store, queue and attempt rows are this
// producer's proposals, not measured budgets.
func healthSealedBounds() []Entry {
	return []Entry{
		{
			// ciphertext||tag of one revision; a snapshot that would seal
			// larger is refused (too_large), never truncated.
			Name: "cloud.health_sealed_ciphertext_bytes", Class: Buffer, Unit: Bytes,
			Limit: 256 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxCiphertextBytes"},
		},
		{
			Name: "cloud.health_sealed_request_bytes", Class: Buffer, Unit: Bytes,
			Limit: 400 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxRequestBytes"},
		},
		{
			// Immutable revisions of one attachment; the next is refused
			// (revisions_exhausted).
			Name: "cloud.health_sealed_revisions", Class: Idempotency, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxRevisions"},
		},
		{
			// An attachment's life from its first revision; past it the
			// local record is pruned and Cloud answers 410.
			Name: "cloud.health_sealed_life_seconds", Class: Idempotency, Unit: Seconds,
			Limit: 7 * 24 * 60 * 60, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxLifeSeconds"},
		},
		{
			Name: "cloud.health_sealed_snapshot_bytes", Class: Buffer, Unit: Bytes,
			Limit: 16 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxSnapshotBytes"},
		},
		{
			Name: "cloud.health_sealed_snapshot_field_chars", Class: Buffer, Unit: Characters,
			Limit: 512, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxSnapshotFieldChars"},
		},
		{
			Name: "cloud.health_sealed_snapshot_coverage", Class: Buffer, Unit: Rows,
			Limit: 32, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxSnapshotCoverage"},
		},
		{
			// Local attachment records (each holds its revisions' exact
			// bodies). A new attachment at the bound is refused at once
			// (store_full); expired records are pruned.
			Name: "cloud.health_sealed_store_attachments", Class: Idempotency, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxPendingAttachments"},
		},
		{
			Name: "cloud.health_sealed_store_bytes", Class: Idempotency, Unit: Bytes,
			Limit: 4 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxStoreBytes"},
		},
		{
			// Sends of one revision per Send call; past it the revision
			// stays pending with the same bytes.
			Name: "cloud.health_sealed_attempts", Class: Idempotency, Unit: Rows,
			Limit: 4, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxAttempts"},
		},
		{
			Name: "cloud.health_sealed_attempt_seconds", Class: Buffer, Unit: Seconds,
			Limit: 10, AtLimit: Disconnect,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxAttemptSeconds"},
		},
		{
			Name: "cloud.health_sealed_response_bytes", Class: Buffer, Unit: Bytes,
			Limit: 16 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxResponseBytes"},
		},
		{
			// The worker's hand-off queue; Offer at the bound refuses at
			// once (busy) and counts the job as dropped.
			Name: "cloud.health_sealed_queued_jobs", Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxQueuedJobs", "internal/adapters/healthsealed.NewWorker:chan(MaxQueuedJobs)"},
		},
		{
			// The machine's content-key epoch record, read whole; a larger
			// file is refused and the epoch stays unknown.
			Name: "cloud.content_key_epoch_record_bytes", Class: Buffer, Unit: Bytes,
			Limit: 4 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/cloud.maxEpochRecordBytes"},
		},
		{
			// Polls of one identity-pairing phase; past it the run stops
			// with ErrIdentityPollExhausted and hands nothing over.
			Name: "cloud.identity_pairing_poll_attempts", Class: Buffer, Unit: Rows,
			Limit: 600, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/cloud.MaxIdentityPollAttempts"},
		},
		{
			// The local decision body (a pairing id and a bool); a larger
			// body is refused 400 before it is decoded.
			Name: "cloud.identity_pairing_decision_bytes", Class: Buffer, Unit: Bytes,
			Limit: 1 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxIdentityDecisionBody"},
		},
		{
			// One identity-pairing run on this machine; past it the run is
			// cancelled and the local page reads failed/expired.
			Name: "cloud.identity_pairing_run_seconds", Class: Buffer, Unit: Seconds,
			Limit: 660, AtLimit: Disconnect,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.IdentityPairingLifetimeLimit"},
		},
		{
			// Browser invitations live on one machine at once; the next start
			// is refused (invite_limit) until one finishes or is cancelled.
			Name: "cloud.browser_invites_live", Class: Buffer, Unit: Rows,
			Limit: 3, AtLimit: Refuse,
			Told: []Channel{Sender, Diagnostics}, EvictedBy: Person,
			Sources: []string{"internal/transport/cloud.BrowserInvitesLiveLimit"},
		},
		{
			// Finished invitations kept for the status route; the oldest goes.
			Name: "cloud.browser_invite_history", Class: Progress, Unit: Rows,
			Limit: 8, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.BrowserInviteHistoryLimit"},
		},
		{
			// The name an invited browser asks to be shown by; a longer one is
			// refused and the browser is shown as "Browser".
			Name: "cloud.browser_invite_name_characters", Class: Buffer, Unit: Characters,
			Limit: 64, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.BrowserInviteNameLimit"},
		},
		{
			Name: "cloud.browser_invite_request_bytes", Class: Buffer, Unit: Bytes,
			Limit: 1 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxBrowserInviteBody"},
		},
		{
			// Frames of the stalled goroutine in one /2 owner snapshot; the
			// writer trims further to fit the snapshot bytes and says so.
			Name: "cloud.health_sealed_snapshot_frames", Class: Buffer, Unit: Rows,
			Limit: 24, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxSnapshotFrames"},
		},
		{
			Name: "cloud.health_sealed_snapshot_sources", Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthsealed.MaxSnapshotSources"},
		},
	}
}
