package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// Gate detail is deliberately normalized from the item row. Rounds and
// attempts are mutable lifecycle receipts; events and authorization rows keep
// the append-only explanation of why a gate was opened or invalidated.
const workV2GateSchema = `
CREATE TABLE IF NOT EXISTS work_v2_gate_rounds (
  id TEXT PRIMARY KEY,
  item_id TEXT NOT NULL REFERENCES work_v2_items(id) ON DELETE CASCADE,
  cycle INTEGER NOT NULL,
  criteria TEXT NOT NULL,
  criteria_version INTEGER NOT NULL,
  criteria_digest TEXT NOT NULL,
  base_commit TEXT NOT NULL,
  candidate TEXT NOT NULL,
  checker_persona TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('queued','dispatching','running','complete','stale','technical_failure')),
  result TEXT NOT NULL DEFAULT '',
  stale_reason TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  finished_at INTEGER
);
CREATE INDEX IF NOT EXISTS work_v2_gate_rounds_item
  ON work_v2_gate_rounds(item_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS work_v2_gate_rounds_state
  ON work_v2_gate_rounds(state, updated_at, id);
CREATE TABLE IF NOT EXISTS work_v2_gate_attempts (
  id TEXT PRIMARY KEY,
  round_id TEXT NOT NULL REFERENCES work_v2_gate_rounds(id) ON DELETE CASCADE,
  attempt INTEGER NOT NULL CHECK (attempt IN (0,1)),
  state TEXT NOT NULL CHECK (state IN ('queued','dispatching','running','succeeded','failed','timed_out','stale')),
  task_id TEXT NOT NULL DEFAULT '',
  respawn_of TEXT NOT NULL DEFAULT '',
  failure_code TEXT NOT NULL DEFAULT '',
  next_retry_at INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  finished_at INTEGER,
  UNIQUE(round_id, attempt)
);
CREATE INDEX IF NOT EXISTS work_v2_gate_attempts_due
  ON work_v2_gate_attempts(next_retry_at, created_at, id)
  WHERE state IN ('queued','dispatching','running');
CREATE TABLE IF NOT EXISTS work_v2_gate_authorizations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  item_id TEXT NOT NULL REFERENCES work_v2_items(id) ON DELETE CASCADE,
  round_id TEXT NOT NULL DEFAULT '',
  cycle INTEGER NOT NULL,
  criteria_version INTEGER NOT NULL,
  criteria_digest TEXT NOT NULL,
  candidate_commit TEXT NOT NULL,
  candidate_tree TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('pass','ai_override','person_override','technical_ai_override','technical_person_override')),
  actor TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  invalidated_at INTEGER,
  invalidated_reason TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS work_v2_gate_authorizations_current
  ON work_v2_gate_authorizations(item_id, cycle, criteria_version, criteria_digest, created_at DESC)
  WHERE invalidated_at IS NULL;
CREATE TABLE IF NOT EXISTS work_v2_gate_escalations (
  id TEXT PRIMARY KEY,
  item_id TEXT NOT NULL REFERENCES work_v2_items(id) ON DELETE CASCADE,
  round_id TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL CHECK (kind IN ('third_fail','technical_verification')),
  state TEXT NOT NULL CHECK (state IN ('waiting_parent_owner','waiting_user','resolved')),
  reason TEXT NOT NULL,
  candidate TEXT NOT NULL DEFAULT '',
  consecutive_failures INTEGER NOT NULL DEFAULT 0,
  parent_owner TEXT NOT NULL DEFAULT '',
  waiting_since INTEGER NOT NULL DEFAULT 0,
  owner_offline_since INTEGER NOT NULL DEFAULT 0,
  promoted_at INTEGER NOT NULL DEFAULT 0,
  resolution TEXT NOT NULL DEFAULT '',
  resolution_reason TEXT NOT NULL DEFAULT '',
  resolved_at INTEGER NOT NULL DEFAULT 0,
  notification_state TEXT NOT NULL DEFAULT 'accepted'
    CHECK (notification_state IN ('accepted','executed','delivered','unknown','observed')),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS work_v2_gate_one_open_escalation
  ON work_v2_gate_escalations(item_id) WHERE state <> 'resolved';
CREATE INDEX IF NOT EXISTS work_v2_gate_escalations_due
  ON work_v2_gate_escalations(state, owner_offline_since, created_at, id);
CREATE TABLE IF NOT EXISTS work_v2_gate_feedback (
  id TEXT PRIMARY KEY,
  item_id TEXT NOT NULL REFERENCES work_v2_items(id) ON DELETE CASCADE,
  round_id TEXT NOT NULL REFERENCES work_v2_gate_rounds(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL,
  body TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('accepted','executed','delivered','unknown','observed')),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS work_v2_gate_feedback_due
  ON work_v2_gate_feedback(state, created_at, id);
CREATE TABLE IF NOT EXISTS work_v2_gate_metrics (
  item_id TEXT PRIMARY KEY REFERENCES work_v2_items(id) ON DELETE CASCADE,
  rounds INTEGER NOT NULL DEFAULT 0,
  fails INTEGER NOT NULL DEFAULT 0,
  findings INTEGER NOT NULL DEFAULT 0,
  overrides INTEGER NOT NULL DEFAULT 0,
  consecutive_fails INTEGER NOT NULL DEFAULT 0,
  counter_cycle INTEGER NOT NULL DEFAULT 0,
  counter_digest TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS work_v2_gate_cleanup (
  task_id TEXT PRIMARY KEY,
  state TEXT NOT NULL CHECK (state IN ('accepted','executed')),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS work_v2_gate_cleanup_due ON work_v2_gate_cleanup(state,created_at,task_id);
`

var (
	ErrWorkGateRoundsFull = errors.New("verification round detail capacity full")
	ErrWorkGateActive     = errors.New("verification round already active")
	ErrWorkGateEscalated  = errors.New("verification escalation unresolved")
	ErrNoWorkGateRound    = errors.New("no such verification round")
)

// WorkGateAuthorization is the exact tuple that may open merging or later
// name a landing. Invalidated rows remain for audit and never authorize.
type WorkGateAuthorization struct {
	ItemID, RoundID, CriteriaDigest, CandidateCommit, CandidateTree string
	Cycle, CriteriaVersion                                          int64
	Kind, Actor, Reason                                             string
	CreatedAt, InvalidatedAt                                        time.Time
}

