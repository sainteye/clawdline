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
