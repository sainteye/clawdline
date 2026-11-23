//go:build windows

package http

// testHomeOwnerGone cannot prove a process is gone here, so a leftover home
// is kept rather than guessed about.
func testHomeOwnerGone(int) bool { return false }
