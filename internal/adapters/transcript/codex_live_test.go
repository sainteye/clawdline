package transcript

import (
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestAManagedCodexTitleMustBeAnExactUniqueMatch(t *testing.T) {
	one := codexLiveIdentity{ID: "c0de0001-0000-4000-8000-000000000001", Name: "Inspect the queue", CWD: "/code/demo"}
	h := &Host{codexLive: map[string]codexLiveIdentity{one.ID: one}}
	base := session.Session{Backend: session.BackendITerm, Assistant: session.AssistantCodex,
		CWD: "/code/demo", Label: "Inspect the queue | demo (codex)"}

	if got, ok := h.codexLiveFor(base); !ok || got.ID != one.ID {
		t.Fatalf("exact match = %+v, %v", got, ok)
	}
	working := base
	working.Label = "⠸ " + working.Label
	if got, ok := h.codexLiveFor(working); !ok || got.ID != one.ID {
		t.Fatalf("working title = %+v, %v", got, ok)
	}
	wrongTitle := base
	wrongTitle.Label = "Another title | demo (codex)"
	if _, ok := h.codexLiveFor(wrongTitle); ok {
		t.Fatal("a different terminal title bound the conversation")
	}
	wrongDirectory := base
	wrongDirectory.CWD = "/code/other"
	if _, ok := h.codexLiveFor(wrongDirectory); ok {
		t.Fatal("a different process cwd bound the conversation")
	}
	tmux := base
	tmux.Backend = session.BackendTmux
	if _, ok := h.codexLiveFor(tmux); ok {
		t.Fatal("the iTerm-only title contract bound a tmux row")
	}

	two := codexLiveIdentity{ID: "c0de0002-0000-4000-8000-000000000002", Name: one.Name, CWD: one.CWD}
	h.codexLive[two.ID] = two
	if _, ok := h.codexLiveFor(base); ok {
		t.Fatal("two exact live matches were ranked instead of left unbound")
	}
}
