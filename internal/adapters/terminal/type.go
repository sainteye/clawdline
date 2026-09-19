package terminal

import (
	"context"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// Type puts text in a pane's input line and does not submit it: Send without
// its Enter. A prompt with pictures in it is assembled in pieces — the words,
// then each picture pasted after them — and only the last step submits.
//
// The words go in as one bracketed paste, as Send's do (submit.go): typed
// with `send-keys -l`, words longer than one read of the pty lost their head
// to Claude Code's own paste detection just as a long notice did.
func (t *Tmux) Type(ctx context.Context, s session.Session, text string) error {
	if text == "" {
		return nil
	}
	return tmuxInput{call: t.call, target: s.ID}.paste(ctx, text)
}
