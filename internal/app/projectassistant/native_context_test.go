package projectassistant

import (
	"context"
	"strings"
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
			read := func(ctx context.Context, key pa.BotKey, id string) (NativeContextFacts, error) {
				memory, err := f.memory.Current(ctx, key)
				if err != nil {
					return NativeContextFacts{}, err
				}
				f.store.mu.Lock()
				defer f.store.mu.Unlock()
				turn := f.store.turns[id]
				binding := f.store.bindings[f.store.c.BindingID]
				return NativeContextFacts{Bot: f.store.bot, Conversation: f.store.c, Turn: &turn, Binding: &binding, Memory: memory}, nil
			}
			ctx := context.Background()
			check := func() error { return f.r.authorized(ctx, q.Turn.Fence.Key, q.Turn.Fence.Project) }
			state, err := f.r.NativeContext(ctx, q.Turn.Fence, check, read)
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

func TestNewSessionClearsStoredContextUsage(t *testing.T) {
	f := newRuntimeFixture(t)
	f.store.mu.Lock()
	f.store.c.Generation = 2
	f.store.c.BindingID = "binding-2"
	f.store.c.LastMessageSeq = 4
	f.store.c.ContextUsage = pa.ContextSample{
		TurnID: "turn-2", BindingID: "binding-2", Generation: 2,
		MemoryRevision: 1, UsedTokens: 402600, WindowTokens: 1000000,
	}
	expectedVersion := f.store.bot.Version
	f.store.mu.Unlock()

	cmd := pa.Command{Key: f.key, Operation: pa.EditOperation, IdempotencyKey: "reset-usage", ContentHash: strings.Repeat("a", 64)}
	if _, err := f.r.NewSession(context.Background(), cmd, expectedVersion); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	if f.store.c.ContextUsage != (pa.ContextSample{}) {
		t.Fatalf("expected cleared ContextUsage, got %+v", f.store.c.ContextUsage)
	}
	if f.store.c.SessionResetGeneration != 2 || f.store.c.SessionResetAfter != 4 {
		t.Fatalf("unexpected reset boundary: gen=%d after=%d", f.store.c.SessionResetGeneration, f.store.c.SessionResetAfter)
	}
}
