package capacity

// healthJournalBounds are the cloud.health_store_* rows: the durable,
// segmented connection-health journal (internal/adapters/healthjournal,
// docs/admin-connection-health-storage.md). Like the cloud.health_* rows they
// are served by the cloud area and unmeasured until the daemon opens a store.
// Every limit is the plan's proposed starting bound, not a measured budget.
func healthJournalBounds() []Entry {
	return []Entry{
		{
			// Every byte of the store's own segments. The oldest sealed
			// segment is deleted and its records counted as pruned early;
			// pages carry the actual retention floor, not the target.
			Name: "cloud.health_store_bytes", Class: Journal, Unit: Bytes,
			Limit: 100 << 20, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthjournal.MaxStoreBytes"},
		},
		{
			// The retention target. A sealed segment whose newest record is
			// older is deleted; bytes may cut retention shorter.
			Name: "cloud.health_store_seconds", Class: Journal, Unit: Seconds, RetentionTarget: true,
			Limit: 7 * 24 * 60 * 60, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthjournal.MaxStoreAgeSeconds"},
		},
		{
			// One segment file: sealed, and a new one opened, before a frame
			// would cross it.
			Name: "cloud.health_store_segment_bytes", Class: Journal, Unit: Bytes,
			Limit: 8 << 20, AtLimit: Rotate,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthjournal.MaxSegmentBytes"},
		},
		{
			// Segment files kept. Every boot opens one, so a restart loop
			// cannot leave thousands of tiny files under the byte bound.
			Name: "cloud.health_store_segments", Class: Journal, Unit: Rows,
			Limit: 256, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthjournal.MaxSegments"},
		},
		{
			// Records admitted to memory and not yet written, by records and
			// by bytes. Offer never waits: past either it refuses, counts, and
			// the next write puts a loss marker on disk.
			Name: "cloud.health_store_queue_rows", Class: Buffer, Unit: Rows,
			Limit: 1024, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthjournal.MaxQueueRecords"},
		},
		{
			Name: "cloud.health_store_queue_bytes", Class: Buffer, Unit: Bytes,
			Limit: 2 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthjournal.MaxQueueBytes"},
		},
		{
			// One frame on disk. A record that would not fit is refused; a
			// frame on disk that claims more is corrupt and ends the read.
			Name: "cloud.health_store_frame_bytes", Class: Buffer, Unit: Bytes,
			Limit: 4096, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthjournal.MaxFrameBytes"},
		},
		{
			// Reads running at once; the next is refused, never queued, so
			// readers cannot pile up behind a slow disk.
			Name: "cloud.health_store_readers", Class: Buffer, Unit: Rows,
			Limit: 4, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{
				"internal/adapters/healthjournal.MaxConcurrentReads",
				"internal/adapters/healthjournal.Open:chan(MaxConcurrentReads)",
			},
		},
		{
			// Directory entries looked at when the store opens. The rest are
			// neither read nor deleted, and recovery says truncated.
			Name: "cloud.health_store_dir_entries", Class: Buffer, Unit: Rows,
			Limit: 4096, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthjournal.MaxDirEntries"},
		},
		{
			// Lost ticket ranges a durability check can name. The oldest is
			// forgotten and its tickets answer unknown, never synced.
			Name: "cloud.health_store_lost_ranges", Class: Observation, Unit: Rows,
			Limit: 64, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/healthjournal.MaxLostRanges"},
		},
	}
}
