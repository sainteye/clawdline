package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// WorkGateCoordinator is the bounded durable loop between Board verification
// rounds and immutable broker checker tasks. Its function seams are explicit
// so failure tests can stop at every persistence/effect boundary.
type WorkGateCoordinator struct {
	Store         *store.Store
	Broker        *orchestrator.Broker
	Now           func() time.Time
	Dispatch      func(context.Context, orchestrator.GateDispatch) (orchestrator.Dispatched, error)
	Record        func(context.Context, string) (orchestrator.Record, error)
	Respawn       func(context.Context, string) (orchestrator.Respawned, error)
	FindRespawn   func(context.Context, string) (orchestrator.Record, bool, error)
	SendFeedback  func(context.Context, string, string) error
	OwnerLive     func(context.Context, string) bool
	NotifyUser    func(context.Context, string, string) error
	PurgeEvidence func(string) error
	Fault         func(string) error
}

// DecideWorkGate applies the closed escalation decision set. Person authority
// is explicit; an agent decision is accepted only from the still-designated
// live parent owner while the escalation remains routed to that owner.
func (w *WorkSystemV2) DecideWorkGate(ctx context.Context, itemID string,
	request contract.WorkGateDecisionRequest, person bool) (WorkV2View, error) {
	if strings.TrimSpace(request.Reason) == "" {
		return WorkV2View{}, workV2Error(400, "reason_required", "A verification escalation decision needs a reason.")
	}
	now := w.now()
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		item, err := tx.Item(itemID)
		if err != nil {
			return err
		}
		if item.Version != request.ExpectedVersion {
			return store.ErrConflict
		}
		escalation, err := tx.OpenWorkGateEscalation(itemID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return work.RefuseV2("verification_escalation_missing", "This item has no unresolved verification escalation.")
			}
			return err
		}
		if !person && (escalation.State != contract.WorkGateEscalationStateWaitingParentOwner ||
			request.SessionID == "" || request.SessionID != escalation.ParentOwner) {
			return work.RefuseV2("verification_decision_forbidden", "Only the designated parent owner may decide this escalation.")
		}
		next := item
		next.UpdatedAt = now
		action := request.Action
		if escalation.Kind == contract.WorkGateEscalationKindThirdFail && action == contract.WorkGateDecisionActionRetry {
			return work.RefuseV2("verification_decision_invalid", "A third FAIL needs direction, acceptance revision, reassignment, override, or cancellation.")
		}
		if escalation.Kind == contract.WorkGateEscalationKindTechnicalVerification && action == contract.WorkGateDecisionActionReviseAcceptance {
			return work.RefuseV2("verification_decision_invalid", "A technical escalation needs repair-retry, direction, reassignment, override, or cancellation.")
		}
		switch action {
		case contract.WorkGateDecisionActionDirection:
			if strings.TrimSpace(request.Direction) == "" {
				return work.RefuseV2("direction_required", "Direction needs the concrete next action for the maker.")
			}
			next.Phase, next.Condition, next.UserAction = work.PhaseImplementing, work.ConditionBlocked, ""
			if escalation.Candidate != nil {
				if err := tx.AddWorkGateFeedback(store.WorkGateFeedback{ID: gateDeterministicID(escalation.ID + ":direction"),
					ItemID: item.ID, RoundID: escalation.RoundID, SessionID: escalation.Candidate.OwnerSessionID,
					Body: request.Direction, CreatedAt: now, UpdatedAt: now}); err != nil {
					return err
				}
			}
		case contract.WorkGateDecisionActionReviseAcceptance:
			if err := validateWorkV2Acceptance(request.AcceptanceCriteria); err != nil {
				return err
			}
			if strings.TrimSpace(request.AcceptanceCriteria) == "" {
				return work.RefuseV2("acceptance_required", "Revising acceptance needs replacement criteria.")
			}
			prev := next
			work.SetAcceptance(&next, request.AcceptanceCriteria)
			next.Phase, next.Condition, next.UserAction = work.PhaseImplementing, "", ""
			if err := tx.InvalidateWorkV2VerificationAuthorization(prev, next, "escalation_acceptance_revised"); err != nil {
				return err
			}
		case contract.WorkGateDecisionActionReassign:
			if strings.TrimSpace(request.TargetSessionID) == "" {
				return work.RefuseV2("target_session_required", "Reassignment needs a replacement Session.")
			}
			if err := tx.ReassignWorkGateOwner(item, gateDeterministicID(escalation.ID+":assignment"),
				request.TargetSessionID, request.SessionID, now); err != nil {
				return err
			}
			next.OwnerSession, next.Phase, next.Condition, next.UserAction = request.TargetSessionID,
				work.PhaseImplementing, "", ""
		case contract.WorkGateDecisionActionRetry:
			if escalation.Candidate == nil || escalation.Candidate.Cycle != item.Cycle ||
				escalation.Candidate.CriteriaVersion != item.AcceptanceVersion ||
				escalation.Candidate.CriteriaDigest != item.AcceptanceDigest {
				return work.RefuseV2("verification_candidate_stale", "Retry needs the escalation's still-current frozen candidate.")
			}
			roundID := gateDeterministicID(escalation.ID + ":retry")
			persona, err := tx.WorkGateCheckerPersona(item)
			if err != nil {
				return err
			}
			if err := tx.ResolveWorkGateEscalation(escalation.ID, action, request.Reason, now); err != nil {
				return err
			}
			if err := tx.ResetWorkGateFailures(item); err != nil {
				return err
			}
			if err := tx.CreateWorkGateRound(contract.WorkGateRound{ID: roundID, ItemID: item.ID,
				Cycle: item.Cycle, Acceptance: contract.WorkGateAcceptance{Criteria: item.AcceptanceCriteria,
					Version: item.AcceptanceVersion, Digest: item.AcceptanceDigest}, Candidate: *escalation.Candidate,
				CheckerPersona: persona, State: contract.WorkGateRoundStateQueued, CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
				Attempts: []contract.WorkGateAttempt{{ID: gateDeterministicID(roundID + ":attempt:0"), RoundID: roundID,
					Attempt: 0, State: contract.WorkGateAttemptStateQueued, CreatedAt: now.Unix(), UpdatedAt: now.Unix()}}},
				item.CycleBaseCommit); err != nil {
				return err
			}
			next.Phase, next.Condition, next.UserAction = work.PhaseVerifying, "", ""
			return tx.PutItem(item, next, "verification.escalation_resolved", request.SessionID,
				payload(map[string]any{"action": action, "reason": request.Reason}))
		case contract.WorkGateDecisionActionOverride:
			if escalation.Candidate == nil || escalation.Candidate.Cycle != item.Cycle ||
				escalation.Candidate.CriteriaVersion != item.AcceptanceVersion ||
				escalation.Candidate.CriteriaDigest != item.AcceptanceDigest {
				return work.RefuseV2("verification_candidate_stale", "Override may authorize only the escalation's current frozen candidate.")
			}
			kind := "ai_override"
			if person {
				kind = "person_override"
			}
			if escalation.Kind == contract.WorkGateEscalationKindTechnicalVerification {
				kind = "technical_ai_override"
				if person {
					kind = "technical_person_override"
				}
			}
			if err := tx.AddWorkGateAuthorization(store.WorkGateAuthorization{ItemID: item.ID,
				RoundID: escalation.RoundID, Cycle: item.Cycle, CriteriaVersion: item.AcceptanceVersion,
				CriteriaDigest: item.AcceptanceDigest, CandidateCommit: escalation.Candidate.Commit,
				CandidateTree: escalation.Candidate.Tree, Kind: kind, Actor: request.SessionID,
				Reason: request.Reason}, now); err != nil {
				return err
			}
			next.Phase, next.Condition, next.UserAction = work.PhaseVerifying, "", ""
		case contract.WorkGateDecisionActionCancel:
			if err := tx.ReleaseWorkGateOwner(item.ID, now); err != nil {
				return err
			}
			next.Phase, next.Condition, next.UserAction, next.OwnerSession = work.PhaseCancelled, "", "", ""
			next.ClosedAt = now
		default:
			return work.RefuseV2("verification_decision_invalid", "Unknown verification escalation decision.")
		}
		if action == contract.WorkGateDecisionActionDirection || action == contract.WorkGateDecisionActionReassign {
			if err := tx.ResetWorkGateFailures(item); err != nil {
				return err
			}
		}
		if err := tx.ResolveWorkGateEscalation(escalation.ID, action, request.Reason, now); err != nil {
			return err
		}
		actor := request.SessionID
		if person {
			actor = "person"
		}
		return tx.PutItem(item, next, "verification.escalation_resolved", actor,
			payload(map[string]any{"action": action, "reason": request.Reason}))
	})
	if err != nil {
		return WorkV2View{}, mapWorkV2Error(err)
	}
	return w.Item(ctx, itemID)
}

