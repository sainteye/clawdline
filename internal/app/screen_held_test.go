package app

import (
	"context"
	"strconv"
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

type sourceFailTerminal struct {
	*slowTerminal
	sourceFailed bool
}

type recordingTerminal struct {
	mu   sync.Mutex
	ids  []string
	fail map[string]bool
}

func (r *recordingTerminal) Capture(_ context.Context, row session.Session) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, row.ID)
	if r.fail[row.ID] {
		delete(r.fail, row.ID)
		return "", false
	}
	return "screen", true
}

func (r *recordingTerminal) captured() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ids...)
}

func (s *sourceFailTerminal) CaptureWithFailure(ctx context.Context, row session.Session) (string, bool, bool) {
	text, ok := s.Capture(ctx, row)
	return text, ok, !ok && s.sourceFailed
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

// An iTerm capture can take longer than the held screen's freshness window.
// Inventory walks rows in a stable order, so without first-capture priority
// the early rows repeatedly refreshed while a later Epic root stayed unknown.
func TestUnreadScreenGetsASlotBeforeEarlierRowsRefresh(t *testing.T) {
	host := &recordingTerminal{fail: map[string]bool{}}
	h := NewHeldScreens(host)
	h.SetLimits(1, ScreenHeldLimit)
	now := time.Unix(2_000_000, 0)
	h.now = func() time.Time { return now }
	rows := []session.Session{
		{ID: "first", Backend: session.BackendITerm},
		{ID: "second", Backend: session.BackendITerm},
		{ID: "third", Backend: session.BackendITerm},
	}
	settle := func() {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			h.mu.Lock()
			quiet := h.inflight == 0
			h.mu.Unlock()
			if quiet {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("capture did not finish")
	}
	for round := 0; round < len(rows); round++ {
		for _, row := range rows {
			h.Capture(context.Background(), row)
		}
		settle()
		now = now.Add(ScreenHeldOnDemand + time.Second)
	}
	if got := host.captured(); len(got) != 3 || got[0] != "first" || got[1] != "second" || got[2] != "third" {
		t.Fatalf("first captures in inventory order = %v, want every row before refresh", got)
	}
	h.Capture(context.Background(), rows[0])
	settle()
	if got := host.captured(); len(got) != 4 || got[3] != "first" {
		t.Fatalf("refresh did not resume after first captures: %v", got)
	}
}

func TestFailedUnreadScreenInBackoffDoesNotHoldOtherCaptures(t *testing.T) {
	host := &recordingTerminal{fail: map[string]bool{"second": true}}
	h := NewHeldScreens(host)
	h.SetLimits(1, ScreenHeldLimit)
	now := time.Unix(2_000_000, 0)
	h.now = func() time.Time { return now }
	rows := []session.Session{
		{ID: "first", Backend: session.BackendITerm},
		{ID: "second", Backend: session.BackendITerm},
		{ID: "third", Backend: session.BackendITerm},
	}
	settle := func() {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			h.mu.Lock()
			quiet := h.inflight == 0
			h.mu.Unlock()
			if quiet {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("capture did not finish")
	}
	for round := 0; round < 3; round++ {
		for _, row := range rows {
			h.Capture(context.Background(), row)
		}
		settle()
		if round == 0 {
			now = now.Add(ScreenHeldOnDemand + time.Second)
		} else {
			now = now.Add(time.Second)
		}
	}
	if got := host.captured(); len(got) != 3 || got[0] != "first" || got[1] != "second" || got[2] != "third" {
		t.Fatalf("failed first capture stalled other rows: %v", got)
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

// A blocked iTerm2 answers none of its tabs. Retrying a different tab on each
// scan used to consume two Apple Event slots continuously despite per-tab
// backoff, and those captures waited ahead of sends.
func TestFailedITermCaptureBacksOffTheSourceNotJustTheTab(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	host := &sourceFailTerminal{sourceFailed: true, slowTerminal: &slowTerminal{answer: func() (string, bool) {
		if fail.Load() {
			return "", false
		}
		return "back", true
	}}}
	h := NewHeldScreens(host)
	h.SetLimits(1, ScreenHeldLimit)
	now := time.Unix(2_000_000, 0)
	h.now = func() time.Time { return now }
	settle := func() {
		t.Helper()
		for i := 0; i < 200; i++ {
			h.mu.Lock()
			quiet := h.inflight == 0
			h.mu.Unlock()
			if quiet {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("capture did not finish")
	}
	row := func(id string, backend session.Backend) session.Session {
		return session.Session{ID: id, Backend: backend, Assistant: session.AssistantClaude}
	}
	h.Capture(context.Background(), row("a", session.BackendITerm))
	settle()
	h.Capture(context.Background(), row("b", session.BackendITerm))
	settle()
	if calls, _, _ := host.stats(); calls != 1 {
		t.Fatalf("another iTerm tab caused %d Apple Events inside source backoff", calls)
	}
	// An iTerm failure must not prevent tmux from being read.
	h.Capture(context.Background(), row("%1", session.BackendTmux))
	settle()
	if calls, _, _ := host.stats(); calls != 2 {
		t.Fatalf("tmux was blocked by iTerm source backoff: %d calls", calls)
	}
	now = now.Add(ScreenBackoffFirst)
	h.Capture(context.Background(), row("b", session.BackendITerm))
	settle()
	if calls, _, _ := host.stats(); calls != 3 {
		t.Fatalf("iTerm source was not probed after backoff: %d calls", calls)
	}
	// A failed probe doubles the quiet stretch; a successful one clears it.
	now = now.Add(ScreenBackoffFirst)
	h.Capture(context.Background(), row("c", session.BackendITerm))
	settle()
	if calls, _, _ := host.stats(); calls != 3 {
		t.Fatalf("iTerm retried before doubled backoff: %d calls", calls)
	}
	now = now.Add(2 * ScreenBackoffFirst)
	fail.Store(false)
	h.Capture(context.Background(), row("c", session.BackendITerm))
	settle()
	h.Capture(context.Background(), row("d", session.BackendITerm))
	settle()
	if calls, _, _ := host.stats(); calls != 5 {
		t.Fatalf("a successful probe did not clear source backoff: %d calls", calls)
	}
}

func TestOneGoneITermTabDoesNotBackOffHealthyTabs(t *testing.T) {
	host := &sourceFailTerminal{slowTerminal: &slowTerminal{answer: func() (string, bool) {
		return "", false
	}}}
	h := NewHeldScreens(host)
	h.SetLimits(1, ScreenHeldLimit)
	settle := func() {
		t.Helper()
		for i := 0; i < 200; i++ {
			h.mu.Lock()
			quiet := h.inflight == 0
			h.mu.Unlock()
			if quiet {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("capture did not finish")
	}
	h.Capture(context.Background(), session.Session{ID: "gone", Backend: session.BackendITerm})
	settle()
	h.Capture(context.Background(), session.Session{ID: "healthy", Backend: session.BackendITerm})
	settle()
	if calls, _, _ := host.stats(); calls != 2 {
		t.Fatalf("a missing tab blocked another iTerm tab: %d calls", calls)
	}
}

func TestITermOutageDoesNotAmplifyAcrossTwentyTabs(t *testing.T) {
	count := func(sourceFailed bool) int {
		host := &sourceFailTerminal{sourceFailed: sourceFailed,
			slowTerminal: &slowTerminal{answer: func() (string, bool) { return "", false }}}
		h := NewHeldScreens(host)
		h.SetLimits(1, ScreenHeldLimit)
		start := time.Unix(2_000_000, 0)
		for second := 0; second < 60; second++ {
			now := start.Add(time.Duration(second) * time.Second)
			h.now = func() time.Time { return now }
			id := string(rune('a' + second%20))
			h.Capture(context.Background(), session.Session{ID: id, Backend: session.BackendITerm})
			for i := 0; i < 200; i++ {
				h.mu.Lock()
				quiet := h.inflight == 0
				h.mu.Unlock()
				if quiet {
					break
				}
				if i == 199 {
					t.Fatal("capture did not finish")
				}
				time.Sleep(time.Millisecond)
			}
		}
		calls, _, _ := host.stats()
		return calls
	}
	perTab, perSource := count(false), count(true)
	t.Logf("60 one-second requests across 20 failing tabs: per-tab=%d Apple Events, per-source=%d", perTab, perSource)
	if perTab < 50 || perSource > 5 {
		t.Fatalf("failure amplification remained: per-tab=%d per-source=%d", perTab, perSource)
	}
}

// changingTerminal answers every pane with whatever the machine shows now.
type changingTerminal struct {
	mu   sync.Mutex
	show string
}

func (c *changingTerminal) Capture(context.Context, session.Session) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.show, true
}

// A row at the end of the list is refreshed like the rest of it. Every slot
// used to go to whichever stale row asked first, and the list asks in one
// order: on 2026-10-02 the front of twenty-six rows took both slots every
// tick, a pane near the end kept the screen it had before its menu was drawn,
// and the console said the menu's choices could not be read.
func TestARowAtTheEndOfTheListIsRefreshedToo(t *testing.T) {
	host := &changingTerminal{show: "prompt"}
	h := NewHeldScreens(host)
	now := time.Unix(2_000_000, 0)
	h.now = func() time.Time { return now }
	rows := make([]session.Session, 8)
	for i := range rows {
		rows[i] = session.Session{ID: "%" + strconv.Itoa(i), Backend: session.BackendTmux}
	}
	settle := func() {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			h.mu.Lock()
			quiet := h.inflight == 0
			h.mu.Unlock()
			if quiet {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("capture did not finish")
	}
	pass := func() {
		for _, row := range rows {
			h.Capture(context.Background(), row)
		}
		settle()
		now = now.Add(ScreenHeldTmux)
	}
	for i := 0; i < len(rows); i++ {
		pass()
	}
	last := rows[len(rows)-1]
	if text, ok := h.Capture(context.Background(), last); !ok || text != "prompt" {
		t.Fatalf("the last row was never read: %q %v", text, ok)
	}
	settle()

	host.mu.Lock()
	host.show = "menu"
	host.mu.Unlock()
	// Two slots and eight rows due every pass: four passes reach every row.
	for i := 0; i < len(rows); i++ {
		pass()
	}
	if text, _ := h.Capture(context.Background(), last); text != "menu" {
		t.Fatalf("after %d passes the last row still holds %q", len(rows), text)
	}
}
