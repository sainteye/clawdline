//go:build windows

package terminal

// socketFile: no tmux server runs on Windows, so there is never a carried
// server to keep.
func socketFile(string) (uint64, bool) { return 0, false }
