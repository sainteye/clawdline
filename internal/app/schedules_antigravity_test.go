package app

import (
	"context"
	"testing"
	"time"
)

func TestScheduleAntigravityFieldsSurviveFormAndProviderSwitch(t *testing.T) {
	book := &ScheduleBook{Places: func(context.Context) []SchedulePlace { return []SchedulePlace{{ID: "place", Path: t.TempDir()}} }, IsDirectory: func(string) bool { return true }}
	body := map[string]any{"place_id": "place", "assistant": "antigravity", "title": "Synthetic native schedule", "instructions": "Synthetic instructions", "trigger_only": true, "model": "native-synthetic", "reasoning_effort": "max"}
	obj, _, refusal := book.build(context.Background(), body, newUUID(), time.Now(), nil)
	if refusal != nil {
		t.Fatalf("native create: %+v", refusal)
	}
	task := obj["task"].(map[string]any)
	delete(body, "model")
	delete(body, "reasoning_effort")
	obj, _, refusal = book.build(context.Background(), body, newUUID(), time.Now(), task)
	if refusal != nil {
		t.Fatalf("native carried save: %+v", refusal)
	}
	saved := obj["task"].(map[string]any)
	if saved["model"] != "native-synthetic" || saved["reasoning_effort"] != "max" {
		t.Fatal("native fields lost")
	}
	body["assistant"] = "codex"
	obj, _, refusal = book.build(context.Background(), body, newUUID(), time.Now(), task)
	if refusal != nil {
		t.Fatalf("switch: %+v", refusal)
	}
	switched := obj["task"].(map[string]any)
	if switched["model"] != nil || switched["reasoning_effort"] != nil {
		t.Fatal("cross-provider fallback")
	}
	body["reasoning_effort"] = "max"
	if _, _, refusal = book.build(context.Background(), body, newUUID(), time.Now(), task); refusal == nil {
		t.Fatal("Codex accepted native max")
	}
	body["assistant"] = "antigravity"
	for _, effort := range []string{"low", "medium", "high", "xhigh", "max", ""} {
		body["reasoning_effort"] = effort
		if _, _, refusal = book.build(context.Background(), body, newUUID(), time.Now(), task); refusal != nil {
			t.Fatalf("native effort %q: %+v", effort, refusal)
		}
	}
	body["reasoning_effort"] = "ultra"
	if _, _, refusal = book.build(context.Background(), body, newUUID(), time.Now(), task); refusal == nil {
		t.Fatal("unknown native effort accepted")
	}
}

func TestScheduleAntigravityCreateUpdateCopyReadBack(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	f.book.Places = func(context.Context) []SchedulePlace { return []SchedulePlace{{ID: "p1", Path: f.dir}} }
	body := map[string]any{"place_id": "p1", "assistant": "antigravity", "title": "Synthetic native schedule", "instructions": "Synthetic instructions", "trigger_only": true, "model": "native-synthetic", "template": map[string]any{"reasoning_effort": "low"}}
	made := f.book.Create(ctx, body, ScheduleAuthority{})
	task := storedTask(t, f, made)
	if task["model"] != "native-synthetic" || task["reasoning_effort"] != "low" {
		t.Fatal("copied template lost native fields")
	}
	id := made.Body["schedule"].(map[string]any)["id"].(string)
	delete(body, "template")
	delete(body, "model")
	body["reasoning_effort"] = "max"
	saved := f.book.Update(ctx, id, body, ScheduleAuthority{})
	if !saved.OK() {
		t.Fatalf("update: %+v", saved)
	}
	record, ok, err := f.book.Detail(ctx, id)
	if err != nil || !ok {
		t.Fatalf("detail: %v %v", ok, err)
	}
	kept := record["task"].(map[string]any)
	if kept["model"] != "native-synthetic" || kept["reasoning_effort"] != "max" {
		t.Fatal("readback lost updated native fields")
	}
	body["model"] = "native-synthetic"
	delete(body, "reasoning_effort")
	body["template"] = map[string]any{"reasoning_effort": kept["reasoning_effort"]}
	copied := storedTask(t, f, f.book.Create(ctx, body, ScheduleAuthority{}))
	if copied["model"] != "native-synthetic" || copied["reasoning_effort"] != "max" {
		t.Fatal("copy lost native fields")
	}
	body["permission_mode"] = "full"
	if refusal := permissionRefusal(ScheduleAuthority{Machine: true}, map[string]any{"task": map[string]any{"assistant": "antigravity", "permission_mode": "full"}}, nil); refusal == nil {
		t.Fatal("native provider bypassed permission authority")
	}
}