type WorkGateFeedback struct {
	ID, ItemID, RoundID, SessionID, Body, State string
	CreatedAt, UpdatedAt                        time.Time
}

// WorkGateEscalationRecord adds coordinator-only routing state to the public
// escalation contract. The public projection deliberately does not disclose
// session identifiers or notification internals.
type WorkGateEscalationRecord struct {
	contract.WorkGateEscalation
	ParentOwner, NotificationState, ResolutionReason string
	OwnerOfflineSince                                int64
}

type WorkGateDue struct {
	Round      contract.WorkGateRound
	Attempt    contract.WorkGateAttempt
	BaseCommit string
}

type WorkGateMetrics struct {
	contract.WorkGateMetrics
	ConsecutiveFails int64
	CounterCycle     int64
	CounterDigest    string
}

type WorkGateExport struct {
	ItemID, SHA256 string
	ItemVersion    int64
	ByteCount      int64
	RoundCount     int64
	Document       []byte
}

// WorkGateRoundDetailsPerItemCount is the fullest retained verification
// history for one item. Diagnostics reports the scope the limit actually
// refuses rather than the total population across unrelated items.
func (s *Store) WorkGateRoundDetailsPerItemCount(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(n),0) FROM (
      SELECT COUNT(*) n FROM work_v2_gate_rounds GROUP BY item_id)`).Scan(&n)
	return n, err
}

// WorkGateRoundDetailsPerStoreCount is every retained verification round.
func (s *Store) WorkGateRoundDetailsPerStoreCount(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM work_v2_gate_rounds`).Scan(&n)
	return n, err
}

// WorkGateTasksPerRoundCount is the fullest persisted attempt lineage for
// one round. The initial task and its one bounded retry are both attempts.
func (s *Store) WorkGateTasksPerRoundCount(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(n),0) FROM (
      SELECT COUNT(*) n FROM work_v2_gate_attempts GROUP BY round_id)`).Scan(&n)
	return n, err
}

func openWorkV2Gates(db *sql.DB) error {
	if _, err := db.Exec(workV2GateSchema); err != nil {
		return err
	}
	has, err := hasColumn(db, "work_v2_gate_rounds", "base_commit")
	if err != nil {
		return err
	}
	if !has {
		_, err = db.Exec(`ALTER TABLE work_v2_gate_rounds ADD COLUMN base_commit TEXT NOT NULL DEFAULT ''`)
	}
	return err
}

func encodeGate(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func decodeCandidate(raw string) (contract.WorkGateCandidateReceipt, error) {
	var out contract.WorkGateCandidateReceipt
	err := json.Unmarshal([]byte(raw), &out)
	return out, err
}

func decodeGateResult(raw string) (*contract.WorkGateResult, error) {
	if raw == "" {
		return nil, nil
	}
	result, err := contract.DecodeWorkGateResult([]byte(raw))
	return &result, err
}

func (t *WorkV2Tx) CreateWorkGateRound(round contract.WorkGateRound, baseCommit string) error {
	var perItem, total, active, escalation int64
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM work_v2_gate_rounds WHERE item_id=?`, round.ItemID).Scan(&perItem); err != nil {
		return err
	}
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM work_v2_gate_rounds`).Scan(&total); err != nil {
		return err
	}
	if perItem >= contract.WorkGateRoundDetailsPerItemLimit || total >= contract.WorkGateRoundDetailsPerStoreLimit {
		return ErrWorkGateRoundsFull
	}
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM work_v2_gate_rounds
      WHERE item_id=? AND state IN ('queued','dispatching','running')`, round.ItemID).Scan(&active); err != nil {
		return err
	}
	if active != 0 {
		return ErrWorkGateActive
	}
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM work_v2_gate_escalations
      WHERE item_id=? AND state<>'resolved'`, round.ItemID).Scan(&escalation); err != nil {
		return err
	}
	if escalation != 0 {
		return ErrWorkGateEscalated
	}
	if len(round.Attempts) != 1 || round.Attempts[0].Attempt != 0 || round.Attempts[0].RoundID != round.ID {
		return fmt.Errorf("a new verification round needs exactly attempt zero")
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_gate_rounds
      (id,item_id,cycle,criteria,criteria_version,criteria_digest,base_commit,candidate,checker_persona,state,result,
       stale_reason,created_at,updated_at,finished_at) VALUES (?,?,?,?,?,?,?,?,?,?,'','',?,?,NULL)`,
		round.ID, round.ItemID, round.Cycle, round.Acceptance.Criteria, round.Acceptance.Version,
		round.Acceptance.Digest, baseCommit, encodeGate(round.Candidate), round.CheckerPersona, round.State,
		round.CreatedAt, round.UpdatedAt)
	if err != nil {
		return err
	}
	a := round.Attempts[0]
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_gate_attempts
      (id,round_id,attempt,state,task_id,respawn_of,failure_code,next_retry_at,created_at,updated_at,finished_at)
      VALUES (?,?,?,?,?,?,?,0,?,?,NULL)`, a.ID, a.RoundID, a.Attempt, a.State, a.TaskID, a.RespawnOf,
		a.FailureCode, a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_gate_metrics(item_id,rounds,counter_cycle,counter_digest)
      VALUES (?,1,?,?) ON CONFLICT(item_id) DO UPDATE SET rounds=rounds+1`,
		round.ItemID, round.Cycle, round.Acceptance.Digest)
	if err == nil {
		t.wrote += 3
	}
	return err
}

func scanWorkGateRound(sc scanner) (contract.WorkGateRound, error) {
	var out contract.WorkGateRound
	var candidate, result string
	var finished sql.NullInt64
	err := sc.Scan(&out.ID, &out.ItemID, &out.Cycle, &out.Acceptance.Criteria, &out.Acceptance.Version,
		&out.Acceptance.Digest, &candidate, &out.CheckerPersona, &out.State, &result, &out.StaleReason,
		&out.CreatedAt, &out.UpdatedAt, &finished)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal([]byte(candidate), &out.Candidate); err != nil {
		return out, err
	}
	if result != "" {
		decoded, err := contract.DecodeWorkGateResult([]byte(result))
		if err != nil {
			return out, err
		}
		out.Result = &decoded
	}
	if finished.Valid {
		out.FinishedAt = finished.Int64
	}
	return out, nil
}

