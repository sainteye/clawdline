package projectassistant

import (
	"context"
	"strings"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestRuntimeContinuationFreshOnlyAndUnknownIsNotReplayed(t *testing.T) {
	for _, mode := range []string{"resume", "missing native history", "acknowledged unknown"} {
		t.Run(mode, func(t *testing.T) {
			f := newRuntimeFixture(t)
			topic := "Research https://example.invalid/topic-alpha"
			first, err := f.submit(t, topic, "topic")
			if err != nil {
				t.Fatal(err)
			}
			q := receivePrompt(t, f.provider)
			old := f.provider.session()
			old.reply <- PromptResult{FinalText: "The banner is 1600 by 900"}
			awaitTurn(t, f, first.ResourceID, pa.TurnSucceeded)
			if mode != "resume" {
				f.provider.mu.Lock()
				f.provider.refuseResume = true
				f.provider.mu.Unlock()
			}
			if mode == "acknowledged unknown" {
				f.store.mu.Lock()
				prior := f.store.turns[first.ResourceID]
				prior.State = pa.TurnUnknown
				prior.Failure = &pa.Failure{Code: pa.ExecutionEffectUnknown}
				f.store.turns[first.ResourceID] = prior
				f.store.mu.Unlock()
			}
			second, err := f.submit(t, "Please research", "followup")
			if err != nil {
				t.Fatal(err)
			}
			next := receivePrompt(t, f.provider)
			if next.Text != "Please research" {
				t.Fatal("old input was replayed", next.Text)
			}
			if mode == "resume" {
				if next.Handoff != nil || next.MemoryChanged {
					t.Fatal("normal resume received redundant recap")
				}
			} else {
				if next.Handoff == nil {
					t.Fatal("fresh session lost handoff")
				}
				opening, err := next.Handoff.Opening(next.Turn, next.Context)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(opening, topic) {
					t.Fatal("fresh prompt lost topic", opening)
				}
				if mode == "acknowledged unknown" {
					if strings.Contains(opening, "The banner is") || len(next.Handoff.Continuation) != 1 || next.Handoff.Continuation[0].State != pa.TurnUnknown {
						t.Fatal("unknown was turned into completed work", opening)
					}
				}
			}
			// An old execution cannot add a reply to this fresh/resumed turn.
			if err := old.sink(context.Background(), ProtocolEvent{ID: "late", Fence: q.Turn.Fence, TurnID: q.Turn.ID, Message: "late old reply"}); err == nil {
				t.Fatal("late event accepted")
			}
			f.provider.session().reply <- PromptResult{FinalText: "One reply"}
			awaitTurn(t, f, second.ResourceID, pa.TurnSucceeded)
			x, _ := f.store.Export(context.Background(), f.key)
			if len(x.Messages) != 4 || len(f.provider.sent) != 0 {
				t.Fatal("extra prompt or visible reply", x.Messages)
			}
		})
	}
}

type continuationBoundaryMemory struct {
	*runtimeMemory
	before func()
}

func (m continuationBoundaryMemory) WithCurrent(ctx context.Context, key pa.BotKey, revision int64, f func(pa.ContextSnapshot) error) error {
	m.before()
	return m.runtimeMemory.WithCurrent(ctx, key, revision, f)
}

func TestRuntimeContinuationFinalForgetAndRevocationSendNothing(t *testing.T) {
	for _, mode := range []string{"forget", "revocation"} {
		t.Run(mode, func(t *testing.T) {
			f := newRuntimeFixture(t)
			first, err := f.submit(t, "Research https://example.invalid/private-topic", "first")
			if err != nil {
				t.Fatal(err)
			}
			receivePrompt(t, f.provider)
			f.provider.session().reply <- PromptResult{FinalText: "Ready"}
			awaitTurn(t, f, first.ResourceID, pa.TurnSucceeded)
			f.provider.mu.Lock()
			f.provider.refuseResume = true
			f.provider.mu.Unlock()
			f.r.o.Memory = continuationBoundaryMemory{runtimeMemory: f.memory, before: func() {
				if mode == "revocation" {
					f.revoked.Store(true)
					return
				}
				f.memory.mu.Lock()
				defer f.memory.mu.Unlock()
				f.memory.current.Revision++
				f.memory.current.ForgottenThrough = f.memory.current.Revision
				f.memory.snapshot.Memory = clone(f.memory.current)
			}}
			second, err := f.submit(t, "Please research", "followup")
			if err != nil {
				t.Fatal(err)
			}
			awaitTurn(t, f, second.ResourceID, pa.TurnUnknown)
			select {
			case q := <-f.provider.sent:
				t.Fatal("final fence sent private history", q)
			default:
			}
			turn, _ := f.store.Turn(context.Background(), f.key, second.ResourceID)
			want := pa.ContextStale
			if mode == "revocation" {
				want = pa.Revoked
			}
			if turn.Failure == nil || turn.Failure.Code != want {
				t.Fatalf("typed refusal=%+v want=%s", turn.Failure, want)
			}
		})
	}
}
