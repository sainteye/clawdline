package app

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// The three loops that each scanned the machine now share one scan, and the
// one caller that may not be given a finished reading still is not.
//
// Everything below is one seam: which consumer gets which freshness. The
// console, the Cloud publisher and the broker's beat all arrive at about the
// same moment on a busy machine, and before this each of them scanned the
// process table, tmux and iTerm2 for itself — and captured a screen per
// qualifying row while it did.
func TestOneScanAnswersEverybodyExceptTheOneThatDecides(t *testing.T) {
	var scans atomic.Int64
	release := make(chan struct{})
	clock := time.Unix(1_000_000, 0)
	r := NewInventoryReading(func(ctx context.Context) session.Inventory {
		scans.Add(1)
		<-release
		return session.Inventory{Complete: true, Provenance: "merged", ObservedAt: clock,
			Sessions: []session.Session{{ID: "%1", Assistant: session.AssistantClaude}}}
	}, 2*time.Second)
	r.now = func() time.Time { return clock }

	// Three readers arriving together: one scan, three answers.
	var wg sync.WaitGroup
	got := make([]session.Inventory, 3)
	for i, read := range []func(context.Context) session.Inventory{r.Recent, r.Recent, r.Fresh} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i] = read(context.Background())
		}()
	}
	// Let them all arrive before the scan answers.
	for r.Counts().Joined < 2 {
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	if n := scans.Load(); n != 1 {
		t.Fatalf("three readers cost %d scans of the machine", n)
	}
	for i, inv := range got {
		if len(inv.Sessions) != 1 {
			t.Fatalf("reader %d was answered %d rows", i, len(inv.Sessions))
		}
	}

	// Inside the TTL a Recent reader costs nothing more.
	clock = clock.Add(time.Second)
	release = make(chan struct{})
	close(release)
	if inv := r.Recent(context.Background()); len(inv.Sessions) != 1 || scans.Load() != 1 {
		t.Fatalf("a reader one second later cost %d scans", scans.Load())
	}
	if c := r.Counts(); c.Held != 1 {
		t.Fatalf("the held reading answered %d readers", c.Held)
	}

	// The broker's is never answered from a reading that had already
	// finished, however young it is: it decides from this whether a child's
	// tab is still there (D05 ③).
	if inv := r.Fresh(context.Background()); len(inv.Sessions) != 1 || scans.Load() != 2 {
		t.Fatalf("the deciding reader was given the shelf: %d scans", scans.Load())
	}

	// And when its own clock runs out first, what comes back is unread — not
	// an older reading wearing this moment's clothes, and not an empty
	// machine. Complete is false, so nothing downstream treats it as the
	// whole list.
	stuck := make(chan struct{})
	r2 := NewInventoryReading(func(ctx context.Context) session.Inventory {
		<-stuck
		return session.Inventory{Complete: true}
	}, 2*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	inv := r2.Fresh(ctx)
	close(stuck)
	if inv.Complete || len(inv.Sessions) != 0 || inv.Provenance != "unread" {
		t.Fatalf("a reader whose clock ran out was answered %+v", inv)
	}
	if len(inv.Notes) == 0 {
		t.Fatal("the unread reading does not say why it is empty")
	}
}

// A reader on a slow clock rides on whatever reading is held while it is
// younger than that clock, and past it is one more Recent reader: it shares a
// scan rather than starting its own, and never takes a reading older than it
// asked for.
func TestASlowReaderRidesOnTheHeldReading(t *testing.T) {
	var scans atomic.Int64
	clock := time.Unix(1_000_000, 0)
	r := NewInventoryReading(func(ctx context.Context) session.Inventory {
		scans.Add(1)
		return session.Inventory{Complete: true, ObservedAt: clock}
	}, 2*time.Second)
	r.now = func() time.Time { return clock }
	r.Fresh(context.Background())
	clock = clock.Add(10 * time.Second)
	if inv := r.Within(context.Background(), 15*time.Second); scans.Load() != 1 || !inv.ObservedAt.Equal(clock.Add(-10*time.Second)) {
		t.Fatalf("a reading ten seconds old cost %d scans for a fifteen-second reader", scans.Load())
	}
	clock = clock.Add(10 * time.Second)
	if inv := r.Within(context.Background(), 15*time.Second); scans.Load() != 2 || !inv.ObservedAt.Equal(clock) {
		t.Fatalf("a reading twenty seconds old was given to a fifteen-second reader (%d scans)", scans.Load())
	}
	if r.Within(context.Background(), 0); scans.Load() != 2 {
		t.Fatalf("an age under the TTL is the TTL: %d scans", scans.Load())
	}
}
