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
	lines := strings.Split(screen, "\n")

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
	for i := len(lines) - 1; i >= 0 && i > len(lines)-14; i-- {
		switch assistant {
		case AssistantCodex:
			t := strings.TrimSpace(lines[i])
			if t == "›" || strings.HasPrefix(t, "› Ask Codex to do anything") {
				return StateIdle, true
			}
		case AssistantClaude:
			t := strings.TrimSpace(lines[i])
			if t == "❯" || strings.HasPrefix(t, "❯ ") {
				return StateIdle, true
			}
		}
	}
	return StateUnknown, false
}
