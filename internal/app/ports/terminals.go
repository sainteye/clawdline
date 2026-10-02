package ports

import (
	"context"

	"github.com/sainteye/clawdline/internal/domain/terminal"
)

// OpenTerminal is what it takes to open an ordinary shell for a person.
type OpenTerminal struct {
	// ProjectPath is the directory the shell starts in. It must exist.
	ProjectPath string
	// ProjectID is recorded with the terminal so a list can be narrowed to one
	// project. Letters, digits and `._:-` only: it is written into the server.
	ProjectID  string
	Cols, Rows int
}

// OwnedTerminals is the ordinary shells this daemon opens and holds on a
// terminal server of its own — never the person's tmux, never an assistant's
// pane (plan v3 D1).
//
// **The server is the only record.** A terminal is a tmux session carrying its
// id as a user option; there is no table beside it, so a daemon that restarts
// lists the same terminals with the same ids and nothing has to be reconciled.
//
// Every failure is a *terminal.Refusal. terminal_closed is a positive answer
// that the terminal is not there; terminal_unreachable is the server not
// answering, and says nothing about whether it is. A caller must never read
// the second as the first.
type OwnedTerminals interface {
	// Open starts a login shell in req.ProjectPath at the given size.
	// terminals_full when terminal.MaxTerminals are open already.
	Open(ctx context.Context, req OpenTerminal) (terminal.Terminal, error)
	// List is every terminal on the server. No server is an empty list and a
	// nil error; a server that could not be asked is an error, never an
	// empty list.
	List(ctx context.Context) ([]terminal.Terminal, error)
	// Frame is one read of the visible screen, its cursor and its modes.
	Frame(ctx context.Context, id terminal.ID) (terminal.Frame, error)
	// Keys types raw bytes as keystrokes, outside any bracketed paste. At
	// most terminal.MaxInputBytes; more is input_too_large with nothing typed.
	Keys(ctx context.Context, id terminal.ID, data []byte) error
	// Paste types text as a paste: bracketed when the program in the terminal
	// asked for bracketed paste. At most terminal.MaxPasteBytes.
	Paste(ctx context.Context, id terminal.ID, text string) error
	// Resize sets the terminal's size. The last size asked for wins.
	Resize(ctx context.Context, id terminal.ID, cols, rows int) error
	// Close ends the terminal and every process in it. Closing one that is
	// already gone is not an error: gone is what was asked for.
	Close(ctx context.Context, id terminal.ID) error
	// History is up to `lines` lines that scrolled off the top, followed by
	// the visible screen. A larger ask is lowered to terminal.MaxHistoryLines.
	History(ctx context.Context, id terminal.ID, lines int) ([]string, error)
	// HistoryBounded has the same contents but refuses before retaining more
	// than maxBytes of captured output. Cloud receipts use this narrower path.
	HistoryBounded(ctx context.Context, id terminal.ID, lines, maxBytes int) ([]string, error)
	// Changed wakes the caller when the terminal drew something. The channel
	// holds one pending wake-up and coalesces the rest: it says "read a
	// frame", never what changed. stop takes the subscription back and must
	// be called. A nil channel with a nil error is a platform that cannot
	// signal, and the caller reads frames on its own clock instead.
	Changed(ctx context.Context, id terminal.ID) (wake <-chan struct{}, stop func(), err error)
}
