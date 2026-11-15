package projectassistant

import (
	"context"
	"fmt"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type resilienceProvider struct {
	*runtimeProvider
	capabilitiesCalls int
	revoke            func()
	atOpen            bool
}

type resilienceMemory struct {
	Memory
	revoke func()
}

func (m resilienceMemory) WithCurrent(ctx context.Context, key pa.BotKey, revision int64, callback func(pa.ContextSnapshot) error) error {
	m.revoke()
	return m.Memory.WithCurrent(ctx, key, revision, callback)
}

func (p *resilienceProvider) Capabilities(ctx context.Context, b pa.Bot) (pa.Capabilities, error) {
	p.capabilitiesCalls++
	if !p.atOpen && p.capabilitiesCalls == 2 {
		p.revoke()
	}
	return p.runtimeProvider.Capabilities(ctx, b)
}

func (p *resilienceProvider) Open(ctx context.Context, q OpenRequest, sink func(context.Context, ProtocolEvent) error) (ProviderSession, error) {
	s, err := p.runtimeProvider.Open(ctx, q, sink)
	if p.atOpen {
		p.revoke()
	}
	return s, err
}

func TestRuntimeRevalidatesAfterExecutionPreparationAndOpen(t *testing.T) {
	for _, name := range []string{"preparation", "open", "memory"} {
		t.Run(name, func(t *testing.T) {
			f := newRuntimeFixture(t)
			revoke := func() { f.revoked.Store(true) }
			if name == "memory" {
				f.r.o.Memory = resilienceMemory{Memory: f.memory, revoke: revoke}
			} else {
				f.r.o.Provider = &resilienceProvider{runtimeProvider: f.provider, revoke: revoke, atOpen: name == "open"}
			}
			receipt, err := f.submit(t, "synthetic input", "revoked")
			if err != nil {
				t.Fatal(err)
			}
			state := pa.TurnFailed
			if name != "preparation" {
				state = pa.TurnUnknown
			}
			eventually(t, func() bool { return f.r.Usage().ActiveActors == 0 })
			turn, err := f.store.Turn(context.Background(), f.key, receipt.ResourceID)
			if err != nil || turn.State != state {
				t.Fatalf("terminal turn: %+v %v, want %s", turn, err, state)
			}
			wantOpens := 0
			if name != "preparation" {
				wantOpens = 1
			}
			if f.provider.count() != wantOpens || len(f.provider.sent) != 0 {
				t.Fatalf("revoked execution: opens=%d sends=%d", f.provider.count(), len(f.provider.sent))
			}
			f.store.mu.Lock()
			defer f.store.mu.Unlock()
			for _, binding := range f.store.bindings {
				if name != "memory" && binding.NativeSessionID != "" {
					t.Fatal("revoked late Open persisted a native binding")
				}
			}
		})
	}
}

func TestRuntimeCapacityFailurePreservesItsBoundaryWithoutReplay(t *testing.T) {
	for _, field := range []string{"incoming_queue", "frame_bytes", "partial_capture", "reply_capture"} {
		t.Run(field, func(t *testing.T) {
			f := newRuntimeFixture(t)
			f.provider.sendErr = fmt.Errorf("native execution: %w", runtimeFailure(pa.CapacityFull, field))
			receipt, err := f.submit(t, "synthetic input", "capacity")
			if err != nil {
				t.Fatal(err)
			}
			receivePrompt(t, f.provider)
			awaitTurn(t, f, receipt.ResourceID, pa.TurnUnknown)
			turn, err := f.store.Turn(context.Background(), f.key, receipt.ResourceID)
			if err != nil || turn.Failure == nil || turn.Failure.Code != pa.CapacityFull || turn.Failure.Field != field {
				t.Fatalf("capacity boundary was discarded: %+v %v", turn.Failure, err)
			}
			replay, err := f.submit(t, "synthetic input", "capacity")
			if err != nil || replay.ID != receipt.ID {
				t.Fatalf("same command did not retain its receipt: %+v %v", replay, err)
			}
			if err = f.r.Recover(context.Background(), f.key); err != nil {
				t.Fatal(err)
			}
			if f.provider.count() != 1 || len(f.provider.sent) != 0 {
				t.Fatal("capacity failure replayed a prompt")
			}
		})
	}
}

func TestRuntimeCapacityFailureKeepsRestartRecoveryMarker(t *testing.T) {
	f := newRuntimeFixture(t)
	f.r.mu.Lock()
	f.r.restarting = true
	f.r.mu.Unlock()
	f.provider.sendErr = runtimeFailure(pa.CapacityFull, "incoming_queue")
	receipt, err := f.submit(t, "synthetic input", "capacity-restart")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	awaitTurn(t, f, receipt.ResourceID, pa.TurnUnknown)
	turn, err := f.store.Turn(context.Background(), f.key, receipt.ResourceID)
	if err != nil || turn.Failure == nil || turn.Failure.Field != RestartField {
		t.Fatalf("restart recovery marker lost: %+v %v", turn.Failure, err)
	}
}
