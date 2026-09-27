package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
)

// A store from before an Epic's owner could break it into child items gains
// the parent column and its index without losing an item; the old item has no
// parent, a new one keeps its parent and the Epic's provenance, a second open
// is a no-op, and Children counts all of an Epic's children and the open
// ones.
func TestOpeningAStoreFromBeforeEpicChildrenAddsTheParent(t *testing.T) {
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
  cycle INTEGER NOT NULL DEFAULT 1, version INTEGER NOT NULL DEFAULT 1, created_via TEXT NOT NULL DEFAULT '');
INSERT INTO work_v2_items
  (id,project_id,project_path,kind,title,description,phase,condition,user_action,deployment_policy,
   owner_session,created_by,created_at,updated_at,closed_at,cycle,version,created_via)
VALUES ('epic-a','p','/p','epic','Existing','Existing','implementing','','','agent_decides','conv','local',1,1,NULL,1,1,'')`)
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
		var index int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='work_v2_items_parent'`).Scan(&index); err != nil || index != 1 {
			t.Fatalf("open %d: parent index count %d, %v", round, index, err)
		}
		got, err := s.WorkV2Item(context.Background(), "epic-a")
		if err != nil || got.Title != "Existing" || got.ParentID != "" {
			t.Fatalf("open %d: existing item: %+v %v", round, got, err)
		}
		if round == 0 {
			via := &work.CreatedViaV2{Session: "conv", At: 5, Epic: "epic-a"}
			for n, phase := range []work.Phase{work.PhaseCreated, work.PhaseCancelled} {
				child := work.ItemV2{ID: "child-" + string(rune('a'+n)), ProjectID: "p", ProjectPath: "/p", Kind: work.KindFeature,
					Title: "Child", Description: "Child", Phase: phase, DeploymentPolicy: work.DeployAgentDecides,
					CreatedBy: work.EpicOwnerActor("conv"), CreatedAt: time.Unix(5, 0), UpdatedAt: time.Unix(5, 0),
					Cycle: 1, Version: 1, CreatedVia: via, ParentID: "epic-a"}
				if err := s.WriteWorkV2(context.Background(), func(tx *WorkV2Tx) error {
					return tx.CreateItem(child, child.CreatedBy, `{}`)
				}); err != nil {
					t.Fatal(err)
				}
			}
		}
		child, err := s.WorkV2Item(context.Background(), "child-a")
		if err != nil || child.ParentID != "epic-a" || child.CreatedVia == nil || child.CreatedVia.Epic != "epic-a" ||
			child.CreatedVia.Run != "" {
			t.Fatalf("open %d: child round trip: %+v %+v %v", round, child, child.CreatedVia, err)
		}
		var all, open int64
		if err := s.WriteWorkV2(context.Background(), func(tx *WorkV2Tx) error {
			var err error
			all, open, err = tx.Children("epic-a")
			return err
		}); err != nil || all != 2 || open != 1 {
			t.Fatalf("open %d: children all=%d open=%d err=%v", round, all, open, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
