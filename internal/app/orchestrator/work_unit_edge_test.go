package orchestrator

import (
	"testing"
	"time"
)

// The token ledger's work-unit cursors hear a task's admission and its end
// from the broker (docs/token-ledger.md "One unit of work"). A failed or
// cancelled ending is an end like any other, with the broker's state as its
// outcome, and an end settled twice or a dispatch replayed is told once.
func TestATaskEndIsToldOnceWithItsOutcomeHoweverItEnds(t *testing.T) {
	for _, end := range []State{StateFailure, StateCancelled, StateTimeout, StateSpawnFailed, StateSuccess} {
		t.Run(string(end), func(t *testing.T) {
			b, ctx := newTestBroker(t)
			type edge struct{ task, edge, outcome string }
			var told []edge
			b.WorkUnitEdge = func(taskID, e, outcome string, at time.Time) {
				if at.IsZero() {
					t.Errorf("the %s edge of %s has no time", e, taskID)
				}
				told = append(told, edge{taskID, e, outcome})
			}
			id := "33333333-3333-4333-8333-333333333333"
			r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "t", State: StateQueued,
				CreatedAt: time.Now(), Claims: []string{}, Root: &RootRef{SessionID: rootConversation, Assistant: "claude"}}
			if _, err := b.create(ctx, r, HashSecret("s")); err != nil {
				t.Fatal(err)
			}
			// The same dispatch replayed: refused, and not a second start.
			if _, err := b.create(ctx, r, HashSecret("s")); refusalCode(err) != "task_exists" {
				t.Fatalf("a replayed dispatch: %v", err)
			}
			if _, err := b.Settle(ctx, id, end, "it ended", nil); err != nil {
				t.Fatal(err)
			}
			// Settled again by a second collector: not a second end.
			if _, err := b.Settle(ctx, id, StateSuccess, "again", nil); err == nil {
				t.Fatal("a second settlement was accepted")
			}
			want := []edge{{id, "start", "queued"}, {id, "end", string(end)}}
			if len(told) != len(want) || told[0] != want[0] || told[1] != want[1] {
				t.Fatalf("told %+v, want %+v", told, want)
			}
		})
	}
}
