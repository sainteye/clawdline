package orchestrator

import (
	"strings"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// Whether a new child is ready to be typed into.
//
// This exists because of what happened the first time it did not. A child was
// spawned into a fresh checkout, Claude Code drew its workspace-trust dialog —
//
//	❯ No, exit
//	  Yes, I trust this folder
//	Enter to confirm · Esc to cancel
//
// — and the broker, seeing an assistant process on a tty, typed the briefing
// and pressed Return. The Return answered the dialog. The child chose "No,
// exit" and the task died before it had read a word.
//
// A caret alone is not a composer: the same glyph draws the highlight in every
// chooser either CLI puts on screen. What distinguishes the composer is that
// **the assistant draws its own furniture under it** and a chooser has none.
// So the rule here is structural rather than a list of the sentences that have
// been seen in dialogs so far: a new dialog nobody has met yet fails this check
// the same way, which a list of known strings would not.
//
// The furniture is not the same furniture, which is what this file got wrong.
// Claude Code frames its composer — a box rule is drawn under the input line.
// Codex draws no frame at all; what it puts under its input line is its status
// bar, the row naming the model, the directory and the branch. Asking a Codex
// screen for a frame is asking it for something it never draws, so every Codex
// child failed this check for its whole 90 seconds and was recorded as
// `spawn_failed` — "the child session did not reach a prompt" — with a live
// composer on screen. Fifty-odd Claude children had passed through the same
// code, so nothing said the reading was half a reading.
//
// Each CLI's furniture is therefore asked for by name, and the assistant the
// task was dispatched with says which to ask for. An exclusion — "a caret that
// is not a menu row" — would have read the composer of either, and would also
// have read the highlighted row of any dialog whose options are not numbered.
//
// The cost of being wrong in the safe direction is a delay: the briefing is
// retried, and the four-minute clock reports honestly if a prompt never came.
// The cost of being wrong the other way is a keystroke that answers a question
// nobody read.

// composerCarets are the glyphs each CLI draws at the start of its input line,
// and at the start of the highlighted row of any chooser.
var composerCarets = []string{"❯", "›"}

// composerTail is how far up the screen a caret is looked for, counted in rows
// that have something on them.
//
// **Not in rows.** It used to be the last twenty lines of the capture, and a
// capture is the whole pane: a 40-row tab holding a 13-row Codex session is 27
// rows of nothing, so the twenty looked at were blank and the answer to both
// questions below was no — neither ready nor choosing, which is the one pair
// that decides nothing. That is the reading a Codex child died of, and a Claude
// Code trust dialog in a tall tab would have died of it the same way: a dialog
// nobody could see is a dialog nobody reports.
const composerTail = 20

// ComposerReady reports whether a screen shows an input line ready for text.
func ComposerReady(screen string, assistant session.Assistant) bool {
	caret, furnished, _ := readComposer(screen, assistant)
	return caret && furnished
}

// Choosing is the same reading from the other side: a caret with no furniture
// under it is something waiting to be answered, and nothing may be typed at it.
func Choosing(screen string, assistant session.Assistant) bool {
	caret, furnished, _ := readComposer(screen, assistant)
	return caret && !furnished
}

// readComposer finds the lowest caret on the screen and says whether the
// assistant's own furniture is under it. No caret at all is a session still
// drawing itself: neither answer is yes, because typing at it drops the bytes
// and calling it a chooser would stop a briefing that is about to be possible.
func readComposer(screen string, assistant session.Assistant) (caret, furnished bool, line string) {
	lines := strings.Split(screen, "\n")
	written := make([]int, 0, len(lines))
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			written = append(written, i)
		}
	}
	if len(written) == 0 {
		return false, false, ""
	}
	bottom := written[len(written)-1]
	from := len(written) - composerTail
	if from < 0 {
		from = 0
	}
	for k := len(written) - 1; k >= from; k-- {
		i := written[k]
		if !hasCaret(lines[i]) {
			continue
		}
		// Stop at the lowest caret rather than looking further up: the topmost
		// caret on a screen full of options would eventually find some
		// furniture and say yes.
		return true, furnitureUnder(lines, i, bottom, assistant), lines[i]
	}
	return false, false, ""
}

