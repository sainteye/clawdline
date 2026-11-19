package projectassistantacp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func testLimits() Limits {
	return Limits{RPCPending: 8, IncomingPermissions: 4, Notifications: 32, FrameBytes: 1 << 20, CaptureBytes: 1 << 20, StderrBytes: 1024, OwnedDescendants: 64, WriteTimeout: time.Millisecond * 100, CleanupTimeout: time.Second, RPCTimeout: time.Second * 3, DescendantPoll: time.Millisecond * 10}
}
func testFence() pa.Fence {
	return pa.Fence{Key: pa.BotKey{MachineID: "machine", BotID: "bot"}, Project: pa.ProjectScope{MachineID: "machine", ProjectID: "project", CanonicalID: "repo", Kind: pa.RepositoryProject}, ConversationID: "chat", BindingID: "binding", Generation: 1}
}

// TestACPTestPeer runs only in a dedicated owned child process. It does not
// invoke an external provider, model, auth store or network endpoint.
func TestACPTestPeer(t *testing.T) {
	if os.Getenv("CLAWDLINE_ACP_TEST_PEER") != "1" {
		return
	}
	mode := os.Getenv("CLAWDLINE_ACP_TEST_MODE")
	if mode == "no-read" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	if mode == "stderr" {
		fmt.Fprint(os.Stderr, strings.Repeat("x", 8192))
	}
	writer := bufio.NewWriter(os.Stdout)
	send := func(f frame) {
		f.JSONRPC = "2.0"
		b, _ := json.Marshal(f)
		writer.Write(b)
		writer.WriteByte('\n')
		writer.Flush()
	}
	sendCompaction := func(id, status string) {
		send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]string{"sessionUpdate": "compaction_update", "compactionId": id, "status": status}})})
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024), 2<<20)
	var promptID json.RawMessage
	for scanner.Scan() {
		var f frame
		if json.Unmarshal(scanner.Bytes(), &f) != nil {
			os.Exit(3)
		}
		switch f.Method {
		case "initialize":
			if mode == "provider-claude-terminal-quota" {
				var init struct {
					ClientCapabilities struct {
						Meta struct {
							JetBrains struct {
								AIR struct {
									Version      int      `json:"version"`
									Capabilities []string `json:"capabilities"`
								} `json:"air"`
							} `json:"jetbrains"`
						} `json:"_meta"`
					} `json:"clientCapabilities"`
				}
				if json.Unmarshal(f.Params, &init) != nil || init.ClientCapabilities.Meta.JetBrains.AIR.Version != 1 ||
					!slices.Contains(init.ClientCapabilities.Meta.JetBrains.AIR.Capabilities, "sessionFailure") {
					os.Exit(9)
				}
			}
			if strings.HasPrefix(mode, "compact-") {
				var init struct {
					ClientCapabilities struct {
						Session struct {
							Compaction json.RawMessage `json:"compaction"`
						} `json:"session"`
					} `json:"clientCapabilities"`
				}
				if json.Unmarshal(f.Params, &init) != nil || string(init.ClientCapabilities.Session.Compaction) != "{}" {
					os.Exit(9)
				}
			}
			if strings.HasPrefix(mode, "configure-") || strings.HasPrefix(mode, "provider-") || mode == "exit" {
				name := adapterPackage(pa.Codex)
				if strings.Contains(mode, "claude") {
					name = adapterPackage(pa.ClaudeCode)
				}
				version := "scripted"
				if strings.Contains(mode, "wrong-version") {
					version = "wrong"
				}
				send(frame{ID: f.ID, Result: raw(map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": name, "version": version}, "agentCapabilities": map[string]any{"loadSession": true, "mcpCapabilities": map[string]bool{"http": true}, "sessionCapabilities": map[string]any{"systemPrompt": map[string]any{}}, "_meta": map[string]any{"claudeCode": map[string]any{}}}})})
			} else if mode == "steer" {
				send(frame{ID: f.ID, Result: raw(map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]bool{"loadSession": true}, "_meta": map[string]any{"steering": map[string]bool{"supported": true}}})})
			} else {
				send(frame{ID: f.ID, Result: raw(map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]bool{"loadSession": true}})})
			}
		case "session/new":
			if path := os.Getenv("CLAWDLINE_ACP_PARAMS_FILE"); path != "" {
				if err := os.WriteFile(path, scanner.Bytes(), 0600); err != nil {
					os.Exit(5)
				}
			}
			if mode == "native-context" || strings.HasPrefix(mode, "compact-") {
				// Adapters may publish metadata before answering session/new.
				send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]any{"sessionUpdate": "available_commands_update", "availableCommands": []any{map[string]string{"name": "compact"}}}})})
				send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]any{"sessionUpdate": "usage_update", "used": 0, "size": 100000}})})
			}
			send(frame{ID: f.ID, Result: raw(map[string]string{"sessionId": peerSessionID("CLAWDLINE_ACP_NEW_ID")})})
		case "session/load":
			if path := os.Getenv("CLAWDLINE_ACP_PARAMS_FILE"); path != "" {
				if err := os.WriteFile(path, scanner.Bytes(), 0600); err != nil {
					os.Exit(5)
				}
			}
			if mode == "load-burst" {
				for i := 0; i < 1024; i++ {
					send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "historical"}}})})
				}
			}
			send(frame{ID: f.ID, Result: raw(map[string]string{"sessionId": peerSessionID("CLAWDLINE_ACP_LOAD_ID")})})
		case "session/prompt":
			promptID = f.ID
			if path := os.Getenv("CLAWDLINE_ACP_PROMPT_LOG"); path != "" {
				log, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					os.Exit(9)
				}
				_, err = log.Write(append(append([]byte(nil), scanner.Bytes()...), '\n'))
				if closeErr := log.Close(); err != nil || closeErr != nil {
					os.Exit(9)
				}
			}
			if mode == "native-context" {
				send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]any{"sessionUpdate": "usage_update", "used": 12, "size": 100000}})})
				send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "Synthetic answer"}}})})
				send(frame{ID: f.ID, Result: raw(map[string]string{"stopReason": "end_turn"})})
				continue
			}
			if strings.HasPrefix(mode, "compact-") {
				var prompt struct {
					SessionID string `json:"sessionId"`
					Prompt    []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"prompt"`
				}
				var fields map[string]json.RawMessage
				if json.Unmarshal(f.Params, &prompt) != nil || json.Unmarshal(f.Params, &fields) != nil || len(fields) != 2 || prompt.SessionID != "scripted-session" || len(prompt.Prompt) != 1 || prompt.Prompt[0].Type != "text" {
					os.Exit(9)
				}
				if mode == "compact-race" && prompt.Prompt[0].Text == "hello" {
					send(frame{ID: f.ID, Result: raw(map[string]string{"stopReason": "end_turn", "text": "Normal answer"})})
					continue
				}
				if prompt.Prompt[0].Text != "/compact" {
					os.Exit(9)
				}
				if mode != "compact-missing-start" {
					sendCompaction("own-compaction", "in_progress")
				}
				switch mode {
				case "compact-wait":
					continue
				case "compact-eof":
					os.Exit(0)
				case "compact-rpc-error":
					send(frame{ID: f.ID, Error: &rpcError{Code: -32603, Message: "Synthetic compact failure"}})
					continue
				case "compact-wrong-id":
					sendCompaction("foreign-compaction", "completed")
				case "compact-failed":
					sendCompaction("own-compaction", "failed")
				case "compact-cancelled":
					sendCompaction("own-compaction", "cancelled")
				case "compact-unknown-status":
					sendCompaction("own-compaction", "not-a-terminal")
				case "compact-malformed-result":
					sendCompaction("own-compaction", "completed")
					send(frame{ID: f.ID, Result: raw(map[string]string{"text": "not a qualified terminal reply"})})
					continue
				case "compact-no-terminal":
					// A smaller reported token count cannot substitute for a terminal.
					send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]any{"sessionUpdate": "usage_update", "used": 0, "size": 100000}})})
				default:
					sendCompaction("own-compaction", "completed")
					if mode == "compact-duplicate-terminal" {
						sendCompaction("own-compaction", "failed")
					}
				}
				send(frame{ID: f.ID, Result: raw(map[string]string{"stopReason": "end_turn"})})
				continue
			}
			if mode == "provider-claude-terminal-quota" {
				send(frame{ID: f.ID, Result: json.RawMessage(os.Getenv("CLAWDLINE_ACP_ERROR"))})
				continue
			}
			if mode == "provider-error" || mode == "provider-error-eof" {
				var rpc rpcError
				if json.Unmarshal([]byte(os.Getenv("CLAWDLINE_ACP_ERROR")), &rpc) != nil {
					os.Exit(9)
				}
				send(frame{ID: f.ID, Error: &rpc})
				if mode == "provider-error-eof" {
					os.Exit(0)
				}
				continue
			}
			if mode == "quota-reply" {
				send(frame{ID: f.ID, Result: raw(map[string]string{"stopReason": "end_turn", "text": "You've hit your usage limit; authentication_failed 429 billing_error"})})
				continue
			}
			if strings.HasPrefix(mode, "native-stop-") {
				send(frame{ID: f.ID, Result: raw(map[string]string{"stopReason": strings.TrimPrefix(mode, "native-stop-"), "text": "native partial reply"})})
				continue
			}
			if strings.HasPrefix(mode, "burst") {
				for i := 0; i < 288; i++ {
					send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": fmt.Sprintf("%03d,", i)}}})})
				}
				if mode == "burst-permission" {
					send(frame{ID: raw("burst-permission"), Method: "session/request_permission", Params: raw(map[string]any{"sessionId": "scripted-session", "options": []any{map[string]string{"optionId": "allow", "name": "Allow once", "kind": "allow_once"}}})})
				} else {
					send(frame{ID: f.ID, Result: raw(map[string]string{"stopReason": "end_turn"})})
				}
				continue
			}
			if mode == "long-stream" || mode == "long-frame" {
				text := strings.Repeat("x", 8192)
				count := 129
				if mode == "long-frame" {
					text = strings.Repeat("x", (1<<20)+32)
					count = 1
				}
				for i := 0; i < count; i++ {
					send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": text}}})})
				}
				send(frame{ID: f.ID, Result: raw(map[string]string{"stopReason": "end_turn"})})
				continue
			}
			if mode == "quote" || mode == "handoff" {
				var payload struct {
					Prompt []struct {
						Text string `json:"text"`
					} `json:"prompt"`
				}
				if json.Unmarshal(f.Params, &payload) != nil || mode == "quote" && len(payload.Prompt) != 2 || mode == "handoff" && len(payload.Prompt) != 1 {
					os.Exit(4)
				}
				texts := []string{}
				for _, block := range payload.Prompt {
					texts = append(texts, block.Text)
				}
				send(frame{ID: f.ID, Result: raw(map[string]string{"stopReason": "end_turn", "text": strings.Join(texts, "\n")})})
				continue
			}
			if mode == "exit" {
				os.Exit(8)
			}
			if mode == "delayed-prompt" {
				time.Sleep(100 * time.Millisecond)
			}
			if mode == "silent" || mode == "steer" {
				continue
			}
			if mode == "permission" {
				send(frame{ID: raw("permission-rpc"), Method: "session/request_permission", Params: raw(map[string]any{"sessionId": "scripted-session", "toolCall": map[string]string{"kind": "read"}, "options": []any{map[string]string{"optionId": "read-once", "name": "Read once", "kind": "allow_once"}, map[string]string{"optionId": "always", "name": "Always", "kind": "allow_always"}, map[string]string{"optionId": "reject", "name": "Reject", "kind": "reject_once"}}})})
				continue
			}
			if mode == "toolcall" {
				// Native tool use reports content as an array, not one block.
				send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": "call-1", "title": "Read notes.md", "kind": "read", "status": "pending", "content": []any{map[string]any{"type": "content", "content": map[string]string{"type": "text", "text": "notes"}}}}})})
				send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]any{"sessionUpdate": "available_commands_update", "availableCommands": []any{}}})})
			}
			if mode == "segments" {
				// Claude Code's adapter streamed a ToolSearch call's markup as
				// chunks before the call itself, and two messages back to back.
				chunk := func(id, text string) {
					send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "messageId": id, "content": map[string]string{"type": "text", "text": text}}})})
				}
				chunk("m1", "<invoke name=\"ToolSearch\">\n<parameter name=\"query\">select:x</parameter>\n</invoke>")
				send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": "call-1", "title": "ToolSearch", "kind": "other", "status": "pending"}})})
				chunk("m2", "Previous run didn't finish; rerunning.")
				chunk("m3", "RESUME-CHECK-77")
				chunk("m3", " done, see <invoke name=\"x\"></invoke> above")
				send(frame{ID: f.ID, Result: raw(map[string]string{"stopReason": "end_turn"})})
				continue
			}
			if mode == "explicit" {
				send(frame{ID: raw("message-rpc"), Method: "clawdline/send_message", Params: raw(map[string]string{"sessionId": "scripted-session", "messageId": "one", "text": "explicit"})})
			}
			if mode == "overflow" {
				fmt.Fprintln(os.Stdout, strings.Repeat("x", 2<<20))
				continue
			}
			send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "partial"}}})})
			send(frame{ID: f.ID, Result: raw(map[string]string{"stopReason": "end_turn"})})
		case "session/set_mode":
			var set struct {
				SessionID string `json:"sessionId"`
				ModeID    string `json:"modeId"`
			}
			if json.Unmarshal(f.Params, &set) != nil || set.SessionID != "scripted-session" || set.ModeID != "plan" {
				send(frame{ID: f.ID, Error: &rpcError{Code: -32602, Message: "Invalid Mode"}})
				continue
			}
			send(frame{ID: f.ID, Result: raw(map[string]any{})})
		case "_session/steering":
			var steer struct {
				SessionID string `json:"sessionId"`
				Prompt    []struct {
					Text string `json:"text"`
				} `json:"prompt"`
				Meta struct {
					Steering struct {
						IdleBehavior string `json:"idleBehavior"`
					} `json:"steering"`
				} `json:"_meta"`
			}
			if json.Unmarshal(f.Params, &steer) != nil || steer.SessionID != "scripted-session" || len(steer.Prompt) != 1 || steer.Prompt[0].Text != "also this" || steer.Meta.Steering.IdleBehavior != "promptRequired" {
				os.Exit(9)
			}
			send(frame{ID: f.ID, Result: raw(map[string]string{"outcome": "injected"})})
			send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "both done"}}})})
			send(frame{ID: promptID, Result: raw(map[string]string{"stopReason": "end_turn"})})
		case "session/cancel":
			if mode == "compact-wait" {
				// A late native terminal must not erase an external cancellation.
				sendCompaction("own-compaction", "completed")
				send(frame{ID: promptID, Result: raw(map[string]string{"stopReason": "end_turn"})})
				continue
			}
			send(frame{ID: promptID, Result: raw(map[string]string{"stopReason": "cancelled"})})
		case "":
			if string(f.ID) == `"permission-rpc"` {
				send(frame{ID: promptID, Result: raw(map[string]string{"stopReason": "end_turn", "text": "after permission"})})
			}
			if string(f.ID) == `"burst-permission"` {
				send(frame{ID: promptID, Result: raw(map[string]string{"stopReason": "end_turn"})})
			}
		case "fixture/ack":
			send(frame{ID: f.ID, Result: raw(map[string]bool{"ok": true})})
		}
	}
	os.Exit(0)
}

// peerSessionID lets a test choose what native session ID the peer answers.
func peerSessionID(name string) string {
	if id := os.Getenv(name); id != "" {
		return id
	}
	return "scripted-session"
}

func peerConfiguration(t *testing.T, mode string) Configuration {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Configuration{Executable: exe, Arguments: []string{"-test.run=^TestACPTestPeer$"}, Environment: []string{"CLAWDLINE_ACP_TEST_PEER=1", "CLAWDLINE_ACP_TEST_MODE=" + mode, "GORACE=atexit_sleep_ms=0", "CLAWDLINE_ACP_ERROR=" + os.Getenv("CLAWDLINE_ACP_ERROR"), "CLAWDLINE_ACP_PROMPT_LOG=" + os.Getenv("CLAWDLINE_ACP_PROMPT_LOG")}}
}
func openTestClient(t *testing.T, mode string, sink func(context.Context, app.ProtocolEvent) error) (*Client, *Process) {
	t.Helper()
	return openClassifiedTestClient(t, mode, sink, true)
}

// openClassifiedTestClient sets what the payload classifier says about every
// native permission request.
func openClassifiedTestClient(t *testing.T, mode string, sink func(context.Context, app.ProtocolEvent) error, readOnly bool) (*Client, *Process) {
	return openLimitedTestClient(t, mode, sink, readOnly, testLimits())
}

func openLimitedTestClient(t *testing.T, mode string, sink func(context.Context, app.ProtocolEvent) error, readOnly bool, l Limits) (*Client, *Process) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows native supervisor is explicitly unavailable")
	}
	if inventoryUnavailable() {
		t.Skip("process inventory is unavailable; safe startup admission must refuse")
	}
	p, err := StartProcess(context.Background(), ProcessOptions{Configuration: peerConfiguration(t, mode), Directory: t.TempDir(), Limits: l})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(p, l, testFence(), sink, func([]byte) bool { return readOnly })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
		defer cancel()
		if err := c.Close(ctx); err != nil {
			t.Errorf("cleanup: %v", err)
		}
		select {
		case <-p.done:
			p.mu.Lock()
			waitErr := p.waitErr
			p.mu.Unlock()
			if exitErr, ok := waitErr.(*exec.ExitError); ok && exitErr.ExitCode() == 66 {
				t.Errorf("scripted peer race detector failed: %v; %s", waitErr, p.Stderr())
			}
		default:
			t.Error("direct owned process was not reaped")
		}
	})
	q := app.OpenRequest{Directory: t.TempDir()}
	if mode == "provider-claude-terminal-quota" {
		q.Bot.Agent = pa.ClaudeCode
	}
	if err = c.initialize(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	return c, p
}
func sendTestPrompt(t *testing.T, c *Client) app.PendingPrompt {
	t.Helper()
	p, err := c.Send(context.Background(), app.PromptRequest{Turn: pa.Turn{ID: "turn", Fence: testFence(), Instructions: pa.InstructionSnapshot{Text: "fixed"}}, Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestACPStreamsAndFallbackWaitsForIncoming(t *testing.T) {
	var mu sync.Mutex
	var events []app.ProtocolEvent
	c, _ := openTestClient(t, "explicit", func(_ context.Context, e app.ProtocolEvent) error {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
		return nil
	})
	result, err := sendTestPrompt(t, c).Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "partial" {
		t.Fatalf("final=%q", result.FinalText)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || events[0].Message != "explicit" || events[1].Trace == nil {
		t.Fatalf("ordered events=%+v", events)
	}
}

func TestACPIncomingPermissionDoesNotWaitForPrompt(t *testing.T) {
	offers := make(chan pa.PermissionOffer, 1)
	c, _ := openTestClient(t, "permission", func(_ context.Context, e app.ProtocolEvent) error {
		if e.Permission != nil {
			offers <- *e.Permission
		}
		return nil
	})
	pending := sendTestPrompt(t, c)
	var offer pa.PermissionOffer
	select {
	case offer = <-offers:
	case <-time.After(time.Second):
		t.Fatal("permission blocked behind prompt")
	}
	if err := c.Answer(context.Background(), offer.NativeRequestID, "read-once"); err != nil {
		t.Fatal(err)
	}
	result, err := pending.Wait(context.Background())
	if err != nil || result.FinalText != "after permission" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if err = c.Answer(context.Background(), offer.NativeRequestID, "read-once"); pa.Code(err) != pa.NotFound {
		t.Fatal("permission repeated", err)
	}
}

func TestACPCancelAndUnknownEffect(t *testing.T) {
	for _, mode := range []string{"silent", "exit", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			c, _ := openTestClient(t, mode, func(context.Context, app.ProtocolEvent) error { return nil })
			p := sendTestPrompt(t, c)
			if mode == "silent" {
				if err := c.Cancel(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := p.Wait(ctx); err == nil {
				t.Fatal("unknown effect reported successful")
			}
		})
	}
}

// A prompt owns the runtime turn deadline, not the shorter control RPC deadline.
func TestACPPromptOutlivesControlRPCDeadline(t *testing.T) {
	c, _ := openTestClient(t, "delayed-prompt", func(context.Context, app.ProtocolEvent) error { return nil })
	c.l.RPCTimeout = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := sendTestPrompt(t, c).Wait(ctx)
	if err != nil || result.FinalText != "partial" {
		t.Fatalf("prompt was cut short by control RPC deadline: result=%+v err=%v", result, err)
	}
}

func TestACPControlRPCStillTimesOut(t *testing.T) {
	c, _ := openTestClient(t, "silent", func(context.Context, app.ProtocolEvent) error { return nil })
	c.l.RPCTimeout = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c.call(ctx, "fixture/unanswered-control", nil); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatalf("unanswered control RPC: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("control RPC waited for the turn deadline")
	}
}

func TestACPTimeoutDoesNotReplay(t *testing.T) {
	c, _ := openTestClient(t, "silent", func(context.Context, app.ProtocolEvent) error { return nil })
	p := sendTestPrompt(t, c)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*10)
	defer cancel()
	if _, err := p.Wait(ctx); pa.Code(err) != pa.ExecutionEffectUnknown || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, err := c.Send(context.Background(), app.PromptRequest{Turn: pa.Turn{ID: "two", Fence: testFence()}, Text: "retry"}); err == nil {
		t.Fatal("dead client accepted another prompt")
	}
}

func TestACPRejectsStaleGenerationBeforeWrite(t *testing.T) {
	c, _ := openTestClient(t, "silent", func(context.Context, app.ProtocolEvent) error { return nil })
	f := testFence()
	f.Generation++
	if _, err := c.Send(context.Background(), app.PromptRequest{Turn: pa.Turn{ID: "wrong", Fence: f}, Text: "hello"}); pa.Code(err) != pa.StaleGeneration {
		t.Fatal(err)
	}
}

func TestACPQuoteBodyTraversesActualStdinAsSeparateBlock(t *testing.T) {
	c, _ := openTestClient(t, "quote", func(context.Context, app.ProtocolEvent) error { return nil })
	pending, err := c.Send(context.Background(), app.PromptRequest{Turn: pa.Turn{ID: "quote-turn", Fence: testFence()}, Text: "explain this", Quote: &app.PromptQuote{MessageID: "reference-id", Text: "precise referenced body"}})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := pending.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.FinalText, "Referenced message (reference-id):\nprecise referenced body") || !strings.HasSuffix(reply.FinalText, "explain this") {
		t.Fatalf("actual peer payload=%q", reply.FinalText)
	}
}

func TestACPFastCloseAfterInitializeKeepsOwnershipReceipt(t *testing.T) {
	for i := 0; i < 5; i++ {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			c, p := openTestClient(t, "", func(context.Context, app.ProtocolEvent) error { return nil })
			if err := c.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			select {
			case <-p.done:
			default:
				t.Fatal("owned child was not reaped")
			}
		})
	}
}

func processAlive(pid int) bool {
	b, err := exec.Command("/bin/ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
	return err == nil && strings.TrimSpace(string(b)) != "" && !strings.HasPrefix(strings.TrimSpace(string(b)), "Z")
}

func inventoryUnavailable() bool {
	_, err := exec.Command("/bin/ps", "-axo", "pid=,ppid=,lstart=").Output()
	return err != nil
}

func TestACPFreshSessionHandoffTraversesStdinAndRejectsStaleOpeningBeforeWrite(t *testing.T) {
	c, _ := openTestClient(t, "handoff", func(context.Context, app.ProtocolEvent) error { return nil })
	f := testFence()
	memory := pa.MemoryRevision{Key: f.Key, Project: f.Project, Revision: 2}
	filtered := pa.ContextSnapshot{Memory: memory, ProvenanceComplete: true, Text: "Filtered handoff summary", Sources: []pa.MemorySource{{ID: "verified-source", Key: f.Key, Project: f.Project, Revision: 1, Kind: "human"}}}
	turn := pa.Turn{ID: "fresh-turn", Fence: f, MemoryRevision: 2, Instructions: pa.InstructionSnapshot{Revision: 3, Text: "Captured standing instructions", RoleSnapshotRef: "role@3"}}
	handoff := &app.SessionHandoff{Identity: f, Agent: pa.Codex, Instructions: turn.Instructions, Context: filtered, Bookmark: app.SessionBookmark{MessageID: "stored-bookmark", MessageSeq: 42}, Unfinished: []app.SessionPending{{Kind: "work", ID: "old-work", State: "unknown", TaskID: "task-reference"}}}
	stale := *handoff
	stale.Instructions.RoleSnapshotRef = "stale-role"
	if _, err := c.Send(context.Background(), app.PromptRequest{Turn: turn, Context: filtered, Text: "New input", Handoff: &stale}); pa.Code(err) != pa.ContextStale {
		t.Fatal("stale opening entered native prompt", err)
	}
	pending, err := c.Send(context.Background(), app.PromptRequest{Turn: turn, Context: filtered, Text: "New input", Handoff: handoff})
	if err != nil {
		t.Fatal(err)
	}
	result, err := pending.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Filtered handoff summary", "Captured standing instructions", "stored-bookmark", "task-reference", "role@3", "New input"} {
		if !strings.Contains(result.FinalText, text) {
			t.Fatal("actual ACP wire omitted", text)
		}
	}
	if strings.Count(result.FinalText, "Filtered handoff summary") != 1 {
		t.Fatal("handoff memory duplicated")
	}
}

// As in CodyNC, the person may allow an action the host cannot prove
// read-only, once. A persistent rule is still refused.
func TestACPPersonMayAllowAChangingActionOnce(t *testing.T) {
	offers := make(chan pa.PermissionOffer, 1)
	c, _ := openClassifiedTestClient(t, "permission", func(_ context.Context, e app.ProtocolEvent) error {
		if e.Permission != nil {
			offers <- *e.Permission
		}
		return nil
	}, false)
	pending := sendTestPrompt(t, c)
	var offer pa.PermissionOffer
	select {
	case offer = <-offers:
	case <-time.After(time.Second):
		t.Fatal("no permission request")
	}
	if err := c.Answer(context.Background(), offer.NativeRequestID, "read-once"); err != nil {
		t.Fatalf("the person's one-time allow was refused: %v", err)
	}
	result, err := pending.Wait(context.Background())
	if err != nil || result.FinalText != "after permission" {
		t.Fatalf("the allowed action did not continue the turn: %+v %v", result, err)
	}
}

// With native tools available, ACP reports tool calls whose content is an
// array. Such an update is not a chat chunk and must not end the turn.
func TestACPToolCallUpdatesDoNotEndTheTurn(t *testing.T) {
	c, _ := openTestClient(t, "toolcall", func(context.Context, app.ProtocolEvent) error { return nil })
	result, err := sendTestPrompt(t, c).Wait(context.Background())
	if err != nil || result.FinalText != "partial" {
		t.Fatalf("a tool call update ended the turn: %+v %v", result, err)
	}
}

// As in Codync, a native session hears its instructions (system prompt) and
// memory (the fresh session's handoff) once; an ordinary later prompt carries
// only the person's text.
func TestALaterPromptCarriesOnlyThePersonsText(t *testing.T) {
	c, _ := openTestClient(t, "quote", func(context.Context, app.ProtocolEvent) error { return nil })
	turn := pa.Turn{ID: "later-turn", Fence: testFence(), Instructions: pa.InstructionSnapshot{Revision: 1, Text: "STANDING-INSTRUCTIONS"}}
	pending, err := c.Send(context.Background(), app.PromptRequest{Turn: turn, Text: "next question", Context: pa.ContextSnapshot{Text: "SAVED-MEMORY"}, Quote: &app.PromptQuote{MessageID: "q", Text: "quoted"}})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := pending.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(reply.FinalText, "STANDING-INSTRUCTIONS") || strings.Contains(reply.FinalText, "SAVED-MEMORY") || !strings.HasSuffix(reply.FinalText, "next question") {
		t.Fatalf("prompt repeated the snapshot: %q", reply.FinalText)
	}
}

// As in Codync, when memory changed since a resumed session last heard it,
// its next prompt carries all of memory as it is now before the text.
func TestAResumedPromptAfterAMemoryChangeCarriesTheWholeMemory(t *testing.T) {
	c, _ := openTestClient(t, "quote", func(context.Context, app.ProtocolEvent) error { return nil })
	turn := pa.Turn{ID: "changed-turn", Fence: testFence(), Instructions: pa.InstructionSnapshot{Revision: 1, Text: "STANDING-INSTRUCTIONS"}}
	pending, err := c.Send(context.Background(), app.PromptRequest{Turn: turn, Text: "next question", Context: pa.ContextSnapshot{Text: "Has two cats\nLikes tea"}, Quote: &app.PromptQuote{MessageID: "q", Text: "quoted"}, MemoryChanged: true})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := pending.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.FinalText, "<memory_update>\nYour memory changed.") || !strings.Contains(reply.FinalText, "Has two cats\nLikes tea\n</memory_update>\nnext question") || strings.Contains(reply.FinalText, "STANDING-INSTRUCTIONS") {
		t.Fatalf("memory update missing: %q", reply.FinalText)
	}
}

// As Codync keeps text segments apart, a tool call or a new message starts a
// trace segment. Only the last text segment becomes the fallback reply;
// narration and tool-call markup stay in the trace.
func TestReplySegmentsAreSeparatedAndToolMarkupIsDropped(t *testing.T) {
	c, _ := openTestClient(t, "segments", func(context.Context, app.ProtocolEvent) error { return nil })
	result, err := sendTestPrompt(t, c).Wait(context.Background())
	want := "RESUME-CHECK-77 done, see <invoke name=\"x\"></invoke> above"
	if err != nil || result.FinalText != want {
		t.Fatalf("reply=%q %v", result.FinalText, err)
	}
}

// As Codync's working indicator, a tool call's title becomes the Bot's
// current activity, carried as a tool_call trace.
func TestAToolCallTitleIsTheBotsActivity(t *testing.T) {
	var mu sync.Mutex
	var traces []pa.Trace
	c, _ := openTestClient(t, "toolcall", func(_ context.Context, e app.ProtocolEvent) error {
		if e.Trace != nil {
			mu.Lock()
			traces = append(traces, *e.Trace)
			mu.Unlock()
		}
		return nil
	})
	if _, err := sendTestPrompt(t, c).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(traces) == 0 || traces[0].Category != "tool_call" || !strings.HasPrefix(traces[0].Text, "Read notes.md\n") {
		t.Fatalf("traces=%+v", traces)
	}
}

// As Codync hands a queued message to the agent, Send now uses ACP
// _session/steering when the adapter advertises it, asking a turn that already
// ended to say so rather than start one.
func TestSendNowSteersIntoTheRunningTurn(t *testing.T) {
	c, _ := openTestClient(t, "steer", func(context.Context, app.ProtocolEvent) error { return nil })
	pending := sendTestPrompt(t, c)
	outcome, err := c.Steer(context.Background(), "also this")
	if err != nil || outcome != app.SteerJoined {
		t.Fatalf("steer: %q %v", outcome, err)
	}
	result, err := pending.Wait(context.Background())
	if err != nil || result.FinalText != "both done" {
		t.Fatalf("reply=%+v %v", result, err)
	}
	if outcome, err = c.Steer(context.Background(), "after"); err != nil || outcome != app.SteerNext {
		t.Fatalf("a steer after the turn: %q %v", outcome, err)
	}
}

// As Codync, a session is put in the Bot's mode with session/set_mode; Claude's
// own fallback from Auto to Accept edits is kept, and a mode the agent does
// not offer refuses the turn rather than run it in another.
func TestASessionIsPutInTheBotsMode(t *testing.T) {
	c, _ := openTestClient(t, "partial", func(context.Context, app.ProtocolEvent) error { return nil })
	offered := func(ids ...string) func(string) bool {
		return func(id string) bool {
			for _, x := range ids {
				if x == id {
					return true
				}
			}
			return false
		}
	}
	claude := func(mode string) pa.Bot { return pa.Bot{Agent: pa.ClaudeCode, Mode: mode} }
	if err := c.applyMode(context.Background(), claude("plan"), "scripted-session", "default", offered("default", "plan", "auto")); err != nil {
		t.Fatalf("switch to plan: %v", err)
	}
	if err := c.applyMode(context.Background(), claude(""), "scripted-session", "auto", offered("auto")); err != nil {
		t.Fatalf("already in Auto: %v", err)
	}
	if err := c.applyMode(context.Background(), claude(""), "scripted-session", "acceptEdits", offered("default", "acceptEdits", "plan")); err != nil {
		t.Fatalf("Claude's Accept edits fallback for a model without Auto: %v", err)
	}
	if err := c.applyMode(context.Background(), claude("acceptEdits"), "scripted-session", "default", offered("default", "plan")); pa.Code(err) != pa.Unsupported {
		t.Fatalf("a mode the agent does not offer: %v", err)
	}
	if err := c.applyMode(context.Background(), claude("default"), "scripted-session", "plan", offered("default", "plan")); pa.Code(err) != pa.Unsupported {
		t.Fatalf("a refused switch: %v", err)
	}
}

// As in Codync, Always allow hands the agent its own standing rule: the
// option goes to the agent like a one-time answer and the turn carries on.
func TestAlwaysAllowGoesToTheAgent(t *testing.T) {
	offers := make(chan pa.PermissionOffer, 1)
	c, _ := openTestClient(t, "permission", func(_ context.Context, e app.ProtocolEvent) error {
		if e.Permission != nil {
			offers <- *e.Permission
		}
		return nil
	})
	pending := sendTestPrompt(t, c)
	var offer pa.PermissionOffer
	select {
	case offer = <-offers:
	case <-time.After(time.Second):
		t.Fatal("no permission request")
	}
	if err := c.Answer(context.Background(), offer.NativeRequestID, "always"); err != nil {
		t.Fatalf("always allow: %v", err)
	}
	if result, err := pending.Wait(context.Background()); err != nil || result.FinalText != "after permission" {
		t.Fatalf("the turn did not carry on: %+v %v", result, err)
	}
}

func TestACPBurstPreservesSlowSinkOrderAndPermission(t *testing.T) {
	for _, mode := range []string{"burst", "burst-permission", "burst-control"} {
		t.Run(mode, func(t *testing.T) {
			l := testLimits()
			l.Notifications = 256 // The former production count, not a new bound.
			var client *Client
			var chunks []string
			path := filepath.Join(t.TempDir(), "prompts.jsonl")
			t.Setenv("CLAWDLINE_ACP_PROMPT_LOG", path)
			c, _ := openLimitedTestClient(t, mode, func(ctx context.Context, e app.ProtocolEvent) error {
				if e.Permission != nil {
					return client.Answer(ctx, e.Permission.NativeRequestID, "allow")
				}
				if e.Trace != nil {
					if mode == "burst-control" && len(chunks) == 0 {
						if _, err := client.call(ctx, "fixture/ack", nil); err != nil {
							return err
						}
					}
					time.Sleep(time.Millisecond)
					chunks = append(chunks, e.Trace.Text)
				}
				return nil
			}, true, l)
			client = c
			result, err := sendTestPrompt(t, c).Wait(context.Background())
			if err != nil {
				t.Fatalf("burst failed: %v", err)
			}
			var expected strings.Builder
			for i := 0; i < 288; i++ {
				expected.WriteString(fmt.Sprintf("%03d,", i))
			}
			if len(chunks) != 288 || strings.Join(chunks, "") != expected.String() || result.FinalText != expected.String() {
				t.Fatal("lost or reordered notification/final response")
			}
			b, err := os.ReadFile(path)
			if err != nil || strings.Count(string(b), "\n") != 1 {
				t.Fatalf("prompt count=%q err=%v", b, err)
			}
		})
	}
}

func TestACPLoadReplaysHistoryBeforeResponseWithoutCapture(t *testing.T) {
	l := testLimits()
	l.Notifications = 256
	c, _ := openLimitedTestClient(t, "load-burst", func(context.Context, app.ProtocolEvent) error { t.Error("history reached the active sink"); return nil }, true, l)
	if err := c.initialize(context.Background(), app.OpenRequest{Directory: t.TempDir(), Resume: true, Binding: pa.ExecutionBinding{NativeSessionID: "scripted-session"}}); err != nil {
		t.Fatalf("load replay failed: %v", err)
	}
	if c.Usage().CapturedBytes != 0 {
		t.Fatal("historical text captured as current reply")
	}
}

func TestACPNativeTextExceedsFormerLocalCaptureAndFrameCaps(t *testing.T) {
	for _, mode := range []string{"long-stream", "long-frame"} {
		t.Run(mode, func(t *testing.T) {
			c, _ := openTestClient(t, mode, func(context.Context, app.ProtocolEvent) error { return nil })
			result, err := sendTestPrompt(t, c).Wait(context.Background())
			if err != nil || len(result.FinalText) <= 1<<20 || strings.Trim(result.FinalText, "x") != "" {
				t.Fatalf("native text bytes=%d err=%v", len(result.FinalText), err)
			}
		})
	}
}

func TestACPCloseCancelsSlowSinkAndDiscardsQueuedEvents(t *testing.T) {
	entered := make(chan struct{})
	var calls atomic.Int32
	c, _ := openTestClient(t, "burst", func(ctx context.Context, _ app.ProtocolEvent) error {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-ctx.Done()
		return ctx.Err()
	})
	pending := sendTestPrompt(t, c)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("sink did not start")
	}
	deadline := time.Now().Add(time.Second)
	for c.Usage().QueuedNotifications <= 256 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if c.Usage().QueuedNotifications <= 256 {
		t.Fatal("reader did not drain burst while sink was blocked")
	}
	if err := c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := pending.Wait(context.Background()); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatalf("close lost unknown: %v", err)
	}
	if calls.Load() != 1 || c.Usage().QueuedNotifications != 0 {
		t.Fatal("queued events survived close")
	}
}

func TestACPNativeOutputStopsRemainTyped(t *testing.T) {
	for _, reason := range []string{"max_tokens", "max_turn_requests"} {
		t.Run(reason, func(t *testing.T) {
			c, _ := openTestClient(t, "native-stop-"+reason, func(context.Context, app.ProtocolEvent) error { return nil })
			_, err := sendTestPrompt(t, c).Wait(context.Background())
			var failure *pa.Failure
			if !errors.As(err, &failure) || failure.Field != "prompt_"+reason || pa.Code(err) == pa.CapacityFull {
				t.Fatalf("native stop lost: %v", err)
			}
		})
	}
}

func TestConversationTraceKeepsNativeStepsAndTextSegments(t *testing.T) {
	var events []app.ProtocolEvent
	c := &Client{sessionID: "trace-session", turnID: "trace-turn", ctx: context.Background(), sink: func(_ context.Context, e app.ProtocolEvent) error {
		events = append(events, e)
		return nil
	}}
	update := func(kind string, fields map[string]any) {
		t.Helper()
		fields["sessionUpdate"] = kind
		if err := c.handle(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "trace-session", "update": fields})}); err != nil {
			t.Fatal(err)
		}
	}
	chunk := func(kind, text string) {
		update(kind, map[string]any{"content": map[string]string{"type": "text", "text": text}})
	}
	chunk("agent_thought_chunk", "Check ")
	chunk("agent_thought_chunk", "the source.")
	update("tool_call", map[string]any{"toolCallId": "read", "title": "Read source.go", "status": "in_progress", "rawInput": map[string]string{"path": "source.go"}})
	update("tool_call_update", map[string]any{"toolCallId": "read", "status": "completed", "content": []any{map[string]any{"type": "content", "content": map[string]string{"type": "text", "text": "Source found"}}}})
	chunk("agent_message_chunk", "First ")
	chunk("agent_message_chunk", "finding.")
	update("tool_call", map[string]any{"toolCallId": "check", "title": "Check result", "status": "in_progress"})
	chunk("agent_message_chunk", "Final result.")
	if len(events) != 8 {
		t.Fatalf("got %d trace events, want all eight", len(events))
	}
	if events[0].Trace.Category != "agent_thought_chunk" || events[0].Trace.Text+events[1].Trace.Text != "Check the source." || events[0].Trace.ID == "" || events[0].Trace.ID != events[1].Trace.ID {
		t.Fatalf("thought chunks lost: %+v", events)
	}
	tool := events[3].Trace
	if tool.ID != events[2].Trace.ID || !tool.Complete || !strings.Contains(tool.Text, "Read source.go") || !strings.Contains(tool.Text, "Source found") {
		t.Fatalf("tool snapshot lost: %+v", tool)
	}
	if events[4].Trace.Text+events[5].Trace.Text != "First finding." || events[5].Trace.ID != events[4].Trace.ID || events[7].Trace.ID == events[5].Trace.ID {
		t.Fatal("narration segments collapsed")
	}
	if strings.Contains(c.partial, "Check the source") || strings.Contains(c.partial, "Source found") {
		t.Fatal("thought or tool output leaked into fallback reply")
	}
}

func TestNativeTerminalOutputMetadataStaysWithItsTool(t *testing.T) {
	var events []app.ProtocolEvent
	c := &Client{sessionID: "s", turnID: "t", ctx: context.Background(), sink: func(_ context.Context, e app.ProtocolEvent) error { events = append(events, e); return nil }}
	update := func(fields map[string]any) {
		t.Helper()
		if err := c.handle(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "s", "update": fields})}); err != nil {
			t.Fatal(err)
		}
	}
	update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "read", "title": "Read state.txt", "status": "in_progress"})
	for _, text := range []string{"status: ", "READY-FOR-REVIEW-42\n"} {
		update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "read", "_meta": map[string]any{"terminal_output_delta": map[string]string{"terminal_id": "read", "data": text}}})
	}
	update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "read", "status": "completed"})
	got := events[len(events)-1].Trace
	if got.ID != "tool-read" || !got.Complete || !strings.Contains(got.Text, "status: READY-FOR-REVIEW-42") || strings.Count(got.Text, "READY-FOR-REVIEW-42") != 1 {
		t.Fatalf("terminal output lost: %+v", got)
	}
	update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "other", "title": "Another command", "status": "in_progress", "_meta": map[string]any{"terminal_output": map[string]string{"terminal_id": "other", "data": "separate output"}}})
	other := events[len(events)-1].Trace
	if strings.Contains(other.Text, "READY-FOR-REVIEW") || !strings.Contains(other.Text, "separate output") {
		t.Fatalf("tool outputs mixed: %+v", other)
	}
}
