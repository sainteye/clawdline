package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
)

func TestOpeningAnOlderWorkStoreAddsTheRequestedUserAction(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, DBFile))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE work_v2_items (
      id TEXT PRIMARY KEY, project_id TEXT NOT NULL, project_path TEXT NOT NULL, kind TEXT NOT NULL,
      title TEXT NOT NULL, description TEXT NOT NULL, phase TEXT NOT NULL, condition TEXT NOT NULL DEFAULT '',
      deployment_policy TEXT NOT NULL, owner_session TEXT NOT NULL DEFAULT '', created_by TEXT NOT NULL,
      created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, closed_at INTEGER, cycle INTEGER NOT NULL DEFAULT 1,
      version INTEGER NOT NULL DEFAULT 1)`)
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
	has, err := hasColumn(s.db, "work_v2_items", "user_action")
	if err != nil || !has {
		t.Fatalf("user_action migration: has=%v err=%v", has, err)
	}
}

func TestOpeningAnOlderWorkStoreAddsCompletionReportsWithoutLosingDocuments(t *testing.T) {
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
CREATE TABLE work_v2_documents (
  id TEXT PRIMARY KEY, work_id TEXT NOT NULL REFERENCES work_v2_items(id) ON DELETE CASCADE,
  role TEXT NOT NULL CHECK (role IN ('spec','design','test','deploy','other')),
  title TEXT NOT NULL, body TEXT NOT NULL DEFAULT '', reference TEXT NOT NULL DEFAULT '',
  position INTEGER NOT NULL, version INTEGER NOT NULL DEFAULT 1, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
INSERT INTO work_v2_items
  (id,project_id,project_path,kind,title,description,phase,condition,user_action,deployment_policy,
   owner_session,created_by,created_at,updated_at,closed_at,cycle,version)
VALUES ('item-a','p','/p','issue','Existing','Existing','created','','','agent_decides','','local',1,1,NULL,1,1);
INSERT INTO work_v2_documents (id,work_id,role,title,body,reference,position,version,created_at,updated_at)
VALUES ('doc-a','item-a','test','Existing test','green','',0,1,1,1)`)
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
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM work_v2_documents WHERE id='doc-a' AND role='test'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("preserved document: count=%d err=%v", count, err)
	}
	_, err = s.db.Exec(`INSERT INTO work_v2_documents
      (id,work_id,role,title,body,reference,position,version,created_at,updated_at)
      VALUES ('doc-b','item-a','completion_report','Completion report','root cause','',1,1,2,2)`)
	if err != nil {
		t.Fatalf("completion_report after migration: %v", err)
	}
}

func v2Item(id string, at time.Time) work.ItemV2 {
	return work.ItemV2{ID: id, ProjectID: "project-a", ProjectPath: "/project-a", Kind: work.KindFeature,
		Title: "Visible feature", Description: "What should change", Phase: work.PhaseCreated,
		DeploymentPolicy: work.DeployAgentDecides, CreatedBy: "local", CreatedAt: at, UpdatedAt: at,
		Cycle: 1, Version: 1}
}

func TestWorkV2StoresVersionedItemsAndAppendOnlyEvents(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	at := time.Unix(1_790_000_000, 0)
	i := v2Item("10000000-0000-4000-8000-000000000001", at)
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		return tx.CreateItem(i, "local", `{"kind":"feature"}`)
	}); err != nil {
		t.Fatal(err)
	}
	held, err := s.WorkV2Item(ctx, i.ID)
	if err != nil {
		t.Fatal(err)
	}
	next := held
	next.Title, next.UpdatedAt = "Renamed", at.Add(time.Second)
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		return tx.PutItem(held, next, "item.edited", "session-a", `{}`)
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		return tx.PutItem(held, next, "item.edited", "session-a", `{}`)
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale write answered %v", err)
	}
	events, err := s.WorkV2Events(ctx, i.ID, 0, 10)
	if err != nil || len(events) != 2 || events[1].PreviousVersion != 1 || events[1].NextVersion != 2 {
		t.Fatalf("events %+v, %v", events, err)
	}
	if _, err := s.db.Exec(`UPDATE work_v2_events SET actor='somebody'`); err == nil {
		t.Fatal("event history was editable")
	}
}