type WorkGatePass struct {
	Due, Dispatched, Completed, Retried, Escalated, FeedbackDelivered int
}

func (c *WorkGateCoordinator) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC().Truncate(time.Second)
	}
	return time.Now().UTC().Truncate(time.Second)
}

func gateDeterministicID(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	h := hex.EncodeToString(sum[:16])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func (c *WorkGateCoordinator) dispatch(ctx context.Context, in orchestrator.GateDispatch) (orchestrator.Dispatched, error) {
	if c.Dispatch != nil {
		return c.Dispatch(ctx, in)
	}
	return c.Broker.DispatchGate(ctx, in)
}

func (c *WorkGateCoordinator) record(ctx context.Context, id string) (orchestrator.Record, error) {
	if c.Record != nil {
		return c.Record(ctx, id)
	}
	r, _, err := c.Broker.Record(ctx, id)
	return r, err
}

func (c *WorkGateCoordinator) respawn(ctx context.Context, id string) (orchestrator.Respawned, error) {
	if c.Respawn != nil {
		return c.Respawn(ctx, id)
	}
	return c.Broker.Respawn(ctx, id, "")
}

func (c *WorkGateCoordinator) findRespawn(ctx context.Context, id string) (orchestrator.Record, bool, error) {
	if c.FindRespawn != nil {
		return c.FindRespawn(ctx, id)
	}
	return c.Broker.FindGateRespawn(ctx, id)
}

// Pass processes at most the registered twenty attempts and twenty feedback
// deliveries. Deferred rows are excluded by the store query, so one 429 cannot
// starve later work.
func (c *WorkGateCoordinator) Pass(ctx context.Context) (WorkGatePass, error) {
	var out WorkGatePass
	due, err := c.Store.DueWorkGateAttempts(ctx, c.now(), contract.WorkGateDueRowsPerPassLimit)
	if err != nil {
		return out, err
	}
	out.Due = len(due)
	for _, row := range due {
		result, err := c.processAttempt(ctx, row)
		if err != nil {
			return out, err
		}
		out.Dispatched += result.Dispatched
		out.Completed += result.Completed
		out.Retried += result.Retried
		out.Escalated += result.Escalated
	}
	feedback, err := c.Store.DueWorkGateFeedback(ctx, contract.WorkGateDueRowsPerPassLimit)
	if err != nil {
		return out, err
	}
	for _, row := range feedback {
		if row.State == "executed" {
			if err := c.Store.SetWorkGateFeedbackState(ctx, row.ID, "executed", "unknown", c.now()); err != nil && !errors.Is(err, store.ErrConflict) {
				return out, err
			}
			continue
		}
		if err := c.Store.SetWorkGateFeedbackState(ctx, row.ID, "accepted", "executed", c.now()); err != nil {
			if errors.Is(err, store.ErrConflict) {
				continue
			}
			return out, err
		}
		if c.Fault != nil {
			if err := c.Fault("feedback_effect_committed"); err != nil {
				return out, err
			}
		}
		delivery := "delivered"
		if c.SendFeedback == nil || c.SendFeedback(ctx, row.SessionID, row.Body) != nil {
			delivery = "unknown"
		}
		if err := c.Store.SetWorkGateFeedbackState(ctx, row.ID, "executed", delivery, c.now()); err != nil {
			return out, err
		}
		if delivery == "delivered" {
			out.FeedbackDelivered++
		}
	}
	if err := c.routeEscalations(ctx); err != nil {
		return out, err
	}
	cleanup, err := c.Store.DueWorkGateCleanup(ctx, contract.WorkGateDueRowsPerPassLimit)
	if err != nil {
		return out, err
	}
	for _, taskID := range cleanup {
		purge := c.PurgeEvidence
		if purge == nil && c.Broker != nil {
			purge = c.Broker.PurgeGateSubmissions
		}
		if purge == nil || purge(taskID) != nil {
			continue
		}
		if err := c.Store.CompleteWorkGateCleanup(ctx, taskID, c.now()); err != nil && !errors.Is(err, store.ErrConflict) {
			return out, err
		}
	}
	return out, nil
}

func (c *WorkGateCoordinator) processAttempt(ctx context.Context, due store.WorkGateDue) (WorkGatePass, error) {
	var out WorkGatePass
	a, round, now := due.Attempt, due.Round, c.now()
	if a.State == contract.WorkGateAttemptStateQueued {
		if err := c.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
			return tx.SetWorkGateAttempt(round.ID, a.ID, contract.WorkGateAttemptStateDispatching,
				a.TaskID, a.RespawnOf, "", time.Time{}, now)
		}); err != nil {
			return out, err
		}
		a.State = contract.WorkGateAttemptStateDispatching
		if c.Fault != nil {
			if err := c.Fault("dispatch_intent_committed"); err != nil {
				return out, err
			}
		}
	}
	if a.State == contract.WorkGateAttemptStateDispatching {
		taskID := a.TaskID
		if taskID == "" {
			taskID = a.ID
		}
		in := orchestrator.GateDispatch{TaskID: taskID, WorkID: round.ItemID,
			Title: "Verify " + round.ItemID, Persona: round.CheckerPersona,
			Instructions: "Independently evaluate every frozen acceptance claim and submit the typed gate result.",
			Root: orchestrator.RootRef{SessionID: round.Candidate.OwnerSessionID, Assistant: "codex",
				ProjectDir: round.Candidate.Repository, Label: "Verification owner"},
			Gate: orchestrator.GateOrigin{Origin: orchestrator.TaskKindVerificationGate, RoundID: round.ID,
				Attempt: int(a.Attempt), BaseCommit: due.BaseCommit,
				Acceptance: round.Acceptance, Candidate: round.Candidate}}
		dispatched, err := c.dispatch(ctx, in)
		if err != nil {
			var refusal orchestrator.Refusal
			if errors.As(err, &refusal) && refusal.Status == 429 {
				backoff := time.Duration(contract.WorkGateRetryBackoffSecondsLimit) * time.Second
				return out, c.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
					return tx.SetWorkGateAttempt(round.ID, a.ID, contract.WorkGateAttemptStateQueued,
						taskID, a.RespawnOf, refusal.Code, now.Add(backoff), now)
				})
			}
			// An untyped transport failure may be response loss after the
			// deterministic task was durably admitted. Reconcile before doing
			// anything that could create a second checker.
			if !errors.As(err, &refusal) {
				if held, heldErr := c.record(ctx, taskID); heldErr == nil && held.ID == taskID {
					err = c.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
						return tx.SetWorkGateAttempt(round.ID, a.ID, contract.WorkGateAttemptStateRunning,
							held.ID, a.RespawnOf, "dispatch_response_lost", time.Time{}, now)
					})
					return out, err
				}
				return out, c.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
					return tx.SetWorkGateAttempt(round.ID, a.ID, contract.WorkGateAttemptStateDispatching,
						taskID, a.RespawnOf, "dispatch_unknown", now.Add(time.Duration(contract.WorkGateRetryBackoffSecondsLimit)*time.Second), now)
				})
			}
			return c.technicalFailure(ctx, due, "dispatch_"+refusal.Code)
		}
		if err := c.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
			return tx.SetWorkGateAttempt(round.ID, a.ID, contract.WorkGateAttemptStateRunning,
				dispatched.Record.ID, a.RespawnOf, "", time.Time{}, now)
		}); err != nil {
			return out, err
		}
		out.Dispatched++
		return out, nil
	}
	if a.State != contract.WorkGateAttemptStateRunning {
		return out, nil
	}
	task, err := c.record(ctx, a.TaskID)
	if err != nil {
		return out, nil // broker unavailability is unknown, never a failed verdict
	}
	if !task.State.Terminal() {
		return out, nil
	}
	if task.State == orchestrator.StateSpawnFailed && a.Attempt == 0 {
		respawned, err := c.respawn(ctx, a.TaskID)
		if err != nil {
			descendant, found, findErr := c.findRespawn(ctx, a.TaskID)
			if findErr == nil && found {
				respawned.Record = descendant
			} else if findErr != nil {
				return out, c.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
					return tx.SetWorkGateAttempt(round.ID, a.ID, contract.WorkGateAttemptStateRunning,
						a.TaskID, a.RespawnOf, "respawn_unknown", now.Add(time.Duration(contract.WorkGateRetryBackoffSecondsLimit)*time.Second), now)
				})
			} else {
				return c.technicalFailure(ctx, due, "respawn_failed")
			}
		}
		err = c.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
			if err := tx.SetWorkGateAttempt(round.ID, a.ID, contract.WorkGateAttemptStateFailed,
				a.TaskID, a.RespawnOf, "spawn_failed", time.Time{}, now); err != nil {
				return err
			}
			return tx.AddWorkGateAttempt(round.ID, gateDeterministicID(round.ID+":attempt:1"),
				respawned.Record.ID, a.TaskID, contract.WorkGateAttemptStateRunning, now)
		})
		out.Retried++
		return out, err
	}
	if task.State != orchestrator.StateSuccess || task.Result == nil || task.Result.GateVerdict == nil {
		return c.technicalFailure(ctx, due, "checker_"+string(task.State))
	}
	if err := c.applyResult(ctx, due, *task.Result.GateVerdict); err != nil {
		return out, err
	}
	out.Completed++
	return out, nil
}

