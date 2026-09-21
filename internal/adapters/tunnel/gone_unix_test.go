//go:build unix

package tunnel

import (
	"errors"
	"syscall"
)

// processGone asks the system whether a process exists. Signal 0 delivers
// nothing; ESRCH is the one answer that means "no such process". EPERM means
// it exists and is somebody else's, which is not gone.
func processGone(pid int) (gone, known bool) {
	err := syscall.Kill(pid, syscall.Signal(0))
	switch {
	case err == nil, errors.Is(err, syscall.EPERM):
		return false, true
	case errors.Is(err, syscall.ESRCH):
		return true, true
	}
	return false, false
}
