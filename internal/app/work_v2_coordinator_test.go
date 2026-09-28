package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func gateCoordinatorFixture(t *testing.T) (*WorkSystemV2, WorkV2View, store.WorkGateDue) {
	t.Helper()
	w := newWorkV2Test(t)
	w.GateSettings = func(context.Context) (WorkV2GateSettings, error) {
		return WorkV2GateSettings{Verify: true}, nil
	}
	created := createWorkV2Test(t, w, work.KindIssue)
	assigned, err := w.Assign(context.Background(), created.Item.ID, AssignWorkV2{ExpectedVersion: created.Item.Version,
		Mode: "existing_session", SessionID: "11111111-1111-4111-8111-111111111111", Actor: "person",
		CycleBaseCommit: strings.Repeat("0", 40)}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	implementing, err := w.Advance(context.Background(), created.Item.ID, AdvanceWorkV2{
		ExpectedVersion: assigned.Item.Version, SessionID: assigned.Item.OwnerSession,
		Next: work.PhaseImplementing, Actor: assigned.Item.OwnerSession}, nil)
	if err != nil {
		t.Fatal(err)
	}
	verifying, due := enterVerificationForTest(t, w, implementing)
	return w, verifying, due
}

func dispatchedGate(in orchestrator.GateDispatch) orchestrator.Dispatched {
	return orchestrator.Dispatched{Record: orchestrator.Record{ID: in.TaskID, State: orchestrator.StateBriefed}}
}

func gateResult(due store.WorkGateDue, verdict contract.WorkGateVerdict) contract.WorkGateResult {
	state := contract.WorkGateClaimStatePassed
	reason := ""
	if verdict == contract.WorkGateVerdictFAIL {
		state, reason = contract.WorkGateClaimStateFailed, "The focused check failed."
	}
	if verdict == contract.WorkGateVerdictNEEDSWORK {
		state, reason = contract.WorkGateClaimStateUnverified, "Loopback is unavailable."
	}
	return contract.WorkGateResult{RoundID: due.Round.ID, TaskID: due.Attempt.TaskID,
		CandidateCommit: due.Round.Candidate.Commit, CandidateTree: due.Round.Candidate.Tree,
		CriteriaVersion: due.Round.Acceptance.Version, CriteriaDigest: due.Round.Acceptance.Digest,
		Verdict: verdict, Summary: "checker result", Claims: []contract.WorkGateClaim{{
			Criterion: due.Round.Acceptance.Criteria, State: state, Reason: reason,
			Evidence: []string{"focused output"}, EvidenceArtifacts: []string{},
		}}}
}

func TestGateCoordinatorRecoversDurableDispatchIntentAfterCrash(t *testing.T) {
	w, _, due := gateCoordinatorFixture(t)
	crashed := false
	dispatches := 0
	c := WorkGateCoordinator{Store: w.Store, Now: w.Now,
		Dispatch: func(context.Context, orchestrator.GateDispatch) (orchestrator.Dispatched, error) {
			dispatches++
			return orchestrator.Dispatched{}, nil
		}, Fault: func(point string) error {
			if point == "dispatch_intent_committed" && !crashed {
				crashed = true
				return errors.New("injected crash")
			}
			return nil
		}}
	if _, err := c.Pass(context.Background()); err == nil {
		t.Fatal("injected crash was not observed")
	}
	round, err := w.Store.WorkGateRound(context.Background(), due.Round.ID)
	if err != nil || round.Attempts[0].State != contract.WorkGateAttemptStateDispatching || dispatches != 0 {
		t.Fatalf("durable intent = %+v dispatches=%d err=%v", round.Attempts, dispatches, err)
	}
	c.Fault = nil
	c.Dispatch = func(_ context.Context, in orchestrator.GateDispatch) (orchestrator.Dispatched, error) {
		dispatches++
		return dispatchedGate(in), nil
	}
	if _, err := c.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if dispatches != 1 {
		t.Fatalf("recovered dispatches = %d, want one", dispatches)
	}
}

func TestGateCoordinatorReconcilesDispatchEffectAfterResponseLoss(t *testing.T) {
	w, _, due := gateCoordinatorFixture(t)
	dispatches := 0
	c := WorkGateCoordinator{Store: w.Store, Now: w.Now,
		Dispatch: func(_ context.Context, in orchestrator.GateDispatch) (orchestrator.Dispatched, error) {
			dispatches++
			return orchestrator.Dispatched{}, errors.New("response lost after durable broker dispatch")
		}, Record: func(_ context.Context, id string) (orchestrator.Record, error) {
			return orchestrator.Record{ID: id, State: orchestrator.StateBriefed}, nil
		}}
	if _, err := c.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	round, err := w.Store.WorkGateRound(context.Background(), due.Round.ID)
	if err != nil || dispatches != 1 || len(round.Attempts) != 1 ||
		round.Attempts[0].State != contract.WorkGateAttemptStateRunning || round.Attempts[0].TaskID != due.Attempt.ID {
		t.Fatalf("dispatch reconciliation = %+v calls=%d err=%v", round.Attempts, dispatches, err)
	}
}

func TestGateCoordinatorDefersCapacityWithoutConsumingTheRetry(t *testing.T) {
	w, _, due := gateCoordinatorFixture(t)
	c := WorkGateCoordinator{Store: w.Store, Now: w.Now,
		Dispatch: func(context.Context, orchestrator.GateDispatch) (orchestrator.Dispatched, error) {
			return orchestrator.Dispatched{}, orchestrator.Refusal{Status: http.StatusTooManyRequests, Code: "over_capacity"}
		}}
	if _, err := c.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	round, err := w.Store.WorkGateRound(context.Background(), due.Round.ID)
	if err != nil || len(round.Attempts) != 1 || round.Attempts[0].State != contract.WorkGateAttemptStateQueued ||
		round.Attempts[0].NextRetryAt != w.Now().Add(300*time.Second).Unix() {
		t.Fatalf("capacity deferral = %+v err=%v", round.Attempts, err)
	}
	if pass, err := c.Pass(context.Background()); err != nil || pass.Due != 0 {
		t.Fatalf("future retry was not excluded: %+v err=%v", pass, err)
	}
}

func TestGateCoordinatorCapacityRefusalDoesNotStarveLaterRunnableWork(t *testing.T) {
	w, _, first := gateCoordinatorFixture(t)
	secondItem := createWorkV2Test(t, w, work.KindIssue)
	secondRound := first.Round
	secondRound.ID = gateDeterministicID(secondItem.Item.ID + ":round")
	secondRound.ItemID = secondItem.Item.ID
	secondRound.Attempts = []contract.WorkGateAttempt{{
		ID:        gateDeterministicID(secondRound.ID + ":attempt:0"),
		RoundID:   secondRound.ID,
		Attempt:   0,
		State:     contract.WorkGateAttemptStateQueued,
		CreatedAt: w.Now().Unix() + 1,
		UpdatedAt: w.Now().Unix() + 1,
	}}
	if err := w.Store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		return tx.CreateWorkGateRound(secondRound, first.BaseCommit)
	}); err != nil {
		t.Fatal(err)
	}
	dispatched := []string{}
	c := WorkGateCoordinator{Store: w.Store, Now: w.Now,
		Dispatch: func(_ context.Context, in orchestrator.GateDispatch) (orchestrator.Dispatched, error) {
			dispatched = append(dispatched, in.TaskID)
			if in.TaskID == first.Attempt.ID {
				return orchestrator.Dispatched{}, orchestrator.Refusal{Status: http.StatusTooManyRequests, Code: "over_capacity"}
			}
			return dispatchedGate(in), nil
		}}
	if _, err := c.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := w.Store.WorkGateRound(context.Background(), secondRound.ID)
	if err != nil || len(dispatched) != 2 || len(got.Attempts) != 1 ||
		got.Attempts[0].State != contract.WorkGateAttemptStateRunning {
		t.Fatalf("capacity fairness dispatches=%v second=%+v err=%v", dispatched, got.Attempts, err)
	}
}

