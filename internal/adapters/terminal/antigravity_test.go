package terminal

import (
	"context"
	"errors"
	"testing"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestAntigravityPaneRequiresFullProof(t *testing.T) {
	tmux := NewTmux()
	ctx := context.Background()
	if _, ok := tmux.antigravityPane(ctx, "%1", "ttyA"); ok {
		t.Fatal("pane command alone admitted IDE")
	}
	proof := ports.AntigravityProcess{PID: 41, Birth: "birth-a", TTY: "ttyA", TerminalID: "%1", Executable: "/synthetic/standalone/agy"}
	tmux.Antigravity = func(context.Context, string, string) (ports.AntigravityProcess, error) { return proof, nil }
	if _, ok := tmux.antigravityPane(ctx, "%1", "ttyA"); !ok {
		t.Fatal("positive pane proof refused")
	}
	if _, ok := tmux.antigravityPane(ctx, "%2", "ttyA"); ok {
		t.Fatal("cross-pane proof admitted")
	}
	if _, ok := tmux.antigravityPane(ctx, "%1", "ttyB"); ok {
		t.Fatal("cross-TTY proof admitted")
	}
	tmux.Antigravity = func(context.Context, string, string) (ports.AntigravityProcess, error) {
		return proof, errors.New("unreadable")
	}
	if _, ok := tmux.antigravityPane(ctx, "%1", "ttyA"); ok {
		t.Fatal("failed reader admitted pane")
	}
	if _, ok, complete := tmux.antigravityPaneStatus(ctx, "%1", "ttyA"); ok || complete {
		t.Fatal("unreadable candidate became authoritative absence")
	}
}

func TestAntigravityNeverSynthesizesQuitOrTrustsComm(t *testing.T) {
	if word, ok := QuitLine(session.AssistantAntigravity); ok || word != "" {
		t.Fatalf("unmeasured quit typed %q", word)
	}
	if assistantOfComm("agy") != "" {
		t.Fatal("old IDE classified by basename")
	}
	if word, ok := QuitLine(session.AssistantClaude); !ok || word != "/exit" {
		t.Fatal("Claude quit regressed")
	}
	if word, ok := QuitLine(session.AssistantCodex); !ok || word != "/quit" {
		t.Fatal("Codex quit regressed")
	}
}

func TestAntigravityRawSendRefusesBeforeLookingForATerminal(t *testing.T) {
	tmux := &Tmux{Binary: "/nonexistent/synthetic-tmux"}
	err := tmux.Send(context.Background(), session.Session{ID: "%synthetic", Assistant: session.AssistantAntigravity}, "hello")
	var unsent Unsent
	if !errors.As(err, &unsent) || unsent.Why != ErrAntigravityInputUnavailable.Error() {
		t.Fatalf("raw send did not require verified action composition: %v", err)
	}
}
