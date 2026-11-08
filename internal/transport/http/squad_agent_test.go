package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/squad"
)

func TestSquadAgentCreatesBindsUpdatesAndLaunchesWithoutPersonCredential(t *testing.T) {
	f := newSquadFixture(t)
	token, err := f.s.gate().files.MachineToken()
	if err != nil {
		t.Fatal(err)
	}
	ask := func(method, path string, body any, key string) (int, []byte) {
		t.Helper()
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		headers := map[string]string{"X-Clawdline-Orchestrator": token, "Content-Type": "application/json", "Idempotency-Key": key}
		r := call{method: method, path: path, body: string(data), headers: headers}.do(f.h)
		return r.Code, r.Body.Bytes()
	}
	// The new contract grants only catalog/settings/scopes, not AI or telemetry.
	for _, path := range []string{"/v1/squad/role-draft", "/v1/squad/session-bindings", "/v1/squad/session-snapshots/anything", "/v1/squad/session-snapshots?conversation=anything", "/v1/squad-packages/export"} {
		if status, _ := ask("GET", path, nil, ""); status != 403 {
			t.Fatalf("Agent got %d on %s", status, path)
		}
	}
	if status, _ := f.ask("GET", "/v1/squad/catalog", "", "", ""); status != 401 {
		t.Fatalf("anonymous=%d", status)
	}
	icon := squad.Builtins().Definitions[0].Icon
	skill := squad.Skill{SkillID: "personal.skill.agent-proof", Version: "1", Source: "Original", License: "UNLICENSED", Name: squad.Names{En: "Check"}, Purpose: squad.Names{En: "Verify sources"}, Content: "Check the sources.", Icon: icon}
	write := map[string]any{"kind": "skill", "expected_version": 0, "entity": map[string]any{"skill": skill}}
	status, first := ask("POST", "/v1/squad/catalog", write, "skill-once")
	if status != 200 {
		t.Fatalf("skill=%d %s", status, first)
	}
	status, again := ask("POST", "/v1/squad/catalog", write, "skill-once")
	if status != 200 || string(first) != string(again) {
		t.Fatalf("replay=%d %s", status, again)
	}
	d := squad.Definition{DefinitionID: "personal.persona.agent-proof", Version: "1", Source: "Original", License: "UNLICENSED", Name: squad.Names{En: "Editor"}, Body: "Preserve this body.", Icon: icon, Teams: []squad.Reference{{ID: "clawdline.team.custom", Version: "1"}}, Skills: []squad.SkillReference{}}
	create := map[string]any{"kind": "definition", "expected_version": 1, "entity": map[string]any{"definition": d}}
	if status, raw := ask("POST", "/v1/squad/catalog", create, "role-once"); status != 200 {
		t.Fatalf("role=%d %s", status, raw)
	}
	binding := map[string]any{"definition_id": d.DefinitionID, "expected_version": 0, "overrides": map[string]any{"handbook": map[string]any{"present": true, "value": "Keep this handbook."}, "skills": map[string]any{"present": true, "value": []squad.Choice{{ID: skill.SkillID, Version: skill.Version, Enabled: true}}}}}
	status, first = ask("PUT", "/v1/squad/settings", binding, "bind-once")
	if status != 200 {
		t.Fatalf("bind=%d %s", status, first)
	}
	status, again = ask("PUT", "/v1/squad/settings", binding, "bind-once")
	if status != 200 || string(first) != string(again) {
		t.Fatalf("bind replay=%d %s", status, again)
	}
	if status, raw := ask("PUT", "/v1/squad/settings", binding, "stale-bind"); status != 409 {
		t.Fatalf("stale=%d %s", status, raw)
	}
	binding["definition_id"] = "personal.persona.missing"
	if status, raw := ask("PUT", "/v1/squad/settings", binding, "missing-role"); status != 404 {
		t.Fatalf("unknown=%d %s", status, raw)
	}
	binding["definition_id"] = d.DefinitionID
	binding["expected_version"] = 1
	binding["overrides"] = map[string]any{"skills": map[string]any{"present": true, "value": []squad.Choice{{ID: "personal.skill.missing", Version: "1", Enabled: true}}}}
	if status, raw := ask("PUT", "/v1/squad/settings", binding, "missing-skill"); status != 400 {
		t.Fatalf("unknown skill=%d %s", status, raw)
	}
	d.Version = "2"
	d.Icon.Accent = "#d97935"
	update := map[string]any{"kind": "definition", "expected_version": 2, "entity": map[string]any{"definition": d}}
	if status, raw := ask("POST", "/v1/squad/catalog", update, "icon-once"); status != 200 {
		t.Fatalf("update=%d %s", status, raw)
	}
	launch, err := f.s.ResolveSquadLaunch(context.Background(), d.DefinitionID, "")
	if err != nil || launch.Definition.Body != d.Body || launch.Definition.Icon.Accent != "#d97935" || launch.Handbook.Text != "Keep this handbook." || len(launch.Skills) != 1 || launch.Skills[0].Content != skill.Content || !launch.Skills[0].Enabled {
		t.Fatalf("snapshot=%+v err=%v", launch, err)
	}
	for _, path := range []string{"/v1/squad/catalog", "/v1/squad/scopes", "/v1/squad/settings", "/v1/squad/definitions/" + d.DefinitionID + "?version=1"} {
		if status, raw := ask(http.MethodGet, path, nil, ""); status != 200 {
			t.Fatalf("read=%d %s", status, raw)
		}
	}
}