func TestGateCoordinatorUsesOneBrokerRespawnForSpawnFailure(t *testing.T) {
	w, _, due := gateCoordinatorFixture(t)
	respawns := 0
	c := WorkGateCoordinator{Store: w.Store, Now: w.Now,
		Dispatch: func(_ context.Context, in orchestrator.GateDispatch) (orchestrator.Dispatched, error) {
			return dispatchedGate(in), nil
		}, Record: func(context.Context, string) (orchestrator.Record, error) {
			return orchestrator.Record{ID: due.Attempt.ID, State: orchestrator.StateSpawnFailed}, nil
		}, Respawn: func(context.Context, string) (orchestrator.Respawned, error) {
			respawns++
			return orchestrator.Respawned{Dispatched: orchestrator.Dispatched{Record: orchestrator.Record{
				ID: "22222222-2222-4222-8222-222222222222", State: orchestrator.StateBriefed}}}, nil
		}}
	if _, err := c.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	round, err := w.Store.WorkGateRound(context.Background(), due.Round.ID)
	if err != nil || respawns != 1 || len(round.Attempts) != 2 ||
		round.Attempts[1].RespawnOf != due.Attempt.ID || round.Attempts[1].State != contract.WorkGateAttemptStateRunning {
		t.Fatalf("respawn receipt = %+v calls=%d err=%v", round.Attempts, respawns, err)
	}
}