func TestWorkV2ListFiltersLifecycleAndSearchesTitleOrDescription(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	at := time.Unix(1_790_000_000, 0)
	open := v2Item("11000000-0000-4000-8000-000000000001", at)
	open.Title = "Search the Board"
	done := v2Item("11000000-0000-4000-8000-000000000002", at.Add(time.Second))
	done.Title, done.Description, done.Phase = "Finished migration", "The archive needle is here", work.PhaseDone
	done.ClosedAt = at.Add(2 * time.Second)
	cancelled := v2Item("11000000-0000-4000-8000-000000000003", at.Add(3*time.Second))
	cancelled.Title, cancelled.Phase, cancelled.ClosedAt = "Abandoned needle", work.PhaseCancelled, at.Add(4*time.Second)
	for _, item := range []work.ItemV2{open, done, cancelled} {
		item := item
		if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error { return tx.CreateItem(item, "local", `{}`) }); err != nil {
			t.Fatal(err)
		}
	}

	rows, truncated, err := s.WorkV2Items(ctx, "", "", "done", "NEEDLE", 20)
	if err != nil || truncated || len(rows) != 1 || rows[0].ID != done.ID {
		t.Fatalf("completed search: %+v truncated=%v err=%v", rows, truncated, err)
	}
	rows, truncated, err = s.WorkV2Items(ctx, "", "", "open", "board", 20)
	if err != nil || truncated || len(rows) != 1 || rows[0].ID != open.ID {
		t.Fatalf("open search: %+v truncated=%v err=%v", rows, truncated, err)
	}
	rows, truncated, err = s.WorkV2Items(ctx, "", "", "all", "needle", 20)
	if err != nil || truncated || len(rows) != 2 || rows[0].ID != cancelled.ID || rows[1].ID != done.ID {
		t.Fatalf("all search: %+v truncated=%v err=%v", rows, truncated, err)
	}
}

func TestWorkV2RootAssignmentIsFoundByConversation(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	at := time.Unix(1_790_000_000, 0)
	item := v2Item("10000000-0000-4000-8000-000000000099", at)
	item.Title = "Fix the Session title"
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		if err := tx.CreateItem(item, "local", `{}`); err != nil {
			return err
		}
		return tx.CreateAssignment(work.AssignmentV2{ID: "assignment-a", WorkID: item.ID,
			Mode: "new_session", SessionID: "session-a", Assistant: "codex", State: "active",
			HumanActor: "local", RootAssignment: "root-a", CreatedAt: at, UpdatedAt: at})
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.WorkV2RootAssignmentForSession(ctx, "/project-a", "codex", "session-a")
	if err != nil || got != "root-a" {
		t.Fatalf("Root Assignment %q, %v", got, err)
	}
	if got, err := s.WorkV2RootAssignmentForSession(ctx, "/another-project", "codex", "session-a"); err != nil || got != "" {
		t.Fatalf("other project Root Assignment %q, %v", got, err)
	}
}

func TestDirectTodoReadMarksOnlyItsSessionAndDeleteRemovesText(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	at := time.Unix(1_790_000_000, 0)
	rows := []work.DirectTodoV2{
		{ID: "todo-a", SessionID: "session-a", Text: "first", CreatedBy: "local", CreatedAt: at, Version: 1},
		{ID: "todo-b", SessionID: "session-b", Text: "other", CreatedBy: "local", CreatedAt: at, Version: 1},
	}
	for _, row := range rows {
		if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error { return tx.CreateDirectTodo(row) }); err != nil {
			t.Fatal(err)
		}
	}
	got, _, err := s.DirectTodosV2(ctx, "session-a", false, true, 20)
	if err != nil || len(got) != 1 || got[0].ReadAt.IsZero() {
		t.Fatalf("read %+v, %v", got, err)
	}
	other, _, _ := s.DirectTodosV2(ctx, "session-b", false, false, 20)
	if len(other) != 1 || !other[0].ReadAt.IsZero() {
		t.Fatalf("another session was marked: %+v", other)
	}
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		deleted, err := tx.DeleteDirectTodo("todo-a", "session-a")
		if !deleted && err == nil {
			t.Fatal("todo was not deleted")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.DirectTodosV2(ctx, "session-a", true, false, 20)
	if len(got) != 0 {
		t.Fatalf("deleted text remained: %+v", got)
	}
}

