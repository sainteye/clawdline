package antigravitysession

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A restart during a turn closed the Gemini process and left the conversation
// asleep until someone wrote to it (2026-11-07). The next start now wakes it
// with one new message, never a replay, and only once.
func TestARestartDuringATurnWakesTheConversationOnceAfterTheNextStart(t *testing.T) {
	h, row, opts := persistentFixture(t)
	release := filepath.Join(opts.AccountDir, "release")
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.Send(context.Background(), row, "finished turn"); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, row)
	stopHost(t, h)

	// A conversation stopped while idle is left alone.
	h = reopenHost(t, opts)
	if n := h.ContinueInterrupted(context.Background()); n != 0 {
		t.Fatalf("an idle conversation was woken %d times", n)
	}

	// A conversation stopped mid-turn is woken.
	hang := filepath.Join(opts.AccountDir, "prompt-uncertain")
	if err := os.WriteFile(hang, nil, 0600); err != nil {
		t.Fatal(err)
	}
	_ = h.Send(context.Background(), row, "interrupted turn")
	prompts := filepath.Join(opts.AccountDir, "prompts")
	waitFor(t, func() bool { return readFile(t, prompts) == "prompt\nprompt\n" })
	stopHost(t, h)
	if err := os.Remove(hang); err != nil {
		t.Fatal(err)
	}

	h = reopenHost(t, opts)
	if n := h.ContinueInterrupted(context.Background()); n != 1 {
		t.Fatalf("woke %d conversations, want 1", n)
	}
	waitFor(t, func() bool { return readFile(t, prompts) == "prompt\nprompt\nprompt\n" })
	idleHost(t, h, row)
	page, err := h.Transcript(row, 100)
	if err != nil {
		t.Fatal(err)
	}
	last := ""
	for _, e := range page.Entries {
		if e.Role == "user" {
			last = e.Text
		}
	}
	if last != ContinueText {
		t.Fatalf("last user message = %q, want the continuation", last)
	}

	// The mark was spent: another restart does not send it again.
	stopHost(t, h)
	h = reopenHost(t, opts)
	if n := h.ContinueInterrupted(context.Background()); n != 0 {
		t.Fatalf("the continuation was sent again (%d)", n)
	}
	if got := readFile(t, prompts); got != "prompt\nprompt\nprompt\n" {
		t.Fatalf("prompts after a second restart = %q", got)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("condition did not hold within 5s")
		}
		time.Sleep(time.Millisecond)
	}
}
