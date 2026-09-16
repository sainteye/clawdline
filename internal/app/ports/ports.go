// Package ports declares what the application needs from the outside world.
// The interfaces are defined by the behaviour the domain requires, and the
// platform implementations in internal/adapters depend inward on them.
package ports

import (
	"context"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// ProcessHost answers what is running on this machine.
type ProcessHost interface {
	// Scan returns one reading of the process table. A failure is carried in
	// the inventory's completeness, not thrown, so a partial answer is still
	// publishable.
	Scan(ctx context.Context) (session.Inventory, error)
}

// Identity is what an assistant's own records say about a running session.
// These are better evidence than anything read off a screen, because they are
// the assistant speaking about itself.
type Identity struct {
	ConversationID string
	Pane           string
	CWD            string
	Label          string
	// Status is the assistant's own word, not ours. The domain maps it.
	Status string
}

// IdentityHost resolves a running process against the assistant's own records.
type IdentityHost interface {
	ForSession(ctx context.Context, s session.Session) (Identity, bool)
}

// ScreenHost reads what is currently drawn in a terminal. It is the weakest
// evidence this daemon has and is only consulted where nothing better exists.
type ScreenHost interface {
	Capture(ctx context.Context, s session.Session) (string, bool)
}

// TerminalHost enumerates and drives terminal sessions. One interface covers
// both surfaces: a terminal somebody else opened (attached) and a pty this
// daemon owns.
type TerminalHost interface {
	Inventory(ctx context.Context) (session.Inventory, error)
	Name() string

	// Send types one line into a session and submits it. A nil error means the
	// bytes reached the tty — never that anything read them. Whether the
	// assistant took the turn is a separate fact with separate evidence.
	Send(ctx context.Context, s session.Session, text string) error

	// Interrupt delivers raw bytes outside a bracketed paste: the byte that
	// stops a turn without closing the session.
	Interrupt(ctx context.Context, s session.Session) error

	// Close takes the session away. The caller proves nothing is still running
	// in it first; this port does not decide that.
	Close(ctx context.Context, s session.Session) error
}
