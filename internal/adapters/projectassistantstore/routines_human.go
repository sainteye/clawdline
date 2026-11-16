package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func (s *Store) RoutineDefinition(ctx context.Context, key pa.BotKey, id string) (pa.Routine, error) {
	var raw []byte
	var d pa.Routine
	err := s.db.QueryRowContext(ctx, `SELECT data FROM project_assistant_routines WHERE machine=? AND bot=? AND id=?`, key.MachineID, key.BotID, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return d, failure(pa.NotFound, "routine")
	}
	if err == nil {
		err = json.Unmarshal(raw, &d)
	}
	if err == nil && d.Key != key {
		err = failure(pa.ScopeDenied, "routine_owner")
	}
	return d, err
}

func (s *Store) QueuedRoutineRuns(ctx context.Context, key pa.BotKey, id string) ([]pa.RoutineRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT data FROM project_assistant_routine_runs WHERE machine=? AND bot=? AND routine=? AND state='queued' ORDER BY id LIMIT ?`, key.MachineID, key.BotID, id, pa.MaxRoutinePendingPerDefinition)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []pa.RoutineRun{}
	for rows.Next() {
		var raw []byte
		var run pa.RoutineRun
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &run); err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// ReadRoutines reuses the source-incarnation signed cursor and existing page
// and byte budgets. SQL reads only scoped rows and the selected run's result.
func (s *ViewStore) ReadRoutines(ctx context.Context, q app.RoutineQuery) (app.RoutinePage, error) {
	out := app.RoutinePage{Definitions: []pa.Routine{}, Runs: []app.RoutineRunResult{}}
	if err := pa.CheckPage(q.PageSize, false); err != nil {
		return out, err
	}
	if len(q.Cursor) > pa.MaxReadViewCursorBytes || q.AuthorizationBinding == "" {
		return out, failure(pa.Invalid, "routine_cursor")
	}
	vq := app.ReadViewQuery{Request: pa.ReadViewRequest{Key: q.Key, ConversationID: q.ConversationID, Mode: pa.ReadViewHistory, SourceEpoch: s.sourceEpoch, Cursor: q.Cursor}, AuthorizationBinding: q.AuthorizationBinding + "/routines/" + q.RoutineID, ByteLimit: cloudops.ReadPlaintextBytesLimit, CursorByteLimit: pa.MaxReadViewCursorBytes}
	tx, err := s.borrowedDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	used := 0
	head, _, err := viewHead(ctx, tx, vq, &used)
	if err != nil {
		return out, err
	}
	if head.Project != q.Project {
		return out, failure(pa.ScopeDenied, "routine_project")
	}
	out.Fence = head.CurrentFence
	var pos viewPosition
	if q.Cursor != "" {
		pos, err = s.position(vq, head)
		if err != nil {
			return out, err
		}
	}
	var rows *sql.Rows
	if q.RoutineID == "" {
		rows, err = tx.QueryContext(ctx, `SELECT rowid,CASE WHEN length(data)<=? THEN data END FROM project_assistant_routines WHERE machine=? AND bot=? AND deleted=0 AND (?=0 OR rowid<?) ORDER BY rowid DESC LIMIT ?`, vq.ByteLimit, q.Key.MachineID, q.Key.BotID, pos.Anchor, pos.Anchor, q.PageSize+1)
	} else {
		var owner []byte
		var d pa.Routine
		err = tx.QueryRowContext(ctx, `SELECT data FROM project_assistant_routines WHERE machine=? AND bot=? AND id=?`, q.Key.MachineID, q.Key.BotID, q.RoutineID).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) {
			return out, failure(pa.NotFound, "routine")
		}
		if err != nil {
			return out, err
		}
		if json.Unmarshal(owner, &d) != nil || d.Project != q.Project || d.ConversationID != q.ConversationID {
			return out, failure(pa.ScopeDenied, "routine_owner")
		}
		rows, err = tx.QueryContext(ctx, `SELECT rowid,CASE WHEN length(data)<=? THEN data END FROM project_assistant_routine_runs WHERE machine=? AND bot=? AND routine=? AND (?=0 OR rowid<?) ORDER BY rowid DESC LIMIT ?`, vq.ByteLimit, q.Key.MachineID, q.Key.BotID, q.RoutineID, pos.Anchor, pos.Anchor, q.PageSize+1)
	}
	if err != nil {
		return out, err
	}
	type candidate struct {
		n   int64
		raw []byte
	}
	candidates := []candidate{}
	more := false
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.n, &c.raw); err != nil {
			rows.Close()
			return out, err
		}
		if len(c.raw) == 0 || used+len(c.raw) > vq.ByteLimit {
			if len(candidates) == 0 {
				rows.Close()
				return out, failure(pa.CapacityFull, "routine_page_bytes")
			}
			more = true
			break
		}
		used += len(c.raw)
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for i, c := range candidates {
		if i == q.PageSize {
			break
		}
		id := ""
		if q.RoutineID == "" {
			var d pa.Routine
			if err = json.Unmarshal(c.raw, &d); err != nil {
				return out, err
			}
			if d.Key != q.Key || d.Project != q.Project || d.ConversationID != q.ConversationID {
				return out, failure(pa.ScopeDenied, "routine_owner")
			}
			if err = pa.ValidateRoutine(d); err != nil {
				return out, err
			}
			if d.Deleted {
				return out, failure(pa.Invalid, "routine_definition")
			}
			out.Definitions = append(out.Definitions, d)
			id = d.ID
		} else {
			var result app.RoutineRunResult
			if err = json.Unmarshal(c.raw, &result.Run); err != nil {
				return out, err
			}
			run := result.Run
			if run.Key != q.Key || run.Project != q.Project || run.ConversationID != q.ConversationID || run.RoutineID != q.RoutineID {
				return out, failure(pa.ScopeDenied, "routine_run_owner")
			}
			if run.TurnID != "" {
				var text, failed sql.NullString
				err = tx.QueryRowContext(ctx, viewAggregateSQL+`SELECT
 (SELECT json_extract(m.value,'$.text') FROM json_each(a.j,'$.Messages') m WHERE json_extract(m.value,'$.turn_id')=? AND json_extract(m.value,'$.kind')='assistant' AND `+viewFenceSQL("m.value", false)+` ORDER BY json_extract(m.value,'$.seq') DESC LIMIT 1),
 (SELECT json_extract(t.value,'$.failure') FROM json_each(a.j,'$.Turns') t WHERE t.key=? AND `+viewFenceSQL("t.value", false)+`) FROM a`, q.Key.MachineID, q.Key.BotID, run.TurnID, run.TurnID).Scan(&text, &failed)
				if err != nil {
					return out, err
				}
				used += len(text.String) + len(failed.String)
				if used > vq.ByteLimit {
					return out, failure(pa.CapacityFull, "routine_page_bytes")
				}
				result.Result = text.String
				if failed.Valid && failed.String != "null" {
					if err = json.Unmarshal([]byte(failed.String), &result.Failure); err != nil {
						return out, err
					}
				}
			}
			out.Runs = append(out.Runs, result)
			id = run.ID
		}
		token, e := s.cursor(vq, head, viewPosition{Collection: "history", Anchor: c.n, ID: id})
		if e != nil {
			return out, e
		}
		out.Cursors = append(out.Cursors, token)
	}
	if more || len(candidates) > q.PageSize {
		out.NextCursor = out.Cursors[len(out.Cursors)-1]
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}