func TestGateCoordinatorFindsRespawnPersistedBeforeItsResponseWasLost(t *testing.T) {
	w, _, due := gateCoordinatorFixture(t)
	descendant := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	c := WorkGateCoordinator{Store: w.Store, Now: w.Now,
		Dispatch: func(_ context.Context, in orchestrator.GateDispatch) (orchestrator.Dispatched, error) {
			return dispatchedGate(in), nil
		}, Record: func(context.Context, string) (orchestrator.Record, error) {
			return orchestrator.Record{ID: due.Attempt.ID, State: orchestrator.StateSpawnFailed}, nil
		}, Respawn: func(context.Context, string) (orchestrator.Respawned, error) {
			return orchestrator.Respawned{}, errors.New("response lost after respawn")
		}, FindRespawn: func(context.Context, string) (orchestrator.Record, bool, error) {
			return orchestrator.Record{ID: descendant, State: orchestrator.StateBriefed}, true, nil
		}}
	if _, err := c.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	round, err := w.Store.WorkGateRound(context.Background(), due.Round.ID)
	if err != nil || len(round.Attempts) != 2 || round.Attempts[1].TaskID != descendant ||
		round.Attempts[1].RespawnOf != due.Attempt.ID {
		t.Fatalf("respawn reconciliation = %+v err=%v", round.Attempts, err)
	}
}

func TestGateCoordinatorEscalatesTheSecondTechnicalFailure(t *testing.T) {
	w, _, due := gateCoordinatorFixture(t)
	c := WorkGateCoordinator{Store: w.Store, Now: w.Now,
		Dispatch: func(_ context.Context, in orchestrator.GateDispatch) (orchestrator.Dispatched, error) {
			return dispatchedGate(in), nil
		}, Record: func(_ context.Context, id string) (orchestrator.Record, error) {
			return orchestrator.Record{ID: id, State: orchestrator.StateFailure}, nil
		}}
	for n := 0; n < 4; n++ {
		if _, err := c.Pass(context.Background()); err != nil {
			t.Fatalf("pass %d: %v", n, err)
		}
	}
	round, err := w.Store.WorkGateRound(context.Background(), due.Round.ID)
	if err != nil || round.State != contract.WorkGateRoundStateTechnicalFailure || len(round.Attempts) != 2 {
		t.Fatalf("technical round = %+v err=%v", round, err)
	}
	escalations, err := w.Store.OpenWorkGateEscalations(context.Background())
	if err != nil || len(escalations) != 1 || escalations[0].Kind != contract.WorkGateEscalationKindTechnicalVerification ||
		escalations[0].State != contract.WorkGateEscalationStateWaitingUser {
		t.Fatalf("technical escalation = %+v err=%v", escalations, err)
	}
}

