package transcript

import (
	"errors"
	"io/fs"
)

// ErrNoRecord is a session's record that is not on disk. For a live session
// that is the ordinary state of one that has just started: Claude Code creates
// the file with the first turn, not when the session opens. It is an absence,
// not a failure, and a caller that shows it as an error shows a new session
// as a broken one.
var ErrNoRecord = errors.New("this session has not written its record yet")

// UnreadableError is a record that is there and could not be read.
//
// Its text says what went wrong and never where. The operating system's own
// error names the file, which is under the person's home directory, and what
// these readers answer reaches paired devices over Cloud.
type UnreadableError struct {
	// Cause is the operating system's reason with the path taken off it
	// (fs.PathError.Err), or the reader's own error when it was not a path's.
	Cause error
}

func (e *UnreadableError) Error() string {
	return "this session's record could not be read: " + e.Cause.Error()
}

func (e *UnreadableError) Unwrap() error { return e.Cause }

// recordError is how a reader here reports a record it could not open, stat
// or read: ErrNoRecord when there is no file, and otherwise an
// UnreadableError whose text carries no path.
func recordError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNoRecord
	}
	cause := err
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		cause = pathErr.Err
	}
	return &UnreadableError{Cause: cause}
}
