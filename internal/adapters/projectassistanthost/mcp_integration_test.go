package projectassistanthost

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// This peer is a Go subprocess, not an installed vendor adapter. It follows
// the actual session MCP descriptor over loopback and has no daemon token,
// native login, model, or direct access to the host's actor-signing functions.
func TestHostOwnedMCPPeer(t *testing.T) {
	if os.Getenv("CLAWDLINE_HOST_MCP_PEER") != "1" {
		return
	}
	write := func(id json.RawMessage, result any) {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	var endpoint, authorization string
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}}
	request := func(method string, id any, params any) []byte {
		v := map[string]any{"jsonrpc": "2.0", "method": method}
		if id != nil {
			v["id"] = id
		}
		if params != nil {
			v["params"] = params
		}
		body, _ := json.Marshal(v)
		r, e := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(body)))
		if e != nil {
			os.Exit(11)
		}
		r.Header.Set("Authorization", authorization)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("MCP-Protocol-Version", "2025-06-18")
		response, e := client.Do(r)
		if e != nil {
			os.Exit(12)
		}
		b, e := io.ReadAll(response.Body)
		response.Body.Close()
		if e != nil || response.StatusCode >= 400 {
			os.Exit(13)
		}
		return b
	}
	call := func(name string, args any) []byte {
		return request("tools/call", "rpc-transport-only", map[string]any{"name": name, "arguments": args})
	}
	check := func(b []byte, wantError bool) {
		var v struct {
			Result struct {
				IsError bool `json:"isError"`
			}
			Error json.RawMessage
		}
		if json.Unmarshal(b, &v) != nil || len(v.Error) != 0 || v.Result.IsError != wantError {
			os.Exit(14)
		}
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var f struct {
			ID     json.RawMessage
			Method string
			Params json.RawMessage
		}
		if json.Unmarshal(scan.Bytes(), &f) != nil {
			os.Exit(15)
		}
		switch f.Method {
		case "initialize":
			write(f.ID, map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": "@agentclientprotocol/codex-acp", "version": "scripted"}, "agentCapabilities": map[string]any{"loadSession": true, "mcpCapabilities": map[string]bool{"http": true}, "sessionCapabilities": map[string]any{"systemPrompt": map[string]any{}}}})
		case "session/new":
			var p struct {
				MCPServers []struct {
					Type, Name, URL string
					Headers         []struct{ Name, Value string }
				} `json:"mcpServers"`
				SystemPrompt struct{ Content []struct{ Text string } } `json:"systemPrompt"`
			}
			if json.Unmarshal(f.Params, &p) != nil || len(p.MCPServers) != 1 || len(p.MCPServers[0].Headers) != 1 || p.MCPServers[0].Type != "http" || len(p.SystemPrompt.Content) != 1 || p.SystemPrompt.Content[0].Text != "Read only" {
				os.Exit(16)
			}
			endpoint, authorization = p.MCPServers[0].URL, p.MCPServers[0].Headers[0].Value
			write(f.ID, map[string]string{"sessionId": "owned-go-peer"})
		case "session/prompt":
			request("initialize", 1, map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "owned-go-peer", "version": "1"}})
			request("notifications/initialized", nil, nil)
			message := map[string]any{"idempotency_key": "native-visible", "text": "One explicit MCP reply."}
			check(call("send_message", message), false)
			check(call("send_message", message), false)
			message["text"] = "Conflicting text"
			check(call("send_message", message), true)
			work := map[string]any{"idempotency_key": "native-work", "brief": "Change only a.go in its isolated worktree.", "claims": []string{"a.go"}}
			check(call("dispatch_work", work), false)
			check(call("dispatch_work", work), true)
			write(f.ID, map[string]string{"stopReason": "end_turn"})
		}
	}
	os.Exit(0)
}

