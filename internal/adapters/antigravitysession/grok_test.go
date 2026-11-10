//go:build darwin || linux

package antigravitysession

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravityprocess"
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

func TestGrokCancellationPersistsPendingToolsWithoutInventingSuccess(t *testing.T) {
	t.Setenv("CLW_WATCHDOG_TOOL_PEER", "yes")
	h, row, opts := grokFixture(t)
	if err := h.Send(context.Background(), row, "run the synthetic foreground command"); err != nil {
		t.Fatal(err)
	}
	c, _ := h.lookup(row)
	waitFor(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return len(c.activeTools) == 2 })
	emitUpdate(t, c, row, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "tool-one", "rawOutput": "partial output"})
	emitUpdate(t, c, row, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "already-done", "title": "Finished read", "status": "completed", "rawOutput": "kept result"})
	if err := h.Interrupt(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, row)
	check := func() {
		t.Helper()
		page, err := h.Transcript(row, 100)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, e := range page.Entries {
			if e.Activity == nil {
				continue
			}
			id := e.Activity.ToolCallID
			want := "cancelled"
			if id == "already-done" {
				want = "completed"
			}
			if e.Activity.Status != want {
				t.Fatalf("tool %s status = %q; want %q", id, e.Activity.Status, want)
			}
			if id == "tool-one" && !strings.Contains(e.Activity.Result, "partial output") {
				t.Fatal("cancellation discarded partial output")
			}
			seen[id] = true
		}
		for _, id := range []string{"tool-one", "tool-two", "already-done"} {
			if !seen[id] {
				t.Fatalf("tool %s missing", id)
			}
		}
	}
	check()
	stopHost(t, h)
	h = reopenHost(t, opts)
	check()
}

func TestGrokNeverAdoptsGeminiFullApprovalProfile(t *testing.T) {
	h, row, opts := grokFixture(t)
	dir := filepath.Join(opts.AccountDir, "antigravity-acp")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"toolPermission":"always-proceed"}`), 0600); err != nil {
		t.Fatal(err)
	}
	c, _ := h.lookup(row)
	if c.client.AlwaysProceedConfigured() {
		t.Fatal("Grok inherited Gemini's full approval profile")
	}
}

func TestGrokRateLimitHasAProviderSpecificFailureWithoutRetry(t *testing.T) {
	h, row, opts := grokFixture(t)
	if err := os.WriteFile(filepath.Join(opts.AccountDir, "prompt-rate-limited"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.Send(t.Context(), row, "synthetic coding request"); err != nil {
		t.Fatal(err)
	}
	var text string
	waitFor(t, func() bool {
		page, err := h.Transcript(row, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Entries {
			if e.Role == "error" {
				text = e.Text
			}
		}
		return text != ""
	})
	if text != "grok_turn_rate_limited" {
		t.Fatalf("rate limit became %q", text)
	}
	if got := readFile(t, filepath.Join(opts.AccountDir, "prompts")); got != "prompt\n" {
		t.Fatalf("prompt was retried: %q", got)
	}
	c, err := h.lookup(row)
	if err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.uncertain || c.row.State != session.StateUnknown || c.endedFinished.Load() {
		t.Fatalf("rate limit invented completion: state=%s uncertain=%v", c.row.State, c.uncertain)
	}
}

func TestGrokRateLimitClassificationKeepsProviderAndDeliveryBoundaries(t *testing.T) {
	for _, tc := range []struct {
		assistant session.Assistant
		code      int64
		delivery  antigravityprocess.Delivery
		want      string
	}{
		{session.AssistantGrok, -32003, antigravityprocess.Acknowledged, "grok_turn_rate_limited"},
		{session.AssistantGrok, -32002, antigravityprocess.Acknowledged, "grok_process_rpc_error"},
		{session.AssistantGrok, -32003, antigravityprocess.Uncertain, "grok_process_rpc_error"},
		{session.AssistantAntigravity, -32003, antigravityprocess.Acknowledged, "antigravity_process_rpc_error"},
	} {
		h := &Host{opts: Options{Assistant: tc.assistant}}
		got := h.turnFailureText(&antigravityprocess.Error{Code: "rpc_error", RPCCode: tc.code, Delivery: tc.delivery})
		if got != tc.want {
			t.Fatalf("%s/%d/%s: %q", tc.assistant, tc.code, tc.delivery, got)
		}
	}
}

func TestGrokProcessFailuresKeepTheirCodeAndUncertainDelivery(t *testing.T) {
	for _, code := range []string{"unexpected_id", "eof", "write_failed"} {
		e := &antigravityprocess.Error{Code: code, Delivery: antigravityprocess.Uncertain}
		h := &Host{opts: Options{Assistant: session.AssistantGrok}}
		if got := h.turnFailureText(e); got != "grok_process_"+code {
			t.Fatalf("provider crossed: %q", got)
		}
		if e.Delivery != antigravityprocess.Uncertain {
			t.Fatal("display mapping changed delivery")
		}
	}
}

func TestGrokMismatchedCompletionIsNotSuccessOrAnotherPrompt(t *testing.T) {
	h, row, opts := grokFixture(t)
	if err := os.WriteFile(filepath.Join(opts.AccountDir, "prompt-wrong-id"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	var completed atomic.Int32
	h.opts.TurnComplete = func(context.Context, session.Session) { completed.Add(1) }
	if err := h.Send(t.Context(), row, "synthetic coding request"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		page, err := h.Transcript(row, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Entries {
			if e.Role == "error" {
				if e.Text != "grok_process_unexpected_id" {
					t.Fatal(e.Text)
				}
				return true
			}
		}
		return false
	})
	c, _ := h.lookup(row)
	c.mu.Lock()
	uncertain, state, life := c.uncertain, c.row.State, c.client.Lifecycle()
	c.mu.Unlock()
	if !uncertain || state != session.StateUnknown || life.Phase != "closed" || life.ResponseIDKind != "number" || life.ResponseIDRelation != "previous" || life.CompletedAt != 0 || completed.Load() != 0 {
		t.Fatal(uncertain, state, life, completed.Load())
	}
	if got := readFile(t, filepath.Join(opts.AccountDir, "prompts")); got != "prompt\n" {
		t.Fatal("replayed", got)
	}
}

// A Grok Read of a photo carries the base64 image twice in one frame. A
// 394 KB JPEG ended a real turn with grok_process_frame_too_large at 1 MiB.
func TestGrokPhotoReadDoesNotEndTheTurn(t *testing.T) {
	h, row, opts := grokFixture(t)
	const photo = 2 << 20
	if err := os.WriteFile(filepath.Join(opts.AccountDir, "prompt-image-read"), []byte(strconv.Itoa(photo)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opts.AccountDir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	var completed atomic.Int32
	h.opts.TurnComplete = func(context.Context, session.Session) { completed.Add(1) }
	if err := h.Send(t.Context(), row, "look at the synthetic photo"); err != nil {
		t.Fatal(err)
	}
	read := false
	waitFor(t, func() bool {
		// The console's lazy page keeps the full body behind a reference.
		page, err := h.TranscriptPage(t.Context(), row, 100, 0, "", true)
		if err != nil {
			t.Fatal(err)
		}
		read = false
		for _, e := range page.Entries {
			if e.Role == "error" {
				t.Fatalf("photo read ended the turn: %s", e.Text)
			}
			read = read || e.Role == "tool" && e.Activity != nil && e.Activity.ResultKnown && e.Detail != nil && e.Detail.TextBytes > photo
		}
		return completed.Load() == 1
	})
	if !read {
		t.Fatal("photo read result was not retained")
	}
}
