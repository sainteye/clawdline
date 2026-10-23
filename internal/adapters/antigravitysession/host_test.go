//go:build darwin || linux

package antigravitysession

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Re-exec uses real private process pipes and synthetic profile files only.
func TestOwnedPeer(t *testing.T) {
	if os.Getenv("CLW_SESSION_PEER") != "yes" {
		return
	}
	enc := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	notify := func(u any) {
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "synthetic-native", "update": u}})
	}
	for scanner.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				ModelID string `json:"modelId"`
				Prompt  []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			} `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			os.Exit(30)
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1}
		case "session/new":
			result = map[string]any{"sessionId": "synthetic-native", "_meta": map[string]any{"antigravity": map[string]any{"tier": map[string]string{"id": "free-tier", "name": "Antigravity Starter Quota"}}}}
			result.(map[string]any)["models"] = map[string]any{"currentModelId": "synthetic-high", "availableModels": []any{map[string]string{"modelId": "synthetic-high", "name": "Synthetic High"}, map[string]string{"modelId": "synthetic-low", "name": "Synthetic Low"}}}
		case "session/set_model":
			_ = os.WriteFile(filepath.Join(os.Getenv("GEMINI_HOME"), "model"), []byte(req.Params.ModelID), 0600)
		case "session/prompt":
			dir := os.Getenv("GEMINI_HOME")
			if len(req.Params.Prompt) == 1 && req.Params.Prompt[0].Text == "/usage" {
				f, _ := os.OpenFile(filepath.Join(dir, "usage"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
				f.WriteString("usage\n")
				f.Close()
				if _, err := os.Stat(filepath.Join(dir, "usage-error")); err == nil {
					_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32001, "message": "SECRET provider diagnostic /account/path"}})
					continue
				}
				notify(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "### Quota Limits\n- **Gemini Models**\n  - **Weekly Limit Remaining:** 62% remaining (resets Oct 29, 2026, 07:51 UTC)\n- **Claude and GPT models**\n  - **Weekly Limit Remaining:** 9% remaining"}})
				result = map[string]string{"stopReason": "end_turn"}
				break
			}
			f, err := os.OpenFile(filepath.Join(dir, "prompts"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			if err != nil {
				os.Exit(31)
			}
			f.WriteString("prompt\n")
			f.Close()
			for {
				if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
					break
				}
				time.Sleep(time.Millisecond)
			}
			notify(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "read-1", "title": "Read synthetic file", "kind": "read", "status": "in_progress"})
			notify(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "synthetic reply"}})
			result = map[string]string{"stopReason": "end_turn"}
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		if req.Method == "session/prompt" && !(len(req.Params.Prompt) == 1 && req.Params.Prompt[0].Text == "/usage") {
			notify(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "read-1", "status": "completed"})
		}
	}
	os.Exit(0)
}

func TestOwnedModelCommandSelectsWithoutPrompt(t *testing.T) {
	h, row, dir := fixture(t)
	checks := 0
	if err := h.SendProtocol(context.Background(), row, "/model synthetic-low", func() error { checks++; return nil }); err != nil {
		t.Fatal(err)
	}
	facts, _ := h.Facts(row)
	if facts.Model != "synthetic-low" {
		t.Fatalf("selected model=%q; command became a prompt", facts.Model)
	}
	if checks != 1 {
		t.Fatalf("checks=%d", checks)
	}
	if _, err := os.Stat(filepath.Join(dir, "prompts")); !os.IsNotExist(err) {
		t.Fatal("model selection called prompt")
	}
	if p, _ := h.Transcript(row, 100); len(p.Entries) != 0 {
		t.Fatal("model selection fabricated a conversation turn")
	}
	if err := h.Send(context.Background(), row, "/model unoffered"); err == nil {
		t.Fatal("unoffered model accepted")
	}
	if err := h.Send(context.Background(), row, "/model synthetic-low extra"); err == nil {
		t.Fatal("nonexact model accepted")
	}
	denied := errors.New("revoked")
	if err := h.SendProtocol(context.Background(), row, "/model synthetic-high", func() error { return denied }); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	selected, _ := os.ReadFile(filepath.Join(dir, "model"))
	if string(selected) != "synthetic-low" {
		t.Fatalf("rejected model reached peer: %q", selected)
	}
	facts, _ = h.Facts(row)
	if len(facts.Models) != 2 || facts.Models[1].Command != "synthetic-low" || facts.Model != "synthetic-low" {
		t.Fatalf("facts=%+v", facts)
	}
	facts.Models[0].ID = "tampered"
	held, _ := h.Facts(row)
	if held.Models[0].ID != "synthetic-high" {
		t.Fatal("model snapshot mutated offered list")
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.Send(context.Background(), row, "ordinary input"); err != nil {
		t.Fatal(err)
	}
	page := settled(t, h, row)
	if page.Entries[0].Text != "ordinary input" {
		t.Fatal(page)
	}
	prompts, _ := os.ReadFile(filepath.Join(dir, "prompts"))
	if string(prompts) != "prompt\n" {
		t.Fatalf("ordinary prompt count=%q", prompts)
	}
}

func TestOwnedQuotaNeverPollutesTranscriptOrBlocksSender(t *testing.T) {
	h, row, dir := fixture(t)
	before, _ := h.Transcript(row, 100)
	if err := h.RefreshQuota(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	facts, _ := h.Facts(row)
	if len(facts.Limits.Windows) != 1 || facts.Limits.Windows[0].UsedPercent != 38 {
		t.Fatalf("quota=%+v", facts.Limits)
	}
	after, _ := h.Transcript(row, 100)
	if len(after.Entries) != 0 || after.Signature != before.Signature {
		t.Fatal("quota mutated transcript", after)
	}
	if err := h.Send(context.Background(), row, "ordinary input"); err != nil {
		t.Fatal(err)
	}
	if err := h.RefreshQuota(context.Background(), row); err == nil {
		t.Fatal("busy usage queued")
	}
	if err := h.Send(context.Background(), row, "/model synthetic-low"); err == nil {
		t.Fatal("busy model selection queued")
	}
	usage, _ := os.ReadFile(filepath.Join(dir, "usage"))
	if string(usage) != "usage\n" {
		t.Fatalf("usage retried: %q", usage)
	}
}

func TestOwnedQuotaFailureStaysUnknownWithoutProviderDiagnostic(t *testing.T) {
	h, row, dir := fixture(t)
	if err := os.WriteFile(filepath.Join(dir, "usage-error"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.RefreshQuota(context.Background(), row); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error=%v", err)
	}
	facts, _ := h.Facts(row)
	if facts.Limits == nil || len(facts.Limits.Windows) != 0 || facts.Limits.UnknownReason != contract.AssistantUnknownReasonUnreadable || facts.Limits.AgeSeconds != nil {
		t.Fatalf("limits=%+v", facts.Limits)
	}
	if page, _ := h.Transcript(row, 100); len(page.Entries) != 0 {
		t.Fatal("quota error entered transcript", page)
	}
	usage, _ := os.ReadFile(filepath.Join(dir, "usage"))
	if string(usage) != "usage\n" {
		t.Fatal("quota error retried")
	}
}
func fixture(t *testing.T) (*Host, session.Session, string) {
	t.Helper()
	t.Setenv("CLW_SESSION_PEER", "yes")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "antigravity-acp"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "antigravity-acp", "settings.json"), []byte(`{"useG1Credits":false,"auth":{"type":"oauth-personal"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(context.Background(), Options{Executable: exe, Args: []string{"-test.run=^TestOwnedPeer$"}, AccountDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := h.Shutdown(); err != nil {
			t.Error(err)
		}
	})
	row, err := h.Open(context.Background(), ports.OpenRequest{Cwd: dir, Name: "Synthetic project"})
	if err != nil {
		t.Fatal(err)
	}
	return h, row, dir
}
func settled(t *testing.T, h *Host, row session.Session) contract.TranscriptPage {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p, err := h.Transcript(row, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Entries) == 3 && p.Entries[1].Activity != nil && p.Entries[1].Activity.Status == "completed" {
			return p
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("native-shaped turn never completed")
	return contract.TranscriptPage{}
}
func TestOwnedSessionAcceptsOnceAndKeepsReadingAfterHTTPReturns(t *testing.T) {
	h, row, dir := fixture(t)
	if row.PID <= 0 || row.TTY != "" || row.ConversationID != "synthetic-native" || row.Binding != session.BindingProtocol || session.SourceForID(row.ID) != "owned" {
		t.Fatalf("incorrect process identity: %+v", row)
	}
	ctx, cancel := context.WithCancel(context.Background())
	checks := 0
	if err := h.SendProtocol(ctx, row, "one synthetic input", func() error { checks++; return nil }); err != nil {
		t.Fatal(err)
	}
	cancel() // HTTP receipt has returned; this must not abort model completion.
	if checks != 1 {
		t.Fatalf("operation checks=%d", checks)
	}
	p, _ := h.Transcript(row, 100)
	if len(p.Entries) != 1 || p.Entries[0].Role != "user" {
		t.Fatalf("acceptance is not completion: %+v", p)
	}
	if err := h.Send(context.Background(), row, "must not queue"); err == nil {
		t.Fatal("busy turn queued a duplicate")
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	page := settled(t, h, row)
	if page.Entries[0].Text != "one synthetic input" || page.Entries[2].Role != "assistant" || page.Entries[2].Text != "synthetic reply" || page.Entries[1].Text != "Read synthetic file" {
		t.Fatalf("lost native order or partial fields: %+v", page.Entries)
	}
	if page.NextAfter != 0 || page.NextBefore != 0 || page.Evidence != contract.EvidenceStructured || page.Signature == p.Signature {
		t.Fatal("incorrect snapshot contract")
	}
	page.Entries[1].Activity.Status = "tampered"
	held, _ := h.Transcript(row, 100)
	if held.Entries[1].Activity.Status != "completed" {
		t.Fatal("reader mutated owned snapshot")
	}
	calls, err := os.ReadFile(filepath.Join(dir, "prompts"))
	if err != nil || string(calls) != "prompt\n" {
		t.Fatalf("prompt replay: %q %v", calls, err)
	}
}
func TestOwnedSessionChecksRevocationAndExactIdentityBeforeWriting(t *testing.T) {
	h, row, dir := fixture(t)
	denied := errors.New("operation revoked")
	if err := h.SendProtocol(context.Background(), row, "not allowed", func() error { return denied }); !errors.Is(err, denied) {
		t.Fatalf("revocation result=%v", err)
	}
	changed := row
	changed.ConversationID = "different"
	if err := h.Send(context.Background(), changed, "wrong conversation"); err == nil {
		t.Fatal("changed identity accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "prompts")); !os.IsNotExist(err) {
		t.Fatal("refused prompt reached peer")
	}
	if p, _ := h.Transcript(row, 100); len(p.Entries) != 0 {
		t.Fatal("local refusal appeared as sent user turn")
	}
	if _, err := h.Open(context.Background(), ports.OpenRequest{Cwd: dir}); err == nil {
		t.Fatal("preview process limit ignored")
	}
	if err := h.Close(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	if h.OwnsProtocol(row) {
		t.Fatal("closed conversation remains writable")
	}
	inv, _ := h.Inventory(context.Background())
	if !inv.Complete || len(inv.Sessions) != 0 {
		t.Fatal("closed session still listed")
	}
}
func TestOwnedSessionBoundsWholeEntriesAndPreservesToolIndex(t *testing.T) {
	c := &conversation{tools: map[string]int{"old": 0}, textIndex: -1, entries: []contract.TranscriptEntry{{Role: "tool", Text: "old"}}}
	c.append(contract.TranscriptEntry{Role: "assistant", Text: strings.Repeat("x", MaxViewBytes-150)})
	c.append(contract.TranscriptEntry{Role: "user", Text: strings.Repeat("y", 200)})
	if c.bytes > MaxViewBytes || len(c.entries) != 1 || c.entries[0].Role != "user" || len(c.tools) != 0 {
		t.Fatal("retained view exceeded budget or kept evicted tool index")
	}
}