func TestOwnedMCPReachesRealStoreBrokerAndDurableChatWithoutRegistration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows native supervisor unavailable")
	}
	f := newHostFixture(t)
	f.o.Limits.PromptTimeout = 15 * time.Second
	f.o.Limits.CleanupTimeout = 2 * time.Second
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f.o.Provider = func() (app.Provider, error) {
		return acp.NewProvider(acp.ProviderOptions{Configurations: map[pa.Agent]acp.Configuration{pa.Codex: {Executable: executable, Arguments: []string{"-test.run=^TestHostOwnedMCPPeer$"}, Environment: []string{"CLAWDLINE_HOST_MCP_PEER=1", "GORACE=atexit_sleep_ms=0"}}}, Limits: acp.Limits{RPCPending: 8, IncomingPermissions: 4, Notifications: 32, FrameBytes: 1 << 20, CaptureBytes: 1 << 20, StderrBytes: 1024, OwnedDescendants: 64, WriteTimeout: time.Second, CleanupTimeout: 2 * time.Second, RPCTimeout: 10 * time.Second, DescendantPoll: 10 * time.Millisecond}, Verify: func(_ context.Context, b pa.Bot, _ acp.Configuration) (pa.Capabilities, error) {
			return pa.Capabilities{Key: b.Key, Project: b.Project, Agent: b.Agent, CLIVersion: "scripted", AdapterVersion: "scripted", Evidence: "Go peer only; no vendor admission", ReadOnlyEnforced: true, NativeMemory: pa.NativeMemoryDisabled, NativeMemoryVerified: true, SendMessage: true}, nil
		}})
	}
	f.start(t)
	f.enable(t)
	b := f.create(t, "native-mcp-bot")
	if _, err = f.send(t, b, "one"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	var link pa.WorkLink
	for {
		x, e := f.h.store.Export(context.Background(), b.Key)
		if e != nil {
			t.Fatal(e)
		}
		if len(x.WorkLinks) == 1 && x.Conversation.ActiveTurnID == "" {
			link = x.WorkLinks[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("native MCP did not finish: turns=%+v work=%+v usage=%+v", x.Turns, x.WorkLinks, f.h.Usage())
		}
		time.Sleep(time.Millisecond)
	}
	if f.launcher.calls.Load() != 1 {
		t.Fatal("native work replayed", f.launcher.calls.Load())
	}
	ctx := context.Background()
	r, _, err := f.o.Broker.Record(ctx, link.TaskID)
	if err != nil || r.Worktree == nil || r.Worktree.Path == f.repo {
		t.Fatal("isolated broker work missing", r, err)
	}
	if err = os.WriteFile(filepath.Join(r.Worktree.Path, "a.go"), []byte("package a\n// MCP fixture delivery\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, r.Worktree.Path, "add", "a.go")
	fixtureGit(t, r.Worktree.Path, "commit", "-q", "-m", "Deliver one isolated MCP fixture change")
	if _, err = f.o.Broker.Settle(ctx, r.ID, orchestrator.StateSuccess, "", &taskdir.Result{Status: "success", Summary: "One durable MCP work result."}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.h.RecoverWork(ctx, b.Key, r.ID, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.h.RecoverWork(ctx, b.Key, r.ID, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err = f.h.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = f.h.RecoverWork(ctx, b.Key, r.ID, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	x, err := f.h.store.Export(ctx, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	var messages, results int
	for _, m := range x.Messages {
		switch m.Kind {
		case pa.AssistantMessage:
			messages++
			if m.Text != "One explicit MCP reply." {
				t.Fatal(m)
			}
		case pa.WorkMessage:
			results++
			if m.Text != "One durable MCP work result." {
				t.Fatal(m)
			}
		}
	}
	if messages != 1 || results != 1 || len(x.WorkLinks) != 1 || f.launcher.calls.Load() != 1 {
		t.Fatal("durable native MCP projection duplicated", messages, results, len(x.WorkLinks), f.launcher.calls.Load())
	}
	found := false
	for _, receipt := range x.Receipts {
		if receipt.Command.IdempotencyKey == "native-work" {
			found = true
			if receipt.State != pa.EffectUnknown {
				t.Fatal("dispatch effect uncertainty erased", receipt)
			}
		}
	}
	if !found {
		t.Fatal("native dispatch receipt missing")
	}
	fmt.Println("Owned Go ACP peer -> HTTP MCP -> Host -> real SQLite/Broker -> isolated worktree -> one durable chat result; vendor/model requests: 0")
}
