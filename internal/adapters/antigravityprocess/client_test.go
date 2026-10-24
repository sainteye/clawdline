//go:build darwin || linux

package antigravityprocess

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The re-exec server uses real stdin/stdout, never the provider or credentials.
func TestACPProcess(t *testing.T) {
	if os.Getenv("CLAWDLINE_ACP_FAKE") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode == "descendant" {
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	wire, err := os.OpenFile(filepath.Join(os.Getenv("GEMINI_HOME"), "wire"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(31)
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1<<22)
	enc := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		wire.Write(append(line, '\n'))
		wire.Sync()
		var m map[string]json.RawMessage
		if json.Unmarshal(line, &m) != nil {
			os.Exit(32)
		}
		var method string
		json.Unmarshal(m["method"], &method)
		if method == "" {
			continue
		}
		id := m["id"]
		if mode == "wrong-id" {
			id = json.RawMessage(`"1"`)
		}
		if mode == "eof" {
			os.Exit(0)
		}
		if mode == "malformed" {
			fmt.Fprintln(os.Stdout, `{bad`)
			time.Sleep(time.Hour)
		}
		if mode == "oversized" {
			fmt.Fprintln(os.Stdout, strings.Repeat("x", 8192))
			time.Sleep(time.Hour)
		}
		if mode == "rpc-error" {
			enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32001, "message": "SECRET /private/account https://auth.invalid"}})
			continue
		}
		if mode == "both" {
			enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}, "error": map[string]any{"code": -1, "message": "bad"}})
			continue
		}
		var result any
		switch method {
		case "initialize":
			if os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" || os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("LC_ALL") != "C" {
				os.Exit(33)
			}
			result = map[string]any{"protocolVersion": 1, "authMethods": []any{map[string]string{"id": "oauth-personal"}}}
			if mode != "no-restore" {
				result.(map[string]any)["agentCapabilities"] = map[string]any{"promptCapabilities": map[string]bool{"embeddedContext": mode != "no-context"}, "loadSession": true, "sessionCapabilities": map[string]any{"resume": map[string]any{}, "list": map[string]any{}}}
			}
		case "authenticate":
			if !strings.Contains(string(m["params"]), `"methodId":"oauth-personal"`) {
				os.Exit(34)
			}
			result = map[string]any{}
		case "session/list":
			result = map[string]any{"sessions": []any{map[string]string{"sessionId": "saved-exact", "cwd": os.Getenv("GEMINI_HOME")}}}
		case "session/load", "session/resume":
			var params struct {
				SessionID string `json:"sessionId"`
			}
			_ = json.Unmarshal(m["params"], &params)
			if mode == "restore-error" {
				enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32001, "message": "SECRET native diagnostic"}})
				continue
			}
			if method == "session/load" {
				enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": params.SessionID, "update": map[string]any{"sessionUpdate": "user_message_chunk", "content": map[string]string{"type": "text", "text": "saved user turn"}}}})
			}
			result = map[string]any{"_meta": map[string]any{"antigravity": map[string]any{"tier": map[string]string{"id": "free-tier", "name": "Antigravity Starter Quota"}}}}
			if mode == "restore-wrong-id" {
				result.(map[string]any)["sessionId"] = "another-conversation"
			}
			if mode == "restore-paid" {
				result.(map[string]any)["_meta"] = map[string]any{}
			}
		case "session/new":
			tier := "free-tier"
			if mode == "paid-tier" {
				tier = "paid"
			}
			result = map[string]any{"sessionId": "fake-session", "models": map[string]string{"currentModelId": "flash-high"}, "configOptions": []any{map[string]string{"id": "model", "currentValue": "flash"}}, "_meta": map[string]any{"antigravity": map[string]any{"tier": map[string]string{"id": tier, "name": "Antigravity Starter Quota"}}}}
		case "session/set_model", "session/set_config_option":
			if mode == "metadata-stall" {
				time.Sleep(time.Hour)
			}
			if mode == "metadata-error" {
				enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32001, "message": "SECRET provider account"}})
				continue
			}
			result = map[string]any{}
			if method == "session/set_config_option" {
				result = map[string]any{"configOptions": []any{map[string]string{"id": "thinking_level", "type": "select", "currentValue": "low"}}}
			}
		case "session/prompt":
			var params struct {
				Prompt []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			}
			_ = json.Unmarshal(m["params"], &params)
			if len(params.Prompt) == 1 && params.Prompt[0].Text == "/usage" {
				if mode == "metadata-stall" {
					time.Sleep(time.Hour)
				}
				if mode == "metadata-error" {
					enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32001, "message": "SECRET provider account"}})
					continue
				}
				for _, text := range []string{"### Quota Limits\n", "- **Gemini Models**\n  - **Weekly Limit Remaining:** 62% remaining"} {
					if mode == "metadata-large" {
						text = strings.Repeat("x", 3000)
					}
					enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fake-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": text}}}})
				}
				enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fake-session", "update": map[string]any{"sessionUpdate": "usage_update", "used": 11, "size": 100}}})
				result = map[string]string{"stopReason": "end_turn"}
				break
			}
			if mode == "stall-read" || mode == "tree" || mode == "tree-exit" {
				if mode == "tree" || mode == "tree-exit" {
					child := exec.Command(os.Args[0], "-test.run=^TestACPProcess$", "--", "descendant")
					child.Env = os.Environ()
					if child.Start() != nil {
						os.Exit(35)
					}
					os.WriteFile(filepath.Join(os.Getenv("GEMINI_HOME"), "child-pid"), []byte(fmt.Sprint(child.Process.Pid)), 0600)
					go func() { child.Wait() }()
					if mode == "tree-exit" {
						os.Exit(0)
					}
				}
				time.Sleep(time.Hour)
			}
			enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fake-session", "update": map[string]string{"sessionUpdate": "agent_message_chunk", "text": "visible"}}})
			paramsPermission := map[string]any{"sessionId": "fake-session", "options": []any{map[string]string{"optionId": "only-real-offer", "kind": "allow_once"}}}
			if strings.HasPrefix(mode, "permission-") {
				paramsPermission["toolCall"] = map[string]string{"toolCallId": "tool-exact", "title": "Synthetic command"}
				paramsPermission["options"] = []any{map[string]string{"optionId": "only-real-offer", "name": "Allow once", "kind": "allow_once"}, map[string]string{"optionId": "reject-exact", "name": "Reject once", "kind": "reject_once"}}
			}
			enc.Encode(map[string]any{"jsonrpc": "2.0", "id": "permission-exact", "method": "session/request_permission", "params": paramsPermission})
			if !scanner.Scan() {
				os.Exit(36)
			}
			line = append([]byte(nil), scanner.Bytes()...)
			wire.Write(append(line, '\n'))
			wire.Sync()
			valid := strings.Contains(string(line), `"outcome":"cancelled"`) && !strings.Contains(string(line), "optionId")
			if mode == "permission-select" {
				valid = strings.Contains(string(line), `"outcome":"selected"`) && strings.Contains(string(line), `"optionId":"reject-exact"`)
			}
			if !strings.Contains(string(line), `"id":"permission-exact"`) || !valid {
				os.Exit(37)
			}
			enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fake-session", "update": map[string]string{"sessionUpdate": "tool_call_update", "status": "completed"}}})
			result = map[string]string{"stopReason": "end_turn"}
		default:
			os.Exit(38)
		}
		enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
		if method == "session/new" && mode == "stall-write" {
			time.Sleep(time.Hour)
		}
		if method == "session/prompt" {
			enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fake-session", "update": map[string]string{"sessionUpdate": "usage_update"}}})
		}
	}
	os.Exit(0)
}

