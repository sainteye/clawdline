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
	dispatch func(context.Context, string, string, string, []string) (pa.WorkLink, error)
	status   func(context.Context, string) (pa.WorkLink, error)
	memory   []pa.MemoryEntry
	edits    *[]app.MemoryEdit
}

func (t mcpTestTools) SendMessage(ctx context.Context, id, text string) error {
	return t.message(ctx, id, text)
}
func (t mcpTestTools) Dispatch(ctx context.Context, id, title, brief string, claims []string) (pa.WorkLink, error) {
	return t.dispatch(ctx, id, title, brief, claims)
}
func (t mcpTestTools) Status(ctx context.Context, id string) (pa.WorkLink, error) {
	return t.status(ctx, id)
}
func (t mcpTestTools) Memory(context.Context) ([]pa.MemoryEntry, error) { return t.memory, nil }
func (t mcpTestTools) EditMemory(_ context.Context, _ string, e app.MemoryEdit) error {
	if t.edits == nil {
		return refusal(pa.Disabled, "native_memory")
	}
	*t.edits = append(*t.edits, e)
	return nil
}

type mcpFixture struct {
	c     *Client
	lease OwnedMCP
	http  *http.Client
}

func newMCPFixture(t *testing.T, tools app.NativeTools) *mcpFixture {
	t.Helper()
	return newMCPFixtureWithContext(t, tools, context.Background(), testLimits())
}

