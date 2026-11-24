package botdevserver

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A failed read of the place registry or store during one routine tick is not
// a revocation: the server keeps running and the next successful tick clears
// the unconfirmed mark without a daemon restart.
func TestReconcileKeepsTheServerThroughAFailedReadAndRecoversOnTheNextTick(t *testing.T) {
	o, _, launches, _ := registryFixture(t)
	var mu sync.Mutex
	now := time.Unix(1_800_000_000, 0)
	o.Clock = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	r := openRegistry(t, o)
	saved, err := r.Start(context.Background(), specFixture(), "first")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, saved, Running)
	readFailure := errors.New("synthetic place registry read: input/output error")
	if err := r.Reconcile(context.Background(), func(context.Context, Spec) error { return readFailure }); !errors.Is(err, readFailure) {
		t.Fatalf("read failure was not reported: %v", err)
	}
	if got := r.view(r.rows[saved.ID]); got.Attempt.State != Running {
		t.Fatalf("one failed read stopped the server: %+v", got.Attempt)
	}
	if r.Unconfirmed(saved.ID) {
		t.Fatal("a single failed read is shown as stuck")
	}
	mu.Lock()
	now = now.Add(capacity.BotServerUnconfirmedNotice)
	mu.Unlock()
	_ = r.Reconcile(context.Background(), func(context.Context, Spec) error { return readFailure })
	if !r.Unconfirmed(saved.ID) {
		t.Fatal("a long unreadable authority is not shown as unconfirmed")
	}
	if got := r.view(r.rows[saved.ID]); got.Attempt.State != Running {
		t.Fatalf("a stuck check stopped the server for the person: %+v", got.Attempt)
	}
	if err := r.Reconcile(context.Background(), func(context.Context, Spec) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if r.Unconfirmed(saved.ID) {
		t.Fatal("a successful tick did not clear the unconfirmed mark")
	}
	if got := r.view(r.rows[saved.ID]); got.Attempt.State != Running || launches.Load() != 1 {
		t.Fatalf("recovery needed a restart: %+v launches=%d", got.Attempt, launches.Load())
	}
}

// An authoritative denial still stops the server and a later successful
// check does not start it again.
func TestReconcileStillStopsOnAnAuthoritativeDenial(t *testing.T) {
	o, _, launches, _ := registryFixture(t)
	r := openRegistry(t, o)
	saved, err := r.Start(context.Background(), specFixture(), "first")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, saved, Running)
	for _, denial := range []error{failure(pa.ScopeDenied, "synthetic_scope"), failure(pa.NotFound, "synthetic_bot")} {
		if !AuthoritativeDenial(denial) {
			t.Fatalf("%v is not authoritative", denial)
		}
	}
	if err := r.Reconcile(context.Background(), func(context.Context, Spec) error { return failure(pa.ScopeDenied, "synthetic_scope") }); err != nil {
		t.Fatal(err)
	}
	if got := r.view(r.rows[saved.ID]); got.Attempt.State != Stopped {
		t.Fatalf("authoritative denial kept the server: %+v", got.Attempt)
	}
	if err := r.Reconcile(context.Background(), func(context.Context, Spec) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if got := r.view(r.rows[saved.ID]); got.Attempt.State != Stopped || launches.Load() != 1 {
		t.Fatalf("a revoked server restarted itself: %+v", got.Attempt)
	}
}

// After a daemon restart the lost attempt still refuses start, stop and plain
// removal, but the person can release it and then remove the definition.
func TestAPersonCanReleaseAndRemoveAServerWhoseOwnerWasLost(t *testing.T) {
	o, c, launches, _ := registryFixture(t)
	s := specFixture()
	fingerprint, _ := s.Fingerprint()
	i := Identity{Key: s.Key, Project: s.Project, ServerID: "saved", AttemptID: "interrupted", Fingerprint: fingerprint}
	durable := Record{Identity: i, Version: 2, State: Running}
	c.rows[i.ServerID] = SavedServer{ID: i.ServerID, Spec: s, Attempt: durable}
	c.journals[i] = &journalFixture{record: durable, changed: make(chan Record, 16)}
	r := openRegistry(t, o)
	if err := r.Remove(context.Background(), s.Key, s.Project, i.ServerID, i.AttemptID); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatalf("removal of an unproved process was not refused: %v", err)
	}
	lost := r.Snapshot(s.Key, s.Project)[0]
	if lost.Attempt.State != Unknown || lost.Attempt.Failure != "owner_lost" {
		t.Fatalf("restart did not report a lost owner: %+v", lost.Attempt)
	}
	if err := r.ReleaseExpected(context.Background(), lost); err != nil {
		t.Fatal(err)
	}
	released := r.Snapshot(s.Key, s.Project)[0]
	if released.Attempt.State != Failed || released.Attempt.Failure != ReleasedUnconfirmed || c.rows[i.ServerID].Attempt != released.Attempt {
		t.Fatalf("release was not journalled: %+v durable=%+v", released.Attempt, c.rows[i.ServerID].Attempt)
	}
	if u := r.Usage(); !u.Known || u.Processes != 0 {
		t.Fatalf("released slot still counted: %+v", u)
	}
	if err := r.RemoveExpected(context.Background(), released); err != nil {
		t.Fatalf("released server cannot be removed: %v", err)
	}
	if len(r.Snapshot(s.Key, s.Project)) != 0 || launches.Load() != 0 {
		t.Fatal("removal left the definition or launched a process")
	}
}

// Release is only for an unconfirmed owner. A running, confirmed server must
// be stopped, and a stale view cannot release a newer attempt.
func TestReleaseRefusesAConfirmedOwnerAndAStaleView(t *testing.T) {
	o, _, _, _ := registryFixture(t)
	r := openRegistry(t, o)
	saved, err := r.Start(context.Background(), specFixture(), "first")
	if err != nil {
		t.Fatal(err)
	}
	waitRegistry(t, r, saved, Running)
	current := r.view(r.rows[saved.ID])
	if err := r.ReleaseExpected(context.Background(), current); pa.Code(err) != pa.Invalid {
		t.Fatalf("a confirmed owner was released: %v", err)
	}
	if err := r.ReleaseExpected(context.Background(), saved); pa.Code(err) != pa.StaleVersion {
		t.Fatalf("a stale view was accepted: %v", err)
	}
}
