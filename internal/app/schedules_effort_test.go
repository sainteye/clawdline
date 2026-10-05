package app

import (
	"context"
	"testing"
	"time"
)

func TestScheduleFormChoosesAndClearsCodexEffort(t *testing.T) {
	dir := t.TempDir()
	id := newUUID()
	book := &ScheduleBook{Places: func(context.Context) []SchedulePlace {
		return []SchedulePlace{{ID: "place", Path: dir}}
	}, IsDirectory: func(string) bool { return true }}
	body := map[string]any{
		"place_id": "place", "assistant": "codex", "title": "Daily", "instructions": "do work",
		"at": "09:00", "days": "daily", "reasoning_effort": "xhigh",
	}
	obj, _, refusal := book.build(context.Background(), body, id, time.Now(), nil)
	if refusal != nil {
		t.Fatalf("choose effort: %v", refusal)
	}
	task := obj["task"].(map[string]any)
	if task["reasoning_effort"] != "xhigh" {
		t.Fatalf("stored effort = %v", task["reasoning_effort"])
	}

	delete(body, "reasoning_effort")
	obj, _, refusal = book.build(context.Background(), body, id, time.Now(), task)
	if refusal != nil || obj["task"].(map[string]any)["reasoning_effort"] != "xhigh" {
		t.Fatalf("unchanged effort was not retained: %v", refusal)
	}

	body["reasoning_effort"] = ""
	obj, _, refusal = book.build(context.Background(), body, id, time.Now(), task)
	if refusal != nil {
		t.Fatalf("clear effort: %v", refusal)
	}
	if _, kept := obj["task"].(map[string]any)["reasoning_effort"]; kept {
		t.Fatal("cleared effort remained")
	}

	body["assistant"] = "claude"
	body["reasoning_effort"] = "high"
	if _, _, refusal = book.build(context.Background(), body, id, time.Now(), task); refusal == nil {
		t.Fatal("Claude accepted Codex effort")
	}
}
