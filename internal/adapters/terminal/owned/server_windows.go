//go:build windows

package owned

import (
	"context"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

// Server on Windows refuses everything by name: there is no tmux, and a
// console this daemon owns (ConPTY) would need a terminal emulator of its own
// to read a screen from (docs/cross-platform.md). The `terminal` capability
// says the same before anybody asks.
type Server struct{}

var _ ports.OwnedTerminals = (*Server)(nil)

// New is the refusing server.
func New(nextDir string) (*Server, error) { return &Server{}, nil }

// CountReading is the `terminal.count` row: none can be open here.
func CountReading(context.Context, string) capacity.Reading {
	return capacity.Reading{Known: true, Note: "no terminal can be opened here: " + unsupported().Error()}
}

func unsupported() error {
	return terminal.Refuse(terminal.CodeUnsupported, "windows has no terminal backend yet")
}

func (*Server) Open(context.Context, ports.OpenTerminal) (terminal.Terminal, error) {
	return terminal.Terminal{}, unsupported()
}
func (*Server) List(context.Context) ([]terminal.Terminal, error) { return nil, unsupported() }
func (*Server) Frame(context.Context, terminal.ID) (terminal.Frame, error) {
	return terminal.Frame{}, unsupported()
}
func (*Server) Keys(context.Context, terminal.ID, []byte) error     { return unsupported() }
func (*Server) Paste(context.Context, terminal.ID, string) error    { return unsupported() }
func (*Server) Resize(context.Context, terminal.ID, int, int) error { return unsupported() }
func (*Server) Close(context.Context, terminal.ID) error            { return unsupported() }
func (*Server) History(context.Context, terminal.ID, int) ([]string, error) {
	return nil, unsupported()
}
func (*Server) HistoryBounded(context.Context, terminal.ID, int, int) ([]string, error) {
	return nil, unsupported()
}

// Changed has no signal to give, as NewPaneSignal has none on Windows.
func (*Server) Changed(context.Context, terminal.ID) (<-chan struct{}, func(), error) {
	return nil, func() {}, unsupported()
}