const workGateRoundColumns = `id,item_id,cycle,criteria,criteria_version,criteria_digest,candidate,
  checker_persona,state,result,stale_reason,created_at,updated_at,finished_at`

func scanWorkGateAttempt(sc scanner) (contract.WorkGateAttempt, error) {
	var out contract.WorkGateAttempt
	var finished sql.NullInt64
	err := sc.Scan(&out.ID, &out.RoundID, &out.Attempt, &out.State, &out.TaskID, &out.RespawnOf,
		&out.FailureCode, &out.NextRetryAt, &out.CreatedAt, &out.UpdatedAt, &finished)
	if finished.Valid {
		out.FinishedAt = finished.Int64
	}
	return out, err
}

const workGateAttemptColumns = `id,round_id,attempt,state,task_id,respawn_of,failure_code,
  next_retry_at,created_at,updated_at,finished_at`

func (t *WorkV2Tx) WorkGateRound(id string) (contract.WorkGateRound, error) {
	round, err := scanWorkGateRound(t.tx.QueryRowContext(t.ctx, `SELECT `+workGateRoundColumns+`
    FROM work_v2_gate_rounds WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return round, ErrNoWorkGateRound
	}
	if err != nil {
		return round, err
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+workGateAttemptColumns+`
    FROM work_v2_gate_attempts WHERE round_id=? ORDER BY attempt,id`, id)
	if err != nil {
		return round, err
	}
	defer rows.Close()
	for rows.Next() {
		a, err := scanWorkGateAttempt(rows)
		if err != nil {
			return round, err
		}
		round.Attempts = append(round.Attempts, a)
	}
	return round, rows.Err()
}

func (s *Store) WorkGateRound(ctx context.Context, id string) (contract.WorkGateRound, error) {
	var out contract.WorkGateRound
	err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		var err error
		out, err = tx.WorkGateRound(id)
		return err
	})
	return out, err
}

// DueWorkGateAttempts is a stable, future-excluding pass ordered so one row
// with a deferred retry cannot starve later runnable work.
func (s *Store) DueWorkGateAttempts(ctx context.Context, now time.Time, limit int) ([]WorkGateDue, error) {
	if limit <= 0 || limit > contract.WorkGateDueRowsPerPassLimit {
		return nil, fmt.Errorf("verification due limit must be 1..%d", contract.WorkGateDueRowsPerPassLimit)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+workGateAttemptColumns+`
    FROM work_v2_gate_attempts WHERE state IN ('queued','dispatching','running')
      AND (next_retry_at=0 OR next_retry_at<=?) ORDER BY next_retry_at,created_at,id LIMIT ?`, now.Unix(), limit)
	if err != nil {
		return nil, err
	}
	attempts := []contract.WorkGateAttempt{}
	for rows.Next() {
		a, err := scanWorkGateAttempt(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		attempts = append(attempts, a)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	out := make([]WorkGateDue, 0, len(attempts))
	for _, a := range attempts {
		var baseCommit string
		r, err := scanWorkGateRound(s.db.QueryRowContext(ctx, `SELECT `+workGateRoundColumns+`
      FROM work_v2_gate_rounds WHERE id=?`, a.RoundID))
		if err != nil {
			return nil, err
		}
		r.Attempts = []contract.WorkGateAttempt{a}
		if err := s.db.QueryRowContext(ctx, `SELECT base_commit FROM work_v2_gate_rounds WHERE id=?`, a.RoundID).Scan(&baseCommit); err != nil {
			return nil, err
		}
		out = append(out, WorkGateDue{Round: r, Attempt: a, BaseCommit: baseCommit})
	}
	return out, nil
}

func (t *WorkV2Tx) SetWorkGateAttempt(roundID, attemptID string, state contract.WorkGateAttemptState,
	taskID, respawnOf, failure string, nextRetry time.Time, at time.Time) error {
	finished := any(nil)
	if state == contract.WorkGateAttemptStateSucceeded || state == contract.WorkGateAttemptStateFailed ||
		state == contract.WorkGateAttemptStateTimedOut || state == contract.WorkGateAttemptStateStale {
		finished = at.Unix()
	}
	res, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_gate_attempts SET state=?,task_id=?,respawn_of=?,
      failure_code=?,next_retry_at=?,updated_at=?,finished_at=? WHERE id=? AND round_id=?`, state, taskID,
		respawnOf, failure, gateUnix(nextRetry), at.Unix(), finished, attemptID, roundID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	roundState := contract.WorkGateRoundStateRunning
	if state == contract.WorkGateAttemptStateQueued {
		roundState = contract.WorkGateRoundStateQueued
	} else if state == contract.WorkGateAttemptStateDispatching {
		roundState = contract.WorkGateRoundStateDispatching
	}
	_, err = t.tx.ExecContext(t.ctx, `UPDATE work_v2_gate_rounds SET state=?,updated_at=?
      WHERE id=? AND state IN ('queued','dispatching','running')`, roundState, at.Unix(), roundID)
	if err == nil {
		t.wrote += 2
	}
	return err
}

func gateUnix(at time.Time) int64 {
	if at.IsZero() {
		return 0
	}
	return at.Unix()
}

func (t *WorkV2Tx) AddWorkGateAttempt(roundID, attemptID, taskID, respawnOf string,
	state contract.WorkGateAttemptState, at time.Time) error {
	var count int64
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM work_v2_gate_attempts WHERE round_id=?`, roundID).Scan(&count); err != nil {
		return err
	}
	if count >= contract.WorkGateTasksPerRoundLimit {
		return ErrWorkGateRoundsFull
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_gate_attempts
      (id,round_id,attempt,state,task_id,respawn_of,next_retry_at,created_at,updated_at)
      VALUES (?,?,1,?,?,?,0,?,?)`, attemptID, roundID, state, taskID, respawnOf, at.Unix(), at.Unix())
	if err == nil {
		t.wrote++
	}
	return err
}

func (t *WorkV2Tx) CompleteWorkGateRound(round contract.WorkGateRound, result contract.WorkGateResult,
	authorization *WorkGateAuthorization, at time.Time) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_gate_rounds SET state='complete',result=?,updated_at=?,finished_at=?
    WHERE id=? AND state IN ('queued','dispatching','running')`, encodeGate(result), at.Unix(), at.Unix(), round.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrConflict
	}
	if authorization != nil {
		_, err = t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_gate_authorizations
      (item_id,round_id,cycle,criteria_version,criteria_digest,candidate_commit,candidate_tree,kind,actor,reason,created_at)
      VALUES (?,?,?,?,?,?,?,?,?,?,?)`, authorization.ItemID, authorization.RoundID, authorization.Cycle,
			authorization.CriteriaVersion, authorization.CriteriaDigest, authorization.CandidateCommit,
			authorization.CandidateTree, authorization.Kind, authorization.Actor, authorization.Reason, at.Unix())
		if err != nil {
			return err
		}
		t.wrote++
	}
	t.wrote++
	return nil
}

