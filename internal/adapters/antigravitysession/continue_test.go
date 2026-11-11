package antigravitysession

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
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

// A turn sets its state to idle when the reply ends and clears its job flag a
// moment later. Shutdown in that moment must not mark the conversation, or the
// next start sends a needless continuation; a turn still working is marked.
func TestShutdownMarksOnlyATurnStillWorking(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state session.State
		want  bool
	}{
		{"reply ended, job unwinding", session.StateIdle, false},
		{"turn still working", session.StateWorking, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, row, opts := persistentFixture(t)
			c, err := h.lookup(row)
			if err != nil {
				t.Fatal(err)
			}
			c.mu.Lock()
			c.busy = true
			c.row.State = tc.state
			c.mu.Unlock()
			stopHost(t, h)
			h = reopenHost(t, opts)
			c, err = h.lookup(row)
			if err != nil {
				t.Fatal(err)
			}
			c.mu.Lock()
			got := c.interrupted
			c.mu.Unlock()
			if got != tc.want {
				t.Fatalf("interrupted = %v, want %v", got, tc.want)
			}
		})
	}
}

// The daemon cancels the context it gave the host before it calls Shutdown
// (2026-11-11: a dev install's stopCloud ran first). The worker then dies under
// a running turn, and that turn must still be woken after the next start
// instead of ending as a failed turn the person has to notice.
func TestATurnWhoseHostContextIsCancelledFirstIsStillWoken(t *testing.T) {
	h, row, opts := persistentFixture(t)
	stopHost(t, h)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, err := New(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.Resume(context.Background(), row.CWD, row.ConversationID); err != nil {
		t.Fatal(err)
	}
	hang := filepath.Join(opts.AccountDir, "prompt-uncertain")
	if err := os.WriteFile(hang, nil, 0600); err != nil {
		t.Fatal(err)
	}
	_ = h.Send(context.Background(), row, "interrupted turn")
	prompts := filepath.Join(opts.AccountDir, "prompts")
	waitFor(t, func() bool { return readFile(t, prompts) == "prompt\n" })
	cancel()
	// The turn's own failure path runs before Shutdown can mark it.
	waitFor(t, func() bool {
		c, _ := h.lookup(row)
		c.mu.Lock()
		defer c.mu.Unlock()
		return !c.busy
	})
	_ = h.Shutdown()
	if err := os.Remove(hang); err != nil {
		t.Fatal(err)
	}

	h = reopenHost(t, opts)
	page, err := h.Transcript(row, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Entries {
		if e.Role == "error" {
			t.Fatalf("a restart was shown as a failed turn: %q", e.Text)
		}
	}
	// The synthetic worker answers a prompt once released.
	if err := os.WriteFile(filepath.Join(opts.AccountDir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if n := h.ContinueInterrupted(context.Background()); n != 1 {
		t.Fatalf("woke %d conversations, want 1", n)
	}
	// The count is taken before sending; prove the continuation reached the
	// worker, as the conversation's last user message, exactly once.
	waitFor(t, func() bool { return readFile(t, prompts) == "prompt\nprompt\n" })
	idleHost(t, h, row)
	page, err = h.Transcript(row, 100)
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
	stopHost(t, h)
	h = reopenHost(t, opts)
	if n := h.ContinueInterrupted(context.Background()); n != 0 {
		t.Fatalf("the continuation was sent again (%d)", n)
	}
	if got := readFile(t, prompts); got != "prompt\nprompt\n" {
		t.Fatalf("prompts after a second restart = %q", got)
	}
}
