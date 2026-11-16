package projectassistantacp

import (
	"bytes"
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type mcpTeamFixtureTools struct {
	mcpTestTools
	enabled atomic.Bool
	calls   atomic.Int32
	wait    time.Duration
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
