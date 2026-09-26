package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/work"
)

func TestAPersonCompletesAnAssignedItemWithOpenSteps(t *testing.T) {
	w := newWorkV2Test(t)
	v := createWorkV2Test(t, w, work.KindFeature)
	owned, err := w.Assign(context.Background(), v.Item.ID, AssignWorkV2{ExpectedVersion: v.Item.Version,
		Mode: "existing_session", SessionID: "session-a", TerminalID: "terminal-a", Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	owned, err = w.Advance(context.Background(), v.Item.ID, AdvanceWorkV2{ExpectedVersion: owned.Item.Version,
		SessionID: "session-a", Next: work.PhaseImplementing, Actor: "session-a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	owned, err = w.AddStep(context.Background(), v.Item.ID, AddStepV2{ExpectedVersion: owned.Item.Version,
		SessionID: "session-a", Title: "Write the migration"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	full, err := w.Item(context.Background(), v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	open := 0
	for _, step := range full.Steps {
		if !step.Done {
			open++
		}
	}
	if open == 0 {
		t.Fatalf("the fixture has no open step to count: %+v", full.Steps)
	}

	if _, err := w.Complete(context.Background(), v.Item.ID, owned.Item.Version-1, "phone", "", nil); err == nil ||
		!strings.Contains(err.Error(), "version_conflict") {
		t.Fatalf("stale completion = %v", err)
	}
	done, err := w.Complete(context.Background(), v.Item.ID, owned.Item.Version, "phone", "  Shipped by hand.  ", nil)
	if err != nil {
		t.Fatal(err)
	}
	if done.Item.Phase != work.PhaseDone || done.Item.OwnerSession != "" || done.Item.ClosedAt.IsZero() ||
		done.Item.Condition != "" || done.Item.UserAction != "" {
		t.Fatalf("completed item = %+v", done.Item)
	}
	full, err = w.Item(context.Background(), v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range full.Assignments {
		if a.State == "active" {
			t.Fatalf("the owning assignment was not released: %+v", full.Assignments)
		}
	}
	last := full.Events[len(full.Events)-1]
	var got struct {
		From      string `json:"from"`
		OpenSteps int    `json:"open_steps"`
		Note      string `json:"note"`
	}
	if err := json.Unmarshal([]byte(last.Payload), &got); err != nil {
		t.Fatal(err)
	}
	if last.Kind != "item.completed" || last.Actor != "phone" || got.From != string(work.PhaseImplementing) ||
		got.OpenSteps != open || got.Note != "Shipped by hand." {
		t.Fatalf("completion event = %+v", last)
	}

	// The item leaves the Session's open list and stays in its recent history.
	assigned, _, err := w.List(context.Background(), "", "session-a", "open", "")
	if err != nil || len(assigned) != 0 {
		t.Fatalf("still assigned after completion: %+v %v", assigned, err)
	}
	recent, _, err := w.RecentlyCompleted(context.Background(), "session-a")
	if err != nil || len(recent) != 1 || recent[0].Item.ID != v.Item.ID {
		t.Fatalf("recent completion: %+v %v", recent, err)
	}

	if _, err := w.Complete(context.Background(), v.Item.ID, done.Item.Version, "phone", "", nil); err == nil ||
		!strings.Contains(err.Error(), "item_terminal") {
		t.Fatalf("completing done work again = %v", err)
	}
}

func TestAPersonCompletesAnUnassignedPlanningItem(t *testing.T) {
	w := newWorkV2Test(t)
	v := createWorkV2Test(t, w, work.KindIssue)
	done, err := w.Complete(context.Background(), v.Item.ID, v.Item.Version, "phone", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if done.Item.Phase != work.PhaseDone || done.Item.ClosedAt.IsZero() {
		t.Fatalf("completed item = %+v", done.Item)
	}
	if _, err := w.Complete(context.Background(), v.Item.ID, v.Item.Version, "phone",
		strings.Repeat("x", workV2CompletionReasonLimit+1), nil); err == nil || !strings.Contains(err.Error(), "note_too_large") {
		t.Fatalf("oversize note = %v", err)
	}
	cancelled := createWorkV2Test(t, w, work.KindIssue)
	gone, err := w.Cancel(context.Background(), cancelled.Item.ID, cancelled.Item.Version, "phone", "not wanted", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Complete(context.Background(), cancelled.Item.ID, gone.Item.Version, "phone", "", nil); err == nil ||
		!strings.Contains(err.Error(), "item_terminal") {
		t.Fatalf("completing cancelled work = %v", err)
	}
}

// A person's completion releases the owning assignment at closed_at, exactly
// as the Session's own completion does, so the assignment ledger alone cannot
// tell them apart.
func TestAnAgentCannotRetractThePersonsCompletion(t *testing.T) {
	w := newWorkV2Test(t)
	v := createWorkV2Test(t, w, work.KindFeature)
	owned, err := w.Assign(context.Background(), v.Item.ID, AssignWorkV2{ExpectedVersion: v.Item.Version,
		Mode: "existing_session", SessionID: "session-a", TerminalID: "terminal-a", Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	done, err := w.Complete(context.Background(), v.Item.ID, owned.Item.Version, "phone", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.ReopenIncomplete(context.Background(), v.Item.ID, AgentReopenWorkV2{
		ExpectedVersion: done.Item.Version, SessionID: "session-a", Reason: "I was not finished.",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "not_completing_session") {
		t.Fatalf("Agent retracted the person's completion: %v", err)
	}

	// Once the person reopens it and the Session completes it itself, the
	// Session's own completion is retractable again.
	reopened, err := w.Reopen(context.Background(), v.Item.ID, done.Item.Version, "phone", nil)
	if err != nil {
		t.Fatal(err)
	}
	owned, err = w.Assign(context.Background(), v.Item.ID, AssignWorkV2{ExpectedVersion: reopened.Item.Version,
		Mode: "existing_session", SessionID: "session-a", TerminalID: "terminal-a", Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	advance := func(next work.Phase, verification string, landing *VerifiedLandingV2, deployment string) {
		t.Helper()
		owned, err = w.Advance(context.Background(), v.Item.ID, AdvanceWorkV2{ExpectedVersion: owned.Item.Version,
			SessionID: "session-a", Next: next, Verification: verification, Landing: landing,
			Deployment: deployment, Actor: "session-a"}, nil)
		if err != nil {
			t.Fatalf("advance to %s: %v", next, err)
		}
	}
	full, err := w.Item(context.Background(), v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Steps) != 0 {
		t.Fatalf("fixture expected no seeded steps on reassignment: %+v", full.Steps)
	}
	advance(work.PhaseImplementing, "", nil, "")
	advance(work.PhaseVerifying, "", nil, "")
	advance(work.PhaseMerging, "tests passed", nil, "")
	advance(work.PhaseDeploying, "", &VerifiedLandingV2{Commit: strings.Repeat("a", 40), Target: "main",
		TargetCommit: strings.Repeat("b", 40), Remote: "origin", RemoteCommit: strings.Repeat("c", 40)}, "")
	advance(work.PhaseDone, "", nil, "production deployment receipt")
	if _, err := w.ReopenIncomplete(context.Background(), v.Item.ID, AgentReopenWorkV2{
		ExpectedVersion: owned.Item.Version, SessionID: "session-a", Reason: "The reported behavior still fails.",
	}, nil); err != nil {
		t.Fatalf("the Session's own later completion is not retractable: %v", err)
	}
}
