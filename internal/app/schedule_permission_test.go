package app

import (
	"context"
	"strings"
	"testing"
)

// permissionFixture is a book with one place and an audit that is kept.
func permissionFixture(t *testing.T) (*w4Fixture, *[]map[string]string) {
	t.Helper()
	f := newW4(t)
	f.book.Places = func(context.Context) []SchedulePlace {
		return []SchedulePlace{{ID: "p1", Label: "p", Path: f.dir}}
	}
	lines := []map[string]string{}
	f.book.Audit = func(event string, fields map[string]string) {
		copied := map[string]string{"event": event}
		for k, v := range fields {
			copied[k] = v
		}
		lines = append(lines, copied)
	}
	return f, &lines
}

// formBody is the flat body the schedule form sends; `extra` is laid over it.
func formBody(extra map[string]any) map[string]any {
	body := map[string]any{
		"title": "nightly", "at": "09:00", "days": "daily", "place_id": "p1",
		"assistant": "claude", "instructions": "do it", "enabled": true,
	}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

func madeID(t *testing.T, made ScheduleReply) string {
	t.Helper()
	if made.Code != "" {
		t.Fatalf("create refused: %s %s", made.Code, made.Message)
	}
	summary, _ := made.Body["schedule"].(map[string]any)
	id, _ := summary["id"].(string)
	return id
}

func storedPermission(t *testing.T, f *w4Fixture, id string) (string, bool) {
	t.Helper()
	record, ok, err := f.book.Detail(context.Background(), id)
	if err != nil || !ok {
		t.Fatalf("schedule %s could not be read back: %v %v", id, ok, err)
	}
	task, _ := record["task"].(map[string]any)
	name, present := task["permission_mode"].(string)
	return name, present
}

// The form's permission field round-trips: a person's device sets it, a save
// without the key keeps it, `""` takes it off, and the parser still refuses a
// name it does not know. Each change is in the audit line of its write.
func TestTheSchedulePermissionFieldRoundTrips(t *testing.T) {
	f, audit := permissionFixture(t)
	ctx := context.Background()
	device := ScheduleAuthority{}

	id := madeID(t, f.book.Create(ctx, formBody(map[string]any{"permission_mode": "full"}), device))
	if got, _ := storedPermission(t, f, id); got != "full" {
		t.Fatalf("a create with permission_mode full stored %q", got)
	}
	if last := (*audit)[len(*audit)-1]; last["permission"] != "full" || last["permission_was"] != "default" {
		t.Fatalf("the create's audit line: %v", last)
	}

	if r := f.book.Update(ctx, id, formBody(map[string]any{"title": "renamed"}), device); r.Code != "" {
		t.Fatalf("a save without the key refused: %s %s", r.Code, r.Message)
	}
	if got, _ := storedPermission(t, f, id); got != "full" {
		t.Fatalf("a save without permission_mode left %q, want the stored full kept", got)
	}
	if last := (*audit)[len(*audit)-1]; last["permission"] != "" {
		t.Fatalf("a save that kept the permission audited a change: %v", last)
	}

	if r := f.book.Update(ctx, id, formBody(map[string]any{"permission_mode": "edits"}), device); r.Code != "" {
		t.Fatalf("a save to edits refused: %s %s", r.Code, r.Message)
	}
	if got, _ := storedPermission(t, f, id); got != "edits" {
		t.Fatalf("a save to edits stored %q", got)
	}

	if r := f.book.Update(ctx, id, formBody(map[string]any{"permission_mode": ""}), device); r.Code != "" {
		t.Fatalf("a save with an empty permission refused: %s %s", r.Code, r.Message)
	}
	if got, present := storedPermission(t, f, id); present {
		t.Fatalf(`a save with permission_mode "" left the key as %q`, got)
	}
	if last := (*audit)[len(*audit)-1]; last["permission"] != "default" || last["permission_was"] != "edits" {
		t.Fatalf("the removal's audit line: %v", last)
	}

	for _, bad := range []any{"root", 3, true} {
		r := f.book.Update(ctx, id, formBody(map[string]any{"permission_mode": bad}), device)
		if r.Code != "bad_request" || !strings.Contains(r.Message, "permission_mode must be one of") {
			t.Errorf("permission_mode %v answered %q %q", bad, r.Code, r.Message)
		}
	}
}

// An agent may keep a schedule's permission and may not set or change it:
// neither this machine's orchestrator token nor a session carrying a person's
// message can grant itself a full-permission wake-up. The refusal names the
// person, and nothing is written.
func TestAnAgentMayNotSetOrChangeASchedulesPermission(t *testing.T) {
	f, _ := permissionFixture(t)
	ctx := context.Background()
	machine := ScheduleAuthority{Machine: true}
	relay := ScheduleAuthority{Run: "run-1", Session: "session-1"}
	once := func(extra map[string]any) map[string]any {
		body := formBody(extra)
		delete(body, "days")
		body["on"] = "2026-09-20"
		return body
	}
	refused := func(what string, r ScheduleReply) {
		t.Helper()
		if r.Status != 403 || r.Code != "permission_needs_person" || !strings.Contains(r.Message, "set by a person") {
			t.Errorf("%s answered %d %q %q, want 403 permission_needs_person", what, r.Status, r.Code, r.Message)
		}
	}

	refused("the token making a once schedule with full", f.book.Create(ctx, once(map[string]any{"permission_mode": "full"}), machine))
	refused("a relayed create with ask", f.book.Create(ctx, formBody(map[string]any{"permission_mode": "ask"}), relay))
	if list, _ := f.book.List(ctx); len(list) != 0 {
		t.Fatalf("a refused create left %d schedules", len(list))
	}
	// The machine's default is what a create without the key has already.
	madeID(t, f.book.Create(ctx, once(map[string]any{"permission_mode": ""}), machine))

	// A once schedule a person gave full permission: the token may save it
	// keeping that, and may not change or remove it.
	onceID := madeID(t, f.book.Create(ctx, once(map[string]any{"permission_mode": "full"}), ScheduleAuthority{}))
	for what, mode := range map[string]any{"the token changing full to ask": "ask", "the token removing full": ""} {
		refused(what, f.book.Update(ctx, onceID, once(map[string]any{"permission_mode": mode}), machine))
	}
	if got, _ := storedPermission(t, f, onceID); got != "full" {
		t.Fatalf("after refused saves the permission is %q", got)
	}
	for what, body := range map[string]map[string]any{
		"without the key": once(map[string]any{"title": "kept"}),
		"with the same":   once(map[string]any{"permission_mode": "full"}),
	} {
		if r := f.book.Update(ctx, onceID, body, machine); r.Code != "" {
			t.Errorf("the token saving %s refused: %s %s", what, r.Code, r.Message)
		}
	}

	// A repeating schedule through the run relay: the same rule.
	dailyID := madeID(t, f.book.Create(ctx, formBody(nil), ScheduleAuthority{}))
	refused("a relayed save granting full", f.book.Update(ctx, dailyID, formBody(map[string]any{"permission_mode": "full"}), relay))
	if _, present := storedPermission(t, f, dailyID); present {
		t.Fatalf("a refused relayed save stored a permission")
	}
	if r := f.book.Update(ctx, dailyID, formBody(map[string]any{"title": "relayed"}), relay); r.Code != "" {
		t.Fatalf("a relayed save that leaves the permission alone refused: %s %s", r.Code, r.Message)
	}
}
