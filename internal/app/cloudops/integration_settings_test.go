package cloudops

import (
	"context"
	"testing"
)

func TestIntegrationSettingsCrossOnlyTheirNarrowMachineRoute(t *testing.T) {
	r := &router{}
	answer := Bridge{MachineID: "mac-01", Router: r}.Handle(context.Background(), request(t, ClassCtl,
		map[string]any{"type": "integration-settings", "session": MachineReplySession, "request": "req-read"}))
	if answer.Status != 200 || len(r.seen) != 1 || r.last().Method != "GET" || r.last().Path != "/v1/settings/integrations" {
		t.Fatalf("read answered %+v, asked %+v", answer, r.seen)
	}
	r = &router{}
	answer = open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
		"type": "integration-settings-update", "session": MachineReplySession, "request": "req-write",
		"changes": map[string]any{"workflow_skills_enabled": true, "graphify_enabled": false},
	}))
	if answer.Status != 200 || len(r.seen) != 1 {
		t.Fatalf("write answered %+v, asked %+v", answer, r.seen)
	}
	got := r.last()
	if got.Method != "POST" || got.Path != "/v1/settings/integrations" ||
		string(got.Body) != `{"graphify_enabled":false,"workflow_skills_enabled":true}` ||
		got.Header[actorHeader] != actorDevice || got.Header["Idempotency-Key"] != "req-write" {
		t.Fatalf("write crossed too broadly: %+v body=%s", got, got.Body)
	}
	for _, changes := range []any{
		map[string]any{"remote": true}, map[string]any{"workflow_skills_enabled": "on"},
		map[string]any{"graphify_enabled": nil}, map[string]any{"project": "/project"}, []any{true},
	} {
		r := &router{}
		bad := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{
			"type": "integration-settings-update", "session": MachineReplySession, "request": "req-bad", "changes": changes,
		}))
		if bad.Code != "malformed_command" || len(r.seen) != 0 {
			t.Fatalf("took changes %#v: %+v, asked %+v", changes, bad, r.seen)
		}
	}
}

func TestWorkflowProfileCrossesOnlyTheIntegrationSettingsRoute(t *testing.T) {
	for _, profile := range []string{"clawdline", "superpowers", "poteto"} {
		r := &router{}
		answer := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{"type": "integration-settings-update", "session": MachineReplySession, "request": "req-profile", "changes": map[string]any{"workflow_profile": profile}}))
		if answer.Status != 200 || len(r.seen) != 1 || r.last().Path != "/v1/settings/integrations" || string(r.last().Body) != `{"workflow_profile":"`+profile+`"}` {
			t.Fatalf("profile route mismatch: %+v %+v", answer, r.seen)
		}
	}
	for _, value := range []any{"", "unknown", true, nil} {
		r := &router{}
		answer := open(r).Handle(context.Background(), request(t, ClassCtl, map[string]any{"type": "integration-settings-update", "session": MachineReplySession, "request": "req-profile-bad", "changes": map[string]any{"workflow_profile": value}}))
		if answer.Code != "malformed_command" || len(r.seen) != 0 {
			t.Fatalf("invalid profile crossed boundary: %+v", answer)
		}
	}
}
