package projectassistantacp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type mcpTestTools struct {
	message  func(context.Context, string, string) error
	dispatch func(context.Context, string, string, []string) (pa.WorkLink, error)
	status   func(context.Context, string) (pa.WorkLink, error)
}

func (t mcpTestTools) SendMessage(ctx context.Context, id, text string) error {
	return t.message(ctx, id, text)
}
func (t mcpTestTools) Dispatch(ctx context.Context, id, brief string, claims []string) (pa.WorkLink, error) {
	return t.dispatch(ctx, id, brief, claims)
}
func (t mcpTestTools) Status(ctx context.Context, id string) (pa.WorkLink, error) {
	return t.status(ctx, id)
}

type mcpFixture struct {
	c     *Client
	lease OwnedMCP
	http  *http.Client
}

func newMCPFixture(t *testing.T, tools app.NativeTools) *mcpFixture {
	t.Helper()
	l := testLimits()
	c := &Client{l: l, rpcSlots: make(chan struct{}, l.RPCPending)}
	lease, err := c.openMCP(context.Background(), app.OpenRequest{Binding: pa.ExecutionBinding{Fence: testFence()}, Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	f := &mcpFixture{c: c, lease: lease, http: &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := lease.Revoke(ctx); err != nil {
			t.Error(err)
		}
		f.http.CloseIdleConnections()
	})
	return f
}
func (f *mcpFixture) post(t *testing.T, body string, change func(*http.Request)) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, f.lease.URL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+f.lease.BearerToken)
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	if change != nil {
		change(req)
	}
	r, err := f.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return r.StatusCode, b
}
func (f *mcpFixture) initialize(t *testing.T) {
	t.Helper()
	code, b := f.post(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"scripted","version":"1"}}}`, nil)
	if code != 200 || !bytes.Contains(b, []byte(`"protocolVersion":"2025-06-18"`)) {
		t.Fatalf("initialize: %d %s", code, b)
	}
	code, b = f.post(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil)
	if code != 202 || len(b) != 0 {
		t.Fatalf("ready: %d %s", code, b)
	}
}
func mcpCall(name string, args any) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "transport-id", "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
	return string(b)
}
func TestOwnedMCPRoutesOnlyAuthenticatedExecutionTools(t *testing.T) {
	var sent, dispatched, read atomic.Int32
	f := newMCPFixture(t, mcpTestTools{
		message: func(_ context.Context, id, text string) error {
			if id != "stable-message" || text != "Visible reply" {
				t.Error(id, text)
			}
			sent.Add(1)
			return nil
		},
		dispatch: func(_ context.Context, id, brief string, claims []string) (pa.WorkLink, error) {
			if id != "stable-work" || brief != "Change one file" || len(claims) != 1 || claims[0] != "one.go" {
				t.Error(id, brief, claims)
			}
			dispatched.Add(1)
			return pa.WorkLink{TaskID: "owned-task"}, nil
		},
		status: func(_ context.Context, id string) (pa.WorkLink, error) {
			read.Add(1)
			return pa.WorkLink{TaskID: id}, nil
		},
	})
	message := mcpCall("send_message", map[string]any{"idempotency_key": "stable-message", "text": "Visible reply"})
	_, b := f.post(t, message, nil)
	if !bytes.Contains(b, []byte("mcp_not_initialized")) && !bytes.Contains(b, []byte(string(pa.Invalid))) {
		t.Fatal(string(b))
	}
	if sent.Load() != 0 {
		t.Fatal("tool before initialization")
	}
	f.initialize(t)
	for _, change := range []func(*http.Request){
		func(r *http.Request) { r.Header.Del("Authorization") },
		func(r *http.Request) { r.Header.Set("Authorization", "Bearer foreign") },
		func(r *http.Request) { r.Header.Add("Authorization", "Bearer foreign") },
		func(r *http.Request) { r.Header.Set("Origin", "https://foreign.example") },
		func(r *http.Request) { r.Host = "foreign.example" },
		func(r *http.Request) { r.Header.Set("MCP-Protocol-Version", "wrong") },
	} {
		code, _ := f.post(t, message, change)
		if code < 400 {
			t.Fatal("foreign authority accepted", code)
		}
	}
	if sent.Load() != 0 {
		t.Fatal("authority rejection had effects")
	}
	for _, body := range []string{message, mcpCall("dispatch_work", map[string]any{"idempotency_key": "stable-work", "brief": "Change one file", "claims": []string{"one.go"}}), mcpCall("work_status", map[string]any{"task_id": "owned-task"})} {
		code, b := f.post(t, body, nil)
		if code != 200 || !bytes.Contains(b, []byte(`"isError":false`)) {
			t.Fatalf("tool %d %s", code, b)
		}
	}
	if sent.Load() != 1 || dispatched.Load() != 1 || read.Load() != 1 {
		t.Fatal("tool routing", sent.Load(), dispatched.Load(), read.Load())
	}
	_, b = f.post(t, `{"jsonrpc":"2.0","id":4,"method":"tools/list"}`, nil)
	if !bytes.Contains(b, []byte(`"send_message"`)) || bytes.Contains(b, []byte(f.lease.BearerToken)) || bytes.Contains(b, []byte(`"shell"`)) {
		t.Fatal("tool catalog leaked authority", string(b))
	}
	for _, body := range []string{
		mcpCall("shell", map[string]any{"command": "echo unsafe"}),
		mcpCall("send_message", map[string]any{"idempotency_key": "new", "text": "unsafe", "actor": testFence()}),
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"send_message","arguments":{"idempotency_key":"new","text":"a","text":"b"}}}`,
	} {
		_, b = f.post(t, body, nil)
		if bytes.Contains(b, []byte(`"isError":false`)) {
			t.Fatal("invalid operation accepted", string(b))
		}
	}
	if sent.Load() != 1 || dispatched.Load() != 1 {
		t.Fatal("invalid operation had effect")
	}
	code, _ := f.post(t, strings.Repeat("x", f.c.l.FrameBytes+1), nil)
	if code != 413 {
		t.Fatal("oversized frame", code)
	}
	if err := f.lease.Revoke(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err := f.http.Post(f.lease.URL, "application/json", strings.NewReader(message))
	if err == nil {
		t.Fatal("listener survived revocation")
	}
}

