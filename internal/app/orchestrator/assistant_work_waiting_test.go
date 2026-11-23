package orchestrator

import (
	"context"
	"sync"
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
