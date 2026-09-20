package orchestrator

import (
	"os"
	"strings"
	"testing"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// The screens under testdata/ are captures, `tmux capture-pane -p -J`, of a
// pane 120 columns by 40 rows.
//
// `claude-trust` is the screen that cost a child its life on 2026-09-18: the
// broker saw an assistant on a tty, typed the briefing, and the Return at the
// end of it answered the dialog — which was highlighting "No, exit".
// `claude-composer` is a running Claude Code session, captured the same way.
//
// The `codex-*` screens are codex-cli 0.155.1, captured on 2026-09-20 from a
// disposable session on a private tmux server: the first screen of a fresh run
// in a directory Codex has not been told about, the composer it draws once it
// has, the same composer while a turn runs, its own `/model` picker, and the
// composer again in the eighty columns tmux gives a new session, where the
// status bar is cut off after its first field. Their directories and every word
// anybody typed into them are this test's own.
//
// **They are kept as files rather than as string constants because the blank
// rows matter.** Codex draws thirteen rows into a forty-row tab and the other
// twenty-seven are empty; a reading that looked at the last twenty rows of the
// capture saw none of the thirteen, and that is the whole of why no Codex child
// was ever briefed. A Go raw string would hold the same rows and show nobody
// they were there.
func screen(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name + ".txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// What each captured screen is, to each assistant's reading of it.
func TestTheTwoCLIsDrawTheirOwnComposer(t *testing.T) {
	for _, c := range []struct {
		screen    string
		assistant session.Assistant
		ready     bool
		choosing  bool
		why       string
	}{
		{"claude-composer", session.AssistantClaude, true, false,
			"a drawn Claude Code composer, so no child would ever be briefed"},
		{"claude-trust", session.AssistantClaude, false, true,
			"the workspace-trust dialog, whose highlighted answer is \"No, exit\""},
		{"codex-composer", session.AssistantCodex, true, false,
			"the composer of a fresh Codex session, which is every Codex child's first screen"},
		{"codex-composer-narrow", session.AssistantCodex, true, false,
			"the same composer in the eighty columns tmux gives a new session, its status bar cut after one field"},
		{"codex-working", session.AssistantCodex, true, false,
			"the composer Codex keeps drawn while a turn of its own runs"},
		{"codex-trust", session.AssistantCodex, false, true,
			"Codex asking whether to trust the directory, before it has drawn anything else"},
		{"codex-picker", session.AssistantCodex, false, true,
			"Codex's own model picker, which it draws where the composer was"},
	} {
		s := screen(t, c.screen)
		if got := ComposerReady(s, c.assistant); got != c.ready {
			t.Errorf("%s read as ready=%v: it is %s", c.screen, got, c.why)
		}
		if got := Choosing(s, c.assistant); got != c.choosing {
			t.Errorf("%s read as choosing=%v: it is %s", c.screen, got, c.why)
		}
	}
}

// Neither CLI's furniture is the other's.
//
// This is the fault that stopped every Codex dispatch on this machine: the one
// reading there was asked for a box rule under the caret, which is Claude
// Code's composer and nothing Codex draws, so a live Codex composer was read as
// neither ready nor choosing for the whole 90 seconds of the briefing and the
// task was recorded as "the child session did not reach a prompt".
//
// The other direction is the reason the two are separate rather than joined by
// an `or`: Codex's status bar under a Claude Code screen would be somebody's
// prose, and a briefing typed on the strength of it would land in a dialog.
func TestOneCLIsFurnitureIsNotTheOthers(t *testing.T) {
	if ComposerReady(screen(t, "codex-composer"), session.AssistantClaude) {
		t.Error("a Codex composer passed Claude Code's reading, which asks for a frame Codex never draws")
	}
	if ComposerReady(screen(t, "claude-composer"), session.AssistantCodex) {
		t.Error("a Claude Code composer passed Codex's reading, which asks for a status bar it never draws")
	}
}

// A caret twenty-nine rows above the bottom of the tab is still a caret.
//
// Both screens below are read by counting rows that have something on them,
// because a capture is the whole pane: Codex's composer sits on row 11 of 40,
// and Claude Code's trust dialog sits just as far up when the tab is taller
// than the dialog. Counting rows instead — the first version of this — reached
// neither, and answered no to both questions, which is the one pair of answers
// that decides nothing at all: not ready, so never briefed; not choosing, so
// never reported as holding a dialog either.
func TestBlankRowsUnderAScreenAreNotTheScreen(t *testing.T) {
	codex := screen(t, "codex-composer")
	if rows := strings.Count(codex, "\n"); rows < 30 {
		t.Fatalf("the capture is %d rows; it is meant to be a whole 40-row pane with the session at the top", rows)
	}
	if !ComposerReady(codex, session.AssistantCodex) {
		t.Error("a Codex composer with the rest of the tab empty under it was not found")
	}

	// The same tab, one dialog taller than its contents. The padding is this
	// test's own; the dialog above it is the capture.
	padded := screen(t, "claude-trust") + strings.Repeat("\n", 20)
	if ComposerReady(padded, session.AssistantClaude) {
		t.Error("a trust dialog in a tall tab was read as a composer; the briefing would answer it")
	}
	if !Choosing(padded, session.AssistantClaude) {
		t.Error("a trust dialog in a tall tab was not recognised as something waiting to be answered")
	}
}

// Codex prints middle dots in prose of its own. While a turn runs, three rows
// above the composer, it draws
//
//	"• Working (17s • esc to interrupt) · 1 background terminal running · …"
//
// which is a row of dot-divided fields under a caret and is not the status bar.
// Two things say so: the bar is the lowest thing Codex draws, and that row
// begins with the marker every turn of the session's own begins with.
func TestOnlyTheLowestRowIsCodexsStatusBar(t *testing.T) {
	working := screen(t, "codex-working")
	rows := strings.Split(working, "\n")
	found := false
	for _, row := range rows {
		if strings.Contains(row, "background terminal running") {
			found = true
			if isStatusBar(row) {
				t.Error("a turn of the session's own was read as the status bar under a composer")
			}
		}
	}
	if !found {
		t.Fatal("the captured working screen no longer holds the dot-divided row this test is about")
	}
	// Cut the capture off directly under that row: the composer and the real
	// bar are gone, and what is left is a caret with prose under it.
	cut := []string{}
	for _, row := range rows {
		cut = append(cut, row)
		if strings.Contains(row, "background terminal running") {
			break
		}
	}
	if ComposerReady(strings.Join(cut, "\n"), session.AssistantCodex) {
		t.Error("a screen whose lowest row is a turn of the session's own was read as a composer")
	}
}

// A screen with no caret at all is a session still starting up. Neither answer
// may be yes: typing at it drops the bytes, and calling it a chooser would stop
// the briefing that is about to become possible.
func TestAStartingSessionIsNeitherReadyNorChoosing(t *testing.T) {
	for _, assistant := range []session.Assistant{session.AssistantClaude, session.AssistantCodex, ""} {
		s := "Loading…\n\n  ▀ ▄ ▀ starting"
		if ComposerReady(s, assistant) {
			t.Errorf("%q: a starting session was read as ready", assistant)
		}
		if Choosing(s, assistant) {
			t.Errorf("%q: a starting session was read as a chooser", assistant)
		}
	}
}

// An assistant this broker was not told the name of is read for a frame: the
// reading every assistant got before Codex was looked at.
//
// Reading it for either furniture instead is what made this test: the lowest
// row of Claude Code's trust dialog is `Enter to confirm · Esc to cancel`,
// divided by a middle dot exactly as Codex's status bar is, so a screen whose
// highlighted answer is "No, exit" would have been read as ready to be typed
// into. Every dispatch, hand-over and assignment records its CLI, so what this
// branch costs is a Codex session nobody named waiting for a frame — a delay,
// which is the side to be wrong on.
func TestAnUnnamedAssistantIsReadForAFrame(t *testing.T) {
	if !ComposerReady(screen(t, "claude-composer"), "") {
		t.Error("a framed composer was not read by a broker that was not told which CLI drew it")
	}
	for _, name := range []string{"claude-trust", "codex-trust", "codex-picker"} {
		if !Choosing(screen(t, name), "") {
			t.Errorf("%s was not read as a dialog by a broker that was not told which CLI drew it", name)
		}
	}
}

// Four minutes of silence from a child.
//
// The first version of this clock read silence as failure, and task
// 69c21384 — a child that had been briefed, was alive, and had simply been
// told not to send heartbeat notes — was recorded as `spawn_failed` while its
// tab sat there working. Silence is not evidence; the tab is, and "the tab is
// gone" is evidence only from the source that owns the tab (D11).
func TestSilenceFromALiveChildIsNotASpawnFailure(t *testing.T) {
	for _, c := range []struct {
		name                              string
		present, sourceComplete, choosing bool
		want                              State
		decided                           bool
	}{
		{"the tab is gone from a complete listing of its terminal", false, true, false, StateSpawnFailed, true},
		{"the tab is missing but its terminal could not be listed", false, false, false, "", false},
		{"the tab is holding a dialog", true, true, true, StateSpawnFailed, true},
		{"the tab is holding a dialog, listing incomplete elsewhere", true, false, true, StateSpawnFailed, true},
		{"the tab is alive and quiet", true, true, false, "", false},
	} {
		state, why, decided := spawnVerdict(c.present, c.sourceComplete, c.choosing, Record{})
		if decided != c.decided || state != c.want {
			t.Errorf("%s: got %q/%v, want %q/%v", c.name, state, decided, c.want, c.decided)
		}
		if decided && why == "" {
			t.Errorf("%s: settled a task without saying why", c.name)
		}
	}
}

// What is in the composer. The fixture above is a running Claude session with a
// message already queued — "Press up to edit queued messages" — which is one of
// the shapes the person saw four times: a copy of this notice was already
// waiting to be read and a second one was typed behind it.
//
// The `Try "…"` line is the one that matters most. Claude Code draws it in an
// *empty* composer, so reading "any text at all" as a draft — which this first
// did — would have held every notice on this machine and dead lettered them all.
func TestWhatIsInTheComposerIsToldApartFromTheCLIsOwnHints(t *testing.T) {
	frame := strings.Repeat("─", 80)
	framed := func(input string) string {
		return "  ctrl+x ctrl+s to send now\n\n" + frame + "\n" + input + "\n" + frame + "\n  ▀ ▄ ▀ a root"
	}
	for _, c := range []struct {
		name  string
		input string
		want  ComposerState
	}{
		{"nothing in it", "❯", ComposerEmpty},
		{"a block cursor sitting in it", "❯ ▌", ComposerEmpty},
		{"Claude Code's hint for an empty line", `❯ Try "refactor internal/app/orchestrator/notice.go"`, ComposerEmpty},
		{"Codex's hint for an empty line", "› Ask Codex to do anything", ComposerEmpty},
		{"messages waiting to be read", "❯ Press up to edit queued messages", ComposerQueued},
		{"messages waiting, the longer spelling", "❯ Press up to edit queued messages, Enter to send them immediately", ComposerQueued},
		{"one message waiting", "❯ Press up to select a queued message, then Enter to edit it", ComposerQueued},
		{"a half-written line", "❯ so about the landing, I think we", ComposerDraft},
	} {
		if got := ReadComposer(framed(c.input), session.AssistantClaude); got != c.want {
			t.Errorf("%s: read as %v, want %v", c.name, got, c.want)
		}
	}
	if got := ReadComposer(screen(t, "claude-composer"), session.AssistantClaude); got != ComposerQueued {
		t.Errorf("the captured composer with a message queued read as %v", got)
	}
	// A dialog's highlighted row is not a composer at all: Choosing answers that
	// screen, and this one says nothing about it.
	if got := ReadComposer(screen(t, "claude-trust"), session.AssistantClaude); got != ComposerNone {
		t.Errorf("a chooser's highlighted option read as a composer holding %v", got)
	}
	// A session still starting has no input line to read.
	if got := ReadComposer("Loading…\n\n  ▀ ▄ ▀ starting", session.AssistantClaude); got != ComposerNone {
		t.Errorf("a starting session read as %v", got)
	}
}

// The same question asked of Codex, which draws no frame. Reading a composer as
// "a caret with a rule under it" — which this first did — answered
// ComposerNone for every Codex root on the machine, so a notice was never held
// for one: not because the composer was empty, but because nobody had looked.
func TestACodexRootsComposerIsReadFromItsStatusBar(t *testing.T) {
	barred := func(input string) string {
		return "  • Worked for 12s\n\n" + input + "\n\n  gpt-5.6-sol high · ~/code/clawdline-go · main"
	}
	for _, c := range []struct {
		name  string
		input string
		want  ComposerState
	}{
		{"nothing in it", "› ", ComposerEmpty},
		{"its own hint for an empty line", "› Ask Codex to do anything", ComposerEmpty},
		{"a half-written line", "› so about the landing, I think we", ComposerDraft},
	} {
		if got := ReadComposer(barred(c.input), session.AssistantCodex); got != c.want {
			t.Errorf("%s: read as %v, want %v", c.name, got, c.want)
		}
		if got := ReadComposer(barred(c.input), session.AssistantClaude); got != ComposerNone {
			t.Errorf("%s: asked for Claude's frame, a Codex screen answered %v", c.name, got)
		}
	}
}
