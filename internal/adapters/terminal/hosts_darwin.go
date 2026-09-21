//go:build darwin

package terminal

import "github.com/sainteye/clawdline/internal/app/ports"

// Hosts is the terminal backends this platform has. The list is the only place
// a platform difference appears; everything above it sees one port.
func Hosts() []ports.TerminalHost { return []ports.TerminalHost{NewTmux(), NewITerm()} }