func (c *WorkGateCoordinator) technicalFailure(ctx context.Context, due store.WorkGateDue, code string) (WorkGatePass, error) {
	var out WorkGatePass
	now := c.now()
	if due.Attempt.Attempt == 0 {
		err := c.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
			if err := tx.SetWorkGateAttempt(due.Round.ID, due.Attempt.ID, contract.WorkGateAttemptStateFailed,
				due.Attempt.TaskID, due.Attempt.RespawnOf, code, time.Time{}, now); err != nil {
				return err
			}
			return tx.AddWorkGateAttempt(due.Round.ID, gateDeterministicID(due.Round.ID+":attempt:1"),
				"", "", contract.WorkGateAttemptStateQueued, now)
		})
		out.Retried++
		return out, err
	}
	owner, live := c.escalationOwner(ctx, due.Round.ItemID)
	err := c.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		if err := tx.FinishWorkGateAttemptAndRound(due.Round.ID, due.Attempt.ID, code,
			contract.WorkGateRoundStateTechnicalFailure, now); err != nil {
			return err
		}
		if err := c.escalate(tx, due.Round, contract.WorkGateEscalationKindTechnicalVerification,
			"Independent verification failed twice for technical reasons.", 0, owner, live, now); err != nil {
			return err
		}
		item, err := tx.Item(due.Round.ItemID)
		if err != nil {
			return err
		}
		next := item
		next.Condition, next.UserAction, next.UpdatedAt = work.ConditionBlocked, "", now
		if !live {
			next.Condition = work.ConditionWaitingUser
			next.UserAction = "Independent verification failed twice; choose retry, repair, reassign, override, or cancel."
		}
		return tx.PutItem(item, next, "verification.technical_escalated", "verification-coordinator",
			payload(map[string]any{"round_id": due.Round.ID, "failure": code}))
	})
	out.Escalated++
	return out, err
}

