package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// slowSource is a process table or terminal that answers after delay, the
// shape of iTerm2's list Apple Event on a busy machine.
type slowSource struct {
	ports.TerminalHost
	name  string
	delay time.Duration
	rows  []session.Session
	asked atomic.Int32
}

func (s *slowSource) Name() string { return s.name }

func (s *slowSource) answer(ctx context.Context) session.Inventory {
	s.asked.Add(1)
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
	}
	return session.Inventory{ObservedAt: time.Now(), Provenance: s.name, Complete: true,
		Sessions: append([]session.Session(nil), s.rows...)}
}

func (s *slowSource) Inventory(ctx context.Context) (session.Inventory, error) {
	return s.answer(ctx), nil
}

func (s *slowSource) Scan(ctx context.Context) (session.Inventory, error) {
	return s.answer(ctx), nil
}

// The sources are asked together, so a reading takes as long as its slowest
// source rather than the sum — and the merge is the one asking them in turn
// made: the process table's row is merged first and keeps what it brought,
// although tmux answered long before it.
func TestTheSourcesAreAskedTogetherAndMergedInTheirOrder(t *testing.T) {
	const delay = 300 * time.Millisecond
	ps := &slowSource{name: "ps", delay: delay, rows: []session.Session{{
		ID: "ps:ttys001", TTY: "ttys001", CWD: "/from-ps", PID: 41, Assistant: session.AssistantClaude}}}
	iterm := &slowSource{name: "iterm", delay: delay}
	tmux := &slowSource{name: "tmux", rows: []session.Session{{
		ID: "%7", Backend: session.BackendTmux, TTY: "ttys001", CWD: "/from-tmux"}}}
	in := Inventory{Process: ps, Terminals: []ports.TerminalHost{iterm, tmux}}

	start := time.Now()
	inv := in.Read(context.Background())
	took := time.Since(start)
	// In turn, ps then iTerm2 would be 2×delay; together it is one.
	sequential := 2 * delay
	t.Logf("three sources, two of them %v slow: read in %v (asked in turn: at least %v)", delay, took, sequential)
	if took >= sequential {
		t.Fatalf("the sources were asked in turn: %v", took)
	}
	if !inv.Complete || len(inv.Sessions) != 1 {
		t.Fatalf("merged reading: %+v", inv)
	}
	row := inv.Sessions[0]
	if row.CWD != "/from-ps" || row.PID != 41 {
		t.Fatalf("the merge did not keep the first source's row first: %+v", row)
	}
	for _, name := range []string{"ps", "iterm", "tmux"} {
		if complete, ok := inv.Sources[name]; !ok || !complete {
			t.Fatalf("source %s: %v %v", name, complete, ok)
		}
	}
}

// A pinned read's check is answered by a scan that finished less than the age
// ago and does not wait for the slow source; past the age it takes a reading
// of its own, exactly as Fresh.
func TestAPinnedReadUsesARecentScanAndAnOldOneIsReplaced(t *testing.T) {
	const delay = 300 * time.Millisecond
	iterm := &slowSource{name: "iterm", delay: delay}
	in := Inventory{Terminals: []ports.TerminalHost{iterm}}
	r := NewInventoryReading(in.Read, 0)
	clock := time.Now()
	r.now = func() time.Time { return clock }

	start := time.Now()
	r.Fresh(context.Background())
	before := time.Since(start)

	start = time.Now()
	r.ScannedWithin(context.Background(), PinnedReadAgeLimit)
	after := time.Since(start)
	t.Logf("execution check: fresh scan %v, scan finished 0 s ago %v", before, after)
	if before < delay || after >= delay/2 {
		t.Fatalf("the check waited for the slow source: fresh %v, recent %v", before, after)
	}
	if got := iterm.asked.Load(); got != 1 {
		t.Fatalf("iTerm2 was asked %d times, want 1", got)
	}

	clock = clock.Add(PinnedReadAgeLimit)
	start = time.Now()
	r.ScannedWithin(context.Background(), PinnedReadAgeLimit)
	if took := time.Since(start); took < delay || iterm.asked.Load() != 2 {
		t.Fatalf("a scan %v old answered the check: %v, asked %d", PinnedReadAgeLimit, took, iterm.asked.Load())
	}
}

// A write's own fresh reading is used by the first Find under its request and
// by nothing after it.
func TestATakenReadingIsUsedOnce(t *testing.T) {
	iterm := &slowSource{name: "iterm", rows: []session.Session{{
		ID: "w1", Backend: session.BackendITerm, TTY: "ttys002", Assistant: session.AssistantClaude}}}
	in := Inventory{Terminals: []ports.TerminalHost{iterm}}
	r := NewInventoryReading(in.Read, 0)
	a := Actions{Inventory: in, Reading: r}
	ctx := WithTakenReading(context.Background(), r.Fresh(context.Background()))
	if _, err := a.Find(ctx, "w1"); err != nil {
		t.Fatal(err)
	}
	if got := r.Counts().Scans; got != 1 {
		t.Fatalf("the first Find scanned again: %d scans", got)
	}
	if _, err := a.Find(ctx, "w1"); err != nil {
		t.Fatal(err)
	}
	if got := r.Counts().Scans; got != 2 {
		t.Fatalf("the second Find did not scan for itself: %d scans", got)
	}
}
