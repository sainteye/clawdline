package projectassistantstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// RoutineRepository is additive to the frozen chat repository port.
func (s *Store) Routines(ctx context.Context, key pa.BotKey) ([]pa.Routine, []pa.RoutineRun, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	defs, runs, err := readRoutines(ctx, tx, key)
	if err != nil {
		return nil, nil, err
	}
	return defs, runs, tx.Commit()
}
func readRoutines(ctx context.Context, tx *sql.Tx, key pa.BotKey) ([]pa.Routine, []pa.RoutineRun, error) {
	defs := []pa.Routine{}
	runs := []pa.RoutineRun{}
	rows, err := tx.QueryContext(ctx, `SELECT data FROM project_assistant_routines WHERE machine=? AND bot=? ORDER BY id`, key.MachineID, key.BotID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var raw []byte
		var d pa.Routine
		if err = rows.Scan(&raw); err == nil {
			err = json.Unmarshal(raw, &d)
		}
		if err != nil {
			rows.Close()
			return nil, nil, err
		}
		defs = append(defs, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT data FROM project_assistant_routine_runs WHERE machine=? AND bot=? ORDER BY id`, key.MachineID, key.BotID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var raw []byte
		var r pa.RoutineRun
		if err = rows.Scan(&raw); err == nil {
			err = json.Unmarshal(raw, &r)
		}
		if err != nil {
			rows.Close()
			return nil, nil, err
		}
		runs = append(runs, r)
	}
	err = rows.Err()
	rows.Close()
	return defs, runs, err
}
func applyRoutines(ctx context.Context, tx *sql.Tx, a aggregate, m app.Mutation) error {
	owner := func(key pa.BotKey, p pa.ProjectScope, c string) bool {
		return key == a.Bot.Key && p == a.Bot.Project && c == a.Conversation.ID
	}
	if m.Routine != nil {
		d := *m.Routine
		if !owner(d.Key, d.Project, d.ConversationID) {
			return failure(pa.ScopeDenied, "routine_owner")
		}
		if d.Progress != "" && (d.ProgressRevision < a.Memory.ForgottenThrough || d.ProgressRevision > a.Memory.Revision) {
			return failure(pa.ContextStale, "routine_progress_revision")
		}
		if err := pa.ValidateRoutine(d); err != nil {
			return err
		}
		var raw []byte
		err := tx.QueryRowContext(ctx, `SELECT data FROM project_assistant_routines WHERE machine=? AND bot=? AND id=?`, d.Key.MachineID, d.Key.BotID, d.ID).Scan(&raw)
		if err == sql.ErrNoRows {
			if m.Command != (pa.Command{}) && (d.Version != 1 || d.Deleted) {
				return failure(pa.Invalid, "routine_create")
			}
			var n int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_routines WHERE deleted=0`).Scan(&n); err != nil {
				return err
			}
			if n >= pa.MaxRoutineDefinitions {
				return failure(pa.CapacityFull, "routine_definitions")
			}
			if d.FollowUp {
				if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_routines d WHERE machine=? AND bot=? AND deleted=0 AND json_extract(CAST(data AS TEXT),'$.follow_up')=1 AND NOT EXISTS(SELECT 1 FROM project_assistant_routine_runs r WHERE r.machine=d.machine AND r.bot=d.bot AND r.routine=d.id AND r.state<>'queued')`, d.Key.MachineID, d.Key.BotID).Scan(&n); err != nil {
					return err
				}
				if n >= pa.MaxRoutineFollowUps {
					return failure(pa.CapacityFull, "follow_up_pending")
				}
			}
		} else if err != nil {
			return err
		} else {
			var old pa.Routine
			if err = json.Unmarshal(raw, &old); err != nil {
				return err
			}
			if old.Deleted || old.Key != d.Key || old.Project != d.Project || old.ConversationID != d.ConversationID || d.Version != old.Version+1 {
				return failure(pa.StaleVersion, "routine")
			}
			if old.FollowUp != d.FollowUp || old.FollowUpChain != d.FollowUpChain || old.MemoryRevision != d.MemoryRevision {
				return failure(pa.ScopeDenied, "follow_up_origin")
			}
		}
		raw, err = json.Marshal(d)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_routines(machine,bot,id,deleted,data) VALUES(?,?,?,?,?) ON CONFLICT(machine,bot,id) DO UPDATE SET deleted=excluded.deleted,data=excluded.data`, d.Key.MachineID, d.Key.BotID, d.ID, d.Deleted, raw); err != nil {
			return err
		}
		if d.Deleted || !d.HasHook() {
			if _, err = tx.ExecContext(ctx, `DELETE FROM project_assistant_routine_hooks WHERE machine=? AND bot=? AND id=?`, d.Key.MachineID, d.Key.BotID, d.ID); err != nil {
				return err
			}
		} else {
			var entropy [pa.MaxRoutineWebhookSecretBytes]byte
			if _, err = rand.Read(entropy[:]); err != nil {
				return err
			}
			query := `INSERT INTO project_assistant_routine_hooks(machine,bot,id,secret) VALUES(?,?,?,?) ON CONFLICT(machine,bot,id) DO NOTHING`
			if m.RotateRoutineHook {
				query = `INSERT INTO project_assistant_routine_hooks(machine,bot,id,secret) VALUES(?,?,?,?) ON CONFLICT(machine,bot,id) DO UPDATE SET secret=excluded.secret`
			}
			if _, err = tx.ExecContext(ctx, query, d.Key.MachineID, d.Key.BotID, d.ID, hex.EncodeToString(entropy[:])); err != nil {
				return err
			}
		}
	}
	if m.RoutineRun != nil {
		r := *m.RoutineRun
		if r.RetryCount < 0 || r.RetryCount > pa.MaxRoutineRetryAttempts || r.NotStartedProved && r.State != pa.TurnQueued {
			return failure(pa.Invalid, "routine_retry")
		}
		if (r.Source != "schedule" && r.Source != "test" && r.Source != "webhook" && r.Source != "follow_up") || len(r.DeliveryID) > pa.MaxRoutineDeliveryIDBytes || r.RoutineID == "" || len(r.Instruction) > pa.MaxRoutineInstructionBytes || r.CreatedAt.IsZero() || r.TimeoutSeconds < 1 || r.TimeoutSeconds > pa.MaxRoutineTimeoutSeconds {
			return failure(pa.Invalid, "routine_run")
		}
		_, err := json.Marshal(r.Event)
		if err != nil {
			return failure(pa.Invalid, "routine_event")
		}
		switch r.State {
		case pa.TurnQueued, pa.TurnRunning:
			if r.Published || r.FinishedAt != nil {
				return failure(pa.Invalid, "routine_run_terminal")
			}
		case pa.TurnSucceeded, pa.TurnFailed, pa.TurnCancelled, pa.TurnUnknown:
			if r.FinishedAt == nil || r.State == pa.TurnUnknown && r.Published {
				return failure(pa.Invalid, "routine_run_terminal")
			}
		default:
			return failure(pa.Invalid, "routine_run_state")
		}
		if r.State == pa.TurnRunning && (r.TurnID == "" || r.BindingID == "" || r.Instruction == "") {
			return failure(pa.Invalid, "routine_run_started")
		}

		if r.ID == "" || !owner(r.Key, r.Project, r.ConversationID) {
			return failure(pa.ScopeDenied, "routine_run_owner")
		}
		var raw []byte
		if err := tx.QueryRowContext(ctx, `SELECT data FROM project_assistant_routines WHERE machine=? AND bot=? AND id=?`, r.Key.MachineID, r.Key.BotID, r.RoutineID).Scan(&raw); err != nil {
			return err
		}
		var d pa.Routine
		if err := json.Unmarshal(raw, &d); err != nil {
			return err
		}
		if (r.State == pa.TurnQueued || r.State == pa.TurnRunning && m.Turn != nil && m.Turn.StartedAt != nil) && (d.Deleted || !d.Enabled && r.Source != "test") {
			return failure(pa.Revoked, "routine_paused")
		}
		if (r.State == pa.TurnQueued || r.State == pa.TurnRunning) && (r.Source == "webhook" || d.FollowUp) && r.MemoryRevision < a.Memory.ForgottenThrough {
			return failure(pa.ContextStale, "routine_event_forgotten")
		}
		if (r.Source == "follow_up") != d.FollowUp {
			return failure(pa.ScopeDenied, "follow_up_run")
		}
		if r.State == pa.TurnQueued {
			var pending int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_routine_runs WHERE machine=? AND bot=? AND routine=? AND state='queued' AND id<>?`, r.Key.MachineID, r.Key.BotID, r.RoutineID, r.ID).Scan(&pending); err != nil {
				return err
			}
			if pending >= pa.MaxRoutinePendingPerDefinition {
				return failure(pa.CapacityFull, "routine_pending")
			}
		}
		if r.TurnID != "" {
			t, ok := a.Turns[r.TurnID]
			if !ok || t.RoutineRunID != r.ID || t.Fence.Key != r.Key {
				return failure(pa.ScopeDenied, "routine_turn")
			}
		}
		if r.BindingID != "" {
			b, ok := a.Bindings[r.BindingID]
			if !ok || !d.FollowUp && b.SessionSlot != "routine-"+r.ID || d.FollowUp && b.SessionSlot != "" {
				return failure(pa.ScopeDenied, "routine_slot")
			}
		}
		err = tx.QueryRowContext(ctx, `SELECT data FROM project_assistant_routine_runs WHERE machine=? AND bot=? AND id=?`, r.Key.MachineID, r.Key.BotID, r.ID).Scan(&raw)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			var old pa.RoutineRun
			if err = json.Unmarshal(raw, &old); err != nil {
				return err
			}
			if old.DeliveryID != r.DeliveryID || !reflect.DeepEqual(old.Event, r.Event) {
				return failure(pa.StaleVersion, "routine_delivery")
			}
			retryNotStarted := m.RoutineNotStarted && old.State == pa.TurnRunning && r.State == pa.TurnQueued && r.NotStartedProved && r.RetryCount == old.RetryCount+1 && r.NotBefore.After(old.CreatedAt) && m.Turn != nil && m.Turn.ID == old.TurnID && m.Turn.State == pa.TurnQueued
			if m.RoutineNotStarted && !retryNotStarted {
				return failure(pa.ScopeDenied, "routine_retry_proof")
			}
			if old.RetryCount != r.RetryCount && !retryNotStarted {
				return failure(pa.ScopeDenied, "routine_retry_count")
			}
			if old.Source != r.Source || old.MemoryRevision != r.MemoryRevision || !old.CreatedAt.Equal(r.CreatedAt) || old.State != pa.TurnQueued && (old.Instruction != r.Instruction || old.TimeoutSeconds != r.TimeoutSeconds) || old.RoutineID != r.RoutineID || old.Key != r.Key || old.Project != r.Project || old.ConversationID != r.ConversationID || old.TurnID != "" && old.TurnID != r.TurnID || old.BindingID != "" && old.BindingID != r.BindingID && !(old.State == pa.TurnQueued && old.NotStartedProved) || old.State != r.State && !pa.CanTransition(old.State, r.State) && !retryNotStarted {
				return failure(pa.StaleVersion, "routine_run")
			}
		}
		raw, err = json.Marshal(r)
		if err != nil {
			return err
		}
		finished := ""
		if r.FinishedAt != nil {
			finished = r.FinishedAt.UTC().Format(time.RFC3339Nano)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_routine_runs(machine,bot,id,routine,state,published,finished,data) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(machine,bot,id) DO UPDATE SET state=excluded.state,published=excluded.published,finished=excluded.finished,data=excluded.data`, r.Key.MachineID, r.Key.BotID, r.ID, r.RoutineID, r.State, r.Published, finished, raw); err != nil {
			return err
		}
		// Only safe, published terminal bookkeeping is reclaimed. Turns, bindings,
		// messages, receipts and unresolved outcomes retain their provenance.
		_, err = tx.ExecContext(ctx, `DELETE FROM project_assistant_routine_runs WHERE rowid IN (SELECT rowid FROM project_assistant_routine_runs WHERE published=1 AND state IN ('succeeded','failed','cancelled') ORDER BY finished DESC,id DESC LIMIT -1 OFFSET ?)`, pa.MaxRoutineFinishedRuns)
		if err != nil {
			return err
		}
	}
	return nil
}

