//go:build !darwin

package terminal

import "context"

// revealFollow is nothing on a platform with no iTerm2 to ask.
//
// Named rather than absent. `select-pane` and `select-window` have already put
// the pane in front inside tmux, which is the whole of what tmux can do; who
// draws that tmux — Ghostty, a Linux terminal, Windows Terminal — decides for
// itself whether a window comes forward, and this daemon has no way to ask any
// of them. A silent no-op is the honest answer here, and the route's own
// contract already says the same thing: ok means the selection was made, never
// that a window is in front of a person.
func revealFollow(ctx context.Context, t *Tmux, paneID string, activate bool) {}
