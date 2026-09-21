//go:build windows

package terminal

import "github.com/sainteye/clawdline/internal/app/ports"

// NewPaneSignal has nothing to build on Windows: there is no `mkfifo`, and the
// whole shape of the tmux signal is a FIFO whose readability is the message.
//
// Nil rather than a stub that answers false to every Attach. The caller reads
// nil as "this build cannot be told when a pane moves" and publishes the screen
// as `on-demand`, which is true and is what the panel then draws — a stub would
// leave the same screen claiming to be four milliseconds behind a pane while
// nothing was ever going to wake it.
func NewPaneSignal(dir string, pipe ports.PipeHost) ports.PaneSignal { return nil }