func (t *WorkV2Tx) FinishWorkGateAttemptAndRound(roundID, attemptID, failure string,
	state contract.WorkGateRoundState, at time.Time) error {
	if state != contract.WorkGateRoundStateStale && state != contract.WorkGateRoundStateTechnicalFailure {
		return fmt.Errorf("unsupported terminal verification round state %q", state)
	}
	attemptState := contract.WorkGateAttemptStateFailed
	if state == contract.WorkGateRoundStateStale {
		attemptState = contract.WorkGateAttemptStateStale
	}
	if _, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_gate_attempts SET state=?,failure_code=?,updated_at=?,finished_at=?
      WHERE id=? AND round_id=? AND state IN ('queued','dispatching','running')`, attemptState, failure,
		at.Unix(), at.Unix(), attemptID, roundID); err != nil {
		return err
	}
	res, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_gate_rounds SET state=?,stale_reason=?,updated_at=?,finished_at=?
      WHERE id=? AND state IN ('queued','dispatching','running')`, state, failure, at.Unix(), at.Unix(), roundID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrConflict
	}
	t.wrote += 2
	return nil
}

func (t *WorkV2Tx) CreateWorkGateEscalation(row WorkGateEscalationRecord) error {
	candidate := ""
	if row.Candidate != nil {
		candidate = encodeGate(row.Candidate)
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_gate_escalations
      (id,item_id,round_id,kind,state,reason,candidate,consecutive_failures,parent_owner,waiting_since,
       owner_offline_since,promoted_at,resolution,resolution_reason,resolved_at,notification_state,created_at,updated_at)
      VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, row.ID, row.ItemID, row.RoundID, row.Kind, row.State,
		row.Reason, candidate, row.ConsecutiveFailures, row.ParentOwner, row.WaitingSince,
		row.OwnerOfflineSince, row.PromotedAt, row.Resolution, row.ResolutionReason, row.ResolvedAt,
		row.NotificationState, row.CreatedAt, row.UpdatedAt)
	if err == nil {
		t.wrote++
	}
	return err
}

func scanWorkGateEscalation(sc scanner) (WorkGateEscalationRecord, error) {
	var out WorkGateEscalationRecord
	var candidate string
	err := sc.Scan(&out.ID, &out.ItemID, &out.RoundID, &out.Kind, &out.State, &out.Reason, &candidate,
		&out.ConsecutiveFailures, &out.ParentOwner, &out.WaitingSince, &out.OwnerOfflineSince,
		&out.PromotedAt, &out.Resolution, &out.ResolutionReason, &out.ResolvedAt,
		&out.NotificationState, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return out, err
	}
	if candidate != "" {
		var value contract.WorkGateCandidateReceipt
		if err := json.Unmarshal([]byte(candidate), &value); err != nil {
			return out, err
		}
		out.Candidate = &value
	}
	return out, nil
}

const workGateEscalationColumns = `id,item_id,round_id,kind,state,reason,candidate,consecutive_failures,
  parent_owner,waiting_since,owner_offline_since,promoted_at,resolution,resolution_reason,resolved_at,
  notification_state,created_at,updated_at`

func (t *WorkV2Tx) OpenWorkGateEscalation(itemID string) (WorkGateEscalationRecord, error) {
	return scanWorkGateEscalation(t.tx.QueryRowContext(t.ctx, `SELECT `+workGateEscalationColumns+`
    FROM work_v2_gate_escalations WHERE item_id=? AND state<>'resolved'`, itemID))
}

func (s *Store) OpenWorkGateEscalations(ctx context.Context) ([]WorkGateEscalationRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+workGateEscalationColumns+`
    FROM work_v2_gate_escalations WHERE state<>'resolved' ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkGateEscalationRecord{}
	for rows.Next() {
		row, err := scanWorkGateEscalation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (t *WorkV2Tx) SetWorkGateEscalationRouting(id string, state contract.WorkGateEscalationState,
	offlineSince, promotedAt int64, notificationState string, at time.Time) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_gate_escalations SET state=?,owner_offline_since=?,
      promoted_at=?,notification_state=?,updated_at=? WHERE id=? AND state<>'resolved'`, state,
		offlineSince, promotedAt, notificationState, at.Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrConflict
	}
	t.wrote++
	return nil
}

func (t *WorkV2Tx) ResolveWorkGateEscalation(id string, action contract.WorkGateDecisionAction,
	reason string, at time.Time) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_gate_escalations SET state='resolved',resolution=?,
      resolution_reason=?,resolved_at=?,updated_at=? WHERE id=? AND state<>'resolved'`, action, reason,
		at.Unix(), at.Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrConflict
	}
	t.wrote++
	return nil
}

func (t *WorkV2Tx) ReassignWorkGateOwner(item work.ItemV2, assignmentID, target, actor string, at time.Time) error {
	if _, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_assignments SET state='released',released_at=?,updated_at=?
      WHERE work_id=? AND state='active'`, at.Unix(), at.Unix(), item.ID); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_assignments
      (id,work_id,mode,session_id,state,human_actor,created_at,updated_at,gate_previewed,planning_gate,verify_gate,
       cycle_base_commit)
      VALUES (?,?,'existing_session',?,'active',?,?,?,1,?,?,?)`, assignmentID, item.ID, target, actor,
		at.Unix(), at.Unix(), item.PlanningGate, item.VerifyGate, item.CycleBaseCommit)
	if err == nil {
		t.wrote += 2
	}
	return err
}

func (t *WorkV2Tx) ReleaseWorkGateOwner(itemID string, at time.Time) error {
	_, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_assignments SET state='released',released_at=?,updated_at=?
    WHERE work_id=? AND state='active'`, at.Unix(), at.Unix(), itemID)
	if err == nil {
		t.wrote++
	}
	return err
}