func gateTupleMatches(round contract.WorkGateRound, result contract.WorkGateResult) bool {
	return result.RoundID == round.ID && result.CriteriaVersion == round.Acceptance.Version &&
		result.CriteriaDigest == round.Acceptance.Digest && result.CandidateCommit == round.Candidate.Commit &&
		result.CandidateTree == round.Candidate.Tree
}

func (c *WorkGateCoordinator) applyResult(ctx context.Context, due store.WorkGateDue, result contract.WorkGateResult) error {
	if err := contract.ValidateWorkGateResult(result); err != nil {
		return c.secondOrRetryMalformed(ctx, due, "malformed_result")
	}
	if result.TaskID != due.Attempt.TaskID || !gateTupleMatches(due.Round, result) {
		return c.secondOrRetryMalformed(ctx, due, "mismatched_result")
	}
	owner, live := c.escalationOwner(ctx, due.Round.ItemID)
	now := c.now()
	return c.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		item, err := tx.Item(due.Round.ItemID)
		if err != nil {
			return err
		}
		if item.Cycle != due.Round.Cycle || item.AcceptanceVersion != due.Round.Acceptance.Version ||
			item.AcceptanceDigest != due.Round.Acceptance.Digest || item.Phase != work.PhaseVerifying {
			return tx.FinishWorkGateAttemptAndRound(due.Round.ID, due.Attempt.ID, "current_tuple_changed",
				contract.WorkGateRoundStateStale, now)
		}
		if err := tx.SetWorkGateAttempt(due.Round.ID, due.Attempt.ID, contract.WorkGateAttemptStateSucceeded,
			due.Attempt.TaskID, due.Attempt.RespawnOf, "", time.Time{}, now); err != nil {
			return err
		}
		var auth *store.WorkGateAuthorization
		if result.Verdict == contract.WorkGateVerdictPASS {
			auth = &store.WorkGateAuthorization{ItemID: item.ID, RoundID: due.Round.ID, Cycle: item.Cycle,
				CriteriaVersion: item.AcceptanceVersion, CriteriaDigest: item.AcceptanceDigest,
				CandidateCommit: due.Round.Candidate.Commit, CandidateTree: due.Round.Candidate.Tree,
				Kind: "pass", Actor: due.Attempt.TaskID}
		}
		if err := tx.CompleteWorkGateRound(due.Round, result, auth, now); err != nil {
			return err
		}
		next := item
		next.UpdatedAt = now
		switch result.Verdict {
		case contract.WorkGateVerdictPASS:
			next.Condition, next.UserAction = "", ""
			if err := tx.ResetWorkGateFailures(item); err != nil {
				return err
			}
		case contract.WorkGateVerdictNEEDSWORK:
			next.Condition = work.ConditionEvidenceUnknown
			next.UserAction = ""
		case contract.WorkGateVerdictFAIL:
			failed := int64(0)
			for _, claim := range result.Claims {
				if claim.State == contract.WorkGateClaimStateFailed {
					failed++
				}
			}
			consecutive, err := tx.IncrementWorkGateFail(item, failed)
			if err != nil {
				return err
			}
			if consecutive >= 3 {
				if err := c.escalate(tx, due.Round, contract.WorkGateEscalationKindThirdFail,
					"Independent verification failed three consecutive rounds for this exact cycle and acceptance digest.",
					consecutive, owner, live, now); err != nil {
					return err
				}
				next.Condition = work.ConditionBlocked
				if !live {
					next.Condition = work.ConditionWaitingUser
					next.UserAction = "A parent owner or the person must decide the verification escalation."
				}
			} else {
				next.Phase, next.Condition = work.PhaseImplementing, ""
				next.UserAction = ""
			}
		}
		body := strings.TrimSpace(result.Summary)
		if body == "" {
			body = "Independent verification returned " + string(result.Verdict) + "."
		}
		if err := tx.AddWorkGateFeedback(store.WorkGateFeedback{ID: gateDeterministicID(due.Round.ID + ":feedback"),
			ItemID: item.ID, RoundID: due.Round.ID, SessionID: due.Round.Candidate.OwnerSessionID,
			Body: body, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		return tx.PutItem(item, next, "verification."+strings.ToLower(string(result.Verdict)),
			"verification-coordinator", payload(map[string]any{"round_id": due.Round.ID, "verdict": result.Verdict}))
	})
}

