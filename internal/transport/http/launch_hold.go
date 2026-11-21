package http

import (
	"context"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// A Session this daemon has just started is a shell for its first seconds:
// tmux gives the pane a login shell, the shell reads the person's rc files,
// and only then runs the assistant. The console shows its composer as soon as
// the start route answers, so a person who types at once used to reach that
// shell, and refuseOrdinaryShell answered 409 not_an_agent_session — "an
// ordinary shell" — about the Session they had just opened. Measured on a
// busy macOS machine on 2026-11-21: the start answered at 0.7 s, the shell
// prompt appeared at 2.7–2.9 s and Claude Code's composer at 2.9–3.2 s; a send
// at 1.0 s was refused.
//
// So a write to a pane this daemon launched waits, inside a short window after
// the launch, for the assistant to be running and its screen to be read,
// instead of being refused. The window is the only grant: a pane nobody
// launched here, or one whose window has passed, is refused at once exactly
// as before, and a pane that is still a shell when the window closes is
// refused then. Nothing is typed into the shell either way.

// LaunchHoldSecondsLimit is how long after a launch a write to its pane may wait
// for the assistant (`sessions.launch_hold_seconds`). Long enough for a slow
// login shell on a loaded machine; short enough that the console's own wait
// for one send (sender.ts) is never what ends it.
const LaunchHoldSecondsLimit = 20

// MaxLaunchHoldRows is how many launched panes are remembered at once
// (`sessions.launch_hold_rows`). Past it the launch remembered longest ago is
// forgotten, and a write to that pane is answered as it was before this hold.
const MaxLaunchHoldRows = 64

// launchHoldPoll is the pause between fresh readings while a write waits.
const launchHoldPoll = 150 * time.Millisecond

// launchHoldScreen is how long a running assistant's unread screen is waited
// for once the process is there. Claude Code drew its composer 0.3–0.4 s
// after the shell ran it in the 2026-11-21 runs.
const launchHoldScreen = 3 * time.Second

type launchHolds struct {
	mu    sync.Mutex
	until map[string]time.Time
	now   func() time.Time
}

var launchHoldsByServer sync.Map // *Server -> *launchHolds

// launchHolds is this server's record of the panes it has just launched.
func (s *Server) launchHolds() *launchHolds {
	if h, ok := launchHoldsByServer.Load(s); ok {
		return h.(*launchHolds)
	}
	got, _ := launchHoldsByServer.LoadOrStore(s, &launchHolds{until: map[string]time.Time{}, now: time.Now})
	return got.(*launchHolds)
}

// opened records a pane the start route just launched.
func (h *launchHolds) opened(id string) {
	if id == "" {
		return
	}
	now := h.now()
	h.mu.Lock()
	defer h.mu.Unlock()
	for key, until := range h.until {
		if !now.Before(until) {
			delete(h.until, key)
		}
	}
	for len(h.until) >= MaxLaunchHoldRows {
		oldest, first := "", time.Time{}
		for key, until := range h.until {
			if oldest == "" || until.Before(first) {
				oldest, first = key, until
			}
		}
		delete(h.until, oldest)
	}
	h.until[id] = now.Add(LaunchHoldSecondsLimit * time.Second)
}

// remaining is how much of id's window is left; zero when it was not launched
// here or its window has passed.
func (h *launchHolds) remaining(id string) time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	until, ok := h.until[id]
	if !ok {
		return 0
	}
	left := until.Sub(h.now())
	if left <= 0 {
		delete(h.until, id)
		return 0
	}
	return left
}

// awaitLaunchedAssistant answers the fresh reading of id a write acts on.
// While id is inside its launch window and that reading is still the shell,
// or the assistant whose screen has not been read yet, it reads again until
// the assistant is there, the window closes, or ctx ends; the last reading is
// what it answers.
func (s *Server) awaitLaunchedAssistant(ctx context.Context, actions app.Actions, id string,
	item session.Session, err error) (session.Session, error) {
	holds := s.launchHolds()
	var screenDeadline time.Time
	for err == nil && (!item.IsAssistant() || item.State == session.StateUnknown) {
		left := holds.remaining(id)
		if left <= 0 {
			break
		}
		// A running assistant whose screen still reads as unknown is given
		// launchHoldScreen to draw, not the rest of the window: a screen that
		// cannot be read at all must not delay a write by the whole window.
		if item.IsAssistant() {
			if screenDeadline.IsZero() {
				screenDeadline = holds.now().Add(launchHoldScreen)
			}
			if !holds.now().Before(screenDeadline) {
				break
			}
		}
		timer := time.NewTimer(min(launchHoldPoll, left))
		select {
		case <-ctx.Done():
			timer.Stop()
			return item, err
		case <-timer.C:
		}
		next, nextErr := actions.Find(ctx, id)
		if nextErr != nil && ctx.Err() != nil {
			return item, err
		}
		item, err = next, nextErr
	}
	return item, err
}
