package terminal

import "fmt"

// Unsupported is what a backend answers when it has no way to do something.
// It is a typed refusal: silence would let a caller believe the effect landed.
type Unsupported struct{ Op string }

func (e Unsupported) Error() string {
	return fmt.Sprintf("this terminal backend cannot %s yet", e.Op)
}

func errUnsupported(op string) error { return Unsupported{Op: op} }
