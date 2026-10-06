//go:build windows

package orchestrator

import (
	"errors"
	"os"
	"time"
)

// callbackSupported: not yet. internal/adapters/supervisor cannot stop a
// process tree on Windows (it needs a Job Object), and a command this daemon
// could not stop is one it does not start.
const callbackSupported = false

var errNoCallbacks = errors.New("callbacks are not supported on Windows")

type callbackProcess struct {
	pid  int
	pgid int
}

func startCallbackProcess(Record, callbackFiles, func() error) (*callbackProcess, error) {
	return nil, errNoCallbacks
}
func (p *callbackProcess) wait() (*int, string) { return nil, "" }
func lockHeld(string) (bool, error)             { return false, errNoCallbacks }
func fence(string) (*os.File, error)            { return nil, errNoCallbacks }
func unfence(*os.File)                          {}
func sameGroup(int, int) bool                   { return false }
func stopGroup(int, time.Duration) error        { return errNoCallbacks }
