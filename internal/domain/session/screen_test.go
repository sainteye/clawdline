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

func TestAnOldInterruptLineDoesNotKeepAFinishedTurnWorking(t *testing.T) {
	screen := "• Working (17s • esc to interrupt)\n" +
		strings.Repeat("the completed answer has another line\n", 30) +
		"› Ask Codex to do anything\n"
	if state, sure := ReadState(screen, AssistantCodex); state != StateIdle || !sure {
		t.Fatalf("a finished turn with an old interrupt line: got %v (sure=%v), want idle", state, sure)
	}
}

func TestACurrentWorkingLineStillReportsWorking(t *testing.T) {
	screen := "• Working (17s • esc to interrupt)\n\n› Ask Codex to do anything\n"
	if state, sure := ReadState(screen, AssistantCodex); state != StateWorking || !sure {
		t.Fatalf("a current working line: got %v (sure=%v), want working", state, sure)
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

// Codex draws a person's draft after the same caret, "› some text", and it
// also draws every sent message in the history that way. A finished child
// whose composer held stray text sat as an unrecognised session, drawn as a
// dashed row in the fleet list, for nine hours (2026-10-09).
func TestACodexComposerHoldingADraftIsIdle(t *testing.T) {
	screen := strings.Join([]string{
		"› You are a Clawdline CHILD agent for task 1234. Say this line first.",
		"",
		"• 複審完成，判定 changes_required。",
		"",
		"  Worked for 4m 30s • 8:02 AM",
		"",
		"",
		"› >|iTerm2 3.7.0",
		"",
		"  GPT-6-Sol high · ~/code/project · project · main · No changes · Context 41% used",
		"                                                         ⚠ 1 warning · f2 to view",
	}, "\n")
	if st, ok := ReadState(screen, AssistantCodex); st != StateIdle || !ok {
		t.Fatalf("a Codex composer holding a draft reads %s, want idle", st)
	}
}

// A sent message in the history has the assistant's answer under it, and an
// approval menu marks its choice with the same caret; neither is a composer.
func TestACodexHistoryLineOrMenuIsNotADraft(t *testing.T) {
	history := strings.Join([]string{
		"› please look at the build",
		"",
		"• Ran go build ./...",
		"  └ ok",
	}, "\n")
	if st, _ := ReadState(history, AssistantCodex); st == StateIdle {
		t.Fatalf("a sent message with output under it read as idle")
	}
	menu := strings.Join([]string{
		"  Would you like to run the following command?",
		"",
		"  $ rm -rf build",
		"",
		"› 1. Yes, proceed (y)",
		"  2. No, and tell Codex what to do differently (esc)",
		"",
		"  Press enter to confirm or esc to cancel",
	}, "\n")
	if st, _ := ReadState(menu, AssistantCodex); st == StateIdle {
		t.Fatalf("an approval menu read as idle")
	}
}