// ComposerState is what a composer holds, once there is one.
type ComposerState int

const (
	// ComposerNone is a screen with no composer on it at all — a dialog, a
	// session still starting. Whether anything may be typed there is
	// ComposerReady's and Choosing's question, not this one's.
	ComposerNone ComposerState = iota
	// ComposerEmpty is an input line with nothing in it, or with nothing but
	// the CLI's own hint about what could be typed.
	ComposerEmpty
	// ComposerQueued is an empty input line with messages behind it that the
	// session has not read yet.
	ComposerQueued
	// ComposerDraft is an input line with something in it that nobody has sent.
	ComposerDraft
)

// ReadComposer answers what is in the composer, and exists because of the other
// way a typed line does damage.
//
// A send is a paste and then Return (internal/adapters/terminal/submit.go); it
// does not clear the input line first. So a completion notice typed at a root
// whose person had half a sentence in the composer is appended to that sentence,
// and the Return submits both — the draft is gone, and what the assistant reads
// is somebody's unfinished thought with a protocol envelope glued to the end of
// it. That has happened on this machine, and a second delivery reported it
// independently.
//
// **What is in the line cannot be read as "any text at all", which is what this
// first tried.** Claude Code draws a hint in its *empty* composer — one of eight
// `Try "…"` sentences, from the same function that draws its queued-message hint
// (read off the installed 2.1.278, and Codex's is the `Ask Codex to do anything`
// that session.ReadState already knows). An idle root would have read as
// occupied, so every notice on this machine would have been held and then dead
// lettered. The hints are therefore named here, which is a list of strings and
// has the cost a list of strings has: a hint nobody has met yet reads as a
// draft. That direction is the survivable one — the notice is held, and a hold
// that runs out spends an attempt and ends visibly in the completion list with
// its reason (notice.go) — where the other direction loses a person's sentence.
//
// The queued hint is its own answer rather than "empty", because it says
// something is already waiting to be read, and whether that matters depends on
// whether this notice is what is waiting. That is the caller's question.
//
// Whether there is a composer at all is readComposer's answer, which asks the
// named CLI for its own furniture. Asking for a frame here, as this first did,
// would have answered ComposerNone for every Codex root on the machine — Codex
// draws no frame — and a notice is not held for a composer that was never
// looked at.
func ReadComposer(screen string, assistant session.Assistant) ComposerState {
	caret, furnished, line := readComposer(screen, assistant)
	if !caret || !furnished {
		return ComposerNone
	}
	text := composerText(line)
	switch {
	case text == "":
		return ComposerEmpty
	case hasAnyPrefix(text, composerQueuedHints):
		return ComposerQueued
	case hasAnyPrefix(text, composerPlaceholders):
		return ComposerEmpty
	}
	return ComposerDraft
}

// composerPlaceholders are what each CLI draws in an input line that is empty.
// Claude Code's is one of eight `Try "…"` sentences built from the files this
// repository's history touches most; Codex's is fixed.
var composerPlaceholders = []string{`Try "`, "Ask Codex to do anything"}

// composerQueuedHints are what Claude Code draws in an empty input line while
// messages it has not read yet wait behind it. All four spellings of it begin
// this way.
var composerQueuedHints = []string{"Press up to edit queued message", "Press up to select a queued message"}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// composerText is what an input line holds after its caret, without the block
// glyphs a terminal draws for the cursor and without the frame's right edge.
func composerText(line string) string {
	trimmed := strings.TrimSpace(line)
	for _, caret := range composerCarets {
		if rest, ok := strings.CutPrefix(trimmed, caret); ok {
			return strings.TrimSpace(strings.Trim(rest, cursorGlyphs))
		}
	}
	return ""
}

// cursorGlyphs are what the two CLIs and the terminals they run in draw for a
// caret sitting in an empty input line, plus the frame's vertical rule. None of
// them is text a person typed.
const cursorGlyphs = " \t│┃▏▎▍▌▋▊▉█▁▂▃▄▅▆▇_"

