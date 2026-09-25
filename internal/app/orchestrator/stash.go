package orchestrator

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// Getting a completion notice past something in a Claude Code root's composer.
//
// **Most of what was held as a draft was not a draft.** Claude Code draws a
// prompt suggestion in an empty composer after a turn ends — its guess at what
// the person will say next, in dim text, taken with Tab — and a plain-text
// capture of the screen cannot tell it from a sentence somebody typed:
//
//	❯ run the tests again and fix what fails
//
// was a root's composer on 2026-09-25, and nobody had typed it. ReadComposer
// answered ComposerDraft, the notice was held for ten minutes, an attempt was
// spent, and it was held again: eight of those is eighty minutes before the
// dead letter's push, and the child that had finished at the start of it was
// left asking in its own tab why its root never moved. A suggestion follows
// every turn on an account that has them, so this was not an edge of the hold:
// it was every completion typed at an idle root.
//
// Neither iTerm2's scripting nor tmux without `-e` carries the dim attribute,
// so the answer does not come from reading harder. It comes from Claude Code's
// own stash (ctrl+s, `chat:stash`), measured on 2.1.282:
//
//   - an input line holding text is moved into the stash, the line is left
//     empty, and `› stashed` is drawn above the frame for as long as the stash
//     is held;
//   - the stash is put back in the input line by Claude Code itself after the
//     next message is submitted ("Draft restored");
//   - an input line holding nothing — a suggestion is not in it — is left as it
//     was, unless a stash is already held, in which case ctrl+s puts that one
//     back instead;
//   - a second ctrl+s over a held stash replaces it, and the first draft is
//     gone.
//
// So one keystroke answers the question the screen could not, and in the case
// where the text is a person's it also makes room for the notice without losing
// a byte of it. The last two rules are why a stash already on screen is a hold
// and not an attempt: that is the one case where the keystroke costs somebody
// a draft.
//
// What this does not cover, and holds for as before: a Codex root (Codex has no
// stash), a composer in vim mode, a draft still being written (the text must be
// the same on two looks a hold apart), and a person who rebound ctrl+s.

// ctrlS is chat:stash in Claude Code's default keybindings.
var ctrlS = []byte{0x13}

// stashMarker is what Claude Code draws above its frame while a stash is held.
// It shares its row with other hints (`Ctrl+Y to paste deleted text · ›
// stashed`), so it is looked for as a suffix of that row.
const stashMarker = "› stashed"

// vimMarkers are the mode lines Claude Code draws when vim mode is on. In
// NORMAL mode a keystroke is a command, and nothing here types at one.
var vimMarkers = []string{"-- INSERT --", "-- NORMAL --", "-- VISUAL --"}

// Prepare runs inside the send's turn of the terminal's lane, before any of the
// notice is typed: press writes raw key bytes, look captures the screen. An
// error means nothing may be typed.
type Prepare func(ctx context.Context, press func([]byte) error, look func() (string, bool)) error

// stashPauses are the looks after the keystroke. Claude Code redraws in tens of
// milliseconds; a second and a half without a change is a keystroke that did
// nothing, not one still on its way.
var stashPauses = []time.Duration{
	50 * time.Millisecond, 100 * time.Millisecond, 150 * time.Millisecond, 200 * time.Millisecond,
	250 * time.Millisecond, 300 * time.Millisecond, 450 * time.Millisecond,
}

// What the look before and after the keystroke found.
const (
	// ComposerStashed is a person's draft moved into Claude Code's stash; it
	// comes back when the notice is submitted.
	ComposerStashed = "stashed"
	// ComposerSuggestion is text in the input line that was never in it: the
	// keystroke left it as it was.
	ComposerSuggestion = "suggestion"
)

// errStashWithheld is every reason the keystroke was not sent or its answer was
// not one of the two above. The notice is held, exactly as it was before this
// file existed.
var errStashWithheld = errors.New("the composer was not cleared")

// StashShown is whether Claude Code is holding a stash: its marker on a row
// above the composer's input line.
func StashShown(screen string) bool {
	caret, _, _ := readComposer(screen, session.AssistantClaude)
	if !caret {
		return false
	}
	lines := strings.Split(screen, "\n")
	input := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if hasCaret(lines[i]) {
			input = i
			break
		}
	}
	for i := input - 1; i >= 0 && i >= input-3; i-- {
		if strings.HasSuffix(strings.TrimSpace(lines[i]), stashMarker) {
			return true
		}
	}
	return false
}

// vimMode is whether the screen shows one of Claude Code's vim mode lines.
func vimMode(screen string) bool {
	for _, m := range vimMarkers {
		if strings.Contains(screen, m) {
			return true
		}
	}
	return false
}

// composerHolds is the text of the input line when ReadComposer calls it a
// draft, and "" otherwise.
func composerHolds(screen string, assistant session.Assistant) string {
	if ReadComposer(screen, assistant) != ComposerDraft {
		return ""
	}
	_, _, line := readComposer(screen, assistant)
	return composerText(line)
}

// stashDraft is the Prepare for a Claude Code root whose composer showed
// `expect` on two looks a hold apart. It writes what it found to *found.
func stashDraft(expect string, found *string) Prepare {
	return func(ctx context.Context, press func([]byte) error, look func() (string, bool)) error {
		before, ok := look()
		if !ok || vimMode(before) || StashShown(before) ||
			composerHolds(before, session.AssistantClaude) != expect {
			return errStashWithheld
		}
		if err := press(ctrlS); err != nil {
			return err
		}
		for _, pause := range stashPauses {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(pause):
			}
			after, ok := look()
			if !ok {
				continue
			}
			if StashShown(after) && composerHolds(after, session.AssistantClaude) != expect {
				*found = ComposerStashed
				return nil
			}
			if composerHolds(after, session.AssistantClaude) != expect {
				// Something else moved: somebody typing, a dialog, a turn
				// starting. Not the answer to this question.
				return errStashWithheld
			}
		}
		// Unchanged, and the stash never appeared: the input line was empty,
		// and the text in it was drawn there by Claude Code.
		*found = ComposerSuggestion
		return nil
	}
}

// StashReboundIn is whether a Claude Code keybindings file gives ctrl+s, or
// chat:stash, a meaning of the person's own. Either spelling anywhere in it
// counts: reading the file's structure to find a binding that leaves both
// alone would be a parser for a format this daemon does not own, and the cost
// of a false yes is only the hold this file replaced.
func StashReboundIn(keybindings []byte) bool {
	s := strings.ToLower(string(keybindings))
	return strings.Contains(s, "ctrl+s") || strings.Contains(s, "chat:stash")
}
