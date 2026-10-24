//go:build darwin || linux

package antigravitysession

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
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
	nativeID := "synthetic-native"
	dir := os.Getenv("GEMINI_HOME")
	if os.Getenv("CLW_IDENTITY_CHECK") == "yes" {
		path := os.Getenv("CLAWDLINE_PROTOCOL_SESSION_FILE")
		if raw, err := os.ReadFile(path); err == nil {
			var identity struct {
				ID string `json:"conversation_id"`
			}
			if json.Unmarshal(raw, &identity) != nil || identity.ID == "" {
				os.Exit(38)
			}
			if err := os.WriteFile(filepath.Join(dir, "identity-start-"+identity.ID), []byte(path), 0600); err != nil {
				os.Exit(39)
			}
		}
	}
	if os.Getenv("CLW_RESTORE_PEER") == "yes" {
		f, _ := os.OpenFile(filepath.Join(dir, "starts"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		f.WriteString("start\n")
		f.Close()
	}
	model, effort := "synthetic-flash", "high"
	mode := "default"
	efforts := []string{"low", "medium", "high"}
	scanner := bufio.NewScanner(os.Stdin)
	notify := func(u any) {
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": nativeID, "update": u}})
	}
	for scanner.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				SessionID string `json:"sessionId"`
				CWD       string `json:"cwd"`
				Cursor    string `json:"cursor"`
				ModelID   string `json:"modelId"`
				ConfigID  string `json:"configId"`
				Value     string `json:"value"`
				Prompt    []struct {
					Text     string `json:"text"`
					Type     string `json:"type"`
					Resource struct {
						URI  string `json:"uri"`
						Text string `json:"text"`
					} `json:"resource"`
				} `json:"prompt"`
			} `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			os.Exit(30)
		}
		var result any = map[string]any{}
		if os.Getenv("CLW_RESTORE_PEER") == "yes" {
			f, _ := os.OpenFile(filepath.Join(dir, "methods"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			f.WriteString(req.Method + "\n")
			f.Close()
		}
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1}
			if os.Getenv("CLW_RESTORE_PEER") == "yes" {
				result.(map[string]any)["agentCapabilities"] = map[string]any{"promptCapabilities": map[string]bool{"embeddedContext": true}, "loadSession": true, "sessionCapabilities": map[string]any{"resume": map[string]any{}, "list": map[string]any{}}}
			}
		case "session/list":
			result = map[string]any{"sessions": []any{}}
			name := "native-list.json"
			if req.Params.Cursor == "older" {
				name = "native-list-older.json"
			}
			if raw, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
				if json.Unmarshal(raw, &result) != nil {
					os.Exit(32)
				}
			}
		case "authenticate":
			if _, err := os.Stat(filepath.Join(dir, "auth-error")); err == nil {
				_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32001, "message": "synthetic auth error"}})
				continue
			}
		case "session/new":
			if os.Getenv("CLW_RESTORE_PEER") == "yes" {
				raw, _ := os.ReadFile(filepath.Join(dir, "session-number"))
				n, _ := strconv.Atoi(string(raw))
				n++
				os.WriteFile(filepath.Join(dir, "session-number"), []byte(strconv.Itoa(n)), 0600)
				nativeID = "synthetic-native-" + strconv.Itoa(n)
			}
			result = map[string]any{"sessionId": nativeID, "_meta": map[string]any{"antigravity": map[string]any{"tier": map[string]string{"id": "free-tier", "name": "Antigravity Starter Quota"}}}}
			result.(map[string]any)["models"] = map[string]any{"currentModelId": "synthetic-high", "availableModels": []any{map[string]string{"modelId": "synthetic-high", "name": "Synthetic High"}, map[string]string{"modelId": "synthetic-low", "name": "Synthetic Low"}}}
			if os.Getenv("CLW_CONFIG_PEER") == "yes" {
				result.(map[string]any)["configOptions"] = syntheticConfigOptions(model, effort, efforts)
			}
		case "session/load", "session/resume":
			nativeID = req.Params.SessionID
			if req.Method == "session/load" {
				if raw, err := os.ReadFile(filepath.Join(dir, nativeID+"-context-replay")); err == nil {
					notify(map[string]any{"sessionUpdate": "user_message_chunk", "content": map[string]string{"type": "text", "text": string(raw)}})
					notify(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "context reply"}})
				}
				for i := 0; i < 2; i++ {
					notify(map[string]any{"sessionUpdate": "user_message_chunk", "content": map[string]string{"type": "text", "text": "identical user"}})
					notify(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "identical reply"}})
				}
				if _, err := os.Stat(filepath.Join(dir, "load-error")); err == nil {
					_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32001, "message": "synthetic load error"}})
					continue
				}
			}
			result = map[string]any{"_meta": map[string]any{"antigravity": map[string]any{"tier": map[string]string{"id": "free-tier", "name": "Antigravity Starter Quota"}}}}
			if os.Getenv("CLW_CONFIG_PEER") == "yes" {
				result.(map[string]any)["configOptions"] = syntheticConfigOptions(model, effort, efforts)
			}
		case "session/set_config_option":
			dir := os.Getenv("GEMINI_HOME")
			f, _ := os.OpenFile(filepath.Join(dir, "config-writes"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			f.WriteString(req.Params.ConfigID + "=" + req.Params.Value + "\n")
			f.Close()
			if _, err := os.Stat(filepath.Join(dir, "hold-config")); err == nil {
				for {
					if _, err := os.Stat(filepath.Join(dir, "release-config")); err == nil {
						break
					}
					time.Sleep(time.Millisecond)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "config-error")); err == nil {
				_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32001, "message": "SECRET provider diagnostic"}})
				continue
			}
			if req.Params.ConfigID == "mode" {
				mode = req.Params.Value
			} else if req.Params.ConfigID == "model" {
				model, effort, efforts = req.Params.Value, "medium", []string{"medium", "high"}
			} else {
				effort = req.Params.Value
			}
			result = map[string]any{"configOptions": syntheticConfigOptions(model, effort, efforts)}
		case "session/set_model":
			_ = os.WriteFile(filepath.Join(os.Getenv("GEMINI_HOME"), "model"), []byte(req.Params.ModelID), 0600)
		case "session/prompt":
			dir := os.Getenv("GEMINI_HOME")
			if os.Getenv("CLW_IDENTITY_CHECK") == "yes" {
				path := os.Getenv("CLAWDLINE_PROTOCOL_SESSION_FILE")
				raw, err := os.ReadFile(path)
				var identity struct {
					Assistant string `json:"assistant"`
					ID        string `json:"conversation_id"`
				}
				if err != nil || json.Unmarshal(raw, &identity) != nil || identity.Assistant != "antigravity" || identity.ID != nativeID {
					os.Exit(36)
				}
				if err := os.WriteFile(filepath.Join(dir, "identity-read-"+nativeID), []byte(path), 0600); err != nil {
					os.Exit(37)
				}
			}
			if os.Getenv("CLW_PERMISSION_PEER") == "yes" {
				for i := 0; i < 2; i++ {
					_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": "native-permission", "method": "session/request_permission", "params": map[string]any{"sessionId": nativeID, "toolCall": map[string]string{"toolCallId": "tool-1", "title": "Run synthetic command?"}, "options": []any{map[string]string{"optionId": "allow-exact", "name": "Allow once", "kind": "allow_once"}, map[string]string{"optionId": "reject-exact", "name": "Reject once", "kind": "reject_once"}}}})
					if !scanner.Scan() {
						os.Exit(34)
					}
					f, _ := os.OpenFile(filepath.Join(dir, "permissions"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
					_, _ = f.Write(append(append([]byte(nil), scanner.Bytes()...), '\n'))
					_ = f.Close()
				}
				_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]string{"stopReason": "end_turn"}})
				continue
			}
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
			payload, _ := json.Marshal(req.Params.Prompt)
			_ = os.WriteFile(filepath.Join(dir, nativeID+"-prompt"), payload, 0600)
			_ = os.WriteFile(filepath.Join(dir, nativeID+"-capability"), []byte(os.Getenv("CLAWDLINE_SQUAD_CAPABILITY_FILE")), 0600)
			if len(req.Params.Prompt) == 2 && req.Params.Prompt[0].Type == "resource" {
				flattened := "Embedded resource (" + req.Params.Prompt[0].Resource.URI + "):\n" + req.Params.Prompt[0].Resource.Text + " " + req.Params.Prompt[1].Text
				_ = os.WriteFile(filepath.Join(dir, nativeID+"-context-replay"), []byte(flattened), 0600)
			}
			f.WriteString("prompt\n")
			f.Close()
			if _, err := os.Stat(filepath.Join(dir, "prompt-uncertain")); err == nil {
				// Keep the process alive with no response. Host shutdown cancels
				// the accepted call, injecting uncertain delivery without relying
				// on platform-specific handling of an already-exited process group.
				for {
					time.Sleep(time.Second)
				}
			}
			if os.Getenv("CLW_RESTORE_PEER") == "yes" {
				notify(map[string]any{"sessionUpdate": "user_message_chunk", "content": map[string]string{"type": "text", "text": req.Params.Prompt[0].Text}})
			}
			for {
				if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
					break
				}
				time.Sleep(time.Millisecond)
			}
			notify(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "read-1", "title": "Read synthetic file", "kind": "read", "status": "in_progress"})
			notify(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "synthetic reply"}})
			reason := "end_turn"
			if raw, err := os.ReadFile(filepath.Join(dir, "prompt-stop-reason")); err == nil {
				reason = strings.TrimSpace(string(raw))
			}
			result = map[string]string{"stopReason": reason}
		}
		if os.Getenv("CLW_MODE_PEER") != "" && (req.Method == "session/new" || req.Method == "session/resume" || req.Method == "session/load" || req.Method == "session/set_config_option") {
			options, _ := result.(map[string]any)["configOptions"].([]any)
			native := mode
			if req.Method == "session/set_config_option" && os.Getenv("CLW_MODE_PEER") == "wrong-ack" {
				native = "default"
			}
			if !(req.Method == "session/set_config_option" && os.Getenv("CLW_MODE_PEER") == "missing-ack") {
				result.(map[string]any)["configOptions"] = append(options, syntheticModeOption(native))
			}
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		if req.Method == "session/set_config_option" {
			if _, err := os.Stat(filepath.Join(os.Getenv("GEMINI_HOME"), "config-update")); err == nil {
				effort = "high"
				notify(map[string]any{"sessionUpdate": "config_option_update", "configOptions": syntheticConfigOptions(model, effort, efforts)})
			}
		}
		if req.Method == "session/prompt" && !(len(req.Params.Prompt) == 1 && req.Params.Prompt[0].Text == "/usage") {
			notify(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "read-1", "status": "completed"})
		}
	}
	os.Exit(0)
}

func syntheticConfigOptions(model, effort string, efforts []string) []any {
	values := []any{}
	for _, v := range efforts {
		values = append(values, map[string]string{"value": v, "name": v})
	}
	return []any{
		map[string]any{"id": "model", "type": "select", "currentValue": model, "options": []any{map[string]string{"value": "synthetic-flash", "name": "Synthetic Flash"}, map[string]string{"value": "synthetic-pro", "name": "Synthetic Pro"}}},
		map[string]any{"id": "thinking_level", "type": "select", "category": "thought_level", "currentValue": effort, "options": values},
	}
}

func TestOwnedNativeModelUsesStructuredBaseID(t *testing.T) {
	t.Setenv("CLW_CONFIG_PEER", "yes")
	h, row, _ := fixture(t)
	facts, _ := h.Facts(row)
	if facts.Model != "synthetic-flash" || len(facts.Models) != 2 || facts.Models[0].Name != "Synthetic Flash" {
		t.Fatalf("structured model was replaced by legacy composite metadata: %+v", facts)
	}
}

func TestOwnedNativeConfigSelectionKeepsAcknowledgedTruth(t *testing.T) {
	t.Setenv("CLW_CONFIG_PEER", "yes")
	h, row, dir := fixture(t)
	facts, _ := h.Facts(row)
	if facts.Model != "synthetic-flash" || facts.Effort != "high" || !reflect.DeepEqual(facts.Efforts, []string{"low", "medium", "high"}) {
		t.Fatal(facts)
	}
	checks := 0
	if err := h.SendProtocol(context.Background(), row, "/effort low", func() error { checks++; return nil }); err != nil {
		t.Fatal(err)
	}
	facts, _ = h.Facts(row)
	if facts.Model != "synthetic-flash" || facts.Effort != "low" || checks != 1 {
		t.Fatal(facts, checks)
	}
	for _, command := range []string{"/effort", "/effort max", "/effort low extra", "/model synthetic-high"} {
		if err := h.Send(context.Background(), row, command); err == nil {
			t.Fatalf("accepted %q", command)
		}
	}
	denied := errors.New("revoked")
	if err := h.SendProtocol(context.Background(), row, "/effort high", func() error { return denied }); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	if err := h.Send(context.Background(), row, "/model synthetic-pro"); err != nil {
		t.Fatal(err)
	}
	facts, _ = h.Facts(row)
	if facts.Model != "synthetic-pro" || facts.Effort != "medium" || !reflect.DeepEqual(facts.Efforts, []string{"medium", "high"}) {
		t.Fatal(facts)
	}
	if err := h.Send(context.Background(), row, "/effort low"); err == nil {
		t.Fatal("stale effort offered")
	}
	if err := os.WriteFile(filepath.Join(dir, "config-error"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.Send(context.Background(), row, "/effort high"); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatal(err)
	}
	held, _ := h.Facts(row)
	if held.Effort != "medium" || held.Model != "synthetic-pro" {
		t.Fatal(held)
	}
	writes, _ := os.ReadFile(filepath.Join(dir, "config-writes"))
	if string(writes) != "thinking_level=low\nmodel=synthetic-pro\nthinking_level=high\n" {
		t.Fatalf("writes=%q", writes)
	}
	for _, file := range []string{"prompts", "usage", "model"} {
		if _, err := os.Stat(filepath.Join(dir, file)); !os.IsNotExist(err) {
			t.Fatalf("metadata wrote %s", file)
		}
	}
	if page, _ := h.Transcript(row, 10); len(page.Entries) != 0 {
		t.Fatal(page)
	}
	facts.Efforts[0] = "tampered"
	held, _ = h.Facts(row)
	if held.Efforts[0] != "medium" {
		t.Fatal("efforts snapshot aliases state")
	}
}

func TestOwnedNativeConfigNotificationFollowsResponse(t *testing.T) {
	t.Setenv("CLW_CONFIG_PEER", "yes")
	h, row, dir := fixture(t)
	if err := os.WriteFile(filepath.Join(dir, "config-update"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.Send(context.Background(), row, "/effort low"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		facts, _ := h.Facts(row)
		if facts.Effort == "high" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("later native notification did not replace acknowledged effort")
}

func TestOwnedLegacyMetadataOffersNoEffortControl(t *testing.T) {
	h, row, dir := fixture(t)
	facts, _ := h.Facts(row)
	if facts.Effort != "" || len(facts.Efforts) != 0 {
		t.Fatal(facts)
	}
	if err := h.Send(context.Background(), row, "/effort high"); err == nil {
		t.Fatal("invented legacy effort control")
	}
	if _, err := os.Stat(filepath.Join(dir, "config-writes")); !os.IsNotExist(err) {
		t.Fatal("legacy effort wrote native pipe")
	}
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
	second, err := h.Open(context.Background(), ports.OpenRequest{Cwd: dir})
	if err != nil {
		t.Fatal("second preview refused", err)
	}
	if _, err := h.Open(context.Background(), ports.OpenRequest{Cwd: dir}); err == nil {
		t.Fatal("preview process limit ignored")
	}
	if err := h.Close(context.Background(), second); err != nil {
		t.Fatal(err)
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
