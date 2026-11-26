package capacity

// boardBounds is every registered row named board.*, decisions.*, epic.*,
// planreview.*, proposals.*, run.* and usage.*: the Board's own small rows
// beside the work items.
func boardBounds() []Entry {
	return append(usageOverviewBounds(), []Entry{
		{
			// The board commands' (actor, requestId) receipts (limits N24):
			// the store's receipt table, scope `board`, since the board's
			// settings moved into clawdline.sqlite3 (D37, T3). They expire by
			// time, and a full window refuses a new command with 429 rather
			// than evicting a receipt somebody may still retry against.
			Name: BoardReceipts, Class: Idempotency, Unit: Rows,
			Limit: 4_096, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
			Projects:  true,
		},
		{
			// The work-unit cursors (store/work_cursors.go): a journal of
			// readings, the oldest let go past the limit in the write that
			// adds one. The ledger's own rows keep every session's
			// cumulative totals; what goes is how an old unit was split.
			Name: UsageWorkCursorRows, Class: Journal, Unit: Rows,
			Limit: 50_000, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/store.WorkCursorRowLimit"},
		},
		{
			// Edges posted by the broker and the Board, waiting for the
			// cursor worker. Past the limit an edge is not read: it is
			// recorded cursor_missing, which every report shows.
			Name: UsageWorkCursorQueue, Class: Buffer, Unit: Rows,
			Limit: 1024, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Log}, EvictedBy: Daemon,
			Sources: []string{"internal/app.workCursorQueueLimit"},
		},
		{
			// Units one GET /v1/usage/work-units reads, the most recent
			// first; the answer says truncated past it.
			Name: UsageWorkUnitsInAnswer, Class: Observation, Unit: Rows,
			Limit: 500, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.workUnitAnswerLimit"},
		},
		{
			// The Board items one person's message, or one Bot question card
			// the person answered, may back when a Session or that Bot
			// creates them on it (work-system-v2 §2, amended 2026-09-25).
			// Counted over every item that run or card created, open or
			// closed; the sixth is refused run_items_exhausted and nothing
			// is written.
			Name: RunCreatedItems, Class: Buffer, Unit: Rows,
			Limit: 5, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.runItemLimit"},
		},
		{
			// How long the person's answer to a Bot's question card backs a
			// Board item the Bot creates on it: as long as a person's
			// message is relayed (work.RelayWindow, one day). An older
			// answer is refused decision_expired and nothing is written.
			Name: RunDecisionAnswerAge, Class: Cache, Unit: Seconds,
			Limit: 86400, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.decisionAnswerAgeLimit"},
		},
		{
			// The Board items one person's message may have a Session claim
			// on it, or assign to a new Session on it, mirroring
			// run.created_items. Counted over every assignment made on that
			// run, whatever its state; the sixth is refused
			// run_claims_exhausted and nothing is written.
			Name: RunClaimedItems, Class: Buffer, Unit: Rows,
			Limit: 5, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.runClaimLimit"},
		},
		{
			// The child items one Epic's owner Session may break it into
			// (work-system-v2 §6.5). Counted over every child of that Epic,
			// open or closed; the thirty-third is refused epic_children_full
			// and nothing is written.
			Name: EpicChildItems, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/work.EpicChildLimit"},
		},
		{
			// The blocking findings a planning-gate refusal names
			// (*_plan_review_blocking). The rest are counted, "and N more";
			// the whole receipt stays on the review task. The findings past
			// the limit coalesce into that one count.
			Name: PlanReviewBlockingListed, Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Coalesce,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/work.PlanReviewBlockingListLimit"},
		},
		{
			// Proposals waiting for a person's answer — the "to confirm"
			// area (design-decisions T4, board-redesign §4.4). At the limit a
			// new proposal is refused with 429 proposals_full in the answer
			// to the session that made it; none waiting is let go early. The
			// work it was about stays with its to-dos, which is also what an
			// unanswered proposal comes to: each one waiting leaves after
			// seven days with that answer (§10 #4). Answered and expired
			// proposals are a person's answers and ride on store.db.
			Name: ProposalsOpen, Class: Buffer, Unit: Rows,
			Limit: 500, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/store.ProposalOpenLimit", "internal/adapters/store.WorkV2ProposalLimit"},
		},
		{
			// Decisions a session asked and nobody has answered (T4). At the
			// limit a new one is refused with 429 decisions_full in the answer
			// to the session asking; none open is let go early. Each leaves
			// by its due — at most seven days — with the default it named.
			Name: DecisionsOpen, Class: Buffer, Unit: Rows,
			Limit: 256, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/store.DecisionOpenLimit"},
		},
	}...)
}
