package antigravitysession

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func userTurns(t *testing.T, h *Host, row session.Session) []string {
	t.Helper()
	page, err := h.Transcript(row, 100)
	if err != nil {
		t.Fatal(err)
	}
	var said []string
	for _, e := range page.Entries {
		if e.Role == "user" {
			said = append(said, e.Text)
		}
	}
	return said
}

// The phone held a Gemini message in the browser while a turn ran
// (2026-11-07); Claude Code and Codex take it at once. The machine now
// accepts it and sends each as its own prompt after the turn, in order.
func TestAMessageSentDuringAGeminiTurnIsAcceptedAndSentAfterIt(t *testing.T) {
	h, row, dir := fixture(t)
	if err := h.Send(context.Background(), row, "first"); err != nil {
		t.Fatal(err)
	}
	checks := 0
	for _, text := range []string{"second", "third"} {
		if err := h.SendProtocol(context.Background(), row, text, func() error { checks++; return nil }); err != nil {
			t.Fatalf("send during a turn: %v", err)
		}
	}
	if checks != 2 {
		t.Fatalf("operation checks=%d, want one per accepted message", checks)
	}
	prompts := filepath.Join(dir, "prompts")
	waitFor(t, func() bool { return readFile(t, prompts) != "" })
	if got := readFile(t, prompts); got != "prompt\n" {
		t.Fatalf("a queued message reached the model during the turn: %q", got)
	}
	if got := userTurns(t, h, row); strings.Join(got, ",") != "first" {
		t.Fatalf("user turns during the turn = %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return readFile(t, prompts) == "prompt\nprompt\nprompt\n" })
	idleHost(t, h, row)
	if got := userTurns(t, h, row); strings.Join(got, ",") != "first,second,third" {
		t.Fatalf("user turns = %q, want first,second,third", got)
	}
	c, _ := h.lookup(row)
	c.mu.Lock()
	left := len(c.queued)
	c.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d messages left queued", left)
	}
}

func TestARevokedOperationIsNotQueued(t *testing.T) {
	h, row, _ := fixture(t)
	if err := h.Send(context.Background(), row, "first"); err != nil {
		t.Fatal(err)
	}
	denied := errors.New("operation revoked")
	if err := h.SendProtocol(context.Background(), row, "not allowed", func() error { return denied }); !errors.Is(err, denied) {
		t.Fatalf("err=%v", err)
	}
	c, _ := h.lookup(row)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.queued) != 0 {
		t.Fatal("a refused message was queued")
	}
}

func TestAFullGeminiQueueRefusesBeforeAccepting(t *testing.T) {
	h, row, _ := fixture(t)
	if err := h.Send(context.Background(), row, "first"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxQueuedPrompts; i++ {
		if err := h.Send(context.Background(), row, "more"); err != nil {
			t.Fatal(err)
		}
	}
	var typed *Error
	if err := h.Send(context.Background(), row, "one too many"); !errors.As(err, &typed) || typed.Code != "queue_full" || !typed.BeforeWrite() {
		t.Fatalf("err=%v, want queue_full before write", err)
	}
	if err := h.Send(context.Background(), row, strings.Repeat("x", MaxQueuedPromptBytes+1)); !errors.As(err, &typed) || typed.Code != "text_too_large" {
		t.Fatalf("err=%v, want text_too_large", err)
	}
}

// A message accepted during a turn is saved with the conversation: a
// restart sends it after the continuation instead of losing it.
func TestAQueuedGeminiMessageSurvivesARestart(t *testing.T) {
	h, row, opts := persistentFixture(t)
	if err := h.Send(context.Background(), row, "long turn"); err != nil {
		t.Fatal(err)
	}
	if err := h.Send(context.Background(), row, "after the turn"); err != nil {
		t.Fatal(err)
	}
	prompts := filepath.Join(opts.AccountDir, "prompts")
	waitFor(t, func() bool { return readFile(t, prompts) != "" })
	t.Logf("prompts before restart %q", readFile(t, prompts))
	stopHost(t, h)
	if err := os.WriteFile(filepath.Join(opts.AccountDir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	h = reopenHost(t, opts)
	if n := h.ContinueInterrupted(context.Background()); n != 1 {
		t.Fatalf("woke %d conversations, want 1", n)
	}
	waitFor(t, func() bool { return readFile(t, prompts) == "prompt\nprompt\nprompt\n" })
	idleHost(t, h, row)
	got := userTurns(t, h, row)
	if len(got) < 2 || got[len(got)-2] != ContinueText || got[len(got)-1] != "after the turn" {
		t.Fatalf("user turns after restart = %q", got)
	}
}
