package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// A store made before W5 has a `coordinator` table whose `session_id` column
// held a terminal id (#36). Opening it renames the column to what it held and
// adds the identity columns; the row it had is kept, and read as a record this
// daemon cannot vouch for rather than as "no role" or as a role.
func TestAnOlderCoordinatorTableIsRenamedNotCopied(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, DBFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE coordinator (
	  id INTEGER PRIMARY KEY CHECK (id = 1), record_id TEXT NOT NULL, label TEXT NOT NULL,
	  session_id TEXT NOT NULL, conversation_id TEXT NOT NULL, assistant TEXT NOT NULL,
	  pid INTEGER NOT NULL, registered_at INTEGER NOT NULL, rebound_at INTEGER NOT NULL DEFAULT 0,
	  generation INTEGER NOT NULL);
	  INSERT INTO coordinator VALUES (1, '%3:c', 'x', '%3', 'c', 'codex', 7, 1, 0, 1);`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, col := range []string{"terminal_id", "tty", "process_start", "aliases"} {
		if has, err := hasColumn(st.db, "coordinator", col); err != nil || !has {
			t.Fatalf("column %s after opening: %v %v", col, has, err)
		}
	}
	if has, _ := hasColumn(st.db, "coordinator", "session_id"); has {
		t.Fatal("the second spelling of a session is still a column")
	}
	var terminal string
	if err := st.db.QueryRow(`SELECT terminal_id FROM coordinator`).Scan(&terminal); err != nil || terminal != "%3" {
		t.Fatalf("the old value moved to %q: %v", terminal, err)
	}
	rec, status, err := st.Coordinator(context.Background())
	if err != nil || rec != nil || status != CoordinatorUnsupported {
		t.Fatalf("the old row reads as %v %+v %v", status, rec, err)
	}
	// Opening again is a no-op.
	_ = st.Close()
	if st, err = Open(dir); err != nil {
		t.Fatalf("a second open: %v", err)
	}
}