func (t *WorkV2Tx) WorkGateAuthorization(item work.ItemV2) (WorkGateAuthorization, error) {
	var out WorkGateAuthorization
	var created int64
	err := t.tx.QueryRowContext(t.ctx, `SELECT item_id,round_id,cycle,criteria_version,criteria_digest,
      candidate_commit,candidate_tree,kind,actor,reason,created_at FROM work_v2_gate_authorizations a
      WHERE a.item_id=? AND a.cycle=? AND a.criteria_version=? AND a.criteria_digest=? AND a.invalidated_at IS NULL
        AND EXISTS (SELECT 1 FROM work_v2_gate_rounds r WHERE r.id=a.round_id AND r.item_id=a.item_id
          AND r.cycle=a.cycle AND r.criteria_version=a.criteria_version AND r.criteria_digest=a.criteria_digest
          AND json_extract(r.candidate,'$.commit')=a.candidate_commit AND json_extract(r.candidate,'$.tree')=a.candidate_tree)
      ORDER BY a.id DESC LIMIT 1`, item.ID, item.Cycle, item.AcceptanceVersion, item.AcceptanceDigest).Scan(
		&out.ItemID, &out.RoundID, &out.Cycle, &out.CriteriaVersion, &out.CriteriaDigest,
		&out.CandidateCommit, &out.CandidateTree, &out.Kind, &out.Actor, &out.Reason, &created)
	if err != nil {
		return out, err
	}
	out.CreatedAt = time.Unix(created, 0)
	return out, nil
}

// WorkGateCompactDetails is the Board-card gate read. It uses a fixed number
// of bounded queries for the entire page and never reads claim evidence,
// acceptance text, attempts, or round history.
func (s *Store) WorkGateCompactDetails(ctx context.Context, items []work.ItemV2) (map[string]contract.WorkGateCompactRead, error) {
	out := make(map[string]contract.WorkGateCompactRead, len(items))
	if len(items) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
		out[item.ID] = contract.WorkGateCompactRead{PlanningGate: item.PlanningGate,
			VerifyGate: item.VerifyGate, GateSnapshotCycle: item.GateSnapshotCycle}
	}
	rawIDs, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	list := string(rawIDs)

	metrics, err := s.db.QueryContext(ctx, `SELECT item_id,rounds,fails,findings,overrides
    FROM work_v2_gate_metrics WHERE item_id IN (SELECT value FROM json_each(?))`, list)
	if err != nil {
		return nil, err
	}
	for metrics.Next() {
		var itemID string
		var value contract.WorkGateMetrics
		if err := metrics.Scan(&itemID, &value.Rounds, &value.Fails, &value.Findings, &value.Overrides); err != nil {
			metrics.Close()
			return nil, err
		}
		compact := out[itemID]
		compact.Metrics = value
		out[itemID] = compact
	}
	if err := metrics.Err(); err != nil {
		metrics.Close()
		return nil, err
	}
	if err := metrics.Close(); err != nil {
		return nil, err
	}

	rounds, err := s.db.QueryContext(ctx, `SELECT r.id,r.item_id,r.state,json_extract(r.candidate,'$.commit'),
      r.criteria_digest,r.created_at,COALESCE(r.finished_at,0),
      CASE WHEN json_valid(r.result) THEN COALESCE(json_extract(r.result,'$.verdict'),'') ELSE '' END
    FROM work_v2_gate_rounds r WHERE r.item_id IN (SELECT value FROM json_each(?))
      AND r.id=(SELECT newer.id FROM work_v2_gate_rounds newer WHERE newer.item_id=r.item_id
        ORDER BY newer.created_at DESC,newer.id DESC LIMIT 1)`, list)
	if err != nil {
		return nil, err
	}
	for rounds.Next() {
		var itemID, state, verdict string
		var value contract.WorkGateRoundSummary
		if err := rounds.Scan(&value.ID, &itemID, &state, &value.CandidateCommit, &value.CriteriaDigest,
			&value.CreatedAt, &value.FinishedAt, &verdict); err != nil {
			rounds.Close()
			return nil, err
		}
		value.State, value.Verdict = contract.WorkGateRoundState(state), contract.WorkGateVerdict(verdict)
		compact := out[itemID]
		compact.LatestRound = &value
		out[itemID] = compact
	}
	if err := rounds.Err(); err != nil {
		rounds.Close()
		return nil, err
	}
	if err := rounds.Close(); err != nil {
		return nil, err
	}

	escalations, err := s.db.QueryContext(ctx, `SELECT `+workGateEscalationColumns+`
    FROM work_v2_gate_escalations WHERE item_id IN (SELECT value FROM json_each(?)) AND state<>'resolved'`, list)
	if err != nil {
		return nil, err
	}
	for escalations.Next() {
		row, err := scanWorkGateEscalation(escalations)
		if err != nil {
			escalations.Close()
			return nil, err
		}
		compact := out[row.ItemID]
		value := row.WorkGateEscalation
		compact.Escalation = &value
		out[row.ItemID] = compact
	}
	if err := escalations.Err(); err != nil {
		escalations.Close()
		return nil, err
	}
	if err := escalations.Close(); err != nil {
		return nil, err
	}

	authorizations, err := s.db.QueryContext(ctx, `SELECT a.item_id,a.kind,a.reason,a.created_at
    FROM work_v2_gate_authorizations a JOIN work_v2_items i ON i.id=a.item_id
    JOIN work_v2_gate_rounds r ON r.id=a.round_id AND r.item_id=a.item_id
    WHERE a.item_id IN (SELECT value FROM json_each(?)) AND a.invalidated_at IS NULL
      AND a.cycle=i.cycle AND a.criteria_version=i.acceptance_version AND a.criteria_digest=i.acceptance_digest
      AND r.cycle=a.cycle AND r.criteria_version=a.criteria_version AND r.criteria_digest=a.criteria_digest
      AND json_extract(r.candidate,'$.commit')=a.candidate_commit
      AND json_extract(r.candidate,'$.tree')=a.candidate_tree
      AND a.id=(SELECT MAX(newer.id) FROM work_v2_gate_authorizations newer
        WHERE newer.item_id=a.item_id AND newer.invalidated_at IS NULL)`, list)
	if err != nil {
		return nil, err
	}
	for authorizations.Next() {
		var itemID string
		var value contract.WorkGateAuthorizationSummary
		if err := authorizations.Scan(&itemID, &value.Kind, &value.Reason, &value.CreatedAt); err != nil {
			authorizations.Close()
			return nil, err
		}
		compact := out[itemID]
		compact.CurrentAuthorization = &value
		out[itemID] = compact
	}
	if err := authorizations.Err(); err != nil {
		authorizations.Close()
		return nil, err
	}
	if err := authorizations.Close(); err != nil {
		return nil, err
	}
	return out, nil
}

