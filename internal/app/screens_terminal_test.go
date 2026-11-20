package app

import (
	"context"
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

type launchSignal struct {
	ports.PaneSignal
	moved    func(string)
	attached chan string
	detached []string
}

func (s *launchSignal) OnMoved(fn func(string)) { s.moved = fn }
func (s *launchSignal) Attach(id string) bool   { s.attached <- id; return true }
func (s *launchSignal) Detach(id string)        { s.detached = append(s.detached, id) }
func (*launchSignal) DetachAll()                {}

type launchScreenHost struct {
	ports.TerminalHost
	captures chan session.Session
	screens  chan string
}

func (*launchScreenHost) Name() string { return string(session.BackendTmux) }
func (h *launchScreenHost) Screen(ctx context.Context, item session.Session, lines int) (string, bool) {
	h.captures <- item
	select {
	case text := <-h.screens:
		return text, true
	case <-ctx.Done():
		return "", false
	}
}

func TestTmuxLaunchSignalChecksTheExactPaneForAReadyScreen(t *testing.T) {
	const id = "%7"
	pipe := &launchSignal{attached: make(chan string, 1)}
	host := &launchScreenHost{captures: make(chan session.Session, 2), screens: make(chan string, 2)}
	published := make(chan string, 1)
	screens := NewScreens([]ports.TerminalHost{host}, pipe, func(id, _ string) { published <- id })
	defer screens.Stop()
	ready := make(chan struct{}, 2)
	screens.ObserveTmuxLaunch(id, session.AssistantCodex, func() { ready <- struct{}{} })
	select {
	case got := <-pipe.attached:
		if got != id {
			t.Fatalf("attached to %q, want %q", got, id)
		}
	case <-time.After(time.Second):
		t.Fatal("launch did not attach the signal")
	}
	assertCapture := func() session.Session {
		t.Helper()
		select {
		case item := <-host.captures:
			if item.ID != id || item.Assistant != session.AssistantCodex {
				t.Fatalf("captured the wrong execution: %+v", item)
			}
			return item
		case <-time.After(time.Second):
			t.Fatal("launch did not capture the pane")
		}
		return session.Session{}
	}
	item := assertCapture()
	host.screens <- "shell starting up"
	// The first capture may still be finishing. Move the launch watch's
	// coalescer to a trailing edge that reads the newly drawn prompt.
	pipe.moved("%8")
	pipe.moved(id)
	assertCapture()
	select {
	case <-ready:
		t.Fatal("shell startup was mistaken for a ready assistant")
	default:
	}
	host.screens <- "› Ask Codex to do anything"
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("the exact pane's new prompt did not wake discovery")
	}
	pipe.moved(id)
	select {
	case got := <-published:
		t.Fatalf("provisional screen was published for %q", got)
	default:
	}
	if snap := screens.Peek(item); !snap.Pending() {
		t.Fatalf("a provisional launch became a public screen: %+v", snap)
	}
	screens.mu.Lock()
	captures := screens.watches[id].captures
	screens.mu.Unlock()
	if captures != 2 {
		t.Fatalf("a new signal extended a completed launch: %d captures", captures)
	}
	screens.Sweep(time.Now().Add(ScreenLiveAfterPress))
	if len(pipe.detached) != 1 || pipe.detached[0] != id {
		t.Fatalf("expired launch watch left its pipe: detached=%q", pipe.detached)
	}
}