func TestGateCoordinatorRetriesTimeoutAndOrdinaryDirectResultOnce(t *testing.T) {
	for _, state := range []orchestrator.State{orchestrator.StateTimeout, orchestrator.StateSuccess} {
		t.Run(string(state), func(t *testing.T) {
			w, _, due := gateCoordinatorFixture(t)
			c := WorkGateCoordinator{Store: w.Store, Now: w.Now,
				Dispatch: func(_ context.Context, in orchestrator.GateDispatch) (orchestrator.Dispatched, error) {
					return dispatchedGate(in), nil
				}, Record: func(_ context.Context, id string) (orchestrator.Record, error) {
					return orchestrator.Record{ID: id, State: state}, nil
				}}
			if _, err := c.Pass(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Pass(context.Background()); err != nil {
				t.Fatal(err)
			}
			round, err := w.Store.WorkGateRound(context.Background(), due.Round.ID)
			if err != nil || len(round.Attempts) != 2 || round.Attempts[1].State != contract.WorkGateAttemptStateQueued {
				t.Fatalf("%s retry = %+v err=%v", state, round.Attempts, err)
			}
		})
	}
}

func TestMalformedAndMismatchedGateResultsNeverAuthorize(t *testing.T) {
	for _, mutate := range []func(*contract.WorkGateResult){
		func(result *contract.WorkGateResult) { result.Claims = nil },
		func(result *contract.WorkGateResult) { result.CandidateCommit = strings.Repeat("f", 40) },
	} {
		w, view, due := gateCoordinatorFixture(t)
		due.Attempt.TaskID = due.Attempt.ID
		result := gateResult(due, contract.WorkGateVerdictPASS)
		mutate(&result)
		if err := (&WorkGateCoordinator{Store: w.Store, Now: w.Now}).applyResult(context.Background(), due, result); err != nil {
			t.Fatal(err)
		}
		round, err := w.Store.WorkGateRound(context.Background(), due.Round.ID)
		if err != nil || len(round.Attempts) != 2 || round.Attempts[1].State != contract.WorkGateAttemptStateQueued {
			t.Fatalf("bad result retry = %+v err=%v", round.Attempts, err)
		}
		if _, err := w.Advance(context.Background(), view.Item.ID, AdvanceWorkV2{ExpectedVersion: view.Item.Version,
			SessionID: view.Item.OwnerSession, Next: work.PhaseMerging, Actor: view.Item.OwnerSession}, nil); workErrorCode(t, err) != "verification_authorization_required" {
			t.Fatalf("bad result authorized merge: %v", err)
		}
	}
}

func TestGateCoordinatorKeepsLoopbackFailureUnknown(t *testing.T) {
	w, _, due := gateCoordinatorFixture(t)
	due.Attempt.TaskID = due.Attempt.ID
	result := gateResult(due, contract.WorkGateVerdictNEEDSWORK)
	if err := (&WorkGateCoordinator{Store: w.Store, Now: w.Now}).applyResult(context.Background(), due, result); err != nil {
		t.Fatal(err)
	}
	view, err := w.Item(context.Background(), due.Round.ItemID)
	if err != nil || view.Item.Phase != work.PhaseVerifying || view.Item.Condition != work.ConditionEvidenceUnknown ||
		view.Item.UserAction != "" {
		t.Fatalf("unknown evidence advanced work: %+v err=%v", view.Item, err)
	}
	if _, err := w.Advance(context.Background(), view.Item.ID, AdvanceWorkV2{ExpectedVersion: view.Item.Version,
		SessionID: view.Item.OwnerSession, Next: work.PhaseMerging, Actor: view.Item.OwnerSession}, nil); workErrorCode(t, err) != "verification_authorization_required" {
		t.Fatalf("unknown evidence merge = %v", err)
	}
}

func TestNeedsWorkBreaksConsecutiveFailSeries(t *testing.T) {
	w, view, due := gateCoordinatorFixture(t)
	if err := w.Store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		_, err := tx.IncrementWorkGateFail(view.Item, 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	due.Attempt.TaskID = due.Attempt.ID
	if err := (&WorkGateCoordinator{Store: w.Store, Now: w.Now}).applyResult(context.Background(), due,
		gateResult(due, contract.WorkGateVerdictNEEDSWORK)); err != nil {
		t.Fatal(err)
	}
	if got := gateFailCount(t, w, view.Item.ID); got != 0 {
		t.Fatalf("NEEDS_WORK left %d consecutive FAILs; want 0", got)
	}
}

func TestGateCoordinatorStalesALateResultAfterCriteriaChange(t *testing.T) {
	w, verifying, due := gateCoordinatorFixture(t)
	criteria := "The replacement acceptance is observable."
	changed, err := w.Edit(context.Background(), verifying.Item.ID, EditWorkV2{ExpectedVersion: verifying.Item.Version,
		AcceptanceCriteria: &criteria, Actor: "person", Person: true}, nil)
	if err != nil || changed.Item.Phase != work.PhaseImplementing {
		t.Fatalf("criteria edit = %+v err=%v", changed.Item, err)
	}
	due.Attempt.TaskID = due.Attempt.ID
	if err := (&WorkGateCoordinator{Store: w.Store, Now: w.Now}).applyResult(context.Background(), due,
		gateResult(due, contract.WorkGateVerdictPASS)); err == nil {
		// The invalidator may already have staled the round, in which case the
		// late result is refused by compare-and-set rather than accepted.
		t.Fatal("late PASS unexpectedly completed")
	}
	if _, err := w.Advance(context.Background(), changed.Item.ID, AdvanceWorkV2{ExpectedVersion: changed.Item.Version,
		SessionID: changed.Item.OwnerSession, Next: work.PhaseVerifying, Actor: changed.Item.OwnerSession}, nil); err == nil {
		t.Fatal("new criteria admitted without a new candidate")
	}
}

func TestThreeConsecutiveFailsEscalateTheFrozenTuple(t *testing.T) {
	w, view, due := gateCoordinatorFixture(t)
	for n := 1; n <= 3; n++ {
		due.Attempt.TaskID = due.Attempt.ID
		if err := (&WorkGateCoordinator{Store: w.Store, Now: w.Now}).applyResult(context.Background(), due,
			gateResult(due, contract.WorkGateVerdictFAIL)); err != nil {
			t.Fatalf("FAIL %d: %v", n, err)
		}
		view, _ = w.Item(context.Background(), view.Item.ID)
		if n < 3 {
			view, due = enterVerificationForTest(t, w, view)
		}
	}
	escalations, err := w.Store.OpenWorkGateEscalations(context.Background())
	if err != nil || len(escalations) != 1 || escalations[0].ConsecutiveFailures != 3 ||
		escalations[0].Kind != contract.WorkGateEscalationKindThirdFail {
		t.Fatalf("third FAIL escalation = %+v err=%v", escalations, err)
	}
	if view.Item.Condition != work.ConditionWaitingUser {
		t.Fatalf("third FAIL condition = %s", view.Item.Condition)
	}
}

func TestParentOwnerOfflineGracePromotesOnceAfter900Seconds(t *testing.T) {
	w := newWorkV2Test(t)
	created := createWorkV2Test(t, w, work.KindIssue)
	start := w.Now()
	err := w.Store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		return tx.CreateWorkGateEscalation(store.WorkGateEscalationRecord{WorkGateEscalation: contract.WorkGateEscalation{
			ID: "33333333-3333-4333-8333-333333333333", ItemID: created.Item.ID,
			Kind:  contract.WorkGateEscalationKindTechnicalVerification,
			State: contract.WorkGateEscalationStateWaitingParentOwner, Reason: "checker failed",
			WaitingSince: start.Unix(), CreatedAt: start.Unix(), UpdatedAt: start.Unix()},
			ParentOwner: "44444444-4444-4444-8444-444444444444", NotificationState: "accepted"})
	})
	if err != nil {
		t.Fatal(err)
	}
	now := start
	notices := 0
	c := WorkGateCoordinator{Store: w.Store, Now: func() time.Time { return now },
		OwnerLive:  func(context.Context, string) bool { return false },
		NotifyUser: func(context.Context, string, string) error { notices++; return nil }}
	if err := c.routeEscalations(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = start.Add(899 * time.Second)
	if err := c.routeEscalations(context.Background()); err != nil {
		t.Fatal(err)
	}
	if notices != 0 {
		t.Fatalf("notice before grace = %d", notices)
	}
	now = start.Add(900 * time.Second)
	if err := c.routeEscalations(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.routeEscalations(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, err := w.Store.OpenWorkGateEscalations(context.Background())
	if err != nil || len(rows) != 1 || rows[0].State != contract.WorkGateEscalationStateWaitingUser || notices != 1 {
		t.Fatalf("promotion = %+v notices=%d err=%v", rows, notices, err)
	}
}

func TestReturningParentClearsTheOfflineGraceClock(t *testing.T) {
	w := newWorkV2Test(t)
	created := createWorkV2Test(t, w, work.KindIssue)
	now := w.Now()
	err := w.Store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		return tx.CreateWorkGateEscalation(store.WorkGateEscalationRecord{WorkGateEscalation: contract.WorkGateEscalation{
			ID: "34333333-3333-4333-8333-333333333333", ItemID: created.Item.ID,
			Kind:  contract.WorkGateEscalationKindTechnicalVerification,
			State: contract.WorkGateEscalationStateWaitingParentOwner, Reason: "checker failed",
			WaitingSince: now.Unix(), CreatedAt: now.Unix(), UpdatedAt: now.Unix()},
			ParentOwner: "45444444-4444-4444-8444-444444444444", NotificationState: "accepted"})
	})
	if err != nil {
		t.Fatal(err)
	}
	live := false
	c := WorkGateCoordinator{Store: w.Store, Now: func() time.Time { return now },
		OwnerLive: func(context.Context, string) bool { return live }}
	if err := c.routeEscalations(context.Background()); err != nil {
		t.Fatal(err)
	}
	live = true
	now = now.Add(899 * time.Second)
	if err := c.routeEscalations(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, err := w.Store.OpenWorkGateEscalations(context.Background())
	if err != nil || len(rows) != 1 || rows[0].OwnerOfflineSince != 0 ||
		rows[0].State != contract.WorkGateEscalationStateWaitingParentOwner {
		t.Fatalf("returning parent = %+v err=%v", rows, err)
	}
}

func gateDecisionFixture(t *testing.T, kind contract.WorkGateEscalationKind) (*WorkSystemV2, WorkV2View, store.WorkGateDue) {
	t.Helper()
	w, view, due := gateCoordinatorFixture(t)
	now := w.Now()
	err := w.Store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		if err := tx.FinishWorkGateAttemptAndRound(due.Round.ID, due.Attempt.ID, "decision fixture",
			contract.WorkGateRoundStateTechnicalFailure, now); err != nil {
			return err
		}
		for n := 0; n < 3; n++ {
			if _, err := tx.IncrementWorkGateFail(view.Item, 1); err != nil {
				return err
			}
		}
		return tx.CreateWorkGateEscalation(store.WorkGateEscalationRecord{WorkGateEscalation: contract.WorkGateEscalation{
			ID: gateDeterministicID(due.Round.ID + ":decision"), ItemID: view.Item.ID, RoundID: due.Round.ID,
			Kind: kind, State: contract.WorkGateEscalationStateWaitingParentOwner, Reason: "decision required",
			Candidate: &due.Round.Candidate, ConsecutiveFailures: 3, WaitingSince: now.Unix(),
			CreatedAt: now.Unix(), UpdatedAt: now.Unix()}, ParentOwner: "55555555-5555-4555-8555-555555555555",
			NotificationState: "accepted"})
	})
	if err != nil {
		t.Fatal(err)
	}
	return w, view, due
}

