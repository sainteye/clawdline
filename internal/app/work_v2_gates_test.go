package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func enterVerificationForTest(t *testing.T, w *WorkSystemV2, owned WorkV2View) (WorkV2View, store.WorkGateDue) {
	t.Helper()
	full, err := w.Item(context.Background(), owned.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	var active work.AssignmentV2
	for _, assignment := range full.Assignments {
		if assignment.State == "active" {
			active = assignment
			break
		}
	}
	roundID := gateDeterministicID(owned.Item.ID + fmt.Sprintf(":round:%d", owned.Item.Version))
	attemptID := gateDeterministicID(roundID + ":attempt:0")
	candidate := contract.WorkGateCandidateReceipt{Repository: owned.Item.ProjectPath, Worktree: owned.Item.ProjectPath,
		Branch: "feature", Commit: strings.Repeat("a", 40), Tree: strings.Repeat("b", 40),
		AssignmentID: active.ID, OwnerSessionID: active.SessionID, Cycle: owned.Item.Cycle,
		CriteriaVersion: owned.Item.AcceptanceVersion, CriteriaDigest: owned.Item.AcceptanceDigest,
		CreatedAt: w.now().Unix()}
	verifying, err := w.Advance(context.Background(), owned.Item.ID, AdvanceWorkV2{ExpectedVersion: owned.Item.Version,
		SessionID: active.SessionID, Next: work.PhaseVerifying, Actor: active.SessionID, Candidate: &candidate,
		RoundID: roundID, AttemptID: attemptID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	round, err := w.Store.WorkGateRound(context.Background(), roundID)
	if err != nil {
		t.Fatal(err)
	}
	round.Attempts[0].TaskID = attemptID
	return verifying, store.WorkGateDue{Round: round, Attempt: round.Attempts[0], BaseCommit: owned.Item.CycleBaseCommit}
}

func passVerificationForTest(t *testing.T, w *WorkSystemV2, due store.WorkGateDue) WorkV2View {
	t.Helper()
	result := contract.WorkGateResult{RoundID: due.Round.ID, TaskID: due.Attempt.TaskID,
		CandidateCommit: due.Round.Candidate.Commit, CandidateTree: due.Round.Candidate.Tree,
		CriteriaVersion: due.Round.Acceptance.Version, CriteriaDigest: due.Round.Acceptance.Digest,
		Verdict: contract.WorkGateVerdictPASS, Summary: "The frozen candidate passed.", Claims: []contract.WorkGateClaim{{
			Criterion: due.Round.Acceptance.Criteria, State: contract.WorkGateClaimStatePassed,
			Evidence: []string{"focused checks passed"}, EvidenceArtifacts: []string{},
		}}}
	c := WorkGateCoordinator{Store: w.Store, Now: func() time.Time { return w.now() }}
	if err := c.applyResult(context.Background(), due, result); err != nil {
		t.Fatal(err)
	}
	view, err := w.Item(context.Background(), due.Round.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func TestGateReadsExposeCompactCardsFullDetailAndOnlyLiveAuthorization(t *testing.T) {
	w, verifying, due := gateCoordinatorFixture(t)
	passed := passVerificationForTest(t, w, due)
	if passed.Gate == nil || passed.Gate.Compact.CurrentAuthorization == nil ||
		passed.Gate.Compact.CurrentAuthorization.Kind != "pass" || len(passed.Gate.RecentRounds) != 1 {
		t.Fatalf("detail authorization = %+v", passed.Gate)
	}
	page, err := w.ListPage(context.Background(), "", "open", "", "")
	if err != nil || len(page.Rows) != 1 {
		t.Fatalf("list page = %+v err=%v", page, err)
	}
	card := page.Rows[0]
	if card.Gate != nil || card.GateCompact == nil || card.GateCompact.LatestRound == nil ||
		card.GateCompact.CurrentAuthorization == nil || card.GateCompact.CurrentAuthorization.Kind != "pass" {
		t.Fatalf("compact card leaked or omitted gate state: %+v", card)
	}
	revised := "A revised observable outcome must pass its own frozen verification."
	edited, err := w.Edit(context.Background(), verifying.Item.ID, EditWorkV2{ExpectedVersion: passed.Item.Version,
		AcceptanceCriteria: &revised, Actor: "person", Person: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := w.Item(context.Background(), edited.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Gate == nil || detail.Gate.Compact.CurrentAuthorization != nil {
		t.Fatalf("invalidated authorization remained public: %+v", detail.Gate)
	}
	page, err = w.ListPage(context.Background(), "", "open", "", "")
	if err != nil || page.Rows[0].GateCompact.CurrentAuthorization != nil {
		t.Fatalf("invalidated compact authorization = %+v err=%v", page.Rows[0].GateCompact, err)
	}
}

func workErrorCode(t *testing.T, err error) string {
	t.Helper()
	var refusal *WorkError
	if !errors.As(err, &refusal) {
		t.Fatalf("untyped error: %v", err)
	}
	return refusal.Code
}

func TestFirstSuccessfulAssignmentCapturesEveryGateModeOnce(t *testing.T) {
	for _, mode := range []WorkV2GateSettings{
		{}, {Planning: true}, {Verify: true}, {Planning: true, Verify: true},
	} {
		t.Run(func() string {
			if mode.Planning {
				if mode.Verify {
					return "planning-on-verify-on"
				}
				return "planning-on-verify-off"
			}
			if mode.Verify {
				return "planning-off-verify-on"
			}
			return "planning-off-verify-off"
		}(), func(t *testing.T) {
			w := newWorkV2Test(t)
			current := mode
			w.GateSettings = func(context.Context) (WorkV2GateSettings, error) { return current, nil }
			created := createWorkV2Test(t, w, work.KindFeature)
			assigned, err := w.Assign(context.Background(), created.Item.ID, AssignWorkV2{
				ExpectedVersion: created.Item.Version, Mode: "existing_session", SessionID: "session-a", Actor: "local",
			}, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !assigned.Item.HasGateSnapshot() || assigned.Item.GateSnapshotAt.IsZero() ||
				assigned.Item.PlanningGate != mode.Planning || assigned.Item.VerifyGate != mode.Verify {
				t.Fatalf("captured snapshot = %+v", assigned.Item)
			}
			current = WorkV2GateSettings{Planning: !mode.Planning, Verify: !mode.Verify}
			reassigned, err := w.Assign(context.Background(), created.Item.ID, AssignWorkV2{
				ExpectedVersion: assigned.Item.Version, Mode: "existing_session", SessionID: "session-b", Actor: "local",
			}, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			if reassigned.Item.GateSnapshotCycle != assigned.Item.GateSnapshotCycle ||
				reassigned.Item.GateSnapshotAt != assigned.Item.GateSnapshotAt ||
				reassigned.Item.PlanningGate != mode.Planning || reassigned.Item.VerifyGate != mode.Verify {
				t.Fatalf("reassignment replaced snapshot: before=%+v after=%+v", assigned.Item, reassigned.Item)
			}
		})
	}
}

func TestARefusedAssignmentWritesNoGateSnapshot(t *testing.T) {
	for _, kind := range []work.Kind{work.KindFeature, work.KindEpic} {
		t.Run(string(kind), func(t *testing.T) {
			w := newWorkV2Test(t)
			w.GateSettings = func(context.Context) (WorkV2GateSettings, error) {
				return WorkV2GateSettings{Planning: true}, nil
			}
			created, err := w.Create(context.Background(), NewWorkV2{ProjectID: "p", ProjectPath: "/p",
				Kind: kind, Title: "Needs acceptance", Description: "Cannot be assigned yet.", Actor: "local"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = w.Assign(context.Background(), created.Item.ID, AssignWorkV2{ExpectedVersion: created.Item.Version,
				Mode: "existing_session", SessionID: "session-a", Actor: "local"}, false, nil)
			if got := workErrorCode(t, err); got != "acceptance_required" {
				t.Fatalf("error = %s", got)
			}
			after, err := w.Item(context.Background(), created.Item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if after.Item.HasGateSnapshot() || after.Item.GateSnapshotCycle != 0 || len(after.Assignments) != 0 ||
				after.Item.Version != created.Item.Version {
				t.Fatalf("refused assignment wrote: %+v assignments=%+v", after.Item, after.Assignments)
			}
		})
	}
}

func TestVerifyOnRejectsFreeFormEvidenceUntilTheCoordinatorAuthorizesTheTuple(t *testing.T) {
	w := newWorkV2Test(t)
	w.GateSettings = func(context.Context) (WorkV2GateSettings, error) {
		return WorkV2GateSettings{Verify: true}, nil
	}
	created := createWorkV2Test(t, w, work.KindIssue)
	assigned, err := w.Assign(context.Background(), created.Item.ID, AssignWorkV2{
		ExpectedVersion: created.Item.Version, Mode: "existing_session", SessionID: "session-a", Actor: "person",
		CycleBaseCommit: strings.Repeat("0", 40),
	}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	implementing, err := w.Advance(context.Background(), created.Item.ID, AdvanceWorkV2{
		ExpectedVersion: assigned.Item.Version, SessionID: "session-a", Next: work.PhaseImplementing, Actor: "session-a",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	verifying, due := enterVerificationForTest(t, w, implementing)
	_, err = w.Advance(context.Background(), created.Item.ID, AdvanceWorkV2{
		ExpectedVersion: verifying.Item.Version, SessionID: "session-a", Next: work.PhaseMerging,
		Verification: "I ran the tests", Actor: "session-a",
	}, nil)
	if got := workErrorCode(t, err); got != "verification_authorization_required" {
		t.Fatalf("free-form verification error = %s", got)
	}
	verifying = passVerificationForTest(t, w, due)
	if verifying.Item.OwnerSession != "session-a" {
		t.Fatalf("PASS changed owner: %+v", verifying.Item)
	}
	merged, err := w.Advance(context.Background(), created.Item.ID, AdvanceWorkV2{
		ExpectedVersion: verifying.Item.Version, SessionID: "session-a", Next: work.PhaseMerging, Actor: "session-a",
	}, nil)
	if err != nil || merged.Item.Phase != work.PhaseMerging {
		t.Fatalf("authorized merge = %+v err=%v", merged.Item, err)
	}
}

func TestPendingRootKeepsItsPreviewButSnapshotsOnlyOnSuccess(t *testing.T) {
	w := newWorkV2Test(t)
	current := WorkV2GateSettings{Planning: true, Verify: true}
	w.GateSettings = func(context.Context) (WorkV2GateSettings, error) { return current, nil }
	created := createWorkV2Test(t, w, work.KindFeature)
	pending, err := w.Assign(context.Background(), created.Item.ID, AssignWorkV2{ExpectedVersion: created.Item.Version,
		Mode: "new_session", AssignmentID: "pending-a", Actor: "person"}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Item.HasGateSnapshot() || len(pending.Assignments) != 1 || !pending.Assignments[0].GatePreviewed ||
		!pending.Assignments[0].PlanningGate || !pending.Assignments[0].VerifyGate {
		t.Fatalf("pending preview = %+v / %+v", pending.Item, pending.Assignments)
	}
	failed, err := w.FinishAssignment(context.Background(), created.Item.ID, FinishAssignmentV2{
		AssignmentID: "pending-a", Failure: "the Session did not open", Actor: "person",
	})
	if err != nil || failed.Item.HasGateSnapshot() || failed.Item.GateSnapshotCycle != 0 {
		t.Fatalf("failed pending assignment = %+v err=%v", failed.Item, err)
	}
	pending, err = w.Assign(context.Background(), created.Item.ID, AssignWorkV2{ExpectedVersion: failed.Item.Version,
		Mode: "new_session", AssignmentID: "pending-b", Actor: "person"}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	current = WorkV2GateSettings{}
	active, err := w.FinishAssignment(context.Background(), created.Item.ID, FinishAssignmentV2{
		AssignmentID: "pending-b", SessionID: "session-a", TerminalID: "terminal-a", Actor: "person",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !active.Item.HasGateSnapshot() || !active.Item.PlanningGate || !active.Item.VerifyGate {
		t.Fatalf("activation recaptured changed settings: %+v", active.Item)
	}
}

func TestPersonReopenClearsTheSnapshotAndNextAssignmentReadsSettingsAgain(t *testing.T) {
	w := newWorkV2Test(t)
	current := WorkV2GateSettings{Planning: true, Verify: true}
	w.GateSettings = func(context.Context) (WorkV2GateSettings, error) { return current, nil }
	created := createWorkV2Test(t, w, work.KindIssue)
	assigned, err := w.Assign(context.Background(), created.Item.ID, AssignWorkV2{ExpectedVersion: created.Item.Version,
		Mode: "existing_session", SessionID: "session-a", Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	done, err := w.Complete(context.Background(), created.Item.ID, assigned.Item.Version, "person", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := w.Reopen(context.Background(), created.Item.ID, done.Item.Version, "person", nil)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Item.HasGateSnapshot() || reopened.Item.GateSnapshotCycle != 0 ||
		reopened.Item.PlanningGate || reopened.Item.VerifyGate || reopened.Item.Cycle != 2 {
		t.Fatalf("person reopen retained snapshot: %+v", reopened.Item)
	}
	current = WorkV2GateSettings{}
	again, err := w.Assign(context.Background(), created.Item.ID, AssignWorkV2{ExpectedVersion: reopened.Item.Version,
		Mode: "existing_session", SessionID: "session-b", Actor: "person"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Item.HasGateSnapshot() || again.Item.GateSnapshotCycle != 2 || again.Item.PlanningGate || again.Item.VerifyGate {
		t.Fatalf("new cycle snapshot = %+v", again.Item)
	}
}

func TestAcceptanceAuthorityVersionsAndInvalidatesVerification(t *testing.T) {
	w := newWorkV2Test(t)
	created, err := w.Create(context.Background(), NewWorkV2{ProjectID: "p", ProjectPath: "/p",
		Kind: work.KindIssue, Title: "Governed", Description: "Exact acceptance.",
		AcceptanceCriteria: "- first\n", Actor: "person"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if created.Item.AcceptanceVersion != 1 || created.Item.AcceptanceDigest != work.AcceptanceDigest("- first\n") {
		t.Fatalf("created acceptance = %+v", created.Item)
	}
	assigned, err := w.Assign(context.Background(), created.Item.ID, AssignWorkV2{ExpectedVersion: created.Item.Version,
		Mode: "existing_session", SessionID: "session-a", Actor: "person"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	second := "- second\n"
	_, err = w.Edit(context.Background(), created.Item.ID, EditWorkV2{ExpectedVersion: assigned.Item.Version,
		AcceptanceCriteria: &second, Actor: "session-a", OwnerSession: "session-a"}, nil)
	if got := workErrorCode(t, err); got != "acceptance_not_agent_editable" {
		t.Fatalf("maker edit = %s", got)
	}
	invalidations := 0
	w.VerificationInvalidator = func(_ *store.WorkV2Tx, _, _ work.ItemV2, reason string) error {
		if reason != "acceptance_changed" {
			t.Fatalf("reason = %q", reason)
		}
		invalidations++
		return nil
	}
	edited, err := w.Edit(context.Background(), created.Item.ID, EditWorkV2{ExpectedVersion: assigned.Item.Version,
		AcceptanceCriteria: &second, Actor: "person", Person: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if edited.Item.AcceptanceVersion != 2 || edited.Item.AcceptanceCriteria != second ||
		edited.Item.AcceptanceDigest != work.AcceptanceDigest(second) || invalidations != 1 {
		t.Fatalf("edited acceptance = %+v invalidations=%d", edited.Item, invalidations)
	}
	implementing, err := w.Advance(context.Background(), created.Item.ID, AdvanceWorkV2{ExpectedVersion: edited.Item.Version,
		SessionID: "session-a", Next: work.PhaseImplementing, Actor: "session-a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	verifying, err := w.Advance(context.Background(), created.Item.ID, AdvanceWorkV2{ExpectedVersion: implementing.Item.Version,
		SessionID: "session-a", Next: work.PhaseVerifying, Actor: "session-a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	third := "- third\n"
	back, err := w.Edit(context.Background(), created.Item.ID, EditWorkV2{ExpectedVersion: verifying.Item.Version,
		AcceptanceCriteria: &third, Actor: "person", Person: true}, nil)
	if err != nil || back.Item.Phase != work.PhaseImplementing || invalidations != 2 {
		t.Fatalf("verifying edit = %+v invalidations=%d err=%v", back.Item, invalidations, err)
	}
	verifying, err = w.Advance(context.Background(), created.Item.ID, AdvanceWorkV2{ExpectedVersion: back.Item.Version,
		SessionID: "session-a", Next: work.PhaseVerifying, Actor: "session-a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	merging, err := w.Advance(context.Background(), created.Item.ID, AdvanceWorkV2{ExpectedVersion: verifying.Item.Version,
		SessionID: "session-a", Next: work.PhaseMerging, Verification: "focused tests passed", Actor: "session-a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Edit(context.Background(), created.Item.ID, EditWorkV2{ExpectedVersion: merging.Item.Version,
		AcceptanceCriteria: &second, Actor: "person", Person: true}, nil)
	if got := workErrorCode(t, err); got != "acceptance_locked" {
		t.Fatalf("merging edit = %s", got)
	}
}