func TestPersonTodoReadKeepsCompletedRowsAfterOpenRows(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	at := time.Unix(1_790_000_000, 0)
	completed := work.DirectTodoV2{ID: "done", SessionID: "session-a", Text: "finished",
		CreatedBy: "local", CreatedAt: at, Version: 1}
	open := work.DirectTodoV2{ID: "open", SessionID: "session-a", Text: "next",
		CreatedBy: "local", CreatedAt: at.Add(time.Minute), Version: 1}
	for _, row := range []work.DirectTodoV2{completed, open} {
		if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error { return tx.CreateDirectTodo(row) }); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		prev, err := tx.DirectTodo(completed.ID)
		if err != nil {
			return err
		}
		next := prev
		next.CompletedAt, next.CompletedBy = at.Add(2*time.Minute), "session-a"
		return tx.PutDirectTodo(prev, next)
	}); err != nil {
		t.Fatal(err)
	}
	rows, truncated, err := s.DirectTodosV2(ctx, "session-a", true, false, 20)
	if err != nil || truncated || len(rows) != 2 || rows[0].ID != open.ID || rows[1].ID != completed.ID {
		t.Fatalf("person read: %+v truncated=%v err=%v", rows, truncated, err)
	}
}

func TestDirectTodoImagesStayWithTheTodoAndShareTheWorkImageBudget(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	at := time.Unix(1_790_000_000, 0)
	todo := work.DirectTodoV2{ID: "todo-images", SessionID: "session-a", Text: "inspect this",
		CreatedBy: "local", CreatedAt: at, Version: 1}
	image := work.DirectTodoImageV2{ID: "31000000-0000-4000-8000-000000000001", TodoID: todo.ID,
		Title: "failure.png", MediaType: "image/png", Width: 80, Height: 40, Position: 0,
		CreatedBy: "local", CreatedAt: at}
	data := []byte("normalized todo png")
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		if err := tx.CreateDirectTodo(todo); err != nil {
			return err
		}
		return tx.AddDirectTodoImage(image, data)
	}); err != nil {
		t.Fatal(err)
	}
	images, err := s.DirectTodoV2Images(ctx, todo.ID)
	if err != nil || len(images) != 1 || images[0].Title != image.Title || images[0].ByteCount != int64(len(data)) {
		t.Fatalf("metadata: %+v, %v", images, err)
	}
	got, ok, err := s.WorkV2ImageBytes(ctx, image.ID)
	if err != nil || !ok || string(got) != string(data) {
		t.Fatalf("bytes: %q %v %v", got, ok, err)
	}
	counts, err := s.WorkV2CapacityCounts(ctx)
	if err != nil || counts["images_per_item"] != 1 || counts["image_bytes_total"] != int64(len(data)) {
		t.Fatalf("capacity: %+v, %v", counts, err)
	}
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		deleted, err := tx.DeleteDirectTodo(todo.ID, todo.SessionID)
		if !deleted && err == nil {
			t.Fatal("todo was not deleted")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.WorkV2ImageBytes(ctx, image.ID); err != nil || ok {
		t.Fatalf("deleted todo left image bytes: %v %v", ok, err)
	}
}

