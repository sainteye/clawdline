package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// A direct landing is one broker record the item's event names, written in
// the phase change's transaction; recorded again it is the same record.

func directLandingTest(commit string) *VerifiedLandingV2 {
	return &VerifiedLandingV2{Commit: commit, Target: "main", TargetCommit: commit, Remote: "origin", RemoteCommit: commit}
}

// mergingWorkV2Test walks an owned item from implementing to merging.
func mergingWorkV2Test(t *testing.T, w *WorkSystemV2, v WorkV2View) WorkV2View {
	t.Helper()
	for _, step := range []AdvanceWorkV2{{Next: work.PhaseVerifying}, {Next: work.PhaseMerging, Verification: "checked"}} {
		step.ExpectedVersion, step.SessionID, step.Actor = v.Item.Version, "session-a", "session-a"
		var err error
		if v, err = w.Advance(context.Background(), v.Item.ID, step, nil); err != nil {
			t.Fatalf("advance to %s: %v", step.Next, err)
		}
	}
	return v
}

func deploy(t *testing.T, w *WorkSystemV2, v WorkV2View, l *VerifiedLandingV2, noLanding string) (WorkV2View, error) {
	t.Helper()
	return w.Advance(context.Background(), v.Item.ID, AdvanceWorkV2{ExpectedVersion: v.Item.Version, SessionID: "session-a",
		Next: work.PhaseDeploying, Landing: l, NoLandingReason: noLanding, Actor: "session-a"}, nil)
}

