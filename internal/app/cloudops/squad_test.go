package cloudops

import (
	"context"
	"testing"
)

func TestSquadPrivateReadsAreNarrowPairedDeviceRoutes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra map[string]any
		path  string
	}{
		{"squad.catalog", nil, "/v1/squad/catalog"},
		{"squad.scopes", nil, "/v1/squad/scopes"},
		{"squad.skill-sources", map[string]any{"provider": "codex", "place_id": "project-a"}, "/v1/squad/skill-sources"},
		{"squad.settings", map[string]any{"place_id": "project-a"}, "/v1/squad/settings"},
		{"squad.definition", map[string]any{"definition_id": "backend"}, "/v1/squad/definitions/backend"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &router{}
			body := map[string]any{"type": tc.name, "session": MachineReplySession, "request": "squad-read"}
			for k, v := range tc.extra {
				body[k] = v
			}
			answer := Bridge{MachineID: "mac-01", Router: r}.Handle(context.Background(), request(t, ClassCtl, body))
			if answer.Status != 200 || len(r.seen) != 1 || r.last().Method != "GET" ||
				r.last().Path != tc.path || r.last().Header[actorHeader] != actorDevice {
				t.Fatalf("answer %+v, route %+v", answer, r.seen)
			}
			if tc.name == "squad.settings" && r.last().Query["place_id"] != "project-a" {
				t.Fatalf("scope query = %+v", r.last().Query)
			}
			body["unexpected"] = "private"
			r = &router{}
			answer = Bridge{MachineID: "mac-01", Router: r}.Handle(context.Background(), request(t, ClassCtl, body))
			if answer.Code != "malformed_read" || len(r.seen) != 0 {
				t.Fatalf("extra field answered %+v, routed %+v", answer, r.seen)
			}
		})
	}
}

func TestSquadSkillSourceDetailCarriesOnlyListedIdentity(t *testing.T) {
	r := &router{}
	id := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	body := map[string]any{"type": "squad.skill-sources", "session": MachineReplySession,
		"request": "source-detail", "provider": "codex", "place_id": "project-a", "id": id, "folder": "true"}
	answer := Bridge{MachineID: "mac-01", Router: r}.Handle(context.Background(), request(t, ClassCtl, body))
	if answer.Status != 200 || len(r.seen) != 1 || r.last().Query["id"] != id || r.last().Query["folder"] != "true" {
		t.Fatalf("detail = %+v, route %+v", answer, r.seen)
	}
	for _, bad := range []map[string]any{
		{"id": "../../private", "folder": "true"},
		{"id": id, "folder": "maybe"},
		{"id": id, "path": "/private", "folder": "true"},
	} {
		for key := range body {
			if key == "id" || key == "folder" || key == "path" {
				delete(body, key)
			}
		}
		for key, value := range bad {
			body[key] = value
		}
		r = &router{}
		answer = Bridge{MachineID: "mac-01", Router: r}.Handle(context.Background(), request(t, ClassCtl, body))
		if answer.Code != "malformed_read" || len(r.seen) != 0 {
			t.Fatalf("unsafe detail = %+v, route %+v", answer, r.seen)
		}
	}
}

func TestSquadCloudWritesNeedTheCommandSwitch(t *testing.T) {
	body := map[string]any{"type": "squad.settings.update", "session": MachineReplySession,
		"request": "squad-change", "changes": map[string]any{
			"definition_id": "backend", "expected_version": 0,
			"overrides": map[string]any{"handbook": map[string]any{"present": true, "value": "private"}},
		}}
	r := &router{}
	answer := Bridge{MachineID: "mac-01", Router: r}.Handle(context.Background(), request(t, ClassCtl, body))
	if answer.Status == 200 || len(r.seen) != 0 {
		t.Fatalf("closed write = %+v, %+v", answer, r.seen)
	}
	answer = open(r).Handle(context.Background(), request(t, ClassCtl, body))
	if answer.Status != 200 || len(r.seen) != 1 || r.last().Method != "PUT" ||
		r.last().Path != "/v1/squad/settings" || r.last().Header[actorHeader] != actorDevice ||
		r.last().Header["Idempotency-Key"] != "squad-change" {
		t.Fatalf("open write = %+v, %+v", answer, r.seen)
	}
}
