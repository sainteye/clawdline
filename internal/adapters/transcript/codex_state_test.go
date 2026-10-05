package transcript

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func writeCodexState(t *testing.T, path string, rows map[string]any) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE threads (id TEXT PRIMARY KEY, cwd TEXT, name TEXT)`); err != nil {
		t.Fatal(err)
	}
	for id, name := range rows {
		if _, err := db.Exec(`INSERT INTO threads (id, cwd, name) VALUES (?, '/code/demo', ?)`, id, name); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCodexStateNamesReadsTheNewestStateDatabaseReadOnly(t *testing.T) {
	home := t.TempDir()
	codex := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codex, 0o700); err != nil {
		t.Fatal(err)
	}
	writeCodexState(t, filepath.Join(codex, "state_4.sqlite"), map[string]any{"a": "Old database"})
	writeCodexState(t, filepath.Join(codex, "state_12.sqlite"), map[string]any{
		"a": "Inspect the queue", "b": nil, "c": "", "d": "Not asked for",
	})
	before, _ := os.ReadDir(codex)

	got := codexStateNames(home, []string{"a", "b", "c", "missing"})
	if len(got) != 1 || got["a"] != "Inspect the queue" {
		t.Fatalf("names = %v; want only a from state_12", got)
	}
	after, _ := os.ReadDir(codex)
	if len(after) != len(before) {
		t.Fatalf("reading created files: %d entries before, %d after", len(before), len(after))
	}
}

func TestCodexStateNamesFallsBackToNothing(t *testing.T) {
	home := t.TempDir()
	if got := codexStateNames(home, []string{"a"}); len(got) != 0 {
		t.Fatalf("no database = %v", got)
	}
	codex := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codex, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codex, "state_5.sqlite"), []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := codexStateNames(home, []string{"a"}); len(got) != 0 {
		t.Fatalf("corrupt database = %v", got)
	}
	if got := codexStateNames(home, nil); len(got) != 0 {
		t.Fatalf("no ids = %v", got)
	}
}