func (t *WorkV2Tx) AddWorkGateAuthorization(authorization WorkGateAuthorization, at time.Time) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_gate_authorizations
      (item_id,round_id,cycle,criteria_version,criteria_digest,candidate_commit,candidate_tree,kind,actor,reason,created_at)
      VALUES (?,?,?,?,?,?,?,?,?,?,?)`, authorization.ItemID, authorization.RoundID, authorization.Cycle,
		authorization.CriteriaVersion, authorization.CriteriaDigest, authorization.CandidateCommit,
		authorization.CandidateTree, authorization.Kind, authorization.Actor, authorization.Reason, at.Unix())
	if err != nil {
		return err
	}
	if authorization.Kind != "pass" {
		_, err = t.tx.ExecContext(t.ctx, `UPDATE work_v2_gate_metrics SET overrides=overrides+1 WHERE item_id=?`,
			authorization.ItemID)
	}
	if err == nil {
		t.wrote++
	}
	return err
}

func (t *WorkV2Tx) invalidateWorkGateAuthorization(prev, next work.ItemV2, reason string) error {
	now := next.UpdatedAt
	if now.IsZero() {
		now = time.Now().Truncate(time.Second)
	}
	if _, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_gate_authorizations
      SET invalidated_at=?,invalidated_reason=? WHERE item_id=? AND invalidated_at IS NULL`, now.Unix(), reason, prev.ID); err != nil {
		return err
	}
	if _, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_gate_rounds
      SET state='stale',stale_reason=?,updated_at=?,finished_at=COALESCE(finished_at,?)
      WHERE item_id=? AND state IN ('queued','dispatching','running','complete')
        AND (cycle<>? OR criteria_version<>? OR criteria_digest<>?)`, reason, now.Unix(), now.Unix(), prev.ID,
		next.Cycle, next.AcceptanceVersion, next.AcceptanceDigest); err != nil {
		return err
	}
	if prev.AcceptanceDigest != next.AcceptanceDigest || prev.Cycle != next.Cycle {
		if _, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_gate_metrics SET consecutive_fails=0,
        counter_cycle=?,counter_digest=? WHERE item_id=?`, next.Cycle, next.AcceptanceDigest, prev.ID); err != nil {
			return err
		}
	}
	t.wrote++
	return nil
}

func (t *WorkV2Tx) WorkGateMetrics(itemID string) (WorkGateMetrics, error) {
	var out WorkGateMetrics
	err := t.tx.QueryRowContext(t.ctx, `SELECT rounds,fails,findings,overrides,consecutive_fails,
      counter_cycle,counter_digest FROM work_v2_gate_metrics WHERE item_id=?`, itemID).Scan(
		&out.Rounds, &out.Fails, &out.Findings, &out.Overrides, &out.ConsecutiveFails,
		&out.CounterCycle, &out.CounterDigest)
	if err == sql.ErrNoRows {
		return out, nil
	}
	return out, err
}

func (t *WorkV2Tx) WorkGateCheckerPersona(item work.ItemV2) (string, error) {
	switch item.Kind {
	case work.KindIssue:
		return "code-reviewer", nil
	case work.KindEpic:
		return "reality-checker", nil
	case work.KindFeature:
		var visual int64
		err := t.tx.QueryRowContext(t.ctx, `SELECT
        (SELECT COUNT(*) FROM work_v2_images WHERE work_id=?) +
        (SELECT COUNT(*) FROM work_v2_documents WHERE work_id=? AND role='design')`, item.ID, item.ID).Scan(&visual)
		if err != nil {
			return "", err
		}
		if visual > 0 {
			return "evidence-collector", nil
		}
		return "reality-checker", nil
	default:
		return "", fmt.Errorf("%s has no verification checker", item.Kind)
	}
}

func (t *WorkV2Tx) IncrementWorkGateFail(item work.ItemV2, findings int64) (int64, error) {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_gate_metrics
      (item_id,fails,findings,consecutive_fails,counter_cycle,counter_digest) VALUES (?,1,?,1,?,?)
      ON CONFLICT(item_id) DO UPDATE SET fails=fails+1,findings=findings+excluded.findings,
        consecutive_fails=CASE WHEN counter_cycle=excluded.counter_cycle AND counter_digest=excluded.counter_digest
          THEN consecutive_fails+1 ELSE 1 END,
        counter_cycle=excluded.counter_cycle,counter_digest=excluded.counter_digest`, item.ID, findings,
		item.Cycle, item.AcceptanceDigest)
	if err != nil {
		return 0, err
	}
	var count int64
	err = t.tx.QueryRowContext(t.ctx, `SELECT consecutive_fails FROM work_v2_gate_metrics WHERE item_id=?`, item.ID).Scan(&count)
	if err == nil {
		t.wrote++
	}
	return count, err
}

func (t *WorkV2Tx) ResetWorkGateFailures(item work.ItemV2) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_gate_metrics(item_id,counter_cycle,counter_digest)
    VALUES (?,?,?) ON CONFLICT(item_id) DO UPDATE SET consecutive_fails=0,counter_cycle=excluded.counter_cycle,
      counter_digest=excluded.counter_digest`, item.ID, item.Cycle, item.AcceptanceDigest)
	if err == nil {
		t.wrote++
	}
	return err
}

