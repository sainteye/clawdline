package projects

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecordedAntigravity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	id := "00000000-0000-4000-8000-000000000001"
	place := Place{Path: "/some/path"}

	if Recorded(place, AssistantAntigravity, id) {
		t.Fatal("expected false before file creation")
	}

	p := filepath.Join(home, ".gemini", "antigravity-cli", "brain", id, ".system_generated", "logs", "transcript_full.jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !Recorded(place, AssistantAntigravity, id) {
		t.Fatal("expected true after transcript created")
	}
}
