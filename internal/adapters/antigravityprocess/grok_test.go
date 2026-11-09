//go:build darwin || linux

package antigravityprocess

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

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestGrokProcessPeer(t *testing.T) {
	if os.Getenv("CLW_XPEER") != "yes" {
		return
	}
	dir := os.Getenv("GROK_HOME")
	if dir == "" || os.Getenv("GEMINI_HOME") != "" || os.Getenv("XAI_API_KEY") != "" || os.Getenv("GROK_DEPLOYMENT_KEY") != "" {
		os.Exit(41)
	}
	wire, err := os.OpenFile(filepath.Join(dir, "wire"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(42)
	}
	defer wire.Close()
	enc := json.NewEncoder(os.Stdout)
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		line := scan.Bytes()
		wire.Write(append(append([]byte(nil), line...), '\n'))
		wire.Sync()
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(line, &req) != nil {
			os.Exit(43)
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			methods := []any{map[string]string{"id": "grok.com"}}
			if os.Getenv("CLW_XAUTH") == "ready" || os.Getenv("CLW_XAUTH") == "expired" {
				methods = append(methods, map[string]string{"id": "cached_token"})
			}
			result = map[string]any{"protocolVersion": 1, "_meta": map[string]any{"grokShell": true, "agentVersion": "1.0.77"}, "authMethods": methods,
				"agentCapabilities": map[string]any{"promptCapabilities": map[string]bool{"embeddedContext": true}}}
		case "authenticate":
			var p struct {
				Method string `json:"methodId"`
				Meta   struct {
					Headless bool `json:"headless"`
				} `json:"_meta"`
			}
			if json.Unmarshal(req.Params, &p) != nil || p.Method != "cached_token" || !p.Meta.Headless {
				os.Exit(44)
			}
			if os.Getenv("CLW_XAUTH") == "expired" {
				enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32000, "message": "private native auth failure"}})
				continue
			}
		case "session/new":
			result = map[string]string{"sessionId": "synthetic-coding-session"}
		case "session/prompt":
			enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "synthetic-coding-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "Synthetic coding reply"}}}})
			result = map[string]string{"stopReason": "end_turn"}
		}
		enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
	os.Exit(0)
}

func grokProcess(t *testing.T, auth string, update func(context.Context, json.RawMessage) error) (*Client, string) {
	t.Helper()
	t.Setenv("CLW_XPEER", "yes")
	t.Setenv("CLW_XAUTH", auth)
	t.Setenv("GEMINI_HOME", "must-not-inherit")
	t.Setenv("XAI_API_KEY", "synthetic-key-not-a-credential")
	t.Setenv("GROK_DEPLOYMENT_KEY", "synthetic-deployment-not-a-credential")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	c, err := Start(context.Background(), Options{Assistant: session.AssistantGrok, Executable: exe, AccountDir: dir, WorkingDir: dir, Args: []string{"-test.run=^TestGrokProcessPeer$"}, MaxFrameBytes: 4096, CallTimeout: time.Second, OnUpdate: update})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c, dir
}

func TestGrokUsesOnlyAdvertisedCachedLogin(t *testing.T) {
	c, dir := grokProcess(t, "missing", nil)
	_, err := c.AuthenticatePersonal(context.Background())
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "auth_required" || typed.Delivery != NotWritten {
		t.Fatalf("missing login: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "wire"))
	if strings.Contains(string(raw), "authenticate") {
		t.Fatal("unadvertised login method was sent")
	}
}

func TestGrokRunsWithoutGeminiSettingsAndKeepsNativeCompletion(t *testing.T) {
	var text string
	c, dir := grokProcess(t, "ready", func(_ context.Context, raw json.RawMessage) error { text += string(raw); return nil })
	if _, err := c.AuthenticatePersonal(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.NewSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	r, err := c.Prompt(context.Background(), "Edit the synthetic program")
	if err != nil || r.StopReason != "end_turn" || !strings.Contains(text, "Synthetic coding reply") {
		t.Fatalf("prompt=%+v error=%v text=%s", r, err, text)
	}
	if _, err := os.Stat(filepath.Join(dir, "antigravity-acp")); !os.IsNotExist(err) {
		t.Fatal("Grok touched Gemini settings")
	}
	if _, err := c.Usage(context.Background()); err == nil {
		t.Fatal("Grok accepted Gemini's metadata prompt")
	}
}

func TestGrokAuthenticationFailureCannotOpenOrPrompt(t *testing.T) {
	c, dir := grokProcess(t, "expired", nil)
	_, err := c.AuthenticatePersonal(context.Background())
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "rpc_error" || typed.Delivery != Acknowledged || strings.Contains(err.Error(), "private") {
		t.Fatalf("auth failure=%v", err)
	}
	if _, err := c.NewSession(context.Background()); err == nil {
		t.Fatal("session created after failed auth")
	}
	if _, err := c.Prompt(context.Background(), "must not be sent"); err == nil {
		t.Fatal("prompt sent after failed auth")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "wire"))
	if strings.Count(string(raw), `"method":"authenticate"`) != 1 || strings.Contains(string(raw), "session/new") || strings.Contains(string(raw), "session/prompt") {
		t.Fatalf("failed auth retried or allowed work: %s", raw)
	}
}
