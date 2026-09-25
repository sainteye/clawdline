package session

import (
	"strings"
	"testing"
)

// A terminal is taller than what the assistant has drawn in it, and the rest of
// the rows come back as blanks. A session whose whole conversation is one
// rejected request draws almost nothing, so its composer sits far from the
// bottom — the newest sessions are the hardest to read, which is backwards.
//
// Measured on 2026-09-21: ttys024, 61 rows, the composer 40 rows above the
// bottom, every one of the last 13 rows blank. The fleet list called it
// `terminal_unreadable` and offered nothing to do about it, while the screen
// itself said plainly that the model had refused the request.
func TestAnIdleScreenIsStillIdleUnderAPileOfBlankRows(t *testing.T) {
	drawn := []string{
		"╭────────────────────────────────────────────────╮",
		"│ >_ OpenAI Codex (v0.155.1)                     │",
		"╰────────────────────────────────────────────────╯",
		"",
		"› drops/one.png",
		"",
		"⚠ Selected model is at capacity. Please try a different model.",
		"",
		"› Ask Codex to do anything",
		"",
		"  gpt-5.6-sol high · ~/code/example · master · No changes",
	}
	screen := strings.Join(drawn, "\n") + strings.Repeat("\n", 40)

	state, sure := ReadState(screen, AssistantCodex)
	if state != StateIdle || !sure {
		t.Fatalf("a drawn composer 40 rows above the bottom: got %v (sure=%v), want idle", state, sure)
	}
}

// The same for Claude, whose composer is a different character.
func TestClaudesComposerIsAlsoFoundAboveTheBlankRows(t *testing.T) {
	screen := "❯ \n" + strings.Repeat("\n", 40)
	if state, sure := ReadState(screen, AssistantClaude); state != StateIdle || !sure {
		t.Fatalf("got %v (sure=%v), want idle", state, sure)
	}
}

// The window still has to be a window: a composer scrolled far up behind a lot
// of later output is not evidence that the session is waiting for a person.
func TestAComposerBuriedUnderLaterOutputIsNotIdle(t *testing.T) {
	screen := "› Ask Codex to do anything\n" + strings.Repeat("some later output\n", 40)
	if state, _ := ReadState(screen, AssistantCodex); state == StateIdle {
		t.Fatalf("a composer buried under 40 lines of output read as idle")
	}
}

// An empty screen is unknown, and unknown is an answer.
func TestAnEmptyScreenStaysUnknown(t *testing.T) {
	if state, sure := ReadState("   \n\n  ", AssistantCodex); state != StateUnknown || sure {
		t.Fatalf("got %v (sure=%v), want unknown", state, sure)
	}
}

// Claude Code draws a no-break space after its caret. A composer holding a
// draft the person has not sent keeps it — "❯ keep going" — and that
// session read as unknown for as long as the draft sat there (a root on
// 2026-09-25). The empty composer, "❯ ", was always read as idle because
// trimming removes the no-break space.
func TestAComposerHoldingADraftIsIdle(t *testing.T) {
	rule := strings.Repeat("─", 40)
	screen := strings.Join([]string{
		"  ✅ done",
		"",
		"✻ Churned for 8m 37s · done 10:44 PM",
		"",
		rule,
		"❯ keep going ",
		rule,
		"  ▀▀▀▀▀▀▀▀ project  a root assignment",
		"  ▀▀▀▀▀▀▀▀ ~/code/project  ⎇ main  Opus · medium",
		"  ⏵⏵ auto mode on · 1 shell",
	}, "\n")
	if st, ok := ReadState(screen, AssistantClaude); st != StateIdle || !ok {
		t.Fatalf("a composer holding a draft reads %s, want idle", st)
	}
	empty := strings.Replace(screen, "❯ keep going ", "❯  ", 1)
	if st, _ := ReadState(empty, AssistantClaude); st != StateIdle {
		t.Fatalf("an empty composer reads %s, want idle", st)
	}
}
