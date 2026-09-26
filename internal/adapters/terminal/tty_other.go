//go:build !darwin && !linux

package terminal

import (
	"errors"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// What is in front of a terminal, on a platform where this daemon cannot ask.
//
// macOS asks a `kern.proc.tty` sysctl and Linux reads /proc (tty_job.go,
// tty_linux.go). Nothing is written for anywhere else, and a guess would be
// worse than the refusal: the whole point of reading the tty is to decide whether a process
// may be signalled and whether a terminal is safe to close, and both of those
// fail closed.
//
// A backend with an answer of its own still has one — tmux names what is in
// front of a pane, which is enough to type a quit word and to see it leave,
// and not enough to signal anything. That is the ladder stopping at the polite
// word rather than pretending to a rung it does not have.

var errNoTTYReading = errors.New("this platform cannot read the processes behind a terminal")

func ttySight(tty string, s session.Session) (farewellSight, error) {
	return farewellSight{}, errNoTTYReading
}

// ttySignal is nothing here: a signal with nothing to aim it at.
var ttySignal func(group int, step escalation) error
