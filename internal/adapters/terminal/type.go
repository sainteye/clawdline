package terminal

import (
	"context"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// Type puts text in a pane's input line and does not submit it: Send without
// its Enter. A prompt with pictures in it is assembled in pieces — the words,
// then each picture pasted after them — and only the last step submits.
func (t *Tmux) Type(ctx context.Context, s session.Session, text string) error {
	if text == "" {
		return nil
	}
	return t.run(ctx, "send-keys", "-t", s.ID, "-l", text)
}
