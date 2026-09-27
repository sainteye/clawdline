package app

import (
	"context"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// epicClock is a clock a test moves, so a plan and a review can be written
// in the same second or in different ones.
type epicClock struct{ at time.Time }

func (c *epicClock) now() time.Time { return c.at }

func newEpicTest(t *testing.T) (*WorkSystemV2, *epicClock) {
	t.Helper()
	w := newWorkV2Test(t)
	clock := &epicClock{at: time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)}
	w.Now = clock.now
	return w, clock
}

// assignedEpic is an Epic owned by session-a, in assigned.
func assignedEpic(t *testing.T, w *WorkSystemV2) WorkV2View {
	t.Helper()
	v := createWorkV2Test(t, w, work.KindEpic)
	assigned, err := w.Assign(context.Background(), v.Item.ID, AssignWorkV2{ExpectedVersion: v.Item.Version,
		Mode: "existing_session", SessionID: "session-a", Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	return assigned
}

func addDoc(w *WorkSystemV2, item *WorkV2View, role, reference string) error {
	v, err := w.AddDocument(context.Background(), item.Item.ID, AddDocumentV2{ExpectedVersion: item.Item.Version,
		SessionID: "session-a", Role: role, Title: role, Body: "What it says", Reference: reference}, nil)
	if err == nil {
		item.Item = v.Item
	}
	return err
}

// reviewTask is a finished plan_review child of session-a on item.
func reviewTask(id, item string, at time.Time) orchestrator.Record {
	return orchestrator.Record{ID: id, Kind: "plan_review", WorkID: item, State: orchestrator.StateSuccess,
		CreatedAt: at, ProjectDir: "/p", Root: &orchestrator.RootRef{SessionID: "session-a", Assistant: "claude"}}
}

func advanceTo(w *WorkSystemV2, item *WorkV2View, next work.Phase) error {
	v, err := w.Advance(context.Background(), item.Item.ID, AdvanceWorkV2{ExpectedVersion: item.Item.Version,
		SessionID: "session-a", Next: next, Actor: "session-a"}, nil)
	if err == nil {
		item.Item = v.Item
	}
	return err
}

// An Epic is assigned like a Feature and lands in its lifecycle lanes;
// Refactor and Plan are still refused.
func TestAnEpicIsAssignableAndRefactorAndPlanAreNot(t *testing.T) {
	w, _ := newEpicTest(t)
	epic := createWorkV2Test(t, w, work.KindEpic)
	if epic.Item.Area() != "unassigned" {
		t.Fatalf("a new Epic is in %q, want unassigned", epic.Item.Area())
	}
	assigned := assignedEpic(t, w)
	if assigned.Item.OwnerSession != "session-a" || assigned.Item.Phase != work.PhaseAssigned ||
		assigned.Item.Area() != "assigned" {
		t.Fatalf("assigned Epic: %+v", assigned.Item)
	}
	for _, kind := range []work.Kind{work.KindRefactor, work.KindPlan} {
		v := createWorkV2Test(t, w, kind)
		_, err := w.Assign(context.Background(), v.Item.ID, AssignWorkV2{ExpectedVersion: v.Item.Version,
			Mode: "existing_session", SessionID: "session-a", Actor: "local"}, false, nil)
		refusedAsWork(t, err, "planning_not_assignable")
	}
}

// A Session's `item add --kind epic` arrives assigned to it with its steps,
// as a Feature does; a Refactor still takes none.
func TestAnEpicFromASessionArrivesAssignedWithItsSteps(t *testing.T) {
	w, _ := newEpicTest(t)
	run := sessionItemRun(t, w, "conv-a")
	v, err := w.CreateFromSession(context.Background(), newSessionItem(run, work.KindEpic, "big", "plan", "build"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.Item.OwnerSession != "conv-a" || v.Item.Phase != work.PhaseAssigned || len(v.Steps) != 2 {
		t.Fatalf("Epic from a Session: %+v steps=%d", v.Item, len(v.Steps))
	}
	_, err = w.CreateFromSession(context.Background(), newSessionItem(run, work.KindRefactor, "r", "a"), nil)
	refusedAsWork(t, err, "planning_has_no_steps")
}

// plan and plan_review belong to an Epic; on a Feature they are refused,
// and the other roles stay where they were.
func TestPlanDocumentsBelongToAnEpicOnly(t *testing.T) {
	w, _ := newEpicTest(t)
	feature := createWorkV2Test(t, w, work.KindFeature)
	owned, err := w.Assign(context.Background(), feature.Item.ID, AssignWorkV2{ExpectedVersion: feature.Item.Version,
		Mode: "existing_session", SessionID: "session-a", Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{work.DocumentPlan, work.DocumentPlanReview} {
		refusedAsWork(t, addDoc(w, &owned, role, ""), "document_role_not_applicable")
	}
	if err := addDoc(w, &owned, "completion_report", ""); err != nil {
		t.Fatalf("completion_report on a Feature: %v", err)
	}
	refusedAsWork(t, addDoc(w, &owned, "wish", ""), "invalid_document_role")
	epic := assignedEpic(t, w)
	if err := addDoc(w, &epic, work.DocumentPlan, ""); err != nil {
		t.Fatalf("plan on an Epic: %v", err)
	}
}

// A plan_review is accepted only when its reference is a finished
// plan_review child of the owner, on this item, dispatched after the plan.
func TestAPlanReviewMustBeARealChildReview(t *testing.T) {
	w, clock := newEpicTest(t)
	ctx := context.Background()
	noPlan := assignedEpic(t, w)
	putTask(t, w.Store, reviewTask("7e000000-0000-4000-8000-000000000000", noPlan.Item.ID, clock.at))
	refusedAsWork(t, addDoc(w, &noPlan, work.DocumentPlanReview, "7e000000-0000-4000-8000-000000000000"),
		"epic_plan_required")

	epic := assignedEpic(t, w)
	if err := addDoc(w, &epic, work.DocumentPlan, ""); err != nil {
		t.Fatal(err)
	}
	planAt := clock.at
	clock.at = clock.at.Add(time.Minute)
	other := "7e000000-0000-4000-8000-0000000000ff"
	cases := []struct {
		name, code string
		ref        string
		task       func(r *orchestrator.Record)
	}{
		{"not a task id", "plan_review_task_unknown", "the review", nil},
		{"no such task", "plan_review_task_unknown", "7e000000-0000-4000-8000-00000000abcd", nil},
		{"another Session's", "plan_review_task_not_owned", "", func(r *orchestrator.Record) { r.Root.SessionID = "session-b" }},
		{"no root", "plan_review_task_not_owned", "", func(r *orchestrator.Record) { r.Root = nil }},
		{"another item's", "plan_review_task_other_item", "", func(r *orchestrator.Record) { r.WorkID = other }},
		{"not a review", "plan_review_task_wrong_kind", "", func(r *orchestrator.Record) { r.Kind = "custom" }},
		{"still running", "plan_review_task_unfinished", "", func(r *orchestrator.Record) { r.State = orchestrator.StateBriefed }},
		{"failed", "plan_review_task_unfinished", "", func(r *orchestrator.Record) { r.State = orchestrator.StateFailure }},
		{"before the plan", "plan_review_task_stale", "", func(r *orchestrator.Record) { r.CreatedAt = planAt.Add(-time.Second) }},
	}
	for n, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ref := c.ref
			if c.task != nil {
				ref = "7e000000-0000-4000-8000-0000000001" + string(rune('0'+n/10)) + string(rune('0'+n%10))
				r := reviewTask(ref, epic.Item.ID, clock.at)
				c.task(&r)
				putTask(t, w.Store, r)
			}
			refusedAsWork(t, addDoc(w, &epic, work.DocumentPlanReview, ref), c.code)
		})
	}
	// Named on no line is fine: the owner and the kind already say whose
	// review of what it is.
	unbound := reviewTask("7e000000-0000-4000-8000-000000000200", "", clock.at)
	putTask(t, w.Store, unbound)
	if err := addDoc(w, &epic, work.DocumentPlanReview, unbound.ID); err != nil {
		t.Fatalf("a finished review on no line: %v", err)
	}
	good := reviewTask("7e000000-0000-4000-8000-000000000201", epic.Item.ID, clock.at)
	putTask(t, w.Store, good)
	if err := addDoc(w, &epic, work.DocumentPlanReview, good.ID); err != nil {
		t.Fatalf("a finished review of this plan: %v", err)
	}
	docs, err := w.Store.WorkV2Documents(ctx, epic.Item.ID)
	if err != nil || len(docs) != 3 || docs[2].Reference != good.ID {
		t.Fatalf("documents: %+v %v", docs, err)
	}
}

// An Epic enters implementing only once a plan has a review written after
// it; a plan rewritten after the review needs another. A Feature needs
// neither.
func TestAnEpicEntersImplementingOnlyAfterAReviewedPlan(t *testing.T) {
	w, clock := newEpicTest(t)
	epic := assignedEpic(t, w)
	refusedAsWork(t, advanceTo(w, &epic, work.PhaseImplementing), "epic_plan_required")
	if err := addDoc(w, &epic, work.DocumentPlan, ""); err != nil {
		t.Fatal(err)
	}
	refusedAsWork(t, advanceTo(w, &epic, work.PhaseImplementing), "epic_plan_review_required")
	// The review lands in the plan's own second: insertion order still puts
	// it after the plan.
	first := reviewTask("7e000000-0000-4000-8000-000000000301", epic.Item.ID, clock.at)
	putTask(t, w.Store, first)
	if err := addDoc(w, &epic, work.DocumentPlanReview, first.ID); err != nil {
		t.Fatal(err)
	}
	// Rewritten after the review, and in that same second: the old review
	// no longer covers it.
	if err := addDoc(w, &epic, work.DocumentPlan, ""); err != nil {
		t.Fatal(err)
	}
	refusedAsWork(t, advanceTo(w, &epic, work.PhaseImplementing), "epic_plan_review_required")
	// A plan written a minute later cannot borrow the earlier review task.
	clock.at = clock.at.Add(time.Minute)
	if err := addDoc(w, &epic, work.DocumentPlan, ""); err != nil {
		t.Fatal(err)
	}
	refusedAsWork(t, addDoc(w, &epic, work.DocumentPlanReview, first.ID), "plan_review_task_stale")
	second := reviewTask("7e000000-0000-4000-8000-000000000302", epic.Item.ID, clock.at)
	putTask(t, w.Store, second)
	if err := addDoc(w, &epic, work.DocumentPlanReview, second.ID); err != nil {
		t.Fatal(err)
	}
	if err := advanceTo(w, &epic, work.PhaseImplementing); err != nil {
		t.Fatalf("a reviewed Epic: %v", err)
	}
	if epic.Item.Phase != work.PhaseImplementing {
		t.Fatalf("phase %s", epic.Item.Phase)
	}

	feature := createWorkV2Test(t, w, work.KindFeature)
	owned, err := w.Assign(context.Background(), feature.Item.ID, AssignWorkV2{ExpectedVersion: feature.Item.Version,
		Mode: "existing_session", SessionID: "session-a", Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := advanceTo(w, &owned, work.PhaseImplementing); err != nil {
		t.Fatalf("a Feature needs no plan: %v", err)
	}
}
