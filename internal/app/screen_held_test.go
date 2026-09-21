package app

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// slowTerminal is a screen reader that takes as long as it is told to and
// counts how many captures are running at once.
type slowTerminal struct {
	takes  time.Duration
	answer func() (string, bool)

	mu      sync.Mutex
	running int
	peak    int
	calls   int
	seen    []context.Context
}

func (s *slowTerminal) Capture(ctx context.Context, _ session.Session) (string, bool) {
	s.mu.Lock()
	s.running++
	s.calls++
	if s.running > s.peak {
		s.peak = s.running
	}
	s.seen = append(s.seen, ctx)
	s.mu.Unlock()
	time.Sleep(s.takes)
	s.mu.Lock()
	s.running--
	s.mu.Unlock()
	if s.answer != nil {
		return s.answer()
	}
	return "screen", true
}

func (s *slowTerminal) stats() (calls, peak int, seen []context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, s.peak, append([]context.Context(nil), s.seen...)
}

// Building the list never waits for a terminal, never starts more captures
// than its slot limit, and never hands a capture the reader's dead clock.
//
// The three are one seam: what a slow or unreachable terminal costs the
// console. Before this each qualifying row was captured inline — twenty rows
// meant twenty AppleScripts in a queue every reader waited behind, and a
// reader past its deadline went on to start one anyway.
func TestTheListDoesNotWaitForATerminalAndIsBoundedWhileItDoesNot(t *testing.T) {
	host := &slowTerminal{takes: 200 * time.Millisecond}
	h := NewHeldScreens(host)

	rows := make([]session.Session, 20)
	for i := range rows {
		rows[i] = session.Session{ID: string(rune('a'+i)) + "-tab", Backend: session.BackendITerm,
			Assistant: session.AssistantClaude}
	}

	// A reader whose own context is already dead: it is answered at once, and
	// what it started did not inherit that context.
	dead, cancel := context.WithCancel(context.Background())
	cancel()
	began := time.Now()
	for _, row := range rows {
		if _, ok := h.Capture(dead, row); ok {
			t.Fatal("a screen was answered before anything had been captured")
		}
	}
	if waited := time.Since(began); waited > 100*time.Millisecond {
		t.Fatalf("building the list waited %s on a terminal", waited)
	}

	// The captures themselves run behind the answer, so give them a moment to
	// start before looking at how many there are.
	for i := 0; i < 200; i++ {
		if n, _, _ := host.stats(); n > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	calls, peak, seen := host.stats()
	if peak > ScreenCaptureLimit {
		t.Errorf("%d captures ran at once; the register allows %d", peak, ScreenCaptureLimit)
	}
	if calls > ScreenCaptureLimit {
		t.Errorf("%d captures were started for one round of twenty rows", calls)
	}
	if calls == 0 {
		t.Fatal("nothing was captured at all")
	}
	for _, ctx := range seen {
		if ctx.Err() != nil {
			t.Fatal("a capture was started on a context that had already expired")
		}
	}
	if c := h.Counts(); c.Refused == 0 {
		t.Error("the rows that found no slot were not counted as refused")
	}

	// Once a capture has come back, the row it belongs to is answered from
	// what is held rather than captured again.
	for {
		if text, ok := h.Capture(context.Background(), rows[0]); ok {
			if text != "screen" {
				t.Fatalf("held screen is %q", text)
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	before, _, _ := host.stats()
	for i := 0; i < 5; i++ {
		if _, ok := h.Capture(context.Background(), rows[0]); !ok {
			t.Fatal("a held screen stopped being answered")
		}
	}
	if after, _, _ := host.stats(); after != before {
		t.Errorf("five reads of a screen held for %s cost %d more captures", ScreenHeldOnDemand, after-before)
	}
}

// A terminal that cannot be read is left alone for longer each time, and one
// success clears it. Without this a tab with no automation permission costs a
// subprocess on every tick, for ever.
func TestATerminalThatCannotBeReadIsLeftAlone(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	host := &slowTerminal{answer: func() (string, bool) {
		if fail.Load() {
			return "", false
		}
		return "back", true
	}}
	h := NewHeldScreens(host)
	now := time.Unix(2_000_000, 0)
	h.now = func() time.Time { return now }
	row := session.Session{ID: "%7", Backend: session.BackendTmux, Assistant: session.AssistantClaude}

	settle := func() {
		for i := 0; i < 200; i++ {
			h.mu.Lock()
			quiet := h.inflight == 0
			h.mu.Unlock()
			if quiet {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("a capture never came back")
	}

	h.Capture(context.Background(), row)
	settle()
	first, _, _ := host.stats()
	if first != 1 {
		t.Fatalf("the first read cost %d captures", first)
	}
	// Inside the backoff, asking again costs nothing.
	now = now.Add(ScreenHeldTmux + time.Second)
	h.Capture(context.Background(), row)
	settle()
	if again, _, _ := host.stats(); again != 1 {
		t.Fatalf("a terminal that had just failed was asked again: %d captures", again)
	}
	if c := h.Counts(); c.Backoff == 0 {
		t.Error("the skipped refresh was not counted as backoff")
	}

	// Past it, it is asked once more — and a success clears the backoff.
	now = now.Add(ScreenBackoffFirst)
	fail.Store(false)
	h.Capture(context.Background(), row)
	settle()
	if asked, _, _ := host.stats(); asked != 2 {
		t.Fatalf("past the backoff the terminal was asked %d times in all", asked)
	}
	if text, ok := h.Capture(context.Background(), row); !ok || text != "back" {
		t.Fatalf("the screen that came back was not held: %q %v", text, ok)
	}
}
