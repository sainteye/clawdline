package projectassistantacp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type mcpServerFixtureTools struct {
	mcpTestTools
	available   error
	calls       []app.DevServerRequest
	publication error
	call        func(context.Context, app.DevServerRequest) (app.DevServerResult, error)
}

type fencedServerWriter struct {
	header         http.Header
	inside         *bool
	wrote, flushed bool
}

func (w *fencedServerWriter) Header() http.Header              { return w.header }
func (w *fencedServerWriter) WriteHeader(int)                  {}
func (w *fencedServerWriter) SetWriteDeadline(time.Time) error { return nil }
func (w *fencedServerWriter) Write(b []byte) (int, error) {
	if !*w.inside {
		return 0, refusal(pa.ContextStale, "write_outside_fence")
	}
	w.wrote = true
	return len(b), nil
}
func (w *fencedServerWriter) FlushError() error {
	if !*w.inside {
		return refusal(pa.ContextStale, "flush_outside_fence")
	}
	w.flushed = true
	return nil
}

func TestMCPServerFlushesBufferedHTTPContentInsideThePublicationFence(t *testing.T) {
	inside := false
	writer := &fencedServerWriter{header: http.Header{}, inside: &inside}
	s := &ownedMCPServer{limits: testLimits()}
	s.writeServerReply(context.Background(), writer, frame{ID: json.RawMessage(`1`)}, []byte(`{"synthetic":"body"}`), serverMCPReply{publish: func(_ context.Context, write func() error) error {
		inside = true
		defer func() { inside = false }()
		return write()
	}})
	if !writer.wrote || !writer.flushed {
		t.Fatal("HTTP buffering escaped the source fence", writer.wrote, writer.flushed)
	}
}

func (f *mcpServerFixtureTools) DevServersAvailable(context.Context) error { return f.available }
func (f *mcpServerFixtureTools) DevServers(ctx context.Context, q app.DevServerRequest) (app.DevServerResult, error) {
	f.calls = append(f.calls, q)
	if f.call != nil {
		return f.call(ctx, q)
	}
	return app.DevServerResult{Servers: []app.DevServerInfo{{ID: "owned", State: "running"}}}, nil
}

func (f *mcpServerFixtureTools) PublishDevServers(_ context.Context, publish func() error) error {
	if f.publication != nil {
		return f.publication
	}
	return publish()
}

func TestOwnedMCPServerToolsRequireSourceCapabilityAndNeverAcceptIdentityOrApproval(t *testing.T) {
	tools := &mcpServerFixtureTools{}
	f := newMCPFixture(t, tools)
	f.initialize(t)
	for _, tc := range []struct {
		name   string
		args   any
		action string
	}{
		{"start_dev_server", map[string]any{"idempotency_key": "start", "command": "synthetic --foreground", "folder": "app"}, "start"},
		{"list_dev_servers", map[string]any{}, "list"},
		{"stop_dev_server", map[string]any{"idempotency_key": "stop", "id": "owned"}, "stop"},
		{"dev_server_logs", map[string]any{"id": "owned", "lines": 2}, "logs"},
	} {
		status, raw := f.post(t, mcpCall(tc.name, tc.args), nil)
		if status != 200 || strings.Contains(string(raw), `"isError":true`) || len(tools.calls) == 0 || tools.calls[len(tools.calls)-1].Action != tc.action {
			t.Fatal(status, string(raw), tools.calls)
		}
	}
	before := len(tools.calls)
	for _, field := range []string{"bot_id", "machine_id", "project", "environment", "pid", "approval", "action"} {
		status, raw := f.post(t, mcpCall("start_dev_server", map[string]any{"command": "synthetic", "idempotency_key": "forged", field: "foreign"}), nil)
		if status != 200 || !strings.Contains(string(raw), `"isError":true`) || len(tools.calls) != before {
			t.Fatal("authority reached source tool", field, status, string(raw))
		}
	}
	status, raw := f.post(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, nil)
	if status != 200 {
		t.Fatal(status, string(raw))
	}
	var list struct {
		Result struct {
			Tools []struct {
				Name        string
				InputSchema json.RawMessage
			}
		}
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range list.Result.Tools {
		names[tool.Name] = true
	}
	for _, name := range []string{"start_dev_server", "list_dev_servers", "stop_dev_server", "dev_server_logs"} {
		if !names[name] {
			t.Fatal("missing source tool", name)
		}
	}
	tools.available = refusal(pa.Revoked, "source")
	_, raw = f.post(t, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`, nil)
	if strings.Contains(string(raw), `"start_dev_server"`) {
		t.Fatal("revoked capability advertised")
	}
	_, raw = f.post(t, mcpCall("start_dev_server", map[string]any{"command": "synthetic", "idempotency_key": "revoked"}), nil)
	if !strings.Contains(string(raw), `"isError":true`) || len(tools.calls) != before {
		t.Fatal("revoked source called", string(raw))
	}
}

func TestMCPServerPublicationRechecksAfterToolBeforeWritingAnyContent(t *testing.T) {
	tools := &mcpServerFixtureTools{publication: refusal(pa.ContextStale, "synthetic_forget"), call: func(context.Context, app.DevServerRequest) (app.DevServerResult, error) {
		return app.DevServerResult{Server: &app.DevServerInfo{Command: "synthetic private command must not escape"}}, nil
	}}
	f := newMCPFixture(t, tools)
	f.initialize(t)
	status, raw := f.post(t, mcpCall("list_dev_servers", map[string]any{}), nil)
	if status != 200 || !strings.Contains(string(raw), string(pa.ContextStale)) || strings.Contains(string(raw), "must not escape") || !strings.Contains(string(raw), `"isError":true`) {
		t.Fatal("stale body reached native transport", status, string(raw))
	}
}

func TestMCPServerReviewUsesTheActualExecutionDeadlineInsteadOfOrdinaryRPCBudget(t *testing.T) {
	tools := &mcpServerFixtureTools{call: func(ctx context.Context, _ app.DevServerRequest) (app.DevServerResult, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) < 100*time.Millisecond {
			return app.DevServerResult{}, refusal(pa.Invalid, "review_cut_off_by_rpc")
		}
		return app.DevServerResult{}, nil
	}}
	limits := testLimits()
	limits.RPCTimeout = 10 * time.Millisecond
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	f := newMCPFixtureWithContext(t, tools, parent, limits)
	f.initialize(t)
	status, raw := f.post(t, mcpCall("start_dev_server", map[string]any{"command": "synthetic", "idempotency_key": "review"}), nil)
	if status != 200 || strings.Contains(string(raw), `"isError":true`) {
		t.Fatal("source review used short RPC budget", status, string(raw))
	}
}
