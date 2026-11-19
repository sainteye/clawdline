package projectassistant

import (
	"context"
	"sync/atomic"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type maintenanceHistoryCheck func(pa.ExecutionBinding) error

func (f maintenanceHistoryCheck) Ready(context.Context) error { return nil }
func (f maintenanceHistoryCheck) CheckNativeHistory(_ context.Context, b pa.ExecutionBinding) error {
	return f(b)
}

type parkedHistoryRepository struct {
	*runtimeStore
	parked pa.ExecutionBinding
}

func (s parkedHistoryRepository) Export(ctx context.Context, key pa.BotKey) (Export, error) {
	x, err := s.runtimeStore.Export(ctx, key)
	x.Bindings = append(x.Bindings, s.parked)
	return x, err
}

func (s parkedHistoryRepository) AgentRest(context.Context, string, pa.Agent) (*pa.AgentRest, error) {
	return nil, nil
}

func TestRuntimeChecksFinalParkedHistoryBeforeOpening(t *testing.T) {
	for _, tc := range []struct {
		name    string
		blocked bool
	}{{"blocked", true}, {"clean", false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRuntimeFixture(t)
			f.store.bot.FallbackAgents = pa.FallbackConfigOf([]pa.Agent{pa.ClaudeCode})
			c := &f.store.c
			c.MainBindingID, c.BindingID, c.Generation = "current-alternate", "current-alternate", 2
			fence := currentFence(f.store.bot, *c)
			previous := fence
			previous.Generation, previous.BindingID = 1, "parked-primary"
			parked := pa.ExecutionBinding{
				Fence: previous, Agent: pa.Codex, NativeSessionID: "parked-codex",
				MemoryRevision: 1, InstructionRevision: 1,
			}
			f.store.bindings[parked.Fence.BindingID] = parked
			f.store.bindings[fence.BindingID] = pa.ExecutionBinding{
				Fence: fence, Agent: pa.ClaudeCode, NativeSessionID: "other-native",
				MemoryRevision: 1, InstructionRevision: 1,
			}
			f.store.order = append(f.store.order, "parked-turn")
			f.store.turns["parked-turn"] = pa.Turn{
				ID: "parked-turn", Fence: previous, State: pa.TurnSucceeded,
				Attempts: []pa.AttemptFact{{Fence: previous, Agent: pa.Codex, Stage: pa.AttemptClosed, SendStarted: true, CleanupConfirmed: true, Effect: "unknown"}},
			}
			f.r.o.Repository = parkedHistoryRepository{runtimeStore: f.store, parked: parked}
			observed := make(chan pa.ExecutionBinding, 2)
			f.r.o.NativeHistory = maintenanceHistoryCheck(func(b pa.ExecutionBinding) error {
				observed <- b
				if tc.blocked && b.NativeSessionID == parked.NativeSessionID {
					return runtimeFailure(pa.ExecutionEffectUnknown, "native_maintenance_history")
				}
				return nil
			})
			receipt, err := f.submit(t, "resume the selected agent", "with-parked-history")
			if err != nil {
				t.Fatal(err)
			}
			if tc.blocked {
				awaitTurn(t, f, receipt.ResourceID, pa.TurnFailed)
				turn, _ := f.store.Turn(context.Background(), f.key, receipt.ResourceID)
				if f.provider.count() != 0 || turn.Failure == nil || pa.Code(turn.Failure) != pa.ExecutionEffectUnknown {
					t.Fatal("blocked fallback opened a provider or retried fresh", f.provider.count(), turn.Failure)
				}
			} else {
				receivePrompt(t, f.provider)
				session := f.provider.session()
				if !session.q.Resume || session.q.Binding.NativeSessionID != parked.NativeSessionID {
					t.Fatal("clean parked history did not resume", session.q)
				}
				session.reply <- PromptResult{FinalText: "completed"}
				awaitTurn(t, f, receipt.ResourceID, pa.TurnSucceeded)
			}
			select {
			case got := <-observed:
				if got != parked {
					t.Fatal("gate checked current alternate before selecting parked history", got)
				}
			default:
				t.Fatal("native history gate was bypassed")
			}
		})
	}
}

func TestRuntimeRechecksKnownHistoryBeforeProviderOpen(t *testing.T) {
	f := newRuntimeFixture(t)
	var checks atomic.Int64
	var armed atomic.Bool
	f.r.o.NativeHistory = maintenanceHistoryCheck(func(pa.ExecutionBinding) error {
		if armed.Load() && checks.Add(1) > 1 {
			return runtimeFailure(pa.ExecutionEffectUnknown, "native_maintenance_journal")
		}
		return nil
	})
	first, err := f.submit(t, "seed", "seed")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	f.provider.session().reply <- PromptResult{FinalText: "completed"}
	awaitTurn(t, f, first.ResourceID, pa.TurnSucceeded)
	armed.Store(true)

	second, err := f.submit(t, "new input", "new-input")
	if err != nil {
		t.Fatal(err)
	}
	awaitTurn(t, f, second.ResourceID, pa.TurnUnknown)
	if f.provider.count() != 1 || checks.Load() != 2 {
		t.Fatal("history that became unreadable crossed the native open boundary", f.provider.count(), checks.Load())
	}
}

func TestMaintenanceHistoryChecksFreshAndReplacementOpenBoundary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		resume  bool
		allowed int
	}{{"fresh", false, 1}, {"resume-unsupported-replacement", true, 2}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRuntimeFixture(t)
			var priorGeneration atomic.Int64
			f.r.o.NativeHistory = maintenanceHistoryCheck(func(binding pa.ExecutionBinding) error {
				previous := priorGeneration.Load()
				if previous > 0 && binding.Fence.Generation > previous && binding.NativeSessionID == "" && f.provider.count() == tc.allowed {
					return runtimeFailure(pa.ExecutionEffectUnknown, "native_maintenance_history")
				}
				return nil
			})
			first, err := f.submit(t, "seed", "seed")
			if err != nil {
				t.Fatal(err)
			}
			receivePrompt(t, f.provider)
			f.provider.session().reply <- PromptResult{FinalText: "seed complete"}
			awaitTurn(t, f, first.ResourceID, pa.TurnSucceeded)
			f.provider.mu.Lock()
			f.provider.refuseResume = tc.resume
			f.provider.mu.Unlock()
			f.store.mu.Lock()
			priorGeneration.Store(f.store.c.Generation)
			if !tc.resume {
				id := f.store.c.BindingID
				binding := f.store.bindings[id]
				binding.NativeSessionID = ""
				f.store.bindings[id] = binding
			}
			f.store.mu.Unlock()

			second, err := f.submit(t, "next", "next")
			if err != nil {
				t.Fatal(err)
			}
			var turn pa.Turn
			eventually(t, func() bool {
				turn, _ = f.store.Turn(context.Background(), f.key, second.ResourceID)
				return f.provider.count() > tc.allowed || turn.State == pa.TurnUnknown
			})
			if opens := f.provider.count(); opens != tc.allowed || turn.State != pa.TurnUnknown || turn.Failure == nil || pa.Code(turn.Failure) != pa.ExecutionEffectUnknown {
				t.Fatalf("same history escaped the final guard: opens=%d state=%s failure=%v", opens, turn.State, turn.Failure)
			}
		})
	}
}