func gateFailCount(t *testing.T, w *WorkSystemV2, itemID string) int64 {
	t.Helper()
	var count int64
	if err := w.Store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		metrics, err := tx.WorkGateMetrics(itemID)
		count = metrics.ConsecutiveFails
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestGateEscalationDecisionMatrixAndCounterResets(t *testing.T) {
	tests := []struct {
		name   string
		kind   contract.WorkGateEscalationKind
		action contract.WorkGateDecisionAction
		reset  bool
	}{
		{"third direction", contract.WorkGateEscalationKindThirdFail, contract.WorkGateDecisionActionDirection, true},
		{"third revise", contract.WorkGateEscalationKindThirdFail, contract.WorkGateDecisionActionReviseAcceptance, true},
		{"third reassign", contract.WorkGateEscalationKindThirdFail, contract.WorkGateDecisionActionReassign, true},
		{"third override", contract.WorkGateEscalationKindThirdFail, contract.WorkGateDecisionActionOverride, false},
		{"third cancel", contract.WorkGateEscalationKindThirdFail, contract.WorkGateDecisionActionCancel, false},
		{"technical direction", contract.WorkGateEscalationKindTechnicalVerification, contract.WorkGateDecisionActionDirection, true},
		{"technical reassign", contract.WorkGateEscalationKindTechnicalVerification, contract.WorkGateDecisionActionReassign, true},
		{"technical retry", contract.WorkGateEscalationKindTechnicalVerification, contract.WorkGateDecisionActionRetry, true},
		{"technical override", contract.WorkGateEscalationKindTechnicalVerification, contract.WorkGateDecisionActionOverride, false},
		{"technical cancel", contract.WorkGateEscalationKindTechnicalVerification, contract.WorkGateDecisionActionCancel, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, view, due := gateDecisionFixture(t, tt.kind)
			request := contract.WorkGateDecisionRequest{Action: tt.action, ExpectedVersion: view.Item.Version,
				SessionID: "55555555-5555-4555-8555-555555555555", Reason: "reasoned decision",
				Direction: "Repair the failing boundary.", AcceptanceCriteria: "The revised outcome is observable.",
				TargetSessionID: "66666666-6666-4666-8666-666666666666"}
			changed, err := w.DecideWorkGate(context.Background(), view.Item.ID, request, false)
			if err != nil {
				t.Fatal(err)
			}
			if tt.reset && gateFailCount(t, w, view.Item.ID) != 0 {
				t.Fatalf("%s did not reset consecutive FAILs", tt.action)
			}
			if tt.action == contract.WorkGateDecisionActionOverride {
				auth, err := func() (store.WorkGateAuthorization, error) {
					var out store.WorkGateAuthorization
					err := w.Store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
						var err error
						out, err = tx.WorkGateAuthorization(changed.Item)
						return err
					})
					return out, err
				}()
				if err != nil || auth.CandidateCommit != due.Round.Candidate.Commit {
					t.Fatalf("override authorization = %+v err=%v", auth, err)
				}
			}
			if tt.action == contract.WorkGateDecisionActionReassign {
				if changed.Item.OwnerSession != request.TargetSessionID || changed.Item.CycleBaseCommit != view.Item.CycleBaseCommit ||
					changed.Item.GateSnapshotCycle != view.Item.GateSnapshotCycle || changed.Item.VerifyGate != view.Item.VerifyGate {
					t.Fatalf("reassignment lost cycle gate identity: before=%+v after=%+v", view.Item, changed.Item)
				}
			}
		})
	}
}

