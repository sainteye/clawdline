package orchestrator

import "strings"

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
// chooser either CLI puts on screen. What distinguishes the composer is that it
// is **framed** — the box rule is drawn under it — and a chooser's options are
// not. So the rule here is structural rather than a list of the sentences that
// have been seen in dialogs so far: a new dialog nobody has met yet fails this
// check the same way, which a list of known strings would not.
//
// The cost of being wrong in the safe direction is a delay: the briefing is
// retried, and the four-minute clock reports honestly if a prompt never came.
// The cost of being wrong the other way is a keystroke that answers a question
// nobody read.

// composerCarets are the glyphs each CLI draws at the start of its input line,
// and at the start of the highlighted row of any chooser.
var composerCarets = []string{"❯", "›"}

// ComposerReady reports whether a screen shows an input line ready for text.
func ComposerReady(screen string) bool {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0 && i > len(lines)-20; i-- {
		if !hasCaret(lines[i]) {
			continue
		}
		// The frame under the input line. Three lines of slack, because a
		// composer with a hint row under it is still a composer.
		for j := i + 1; j < len(lines) && j <= i+3; j++ {
			if isRule(lines[j]) {
				return true
			}
		}
		// A caret with no frame under it is a chooser's highlight. Stop here
		// rather than looking further up: the topmost caret on a screen full of
		// options would eventually find some rule and say yes.
		return false
	}
	return false
}

// Choosing is the same reading from the other side: a caret with no frame is
// something waiting to be answered, and nothing may be typed at it.
func Choosing(screen string) bool {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0 && i > len(lines)-20; i-- {
		if !hasCaret(lines[i]) {
			continue
		}
		for j := i + 1; j < len(lines) && j <= i+3; j++ {
			if isRule(lines[j]) {
				return false
			}
		}
		return true
	}
	return false
}

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
