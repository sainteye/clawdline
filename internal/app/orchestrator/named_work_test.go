package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
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

// A dispatch may name a Board (v2) item: an Epic's owner dispatches the review
// of its plan with --work-id <item>. It is admitted onto that line when the
// item is in the dispatch's Project and open, refused by name otherwise, and
// the v1 board is not written for it.
func TestADispatchMayNameABoardItem(t *testing.T) {
	b, ctx, clock := newTodoBroker(t)
	project := t.TempDir()
	put := func(id, path string, phase work.Phase) {
		t.Helper()
		now := clock.now()
		it := work.ItemV2{ID: id, ProjectID: "p", ProjectPath: path, Kind: work.KindEpic, Title: "Epic",
			Description: "Large.", Phase: phase, DeploymentPolicy: work.DeployAgentDecides, CreatedBy: "local",
			CreatedAt: now, UpdatedAt: now, Cycle: 1, Version: 1}
		if err := b.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error { return tx.CreateItem(it, "local", "{}") }); err != nil {
			t.Fatal(err)
		}
	}
	open, elsewhere, closed := "0b0a0000-0000-4000-8000-0000000002a1", "0b0a0000-0000-4000-8000-0000000002a2",
		"0b0a0000-0000-4000-8000-0000000002a3"
	put(open, project, work.PhaseAssigned)
	put(elsewhere, t.TempDir(), work.PhaseAssigned)
	put(closed, project, work.PhaseCancelled)
	for _, c := range []struct{ name, task, workID, code string }{
		{"another project's item", "d15a0000-0000-4000-8000-000000000201", elsewhere, work.RefusedWorkOtherProject},
		{"a closed item", "d15a0000-0000-4000-8000-000000000202", closed, work.RefusedWorkClosed},
	} {
		writeBrief(t, b, c.task, project, map[string]any{"work_id": c.workID, "kind": "plan_review"})
		if _, err := b.Dispatch(ctx, DispatchRequest{TaskID: c.task, Secret: w1Secret}); refusalCode(err) != c.code {
			t.Fatalf("%s answered %v", c.name, err)
		}
	}
	id := "d15a0000-0000-4000-8000-000000000210"
	writeBrief(t, b, id, project, map[string]any{"work_id": open, "kind": "plan_review"})
	out, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret})
	if err != nil {
		t.Fatalf("a dispatch naming a Board item: %v", err)
	}
	if out.Record.WorkID != open || out.Record.WorkFrom != work.WorkNamed || out.Record.Kind != "plan_review" {
		t.Fatalf("the task: %s/%s kind %s", out.Record.WorkID, out.Record.WorkFrom, out.Record.Kind)
	}
	for _, w := range out.Warnings {
		if w.Code == "work_not_placed" {
			t.Fatalf("a Board item was treated as a missing v1 item: %v", w)
		}
	}
	if _, err := b.Store.WorkItem(ctx, open); err == nil {
		t.Fatal("the dispatch wrote a v1 item for a Board item")
	}
}

// One child may carry several Board items: --work-id repeated. The first stays
// the task's line; each of the rest is checked as work_id is, and the brief
// keeps them for a respawn. A list the broker cannot keep whole is refused
// whole, never trimmed.
func TestADispatchMayCarrySeveralBoardItems(t *testing.T) {
	b, ctx, clock := newTodoBroker(t)
	project := t.TempDir()
	put := func(id, path string, phase work.Phase) {
		t.Helper()
		now := clock.now()
		it := work.ItemV2{ID: id, ProjectID: "p", ProjectPath: path, Kind: work.KindIssue, Title: "Issue",
			Description: "Small.", Phase: phase, DeploymentPolicy: work.DeployAgentDecides, CreatedBy: "local",
			CreatedAt: now, UpdatedAt: now, Cycle: 1, Version: 1}
		if err := b.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error { return tx.CreateItem(it, "local", "{}") }); err != nil {
			t.Fatal(err)
		}
	}
	first, second, third := "0b0a0000-0000-4000-8000-0000000003a1", "0b0a0000-0000-4000-8000-0000000003a2",
		"0b0a0000-0000-4000-8000-0000000003a3"
	elsewhere, closed, absent := "0b0a0000-0000-4000-8000-0000000003a4", "0b0a0000-0000-4000-8000-0000000003a5",
		"0b0a0000-0000-4000-8000-0000000003a6"
	put(first, project, work.PhaseAssigned)
	put(second, project, work.PhaseImplementing)
	put(third, project, work.PhaseAssigned)
	put(elsewhere, t.TempDir(), work.PhaseAssigned)
	put(closed, project, work.PhaseDone)
	many := []string{}
	for i := 0; i <= MaxAlsoWorkIDs; i++ {
		many = append(many, "0b0a0000-0000-4000-8000-0000000004"+string(rune('0'+i))+"0")
	}
	for i, c := range []struct {
		name string
		work any
		also any
		code string
	}{
		{"another project's item", first, []string{elsewhere}, work.RefusedWorkOtherProject},
		{"a closed item", first, []string{closed}, work.RefusedWorkClosed},
		{"no such item", first, []string{absent}, "also_work_not_found"},
		{"work_id again", first, []string{first}, "bad_task"},
		{"twice", first, []string{second, second}, "bad_task"},
		{"no work_id", nil, []string{second}, "bad_task"},
		{"not a list", first, second, "bad_task"},
		{"too many", first, many, "bad_task"},
	} {
		task := "d15a0000-0000-4000-8000-00000000030" + string(rune('0'+i))
		writeBrief(t, b, task, project, map[string]any{"work_id": c.work, "also_work_ids": c.also})
		if _, err := b.Dispatch(ctx, DispatchRequest{TaskID: task, Secret: w1Secret}); refusalCode(err) != c.code {
			t.Fatalf("%s answered %v", c.name, err)
		}
	}
	id := "d15a0000-0000-4000-8000-000000000310"
	writeBrief(t, b, id, project, map[string]any{"work_id": first, "also_work_ids": []string{second, third}})
	out, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret})
	if err != nil {
		t.Fatalf("a dispatch carrying three items: %v", err)
	}
	if out.Record.WorkID != first || len(out.Record.AlsoWorkIDs) != 2 || out.Record.AlsoWorkIDs[0] != second ||
		out.Record.AlsoWorkIDs[1] != third || out.Record.Brief().AlsoWorkIDs[1] != third {
		t.Fatalf("the task: %s + %v (brief %v)", out.Record.WorkID, out.Record.AlsoWorkIDs, out.Record.Brief().AlsoWorkIDs)
	}
	for _, item := range []string{first, second, third} {
		var bound []store.BrokerRow
		if err := b.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
			var err error
			bound, err = tx.Tasks(item)
			return err
		}); err != nil || len(bound) != 1 || bound[0].ID != id {
			t.Fatalf("tasks of %s: %d rows, %v", item, len(bound), err)
		}
	}
}

