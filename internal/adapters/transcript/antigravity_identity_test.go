package transcript

import (
	"context"
	"errors"
	"testing"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

type antigravityIdentityFixture struct {
	value ports.AntigravityBinding
	err   error
}

func (f antigravityIdentityFixture) AntigravityBinding(context.Context, session.Session) (ports.AntigravityBinding, error) {
	return f.value, f.err
}
func TestAntigravityIdentityRequiresPositiveBinding(t *testing.T) {
	h := NewHost()
	s := session.Session{Assistant: session.AssistantAntigravity, ConversationID: "11111111-1111-4111-8111-111111111111", CWD: "/synthetic/shared"}
	if _, ok := h.ForSession(context.Background(), s); ok || h.RecordPath(s) != "" {
		t.Fatal("argv/cwd guessed an identity")
	}
	b := ports.AntigravityBinding{ConversationID: s.ConversationID, TranscriptPath: "/synthetic/exact/transcript_full.jsonl", Verified: true, Process: ports.AntigravityProcess{TerminalID: "%1"}}
	h.SetAntigravityBindings(antigravityIdentityFixture{value: b})
	got, ok := h.ForSession(context.Background(), s)
	if !ok || got.ConversationID != b.ConversationID || got.Pane != "%1" || string(got.Binding) != "launch_hook" || h.RecordPath(s) != b.TranscriptPath {
		t.Fatalf("lost exact source: %+v %v", got, ok)
	}
	h.SetAntigravityBindings(antigravityIdentityFixture{value: b, err: errors.New("unverified")})
	if _, ok = h.ForSession(context.Background(), s); ok || h.RecordPath(s) != "" {
		t.Fatal("failed source retained authority")
	}
	b.Verified = false
	h.SetAntigravityBindings(antigravityIdentityFixture{value: b})
	if _, ok = h.ForSession(context.Background(), s); ok {
		t.Fatal("recovered source became live")
	}
}
