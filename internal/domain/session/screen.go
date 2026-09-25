package session

import "strings"

// ReadState decides what a terminal's visible screen says a session is doing.
//
// It is pure and deliberately conservative. Three answers are supported by what
// a screen can actually show, and anything else is `unknown` — which is a real
// answer, not a failure. Flattening an unreadable screen into `idle` is the one
// wrong answer a fleet list must not give, because "nothing is happening" is
// exactly what somebody acts on.
func ReadState(screen string, assistant Assistant) (State, bool) {
	if strings.TrimSpace(screen) == "" {
		return StateUnknown, false
	}
	// A live line with the assistant's own clock in it is the strongest sign
	// of a running turn, and it is the Swift app's definition of working.
	if WorkingLine(screen, assistant, 25) != "" {
		return StateWorking, true
	}
	lines := strings.Split(Plain(screen), "\n")

	// Both assistants draw the same shape while a turn runs: a bullet, a word
	// and a clock, with the way out named on the same line.
	for _, line := range lines {
		if strings.Contains(line, "esc to interrupt") ||
			strings.Contains(line, "Esc to interrupt") {
			return StateWorking, true
		}
	}

	// An empty composer is positive evidence of an idle session: the assistant
	// has drawn its prompt and is waiting for a person, not for itself.
	//
	// The window is counted from the last row that has anything on it, not from
	// the bottom of the terminal. A tall window holding a short conversation
	// comes back padded with blank rows, and counting those made the newest
	// sessions the hardest ones to read: on 2026-09-21 a session whose only
	// turn had been refused by the model sat 40 rows above the bottom and was
	// reported as an unreadable terminal for 23 minutes.
	end := len(lines) - 1
	for end >= 0 && strings.TrimSpace(lines[end]) == "" {
		end--
	}
	for i := end; i >= 0 && i > end-14; i-- {
		switch assistant {
		case AssistantCodex:
			t := strings.TrimSpace(lines[i])
			if t == "›" || strings.HasPrefix(t, "› Ask Codex to do anything") {
				return StateIdle, true
			}
		case AssistantClaude:
			t := strings.TrimSpace(lines[i])
			// Claude Code puts a no-break space (U+00A0) after its caret.
			// An empty composer trims down to the caret alone; one holding a
			// draft the person has not sent keeps the no-break space, and
			// matching only an ordinary space read that session as unknown
			// for as long as the draft sat there (measured 2026-09-25).
			if t == "❯" || strings.HasPrefix(t, "❯ ") || strings.HasPrefix(t, "❯\u00a0") {
				return StateIdle, true
			}
		}
	}
	return StateUnknown, false
}
