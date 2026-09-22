package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
)

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