// furnitureUnder asks the one question that tells a composer from a chooser,
// in the spelling of the CLI that drew the screen.
//
// An assistant this broker was not told the name of is asked for a frame,
// which is what every assistant was asked for before Codex was looked at.
// Asking for either instead would read Claude Code's trust dialog as a
// composer: `Enter to confirm · Esc to cancel` is the lowest row of it and is
// divided by a middle dot, so it passes for Codex's status bar. Every dispatch,
// hand-over and assignment records which CLI it opened, so this is the branch
// that does not happen; being wrong here costs a delay, and being wrong the
// other way answers a dialog.
func furnitureUnder(lines []string, caret, bottom int, assistant session.Assistant) bool {
	if assistant == session.AssistantCodex {
		return statusUnder(lines, caret, bottom)
	}
	return frameUnder(lines, caret)
}

// frameUnder is Claude Code's composer: the box rule drawn under the input
// line. Three lines of slack, because a composer with a hint row under it is
// still a composer.
func frameUnder(lines []string, caret int) bool {
	for j := caret + 1; j < len(lines) && j <= caret+3; j++ {
		if isRule(lines[j]) {
			return true
		}
	}
	return false
}

// statusUnder is Codex's composer: the status bar under the input line, which
// is the lowest thing Codex draws and belongs to the composer alone. Captured
// on 2026-09-20 from codex-cli 0.155.1, opening its own `/model` picker takes
// the composer and the status bar away together and puts the dialog where the
// transcript was, which is what makes the bar evidence rather than decoration.
//
// Four lines of slack: an empty composer sits two rows above its bar, and one
// holding a typed message sits a row or two further up.
func statusUnder(lines []string, caret, bottom int) bool {
	return bottom > caret && bottom <= caret+4 && isStatusBar(lines[bottom])
}

// isStatusBar is a row of fields divided by a middle dot — model, directory,
// branch, context — which is how Codex draws the one under its composer.
//
// **One divider, not two.** A pane is whatever width the terminal gave it, and
// tmux gives a new session eighty columns: the bar is truncated after its first
// field there, so the whole of it is `gpt-5.6-sol high · ~/…/work/repo…`.
// Asking for two dividers read that live composer as a dialog and settled the
// task `spawn_failed` — measured on this daemon's own dispatch, 2026-09-20,
// which is why the acceptance for this is a Codex child that finishes rather
// than a test that passes.
//
// A row that heads a turn of the session's own is never the bar, however it is
// divided. While a turn runs Codex draws `• Working (17s • esc to interrupt) ·
// 1 background terminal running · /ps to view · /stop to close`, and that is
// the transcript talking; the bar is furniture, and the two are told apart by
// the marker the transcript's rows begin with.
func isStatusBar(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || !strings.Contains(line, " · ") {
		return false
	}
	for _, marker := range turnMarkers {
		if strings.HasPrefix(trimmed, marker) {
			return false
		}
	}
	return true
}

// turnMarkers head a turn of a session's own — an answer, a tool call, the
// spinner on one still being written. The same set internal/domain/session
// reads menus by, kept here because this file is read on its own.
var turnMarkers = []string{"•", "⏺", "✳", "✻", "✽", "✢", "✶", "✱", "✴", "◐", "◑", "◒", "◓", "◴", "◵", "◶", "◷", "⎿"}

func hasCaret(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, caret := range composerCarets {
		if strings.HasPrefix(trimmed, caret) {
			return true
		}
	}
	return false
}

// isRule is a line that is nothing but box-drawing horizontals. Four is the
// shortest run worth calling a frame; a stray glyph in prose is not one.
func isRule(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	runs := 0
	for _, r := range trimmed {
		switch r {
		case '─', '━', '═', '╌', '┄', '┈', '╭', '╮', '╰', '╯', '│', '┌', '┐', '└', '┘':
			runs++
		default:
			return false
		}
	}
	return runs >= 4
}
