package terminal

import (
	"context"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// TypeAntigravityFirst admits no generic send and performs the supplied live
// check again immediately before each separate tmux operation.
func (t *Tmux) TypeAntigravityFirst(ctx context.Context, s session.Session, text string, check func(context.Context) error) error {
	t, s = t.sessionTarget(s)
	if t == nil {
		return ErrAntigravityInputUnavailable
	}
	if s.Assistant != session.AssistantAntigravity || s.Backend != session.BackendTmux || check == nil {
		return ErrAntigravityInputUnavailable
	}
	call := func(c context.Context, stdin string, args ...string) (string, error) {
		// This scoped cleanup deletes only the random buffer created by this
		// paste; revocation must stop input without stranding its private text.
		if len(args) == 0 || args[0] != "delete-buffer" {
			if err := check(c); err != nil {
				return "", err
			}
		}
		return t.call(c, stdin, args...)
	}
	return tmuxInput{call: call, target: s.ID}.paste(ctx, text)
}
func (t *Tmux) EnterAntigravityFirst(ctx context.Context, s session.Session, check func(context.Context) error) error {
	t, s = t.sessionTarget(s)
	if t == nil {
		return ErrAntigravityInputUnavailable
	}
	if s.Assistant != session.AssistantAntigravity || s.Backend != session.BackendTmux || check == nil {
		return ErrAntigravityInputUnavailable
	}
	if err := check(ctx); err != nil {
		return err
	}
	return t.Keystroke(ctx, s, []byte{0x0d})
}
