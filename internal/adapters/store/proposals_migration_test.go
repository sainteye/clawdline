package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
)

// A daemon that already has proposals carries those rows through the table
// rebuild that widens the state constraint for `resolved`. The migrated table
// accepts the evidence-backed terminal state, and opening it again does not
// rebuild or lose that evidence.
func TestAnOlderProposalTableKeepsItsRowsAndAcceptsResolution(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, DBFile))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE proposals (
  id TEXT PRIMARY KEY, work_id TEXT NOT NULL, task_id TEXT, session TEXT NOT NULL,
  source TEXT NOT NULL, project TEXT NOT NULL, title TEXT NOT NULL,
  signals TEXT NOT NULL, effects TEXT NOT NULL, ask INTEGER NOT NULL,
  ask_reason TEXT NOT NULL, channel TEXT NOT NULL, question TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL CHECK (state IN ('pending','answered','expired','withdrawn')),
  answer TEXT, answered_by TEXT, answered_at INTEGER, created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL, asked_inline_at INTEGER,
  withdrawn_reason TEXT, withdrawn_at INTEGER, version INTEGER NOT NULL DEFAULT 0
);
INSERT INTO proposals (id, work_id, session, source, project, title, signals, effects, ask,
  ask_reason, channel, state, created_at, expires_at)
VALUES ('proposal', 'line', 'root', 'session', '/project', 'Checked condition',
  '["cross_session"]', '[]', 0, 'human_absent', 'to_confirm', 'pending', 10, 20);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := st.WriteWork(ctx, func(tx *WorkTx) error {
		prev, err := tx.Proposal("proposal")
		if err != nil {
			return err
		}
		next, err := work.ResolveProposal(prev, "The condition was already handled.",
			"internal/worker.go:42", "root:root", time.Unix(30, 0))
		if err != nil {
			return err
		}
		return tx.PutProposal(next, &prev, 0)
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(dir)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer st.Close()
	got, err := st.ProposalRow(ctx, "proposal")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != work.ProposalResolved || got.ResolutionEvidence != "internal/worker.go:42" ||
		got.ResolvedBy != "root:root" || !got.ResolvedAt.Equal(time.Unix(30, 0)) {
		t.Fatalf("migrated resolution: %+v", got)
	}
}
