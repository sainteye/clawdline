package capacity

// brokerBounds is every registered row named coordinator.*, dispatch.*,
// handoff.*, leases.*, orchestrator.* and waits.*: the broker's dispatch,
// handoff and coordination rows.
func brokerBounds() []Entry {
	return []Entry{
		{Name: "orchestrator.delivery_bytes", Class: Buffer, Unit: Bytes, Limit: 256 << 10, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/session.DeliveryEvidenceBytesLimit"}},
		{Name: "orchestrator.delivery_commits", Class: Buffer, Unit: Rows, Limit: 4096, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/session.DeliveryEvidenceCommitsLimit"}},
		{
			// The askers waiting for one lease — the compile slot, or one
			// checkout's landing (D06 ②, D20). At the limit a new asker is
			// refused with 429 queue_full and a retry_after, in the answer
			// to its own ask; nobody already in line is let go. A waiter that
			// stops asking is passed over at once and forgotten after half an
			// hour. Used is the longest line.
			Name: LeasesQueue, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app/orchestrator.LeaseQueueLimit"},
		},
		{
			// The machine role's earlier bindings, kept only to route a
			// completion notice for a task an earlier binding dispatched
			// (Coordinator.swift keeps the last 32). A rebind past the limit
			// lets the oldest go; every rebind is also an event in the store,
			// so nothing is lost but the routing.
			Name: CoordinatorAliases, Class: Journal, Unit: Rows,
			Limit: 32, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/domain/coordinator.AliasLimit"},
		},
		{
			// A bind retries a transient unknown inventory reading a few times,
			// then refuses to replace the old holder until liveness is proved.
			Name: CoordinatorBindAttempts, Class: Buffer, Unit: Rows,
			Limit: 6, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"cmd/clawdline.coordinatorBindAttemptLimit"},
		},
		{
			// File waits not yet fully released: one session's paths held,
			// others waiting to be let go. The Swift app had no limit. At
			// this one a new wait is refused with 429 waits_full in the
			// answer to the session asking; an open wait is never dropped —
			// only its owner releases it, or its waiters leave.
			Name: WaitsOpen, Class: Buffer, Unit: Rows,
			Limit: 256, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app/orchestrator.WaitsOpenLimit"},
		},
		{
			// A Root Assignment at terminal_opened, or a handoff at opening,
			// lasts only while its opening request runs, which touches the
			// row at least every 150 seconds. One unchanged for this long was
			// left by a daemon that stopped mid-opening: the beat records it
			// failed with terminal_open_timeout or handoff_open_timeout and
			// an event of that name. Nothing is kept waiting at the limit.
			Name: OpeningStuckSeconds, Class: Observation, Unit: Seconds,
			Limit: 1800, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.OpeningStuckSecondsLimit"},
		},
		{
			// The items one child carries beside its work_id: one branch and
			// one landing for all of them. A dispatch naming more is refused
			// whole with bad_task, never trimmed, because a dropped item is
			// one whose landing would silently not count.
			Name: DispatchAlsoWorkIDs, Class: Buffer, Unit: Rows,
			Limit: 7, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.MaxAlsoWorkIDs"},
		},
		{
			// The worktrees one handoff pack reads, the bytes of one
			// worktree's patch and of all the pack's patches. Past each the
			// pack refuses that copy and says "truncated" and what it left
			// to read by hand; the reassignment itself never fails on it.
			Name: HandoffWorktrees, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.MaxHandoffWorktrees"},
		},
		{
			Name: HandoffPatchBytes, Class: Buffer, Unit: Bytes,
			Limit: 8 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.MaxHandoffPatchBytes"},
		},
		{
			Name: HandoffPackBytes, Class: Buffer, Unit: Bytes,
			Limit: 32 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/orchestrator.MaxHandoffPackBytes"},
		},
	}
}