func TestConfigOptionUsesOneCheckedMetadataRPC(t *testing.T) {
	c, dir := fake(t, "happy", nil)
	if err := c.SetConfigOptionChecked(context.Background(), "thinking_level", "low", nil, nil); err == nil {
		t.Fatal("unauthenticated config accepted")
	}
	ready(t, c)
	checks, applied := 0, 0
	if err := c.SetConfigOptionChecked(context.Background(), "thinking_level", "low", func() error { checks++; return nil }, func(raw json.RawMessage) error {
		applied++
		if !strings.Contains(string(raw), `"currentValue":"low"`) {
			return errors.New("missing native result")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if checks != 1 || applied != 1 {
		t.Fatal(checks, applied)
	}
	wire, err := os.ReadFile(filepath.Join(dir, "wire"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(wire)), "\n")
	var last struct {
		Method string            `json:"method"`
		Params map[string]string `json:"params"`
	}
	if len(lines) != 4 || json.Unmarshal([]byte(lines[3]), &last) != nil || last.Method != "session/set_config_option" || !reflect.DeepEqual(last.Params, map[string]string{"sessionId": "fake-session", "configId": "thinking_level", "value": "low"}) {
		t.Fatalf("wire=%s", wire)
	}
	denied := errors.New("revoked")
	if err := c.SetConfigOptionChecked(context.Background(), "model", "synthetic", func() error { return denied }, nil); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	settings(t, dir, `{"useG1Credits":true,"auth":{"type":"oauth-personal"}}`)
	requireError(t, c.SetConfigOptionChecked(context.Background(), "thinking_level", "high", nil, nil), "personal_settings_required", NotWritten)
	if got := methods(t, dir); len(got) != 4 {
		t.Fatal("refused config reached peer", got)
	}
}

func TestConfigOptionFailuresDoNotApplyOrRetry(t *testing.T) {
	for _, mode := range []string{"metadata-error", "metadata-stall"} {
		t.Run(mode, func(t *testing.T) {
			c, dir := fake(t, mode, nil)
			ready(t, c)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			applied := false
			err := c.SetConfigOptionChecked(ctx, "thinking_level", "low", nil, func(json.RawMessage) error { applied = true; return nil })
			code, delivery := "rpc_error", Acknowledged
			if mode == "metadata-stall" {
				code, delivery = "cancelled", Uncertain
			}
			requireError(t, err, code, delivery)
			if applied || len(methods(t, dir)) != 4 {
				t.Fatal("failed config applied or retried")
			}
		})
	}
}

func TestMetadataCallsKeepUpdatesSeparateAndUseOneRPC(t *testing.T) {
	var mu sync.Mutex
	var updates []string
	c, dir := fake(t, "happy", func(_ context.Context, raw json.RawMessage) error {
		var m struct {
			Params struct {
				Update struct {
					Kind string `json:"sessionUpdate"`
				}
			}
		}
		_ = json.Unmarshal(raw, &m)
		mu.Lock()
		updates = append(updates, m.Params.Update.Kind)
		mu.Unlock()
		return nil
	})
	ready(t, c)
	checks := 0
	if err := c.SetModelChecked(context.Background(), "synthetic-low", func() error { checks++; return nil }); err != nil {
		t.Fatal(err)
	}
	text, err := c.Usage(context.Background())
	if err != nil || !strings.Contains(text, "62% remaining") || checks != 1 {
		t.Fatalf("usage=%q err=%v checks=%d", text, err, checks)
	}
	mu.Lock()
	for _, kind := range updates {
		if kind == "agent_message_chunk" {
			t.Fatal("quota leaked into conversation callback")
		}
	}
	if len(updates) == 0 {
		t.Fatal("non-message metadata updates lost")
	}
	mu.Unlock()
	if got := methods(t, dir); !reflect.DeepEqual(got, []string{"initialize", "authenticate", "session/new", "session/set_model", "session/prompt"}) {
		t.Fatal(got)
	}
	// The next ordinary turn remains a prompt and its text reaches the callback.
	if _, err := c.Prompt(context.Background(), "ordinary turn"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(strings.Join(updates, ","), "agent_message_chunk") {
		t.Fatal("usage collector captured a later ordinary turn")
	}
}

func TestModelOperationRevocationHoldsUntilPipeWrite(t *testing.T) {
	c, dir := fake(t, "happy", nil)
	ready(t, c)
	c.writeMu.Lock()
	result := make(chan error, 1)
	denied := errors.New("revoked")
	go func() {
		result <- c.SetModelChecked(context.Background(), "synthetic-low", func() error { return denied })
	}()
	// Wait until this call owns callMu but cannot yet reach its operation check.
	deadline := time.Now().Add(time.Second)
	for c.callMu.TryLock() {
		c.callMu.Unlock()
		if time.Now().After(deadline) {
			c.writeMu.Unlock()
			t.Fatal("model call did not start")
		}
		time.Sleep(time.Millisecond)
	}
	c.writeMu.Unlock()
	if err := <-result; !errors.Is(err, denied) {
		t.Fatal(err)
	}
	if got := methods(t, dir); len(got) != 3 {
		t.Fatal("revoked model reached pipe", got)
	}
}

func TestMetadataCallsRefuseUnsafeSettingsAndStates(t *testing.T) {
	c, dir := fake(t, "happy", nil)
	if err := c.SetModelChecked(context.Background(), "synthetic", nil); err == nil {
		t.Fatal("unauthenticated model selection")
	}
	if _, err := c.Usage(context.Background()); err == nil {
		t.Fatal("unauthenticated usage")
	}
	ready(t, c)
	settings(t, dir, `{"useG1Credits":true,"auth":{"type":"oauth-personal"}}`)
	requireError(t, c.SetModelChecked(context.Background(), "synthetic", nil), "personal_settings_required", NotWritten)
	_, err := c.Usage(context.Background())
	requireError(t, err, "personal_settings_required", NotWritten)
	if got := methods(t, dir); len(got) != 3 {
		t.Fatal(got)
	}
}

func TestMetadataErrorsCancellationAndBusyNeverRetry(t *testing.T) {
	for _, model := range []bool{true, false} {
		t.Run(fmt.Sprint(model), func(t *testing.T) {
			for _, mode := range []string{"metadata-error", "metadata-stall", "metadata-large"} {
				t.Run(mode, func(t *testing.T) {
					if model && mode == "metadata-large" {
						t.Skip("only usage aggregates frames")
					}
					c, dir := fake(t, mode, nil)
					ready(t, c)
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					result := make(chan error, 1)
					go func() {
						if model {
							result <- c.SetModelChecked(ctx, "synthetic", nil)
						} else {
							_, err := c.Usage(ctx)
							result <- err
						}
					}()
					if mode == "metadata-stall" {
						deadline := time.Now().Add(time.Second)
						for len(methods(t, dir)) < 4 && time.Now().Before(deadline) {
							time.Sleep(time.Millisecond)
						}
						_, err := c.Usage(context.Background())
						requireError(t, err, "busy", NotWritten)
						cancel()
					}
					err := <-result
					code, delivery := "rpc_error", Acknowledged
					if mode == "metadata-stall" {
						code, delivery = "cancelled", Uncertain
					}
					if mode == "metadata-large" {
						code = "usage_too_large"
					}
					requireError(t, err, code, delivery)
					if strings.Contains(err.Error(), "SECRET") {
						t.Fatal("raw provider error leaked")
					}
					if got := methods(t, dir); len(got) != 4 {
						t.Fatal("metadata retry", got)
					}
				})
			}
		})
	}
}

func fake(t *testing.T, mode string, update func(context.Context, json.RawMessage) error) (*Client, string) {
	return fakePermission(t, mode, update, nil)
}
func fakePermission(t *testing.T, mode string, update func(context.Context, json.RawMessage) error, permission func(context.Context, *PermissionRequest) error) (*Client, string) {
	t.Helper()
	t.Setenv("CLAWDLINE_ACP_FAKE", "1")
	t.Setenv("OPENAI_API_KEY", "synthetic")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "synthetic")
	t.Setenv("GEMINI_API_KEY", "synthetic")
	dir := t.TempDir()
	settings(t, dir, `{"useG1Credits":false,"auth":{"type":"oauth-personal"}}`)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	maxFrame := 4096
	if mode == "stall-write" {
		maxFrame = 1 << 22
	}
	c, err := Start(context.Background(), Options{Executable: exe, Args: []string{"-test.run=^TestACPProcess$", "--", mode}, AccountDir: dir, WorkingDir: dir, MaxFrameBytes: maxFrame, CallTimeout: 2 * time.Second, OnUpdate: update, OnPermission: permission})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			// Darwin's restricted sandbox returns EPERM for a group consisting
			// only of an exited, unreaped leader. Keep cleanup unknown visible.
			if mode != "eof" {
				t.Error(err)
			} else {
				requireError(t, err, "cleanup_failed", Uncertain)
				t.Log("natural EOF group cleanup returned a visible failure")
			}
		}
	})
	return c, dir
}
func settings(t *testing.T, dir, value string) {
	t.Helper()
	p := filepath.Join(dir, "antigravity-acp")
	if err := os.MkdirAll(p, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "settings.json"), []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}
