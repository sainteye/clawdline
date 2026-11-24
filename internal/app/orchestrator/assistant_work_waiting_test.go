package orchestrator

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A Bot-owned task that starts waiting for a person reaches the Bot host
// once, at the first moment it waits; staying at the dialog does not repeat
// it, and the later settlement is a separate terminal delivery.
func TestBotWorkWaitingForAPersonReachesTheHostOnce(t *testing.T) {
	b, ctx := newTestBroker(t)
	var mu sync.Mutex
	var seen []pa.WorkState
	b.AssistantWorkChanged = func(context.Context, AssistantWorkSnapshot) error { return nil }
	b.AssistantWorkCompleted = func(_ context.Context, s AssistantWorkSnapshot) error {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, s.Link.State)
		return nil
	}
	now := time.Now().UTC()
	id := "b0a10001-0000-4000-8000-000000000001"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "waiting child", State: StateSpawning,
		CreatedAt: now, Claims: []string{}, TimeoutMinutes: 60, SpawnedAt: now,
		AssistantOwner: &AssistantOwner{Fence: pa.Fence{Key: pa.BotKey{MachineID: "m", BotID: "b"}}, TurnID: "turn"}}
	if err := b.save(ctx, r, HashSecret("s"), "task.spawned"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := b.mutate(ctx, id, "task.spawned", func(r *Record) error {
			r.AwaitingDialogSince = now
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	wait := func(n int) []pa.WorkState {
		t.Helper()
		b.RecoverEffects(ctx)
		deadline := time.Now().Add(5 * time.Second)
		for {
			mu.Lock()
			got := append([]pa.WorkState(nil), seen...)
			mu.Unlock()
			if len(got) >= n || time.Now().After(deadline) {
				return got
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if got := wait(1); len(got) != 1 || got[0] != pa.WorkWaitingForPerson {
		t.Fatalf("host deliveries %v, want one waiting", got)
	}
	if _, err := b.Settle(ctx, id, StateCancelled, "", nil); err != nil {
		t.Fatal(err)
	}
	if got := wait(2); len(got) != 2 || got[1] != pa.WorkFailed {
		t.Fatalf("host deliveries %v, want one waiting and one terminal", got)
	}
}

func TestBotLandingRetriesProjectionWithoutAnotherReview(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := b.now()
	b.Clock = func() time.Time { return now }
	id := "b0a10001-0000-4000-8000-000000000002"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "Synthetic result", State: StateSuccess,
		CreatedAt: now, FinishedAt: now, Claims: []string{}, TimeoutMinutes: 60, Landing: &Landing{State: LandingPending},
		AssistantOwner: &AssistantOwner{Fence: pa.Fence{Key: pa.BotKey{MachineID: "m", BotID: "b"}}, TurnID: "turn"}}
	if err := b.save(ctx, r, HashSecret("s"), "task.finished"); err != nil {
		t.Fatal(err)
	}
	var deliveries, reviews atomic.Int32
	b.AssistantWorkChanged = func(_ context.Context, snapshot AssistantWorkSnapshot) error {
		if snapshot.Link.Landing != string(LandingAbandoned) {
			t.Errorf("landing projection = %s", snapshot.Link.Landing)
		}
		if deliveries.Add(1) == 1 {
			return &pa.Failure{Code: pa.StaleVersion, Field: "version"}
		}
		return nil
	}
	b.AssistantWorkCompleted = func(context.Context, AssistantWorkSnapshot) error { reviews.Add(1); return nil }
	if _, err := b.Land(ctx, id, LandingRequest{State: string(LandingAbandoned), Note: "Superseded fixture", Machine: true}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(unattendedPending + time.Second)
	b.RecoverEffects(ctx)
	if deliveries.Load() != 2 || reviews.Load() != 0 {
		t.Fatalf("deliveries=%d reviews=%d", deliveries.Load(), reviews.Load())
	}
}
