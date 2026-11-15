package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/json"
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
		}
		raw, err = json.Marshal(d)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_routines(machine,bot,id,deleted,data) VALUES(?,?,?,?,?) ON CONFLICT(machine,bot,id) DO UPDATE SET deleted=excluded.deleted,data=excluded.data`, d.Key.MachineID, d.Key.BotID, d.ID, d.Deleted, raw); err != nil {
			return err
		}
	}
	if m.RoutineRun != nil {
		r := *m.RoutineRun
		if (r.Source != "schedule" && r.Source != "test") || r.RoutineID == "" || len(r.Instruction) > pa.MaxRoutineInstructionBytes || r.CreatedAt.IsZero() || r.TimeoutSeconds < 1 || r.TimeoutSeconds > pa.MaxRoutineTimeoutSeconds {
			return failure(pa.Invalid, "routine_run")
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
			if !ok || b.SessionSlot != "routine-"+r.ID {
				return failure(pa.ScopeDenied, "routine_slot")
			}
		}
		err := tx.QueryRowContext(ctx, `SELECT data FROM project_assistant_routine_runs WHERE machine=? AND bot=? AND id=?`, r.Key.MachineID, r.Key.BotID, r.ID).Scan(&raw)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			var old pa.RoutineRun
			if err = json.Unmarshal(raw, &old); err != nil {
				return err
			}
			if old.Source != r.Source || !old.CreatedAt.Equal(r.CreatedAt) || old.State != pa.TurnQueued && (old.Instruction != r.Instruction || old.TimeoutSeconds != r.TimeoutSeconds) || old.RoutineID != r.RoutineID || old.Key != r.Key || old.Project != r.Project || old.ConversationID != r.ConversationID || old.TurnID != "" && old.TurnID != r.TurnID || old.BindingID != "" && old.BindingID != r.BindingID || old.State != r.State && !pa.CanTransition(old.State, r.State) {
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
