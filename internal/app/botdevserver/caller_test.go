package botdevserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestServerCallerCoalescesConcurrentRequestsAndCopiesReceipts(t *testing.T) {
	lifetime, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := NewCaller(lifetime, func(ctx context.Context) error { return ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	run := func(context.Context) (json.RawMessage, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return json.RawMessage(`{"accepted":true}`), nil
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			b, err := c.Do(context.Background(), "one", strings.Repeat("a", 64), run)
			if err != nil || string(b) != `{"accepted":true}` {
				t.Errorf("reply=%s %v", b, err)
			}
			if len(b) > 0 {
				b[0] = 'x'
			}
		}()
	}
	awaitOwner(t, entered)
	close(release)
	workers.Wait()
	b, err := c.Do(context.Background(), "one", strings.Repeat("a", 64), run)
	if err != nil || string(b) != `{"accepted":true}` || calls.Load() != 1 {
		t.Fatalf("replay=%s %v calls=%d", b, err, calls.Load())
	}
	if _, err := c.Do(context.Background(), "one", strings.Repeat("b", 64), run); pa.Code(err) != pa.IdempotencyConflict {
		t.Fatalf("changed payload=%v", err)
	}
}

func TestServerCallerRetainsUnknownAndNoOpReceiptsAtCapacity(t *testing.T) {
	lifetime, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _ := NewCaller(lifetime, func(ctx context.Context) error { return ctx.Err() })
	var calls atomic.Int32
	unknown := func(context.Context) (json.RawMessage, error) {
		calls.Add(1)
		return nil, failure(pa.ExecutionEffectUnknown, "synthetic")
	}
	for range 2 {
		if _, err := c.Do(context.Background(), "unknown", strings.Repeat("a", 64), unknown); pa.Code(err) != pa.ExecutionEffectUnknown {
			t.Fatal(err)
		}
	}
	for n := 1; n < capacity.MaximumBotServerCallerReceipts; n++ {
		_, err := c.Do(context.Background(), fmt.Sprint(n), strings.Repeat("a", 64), func(context.Context) (json.RawMessage, error) {
			calls.Add(1)
			return json.RawMessage(`{"already_running":true}`), nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Do(context.Background(), "overflow", strings.Repeat("a", 64), unknown); pa.Code(err) != pa.CapacityFull {
		t.Fatalf("overflow=%v", err)
	}
	_, _ = c.Do(context.Background(), "1", strings.Repeat("a", 64), unknown)
	_, _ = c.Do(context.Background(), "unknown", strings.Repeat("a", 64), unknown)
	if calls.Load() != capacity.MaximumBotServerCallerReceipts {
		t.Fatal("capacity pressure evicted an earlier no-op or unknown receipt")
	}
}

func TestServerCallerRechecksAuthorityBeforeReturningOrReplayingContent(t *testing.T) {
	lifetime, cancel := context.WithCancel(context.Background())
	defer cancel()
	var revoked atomic.Bool
	c, _ := NewCaller(lifetime, func(context.Context) error {
		if revoked.Load() {
			return failure(pa.ContextStale, "synthetic_forget")
		}
		return nil
	})
	var calls atomic.Int32
	run := func(context.Context) (json.RawMessage, error) {
		calls.Add(1)
		revoked.Store(true)
		return json.RawMessage(`{"private":"synthetic"}`), nil
	}
	for range 2 {
		if b, err := c.Do(context.Background(), "one", strings.Repeat("a", 64), run); len(b) != 0 || pa.Code(err) != pa.ContextStale {
			t.Fatalf("revoked result disclosed: %s %v", b, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("revocation reran a command")
	}
	cancel()
	if _, err := c.Do(context.Background(), "one", strings.Repeat("a", 64), run); pa.Code(err) != pa.Revoked {
		t.Fatal(err)
	}
}

func TestServerCallerRemembersTheResultAfterTheRequestDisconnects(t *testing.T) {
	lifetime, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _ := NewCaller(lifetime, func(ctx context.Context) error { return ctx.Err() })
	request, disconnect := context.WithCancel(context.Background())
	var calls atomic.Int32
	run := func(context.Context) (json.RawMessage, error) {
		calls.Add(1)
		disconnect()
		return json.RawMessage(`{"accepted":true}`), nil
	}
	_, _ = c.Do(request, "one", strings.Repeat("a", 64), run)
	b, err := c.Do(context.Background(), "one", strings.Repeat("a", 64), run)
	if err != nil || string(b) != `{"accepted":true}` || calls.Load() != 1 {
		t.Fatalf("disconnection lost receipt: %s %v calls=%d", b, err, calls.Load())
	}
}
