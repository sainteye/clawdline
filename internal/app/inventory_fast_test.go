package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestSlowITermRefreshDoesNotHoldReadersOrBecomeASecondScan(t *testing.T) {
	var now atomic.Int64
	now.Store(1_000_000)
	var calls atomic.Int64
	started := make(chan struct{})
	release := make(chan struct{})
	r := NewInventoryReading(func(context.Context) session.Inventory {
		n := calls.Add(1)
		observed := time.Unix(now.Load(), 0)
		if n == 2 {
			close(started)
			<-release
		}
		state := session.StateWorking
		if n > 1 {
			state = session.StateIdle
		}
		return session.Inventory{ObservedAt: observed, Complete: true,
			Provenance: "merged", Sources: map[string]bool{"iterm": true},
			Sessions: []session.Session{{ID: "GUID-A", Backend: session.BackendITerm,
				Assistant: session.AssistantCodex, State: state}}}
	}, time.Second)
	r.now = func() time.Time { return time.Unix(now.Load(), 0) }
	r.SetRetentionAge(120)
	if first := r.Fast(context.Background()); len(first.Sessions) != 1 || !first.Complete {
		t.Fatalf("first reading: %+v", first)
	}
	now.Add(3)
	read := make(chan session.Inventory, 1)
	go func() { read <- r.Fast(context.Background()) }()
	select {
	case stale := <-read:
		if stale.Complete || len(stale.Sessions) != 1 ||
			stale.Sessions[0].Observation.Freshness != session.FreshnessUnverified ||
			stale.Sessions[0].State != session.StateWorking {
			t.Fatalf("the slow scan's prior row: %+v", stale)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("a drawing waited for the Apple Event")
	}
	<-started
	for range 5 {
		if got := r.Fast(context.Background()); len(got.Sessions) != 1 {
			t.Fatalf("reader lost the held row: %+v", got)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("%d scans ran while the first refresh was held", calls.Load())
	}
	a := Actions{Reading: r}
	if row, err := a.FindForRead(context.Background(), "GUID-A"); err != nil || row.ID != "GUID-A" {
		t.Fatalf("read-only lookup did not use the displayed row: %+v, %v", row, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := a.Find(ctx, "GUID-A"); err == nil {
		t.Fatal("a deciding lookup used an unverified row")
	}
	now.Add(10) // the Apple Event finished long after it began
	close(release)
	deadline := time.After(time.Second)
	for {
		if held, ok := r.Held(); ok && len(held.Sessions) == 1 && held.Sessions[0].State == session.StateIdle {
			break
		}
		select {
		case <-deadline:
			t.Fatal("the background scan did not publish its new reading")
		case <-time.After(time.Millisecond):
		}
	}
	if got := r.Fast(context.Background()); len(got.Sessions) != 1 || calls.Load() != 2 {
		t.Fatalf("a slow completed scan was repeated immediately: calls=%d reading=%+v", calls.Load(), got)
	}
}

func TestFastInventoryNeverExtendsAStaleRowPastItsRetention(t *testing.T) {
	var now atomic.Int64
	now.Store(2_000_000)
	var calls atomic.Int64
	release := make(chan struct{})
	r := NewInventoryReading(func(context.Context) session.Inventory {
		if calls.Add(1) == 2 {
			<-release
		}
		return session.Inventory{ObservedAt: time.Unix(now.Load(), 0), Complete: true,
			Provenance: "iterm", Sources: map[string]bool{"iterm": true},
			Sessions: []session.Session{{ID: "GUID-A", Backend: session.BackendITerm}}}
	}, time.Second)
	r.now = func() time.Time { return time.Unix(now.Load(), 0) }
	r.SetRetentionAge(120)
	r.Fast(context.Background())
	now.Add(120)
	got := r.Fast(context.Background())
	close(release)
	if got.Complete || len(got.Sessions) != 0 || got.Observation.Freshness != session.FreshnessMissing {
		t.Fatalf("expired reading: %+v", got)
	}
}