func ready(t *testing.T, c *Client) {
	t.Helper()
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AuthenticatePersonal(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.NewSession(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func requireError(t *testing.T, err error, code string, delivery Delivery) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Code != code || e.Delivery != delivery {
		t.Fatalf("got %#v; want %s %s", err, code, delivery)
	}
}
func methods(t *testing.T, dir string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "wire"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var m map[string]json.RawMessage
		json.Unmarshal([]byte(line), &m)
		var method string
		json.Unmarshal(m["method"], &method)
		if method != "" {
			out = append(out, method)
		}
	}
	return out
}

func TestPersonalFlowAndOrderedLateUpdates(t *testing.T) {
	var mu sync.Mutex
	var updates []string
	late := make(chan struct{})
	c, dir := fake(t, "happy", func(ctx context.Context, raw json.RawMessage) error {
		var m struct {
			Params struct {
				Update struct {
					Kind string `json:"sessionUpdate"`
				}
			}
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			return err
		}
		mu.Lock()
		updates = append(updates, m.Params.Update.Kind)
		mu.Unlock()
		if m.Params.Update.Kind == "usage_update" {
			close(late)
		}
		return nil
	})
	ready(t, c)
	if c.SessionID() != "fake-session" {
		t.Fatal("lost identity")
	}
	r, err := c.Prompt(context.Background(), "hello")
	if err != nil || r.StopReason != "end_turn" {
		t.Fatalf("%+v %v", r, err)
	}
	select {
	case <-late:
	case <-time.After(time.Second):
		t.Fatal("late update was lost")
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(updates, []string{"agent_message_chunk", "tool_call_update", "usage_update"}) {
		t.Fatal(updates)
	}
	if got := methods(t, dir); !reflect.DeepEqual(got, []string{"initialize", "authenticate", "session/new", "session/prompt"}) {
		t.Fatal(got)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPersonalSettingsAndTierRefuseBeforePrompt(t *testing.T) {
	for _, value := range []string{`{}`, `{"useG1Credits":true,"auth":{"type":"oauth-personal"}}`, `{"useG1Credits":false,"auth":{"type":"api-key"}}`, `{"useG1Credits":false,"useG1Credits":true,"auth":{"type":"oauth-personal"}}`, `bad`} {
		t.Run(value, func(t *testing.T) {
			c, dir := fake(t, "happy", nil)
			if _, err := c.Initialize(context.Background()); err != nil {
				t.Fatal(err)
			}
			settings(t, dir, value)
			_, err := c.AuthenticatePersonal(context.Background())
			requireError(t, err, "personal_settings_required", NotWritten)
			if got := methods(t, dir); len(got) != 1 {
				t.Fatal(got)
			}
		})
	}
	t.Run("changed-between-calls", func(t *testing.T) {
		c, dir := fake(t, "happy", nil)
		ready(t, c)
		settings(t, dir, `{"useG1Credits":true,"auth":{"type":"oauth-personal"}}`)
		_, err := c.Prompt(context.Background(), "never send")
		requireError(t, err, "personal_settings_required", NotWritten)
		if got := methods(t, dir); len(got) != 3 {
			t.Fatal(got)
		}
	})
	t.Run("tier", func(t *testing.T) {
		c, dir := fake(t, "paid-tier", nil)
		c.Initialize(context.Background())
		c.AuthenticatePersonal(context.Background())
		_, err := c.NewSession(context.Background())
		requireError(t, err, "free_tier_required", Acknowledged)
		_, err = c.Prompt(context.Background(), "never send")
		requireError(t, err, "not_ready", NotWritten)
		if got := methods(t, dir); len(got) != 3 {
			t.Fatal(got)
		}
	})
	t.Run("initialize-is-not-auth", func(t *testing.T) {
		c, _ := fake(t, "happy", nil)
		c.Initialize(context.Background())
		_, err := c.Prompt(context.Background(), "never send")
		requireError(t, err, "not_ready", NotWritten)
	})
}

func TestProtocolFailuresHaveSanitizedDistinctDelivery(t *testing.T) {
	for mode, code := range map[string]string{"wrong-id": "unexpected_id", "eof": "eof", "malformed": "invalid_frame", "oversized": "frame_too_large", "rpc-error": "rpc_error", "both": "invalid_response"} {
		t.Run(mode, func(t *testing.T) {
			c, dir := fake(t, mode, nil)
			_, err := c.Initialize(context.Background())
			delivery := Uncertain
			if mode == "rpc-error" {
				delivery = Acknowledged
			}
			requireError(t, err, code, delivery)
			if strings.Contains(err.Error(), "SECRET") {
				t.Fatal(err)
			}
			if mode == "rpc-error" {
				var e *Error
				errors.As(err, &e)
				if e.RPCCode != -32001 {
					t.Fatal(e)
				}
			}
			if got := methods(t, dir); len(got) != 1 {
				t.Fatal("retransmission", got)
			}
		})
	}
}

func TestCancellationUnblocksPipeIOAndRefusesReplay(t *testing.T) {
	for _, mode := range []string{"stall-read", "stall-write"} {
		t.Run(mode, func(t *testing.T) {
			c, _ := fake(t, mode, nil)
			ready(t, c)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			text := "hello"
			if mode == "stall-write" {
				text = strings.Repeat("x", 1<<20)
			}
			_, err := c.Prompt(ctx, text)
			requireError(t, err, "cancelled", Uncertain)
			_, err = c.Prompt(context.Background(), "no replay")
			requireError(t, err, "closed", NotWritten)
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOwnedChildCleanupAndConcurrentCancel(t *testing.T) {
	c, dir := fake(t, "tree", nil)
	ready(t, c)
	result := make(chan error)
	go func() { _, err := c.Prompt(context.Background(), "spawn"); result <- err }()
	var pid int
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(filepath.Join(dir, "child-pid"))
		if len(b) > 0 {
			fmt.Sscan(string(b), &pid)
			break
		}
		time.Sleep(time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("child not started")
	}
	// The child is a real member of the private group, not just a fake PID.
	if group, err := syscall.Getpgid(pid); err != nil || group != c.cmd.Process.Pid {
		t.Fatalf("group=%d err=%v", group, err)
	}
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Cancel(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	select {
	case err := <-result:
		var e *Error
		if !errors.As(err, &e) || e.Delivery != Uncertain {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("call did not exit")
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("owned child %d survived", pid)
}

func TestLifetimeCancellationAndBusyAndDeadline(t *testing.T) {
	t.Run("busy-and-call-deadline", func(t *testing.T) {
		c, dir := fake(t, "stall-read", nil)
		ready(t, c)
		result := make(chan error)
		go func() { _, err := c.Prompt(context.Background(), "once"); result <- err }()
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if len(methods(t, dir)) == 4 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		_, err := c.Prompt(context.Background(), "duplicate")
		requireError(t, err, "busy", NotWritten)
		select {
		case err := <-result:
			requireError(t, err, "cancelled", Uncertain)
		case <-time.After(3 * time.Second):
			t.Fatal("deadline did not stop call")
		}
		if got := methods(t, dir); len(got) != 4 {
			t.Fatal("automatic replay", got)
		}
	})
	t.Run("lifetime", func(t *testing.T) {
		t.Setenv("CLAWDLINE_ACP_FAKE", "1")
		dir := t.TempDir()
		exe, _ := os.Executable()
		ctx, cancel := context.WithCancel(context.Background())
		c, err := Start(ctx, Options{Executable: exe, Args: []string{"-test.run=^TestACPProcess$", "--", "happy"}, WorkingDir: dir, AccountDir: dir, MaxFrameBytes: 4096, CallTimeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Initialize(context.Background()); err != nil {
			t.Fatal(err)
		}
		cancel()
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Kill(c.cmd.Process.Pid, 0); err != syscall.ESRCH {
			t.Fatalf("leader remains: %v", err)
		}
	})
}

func TestExitedLeaderStillOwnsLiveDescendant(t *testing.T) {
	c, dir := fake(t, "tree-exit", nil)
	ready(t, c)
	_, err := c.Prompt(context.Background(), "exit leader")
	requireError(t, err, "eof", Uncertain)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "child-pid"))
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	fmt.Sscan(string(b), &pid)
	if pid <= 0 {
		t.Fatal("no child pid")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("descendant %d survived its exited leader", pid)
}
