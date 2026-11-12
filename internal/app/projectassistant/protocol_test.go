package projectassistant

import (
	"strings"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestRuntimeOptionsHaveNoSilentDefaults(t *testing.T) {
	if _, err := NewRuntime(RuntimeOptions{}); pa.Code(err) != pa.Invalid {
		t.Fatal(err)
	}
	f := newRuntimeFixture(t)
	o := f.r.o
	o.Limits.MaxProcesses = 0
	if _, err := NewRuntime(o); pa.Code(err) != pa.Invalid {
		t.Fatal(err)
	}
}

func TestQuoteScopeProvenanceAndBoundsFailClosed(t *testing.T) {
	f := newRuntimeFixture(t)
	fence := pa.Fence{Key: f.key, Project: f.scope, ConversationID: "chat", BindingID: "old", Generation: 1}
	message := pa.Message{ID: "quote", Fence: fence, TurnID: "source", Text: "quoted text", Kind: pa.AssistantMessage}
	turn := pa.Turn{ID: "source", Fence: fence, MessageID: "original-user", State: pa.TurnSucceeded, MemoryRevision: 1}
	for _, test := range []struct {
		name   string
		change func(*Export, *pa.MemoryRevision)
		code   pa.ErrorCode
	}{
		{"missing", func(e *Export, _ *pa.MemoryRevision) { e.Messages = nil }, pa.NotFound},
		{"foreign_bot", func(e *Export, _ *pa.MemoryRevision) { e.Messages[0].Fence.Key.BotID = "other" }, pa.ScopeDenied},
		{"foreign_conversation", func(e *Export, _ *pa.MemoryRevision) { e.Messages[0].Fence.ConversationID = "other" }, pa.ScopeDenied},
		{"missing_source", func(e *Export, _ *pa.MemoryRevision) { e.Turns = nil }, pa.ContextStale},
		{"foreign_source", func(e *Export, _ *pa.MemoryRevision) { e.Turns[0].Fence.ConversationID = "other" }, pa.ScopeDenied},
		{"forgotten_id", func(_ *Export, m *pa.MemoryRevision) { m.ForgottenSourceIDs = []string{"quote"} }, pa.ContextStale},
		{"forgotten_source", func(_ *Export, m *pa.MemoryRevision) { m.ForgottenSourceIDs = []string{"original-user"} }, pa.ContextStale},
		{"old_revision", func(_ *Export, m *pa.MemoryRevision) { m.Revision = 2; m.ForgottenThrough = 2 }, pa.ContextStale},
		{"unknown_revision", func(e *Export, m *pa.MemoryRevision) { e.Turns[0].MemoryRevision = 0; m.ForgottenThrough = 1 }, pa.ContextStale},
		{"unknown_turn", func(e *Export, m *pa.MemoryRevision) { e.Turns[0].State = pa.TurnUnknown; m.ForgottenThrough = 1 }, pa.ContextStale},
		{"too_long", func(e *Export, _ *pa.MemoryRevision) { e.Messages[0].Text = strings.Repeat("x", pa.MaxMessageBytes+1) }, pa.CapacityFull},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := Export{Messages: []pa.Message{message}, Turns: []pa.Turn{turn}}
			memory := f.memory.current
			test.change(&e, &memory)
			if _, err := resolvePromptQuote(e, "quote", fence, memory); pa.Code(err) != test.code {
				t.Fatalf("wanted %s got %v", test.code, err)
			}
		})
	}
}
