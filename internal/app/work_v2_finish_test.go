package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// After a merge the item goes the rest of the way in one step, through the
// same gates the four phase commands meet; the four commands still work; and
// a child carrying several items lands for each of them.

// implementingWorkV2Test is an item owned by session-a, in implementing.
func implementingWorkV2Test(t *testing.T, w *WorkSystemV2) WorkV2View {
	t.Helper()
	v := createWorkV2Test(t, w, work.KindIssue)
	assigned, err := w.Assign(context.Background(), v.Item.ID, AssignWorkV2{ExpectedVersion: v.Item.Version,
		Mode: "existing_session", SessionID: "session-a", Actor: "person", CycleBaseCommit: strings.Repeat("0", 40)}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	implementing, err := w.Advance(context.Background(), v.Item.ID, AdvanceWorkV2{ExpectedVersion: assigned.Item.Version,
		SessionID: "session-a", Next: work.PhaseImplementing, Actor: "session-a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return implementing
}

// landedTaskFor plants a finished task whose landing the broker recorded,
// bound to workID and carrying also.
func landedTaskFor(t *testing.T, w *WorkSystemV2, id, workID string, also ...string) {
	t.Helper()
	at := w.now()
	putTask(t, w.Store, orchestrator.Record{ID: id, Kind: "custom", Title: "the child", WorkID: workID,
		AlsoWorkIDs: also, WorkFrom: work.WorkNamed, State: orchestrator.StateSuccess, CreatedAt: at, FinishedAt: at,
		Root:    &orchestrator.RootRef{SessionID: "session-a", Assistant: "claude"},
		Landing: &orchestrator.Landing{State: orchestrator.LandingLanded, Target: "main", Commit: strings.Repeat("d", 40), At: at}})
}

func phaseEvents(t *testing.T, w *WorkSystemV2, id string) []work.EventV2 {
	t.Helper()
	events, err := w.Store.WorkV2Events(context.Background(), id, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []work.EventV2
	for _, e := range events {
		if e.Kind == "item.phase_changed" {
			out = append(out, e)
		}
	}
	return out
}

func TestTheFourPhaseCommandsStillCloseALandedItem(t *testing.T) {
	w := newWorkV2Test(t)
	owned := implementingWorkV2Test(t, w)
	landedTaskFor(t, w, "7a5c0000-0000-4000-8000-0000000000f1", owned.Item.ID)
	for _, step := range []AdvanceWorkV2{
		{Next: work.PhaseVerifying},
		{Next: work.PhaseMerging, Verification: "go test ./... passed"},
		{Next: work.PhaseDeploying},
		{Next: work.PhaseDone, Deployment: "daemon rebuilt"},
	} {
		step.ExpectedVersion, step.SessionID, step.Actor = owned.Item.Version, "session-a", "session-a"
		var err error
		owned, err = w.Advance(context.Background(), owned.Item.ID, step, nil)
		if err != nil {
			t.Fatalf("advance to %s: %v", step.Next, err)
		}
	}
	if owned.Item.Phase != work.PhaseDone {
		t.Fatalf("phase = %s", owned.Item.Phase)
	}
	events := phaseEvents(t, w, owned.Item.ID)
	deploying := events[len(events)-2].Payload
	if !strings.Contains(deploying, `"landed_tasks":["7a5c0000-0000-4000-8000-0000000000f1"]`) || strings.Contains(deploying, `"finish"`) {
		t.Fatalf("the deploying step does not name the landing it rested on: %s", deploying)
	}
}

func TestFinishTakesALandedItemToDoneInOneStep(t *testing.T) {
	w := newWorkV2Test(t)
	owned := implementingWorkV2Test(t, w)
	before := len(phaseEvents(t, w, owned.Item.ID))
	landedTaskFor(t, w, "7a5c0000-0000-4000-8000-0000000000f2", owned.Item.ID)
	effect := store.Effect{Kind: "test.effect", Subject: owned.Item.ID, Payload: []byte(`{}`)}
	done, err := w.Finish(context.Background(), owned.Item.ID, FinishWorkV2{ExpectedVersion: owned.Item.Version,
		SessionID: "session-a", Verification: "go test ./... passed", Deployment: "daemon rebuilt",
		Actor: "session-a", Effects: []store.Effect{effect}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if done.Item.Phase != work.PhaseDone || done.Item.OwnerSession != "" || done.Item.Version != owned.Item.Version+4 ||
		len(done.EffectIDs) != 1 {
		t.Fatalf("finished = %+v effects %v", done.Item, done.EffectIDs)
	}
	events := phaseEvents(t, w, owned.Item.ID)[before:]
	want := []string{`"to":"verifying"`, `"to":"merging"`, `"to":"deploying"`, `"to":"done"`}
	if len(events) != len(want) {
		t.Fatalf("%d phase events, want %d", len(events), len(want))
	}
	for i, e := range events {
		if !strings.Contains(e.Payload, want[i]) || !strings.Contains(e.Payload, `"finish":true`) {
			t.Fatalf("event %d = %s", i, e.Payload)
		}
	}
	if !strings.Contains(events[1].Payload, `"verification":"go test ./... passed"`) ||
		!strings.Contains(events[2].Payload, `"landed_tasks":["7a5c0000-0000-4000-8000-0000000000f2"]`) ||
		!strings.Contains(events[3].Payload, `"deployment":"daemon rebuilt"`) {
		t.Fatalf("the notes were not recorded where the four commands record them: %+v", events)
	}

	// The same landing seen twice: a second finish, with the version it read
	// before the first, answers the done item and writes nothing.
	again, err := w.Finish(context.Background(), owned.Item.ID, FinishWorkV2{ExpectedVersion: owned.Item.Version,
		SessionID: "session-a", Verification: "again", Deployment: "again", Actor: "session-a",
		Effects: []store.Effect{effect}}, nil)
	if err != nil || again.Item.Phase != work.PhaseDone || again.Item.Version != done.Item.Version || len(again.EffectIDs) != 0 {
		t.Fatalf("second finish = %+v effects %v err %v", again.Item, again.EffectIDs, err)
	}
	if n := len(phaseEvents(t, w, owned.Item.ID)[before:]); n != 4 {
		t.Fatalf("the second finish wrote: %d phase events", n)
	}
}

func TestFinishNamesTheMissingNoteAndWritesNothing(t *testing.T) {
	w := newWorkV2Test(t)
	owned := implementingWorkV2Test(t, w)
	finish := func(c FinishWorkV2) error {
		c.ExpectedVersion, c.SessionID, c.Actor = owned.Item.Version, "session-a", "session-a"
		_, err := w.Finish(context.Background(), owned.Item.ID, c, nil)
		return err
	}
	if got := workErrorCode(t, finish(FinishWorkV2{Deployment: "x"})); got != "verification_required" {
		t.Fatalf("no verification: %s", got)
	}
	if got := workErrorCode(t, finish(FinishWorkV2{Verification: "x", Deployment: "x"})); got != "landing_required" {
		t.Fatalf("no landing: %s", got)
	}
	landedTaskFor(t, w, "7a5c0000-0000-4000-8000-0000000000f3", owned.Item.ID)
	if got := workErrorCode(t, finish(FinishWorkV2{Verification: "x"})); got != "deployment_required" {
		t.Fatalf("no deployment note: %s", got)
	}
	_, err := w.Finish(context.Background(), owned.Item.ID, FinishWorkV2{ExpectedVersion: owned.Item.Version,
		SessionID: "session-b", Verification: "x", Deployment: "x", Actor: "session-b"}, nil)
	if got := workErrorCode(t, err); got != "not_item_owner" {
		t.Fatalf("another Session: %s", got)
	}
	after, err := w.Item(context.Background(), owned.Item.ID)
	if err != nil || after.Item.Phase != work.PhaseImplementing || after.Item.Version != owned.Item.Version {
		t.Fatalf("a refused finish wrote: %+v %v", after.Item, err)
	}
}

func TestEveryItemACarryingChildServedCountsItsLanding(t *testing.T) {
	w := newWorkV2Test(t)
	first := implementingWorkV2Test(t, w)
	second := implementingWorkV2Test(t, w)
	third := implementingWorkV2Test(t, w)
	landedTaskFor(t, w, "7a5c0000-0000-4000-8000-0000000000f4", first.Item.ID, second.Item.ID)
	for _, it := range []WorkV2View{first, second} {
		done, err := w.Finish(context.Background(), it.Item.ID, FinishWorkV2{ExpectedVersion: it.Item.Version,
			SessionID: "session-a", Verification: "checked", NoDeploymentReason: "library only", Actor: "session-a"}, nil)
		if err != nil || done.Item.Phase != work.PhaseDone {
			t.Fatalf("item %s: %+v %v", it.Item.ID, done.Item, err)
		}
		facts, err := w.FinishFacts(context.Background(), it.Item.ID)
		if err != nil || len(facts.Landings) != 1 || facts.Landings[0].Task != "7a5c0000-0000-4000-8000-0000000000f4" ||
			facts.Landings[0].Target != "main" {
			t.Fatalf("landings of %s = %+v %v", it.Item.ID, facts.Landings, err)
		}
	}
	_, err := w.Finish(context.Background(), third.Item.ID, FinishWorkV2{ExpectedVersion: third.Item.Version,
		SessionID: "session-a", Verification: "checked", NoDeploymentReason: "library only", Actor: "session-a"}, nil)
	if got := workErrorCode(t, err); got != "landing_required" {
		t.Fatalf("an item the child did not carry: %s", got)
	}
}

func TestFinishStillNeedsTheVerificationPassAndItsCandidate(t *testing.T) {
	w := newWorkV2Test(t)
	w.GateSettings = func(context.Context) (WorkV2GateSettings, error) {
		return WorkV2GateSettings{Verify: true}, nil
	}
	owned := implementingWorkV2Test(t, w)
	finish := func(expected int64, landing *VerifiedLandingV2) (WorkV2View, error) {
		return w.Finish(context.Background(), owned.Item.ID, FinishWorkV2{ExpectedVersion: expected,
			SessionID: "session-a", Landing: landing, Deployment: "shipped", Actor: "session-a"}, nil)
	}
	// From implementing a gated item needs its candidate: finish cannot
	// enter verification for it.
	if _, err := finish(owned.Item.Version, nil); workErrorCode(t, err) != "verification_candidate_required" {
		t.Fatalf("gated from implementing: %v", err)
	}
	verifying, due := enterVerificationForTest(t, w, owned)
	if _, err := finish(verifying.Item.Version, nil); workErrorCode(t, err) != "verification_authorization_required" {
		t.Fatalf("without a PASS: %v", err)
	}
	passed := passVerificationForTest(t, w, due)
	facts, err := w.FinishFacts(context.Background(), owned.Item.ID)
	if err != nil || facts.Candidate != due.Round.Candidate.Commit {
		t.Fatalf("candidate = %q %v", facts.Candidate, err)
	}
	wrong := &VerifiedLandingV2{Commit: strings.Repeat("e", 40), Target: "main", TargetCommit: "t", Remote: "origin", RemoteCommit: "r"}
	if _, err := finish(passed.Item.Version, wrong); workErrorCode(t, err) != "verified_candidate_mismatch" {
		t.Fatalf("another commit: %v", err)
	}
	right := &VerifiedLandingV2{Commit: facts.Candidate, Target: "main", TargetCommit: "t", Remote: "origin", RemoteCommit: "r"}
	done, err := finish(passed.Item.Version, right)
	if err != nil || done.Item.Phase != work.PhaseDone {
		t.Fatalf("authorized finish = %+v %v", done.Item, err)
	}
}
