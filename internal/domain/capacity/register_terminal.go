package capacity

// terminalBounds is every registered row named terminal.*.
func terminalBounds() []Entry {
	return []Entry{
		{
			// Project directories that keep a login shell ready for the next
			// Session started there (terminal/warm.go). Past it the directory
			// started in longest ago loses its shell; a start there is cold.
			Name: "terminal.warm_shells", Class: Observation, Unit: Rows,
			Limit: 3, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/terminal.MaxWarmShells"},
		},
		{
			// How long a ready shell is kept before it is closed and started
			// again, so edits to rc files reach the next Session.
			Name: "terminal.warm_shell_seconds", Class: Observation, Unit: Seconds,
			Limit: 30 * 60, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/terminal.WarmShellSecondsLimit"},
		},
		{
			// Terminals open on this machine's own tmux server (limits N59).
			// Each is a person's running shell, so none is let go at the
			// limit: the next open is refused with terminals_full, and only
			// a person closes one.
			Name: TerminalCount, Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Person,
			Sources: []string{"internal/domain/terminal.MaxTerminals"},
		},
		{
			// One batch of keystrokes. Past it the batch is refused with
			// input_too_large before a byte is typed; the sender splits.
			Name: TerminalInputBytes, Class: Buffer, Unit: Bytes,
			Limit: 4 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/terminal.MaxInputBytes"},
		},
		{
			// One paste, the same way.
			Name: TerminalPasteBytes, Class: Buffer, Unit: Bytes,
			Limit: 1 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/terminal.MaxPasteBytes"},
		},
		{
			// One read of a terminal's history. A larger ask is lowered to
			// this; the older lines stay in the server's own history.
			Name: TerminalHistoryLines, Class: Observation, Unit: Rows,
			Limit: 2000, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/domain/terminal.MaxHistoryLines"},
		},
		{
			// Inputs and pastes being typed into the machine's terminals, and
			// the ones waiting behind them (limits N60). Terminals have lanes
			// of their own and never take one of the Agent lanes' sixteen.
			// The next is refused terminal_busy with nothing typed; the
			// sender retries the same number.
			Name: TerminalLane, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/terminals.LaneLimit"},
		},
		{
			// Streams one terminal serves at once. The next viewer is refused
			// terminal_viewers_full; nobody watching is cut off for it.
			Name: TerminalViewers, Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/terminals.MaxViewers"},
		},
		{
			// Terminal streams the machine serves at once, the same way.
			Name: TerminalStreams, Class: Buffer, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/terminals.MaxStreams"},
		},
		{
			// How long a control lease lasts unrenewed. Past it the lease is
			// lapsed: its holder's next input is lease_expired, and anybody
			// let in may acquire it.
			Name: TerminalLeaseSeconds, Class: Cache, Unit: Seconds,
			Limit: 30, AtLimit: Expire,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/terminals.MaxLeaseSeconds"},
		},
		{
			// One terminal request's body: a 1 MiB paste JSON-escaped, and
			// the fields beside it. Larger is refused 413 before it is read.
			Name: TerminalBodyBytes, Class: Buffer, Unit: Bytes,
			Limit: 6<<20 + 4<<10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.terminalBodyLimit"},
		},
		{
			// One line typed into a new iTerm2 tab or tmux pane to start an
			// assistant. Its shell has not taken the tty yet, and macOS keeps
			// 1024 bytes of a line in canonical mode: a longer one arrived cut
			// off and started nothing. A line past this is never typed; it is
			// written to a script and the short line that runs it is typed.
			Name: TerminalLaunchLineBytes, Class: Buffer, Unit: Bytes,
			Limit: 512, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/terminal.MaxTypedLaunchBytes"},
		},
		{
			// Those scripts. Each removes itself when its tab runs it; one a
			// tab never ran stays, and the oldest go first.
			Name: TerminalLaunchScripts, Class: Work, Unit: Rows,
			Limit: 64, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/terminal.MaxLaunchScripts"},
		},
	}
}
