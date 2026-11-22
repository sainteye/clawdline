package projectassistantacp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
)

type mcpRoutineFixtureTools struct {
	mcpTestTools
	last app.RoutineRequest
}

func (r *mcpRoutineFixtureTools) Routines(_ context.Context, q app.RoutineRequest) (app.RoutineResult, error) {
	r.last = q
	return app.RoutineResult{ID: "synthetic-routine"}, nil
}

func TestMCPRoutineHooksAndFollowUpsUseOwnedTool(t *testing.T) {
	tools := &mcpRoutineFixtureTools{}
	f := newMCPFixture(t, tools)
	f.initialize(t)
	for _, tc := range []struct{ name, action, arguments string }{
		{"save_routine", "save", `{"idempotency_key":"save","name":"Synthetic","instruction":"Check synthetic state","triggers":[{"type":"event","source":"github","event":"issues.opened","filters":{"repo":"synthetic/repo"}},{"type":"webhook"}]}`},
		{"routine_hook", "hook", `{"id":"synthetic"}`},
		{"routine_webhook_status", "webhook_status", `{"id":"synthetic"}`},
		{"rotate_routine_hook", "rotate_hook", `{"id":"synthetic","idempotency_key":"rotate"}`},
		{"follow_up", "follow_up", `{"minutes":20,"content":"Synthetic build is pending","idempotency_key":"follow"}`},
		{"cancel_follow_up", "cancel_follow_up", `{"id":"synthetic","idempotency_key":"cancel"}`},
	} {
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tc.name + `","arguments":` + tc.arguments + `}}`
		status, raw := f.post(t, body, nil)
		if status != 200 || strings.Contains(string(raw), `"isError":true`) || tools.last.Action != tc.action {
			t.Fatalf("owned tool %s: %d %s %+v", tc.name, status, raw, tools.last)
		}
		if tc.action == "save" && (len(tools.last.Triggers) != 2 || tools.last.Triggers[0].Filters["repo"] != "synthetic/repo") {
			t.Fatal("event intent lost", tools.last)
		}
		if tc.action == "follow_up" && tools.last.Minutes != 20 {
			t.Fatal("follow-up deadline lost", tools.last)
		}
	}
	status, raw := f.post(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, nil)
	if status != 200 {
		t.Fatal(status, string(raw))
	}
	var reply struct {
		Result struct {
			Tools []struct {
				Name   string          `json:"name"`
				Schema json.RawMessage `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range reply.Result.Tools {
		if tool.Name == "save_routine" {
			found = strings.Contains(string(tool.Schema), `"webhook"`) && strings.Contains(string(tool.Schema), `"event"`)
		}
	}
	if !found {
		t.Fatal("native tool cannot describe hook schedules", string(raw))
	}
}

func TestOwnedMCPWebhookGuideCanBeReadBeforeCreatingARoutine(t *testing.T) {
	f := newMCPFixture(t, mcpTestTools{})
	f.initialize(t)

	status, raw := f.post(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, nil)
	if status != 200 || !strings.Contains(string(raw), `"name":"read_webhook_guide"`) {
		t.Fatalf("Webhook guide cannot be discovered: %d %s", status, raw)
	}

	for _, tt := range []struct {
		lang string
		want []string
	}{
		{"en", []string{"Set up a Bot Webhook", "Public Cloud address", "Cloud confirms", "Retrieve key"}},
		{"zh-Hant", []string{"設定 Bot Webhook", "公開 Cloud 位址", "Cloud 確認", "取得金鑰"}},
	} {
		t.Run(tt.lang, func(t *testing.T) {
			status, raw := f.post(t, mcpCall("read_webhook_guide", map[string]any{"lang": tt.lang}), nil)
			if status != 200 || !strings.Contains(string(raw), `"isError":false`) {
				t.Fatalf("guide read failed: %d %s", status, raw)
			}
			for _, want := range tt.want {
				if !strings.Contains(string(raw), want) {
					t.Errorf("guide omits %q", want)
				}
			}
		})
	}
}