func (t *WorkV2Tx) AddWorkGateFeedback(row WorkGateFeedback) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_gate_feedback
      (id,item_id,round_id,session_id,body,state,created_at,updated_at) VALUES (?,?,?,?,?,'accepted',?,?)`,
		row.ID, row.ItemID, row.RoundID, row.SessionID, row.Body, row.CreatedAt.Unix(), row.UpdatedAt.Unix())
	if err == nil {
		t.wrote++
	}
	return err
}

func (s *Store) DueWorkGateFeedback(ctx context.Context, limit int) ([]WorkGateFeedback, error) {
	if limit <= 0 || limit > contract.WorkGateDueRowsPerPassLimit {
		return nil, fmt.Errorf("verification feedback limit must be 1..%d", contract.WorkGateDueRowsPerPassLimit)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,item_id,round_id,session_id,body,state,created_at,updated_at
    FROM work_v2_gate_feedback WHERE state IN ('accepted','executed') ORDER BY created_at,id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkGateFeedback{}
	for rows.Next() {
		var row WorkGateFeedback
		var created, updated int64
		if err := rows.Scan(&row.ID, &row.ItemID, &row.RoundID, &row.SessionID, &row.Body, &row.State,
			&created, &updated); err != nil {
			return nil, err
		}
		row.CreatedAt, row.UpdatedAt = time.Unix(created, 0), time.Unix(updated, 0)
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) WorkGateFeedbacks(ctx context.Context, itemID string) ([]WorkGateFeedback, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,item_id,round_id,session_id,body,state,created_at,updated_at
    FROM work_v2_gate_feedback WHERE item_id=? ORDER BY created_at,id`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkGateFeedback{}
	for rows.Next() {
		var row WorkGateFeedback
		var created, updated int64
		if err := rows.Scan(&row.ID, &row.ItemID, &row.RoundID, &row.SessionID, &row.Body, &row.State,
			&created, &updated); err != nil {
			return nil, err
		}
		row.CreatedAt, row.UpdatedAt = time.Unix(created, 0), time.Unix(updated, 0)
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) SetWorkGateFeedbackState(ctx context.Context, id, from, to string, at time.Time) error {
	return s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		res, err := tx.tx.ExecContext(ctx, `UPDATE work_v2_gate_feedback SET state=?,updated_at=? WHERE id=? AND state=?`,
			to, at.Unix(), id, from)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrConflict
		}
		tx.wrote++
		return nil
	})
}

func (s *Store) ObserveWorkGateFeedback(ctx context.Context, sessionID string, at time.Time) error {
	return s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		res, err := tx.tx.ExecContext(ctx, `UPDATE work_v2_gate_feedback SET state='observed',updated_at=?
      WHERE session_id=? AND state IN ('delivered','unknown')`, at.Unix(), sessionID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err == nil {
			tx.wrote += n
		}
		return err
	})
}

func (s *Store) WorkGateRecentRounds(ctx context.Context, itemID string, limit int) ([]contract.WorkGateRound, bool, error) {
	if limit <= 0 || limit > contract.WorkGateRecentRoundsPerItemReadLimit {
		return nil, false, fmt.Errorf("verification history limit must be 1..%d", contract.WorkGateRecentRoundsPerItemReadLimit)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+workGateRoundColumns+` FROM work_v2_gate_rounds
    WHERE item_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, itemID, limit+1)
	if err != nil {
		return nil, false, err
	}
	out := []contract.WorkGateRound{}
	for rows.Next() {
		r, err := scanWorkGateRound(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, false, err
	}
	if err := rows.Close(); err != nil {
		return nil, false, err
	}
	truncated := len(out) > limit
	if truncated {
		out = out[:limit]
	}
	for n := range out {
		full, err := s.WorkGateRound(ctx, out[n].ID)
		if err != nil {
			return nil, false, err
		}
		out[n] = full
	}
	return out, truncated, nil
}

func (s *Store) WorkGateDetail(ctx context.Context, item work.ItemV2) (contract.WorkGateDetailRead, error) {
	rounds, truncated, err := s.WorkGateRecentRounds(ctx, item.ID, contract.WorkGateRecentRoundsPerItemReadLimit)
	if err != nil {
		return contract.WorkGateDetailRead{}, err
	}
	for n := range rounds {
		rounds[n] = publicWorkGateRound(rounds[n])
	}
	var metrics WorkGateMetrics
	var authorization *WorkGateAuthorization
	err = s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		var inner error
		metrics, inner = tx.WorkGateMetrics(item.ID)
		if inner != nil {
			return inner
		}
		current, inner := tx.WorkGateAuthorization(item)
		if inner == sql.ErrNoRows {
			return nil
		}
		if inner != nil {
			return inner
		}
		authorization = &current
		return nil
	})
	if err != nil {
		return contract.WorkGateDetailRead{}, err
	}
	detail := contract.WorkGateDetailRead{Acceptance: contract.WorkGateAcceptance{Criteria: item.AcceptanceCriteria,
		Version: item.AcceptanceVersion, Digest: item.AcceptanceDigest}, RecentRounds: rounds,
		RecentRoundsTruncated: truncated, Compact: contract.WorkGateCompactRead{PlanningGate: item.PlanningGate,
			VerifyGate: item.VerifyGate, GateSnapshotCycle: item.GateSnapshotCycle, Metrics: metrics.WorkGateMetrics}}
	if authorization != nil {
		detail.Compact.CurrentAuthorization = &contract.WorkGateAuthorizationSummary{Kind: authorization.Kind,
			Reason: authorization.Reason, CreatedAt: authorization.CreatedAt.Unix()}
	}
	if item.HasGateSnapshot() {
		detail.CycleSnapshot = &contract.WorkGateCycleSnapshot{Cycle: item.GateSnapshotCycle,
			CapturedAt: item.GateSnapshotAt.Unix(), PlanningGate: item.PlanningGate, VerifyGate: item.VerifyGate,
			CycleBaseCommit: item.CycleBaseCommit}
	}
	if len(rounds) != 0 {
		r := rounds[0]
		detail.Candidate = &r.Candidate
		summary := contract.WorkGateRoundSummary{ID: r.ID, State: r.State, CandidateCommit: r.Candidate.Commit,
			CriteriaDigest: r.Acceptance.Digest, CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt}
		if r.Result != nil {
			summary.Verdict = r.Result.Verdict
		}
		detail.Compact.LatestRound = &summary
	}
	row, err := scanWorkGateEscalation(s.db.QueryRowContext(ctx, `SELECT `+workGateEscalationColumns+`
    FROM work_v2_gate_escalations WHERE item_id=? AND state<>'resolved'`, item.ID))
	if err == nil {
		if row.Candidate != nil {
			candidate := publicWorkGateCandidate(*row.Candidate)
			row.Candidate = &candidate
		}
		detail.Compact.Escalation = &row.WorkGateEscalation
	} else if err != sql.ErrNoRows {
		return contract.WorkGateDetailRead{}, err
	}
	return detail, nil
}

// Public gate reads keep the immutable identity needed to understand a
// verdict, but never carry this machine's repository or worktree paths. The
// coordinator reads the stored round directly and retains both paths when it
// creates or validates the checker checkout.
func publicWorkGateCandidate(candidate contract.WorkGateCandidateReceipt) contract.WorkGateCandidateReceipt {
	candidate.Repository = ""
	candidate.Worktree = ""
	return candidate
}

func publicWorkGateRound(round contract.WorkGateRound) contract.WorkGateRound {
	round.Candidate = publicWorkGateCandidate(round.Candidate)
	return round
}

func (t *WorkV2Tx) exportableWorkGateRounds(item work.ItemV2) ([]contract.WorkGateRound, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT id FROM work_v2_gate_rounds r
    WHERE r.item_id=? AND r.state IN ('complete','stale','technical_failure')
      AND NOT EXISTS (SELECT 1 FROM work_v2_gate_authorizations a
        WHERE a.round_id=r.id AND a.invalidated_at IS NULL)
      AND NOT EXISTS (SELECT 1 FROM work_v2_gate_escalations e
        WHERE e.round_id=r.id AND e.state<>'resolved')
      AND NOT (r.cycle=? AND r.criteria_digest=? AND
        CASE WHEN json_valid(r.result) THEN json_extract(r.result,'$.verdict')='FAIL' ELSE 0 END
        AND COALESCE((SELECT consecutive_fails FROM work_v2_gate_metrics m WHERE m.item_id=r.item_id),0)>0)
    ORDER BY r.created_at,r.id`, item.ID, item.Cycle, item.AcceptanceDigest)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	out := make([]contract.WorkGateRound, 0, len(ids))
	for _, id := range ids {
		round, err := t.WorkGateRound(id)
		if err != nil {
			return nil, err
		}
		out = append(out, round)
	}
	return out, nil
}

