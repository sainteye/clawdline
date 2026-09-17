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
	// Rungs are the label's parts; Label is PreferredLabel(Rungs).
	Rungs session.LabelRungs
	// CustomTitle is the conversation's current `/rename`, if any.
	CustomTitle string
}

// OpenRequest is what it takes to start a session.
type OpenRequest struct {
	Name    string
	Cwd     string
	Command string
	// Env is what the session needs to find its own work. A child that has to
	// parse its task directory out of prose cannot be scripted, and the first
	// thing any child does is read that directory.
	Env map[string]string
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

// KeyHost types raw key bytes into a session as one keypress, outside any
// bracketed paste. It is apart from TerminalHost because only the menu-answer
// path uses it, and that path allows a closed set of bytes (app.Actions.Key):
// a byte channel into a tty is an escape-sequence channel into a tty.
type KeyHost interface {
	Keystroke(ctx context.Context, s session.Session, bytes []byte) error
}

// TerminalHost enumerates and drives terminal sessions. One interface covers
// both surfaces: a terminal somebody else opened (attached) and a pty this
// daemon owns.
type TerminalHost interface {
	Inventory(ctx context.Context) (session.Inventory, error)
	Name() string

	// Open starts a new session and returns it. The caller names the command,
	// because a machine may carry a wrapper for an assistant and this port is
	// in no position to know.
	Open(ctx context.Context, req OpenRequest) (session.Session, error)

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

// Launcher opens a new terminal and types one line into its shell. It is the
// start route's port, apart from TerminalHost because what it opens is decided
// by the machine's terminal setting and by what is running, not by a backend
// the caller already chose — StartPoints.start in the Swift app.
//
// Every method answers with a typed error rather than doing nothing: a page
// told a tab exists when none does sends somebody to look for it.
type Launcher interface {
	// ITermRunning is whether iTerm2 is open. It never launches it.
	ITermRunning(ctx context.Context) (bool, error)
	// TmuxReach is 0 for no tmux, 1 for tmux with no server, 2 for a server
	// with panes on it (projects.TmuxReach).
	TmuxReach(ctx context.Context) int
	// NewITermTab opens a tab without bringing iTerm2 forward and types line
	// into it. The answer is the iTerm2 session id.
	NewITermTab(ctx context.Context, line string) (string, error)
	// NewTmuxWindow adds a window to the running server with a login shell in
	// cwd and types command into it. The answer is the pane id.
	NewTmuxWindow(ctx context.Context, cwd, command string) (string, error)
	// NewTmuxSession starts a server with a detached session named name, the
	// same way. The answer is the pane id.
	NewTmuxSession(ctx context.Context, cwd, name, command string) (string, error)
}
