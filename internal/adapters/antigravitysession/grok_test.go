//go:build darwin || linux

package antigravitysession

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func grokFixture(t *testing.T) (*Host, session.Session, Options) {
	t.Helper()
	t.Setenv("CLW_SESSION_PEER", "yes")
	t.Setenv("CLW_RESTORE_PEER", "yes")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	opts := Options{Assistant: session.AssistantGrok, Executable: exe, AccountDir: dir,
		StateDir: filepath.Join(dir, "history"), Args: []string{"-test.run=^TestOwnedPeer$"}}
	h := reopenHost(t, opts)
	row, err := h.Open(context.Background(), ports.OpenRequest{Cwd: dir, Name: "Synthetic coding project"})
	if err != nil {
		t.Fatal(err)
	}
	if row.Assistant != session.AssistantGrok {
		t.Fatalf("provider = %q", row.Assistant)
	}
	return h, row, opts
}

func TestGrokRestartDoesNotPromptOrDrainQueuedEdits(t *testing.T) {
	h, row, opts := grokFixture(t)
	if err := os.WriteFile(filepath.Join(opts.AccountDir, "prompt-uncertain"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.Send(context.Background(), row, "edit the synthetic program"); err != nil {
		t.Fatal(err)
	}
	prompts := filepath.Join(opts.AccountDir, "prompts")
	waitFor(t, func() bool { return readFile(t, prompts) == "prompt\n" })
	if err := h.Send(context.Background(), row, "queued synthetic edit"); err != nil {
		t.Fatal(err)
	}
	stopHost(t, h)
	h = reopenHost(t, opts)
	if n := h.ContinueInterrupted(context.Background()); n != 0 {
		t.Fatalf("restart launched %d automatic prompts", n)
	}
	if got := readFile(t, prompts); got != "prompt\n" {
		t.Fatalf("restart replayed work: %q", got)
	}
	c, err := h.lookup(row)
	if err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.uncertain || c.row.State != session.StateUnknown || len(c.queued) != 1 {
		t.Fatalf("restart lost uncertainty or pending input: state=%s uncertain=%v queue=%d", c.row.State, c.uncertain, len(c.queued))
	}
}

func TestGrokTextAndSilenceNeverReportSuccess(t *testing.T) {
	for _, marker := range []bool{false, true} {
		t.Run(map[bool]string{false: "text and usage", true: "Gemini marker"}[marker], func(t *testing.T) {
			t.Setenv("CLW_INTERRUPT_PEER", "yes")
			h, row, _ := grokFixture(t)
			h.opts.InactivityTimeout = 100 * time.Millisecond
			h.opts.TurnEndGrace = 10 * time.Millisecond
			var completed atomic.Int32
			h.opts.TurnComplete = func(context.Context, session.Session) { completed.Add(1) }
			if err := h.Send(context.Background(), row, "finish the synthetic edit"); err != nil {
				t.Fatal(err)
			}
			c, _ := h.lookup(row)
			waitFor(t, func() bool {
				c.mu.Lock()
				defer c.mu.Unlock()
				return c.row.State == session.StateWorking && c.writeGate == nil
			})
			text := "Synthetic reply."
			if marker {
				text += "\n" + TurnEndMarker
			}
			emitUpdate(t, c, row, say(text))
			emitUpdate(t, c, row, map[string]any{"sessionUpdate": "usage_update", "used": 1, "size": 100})
			waitFor(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return !c.busy })
			if completed.Load() != 0 {
				t.Fatal("text without native completion emitted success")
			}
			c.mu.Lock()
			state, uncertain := c.row.State, c.uncertain
			c.mu.Unlock()
			if state != session.StateUnknown || !uncertain {
				t.Fatalf("missing native completion became state=%s uncertain=%v", state, uncertain)
			}
			page, err := h.Transcript(row, 100)
			if err != nil {
				t.Fatal(err)
			}
			foundError, foundText := false, false
			for _, e := range page.Entries {
				foundError = foundError || e.Role == "error"
				foundText = foundText || e.Role == "assistant" && strings.Contains(e.Text, text)
			}
			if !foundError || !foundText {
				t.Fatalf("lost timeout or verbatim native text: %+v", page.Entries)
			}
		})
	}
}

func TestGrokHistoryCannotBeOpenedAsGemini(t *testing.T) {
	h, _, opts := grokFixture(t)
	stopHost(t, h)
	opts.Assistant = session.AssistantAntigravity
	other, err := New(context.Background(), opts)
	if err == nil {
		other.Shutdown()
		t.Fatal("Grok history was adopted by Gemini")
	}
}

func TestGrokNativeToolsReceiveGrokIdentity(t *testing.T) {
	h, row, _ := grokFixture(t)
	raw, err := os.ReadFile(h.identityPath(row.ID))
	if err != nil {
		t.Fatal(err)
	}
	var identity struct {
		Assistant    string `json:"assistant"`
		Conversation string `json:"conversation_id"`
	}
	if json.Unmarshal(raw, &identity) != nil || identity.Assistant != "grok" || identity.Conversation != row.ConversationID {
		t.Fatalf("native tools received a different provider or conversation: %s", raw)
	}
}

func TestGrokPermissionIsAnExplicitNativeLaunchCeiling(t *testing.T) {
	if !reflect.DeepEqual(grokArgs("ask"), []string{"--permission-mode", "default", "agent", "--no-leader", "stdio"}) ||
		!reflect.DeepEqual(grokArgs("full"), []string{"agent", "--no-leader", "--always-approve", "stdio"}) {
		t.Fatal("native permission argv changed")
	}
	h, row, _ := grokFixture(t)
	if err := h.ConfigureDispatch(t.Context(), row, "", "", "full"); err == nil {
		t.Fatal("ask session was widened after launch")
	}
	if err := h.ConfigureDispatch(t.Context(), row, "", "", "edits"); err == nil {
		t.Fatal("edits silently became Auto")
	}
	if err := h.ConfigureDispatch(t.Context(), row, "", "", "ask"); err != nil {
		t.Fatal(err)
	}
}

func TestGrokUnsupportedControlsNeverReachTheNativePrompt(t *testing.T) {
	h, row, opts := grokFixture(t)
	for _, text := range []string{"/", "/help", "/effort high", "/always-approve", "! rm synthetic"} {
		if err := h.Send(t.Context(), row, text); err == nil {
			t.Fatalf("control accepted: %q", text)
		}
	}
	if _, err := os.Stat(filepath.Join(opts.AccountDir, "prompts")); !os.IsNotExist(err) {
		t.Fatal("control wrote a native prompt")
	}
	for _, text := range []string{"Review the program", "/workspace/source.go", "/README.md", "/model offered-model"} {
		if grokControlDraft(text) {
			t.Fatalf("ordinary input blocked: %q", text)
		}
	}
}

func TestGrokUnknownQuotaNeverClaimsGeminiUsage(t *testing.T) {
	h, row, _ := grokFixture(t)
	facts, err := h.Facts(row)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Limits == nil || facts.Limits.UnknownReason == "" || len(facts.Limits.Windows) != 0 || facts.Limits.Detail != "Grok" {
		t.Fatalf("Grok quota crossed providers: %+v", facts.Limits)
	}
}

func TestGrokAccountQuotaIsExplicitlyUnsupported(t *testing.T) {
	h, _, _ := grokFixture(t)
	if reading, err := h.AccountQuota(context.Background()); err == nil || reading != nil || err.Error() != "antigravity_session_usage_unsupported" {
		t.Fatalf("unsupported quota claimed a reading: %+v %v", reading, err)
	}
}
