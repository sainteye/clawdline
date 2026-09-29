package store

import (
	"context"
	"testing"
)

func TestCloseResponsibilityReadSeparatesOpenFromCompletedWork(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	statements := []string{
		`INSERT INTO work_v2_items
 (id,project_id,project_path,kind,title,description,phase,deployment_policy,owner_session,created_by,created_at,updated_at)
 VALUES ('open-board','p','/p','issue','title','body','implementing','not_required','active','test',1,1),
 ('done-board','p','/p','issue','title','body','done','not_required','finished','test',1,1)`,
		`INSERT INTO session_direct_todos (id,session_id,text,created_by,created_at,completed_at)
 VALUES ('open-direct','active','do it','test',1,NULL),('done-direct','finished','done','test',1,2)`,
		`INSERT INTO todos (id,owner_session,origin,state,reason,created_at,updated_at)
 VALUES ('open-dispatch','active','dispatch','open','dispatched',1,1),
 ('done-dispatch','finished','dispatch','done','landed',1,2)`,
	}
	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.OpenSessionResponsibilities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"board_item_open:open-board": true,
		"session_todo_open:open-direct": true, "dispatch_todo_open:open-dispatch": true}
	if len(got) != len(want) {
		t.Fatalf("unfinished = %+v", got)
	}
	for _, item := range got {
		if item.Session != "active" || !want[item.Code+":"+item.ID] {
			t.Fatalf("unexpected responsibility %+v", item)
		}
	}
}
