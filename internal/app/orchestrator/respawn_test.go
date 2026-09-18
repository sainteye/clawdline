package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The respawn family limit is counted over everything descending from one
// original, not over a chain's depth: the id a root has in hand is the one
// that failed, and it will ask about that one again and again.
func TestRespawnIsLimitedPerFamily(t *testing.T) {
	b, ctx := newTestBroker(t)
	original := "91111111-1111-4111-8111-111111111111"
	brief := map[string]any{
		"clawdline_protocol": 1, "task_id": original, "kind": "custom", "assistant": "claude",
		"permission_mode": "full", "project_dir": t.TempDir(), "title": "retry me",
		"instructions": "do the thing", "claims": []string{}, "isolation": "none", "timeout_minutes": 30,
		"root": map[string]any{"session_id": rootConversation, "assistant": "claude", "label": "root"},
	}
	dir := b.Tasks.Path(original)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(brief)
	if err := os.WriteFile(filepath.Join(dir, "task.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := b.save(ctx, Record{Protocol: Protocol, ID: original, Assistant: "claude", Title: "retry me",
		State: StateSpawnFailed, CreatedAt: time.Now(), Claims: []string{}, Dir: dir}, HashSecret("s"), "task.spawn_failed"); err != nil {
		t.Fatal(err)
	}

	first, err := b.Respawn(ctx, original, "")
	if err != nil {
		t.Fatalf("first respawn: %v", err)
	}
	if first.Record.RespawnOf != original || first.Record.RespawnGeneration != 1 || first.Original != original {
		t.Fatalf("first respawn record = %+v", first)
	}
	if !IsTaskSecret(first.Secret) || first.Record.ID == original {
		t.Fatalf("the respawn did not get a fresh id and secret: %+v", first)
	}
	if first.Record.Title != "retry me" || first.Record.Instructions != "do the thing" {
		t.Errorf("task.json was not copied: %+v", first.Record)
	}
	if first.Record.State != StateSpawnFailed {
		// No terminal in a test: the retry fails the same way, which is what
		// lets the family be exercised without opening anything.
		t.Fatalf("respawned task is %q", first.Record.State)
	}
	if _, err := b.Respawn(ctx, original, ""); err != nil {
		t.Fatalf("second respawn: %v", err)
	}
	for _, id := range []string{original, first.Record.ID} {
		_, err := b.Respawn(ctx, id, "")
		ref, ok := err.(Refusal)
		if !ok || ref.Code != "respawn_exhausted" || ref.Extra["original_task"] != original || ref.Extra["respawns"] != 2 {
			t.Fatalf("third respawn from %s: %v", id[:8], err)
		}
	}

	done := "92222222-2222-4222-8222-222222222222"
	if err := b.save(ctx, Record{Protocol: Protocol, ID: done, Assistant: "claude", State: StateSuccess,
		CreatedAt: time.Now(), Claims: []string{}}, HashSecret("s"), "task.success"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Respawn(ctx, done, ""); err == nil || err.(Refusal).Code != "not_respawnable" {
		t.Fatalf("a finished task was respawned: %v", err)
	}
}