func workGateExportDocument(item work.ItemV2, rounds []contract.WorkGateRound) WorkGateExport {
	publicRounds := make([]contract.WorkGateRound, len(rounds))
	for n := range rounds {
		publicRounds[n] = publicWorkGateRound(rounds[n])
	}
	document, _ := json.Marshal(struct {
		Protocol    string                   `json:"protocol"`
		ItemID      string                   `json:"item_id"`
		ItemVersion int64                    `json:"item_version"`
		Rounds      []contract.WorkGateRound `json:"rounds"`
	}{Protocol: "clawdline.work-gate-export.v1", ItemID: item.ID, ItemVersion: item.Version, Rounds: publicRounds})
	document = append(document, '\n')
	sum := sha256.Sum256(document)
	return WorkGateExport{ItemID: item.ID, ItemVersion: item.Version, SHA256: hex.EncodeToString(sum[:]),
		ByteCount: int64(len(document)), RoundCount: int64(len(rounds)), Document: document}
}

func (s *Store) ExportWorkGateDetails(ctx context.Context, itemID string) (WorkGateExport, error) {
	var out WorkGateExport
	err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		item, err := tx.Item(itemID)
		if err != nil {
			return err
		}
		rounds, err := tx.exportableWorkGateRounds(item)
		if err != nil {
			return err
		}
		out = workGateExportDocument(item, rounds)
		return nil
	})
	return out, err
}

func (s *Store) PurgeWorkGateDetails(ctx context.Context, itemID string, expectedVersion int64,
	expectedDigest string, at time.Time) (int64, error) {
	var purged int64
	err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		item, err := tx.Item(itemID)
		if err != nil {
			return err
		}
		if item.Version != expectedVersion {
			return ErrConflict
		}
		rounds, err := tx.exportableWorkGateRounds(item)
		if err != nil {
			return err
		}
		export := workGateExportDocument(item, rounds)
		if export.SHA256 != expectedDigest {
			return ErrConflict
		}
		for _, round := range rounds {
			for _, attempt := range round.Attempts {
				taskID := attempt.TaskID
				if taskID == "" {
					taskID = attempt.ID
				}
				if _, err := tx.tx.ExecContext(ctx, `INSERT INTO work_v2_gate_cleanup(task_id,state,created_at,updated_at)
            VALUES (?,'accepted',?,?) ON CONFLICT(task_id) DO NOTHING`, taskID, at.Unix(), at.Unix()); err != nil {
					return err
				}
			}
			res, err := tx.tx.ExecContext(ctx, `DELETE FROM work_v2_gate_rounds WHERE id=?`, round.ID)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			purged += n
		}
		next := item
		next.UpdatedAt = at
		if err := tx.PutItem(item, next, "verification.details_purged", "person",
			encodeGate(map[string]any{"sha256": export.SHA256, "rounds": purged})); err != nil {
			return err
		}
		tx.wrote += purged
		return nil
	})
	return purged, err
}

func (s *Store) DueWorkGateCleanup(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 || limit > contract.WorkGateDueRowsPerPassLimit {
		return nil, fmt.Errorf("verification cleanup limit must be 1..%d", contract.WorkGateDueRowsPerPassLimit)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT task_id FROM work_v2_gate_cleanup
    WHERE state='accepted' ORDER BY created_at,task_id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) CompleteWorkGateCleanup(ctx context.Context, taskID string, _ time.Time) error {
	return s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		res, err := tx.tx.ExecContext(ctx, `DELETE FROM work_v2_gate_cleanup
      WHERE task_id=? AND state='accepted'`, taskID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrConflict
		}
		tx.wrote++
		return nil
	})
}

// Compile-time use of decode helpers guards accidental weakening when the
// schema changes independently of the generated contract.
var _ = decodeCandidate
var _ = decodeGateResult