// A dispatch bound to a planning-gated Epic whose latest plan review has a
// blocking finding is refused by name, and lists that finding; a new plan
// review is still dispatched, and a review with only non-blocking findings,
// or a legacy one with no severities, stops nothing.
func TestADispatchOnAPlanWithABlockingReviewIsRefused(t *testing.T) {
	b, ctx, clock := newTodoBroker(t)
	project := t.TempDir()
	put := func(item, review string, receipt string) {
		t.Helper()
		now := clock.now()
		it := work.ItemV2{ID: item, ProjectID: "p", ProjectPath: project, Kind: work.KindEpic, Title: "Epic",
			Description: "Large.", Phase: work.PhaseAssigned, DeploymentPolicy: work.DeployAgentDecides, CreatedBy: "local",
			CreatedAt: now, UpdatedAt: now, Cycle: 1, Version: 1, GateSnapshotCycle: 1, GateSnapshotAt: now,
			PlanningGate: true, AcceptanceCriteria: "It works."}
		rec := Record{ID: review, Kind: "plan_review", WorkID: item, State: StateSuccess, CreatedAt: now, ProjectDir: project,
			Result: &taskdir.Result{Status: "success", Summary: "Reviewed", Review: json.RawMessage(receipt)}}
		body, _ := json.Marshal(rec)
		if _, err := b.Store.CreateBrokerTask(ctx, store.BrokerRow{ID: review, Project: project, Assistant: "claude",
			State: string(StateSuccess), CreatedAt: now, SecretHash: "h", Record: body}, nil); err != nil {
			t.Fatal(err)
		}
		if err := b.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
			if err := tx.CreateItem(it, "local", "{}"); err != nil {
				return err
			}
			for n, role := range []string{work.DocumentPlan, work.DocumentPlanReview} {
				ref := ""
				if role == work.DocumentPlanReview {
					ref = review
				}
				if err := tx.AddDocument(work.DocumentV2{ID: item[:len(item)-1] + string(rune('a'+n)), WorkID: item, Role: role,
					Title: role, Reference: ref, Position: int64(n + 1), Version: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	receipt := func(severity string) string {
		sev := ""
		if severity != "" {
			sev = `"severity":"` + severity + `",`
		}
		return `{"verdict":"changes_required","axes":[{"axis":"specification","status":"findings","findings":[` +
			`{"id":"f-1",` + sev + `"summary":"No rollback for the migration","evidence":["plan.md:3"]}]},` +
			`{"axis":"repository_invariants","status":"pass","findings":[]},{"axis":"runtime_failure_behavior","status":"pass","findings":[]}]}`
	}
	blocked, nonBlocking, legacy := "0b0a0000-0000-4000-8000-0000000005a1", "0b0a0000-0000-4000-8000-0000000005b1",
		"0b0a0000-0000-4000-8000-0000000005c1"
	put(blocked, "7e000000-0000-4000-8000-0000000005a1", receipt("blocking"))
	put(nonBlocking, "7e000000-0000-4000-8000-0000000005b1", receipt("non_blocking"))
	put(legacy, "7e000000-0000-4000-8000-0000000005c1", receipt(""))

	task := "d15a0000-0000-4000-8000-000000000501"
	writeBrief(t, b, task, project, map[string]any{"work_id": blocked, "kind": "custom"})
	_, err := b.Dispatch(ctx, DispatchRequest{TaskID: task, Secret: w1Secret})
	if refusalCode(err) != "epic_plan_review_blocking" || !strings.Contains(err.Error(), "No rollback for the migration") {
		t.Fatalf("a dispatch past a blocking review answered %v", err)
	}
	for n, c := range []struct{ name, item, kind string }{
		{"a new plan review", blocked, "plan_review"},
		{"only non-blocking findings", nonBlocking, "custom"},
		{"a legacy receipt without severities", legacy, "custom"},
	} {
		task := "d15a0000-0000-4000-8000-00000000051" + string(rune('0'+n))
		writeBrief(t, b, task, project, map[string]any{"work_id": c.item, "kind": c.kind})
		if _, err := b.Dispatch(ctx, DispatchRequest{TaskID: task, Secret: w1Secret}); err != nil {
			t.Fatalf("%s was refused: %v", c.name, err)
		}
	}
}