func TestGateEscalationRejectsActionsForTheOtherKind(t *testing.T) {
	for _, tt := range []struct {
		kind   contract.WorkGateEscalationKind
		action contract.WorkGateDecisionAction
	}{
		{contract.WorkGateEscalationKindThirdFail, contract.WorkGateDecisionActionRetry},
		{contract.WorkGateEscalationKindTechnicalVerification, contract.WorkGateDecisionActionReviseAcceptance},
	} {
		w, view, _ := gateDecisionFixture(t, tt.kind)
		_, err := w.DecideWorkGate(context.Background(), view.Item.ID, contract.WorkGateDecisionRequest{
			Action: tt.action, ExpectedVersion: view.Item.Version, SessionID: "55555555-5555-4555-8555-555555555555",
			Reason: "wrong resolution kind", AcceptanceCriteria: "replacement"}, false)
		if got := workErrorCode(t, err); got != "verification_decision_invalid" {
			t.Fatalf("%s/%s = %s", tt.kind, tt.action, got)
		}
		if gateFailCount(t, w, view.Item.ID) != 3 {
			t.Fatal("refused decision changed the consecutive FAIL series")
		}
	}
}

func TestPersonMayResolveAnOfflineTechnicalEscalation(t *testing.T) {
	w, view, due := gateDecisionFixture(t, contract.WorkGateEscalationKindTechnicalVerification)
	if _, err := w.DecideWorkGate(context.Background(), view.Item.ID, contract.WorkGateDecisionRequest{
		Action: contract.WorkGateDecisionActionOverride, ExpectedVersion: view.Item.Version + 1,
		Reason: "stale decision"}, true); workErrorCode(t, err) != "version_conflict" {
		t.Fatalf("stale decision = %v", err)
	}
	changed, err := w.DecideWorkGate(context.Background(), view.Item.ID, contract.WorkGateDecisionRequest{
		Action: contract.WorkGateDecisionActionOverride, ExpectedVersion: view.Item.Version,
		Reason: "The person accepts this exact frozen candidate."}, true)
	if err != nil {
		t.Fatal(err)
	}
	var auth store.WorkGateAuthorization
	err = w.Store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		var err error
		auth, err = tx.WorkGateAuthorization(changed.Item)
		return err
	})
	if err != nil || auth.Kind != "technical_person_override" || auth.CandidateCommit != due.Round.Candidate.Commit {
		t.Fatalf("person override = %+v err=%v", auth, err)
	}
}

