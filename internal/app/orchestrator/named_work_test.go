package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// BD-4: a dispatch that names a work item says which of a person's lines it
// is serving, and the broker holds it to that — the name is checked before
// anything exists, and the item is on the board by the time the tab opens.
// Before this, `work_id` was checked for shape alone: 52 dispatches in one
// day named nothing, the board stayed empty, and the three items that were
// finished could only leave it as "a person dropped it".

// plannedItem puts one planned Backlog item in the store.
func plannedItem(t *testing.T, b *Broker, ctx context.Context, id, project string, at time.Time) work.Item {
	t.Helper()
	it := work.Item{ID: id, Project: project, Title: "the work", CreatedAt: at, CreatedBy: "user",
		Place: work.PlaceBacklog, State: work.ItemPlanned, PlacedAt: at}
	c := work.Change{From: work.PlaceNone, To: work.PlaceBacklog, State: work.ItemPlanned,
		Trigger: "created", Actor: "user", Evidence: map[string]any{"op": "create"}}
	err := b.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		return tx.Create(it, store.MoveOf(id, c, at), 0)
	})
	if err != nil {
		t.Fatalf("the Backlog item: %v", err)
	}
	return it
}

func TestADispatchThatNamesAWorkItemCommitsToIt(t *testing.T) {
	b, ctx, clock := newTodoBroker(t)
	project := t.TempDir()
	item := "0b0a0000-0000-4000-8000-0000000000a1"
	plannedItem(t, b, ctx, item, project, clock.now())

	// Refused, each by its own name, and nothing is started by any of them.
	cases := []struct {
		name, task, workID, code string
	}{
		{"no such item", "d15a0000-0000-4000-8000-000000000001",
			"0b0a0000-0000-4000-8000-00000000dead", work.RefusedWorkNotFound},
		{"another project's item", "d15a0000-0000-4000-8000-000000000002", item, work.RefusedWorkOtherProject},
	}
	for _, c := range cases {
		dir := project
		if c.code == work.RefusedWorkOtherProject {
			dir = t.TempDir()
		}
		writeBrief(t, b, c.task, dir, map[string]any{"work_id": c.workID})
		_, err := b.Dispatch(ctx, DispatchRequest{TaskID: c.task, Secret: w1Secret})
		if refusalCode(err) != c.code {
			t.Fatalf("%s answered %v", c.name, err)
		}
		if _, _, err := b.Record(ctx, c.task); !isNotFound(err) {
			t.Fatalf("%s: the task was recorded anyway", c.name)
		}
	}

	// Admitted: the record is on that line, and the item is on the board with
	// the move that says which root committed to it.
	id := "d15a0000-0000-4000-8000-000000000010"
	writeBrief(t, b, id, project, map[string]any{"work_id": item})
	out, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret})
	if err != nil {
		t.Fatalf("a dispatch naming a planned item: %v", err)
	}
	if out.Record.WorkID != item || out.Record.WorkFrom != work.WorkNamed {
		t.Fatalf("the task's line: %s/%s", out.Record.WorkID, out.Record.WorkFrom)
	}
	it, err := b.Store.WorkItem(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if it.Place != work.PlaceBoard || it.State != work.ItemActive || it.Commitment != work.CommitDispatch ||
		it.Owner != rootConversation {
		t.Fatalf("the item the dispatch named: %+v", it)
	}
	rows, err := b.Store.WorkMoves(ctx, item, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	last := rows[len(rows)-1]
	if last.Trigger != work.TriggerDispatched || last.Actor != "root:"+rootConversation ||
		last.To != work.PlaceBoard {
		t.Fatalf("the move: %+v", last)
	}

	// A second dispatch on the same item moves nothing twice: it is already
	// committed, and the facts of both tasks are its own.
	again := "d15a0000-0000-4000-8000-000000000011"
	writeBrief(t, b, again, project, map[string]any{"work_id": item})
	if _, err := b.Dispatch(ctx, DispatchRequest{TaskID: again, Secret: w1Secret}); err != nil {
		t.Fatalf("a second dispatch naming the same item: %v", err)
	}
	if n, err := b.Store.WorkMoves(ctx, item, 0, 10); err != nil || len(n) != len(rows) {
		t.Fatalf("a second dispatch wrote another move: %d then %d (%v)", len(rows), len(n), err)
	}

	// And the item that is finished refuses the next dispatch by name rather
	// than quietly reopening: a closed item is not a line to send work to.
	err = b.Store.WriteWork(ctx, func(tx *store.WorkTx) error {
		cur, err := tx.Item(item)
		if err != nil {
			return err
		}
		c := work.Change{From: work.PlaceBoard, To: work.PlaceBoard, State: work.ItemDone,
			ClosedReason: work.ClosedDoneElsewhere, Trigger: string(work.OpDoneElsewhere), Actor: "user",
			Evidence: map[string]any{"reason": "done by hand"}}
		return tx.Put(cur, c.Apply(cur, clock.now()), store.MoveOf(item, c, clock.now()))
	})
	if err != nil {
		t.Fatal(err)
	}
	shut := "d15a0000-0000-4000-8000-000000000012"
	writeBrief(t, b, shut, project, map[string]any{"work_id": item})
	if _, err := b.Dispatch(ctx, DispatchRequest{TaskID: shut, Secret: w1Secret}); refusalCode(err) != work.RefusedWorkClosed {
		t.Fatalf("naming a closed item answered %v", err)
	}

	// Control: a dispatch that names nothing is admitted exactly as before,
	// on a line of its own, and puts nothing on anybody's board.
	plain := "d15a0000-0000-4000-8000-000000000020"
	writeBrief(t, b, plain, project, nil)
	out, err = b.Dispatch(ctx, DispatchRequest{TaskID: plain, Secret: w1Secret})
	if err != nil {
		t.Fatalf("a dispatch naming no item: %v", err)
	}
	if out.Record.WorkID != plain || out.Record.WorkFrom != work.WorkDispatch {
		t.Fatalf("a dispatch naming no item is on %s/%s", out.Record.WorkID, out.Record.WorkFrom)
	}
	if _, err := b.Store.WorkItem(ctx, plain); err == nil {
		t.Fatal("a dispatch naming no item made a work item")
	}
}
