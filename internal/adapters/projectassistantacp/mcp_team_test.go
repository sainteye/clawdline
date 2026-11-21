package projectassistantacp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type mcpTeamFixtureTools struct {
	mcpTestTools
	enabled   atomic.Bool
	calls     atomic.Int32
	wait      time.Duration
	decision  <-chan struct{}
	cancelled chan<- struct{}
}

func (t *mcpTeamFixtureTools) TeamAvailable(context.Context) error {
	if !t.enabled.Load() {
		return refusal(pa.ScopeDenied, "manage_team")
	}
	return nil
}
func (t *mcpTeamFixtureTools) Team(ctx context.Context, name string, args json.RawMessage) (any, error) {
	t.calls.Add(1)
	if deadline, ok := ctx.Deadline(); ok {
		t.wait = time.Until(deadline)
	}
	if t.decision != nil {
		select {
		case <-t.decision:
		case <-ctx.Done():
			close(t.cancelled)
			return nil, ctx.Err()
		}
	}
	return map[string]any{"name": name, "arguments": args}, nil
}

func TestMCPTeamToolsAreAdvertisedAndCalledOnlyWhileTheGrantIsOn(t *testing.T) {
	tools := &mcpTeamFixtureTools{}
	f := newMCPFixture(t, tools)
	f.initialize(t)
	list := `{"jsonrpc":"2.0","id":"list","method":"tools/list"}`
	_, body := f.post(t, list, nil)
	if bytes.Contains(body, []byte(`"list_team"`)) {
		t.Fatal("off by default still advertised management")
	}
	_, body = f.post(t, mcpCall("create_bot", map[string]any{"idempotency_key": "one", "name": "Helper"}), nil)
	if !bytes.Contains(body, []byte(string(pa.ScopeDenied))) || tools.calls.Load() != 0 {
		t.Fatal(string(body))
	}
	tools.enabled.Store(true)
	_, body = f.post(t, list, nil)
	for _, name := range []string{"list_team", "create_bot", "update_bot"} {
		if !bytes.Contains(body, []byte(`"`+name+`"`)) {
			t.Fatalf("missing %s: %s", name, body)
		}
	}
	if bytes.Contains(body, []byte(`"delete_bot"`)) {
		t.Fatal("a manager was offered deletion")
	}
	_, body = f.post(t, mcpCall("create_bot", map[string]any{"idempotency_key": "one", "name": "Helper"}), nil)
	if !bytes.Contains(body, []byte(`"isError":false`)) || tools.calls.Load() != 1 {
		t.Fatal(string(body))
	}
	if tools.wait < 270*time.Second {
		t.Fatalf("approval call had only %s, below the 270-second human wait", tools.wait)
	}
	tools.enabled.Store(false)
	_, body = f.post(t, mcpCall("update_bot", map[string]any{"idempotency_key": "two", "botId": "self", "name": "Changed"}), nil)
	if !bytes.Contains(body, []byte(string(pa.ScopeDenied))) || tools.calls.Load() != 1 {
		t.Fatal("a previously advertised tool bypassed revocation", string(body))
	}
}

func TestMCPTeamApprovalStreamsBeforeDecisionAndFencesDisconnect(t *testing.T) {
	for _, disconnected := range []bool{false, true} {
		name := "answer"
		if disconnected {
			name = "disconnect"
		}
		t.Run(name, func(t *testing.T) {
			decision, cancelled := make(chan struct{}), make(chan struct{})
			tools := &mcpTeamFixtureTools{decision: decision, cancelled: cancelled}
			tools.enabled.Store(true)
			f := newMCPFixture(t, tools)
			f.initialize(t)
			transport := &http.Transport{ResponseHeaderTimeout: time.Second}
			defer transport.CloseIdleConnections()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.lease.URL, strings.NewReader(mcpCall("create_bot", map[string]any{"idempotency_key": "held", "name": "Helper"})))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+f.lease.BearerToken)
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("MCP-Protocol-Version", "2025-06-18")
			response, err := (&http.Client{Transport: transport}).Do(req)
			if err != nil {
				t.Fatalf("the client got no headers before an unanswered tool's timeout: %v", err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
				t.Fatalf("expected an SSE stream, got %s (%s)", response.Status, response.Header.Get("Content-Type"))
			}
			reader := bufio.NewReader(response.Body)
			if line, err := reader.ReadString('\n'); err != nil || !strings.HasPrefix(line, ":") {
				t.Fatalf("expected a flushed comment while the decision is held: %q %v", line, err)
			}
			if disconnected {
				cancel()
				select {
				case <-cancelled:
				case <-time.After(time.Second):
					t.Fatal("disconnect left a live approval")
				}
				return
			}
			close(decision)
			var frameLine string
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					t.Fatal(err)
				}
				if data, ok := strings.CutPrefix(line, "data: "); ok {
					frameLine = data
					break
				}
			}
			var reply frame
			if err := json.Unmarshal([]byte(frameLine), &reply); err != nil || reply.Error != nil || !bytes.Contains(reply.Result, []byte(`"isError":false`)) {
				t.Fatalf("invalid terminal response: %s (%v)", frameLine, err)
			}
		})
	}
}