func TestWorkV2ReferenceImagesKeepMetadataAndBytesTogether(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	at := time.Unix(1_790_000_000, 0)
	item := v2Item("30000000-0000-4000-8000-000000000001", at)
	image := work.ImageV2{ID: "30000000-0000-4000-8000-000000000002", WorkID: item.ID, Title: "checkout.png",
		MediaType: "image/png", Width: 80, Height: 40, Position: 2, CreatedBy: "local", CreatedAt: at}
	data := []byte("normalized png bytes")
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		if err := tx.CreateItem(item, "local", `{}`); err != nil {
			return err
		}
		return tx.AddImage(image, data)
	}); err != nil {
		t.Fatal(err)
	}
	images, err := s.WorkV2Images(ctx, item.ID)
	if err != nil || len(images) != 1 || images[0].Title != image.Title || images[0].ByteCount != int64(len(data)) {
		t.Fatalf("metadata: %+v, %v", images, err)
	}
	got, ok, err := s.WorkV2ImageBytes(ctx, image.ID)
	if err != nil || !ok || string(got) != string(data) {
		t.Fatalf("bytes: %q %v %v", got, ok, err)
	}
	counts, err := s.WorkV2CapacityCounts(ctx)
	if err != nil || counts["images_per_item"] != 1 || counts["image_bytes_total"] != int64(len(data)) {
		t.Fatalf("capacity: %+v, %v", counts, err)
	}
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error { return tx.DeleteImage(image.ID) }); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.WorkV2ImageBytes(ctx, image.ID); err != nil || ok {
		t.Fatalf("deleted image still readable: %v %v", ok, err)
	}
}

func TestWorkV2ReferenceImageCountRefusesWithoutEvicting(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	at := time.Unix(1_790_000_000, 0)
	item := v2Item("31000000-0000-4000-8000-000000000001", at)
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error { return tx.CreateItem(item, "local", `{}`) }); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < WorkV2ImageLimit; n++ {
		id := fmt.Sprintf("31000000-0000-4000-8000-%012d", n+2)
		err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
			return tx.AddImage(work.ImageV2{ID: id, WorkID: item.ID, Title: id, MediaType: "image/png",
				Width: 1, Height: 1, CreatedBy: "local", CreatedAt: at}, []byte{byte(n)})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	err = s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		return tx.AddImage(work.ImageV2{ID: "31000000-0000-4000-8000-000000000099", WorkID: item.ID,
			Title: "too many", MediaType: "image/png", Width: 1, Height: 1, CreatedBy: "local", CreatedAt: at}, []byte{9})
	})
	if !errors.Is(err, ErrWorkV2ImagesFull) {
		t.Fatalf("seventh image answered %v", err)
	}
	images, _ := s.WorkV2Images(ctx, item.ID)
	if len(images) != WorkV2ImageLimit {
		t.Fatalf("limit evicted an existing image: %d", len(images))
	}
}

func TestTheExplicitV1ResetDoesNotTouchV2OrBrokerHistory(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	at := time.Unix(1_790_000_000, 0)
	old := work.Item{ID: "20000000-0000-4000-8000-000000000001", Project: "/old", Title: "old",
		CreatedAt: at, CreatedBy: "user", Place: work.PlaceBacklog, State: work.ItemPlanned, PlacedAt: at}
	if err := s.WriteWork(ctx, func(tx *WorkTx) error {
		return tx.Create(old, MoveOf(old.ID, work.Change{From: work.PlaceNone, To: work.PlaceBacklog,
			State: work.ItemPlanned, Trigger: "created", Actor: "user"}, at), 0)
	}); err != nil {
		t.Fatal(err)
	}
	newItem := v2Item("20000000-0000-4000-8000-000000000002", at)
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error { return tx.CreateItem(newItem, "local", `{}`) }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO broker_tasks
      (id,project,repository,assistant,state,created_at,updated_at,secret_hash,record,version) VALUES
      ('task-kept','/kept','','codex','queued',?,?,'hash','{}',0)`, at.Unix(), at.Unix()); err != nil {
		t.Fatal(err)
	}
	counts, err := s.ResetWorkV1(ctx)
	if err != nil || counts["work"] != 1 || counts["moves"] != 1 {
		t.Fatalf("reset %+v, %v", counts, err)
	}
	if _, err := s.WorkV2Item(ctx, newItem.ID); err != nil {
		t.Fatalf("v2 item was touched: %v", err)
	}
	var tasks int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM broker_tasks WHERE id='task-kept'`).Scan(&tasks); err != nil || tasks != 1 {
		t.Fatalf("broker history was touched: %d %v", tasks, err)
	}
}
