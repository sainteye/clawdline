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

// An item still in `assigned` with no step done is the one kind a close may
// release; one moved on, or with a step done, is started and stays open.
func TestCloseResponsibilityReadSeparatesUnstartedBoardItems(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	statements := []string{
		`INSERT INTO work_v2_items
 (id,project_id,project_path,kind,title,description,phase,deployment_policy,owner_session,created_by,created_at,updated_at,version)
 VALUES ('fresh','p','/p','issue','t','b','assigned','not_required','s1','test',1,1,4),
 ('stepped','p','/p','issue','t','b','assigned','not_required','s1','test',1,1,1),
 ('moving','p','/p','issue','t','b','implementing','not_required','s1','test',1,1,1),
 ('elsewhere','p','/p','issue','t','b','assigned','not_required','s2','test',1,1,1)`,
		`INSERT INTO work_v2_steps (id,work_id,title,done,position,created_by,created_at)
 VALUES ('a','stepped','one',1,0,'test',1),('b','fresh','two',0,0,'test',1)`,
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
	codes := map[string]string{}
	for _, item := range got {
		codes[item.ID] = item.Code
	}
	want := map[string]string{"fresh": "board_item_unstarted", "stepped": "board_item_open",
		"moving": "board_item_open", "elsewhere": "board_item_unstarted"}
	if len(codes) != len(want) {
		t.Fatalf("codes = %v", codes)
	}
	for id, code := range want {
		if codes[id] != code {
			t.Fatalf("%s = %q, want %q (all %v)", id, codes[id], code, codes)
		}
	}
	items, err := s.UnstartedBoardItems(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "fresh" || items[0].Version != 4 {
		t.Fatalf("unstarted = %+v", items)
	}
}
