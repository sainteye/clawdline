package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
)

// A store from before an assignment could name a persona gains the column
// empty: its assignments read as none, and a new one records the persona it
// opened a Session as.
func TestOpeningAStoreFromBeforePersonasAddsTheAssignmentsPersona(t *testing.T) {
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
CREATE TABLE work_v2_assignments (
  id TEXT PRIMARY KEY, work_id TEXT NOT NULL REFERENCES work_v2_items(id) ON DELETE CASCADE,
  mode TEXT NOT NULL CHECK (mode IN ('existing_session','new_session')),
  session_id TEXT NOT NULL DEFAULT '', terminal_id TEXT NOT NULL DEFAULT '', assistant TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '', state TEXT NOT NULL CHECK (state IN ('assigning','active','released','failed')),
  human_actor TEXT NOT NULL, root_assignment_id TEXT NOT NULL DEFAULT '', failure TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, released_at INTEGER,
  claimed_via TEXT NOT NULL DEFAULT '');
INSERT INTO work_v2_items
  (id,project_id,project_path,kind,title,description,phase,condition,user_action,deployment_policy,
   owner_session,created_by,created_at,updated_at,closed_at,cycle,version)
VALUES ('item-a','p','/p','issue','Existing','Existing','assigned','','','agent_decides','conv','local',1,1,NULL,1,2);
INSERT INTO work_v2_assignments
  (id,work_id,mode,session_id,terminal_id,assistant,model,state,human_actor,created_at,updated_at)
VALUES ('asg-a','item-a','new_session','conv','%1','claude','default','active','local',1,1)`)
	if closeErr := db.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if has, err := hasColumn(s.db, "work_v2_assignments", "persona"); err != nil || !has {
		t.Fatalf("persona migration: has=%v err=%v", has, err)
	}
	ctx := context.Background()
	got, err := s.WorkV2Assignments(ctx, "item-a")
	if err != nil || len(got) != 1 || got[0].ID != "asg-a" || got[0].Persona != "" {
		t.Fatalf("existing assignment after the migration: %+v %v", got, err)
	}
	now := time.Unix(5, 0)
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		return tx.CreateAssignment(work.AssignmentV2{ID: "asg-b", WorkID: "item-a", Mode: "new_session",
			Assistant: "codex", Model: "default", State: "failed", HumanActor: "local", CreatedAt: now, UpdatedAt: now,
			Persona: "reality-checker"})
	}); err != nil {
		t.Fatal(err)
	}
	got, err = s.WorkV2Assignments(ctx, "item-a")
	if err != nil || len(got) != 2 || got[1].ID != "asg-b" || got[1].Persona != "reality-checker" {
		t.Fatalf("persona round trip: %+v %v", got, err)
	}
}
