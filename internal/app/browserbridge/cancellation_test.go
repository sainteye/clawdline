package browserbridge

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// waitQueued waits until the bridge holds n queued commands.
func waitQueued(t *testing.T, b *Bridge, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		got := len(b.queue)
		b.mu.Unlock()
		if got == n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("queue never reached %d", n)
}

// A cancelled owned caller's command, still queued, is never handed to a host.
// It answers browser_cancelled: the daemon knows it never left.
func TestACancelledOwnedCommandIsNeverPolled(t *testing.T) {
	b, _ := connected(t)
	b.mu.Lock()
	b.owner[4] = "bot-a"
	b.mu.Unlock()
	// The caller is cancelled but has not woken yet (its Done is not seen):
	// the poll is the only thing between the cancellation and the effect.
	caller, wakeCaller := context.WithCancel(context.Background())
	t.Cleanup(wakeCaller)
	ctx := &cancelledUnseen{Context: caller}
	readCtx, stopRead := context.WithCancel(context.Background())
	t.Cleanup(stopRead)
	done, other := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := b.DoOwnedWithAdmission(readCtx, "bot-a", "read", json.RawMessage(`{"tab":4}`), func() {})
		other <- err
	}()
	waitQueued(t, b, 1)
	go func() {
		_, err := b.DoOwnedWithAdmission(ctx, "bot-a", "click", json.RawMessage(`{"tab":4,"label":"Pay"}`), func() {})
		done <- err
	}()
	waitQueued(t, b, 2)
	b.mu.Lock()
	ctx.cancelled = true
	b.mu.Unlock()
	polled := b.Poll(context.Background(), HostInfo{Version: "test"})
	if len(polled) != 1 || polled[0].Op != "read" {
		t.Fatalf("a cancelled command was handed out: %+v", polled)
	}
	// Poll has observed Err while Done was still open; now let the caller wake.
	wakeCaller()
	if err := <-done; err != Cancelled {
		t.Fatalf("err = %v, want %s", err, Cancelled)
	}
	// Finish the surviving read before checking the cancellation record.
	if err := b.Deliver([]Result{{ID: polled[0].ID, OK: true, Tab: 4, Value: json.RawMessage(`{"ok":true}`)}}); err != nil {
		t.Fatal(err)
	}
	if err := <-other; err != nil {
		t.Fatalf("surviving read = %v", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.audit) != 2 {
		t.Fatalf("audit = %+v", b.audit)
	}
	seen := map[string]bool{}
	for _, row := range b.audit {
		if seen[row.Op] {
			t.Fatalf("duplicate audit operation = %+v", b.audit)
		}
		seen[row.Op] = true
		switch row.Op {
		case "click":
			if row.OK || row.Error != string(Cancelled) {
				t.Fatalf("cancelled click audit = %+v", row)
			}
		case "read":
			if !row.OK || row.Error != "" {
				t.Fatalf("surviving read audit = %+v", row)
			}
		default:
			t.Fatalf("unexpected audit = %+v", row)
		}
	}
	if len(b.withheld) != 0 || len(b.waiting) != 0 || len(b.inflight) != 0 || len(b.queue) != 0 {
		t.Fatalf("leaked bookkeeping: withheld %v waiting %v inflight %v queue %v", b.withheld, b.waiting, b.inflight, b.queue)
	}
}

// A caller cancelled before admission enqueues nothing.
func TestAnAlreadyCancelledOwnedCallerEnqueuesNothing(t *testing.T) {
	b, _ := connected(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.DoOwnedWithAdmission(ctx, "bot-a", "open", json.RawMessage(`{"url":"https://example.com"}`), func() {}); err != Cancelled {
		t.Fatalf("err = %v, want %s", err, Cancelled)
	}
	if got := b.Status().Pending; got != 0 {
		t.Fatalf("pending = %d", got)
	}
}

// Once a host took the command, cancellation cannot undo it: the caller gets
// a timeout (the effect may have happened), the late result is not returned,
// and nothing is replayed.
func TestACancelAfterPollIsATimeoutAndIsNotReplayed(t *testing.T) {
	b, _ := connected(t)
	b.mu.Lock()
	b.owner[4] = "bot-a"
	b.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := b.DoOwnedWithAdmission(ctx, "bot-a", "click", json.RawMessage(`{"tab":4,"label":"Pay"}`), func() {})
		done <- err
	}()
	waitQueued(t, b, 1)
	polled := b.Poll(context.Background(), HostInfo{Version: "test"})
	if len(polled) != 1 {
		t.Fatalf("polled %d", len(polled))
	}
	cancel()
	if err := <-done; err != TimedOut {
		t.Fatalf("err = %v, want %s", err, TimedOut)
	}
	_ = b.Deliver([]Result{{ID: polled[0].ID, OK: true, Tab: 4, Value: json.RawMessage(`{"late":true}`)}})
	short, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer stop()
	if again := b.Poll(short, HostInfo{Version: "test"}); len(again) != 0 {
		t.Fatalf("the click was replayed: %+v", again)
	}
}

// Closing a cancelled, never-sent close releases its tab reservation.
func TestACancelledOwnedCloseReleasesTheTab(t *testing.T) {
	b, _ := connected(t)
	b.mu.Lock()
	b.owner[4] = "bot-a"
	b.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := b.DoOwnedWithAdmission(ctx, "bot-a", "close", json.RawMessage(`{"tab":4}`), func() {})
		done <- err
	}()
	waitQueued(t, b, 1)
	b.mu.Lock()
	cancel()
	b.mu.Unlock()
	if polled := b.Poll(context.Background(), HostInfo{Version: "test"}); len(polled) != 0 {
		t.Fatalf("polled %+v", polled)
	}
	if err := <-done; err != Cancelled {
		t.Fatalf("err = %v", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closing[4] != "" || b.owner[4] != "bot-a" {
		t.Fatalf("closing %v owner %v", b.closing, b.owner)
	}
}

// cancelledUnseen reports cancellation through Err before its Done fires,
// the window in which a host could otherwise take the command.
type cancelledUnseen struct {
	context.Context
	cancelled bool
}

func (c *cancelledUnseen) Err() error {
	if c.cancelled {
		return context.Canceled
	}
	return nil
}
