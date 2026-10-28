package capacity

// sessionsBounds is every registered row named sessions.*.
func sessionsBounds() []Entry {
	return []Entry{
		{Name: "sessions.claude_remote_request_seconds", Class: Observation, Unit: Seconds, Limit: 10, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/clauderemote.RequestSecondsLimit"}},
		{Name: "sessions.claude_remote_response_bytes", Class: Buffer, Unit: Bytes, Limit: 8 << 20, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/clauderemote.ResponseBytesLimit"}},
		{
			// Provider-native child work carried on one session row. The newest
			// running work wins; the payload says how many rows were omitted.
			Name: SessionsAgentRows, Class: Observation, Unit: Rows,
			Limit: 6, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/subagents.MaximumShown"},
		},
		{
			// The tail of one background command's output the Shell panel is
			// sent (limits N50). The file is Claude Code's and stays whole:
			// past this only its oldest bytes are left out of the answer,
			// which says `truncated`. Asked of a request, never retained.
			Name: SessionsShellOutputBytes, Class: Observation, Unit: Bytes,
			Limit: 1 << 20, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/transcript.MaxShellOutput"},
		},
		{
			// How many sessions one reading of the machine may ask an
			// activity time of (session.Activity). Each one is a stat of a
			// file the assistant writes anyway, inside the one producer, so
			// the cost is a stat per row per reading and not a transcript
			// read — but a bound that grows with the number of rows is not a
			// bound, and the one thing a list of sessions must survive is a
			// machine with a great many of them.
			//
			// Past the limit the row is not read and says so: its activity
			// comes back `unread`, which the order puts ahead of every known
			// time inside its state rather than below them. That is the whole
			// point of refusing rather than evicting here — a row left unread
			// must not be mistaken for a row that has been quiet, and it is
			// the reader, not this row, that is told.
			Name: SessionsActivityReads, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.ActivityReadLimit"},
		},
		{
			// Conversations one boot records. They are read again from the
			// machine on every complete reading; past the limit the ones that
			// moved longest ago are not recorded and the count says so.
			Name: SessionsRestoreRows, Class: Observation, Unit: Rows,
			Limit: 200, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.restoreRowsLimit"},
		},
		{
			// This boot and the one before it. An older boot's rows can no
			// longer be offered, so they go with it.
			Name: SessionsRestoreBoots, Class: Journal, Unit: Rows,
			Limit: 2, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.restoreBootsLimit"},
		},
		{
			// Conversations one restore may name. A longer list is refused
			// whole, and nothing is opened.
			Name: SessionsRestoreBatch, Class: Buffer, Unit: Rows,
			Limit: 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.restoreBatchLimit"},
		},
		{
			// How stale a recorded last_seen may grow while the set of open
			// conversations is unchanged; past it the next complete reading
			// writes the same set again with the new time.
			Name: SessionsRestoreSeenAge, Class: Cache, Unit: Seconds,
			Limit: 300, AtLimit: Expire,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.restoreSeenLimit"},
		},
		{
			// How stale the boot's own last_seen may grow while complete
			// readings keep arriving with nothing changed; past it the next
			// one moves last_seen alone, one row update. It is what the
			// grace line after a reboot is drawn from.
			Name: SessionsRestoreBeat, Class: Cache, Unit: Seconds,
			Limit: 60, AtLimit: Expire,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.restoreBeatLimit"},
		},
		{
			// How long before the previous boot was last seen a conversation
			// may have gone and still be offered back: a shutdown's final
			// wave. One that went earlier expires from the offer; its row
			// stays until the boot does.
			Name: SessionsRestoreGrace, Class: Cache, Unit: Seconds,
			Limit: 180, AtLimit: Expire,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.restoreGraceLimit"},
		},
		{
			// Archived conversations kept. Each is a row a person asked for,
			// so past the limit the one archived longest ago is dropped and
			// the count says so; its transcript stays in the assistant's own
			// history, resumable from the place's past list while it is
			// still on it.
			Name: SessionsArchiveRows, Class: Journal, Unit: Rows,
			Limit: 500, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.archiveRowsLimit"},
		},
		{
			// Conversations one archive restore may name. A longer list is
			// refused whole, and nothing is opened.
			Name: SessionsArchiveBatch, Class: Buffer, Unit: Rows,
			Limit: 20, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.archiveBatchLimit"},
		},
	}
}
