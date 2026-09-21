//go:build !darwin

package terminal

import "github.com/sainteye/clawdline/internal/app/ports"

// Hosts is the terminal backends this platform has. Windows and Linux have no
// iTerm2; they reach the same port through tmux and, later, through ptys this
// daemon owns.
func Hosts() []ports.TerminalHost { return []ports.TerminalHost{NewTmux()} }