func (c *WorkGateCoordinator) secondOrRetryMalformed(ctx context.Context, due store.WorkGateDue, code string) error {
	_, err := c.technicalFailure(ctx, due, code)
	return err
}

func (c *WorkGateCoordinator) escalationOwner(ctx context.Context, itemID string) (string, bool) {
	item, err := c.Store.WorkV2Item(ctx, itemID)
	if err != nil || item.ParentID == "" {
		return "", false
	}
	parent, err := c.Store.WorkV2Item(ctx, item.ParentID)
	if err != nil || parent.OwnerSession == "" {
		return "", false
	}
	live := c.OwnerLive != nil && c.OwnerLive(ctx, parent.OwnerSession)
	return parent.OwnerSession, live
}

func (c *WorkGateCoordinator) escalate(tx *store.WorkV2Tx, round contract.WorkGateRound,
	kind contract.WorkGateEscalationKind, reason string, failures int64, owner string, live bool, now time.Time) error {
	state := contract.WorkGateEscalationStateWaitingUser
	if owner != "" && live {
		state = contract.WorkGateEscalationStateWaitingParentOwner
	}
	if err := tx.CreateWorkGateEscalation(store.WorkGateEscalationRecord{WorkGateEscalation: contract.WorkGateEscalation{
		ID: gateDeterministicID(round.ID + ":escalation"), ItemID: round.ItemID, RoundID: round.ID,
		Kind: kind, State: state, Reason: reason, Candidate: &round.Candidate,
		ConsecutiveFailures: failures, WaitingSince: now.Unix(), CreatedAt: now.Unix(), UpdatedAt: now.Unix()},
		ParentOwner: owner, NotificationState: "accepted"}); err != nil {
		return err
	}
	if owner != "" && live {
		return tx.AddWorkGateFeedback(store.WorkGateFeedback{ID: gateDeterministicID(round.ID + ":parent-courtesy"),
			ItemID: round.ItemID, RoundID: round.ID, SessionID: owner, Body: reason,
			CreatedAt: now, UpdatedAt: now})
	}
	return nil
}

