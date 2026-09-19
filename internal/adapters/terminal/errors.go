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
type Unconfirmed struct{ Why string }

func (u Unconfirmed) Error() string { return u.Why }
