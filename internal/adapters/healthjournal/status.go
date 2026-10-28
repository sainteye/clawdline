package healthjournal

import "time"

// Status is the store's state for diagnostics and the capacity rows. It is
// assembled from atomics, a segment-list snapshot and a short lost-range lock.
// It performs no filesystem or network reads.
type Status struct {
	Schema string `json:"schema"`
	// State is ok, disk_full, read_only, permission_denied, io_error,
	// stalled (a write or fsync has run longer than StallAfter) or closed.
	State               string     `json:"state"`
	LastError           Code       `json:"last_error,omitempty"`
	StateSince          time.Time  `json:"state_since"`
	BootID              string     `json:"boot_id"`
	Build               string     `json:"build_commit"`
	CrashLoss           string     `json:"crash_loss_window"`
	Admitted            uint64     `json:"admitted_through"`
	Written             uint64     `json:"written_through"`
	Synced              uint64     `json:"synced_through"`
	QueueRows           int64      `json:"queue_records"`
	QueueBytes          int64      `json:"queue_bytes"`
	Refused             uint64     `json:"refused_queue_full"`
	Invalid             uint64     `json:"refused_invalid"`
	TooLarge            uint64     `json:"refused_too_large"`
	Lost                uint64     `json:"lost_records"`
	DroppedOnClose      uint64     `json:"dropped_at_close"`
	Segments            int        `json:"segments"`
	Bytes               int64      `json:"bytes"`
	LargestSegmentBytes int64      `json:"largest_segment_bytes"`
	LostRanges          int        `json:"lost_ranges"`
	Records             uint64     `json:"records"`
	Floor               *time.Time `json:"retention_floor,omitempty"`
	PrunedAge           uint64     `json:"pruned_segments_age"`
	PrunedBytes         uint64     `json:"pruned_segments_bytes"`
	PrunedCount         uint64     `json:"pruned_segments_count"`
	PrunedRecords       uint64     `json:"pruned_records"`
	PruneFailures       uint64     `json:"prune_failures"`
	Shed                uint64     `json:"shed_on_disk_full"`
	Writes              uint64     `json:"writes"`
	// Readers is how many Reads hold one of the MaxConcurrentReads slots.
	Readers  int      `json:"active_readers"`
	Syncs    uint64   `json:"syncs"`
	Recovery Recovery `json:"recovery"`
}

// Status reads the store now.
func (s *Store) Status() Status {
	h := s.health.Load()
	now := s.clock.Now()
	st := Status{
		Schema: StatusSchema, State: h.state, LastError: h.code, StateSince: h.at,
		BootID: s.bootID, Build: s.build, CrashLoss: CrashLossWindow,
		Admitted: s.st.admittedThrough.Load(), Written: s.st.writtenThrough.Load(), Synced: s.st.syncedThrough.Load(),
		QueueRows: s.st.queueRows.Load(), QueueBytes: s.st.queueBytes.Load(),
		Refused: s.st.refused.Load(), Invalid: s.st.invalid.Load(), TooLarge: s.st.tooLarge.Load(),
		Lost: s.st.lost.Load(), DroppedOnClose: s.st.droppedAtClose.Load(),
		PrunedAge: s.st.prunedAge.Load(), PrunedBytes: s.st.prunedBytes.Load(), PrunedCount: s.st.prunedCount.Load(),
		PrunedRecords: s.st.prunedRecords.Load(), PruneFailures: s.st.pruneFailures.Load(), Shed: s.st.shed.Load(),
		Writes: s.st.writes.Load(), Syncs: s.st.syncs.Load(),
		Recovery: s.recovery, Readers: len(s.readers),
	}
	if started := s.writeStarted.Load(); started != 0 && int64(now.Mono)-started > int64(StallAfter) {
		st.State = StateStalled
	}
	s.lostMu.Lock()
	st.LostRanges = len(s.lost)
	s.lostMu.Unlock()
	segs := s.snapshot()
	st.Segments = len(segs)
	for _, g := range segs {
		size := g.size.Load()
		st.Bytes += size
		st.LargestSegmentBytes = max(st.LargestSegmentBytes, size)
		st.Records += g.records.Load()
	}
	st.Floor = floor(segs)
	return st
}
