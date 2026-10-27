package capacity

// sessionBounds is every registered row named session.*, naming.*, schedule.*
// and snippets.*.
func sessionBounds() []Entry {
	return []Entry{
		{Name: "session.tool_preview_bytes", Class: Observation, Unit: Bytes, Limit: 1024, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/transcript.ToolPreviewBytesLimit"}},
		{
			// The sheet reveals retained runs in batches; older rows remain available.
			// TestScheduleHistoryPageIsRegistered checks the console's matching bound.
			Name: "schedule.history_page_rows", Class: Observation, Unit: Rows,
			Limit: 10, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// One provider page per explicit history request; older rows remain on
			// disk and are reached only by following the opaque continuation.
			Name: SessionCodexPastPage, Class: Observation, Unit: Rows,
			Limit: 20, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projects.CodexPastPageLimit"},
		},
		{
			// One durable identity per terminal id on this machine. Full
			// refuses a newly observed id; no generation is silently evicted.
			Name: SessionExecutions, Class: Evidence, Unit: Rows,
			Limit: 4096, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Health, Sender, Log}, EvictedBy: Person,
			Sources: []string{"internal/adapters/store.ExecutionRecordsLimit"},
		},
		{
			// A current record-movement observation stops being recent after
			// this age. The status publisher marks no_movement and tells viewers
			// the threshold; an unknown reading never becomes a quiet one.
			Name: SessionNoMovement, Class: Cache, Unit: Seconds,
			Limit: 1800, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/cloud.SessionNoMovementSecondsLimit"},
		},
		{
			// The live tasks one waiting Session row names. The first ones in
			// store order are kept; the row counts the rest it left out.
			Name: SessionWaitingFor, Class: Observation, Unit: Rows,
			Limit: 8, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/http.MaximumWaitTargets"},
		},
		{
			// The Claude transcripts of one project opened, newest first, to
			// match finished scheduled runs to their children's conversations
			// for the history sheet and the resume route. Asked of a request,
			// never retained: past it the older transcripts are not opened,
			// and a run among them offers no resume.
			Name: ScheduleRunTranscriptsProbed, Class: Observation, Unit: Rows,
			Limit: 256, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/projects.MaxChildTranscriptsProbed"},
		},
		{
			// How much of one transcript is read to find its first message.
			// The file is Claude Code's and stays whole; a first message past
			// this is not found, and that run offers no resume.
			Name: ScheduleRunTranscriptHead, Class: Observation, Unit: Bytes,
			Limit: 64 << 10, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/projects.MaxChildTranscriptHead"},
		},
		{
			Name: SessionDirectTodos, Class: Evidence, Unit: Rows,
			Limit: 500, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/store.DirectTodoV2Limit"},
		},
		{
			Name: HumanInterventionsOpen, Class: Evidence, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person,
			Sources: []string{"internal/adapters/store.HumanInterventionsOpenLimit"},
		},
		{
			Name: HumanInterventionsTotal, Class: Progress, Unit: Rows,
			Limit: 2000, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/store.HumanInterventionsTotalLimit"},
		},
		{
			Name: HumanInterventionsRecent, Class: Cache, Unit: Rows,
			Limit: 5, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/store.HumanInterventionsRecentLimit"},
		},
		{
			Name: HumanInterventionTitle, Class: Buffer, Unit: Bytes,
			Limit: 120, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.humanInterventionTitleLimit"},
		},
		{
			Name: HumanInterventionText, Class: Buffer, Unit: Bytes,
			Limit: 500, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.humanInterventionTextLimit"},
		},
		{
			Name: HumanInterventionDetail, Class: Buffer, Unit: Bytes,
			Limit: 16 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.humanInterventionDetailLimit"},
		},
		{
			Name: HumanInterventionDocument, Class: Buffer, Unit: Bytes,
			Limit: 2 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.humanInterventionDocumentLimit"},
		},
		{
			Name: HumanInterventionDraft, Class: Buffer, Unit: Bytes,
			Limit: 2 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.humanInterventionDraftLimit"},
		},
		{
			Name: SecretHandoffBytes, Class: Buffer, Unit: Bytes,
			Limit: 16 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.MaxSecretHandoffBytes"},
		},
		{
			Name: SecretHandoffPending, Class: Buffer, Unit: Rows,
			Limit: 128, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.MaxSecretHandoffPending"},
		},
		{
			Name: SecretHandoffSeconds, Class: Idempotency, Unit: Seconds,
			Limit: 15 * 60, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.MaxSecretHandoffSeconds"},
		},
		{
			Name: SecretHandoffDeliverySeconds, Class: Buffer, Unit: Seconds,
			Limit: 5, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.MaxSecretHandoffDeliverySeconds"},
		},
		{
			Name: SessionDirectTodoBytes, Class: Evidence, Unit: Bytes,
			Limit: 8 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person,
			Sources: []string{"internal/app.directTodoTextLimit"},
		},
		{
			// The rows one Session may add to its own to-do list in one call.
			// The whole batch is written or none of it is.
			Name: SessionTodoBatchRows, Class: Buffer, Unit: Rows,
			Limit: 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.sessionTodoBatchLimit"},
		},
		{
			// The delivered, unfinished to-dos one turn receipt lists
			// (work-system-v2 §11.2). A row past it is not admitted to the
			// answer, which says so with open_todos_truncated; nothing is
			// removed from the store.
			Name: ReportOpenTodoRows, Class: Buffer, Unit: Rows,
			Limit: 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.reportOpenTodoLimit"},
		},
		{
			// How much of each listed to-do's text that receipt repeats, on
			// one line and ending in an ellipsis when cut; the id names the
			// whole row.
			Name: ReportOpenTodoCharacters, Class: Buffer, Unit: Characters,
			Limit: 120, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app.reportOpenTodoTextLimit"},
		},
		{
			// Every snippet on this machine. Each one is a piece of text a
			// person wrote, so nothing is let go at the limit: one more is
			// refused with 409 `snippet_limit_reached`, in the answer to the
			// sheet that asked, and only a person deletes one. The bytes they
			// hold are bounded per field at the door (internal/domain/snippet)
			// and accumulate on store.db, which has its own row.
			Name: SnippetsTotal, Class: Evidence, Unit: Rows,
			Limit: 100, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Projects:  true,
			Sources:   []string{"internal/adapters/store.SnippetTotalLimit"},
		},
		{
			// One group of them: every project, or one project. The second
			// bound exists because the first is reached by a hundred snippets
			// anywhere, and a list of fifty in one sheet is already longer than
			// anybody scrolls. Refused the same way, and Used is the fullest
			// group, named in the reading.
			Name: SnippetsScope, Class: Evidence, Unit: Rows,
			Limit: 50, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Projects:  true,
			Sources:   []string{"internal/adapters/store.SnippetScopeLimit"},
		},
		{
			// One title write. The body holds one short string; anything larger
			// is refused before it is decoded or copied into the settings file.
			Name: SessionTitleRequestBytes, Class: Buffer, Unit: Bytes,
			Limit: 16 << 10, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
		},
		{
			// A session title is one visible line. Control characters and runs of
			// whitespace are normalized before this character count is applied.
			Name: SessionTitleCharacters, Class: Buffer, Unit: Characters,
			Limit: 200, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
		},
		{
			// Manual session names are user input. The oldest row gives way only
			// after the newer write is admitted, as the retired app did.
			Name: SessionTitleRows, Class: UserInput, Unit: Rows,
			Limit: 200, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
		},
		{
			// A terminal can outlive many conversations. A manual title not seen
			// for ninety days gives way so it cannot name a later occupant.
			Name: SessionTitleAge, Class: UserInput, Unit: Seconds,
			Limit: 90 * 24 * 60 * 60, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
		},
		{
			// One smart-title turn reads the session's resolved opening
			// request, the newest later requests that fit, and an excerpt of
			// the latest reply. Older later requests give way first.
			Name: NamingContextBytes, Class: Observation, Unit: Bytes,
			Limit: 12 << 10, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/http.namingContextLimit"},
		},
		{
			// The newest transcript rows a smart-title turn looks through for
			// later requests and the latest reply. Older rows are not read.
			Name: NamingTailEntries, Class: Observation, Unit: Rows,
			Limit: 400, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/http.namingTailLimit"},
		},
		{
			// Closes Sessions asked for themselves while still working. They
			// live in memory; a restart drops them. One past the cap is
			// refused close_schedule_full and nothing is waiting for it.
			Name: SessionCloseScheduled, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxScheduledCloses"},
		},
		{
			// A repeat for the same terminal is the same request: it answers
			// the same 202 and adds nothing.
			Name: SessionCloseScheduledPerTerminal, Class: Buffer, Unit: Rows,
			Limit: 1, AtLimit: Coalesce,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxScheduledClosesPerTerminal"},
		},
		{
			// A terminal still working this long after its close was asked
			// for has its request dropped, with a session.close_schedule_dropped
			// event; the agent can ask again.
			Name: SessionCloseScheduledSeconds, Class: Cache, Unit: Seconds,
			Limit: 900, AtLimit: Expire,
			Told: []Channel{Diagnostics, Log}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxScheduledCloseWait"},
		},
	}
}
