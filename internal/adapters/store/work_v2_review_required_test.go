package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
)

// A store from before the person's "Needs independent review" switch gains
// the column without losing an item; the old Feature reads unchecked, a
// checked Feature round-trips through create and update, and a second open is
// a no-op.
func TestOpeningAStoreFromBeforeReviewRequiredReadsUnchecked(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, DBFile))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE work_v2_items (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL, project_path TEXT NOT NULL, kind TEXT NOT NULL,
  title TEXT NOT NULL, description TEXT NOT NULL, phase TEXT NOT NULL, condition TEXT NOT NULL DEFAULT '',
  user_action TEXT NOT NULL DEFAULT '', deployment_policy TEXT NOT NULL, owner_session TEXT NOT NULL DEFAULT '',
  created_by TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, closed_at INTEGER,
  cycle INTEGER NOT NULL DEFAULT 1, version INTEGER NOT NULL DEFAULT 1);
INSERT INTO work_v2_items
  (id,project_id,project_path,kind,title,description,phase,condition,user_action,deployment_policy,
   owner_session,created_by,created_at,updated_at,closed_at,cycle,version)
VALUES ('feature-a','p','/p','feature','Existing','Existing','created','','','required','','local',1,1,NULL,1,1)`)
	if closeErr := db.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 2; round++ {
		s, err := Open(dir)
		if err != nil {
			t.Fatalf("open %d: %v", round, err)
		}
		got, err := s.WorkV2Item(context.Background(), "feature-a")
		if err != nil || got.Title != "Existing" || got.ReviewRequired {
			t.Fatalf("open %d: existing Feature after the migration: %+v %v", round, got, err)
		}
		if round == 1 {
			s.Close()
			break
		}
		item := work.ItemV2{ID: "feature-b", ProjectID: "p", ProjectPath: "/p", Kind: work.KindFeature, Title: "New",
			Description: "New", Phase: work.PhaseCreated, DeploymentPolicy: work.DeployAgentDecides, ReviewRequired: true,
			CreatedBy: "local", CreatedAt: time.Unix(5, 0), UpdatedAt: time.Unix(5, 0), Cycle: 1, Version: 1}
		if err := s.WriteWorkV2(context.Background(), func(tx *WorkV2Tx) error { return tx.CreateItem(item, "local", `{}`) }); err != nil {
			t.Fatal(err)
		}
		created, err := s.WorkV2Item(context.Background(), "feature-b")
		if err != nil || !created.ReviewRequired {
			t.Fatalf("checked Feature round trip: %+v %v", created, err)
		}
		next := created
		next.ReviewRequired = false
		if err := s.WriteWorkV2(context.Background(), func(tx *WorkV2Tx) error {
			return tx.PutItem(created, next, "item.edited", "local", `{}`)
		}); err != nil {
			t.Fatal(err)
		}
		if updated, err := s.WorkV2Item(context.Background(), "feature-b"); err != nil || updated.ReviewRequired {
			t.Fatalf("unchecking did not persist: %+v %v", updated, err)
		}
		s.Close()
	}
}
