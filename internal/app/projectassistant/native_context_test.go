package projectassistant

import (
	"context"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestRuntimeNativeContextRejectsRevokedOrUnknownOwner(t *testing.T) {
	for _, name := range []string{"revoked-binding", "unknown-turn", "changed-instructions"} {
		t.Run(name, func(t *testing.T) {
			f := newRuntimeFixture(t)
			if _, err := f.submit(t, "live", "live"); err != nil {
				t.Fatal(err)
			}
			q := receivePrompt(t, f.provider)
			f.store.mu.Lock()
			switch name {
			case "revoked-binding":
				binding := f.store.bindings[q.Turn.Fence.BindingID]
				binding.Revoked = true
				f.store.bindings[q.Turn.Fence.BindingID] = binding
			case "unknown-turn":
				turn := f.store.turns[q.Turn.ID]
				turn.State = pa.TurnUnknown
				f.store.turns[q.Turn.ID] = turn
			case "changed-instructions":
				f.store.bot.Instructions.Revision++
			}
			f.store.mu.Unlock()
			state, err := f.r.NativeContext(context.Background(), q.Turn.Fence, nil)
			if state.NativeSessionID != "" || state.CommandsKnown || state.UsageKnown {
				t.Fatalf("unqualified reading: %+v", state)
			}
			if name == "revoked-binding" && pa.Code(err) != pa.Revoked {
				t.Fatalf("revocation: %v", err)
			}
			if name == "changed-instructions" && pa.Code(err) != pa.ContextStale {
				t.Fatalf("instructions: %v", err)
			}
			if name == "unknown-turn" && err != nil {
				t.Fatalf("unknown: %v", err)
			}
			if f.provider.count() != 1 {
				t.Fatal("a read opened a provider")
			}
		})
	}
}
