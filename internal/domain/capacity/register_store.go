package capacity

// storeBounds is every registered row named store.*.
func storeBounds() []Entry {
	return []Entry{
		{
			Name: StoreAntigravityProjectConsents, Class: Evidence, Unit: Rows,
			Limit: 1024, AtLimit: Refuse, Told: []Channel{Diagnostics, Notice, Health}, EvictedBy: Person,
			Sources: []string{"internal/adapters/store.AntigravityProjectConsentLimit"},
		},
		{
			Name: StoreAntigravityConsentRootBytes, Class: Buffer, Unit: Bytes,
			Limit: 4096, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/store.AntigravityConsentRootBytesLimit"},
		},
		{
			Name: StoreAntigravityConsentIdentityBytes, Class: Buffer, Unit: Bytes,
			Limit: 128, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/store.AntigravityConsentIdentityBytesLimit"},
		},
		{
			// clawdline.sqlite3 with its -wal and -shm. Evidence: the broker's
			// task records and landings are in it.
			Name: StoreDB, Class: Evidence, Unit: Bytes,
			Limit: 1 << 30, AtLimit: Nothing,
			Deviation: "limits §7.1: C1 only measures this row. Its class refuses new writes at the limit (limits §4.4 step 5, 507 storage_exhausted); nothing refuses yet, and ok:false on /v1/health is the only effect of full.",
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Projects:  true,
		},
		{
			// The store's events table, the operational journal. Rows past
			// EventAgeLimit are deleted except the newest per subject of each
			// kind a program reads back, and that newest row goes too once its
			// subject has been silent for EventKeptAgeLimit; past this many rows
			// the oldest of the rest go first, whatever their age. Pruned in
			// batches on the reference-image sweep's clock (store.MaintainEvery).
			Name: StoreEvents, Class: Journal, Unit: Rows,
			Limit: 150_000, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources: []string{"internal/adapters/store.EventAgeLimit", "internal/adapters/store.EventKeepLimit",
				"internal/adapters/store.EventPruneBatchLimit", "internal/adapters/store.EventKeptAgeLimit"},
		},
		{
			// Rows one delete of Open's cleanup of finished Bot turns'
			// process removes (streamed chunks, tool steps, their runtime
			// receipts, superseded roster rows); the cleanup repeats until
			// nothing is left, one short transaction at a time.
			Name: StoreBotProcessPruneBatch, Class: Buffer, Unit: Rows,
			Limit: 5_000, AtLimit: Coalesce,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/store.BotProcessPruneBatchLimit"},
		},
		{
			// Free bytes in clawdline.sqlite3 at which Open gives the space
			// back with VACUUM after that cleanup; below it the space is
			// reused by later writes.
			Name: StoreBotProcessVacuumFree, Class: Buffer, Unit: Bytes,
			Limit: 64 << 20, AtLimit: Coalesce,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/store.BotProcessVacuumFreeLimit"},
		},
		{
			// How long Open spends on that cleanup before the maintenance
			// clock takes the rest, well inside the updater's health wait.
			Name: StoreBotProcessOpenBudget, Class: Buffer, Unit: Seconds,
			Limit: 15, AtLimit: Coalesce,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/store.BotProcessOpenBudgetLimit"},
		},
		{
			// The store's request receipts (D03, W2): per scope, inside each
			// receipt's window. At the limit a new request in that scope is
			// refused with a Retry-After; nothing inside its window is
			// evicted, and an answered receipt past it keeps only its key and
			// digest. Used is the fullest scope.
			Name: StoreReceipts, Class: Idempotency, Unit: Rows,
			Limit: 4_096, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/store.ReceiptLimit"},
		},
		{
			// A done outbox row is let go a week after it finished, except the
			// newest row of each subject for the two kinds read by subject
			// (capacity.push, session.pause.delivery). Failed and unknown rows
			// stay.
			Name: StoreDoneEffectRetention, Class: Journal, Unit: Seconds,
			Limit: 604_800, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/store.DoneEffectRetentionSecondsLimit"},
		},
		{
			// An expired receipt with a random key is deleted a week after its
			// request was first claimed, well past the longest resend (a saved
			// console card, 24 hours). A CLI command retried with the same --key
			// after this runs again; that is the horizon, and the row's copy
			// says so. A key derived from the request's content keeps its
			// tombstone for good.
			Name: StoreReceiptTombstoneRetention, Class: Idempotency, Unit: Seconds,
			Limit: 604_800, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/store.ReceiptTombstoneRetentionSecondsLimit"},
		},
		{
			// The most rows one maintenance pass prunes from one table; the
			// oldest go first and the rest wait for the next pass.
			Name: StoreRetentionPruneBatch, Class: Journal, Unit: Rows,
			Limit: 500, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/store.RetentionPruneBatchLimit"},
		},
		{
			// The store's read-only connections (internal/adapters/store,
			// sqlite.go openReader). Reads made outside a write use them, so
			// they no longer queue behind the one write connection; past the
			// limit a reader waits for one on its own context and is refused
			// when that context ends. Writes never wait for a reader.
			Name: StoreReadConnections, Class: Buffer, Unit: Rows,
			Limit: 4, AtLimit: Refuse,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/store.ReadConnectionsLimit"},
		},
	}
}
