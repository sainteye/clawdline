// Package bootid reads the operating system's own name for the boot this
// process is running in.
//
// It is what lets the daemon tell "the machine restarted" from "the daemon
// restarted": the second leaves every tmux pane and iTerm2 tab where it was,
// the first takes all of them away, and only the first is a reason to offer
// the sessions back (docs/session-restore.md).
//
// Unknown is not a value. A platform that has no such name, or a read that
// failed, answers an error and no id, and the caller records nothing: a guessed
// id would either merge two boots, offering nothing after a real reboot, or
// split one, offering sessions that are still open.
package bootid

import (
	"context"
	"errors"
	"strings"
)

// ErrUnsupported is a platform with no boot id this package knows how to read.
var ErrUnsupported = errors.New("this platform has no boot id this daemon can read")

// ErrEmpty is a source that answered and said nothing.
var ErrEmpty = errors.New("the boot id source answered empty")

// Read answers this boot's id, or why there is none.
func Read(ctx context.Context) (string, error) { return read(ctx) }

// clean is one line of the source's answer. A multi-line or blank answer is
// not an id.
func clean(raw []byte) (string, error) {
	id := strings.TrimSpace(string(raw))
	if id == "" {
		return "", ErrEmpty
	}
	if strings.ContainsAny(id, "\r\n\x00") || len(id) > 128 {
		return "", errors.New("the boot id source answered something that is not one id")
	}
	return id, nil
}