func TestParentCourtesyFeedbackIsDurableDeliveredAndObserved(t *testing.T) {
	w, _, due := gateCoordinatorFixture(t)
	now := w.Now()
	c := WorkGateCoordinator{Store: w.Store, Now: w.Now,
		Dispatch: func(context.Context, orchestrator.GateDispatch) (orchestrator.Dispatched, error) {
			return orchestrator.Dispatched{}, orchestrator.Refusal{Status: http.StatusTooManyRequests, Code: "over_capacity"}
		}, SendFeedback: func(context.Context, string, string) error { return nil }}
	err := w.Store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		return c.escalate(tx, due.Round, contract.WorkGateEscalationKindTechnicalVerification,
			"checker failed twice", 0, "77777777-7777-4777-8777-777777777777", true, now)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	feedback, err := w.Store.WorkGateFeedbacks(context.Background(), due.Round.ItemID)
	if err != nil || len(feedback) != 1 || feedback[0].State != "delivered" {
		t.Fatalf("parent courtesy = %+v err=%v", feedback, err)
	}
	if err := w.Store.ObserveWorkGateFeedback(context.Background(), feedback[0].SessionID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	feedback, _ = w.Store.WorkGateFeedbacks(context.Background(), due.Round.ItemID)
	if feedback[0].State != "observed" {
		t.Fatalf("observed courtesy = %+v", feedback[0])
	}
}

func TestExecutedCourtesyFeedbackBecomesUnknownAfterRestartWithoutResend(t *testing.T) {
	w, _, due := gateCoordinatorFixture(t)
	err := w.Store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		return tx.AddWorkGateFeedback(store.WorkGateFeedback{
			ID: gateDeterministicID(due.Round.ID + ":feedback-crash"), ItemID: due.Round.ItemID,
			RoundID: due.Round.ID, SessionID: "77777777-7777-4777-8777-777777777777",
			Body: "verification needs a decision", CreatedAt: w.Now(), UpdatedAt: w.Now()})
	})
	if err != nil {
		t.Fatal(err)
	}
	c := WorkGateCoordinator{Store: w.Store, Now: w.Now,
		Dispatch: func(context.Context, orchestrator.GateDispatch) (orchestrator.Dispatched, error) {
			return orchestrator.Dispatched{}, orchestrator.Refusal{Status: http.StatusTooManyRequests, Code: "over_capacity"}
		}, Fault: func(point string) error {
			if point == "feedback_effect_committed" {
				return errors.New("crash before feedback effect")
			}
			return nil
		}}
	if _, err := c.Pass(context.Background()); err == nil {
		t.Fatal("feedback crash was not injected")
	}
	sends := 0
	c.Fault = nil
	c.SendFeedback = func(context.Context, string, string) error { sends++; return nil }
	if _, err := c.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, err := w.Store.WorkGateFeedbacks(context.Background(), due.Round.ItemID)
	if err != nil || len(rows) != 1 || rows[0].State != "unknown" || sends != 0 {
		t.Fatalf("feedback restart rows=%+v sends=%d err=%v", rows, sends, err)
	}
}

func TestExecutedEscalationNotificationBecomesUnknownAfterRestartWithoutResend(t *testing.T) {
	w, _, due := gateCoordinatorFixture(t)
	err := w.Store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		return (&WorkGateCoordinator{}).escalate(tx, due.Round,
			contract.WorkGateEscalationKindTechnicalVerification, "checker failed twice", 0, "", false, w.Now())
	})
	if err != nil {
		t.Fatal(err)
	}
	c := WorkGateCoordinator{Store: w.Store, Now: w.Now,
		Dispatch: func(context.Context, orchestrator.GateDispatch) (orchestrator.Dispatched, error) {
			return orchestrator.Dispatched{}, orchestrator.Refusal{Status: http.StatusTooManyRequests, Code: "over_capacity"}
		}, Fault: func(point string) error {
			if point == "notification_effect_committed" {
				return errors.New("crash before notification effect")
			}
			return nil
		}}
	if _, err := c.Pass(context.Background()); err == nil {
		t.Fatal("notification crash was not injected")
	}
	notices := 0
	c.Fault = nil
	c.NotifyUser = func(context.Context, string, string) error { notices++; return nil }
	if _, err := c.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, err := w.Store.OpenWorkGateEscalations(context.Background())
	if err != nil || len(rows) != 1 || rows[0].NotificationState != "unknown" || notices != 0 {
		t.Fatalf("notification restart rows=%+v notices=%d err=%v", rows, notices, err)
	}
}

