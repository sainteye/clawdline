//go:build darwin || linux

package antigravitysession

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravityacp"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func contextLaunch(name string) LaunchContext {
	return LaunchContext{URI: "file:///synthetic/" + name + ".md", Text: name + " role and memory", Persona: name, SquadLaunchID: name + "-launch", SquadCapabilityPath: "/synthetic/" + name + "-capability"}
}

func openContext(t *testing.T, h *Host, opts Options, launch LaunchContext) session.Session {
	t.Helper()
	row, err := h.OpenWithContext(context.Background(), ports.OpenRequest{Cwd: opts.AccountDir}, launch)
	if err != nil {
		t.Fatal(err)
	}
	if row.Persona != launch.Persona || row.SquadLaunchID != launch.SquadLaunchID {
		t.Fatalf("identity: %+v", row)
	}
	return row
}

func TestLaunchContextSurvivesBlankAndAcceptedReopen(t *testing.T) {
	h, _, opts := persistentFixture(t)
	launch := contextLaunch("backend")
	row := openContext(t, h, opts, launch)
	stopHost(t, h)
	h = reopenHost(t, opts)
	if strings.Contains(readFile(t, filepath.Join(opts.AccountDir, "methods")), "session/prompt") {
		t.Fatal("blank launch consumed a prompt")
	}
	var err error
	row, err = h.Resume(context.Background(), row.CWD, row.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Persona != launch.Persona || row.SquadLaunchID != launch.SquadLaunchID {
		t.Fatal("lost original identity")
	}
	denied := errors.New("synthetic authority failure")
	if err = h.SendProtocol(context.Background(), row, "identical user", func() error { return denied }); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	c, _ := h.lookup(row)
	c.mu.Lock()
	injected := c.injected
	c.mu.Unlock()
	if injected != nil {
		t.Fatal("pre-write refusal consumed context")
	}
	if err = os.WriteFile(filepath.Join(opts.AccountDir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err = h.Send(context.Background(), row, "identical user"); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, row)
	assertPromptContext(t, opts, row, true, launch.Text)
	stopHost(t, h)
	h = reopenHost(t, opts)
	row, err = h.Resume(context.Background(), row.CWD, row.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(opts.AccountDir, "prompts")); got != "prompt\n" {
		t.Fatalf("reopen resent input: %q", got)
	}
	if err = h.Send(context.Background(), row, "second"); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, row)
	assertPromptContext(t, opts, row, false, "")
	forceLoad(t, h, row)
	// A failed staged replay must leave the old visible history intact.
	before, err := h.Transcript(row, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(opts.AccountDir, "load-error"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = h.Resume(context.Background(), row.CWD, row.ConversationID); err == nil {
		t.Fatal("failed replay published")
	}
	after, err := h.Transcript(row, 100)
	if err != nil {
		t.Fatal(err)
	}
	if before.Signature != after.Signature || len(before.Entries) != len(after.Entries) {
		t.Fatal("failed replay changed history")
	}
	if err = os.Remove(filepath.Join(opts.AccountDir, "load-error")); err != nil {
		t.Fatal(err)
	}
	row, err = h.Resume(context.Background(), row.CWD, row.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	page, err := h.Transcript(row, 100)
	if err != nil {
		t.Fatal(err)
	}
	users := 0
	for _, e := range page.Entries {
		if strings.Contains(e.Text, launch.Text) {
			t.Fatal("context leaked into full history")
		}
		if e.Role == "user" {
			users++
			if e.Text != "identical user" {
				t.Fatalf("original text lost: %q", e.Text)
			}
		}
	}
	if users != 3 {
		t.Fatalf("later identical turns were lost: %d", users)
	}
}

func assertPromptContext(t *testing.T, opts Options, row session.Session, embedded bool, text string) {
	t.Helper()
	var prompt []struct {
		Type, Text string
		Resource   struct{ URI, Text string }
	}
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(opts.AccountDir, row.ConversationID+"-prompt"))), &prompt); err != nil {
		t.Fatal(err)
	}
	if embedded {
		if len(prompt) != 2 || prompt[0].Type != "resource" || prompt[0].Resource.Text != text || prompt[1].Text != "identical user" {
			t.Fatalf("first prompt: %+v", prompt)
		}
	} else if len(prompt) != 1 || prompt[0].Type != "text" {
		t.Fatalf("next prompt: %+v", prompt)
	}
}

func TestLaunchContextsAndCapabilityPathsStayConversationOwned(t *testing.T) {
	t.Setenv("CLAWDLINE_SQUAD_CAPABILITY_FILE", "/parent/capability")
	h, plain, opts := persistentFixture(t)
	if err := os.WriteFile(filepath.Join(opts.AccountDir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"backend", "reviewer"} {
		launch := contextLaunch(name)
		row := openContext(t, h, opts, launch)
		if err := h.Send(context.Background(), row, "identical user"); err != nil {
			t.Fatal(err)
		}
		idleHost(t, h, row)
		assertPromptContext(t, opts, row, true, launch.Text)
		if got := readFile(t, filepath.Join(opts.AccountDir, row.ConversationID+"-capability")); got != launch.SquadCapabilityPath {
			t.Fatalf("capability: %q", got)
		}
		if err := h.Close(context.Background(), row); err != nil {
			t.Fatal(err)
		}
		if _, err := h.Resume(context.Background(), row.CWD, row.ConversationID); err != nil {
			t.Fatal(err)
		}
		if err := h.Send(context.Background(), row, "next"); err != nil {
			t.Fatal(err)
		}
		idleHost(t, h, row)
		if got := readFile(t, filepath.Join(opts.AccountDir, row.ConversationID+"-capability")); got != launch.SquadCapabilityPath {
			t.Fatalf("restored capability: %q", got)
		}
		if err := h.Close(context.Background(), row); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.Send(context.Background(), plain, "plain"); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, plain)
	if got := readFile(t, filepath.Join(opts.AccountDir, plain.ConversationID+"-capability")); got != "" {
		t.Fatalf("plain inherited parent actor: %q", got)
	}
}

func TestReplayContextOnlyMatchesFirstExactUserChunk(t *testing.T) {
	launch := contextLaunch("backend")
	flat := "Embedded resource (" + launch.URI + "):\n" + launch.Text + " original"
	for _, first := range []string{flat, flat + " changed", "other first turn"} {
		c := &conversation{launchContext: launch, injected: &injectedTurn{OriginalText: "original"}, replaying: true}
		u := antigravityacp.SessionUpdate{Kind: "user_message_chunk", Text: first}
		c.cleanReplayContext(&u)
		want := first
		if first == flat {
			want = "original"
		}
		if u.Text != want {
			t.Fatalf("changed unrelated text: %q", u.Text)
		}
		u.Text = flat
		c.cleanReplayContext(&u)
		if u.Text != flat {
			t.Fatal("changed later identical expansion")
		}
		tool := antigravityacp.SessionUpdate{Kind: "tool_call", Text: flat}
		c.cleanReplayContext(&tool)
		if tool.Text != flat {
			t.Fatal("changed tool result")
		}
	}
}

func TestUncertainContextPromptIsNotInjectedAgainOnResume(t *testing.T) {
	h, _, opts := persistentFixture(t)
	launch := contextLaunch("backend")
	row := openContext(t, h, opts, launch)
	if err := os.WriteFile(filepath.Join(opts.AccountDir, "prompt-uncertain"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.Send(context.Background(), row, "identical user"); err != nil {
		t.Fatal(err)
	}
	// Pipe acceptance precedes peer observation. Wait for the synthetic peer's
	// durable receipt before stopping it to test an actually observed prompt.
	deadline := time.Now().Add(3 * time.Second)
	for readFile(t, filepath.Join(opts.AccountDir, "prompts")) != "prompt\n" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if readFile(t, filepath.Join(opts.AccountDir, "prompts")) != "prompt\n" {
		t.Fatal("synthetic peer did not observe the accepted prompt")
	}
	stopHost(t, h)
	if err := os.Remove(filepath.Join(opts.AccountDir, "prompt-uncertain")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opts.AccountDir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	h = reopenHost(t, opts)
	var err error
	row, err = h.Resume(context.Background(), row.CWD, row.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(opts.AccountDir, "prompts")); got != "prompt\n" {
		t.Fatalf("uncertain prompt resent: %q", got)
	}
	if err = h.Send(context.Background(), row, "new turn"); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, row)
	assertPromptContext(t, opts, row, false, "")
	c, _ := h.lookup(row)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.injected == nil || c.injected.OriginalText != "identical user" || c.launchContext != launch {
		t.Fatal("uncertain launch identity changed")
	}
}