func TestARootLandingRecordedTwiceIsOneRecord(t *testing.T) {
	w := newWorkV2Test(t)
	v := mergingWorkV2Test(t, w, implementingWorkV2Test(t, w))
	commit := strings.Repeat("a", 40)
	v, err := deploy(t, w, v, directLandingTest(commit), "")
	if err != nil {
		t.Fatal(err)
	}
	landings, err := w.ItemLandings(context.Background(), v.Item.ID)
	if err != nil || len(landings) != 1 || landings[0].Source != LandingSourceRoot || landings[0].Repository != "/p" {
		t.Fatalf("landings = %+v %v", landings, err)
	}
	first := landings[0].ID
	events := phaseEvents(t, w, v.Item.ID)
	if p := events[len(events)-1].Payload; !strings.Contains(p, `"landing_id":"`+first+`"`) || strings.Contains(p, `"landing":`) {
		t.Fatalf("deploying event = %s", p)
	}

	// Closed, reopened, walked back to merging and landed on the same commit:
	// the same record, no second row.
	done, err := w.Advance(context.Background(), v.Item.ID, AdvanceWorkV2{ExpectedVersion: v.Item.Version,
		SessionID: "session-a", Next: work.PhaseDone, NoDeploymentReason: "none", Actor: "session-a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := w.Reopen(context.Background(), v.Item.ID, done.Item.Version, "person", nil)
	if err != nil {
		t.Fatal(err)
	}
	assigned, err := w.Assign(context.Background(), v.Item.ID, AssignWorkV2{ExpectedVersion: reopened.Item.Version,
		Mode: "existing_session", SessionID: "session-a", Actor: "person", CycleBaseCommit: strings.Repeat("0", 40)}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	again, err := w.Advance(context.Background(), v.Item.ID, AdvanceWorkV2{ExpectedVersion: assigned.Item.Version,
		SessionID: "session-a", Next: work.PhaseImplementing, Actor: "session-a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	again = mergingWorkV2Test(t, w, again)
	retarget := directLandingTest(commit)
	retarget.TargetCommit = strings.Repeat("b", 40) // the branch moved on since
	if _, err := deploy(t, w, again, retarget, ""); err != nil {
		t.Fatal(err)
	}
	landings, err = w.ItemLandings(context.Background(), v.Item.ID)
	if err != nil || len(landings) != 1 || landings[0].ID != first || landings[0].TargetCommit != commit {
		t.Fatalf("after the same landing again: %+v %v", landings, err)
	}
	events = phaseEvents(t, w, v.Item.ID)
	if p := events[len(events)-1].Payload; !strings.Contains(p, `"landing_id":"`+first+`"`) {
		t.Fatalf("second deploying event = %s", p)
	}
}

// An event an older daemon wrote, with its landing copy, still reads: the
// item shows the event as it was and lists the copy as a landing.
func TestALegacyLandingCopyStillReads(t *testing.T) {
	w := newWorkV2Test(t)
	v := mergingWorkV2Test(t, w, implementingWorkV2Test(t, w))
	legacy := `{"from":"merging","to":"deploying","verification":"","landing":{"commit":"c0ffee","target":"main",` +
		`"target_commit":"c0ffee","remote":"origin","remote_commit":"c0ffee"},"deployment":"","no_deployment_reason":""}`
	if err := w.Store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		next := v.Item
		next.Phase = work.PhaseDeploying
		return tx.PutItem(v.Item, next, "item.phase_changed", "session-a", legacy)
	}); err != nil {
		t.Fatal(err)
	}
	read, err := w.Item(context.Background(), v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := read.Events[len(read.Events)-1]
	if last.Payload != legacy {
		t.Fatalf("legacy event reads as %s", last.Payload)
	}
	if len(read.Landings) != 1 || read.Landings[0].Source != LandingSourcePhaseEvent || read.Landings[0].Commit != "c0ffee" ||
		read.Landings[0].Repository != "/p" || read.Landings[0].Remote != "origin" {
		t.Fatalf("legacy landing = %+v", read.Landings)
	}
}

// --no-landing-reason: what it stands in for, and what it refuses.
func TestNoLandingReasonStandsInOnlyForNoCode(t *testing.T) {
	w := newWorkV2Test(t)
	v := mergingWorkV2Test(t, w, implementingWorkV2Test(t, w))
	if _, err := deploy(t, w, v, directLandingTest(strings.Repeat("a", 40)), "docs only"); err == nil ||
		!strings.Contains(err.Error(), "invalid_landing_evidence") {
		t.Fatalf("reason with a landing: %v", err)
	}
	at := w.now()
	putTask(t, w.Store, orchestrator.Record{ID: "7a5c0000-0000-4000-8000-0000000000b1", Kind: "custom", Title: "owed",
		WorkID: v.Item.ID, WorkFrom: work.WorkNamed, State: orchestrator.StateSuccess, CreatedAt: at, FinishedAt: at,
		Landing: &orchestrator.Landing{State: orchestrator.LandingPending}})
	if _, err := deploy(t, w, v, nil, "docs only"); err == nil || !strings.Contains(err.Error(), "landing_owed") {
		t.Fatalf("reason with a pending landing: %v", err)
	}
	putTask(t, w.Store, orchestrator.Record{ID: "7a5c0000-0000-4000-8000-0000000000b1", Kind: "custom", Title: "owed",
		WorkID: v.Item.ID, WorkFrom: work.WorkNamed, State: orchestrator.StateSuccess, CreatedAt: at, FinishedAt: at,
		Landing: &orchestrator.Landing{State: orchestrator.LandingLanded, Target: "main", Commit: "d"}})
	if _, err := deploy(t, w, v, nil, "docs only"); err == nil || !strings.Contains(err.Error(), "landing_recorded") {
		t.Fatalf("reason beside a landed task: %v", err)
	}
	unchanged, err := w.Item(context.Background(), v.Item.ID)
	if err != nil || unchanged.Item.Version != v.Item.Version {
		t.Fatalf("a refused reason wrote: %d -> %d %v", v.Item.Version, unchanged.Item.Version, err)
	}

	// An item with nothing bound reaches done on the reason, and keeps it.
	other := mergingWorkV2Test(t, w, implementingWorkV2Test(t, w))
	deployed, err := deploy(t, w, other, nil, "a decision, no code")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Advance(context.Background(), other.Item.ID, AdvanceWorkV2{ExpectedVersion: deployed.Item.Version,
		SessionID: "session-a", Next: work.PhaseDone, NoDeploymentReason: "none", Actor: "session-a"}, nil); err != nil {
		t.Fatal(err)
	}
	read, err := w.Item(context.Background(), other.Item.ID)
	if err != nil || read.CardProgress.NoLandingReason != "a decision, no code" {
		t.Fatalf("no landing reason after done = %q %v", read.CardProgress.NoLandingReason, err)
	}
	if _, err := w.Advance(context.Background(), v.Item.ID, AdvanceWorkV2{ExpectedVersion: v.Item.Version,
		SessionID: "session-a", Next: work.PhaseImplementing, NoLandingReason: "x", Actor: "session-a"}, nil); err == nil ||
		!strings.Contains(err.Error(), "invalid_landing_evidence") {
		t.Fatalf("reason on another step: %v", err)
	}
}
