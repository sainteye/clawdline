package capacity

// workBounds is every registered row named work.*.
func workBounds() []Entry {
	return []Entry{
		{Name: WorkCardDescriptionRunes, Class: Observation, Unit: Characters,
			Limit: 200, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.workV2CardDescriptionRunesLimit"}},
		{
			// Open work items (design-decisions T3): on the board and not
			// closed, or planned in the Backlog. Each is a person's plan or
			// a commitment somebody made, so nothing is let go at the limit:
			// a new item is refused with 507 work_full, and only a person
			// closes or drops one. Every board item has an exit (a landing,
			// the closure queue, three quiet days back to the Backlog); the
			// Backlog's exit is a person, by design (board-redesign §5.3).
			// Moves are append-only and ride on store.db.
			Name: WorkOpen, Class: Evidence, Unit: Rows,
			Limit: 2_000, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Projects:  true,
			Sources:   []string{"internal/adapters/store.WorkOpenLimit", "internal/adapters/store.WorkV2OpenLimit"},
		},
		{
			// One page of the current Board. The page is an observation, not
			// retained data: rows after this one stay in the store and the
			// answer names the keyset cursor that reads them next. Keeping the
			// first page small also bounds the documents, images, steps and
			// active-claim reads needed before the Console can paint.
			Name: WorkListPageRows, Class: Observation, Unit: Rows,
			Limit: 24, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.WorkV2ListPageLimit"},
		},
		{
			Name: WorkPlanning, Class: Evidence, Unit: Rows,
			Limit: 1_000, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Projects:  true,
			Sources:   []string{"internal/adapters/store.WorkV2PlanningLimit"},
		},
		{
			Name: WorkAssignments, Class: Evidence, Unit: Rows,
			Limit: 4_000, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/store.WorkV2AssignmentLimit"},
		},
		// Every document but the item's one completion_report, which always
		// has its own place on top of these: an item holds at most 32+1.
		{
			Name: WorkDocumentsPerItem, Class: Evidence, Unit: Rows,
			Limit: 32, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/store.WorkV2DocumentLimit"},
		},
		{
			Name: WorkImagesPerItem, Class: Evidence, Unit: Rows,
			Limit: 6, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Projects:  true,
			Sources:   []string{"internal/adapters/store.WorkV2ImageLimit"},
		},
		{
			Name: WorkImageBytes, Class: Evidence, Unit: Bytes,
			Limit: 5 << 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Projects:  true,
			Sources:   []string{"internal/transport/http.workV2ImageByteLimit"},
		},
		{
			Name: WorkImageBytesPerItem, Class: Evidence, Unit: Bytes,
			Limit: 15 << 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Projects:  true,
			Sources:   []string{"internal/adapters/store.WorkV2ImageItemLimit"},
		},
		{
			Name: WorkImageBytesTotal, Class: Evidence, Unit: Bytes,
			Limit: 512 << 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/store.WorkV2ImageTotalLimit"},
		},
		{
			Name: WorkImageRequestBodyBytes, Class: Buffer, Unit: Bytes,
			Limit: 18 << 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Projects:  true,
			Sources:   []string{"internal/transport/http.workV2ImageBodyLimit", "internal/app/cloudops.workV2CloudImageBodyLimit"},
		},
		{
			Name: WorkStepsPerItem, Class: Evidence, Unit: Rows,
			Limit: 128, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/store.WorkV2StepLimit"},
		},
		{
			// The broker's root landing rows one Board item keeps, and the
			// legacy landing copies its read lists (internal/adapters/store/root_landings.go).
			Name: WorkRootLandingsPerItem, Class: Evidence, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/store.WorkV2RootLandingLimit"},
		},
		{
			Name: WorkItemTitleBytes, Class: Evidence, Unit: Bytes,
			Limit: 240, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person, Projects: true,
			Sources: []string{"internal/app.workV2TitleLimit"},
		},
		{
			Name: WorkItemDescriptionBytes, Class: Evidence, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person, Projects: true,
			Sources: []string{"internal/app.workV2DescriptionLimit", "cmd/clawdline.itemTextLimit"},
		},
		{
			Name: WorkItemUserActionBytes, Class: Evidence, Unit: Bytes,
			Limit: 8 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person, Projects: true,
			Sources: []string{"internal/app.workV2UserActionLimit"},
		},
		{
			Name: WorkCompletionReasonBytes, Class: Evidence, Unit: Bytes,
			Limit: 8 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person, Projects: true,
			Sources: []string{"internal/app.workV2CompletionReasonLimit"},
		},
		{
			Name: WorkRequestBodyBytes, Class: Buffer, Unit: Bytes,
			Limit: 96 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.workV2BodyLimit", "internal/app/cloudops.workV2CloudBodyLimit",
				"cmd/clawdline.todoInputLimit"},
		},
		{
			// The daily and weekly digests (T4, board-redesign §8), one row
			// each: about two years of them. Past the limit the oldest is let
			// go in the transaction that writes the newest; every fact one
			// summarised is still in moves, proposals and decisions.
			Name: WorkDigests, Class: Journal, Unit: Rows,
			Limit: 800, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/store.DigestKeepLimit"},
		},
	}
}
