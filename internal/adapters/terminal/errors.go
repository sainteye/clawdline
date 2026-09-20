package terminal

import "fmt"

// Unsupported is what a backend answers when it has no way to do something.
// It is a typed refusal: silence would let a caller believe the effect landed.
type Unsupported struct{ Op string }

func (e Unsupported) Error() string {
	return fmt.Sprintf("this terminal backend cannot %s yet", e.Op)
}

func errUnsupported(op string) error { return Unsupported{Op: op} }

// Unsent is a write that put nothing on the terminal: it was refused before
// its first byte — the session was not found, the terminal was not running,
// the lane never came free. Every other failure of a write may have landed
// part or all of what it carried, and a caller deciding whether to type the
// same line again needs the two apart: typing after an Unsent cannot deliver
// a line twice, and typing after anything else can.
type Unsent struct{ Why string }

func (u Unsent) Error() string { return u.Why }

// Unsubmitted is a line that was typed and never submitted: the text went into
// the terminal, the program in it never showed the text arriving, and so no
// Enter was pressed (submit.go). It is not Unsent — the text may be sitting in
// the input line, or may arrive there once the program reads again — and it is
// not a delivery. Typing the same line again can leave two copies in one input
// line; it cannot submit half of one.
type Unsubmitted struct{ Why string }

func (u Unsubmitted) Error() string { return u.Why }

// Unconfirmed is an effect that was asked for and not answered, and whose
// outcome a look afterwards could not settle either way. It is neither a
// success nor a failure: an iTerm2 close that ran out of time has been seen to
// close its session later all the same — iTerm2 was asking a person first — so
// a caller must not count it done, the tab may still be there, and must not ask
// again as though it had failed.
type Unconfirmed struct {
	Why string
	// Attention is an effect that went unanswered because something on this
	// Mac's screen is waiting for a person: iTerm2's own "Close tab #N? This
	// tab is running …" sheet, or a refused automation permission. It is kept
	// apart from every other unanswered effect because it is the one a person
	// can end by walking to the machine, and telling them that is the whole
	// difference between a dead end and a next step.
	Attention bool
}

func (u Unconfirmed) Error() string { return u.Why }

// The four ways a close can stop before it closes (farewell.go). They are
// apart from one another because a person reading "it did not close" learns
// nothing they can act on, and each of these says something different about
// what to do next: wait, look at the terminal, or answer the question on the
// screen.

// Unreadable is a terminal whose contents could not be read. Nothing was
// typed, nothing was signalled and nothing was closed: what is in a terminal
// is the whole basis of a close, and an unreadable terminal is not an empty
// one.
type Unreadable struct{ Why string }

func (u Unreadable) Error() string { return u.Why }

// Occupied is a terminal holding something this close was not asked about — a
// command somebody started, a different assistant, a process newer than the
// reading the close is acting on. Nothing was signalled and the terminal was
// left as it was.
type Occupied struct{ Why string }

func (o Occupied) Error() string { return o.Why }

// QuitRefused is an assistant that would not take its own quit word, on a
// machine with no way to ask the process itself. The session is still running
// and the terminal is still open.
type QuitRefused struct{ Why string }

func (q QuitRefused) Error() string { return q.Why }

// StillRunning is an assistant that was asked to leave — by its own word, and
// then by a signal where this machine could send one — and did not. The
// terminal is left open on purpose: closing it would hang up the tty under a
// process that is demonstrably still writing.
type StillRunning struct{ Why string }

func (s StillRunning) Error() string { return s.Why }

// Unwatched is a selection nobody can see: the pane was selected inside tmux
// and no client is attached to the tmux session holding it, so the pane is on
// no screen on this machine. It is not a failure of the selection — that
// landed, and the next client to attach arrives on it — and it is not a
// terminal that misbehaved, so it is neither `Unsupported` nor an I/O error.
// It is the one thing a caller must not be told `ok` about: a page that says a
// window is now in front of somebody sends them to look for it.
type Unwatched struct{ Session string }

func (u Unwatched) Error() string {
	return fmt.Sprintf("nobody can see this session: no terminal is attached to tmux session %q", u.Session)
}