func (c *WorkGateCoordinator) routeEscalations(ctx context.Context) error {
	rows, err := c.Store.OpenWorkGateEscalations(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		now := c.now()
		sendNotification := false
		if row.State == contract.WorkGateEscalationStateWaitingParentOwner {
			if c.OwnerLive != nil && c.OwnerLive(ctx, row.ParentOwner) {
				if row.OwnerOfflineSince != 0 {
					err := c.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
						return tx.SetWorkGateEscalationRouting(row.ID, row.State, 0, row.PromotedAt,
							row.NotificationState, now)
					})
					if err != nil {
						return err
					}
				}
				continue
			}
			if row.OwnerOfflineSince == 0 {
				if err := c.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
					return tx.SetWorkGateEscalationRouting(row.ID, row.State, now.Unix(), row.PromotedAt,
						row.NotificationState, now)
				}); err != nil {
					return err
				}
				continue
			}
			if now.Unix()-row.OwnerOfflineSince < contract.WorkGateOwnerOfflineGraceSecondsLimit {
				continue
			}
			if err := c.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
				item, err := tx.Item(row.ItemID)
				if err != nil {
					return err
				}
				next := item
				next.Condition, next.UserAction, next.UpdatedAt = work.ConditionWaitingUser,
					"The parent owner remained offline for 900 seconds; the person must decide.", now
				if err := tx.PutItem(item, next, "verification.escalation_promoted", "verification-coordinator",
					payload(map[string]any{"escalation_id": row.ID})); err != nil {
					return err
				}
				return tx.SetWorkGateEscalationRouting(row.ID, contract.WorkGateEscalationStateWaitingUser,
					row.OwnerOfflineSince, now.Unix(), "executed", now)
			}); err != nil {
				return err
			}
			row.State, row.NotificationState = contract.WorkGateEscalationStateWaitingUser, "executed"
			sendNotification = true
		}
		if row.State == contract.WorkGateEscalationStateWaitingUser && row.NotificationState == "accepted" {
			if err := c.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
				return tx.SetWorkGateEscalationRouting(row.ID, row.State, row.OwnerOfflineSince,
					row.PromotedAt, "executed", now)
			}); err != nil {
				return err
			}
			row.NotificationState = "executed"
			sendNotification = true
		}
		if row.State == contract.WorkGateEscalationStateWaitingUser && row.NotificationState == "executed" && !sendNotification {
			if err := c.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
				return tx.SetWorkGateEscalationRouting(row.ID, row.State, row.OwnerOfflineSince,
					row.PromotedAt, "unknown", now)
			}); err != nil {
				return err
			}
			continue
		}
		if sendNotification {
			if c.Fault != nil {
				if err := c.Fault("notification_effect_committed"); err != nil {
					return err
				}
			}
			state := "unknown"
			if c.NotifyUser != nil && c.NotifyUser(ctx, "Verification decision required", row.Reason) == nil {
				state = "delivered"
			}
			if err := c.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
				return tx.SetWorkGateEscalationRouting(row.ID, row.State, row.OwnerOfflineSince,
					row.PromotedAt, state, now)
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