func TestOfflineRoutingTouchesEveryDueOwnerFairly(t *testing.T) {
	w := newWorkV2Test(t)
	first := createWorkV2Test(t, w, work.KindIssue)
	second, err := w.Create(context.Background(), NewWorkV2{ProjectID: "p", ProjectPath: "/p", Kind: work.KindIssue,
		Title: "Second item", Description: "Another item", AcceptanceCriteria: "Observable", Actor: "local"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := w.Now()
	for n, item := range []WorkV2View{first, second} {
		err := w.Store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
			return tx.CreateWorkGateEscalation(store.WorkGateEscalationRecord{WorkGateEscalation: contract.WorkGateEscalation{
				ID: gateDeterministicID(item.Item.ID + ":fair"), ItemID: item.Item.ID,
				Kind:  contract.WorkGateEscalationKindTechnicalVerification,
				State: contract.WorkGateEscalationStateWaitingParentOwner, Reason: "offline",
				WaitingSince: now.Unix(), CreatedAt: now.Unix() + int64(n), UpdatedAt: now.Unix()},
				ParentOwner: "88888888-8888-4888-8888-888888888888", NotificationState: "accepted"})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	c := WorkGateCoordinator{Store: w.Store, Now: w.Now, OwnerLive: func(context.Context, string) bool { return false }}
	if err := c.routeEscalations(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, err := w.Store.OpenWorkGateEscalations(context.Background())
	if err != nil || len(rows) != 2 || rows[0].OwnerOfflineSince == 0 || rows[1].OwnerOfflineSince == 0 {
		t.Fatalf("fair offline routing = %+v err=%v", rows, err)
	}
}

func TestGateDetailCapacityExportsPurgesAndAdmitsAgain(t *testing.T) {
	w := newWorkV2Test(t)
	created := createWorkV2Test(t, w, work.KindIssue)
	now := w.Now()
	candidate := contract.WorkGateCandidateReceipt{Repository: "/p", Worktree: "/p/w", Branch: "feature",
		Commit: strings.Repeat("a", 40), Tree: strings.Repeat("b", 40),
		AssignmentID: "99999999-9999-4999-8999-999999999999", OwnerSessionID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		Cycle: created.Item.Cycle, CriteriaVersion: created.Item.AcceptanceVersion,
		CriteriaDigest: created.Item.AcceptanceDigest, CreatedAt: now.Unix()}
	makeRound := func(n int) contract.WorkGateRound {
		id := gateDeterministicID(created.Item.ID + fmt.Sprintf(":capacity:%d", n))
		return contract.WorkGateRound{ID: id, ItemID: created.Item.ID, Cycle: created.Item.Cycle,
			Acceptance: contract.WorkGateAcceptance{Criteria: created.Item.AcceptanceCriteria,
				Version: created.Item.AcceptanceVersion, Digest: created.Item.AcceptanceDigest},
			Candidate: candidate, CheckerPersona: "code-reviewer", State: contract.WorkGateRoundStateQueued,
			CreatedAt: now.Unix() + int64(n), UpdatedAt: now.Unix() + int64(n), Attempts: []contract.WorkGateAttempt{{
				ID: gateDeterministicID(id + ":attempt:0"), RoundID: id, Attempt: 0,
				State: contract.WorkGateAttemptStateQueued, CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
			}}}
	}
	for n := 0; n < contract.WorkGateRoundDetailsPerItemLimit; n++ {
		round := makeRound(n)
		if err := w.Store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
			if err := tx.CreateWorkGateRound(round, strings.Repeat("0", 40)); err != nil {
				return err
			}
			return tx.FinishWorkGateAttemptAndRound(round.ID, round.Attempts[0].ID, "closed detail",
				contract.WorkGateRoundStateTechnicalFailure, now)
		}); err != nil {
			t.Fatalf("round %d: %v", n, err)
		}
	}
	extra := makeRound(100)
	if err := w.Store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		return tx.CreateWorkGateRound(extra, strings.Repeat("0", 40))
	}); !errors.Is(err, store.ErrWorkGateRoundsFull) {
		t.Fatalf("capacity overflow = %v", err)
	}
	export, err := w.Store.ExportWorkGateDetails(context.Background(), created.Item.ID)
	if err != nil || export.RoundCount != contract.WorkGateRoundDetailsPerItemLimit || export.SHA256 == "" {
		t.Fatalf("export = %+v err=%v", export, err)
	}
	if _, err := w.Store.PurgeWorkGateDetails(context.Background(), created.Item.ID,
		export.ItemVersion+1, export.SHA256, now); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale purge = %v", err)
	}
	purged, err := w.Store.PurgeWorkGateDetails(context.Background(), created.Item.ID,
		export.ItemVersion, export.SHA256, now)
	if err != nil || purged != contract.WorkGateRoundDetailsPerItemLimit {
		t.Fatalf("purge = %d err=%v", purged, err)
	}
	removed := 0
	cleanupCoordinator := WorkGateCoordinator{Store: w.Store, Now: w.Now, PurgeEvidence: func(string) error {
		removed++
		return nil
	}}
	for removed < contract.WorkGateRoundDetailsPerItemLimit {
		if _, err := cleanupCoordinator.Pass(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if due, err := w.Store.DueWorkGateCleanup(context.Background(), contract.WorkGateDueRowsPerPassLimit); err != nil || len(due) != 0 {
		t.Fatalf("evidence cleanup remains = %v err=%v", due, err)
	}
	if err := w.Store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		return tx.CreateWorkGateRound(extra, strings.Repeat("0", 40))
	}); err != nil {
		t.Fatalf("new round after purge: %v", err)
	}
}
