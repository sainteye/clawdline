//go:build !windows

package terminal

import (
	"os"
	"syscall"
)

// socketFile is the inode of the socket at path, and whether there is one.
// A tmux server that starts again on the same path makes a new file, so the
// inode tells one server's socket from the next.
func socketFile(path string) (uint64, bool) {
	info, err := os.Stat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return 0, false
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino), true
	}
	return 0, true
}
