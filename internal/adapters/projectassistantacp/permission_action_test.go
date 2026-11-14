package projectassistantacp

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

func TestAPermissionCardSaysWhatTheToolWantsToDo(t *testing.T) {
	run := permissionAction(json.RawMessage(`{"sessionId":"s","toolCall":{"toolCallId":"c","title":"rm -rf /tmp/x","kind":"execute","rawInput":{"command":"rm -rf /tmp/x","description":"Delete"}},"options":[]}`))
	if run == nil || run.ToolKind != "execute" || run.Title != "rm -rf /tmp/x" || run.Detail != "rm -rf /tmp/x" || run.Shortened {
		t.Fatalf("command: %+v", run)
	}
	edit := permissionAction(json.RawMessage(`{"toolCall":{"title":"Write notes.md","kind":"edit","rawInput":{"file_path":"/repo/notes.md"},"content":[{"type":"diff","path":"/repo/notes.md","oldText":"a","newText":"b"},{"type":"content","content":{"type":"text","text":"ignored when input exists"}}]}}`))
	if edit == nil || edit.Detail != "/repo/notes.md" || len(edit.Diffs) != 1 || edit.Diffs[0].OldText != "a" || edit.Diffs[0].NewText != "b" {
		t.Fatalf("edit: %+v", edit)
	}
	plan := permissionAction(json.RawMessage(`{"toolCall":{"title":"Approve Plan","kind":"switch_mode","rawInput":{"plan":"# Plan\n1. Write","planFilePath":"/p.md"}}}`))
	if plan == nil || plan.Detail != "# Plan\n1. Write" {
		t.Fatalf("plan text: %+v", plan)
	}
	other := permissionAction(json.RawMessage(`{"toolCall":{"title":"Other","kind":"other","rawInput":{"answer":"x"}}}`))
	if other == nil || other.Detail != `{"answer":"x"}` {
		t.Fatalf("input as JSON: %+v", other)
	}
	if permissionAction(json.RawMessage(`{"toolCall":"not an object"}`)) != nil || permissionAction(json.RawMessage(`{"options":[]}`)) != nil {
		t.Fatal("an unreadable or empty tool call produced an action")
	}
}

func TestALargeActionIsShortenedWithinItsBound(t *testing.T) {
	big := strings.Repeat("界", capacity.MaximumBotPermissionActionBytes)
	raw, _ := json.Marshal(map[string]any{"toolCall": map[string]any{"title": "Write", "kind": "edit", "rawInput": map[string]string{"command": big},
		"content": []any{map[string]string{"type": "diff", "path": "a", "oldText": big, "newText": big}, map[string]string{"type": "diff", "path": "b", "oldText": "x", "newText": "y"}}}})
	a := permissionAction(raw)
	total := len(a.Title) + len(a.ToolKind) + len(a.Detail)
	for _, d := range a.Diffs {
		total += len(d.Path) + len(d.OldText) + len(d.NewText)
		if !utf8.ValidString(d.OldText) || !utf8.ValidString(d.NewText) {
			t.Fatal("a diff was cut inside a character")
		}
	}
	if !a.Shortened || total > capacity.MaximumBotPermissionActionBytes || !utf8.ValidString(a.Detail) {
		t.Fatalf("shortened=%v total=%d", a.Shortened, total)
	}
}