func newMCPFixtureWithContext(t *testing.T, tools app.NativeTools, parent context.Context, l Limits) *mcpFixture {
	t.Helper()
	c := &Client{l: l, rpcSlots: make(chan struct{}, l.RPCPending)}
	lease, err := c.openMCP(parent, app.OpenRequest{Binding: pa.ExecutionBinding{Fence: testFence()}, Tools: tools})
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
		dispatch: func(_ context.Context, id, title, brief string, claims []string) (pa.WorkLink, error) {
			if id != "stable-work" || title != "Keep drafts after reconnecting" || brief != "Change one file" || len(claims) != 1 || claims[0] != "one.go" {
				t.Error(id, title, brief, claims)
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
	for _, body := range []string{message, mcpCall("dispatch_work", map[string]any{"idempotency_key": "stable-work", "title": "Keep drafts after reconnecting", "brief": "Change one file", "claims": []string{"one.go"}}), mcpCall("work_status", map[string]any{"task_id": "owned-task"})} {
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
	f := newMCPFixture(t, mcpTestTools{dispatch: func(context.Context, string, string, string, []string) (pa.WorkLink, error) {
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

// As in Codync's memory server, a Bot reads, searches and edits its memory
// through its own tools; an edit reaches the host with its idempotency key.
func TestOwnedMCPMemoryToolsReadSearchAndEdit(t *testing.T) {
	var edits []app.MemoryEdit
	f := newMCPFixture(t, mcpTestTools{memory: []pa.MemoryEntry{{ID: "e1", Text: "Has two cats"}, {ID: "e2", Text: "Likes green tea"}}, edits: &edits})
	f.initialize(t)
	_, b := f.post(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, nil)
	for _, name := range []string{"read_memory", "search_memory", "edit_memory"} {
		if !bytes.Contains(b, []byte(`"name":"`+name+`"`)) {
			t.Fatalf("%s not listed: %s", name, b)
		}
	}
	if _, b = f.post(t, mcpCall("read_memory", nil), nil); !bytes.Contains(b, []byte("Has two cats")) || !bytes.Contains(b, []byte("Likes green tea")) || bytes.Contains(b, []byte(`"isError":true`)) {
		t.Fatalf("read_memory: %s", b)
	}
	if _, b = f.post(t, mcpCall("search_memory", map[string]any{"query": "TEA green"}), nil); !bytes.Contains(b, []byte("Likes green tea")) || bytes.Contains(b, []byte("Has two cats")) {
		t.Fatalf("search_memory: %s", b)
	}
	if _, b = f.post(t, mcpCall("edit_memory", map[string]any{"idempotency_key": "k1", "old": "two cats", "new": "three cats"}), nil); bytes.Contains(b, []byte(`"isError":true`)) {
		t.Fatalf("edit_memory: %s", b)
	}
	if _, b = f.post(t, mcpCall("edit_memory", map[string]any{"idempotency_key": "k2"}), nil); !bytes.Contains(b, []byte(`"isError":true`)) {
		t.Fatalf("an edit with nothing to change was accepted: %s", b)
	}
	if len(edits) != 1 || edits[0].Old != "two cats" || edits[0].New != "three cats" {
		t.Fatalf("edits=%+v", edits)
	}
}

type topicDetailTestTools struct {
	mcpTestTools
	calls *[]string
}

func (t topicDetailTestTools) MemoryDetail(_ context.Context, id string) (pa.MemoryDetail, error) {
	*t.calls = append(*t.calls, id)
	if id != "topic" {
		return pa.MemoryDetail{}, refusal(pa.NotFound, "memory_entry")
	}
	return pa.MemoryDetail{Layer: "bot", ID: id, Kind: pa.TopicMemory, Title: "Release checklist", Summary: "Readiness", Text: "BODY-ONLY-TOPIC-DETAIL", Revision: "2"}, nil
}
func TestOwnedMCPTopicMemoryIndexAndExplicitBodyRead(t *testing.T) {
	calls := []string{}
	f := newMCPFixture(t, topicDetailTestTools{mcpTestTools: mcpTestTools{memory: []pa.MemoryEntry{{ID: "topic", Kind: pa.TopicMemory, Title: "Release checklist", Text: "Readiness"}}}, calls: &calls})
	f.initialize(t)
	_, body := f.post(t, mcpCall("read_memory", nil), nil)
	if bytes.Contains(body, []byte("BODY-ONLY")) || !bytes.Contains(body, []byte("Release checklist")) || len(calls) != 0 {
		t.Fatal("index read fetched or exposed a body", string(body), calls)
	}
	_, body = f.post(t, mcpCall("read_memory", map[string]any{"entry_id": "topic"}), nil)
	if !bytes.Contains(body, []byte("BODY-ONLY-TOPIC-DETAIL")) || bytes.Contains(body, []byte(`"isError":true`)) {
		t.Fatal("explicit note read failed", string(body))
	}
	_, body = f.post(t, mcpCall("read_memory", map[string]any{"entry_id": "foreign"}), nil)
	if !bytes.Contains(body, []byte(string(pa.NotFound))) {
		t.Fatal("unknown ID appeared readable", string(body))
	}
	_, body = f.post(t, mcpCall("read_memory", map[string]any{"entry_id": "topic", "bot_id": "foreign"}), nil)
	if !bytes.Contains(body, []byte(`"isError":true`)) || len(calls) != 2 {
		t.Fatal("client-selected owner reached note reader", string(body), calls)
	}
}

func TestOwnedMCPTopicNoteEditUsesRevisionAndRejectsMixedAuthority(t *testing.T) {
	var edits []app.MemoryEdit
	f := newMCPFixture(t, mcpTestTools{edits: &edits})
	f.initialize(t)
	note := map[string]any{"title": "Release checklist", "summary": "Readiness", "body": "Full synthetic checklist", "expected_revision": 1}
	_, body := f.post(t, mcpCall("edit_memory", map[string]any{"idempotency_key": "topic-create", "note": note}), nil)
	if bytes.Contains(body, []byte(`"isError":true`)) || len(edits) != 1 {
		t.Fatal("revisioned topic note did not reach its owned tool", string(body), edits)
	}
	if edits[0].Note == nil || edits[0].Note.Body != "Full synthetic checklist" || edits[0].Note.ExpectedRevision != 1 {
		t.Fatal("note edit fields lost", edits)
	}
	for name, args := range map[string]map[string]any{
		"mixed":            {"idempotency_key": "mixed", "note": note, "append": "A short fact"},
		"owner override":   {"idempotency_key": "foreign", "note": map[string]any{"bot_id": "other", "expected_revision": 1}},
		"missing revision": {"idempotency_key": "missing", "note": map[string]any{"title": "Title", "summary": "Index", "body": "Body"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, body := f.post(t, mcpCall("edit_memory", args), nil)
			if !bytes.Contains(body, []byte(`"isError":true`)) || len(edits) != 1 {
				t.Fatal("invalid edit reached native tool", string(body), edits)
			}
		})
	}
}

type topicIndexTestTools struct{ mcpTestTools }

func (t topicIndexTestTools) MemoryIndex(context.Context) (app.MemoryIndex, error) {
	return app.MemoryIndex{Revision: 7, Entries: t.memory}, nil
}
func TestOwnedMCPTopicIndexCarriesItsCASRevision(t *testing.T) {
	f := newMCPFixture(t, topicIndexTestTools{mcpTestTools{memory: []pa.MemoryEntry{{ID: "topic", Kind: pa.TopicMemory, Title: "Checklist", Text: "Readiness"}}}})
	f.initialize(t)
	_, body := f.post(t, mcpCall("read_memory", nil), nil)
	if !bytes.Contains(body, []byte(`\"revision\":7`)) || !bytes.Contains(body, []byte("Checklist")) {
		t.Fatal("index revision absent", string(body))
	}
}

// Antigravity sends notifications/roots/list_changed after the handshake; a
// 400 made it drop the whole server, so its Gemini Bot had no Clawdline tools.
func TestOwnedMCPAcceptsClientNotificationsItDoesNotActOn(t *testing.T) {
	f := newMCPFixture(t, mcpTestTools{})
	f.initialize(t)
	for _, method := range []string{"notifications/roots/list_changed", "notifications/cancelled", "notifications/progress"} {
		if code, b := f.post(t, `{"jsonrpc":"2.0","method":"`+method+`"}`, nil); code != 202 || len(b) != 0 {
			t.Fatalf("%s: %d %s", method, code, b)
		}
	}
	if code, _ := f.post(t, `{"jsonrpc":"2.0","method":"tools/list"}`, nil); code != 400 {
		t.Fatalf("a request without an id is still refused: %d", code)
	}
	if code, b := f.post(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, nil); code != 200 || !strings.Contains(string(b), `"send_file"`) {
		t.Fatalf("tools after the notification: %d %s", code, b)
	}
}

type leadControlTestTools struct {
	mcpTestTools
	allowed atomic.Bool
	calls   atomic.Int32
}

func (t *leadControlTestTools) LeadBotAvailable(context.Context) error {
	if !t.allowed.Load() {
		return refusal(pa.ScopeDenied, "lead_role")
	}
	return nil
}

func (t *leadControlTestTools) ManageBots(ctx context.Context, q app.LeadBotRequest) (json.RawMessage, error) {
	if err := t.LeadBotAvailable(ctx); err != nil {
		return nil, err
	}
	if q.Method != "PATCH" || q.Path != "/target" || q.IdempotencyKey != "rename-once" {
		return nil, refusal(pa.Invalid, "test_arguments")
	}
	t.calls.Add(1)
	return json.RawMessage(`{"result":"renamed"}`), nil
}

func TestOwnedMCPLeadControlFollowsTheLiveRole(t *testing.T) {
	tools := &leadControlTestTools{}
	tools.allowed.Store(true)
	f := newMCPFixture(t, tools)
	f.initialize(t)
	const list = `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	if code, b := f.post(t, list, nil); code != 200 || !bytes.Contains(b, []byte(`"name":"manage_bots"`)) {
		t.Fatalf("Lead control discovery: %d %s", code, b)
	}
	args := map[string]any{"method": "PATCH", "path": "/target", "idempotency_key": "rename-once", "body": map[string]any{"settings": map[string]string{"name": "Renamed"}}}
	if code, b := f.post(t, mcpCall("manage_bots", args), nil); code != 200 || !bytes.Contains(b, []byte(`renamed`)) {
		t.Fatalf("Lead control: %d %s", code, b)
	}
	tools.allowed.Store(false)
	if code, b := f.post(t, list, nil); code != 200 || bytes.Contains(b, []byte(`"name":"manage_bots"`)) {
		t.Fatalf("Former Lead tool discovery: %d %s", code, b)
	}
	if code, b := f.post(t, mcpCall("manage_bots", args), nil); code != 200 || !bytes.Contains(b, []byte(`pa_scope_denied`)) || !bytes.Contains(b, []byte(`lead_role`)) || tools.calls.Load() != 1 {
		t.Fatalf("Former Lead could reuse the tool: %d %s", code, b)
	}
}

type completionMCPTools struct {
	mcpTestTools
	record func(id, title, brief string, claims []string, project, persona, boardItemID string) pa.WorkLink
}

func (t completionMCPTools) DispatchWithCompletion(_ context.Context, id, title, brief string, claims []string, project, persona, boardItemID string) (pa.WorkLink, error) {
	return t.record(id, title, brief, claims, project, persona, boardItemID), nil
}

func TestMCPDispatchRoutesReadOnlyWorkWithoutAProject(t *testing.T) {
	f := newMCPFixture(t, mcpTestTools{dispatch: func(_ context.Context, _, _, _ string, claims []string) (pa.WorkLink, error) {
		if claims == nil || len(claims) != 0 {
			t.Errorf("expected an explicit empty claims array, got %#v", claims)
		}
		return pa.WorkLink{TaskID: "read-only-task"}, nil
	}})
	f.initialize(t)
	code, body := f.post(t, mcpCall("dispatch_work", map[string]any{
		"idempotency_key": "read-only", "title": "Inspect the result", "brief": "Read only.", "claims": []string{},
	}), nil)
	if code != http.StatusOK || !bytes.Contains(body, []byte(`\"task_id\":\"read-only-task\"`)) {
		t.Fatalf("read-only dispatch response: %d %s", code, body)
	}
}

func TestMCPDispatchRefusalNamesMissingClaims(t *testing.T) {
	f := newMCPFixture(t, mcpTestTools{})
	f.initialize(t)
	code, body := f.post(t, mcpCall("dispatch_work", map[string]any{
		"idempotency_key": "read-only", "title": "Inspect the result", "brief": "Read only.",
	}), nil)
	if code != http.StatusOK || !bytes.Contains(body, []byte(`"isError":true`)) ||
		!bytes.Contains(body, []byte(`\"code\":\"pa_invalid\"`)) || !bytes.Contains(body, []byte(`\"reason\":\"Add claims`)) {
		t.Fatalf("dispatch refusal did not name the missing field: %d %s", code, body)
	}
}

func TestMCPDispatchRequestsCompletionForReadOnlyWork(t *testing.T) {
	var calls atomic.Int64
	f := newMCPFixture(t, completionMCPTools{record: func(id, title, brief string, claims []string, project, persona, boardItemID string) pa.WorkLink {
		calls.Add(1)
		if id != "dispatch-1" || title != "Wait for checks" || brief != "Run the checks without editing" || claims == nil || len(claims) != 0 ||
			project != "" || persona != "reviewer" || boardItemID != "" {
			t.Errorf("unexpected callback dispatch: %q %q %q %v %q %q %q", id, title, brief, claims, project, persona, boardItemID)
		}
		return pa.WorkLink{TaskID: "one", State: pa.WorkAccepted}
	}})
	f.initialize(t)
	code, body := f.post(t, mcpCall("dispatch_work", map[string]any{
		"idempotency_key": "dispatch-1", "title": "Wait for checks", "brief": "Run the checks without editing",
		"claims": []string{}, "persona": "reviewer", "continue_on_completion": true,
	}), nil)
	if code != http.StatusOK || calls.Load() != 1 || !bytes.Contains(body, []byte(`\"continue_on_completion\":true`)) || !bytes.Contains(body, []byte(`\"task_id\":\"one\"`)) {
		t.Fatalf("callback admission response: %d %s", code, body)
	}
}