func TestOwnedMCPUnknownToolEffectIsTypedAndNeverRetried(t *testing.T) {
	var calls atomic.Int32
	f := newMCPFixture(t, mcpTestTools{dispatch: func(context.Context, string, string, []string) (pa.WorkLink, error) {
		calls.Add(1)
		return pa.WorkLink{}, refusal(pa.OutcomeUnknown, "broker")
	}})
	f.initialize(t)
	code, b := f.post(t, mcpCall("dispatch_work", map[string]any{"idempotency_key": "stable", "brief": "Change", "claims": []string{"one.go"}}), nil)
	if code != 200 || !bytes.Contains(b, []byte(`"isError":true`)) || !bytes.Contains(b, []byte(string(pa.OutcomeUnknown))) || calls.Load() != 1 {
		t.Fatalf("unknown effect %d %s calls %d", code, b, calls.Load())
	}
}

func TestOwnedMCPCleanupRetainsSharedSlotUntilCallbackExits(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	f := newMCPFixture(t, mcpTestTools{message: func(ctx context.Context, _, _ string) error { close(started); <-release; return ctx.Err() }})
	f.initialize(t)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		req, _ := http.NewRequest(http.MethodPost, f.lease.URL, strings.NewReader(mcpCall("send_message", map[string]any{"idempotency_key": "stable", "text": "waiting"})))
		req.Header.Set("Authorization", "Bearer "+f.lease.BearerToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("MCP-Protocol-Version", "2025-06-18")
		r, e := f.http.Do(req)
		if e == nil {
			r.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("tool did not enter")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.lease.Revoke(ctx); pa.Code(err) != pa.ExecutionEffectUnknown {
		close(release)
		t.Fatal("cleanup detached effect", err)
	}
	if len(f.c.rpcSlots) != 1 {
		close(release)
		t.Fatal("cleanup dropped occupied slot", len(f.c.rpcSlots))
	}
	close(release)
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := f.lease.Revoke(ctx); err != nil {
		t.Fatal(err)
	}
	<-finished
	if len(f.c.rpcSlots) != 0 {
		t.Fatal("verified cleanup retained slot")
	}
}

func TestOwnedMCPAndACPShareRegisteredRPCBudget(t *testing.T) {
	f := newMCPFixture(t, mcpTestTools{})
	for range f.c.l.RPCPending {
		f.c.rpcSlots <- struct{}{}
	}
	code, b := f.post(t, `{}`, nil)
	if code != 503 || !bytes.Contains(b, []byte(string(pa.CapacityFull))) {
		t.Fatal("shared budget", code, string(b))
	}
	for range f.c.l.RPCPending {
		<-f.c.rpcSlots
	}
	f.initialize(t)
}

func TestOwnedMCPAcceptsOnlyLoopbackDescriptor(t *testing.T) {
	good := OwnedMCP{Fence: testFence(), URL: "http://127.0.0.1:12345/mcp", BearerToken: "owned", Revoke: func(context.Context) error { return nil }}
	if err := good.validate(testFence()); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"https://127.0.0.1:12345/mcp", "http://localhost:12345/mcp", "http://192.0.2.1:12345/mcp", "http://user@127.0.0.1:12345/mcp", "http://127.0.0.1/mcp", "http://127.0.0.1:12345/mcp?token=hidden"} {
		bad := good
		bad.URL = endpoint
		if err := bad.validate(testFence()); err == nil {
			t.Fatal("unowned endpoint", endpoint)
		}
	}
	bad := good
	bad.Fence.Generation++
	if err := bad.validate(testFence()); pa.Code(err) != pa.StaleGeneration {
		t.Fatal(err)
	}
	bad = good
	bad.BearerToken = "owned\r\nX-Other: value"
	if err := bad.validate(testFence()); err == nil {
		t.Fatal("header injection")
	}
}
