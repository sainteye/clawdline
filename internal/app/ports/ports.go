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

// TerminalHost enumerates and drives terminal sessions. One interface covers
// both surfaces: a terminal somebody else opened (attached) and a pty this
// daemon owns.
type TerminalHost interface {
	Inventory(ctx context.Context) (session.Inventory, error)
	Name() string
}
