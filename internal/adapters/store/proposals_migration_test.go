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

// Questions written before work_id became mandatory have no Board context to
// present to a person. Opening that store removes only those rows, keeps every
// linked answer, and leaves a schema that cannot admit another unlinked row.
func TestOpeningAnOlderDecisionTableRemovesOnlyUnlinkedQuestions(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, DBFile))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE decisions (
  id TEXT PRIMARY KEY, session TEXT NOT NULL, work_id TEXT, task_id TEXT,
  project TEXT NOT NULL DEFAULT '', question TEXT NOT NULL, options TEXT NOT NULL,
  default_option TEXT NOT NULL, blocking INTEGER NOT NULL, state TEXT NOT NULL,
  answer TEXT, answered_by TEXT, answered_at INTEGER, created_at INTEGER NOT NULL,
  due_at INTEGER NOT NULL, push TEXT NOT NULL, pushed_at INTEGER,
  version INTEGER NOT NULL DEFAULT 0
);
INSERT INTO decisions
  (id,session,work_id,project,question,options,default_option,blocking,state,answer,answered_by,
   answered_at,created_at,due_at,push)
VALUES
  ('unlinked-null','session-a',NULL,'/p','Old question','[{"id":"yes","label":"Yes"}]','yes',0,
   'open',NULL,NULL,NULL,10,20,'none'),
  ('unlinked-blank','session-a','','/p','Blank question','[{"id":"yes","label":"Yes"}]','yes',0,
   'open',NULL,NULL,NULL,11,21,'none'),
  ('linked','session-a','work-a','/p','Kept answer','[{"id":"yes","label":"Yes"}]','yes',0,
   'answered','yes','person',30,12,22,'none');`)
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
	defer st.Close()
	ctx := context.Background()
	rows, err := st.Decisions(ctx, DecisionQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "linked" || rows[0].Answer != "yes" {
		t.Fatalf("linked decision after cleanup: %+v", rows)
	}
	for _, workID := range []any{nil, ""} {
		_, err = st.db.Exec(`INSERT INTO decisions
  (id,session,work_id,project,question,options,default_option,blocking,state,created_at,due_at,push)
VALUES ('again','session-a',?,'/p','No context','[]','yes',0,'open',40,50,'none')`, workID)
		if err == nil {
			t.Fatalf("work_id %v was accepted by the migrated schema", workID)
		}
	}
}

// A decisions table written before `withdrawn` is rebuilt to accept it with
// every row as it was; the answer rule still holds for the states that carry
// one, and opening it again changes nothing.
func TestAnOlderDecisionTableAcceptsWithdrawnAndKeepsItsRows(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, DBFile))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE decisions (
  id             TEXT    PRIMARY KEY,
  session        TEXT    NOT NULL CHECK (session <> ''),
  work_id        TEXT    NOT NULL CHECK (work_id <> ''),
  task_id        TEXT,
  project        TEXT    NOT NULL DEFAULT '',
  question       TEXT    NOT NULL CHECK (question <> ''),
  options        TEXT    NOT NULL CHECK (json_valid(options)),
  default_option TEXT    NOT NULL CHECK (default_option <> ''),
  blocking       INTEGER NOT NULL CHECK (blocking IN (0, 1)),
  state          TEXT    NOT NULL CHECK (state IN ('open','answered','defaulted')),
  answer         TEXT,
  answered_by    TEXT,
  answered_at    INTEGER,
  created_at     INTEGER NOT NULL,
  due_at         INTEGER NOT NULL,
  push           TEXT    NOT NULL CHECK (push IN
                   ('none','pending','sent','not_subscribed','over_budget','failed','unknown')),
  pushed_at      INTEGER,
  version        INTEGER NOT NULL DEFAULT 0,
  CHECK ((state = 'open') = (answer IS NULL)),
  CHECK (blocking = 1 OR push = 'none')
);
INSERT INTO decisions
  (id,session,work_id,project,question,options,default_option,blocking,state,answer,answered_by,
   answered_at,created_at,due_at,push,version)
VALUES
  ('waiting','session-a','work-a','/p','Still open','[{"id":"yes","label":"Yes"}]','yes',0,
   'open',NULL,NULL,NULL,10,20,'none',3),
  ('kept','session-a','work-a','/p','Kept answer','[{"id":"yes","label":"Yes"}]','yes',0,
   'answered','yes','person',30,12,22,'none',1);`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE decisions SET state = 'withdrawn' WHERE id = 'waiting'`); err == nil {
		t.Fatal("the older table already accepted withdrawn; this test proves nothing")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for open := 0; open < 2; open++ {
		st, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		rows, err := st.Decisions(ctx, DecisionQuery{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		byID := map[string]work.Decision{}
		for _, r := range rows {
			byID[r.ID] = r
		}
		if len(rows) != 2 || byID["kept"].Answer != "yes" || byID["kept"].State != work.DecisionAnswered ||
			byID["waiting"].Version != int64(3+open) {
			t.Fatalf("rows after opening %d: %+v", open+1, rows)
		}
		if open == 0 {
			err = st.WriteWork(ctx, func(tx *WorkTx) error {
				prev, err := tx.Decision("waiting")
				if err != nil {
					return err
				}
				next, ok := work.WithdrawDecision(prev, "session-a", time.Unix(40, 0))
				if !ok {
					t.Fatalf("an open decision was not withdrawable: %+v", prev)
				}
				return tx.PutDecision(next, &prev, 0)
			})
			if err != nil {
				t.Fatalf("withdrawn on the migrated table: %v", err)
			}
			if _, err := st.db.Exec(`UPDATE decisions SET answer = NULL WHERE id = 'kept'`); err == nil {
				t.Fatal("an answered decision without its answer was accepted")
			}
		} else if byID["waiting"].State != work.DecisionWithdrawn || byID["waiting"].Answer != "" {
			t.Fatalf("withdrawn row after reopening: %+v", byID["waiting"])
		}
		st.Close()
	}
}