// Forget cannot attribute a compacted note to individual sources, so conservatively
// clear every routine note in the same transaction as the forget receipt.
func clearRoutineProgress(ctx context.Context, tx *sql.Tx, a aggregate) error {
	definitions, _, err := readRoutines(ctx, tx, a.Bot.Key)
	if err != nil {
		return err
	}
	for _, d := range definitions {
		if d.Progress == "" && (!d.FollowUp || d.Deleted) {
			continue
		}
		d.Progress = ""
		if d.FollowUp {
			d.Enabled, d.Deleted = false, true
		}
		d.ProgressRevision = a.Memory.Revision
		d.Version++
		raw, err := json.Marshal(d)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE project_assistant_routines SET data=? WHERE machine=? AND bot=? AND id=?`, raw, d.Key.MachineID, d.Key.BotID, d.ID); err != nil {
			return err
		}
	}
	return nil
}

// RoutineHook resolves a bearer capability without exposing it in any export.
// Ambiguous identifiers are unusable, just like unknown ones.
func (s *Store) RoutineHook(ctx context.Context, id string) (pa.Routine, string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.data,h.secret FROM project_assistant_routines r JOIN project_assistant_routine_hooks h USING(machine,bot,id) WHERE r.id=? AND r.deleted=0`, id)
	if err != nil {
		return pa.Routine{}, "", err
	}
	defer rows.Close()
	var d pa.Routine
	var secret string
	count := 0
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw, &secret); err != nil {
			return pa.Routine{}, "", err
		}
		if err = json.Unmarshal(raw, &d); err != nil {
			return pa.Routine{}, "", err
		}
		count++
	}
	if err = rows.Err(); err != nil {
		return pa.Routine{}, "", err
	}
	if count != 1 {
		return pa.Routine{}, "", failure(pa.NotFound, "routine_hook")
	}
	return d, secret, nil
}
