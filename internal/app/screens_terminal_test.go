package app

import (
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

type terminalPipeSentinel struct {
	ports.PaneSignal
	attached []string
}

func (*terminalPipeSentinel) OnMoved(func(string)) {}
func (p *terminalPipeSentinel) Attach(id string) bool {
	p.attached = append(p.attached, id)
	return true
}
func (*terminalPipeSentinel) DetachAll() {}

func TestTerminalSessionViewDoesNotReplaceTheTerminalsPipe(t *testing.T) {
	pipe := &terminalPipeSentinel{}
	screens := NewScreens(nil, pipe, nil)
	defer screens.Stop()
	now := time.Now()
	row := session.Session{ID: "terminal:%0", Backend: session.BackendTmux}
	screens.demand(row, now.Add(ScreenLease), now)
	reading := screens.snapshot(row, now.Add(ScreenLease))
	if len(pipe.attached) != 0 || reading.Channel != ScreenOnDemand {
		t.Fatalf("Session view took Terminal's pipe: attached=%q channel=%s", pipe.attached, reading.Channel)
	}
	// After the first read, ordinary tmux waits for its signal. A Terminal
	// Session must continue refreshing even though it does not own that signal.
	watch := &screenWatch{session: row, at: now}
	if !screens.shouldCaptureOnRead(watch, now.Add(ScreenOnDemandFloor)) {
		t.Fatal("Terminal Session stopped refreshing after its first capture")
	}
	watch.session.ID = "%0"
	if screens.shouldCaptureOnRead(watch, now.Add(ScreenOnDemandFloor)) {
		t.Fatal("ordinary tmux stopped using its signal")
	}
}
