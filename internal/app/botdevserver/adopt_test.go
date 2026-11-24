package botdevserver

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// A long unreadable authority is reported as unconfirmed and logged exactly
// once, however many ticks repeat it; recovery is logged once too.
func TestUnconfirmedIsLoggedOnceAcrossRepeatedTicks(t *testing.T) {
	o, _, _, _ := registryFixture(t)
	var mu sync.Mutex
	now := time.Unix(1_800_000_000, 0)
	o.Clock = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	var lines []string
	o.Logf = func(format string, args ...any) { mu.Lock(); defer mu.Unlock(); lines = append(lines, fmt.Sprintf(format, args...)) }
	r := openRegistry(t, o)
	saved, err := r.Start(context.Background(), specFixture(), "first")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, saved, Running)
	readFailure := errors.New("synthetic store read failure")
	fail := func(context.Context, Spec) error { return readFailure }
	for tick := 0; tick < 40; tick++ {
		_ = r.Reconcile(context.Background(), fail)
		mu.Lock()
		now = now.Add(time.Second)
		mu.Unlock()
	}
	if !r.Unconfirmed(saved.ID) {
		t.Fatal("a 40 s unreadable authority is not unconfirmed")
	}
	_ = r.Reconcile(context.Background(), fail)
	mu.Lock()
	got := append([]string(nil), lines...)
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("unconfirmed notice logged %d times, want once: %q", len(got), got)
	}
	if err := r.Reconcile(context.Background(), func(context.Context, Spec) error { return nil }); err != nil {
		t.Fatal(err)
	}
	_ = r.Reconcile(context.Background(), func(context.Context, Spec) error { return nil })
	mu.Lock()
	defer mu.Unlock()
	if len(lines) != 2 {
		t.Fatalf("recovery not logged exactly once: %q", lines)
	}
	if capacity.BotServerUnconfirmedNotice > 40*time.Second {
		t.Fatal("fixture assumes the notice is at most 40 s")
	}
}

type adoptedFixture struct {
	processFixture
	mark Mark
}

func (p *adoptedFixture) Mark() (Mark, bool) { return p.mark, true }

func lostRunning(t *testing.T, o *RegistryOptions, c *catalogFixture, mark Mark) (Spec, Identity) {
	t.Helper()
	s := specFixture()
	fingerprint, _ := s.Fingerprint()
	i := Identity{Key: s.Key, Project: s.Project, ServerID: "saved", AttemptID: "before-restart", Fingerprint: fingerprint}
	durable := Record{Identity: i, Version: 2, State: Running, Mark: mark}
	c.rows[i.ServerID] = SavedServer{ID: i.ServerID, Spec: s, Attempt: durable}
	c.journals[i] = &journalFixture{record: durable, changed: make(chan Record, 16)}
	return s, i
}

// After a Host restart, a running attempt whose journalled PID and start time
// still name a live process is adopted as running, counted, and stoppable.
func TestARestartAdoptsTheStillRunningServer(t *testing.T) {
	o, c, launches, _ := registryFixture(t)
	mark := Mark{PID: 4242, Started: 1_800_000_000}
	s, i := lostRunning(t, &o, c, mark)
	p := &adoptedFixture{processFixture: processFixture{exit: make(chan struct{})}, mark: mark}
	var asked Mark
	o.Adopt = func(_ context.Context, _ Spec, m Mark) (Process, error) { asked = m; return p, nil }
	r := openRegistry(t, o)
	got := r.Snapshot(s.Key, s.Project)[0]
	if got.Attempt.State != Running || got.Attempt.Failure != "" || asked != mark {
		t.Fatalf("running server was not adopted: %+v asked=%+v", got.Attempt, asked)
	}
	if u := r.Usage(); !u.Known || u.Processes != 1 || launches.Load() != 0 {
		t.Fatalf("adoption counted wrong or launched: %+v launches=%d", u, launches.Load())
	}
	if err := r.StopExpected(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	stopped := r.Snapshot(s.Key, s.Project)[0]
	if stopped.Attempt.State != Stopped || p.closeCount.Load() == 0 || c.journals[i].record.State != Stopped {
		t.Fatalf("adopted server was not stopped through its process: %+v closes=%d", stopped.Attempt, p.closeCount.Load())
	}
	if err := r.RemoveExpected(context.Background(), stopped); err != nil {
		t.Fatalf("stopped adopted server cannot be removed: %v", err)
	}
	if u := r.Usage(); u.Processes != 0 || u.Definitions != 0 {
		t.Fatalf("removal left usage: %+v", u)
	}
}

// When the process is gone or its start time differs, nothing is adopted and
// the attempt stays owner_lost as before.
func TestARestartDoesNotAdoptAMismatchedProcess(t *testing.T) {
	o, c, _, _ := registryFixture(t)
	s, _ := lostRunning(t, &o, c, Mark{PID: 4242, Started: 1_800_000_000})
	o.Adopt = func(context.Context, Spec, Mark) (Process, error) { return nil, nil }
	r := openRegistry(t, o)
	got := r.Snapshot(s.Key, s.Project)[0]
	if got.Attempt.State != Unknown || got.Attempt.Failure != "owner_lost" {
		t.Fatalf("a mismatched process was adopted: %+v", got.Attempt)
	}
}
