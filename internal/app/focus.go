package app

import (
	"context"

	"github.com/sainteye/clawdline-go/internal/adapters/terminal"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// Focus brings a session's terminal to the front on this machine.
//
// It sits beside Send and Close rather than in the terminal package for the
// same reason they do: the precondition — that this id is a session on a
// reading of this machine that was complete enough to say so — is not the
// terminal's to check, and a `not_found` on an incomplete reading would turn a
// terminal that lost accessibility for a moment into a session somebody
// deleted.
//
// **A nil error is the selection, not the window.** What the backend was asked
// for is that this session becomes the current one; whether an application
// comes forward is decided by whichever emulator is drawing it, and the only
// one that can be asked is asked after this has already answered. A backend
// with no way to do it at all — an owned pty, a platform with no such terminal
// — answers `backend_unsupported` rather than quietly doing nothing, because a
// page told a window is now in front of somebody sends them to look for it.
func (a Actions) Focus(ctx context.Context, id string) (session.Session, error) {
	s, err := a.Find(ctx, id)
	if err != nil {
		return session.Session{}, err
	}
	h, err := a.host(s)
	if err != nil {
		return session.Session{}, err
	}
	if err := h.Reveal(ctx, s, true); err != nil {
		if _, no := err.(terminal.Unsupported); no {
			return s, Refusal{Code: "backend_unsupported", Detail: err.Error()}
		}
		// The original's sentence for this, as `/key` already writes it
		// (keys.go): a terminal command that did not complete, and what the
		// terminal itself said about it.
		return s, Refusal{Code: "terminal_io_failed",
			Detail: "The terminal command did not complete: " + err.Error()}
	}
	a.record(ctx, "session.focus", s.ID, nil)
	return s, nil
}
