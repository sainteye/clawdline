package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
)

// The session to-do list (board-redesign §3.2, design-decisions T2).
//
// Its own table, beside the broker's and in the same file, because the two
// things it must never be are a second copy of a broker fact and a row on a
// person's board. A to-do is written in the transaction that records the fact
// it follows — through Tx, never on its own — so a landing and the to-do it
// closes are one fact (D02); the table has no column a person edits, and
// nothing that draws the board reads it.
//
// Its states and origins are the redesign's schema, checked by SQLite: a row
// in a state the rules do not have is refused by the file, not by a reader
// hoping for the best.

const todosSchema = `
CREATE TABLE IF NOT EXISTS todos (
  id              TEXT    PRIMARY KEY,
  work_id         TEXT,
  owner_session   TEXT    NOT NULL,
  owner_assistant TEXT    NOT NULL DEFAULT '',
  origin          TEXT    NOT NULL
                  CHECK (origin IN ('dispatch','result_remaining','deliver_remaining','obligation')),
  task_id         TEXT,
  project         TEXT    NOT NULL DEFAULT '',
  title           TEXT    NOT NULL DEFAULT '',
  state           TEXT    NOT NULL CHECK (state IN ('open','done','dropped','handed_off')),
  reason          TEXT    NOT NULL,
  handed_to       TEXT,
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL,
  handed_off_at   INTEGER,
  closed_at       INTEGER,
  escalated_at    INTEGER,
  version         INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS todos_origin_task ON todos(origin, task_id);
CREATE INDEX IF NOT EXISTS todos_owner ON todos(owner_session, state, created_at);
CREATE INDEX IF NOT EXISTS todos_state ON todos(state);
`

// ErrNoTodo is "no to-do has that id". It is distinct from a read that failed.
var ErrNoTodo = errors.New("no such to-do")

// TodoRow is a to-do as stored: the rules' own value, and the compare-and-set
// counter a write must still find.
type TodoRow struct {
	work.Todo
	Version int64
}

func openTodos(db *sql.DB) error {
	_, err := db.Exec(todosSchema)
	return err
}

const todoColumns = `id, COALESCE(work_id, ''), owner_session, owner_assistant, origin, COALESCE(task_id, ''),
  project, title, state, reason, COALESCE(handed_to, ''), created_at, updated_at,
  COALESCE(handed_off_at, 0), COALESCE(closed_at, 0), version`

func scanTodo(sc scanner) (TodoRow, error) {
	var r TodoRow
	var origin, state string
	var created, updated, handed, closed int64
	err := sc.Scan(&r.ID, &r.WorkID, &r.Owner, &r.OwnerAssistant, &origin, &r.Task, &r.Project, &r.Title,
		&state, &r.Reason, &r.HandedTo, &created, &updated, &handed, &closed, &r.Version)
	if err == sql.ErrNoRows {
		return TodoRow{}, ErrNoTodo
	}
	if err != nil {
		return TodoRow{}, err
	}
	r.Origin, r.State = work.TodoOrigin(origin), work.TodoState(state)
	r.CreatedAt, r.UpdatedAt = time.Unix(created, 0), time.Unix(updated, 0)
	r.HandedOffAt, r.ClosedAt = unixOrZero(handed), unixOrZero(closed)
	return r, nil
}

func unixOrZero(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(v, 0)
}

func zeroOrUnix(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Unix()
}

func emptyOrNull(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// Todo reads one to-do as the transaction sees it.
func (t *Tx) Todo(id string) (TodoRow, error) {
	return scanTodo(t.tx.QueryRowContext(t.ctx, `SELECT `+todoColumns+` FROM todos WHERE id = ?`, id))
}

// PutTodo writes a to-do inside the transaction, with the events that explain
// it. With prev nil it is the to-do's first write and fails if the id is
// taken; otherwise it is a compare-and-set against prev's version. Either way
// a writer holding a stale copy is told ErrConflict and nothing is written.
func (t *Tx) PutTodo(next work.Todo, prev *TodoRow, events ...Event) error {
	var (
		res sql.Result
		err error
	)
	if prev == nil {
		res, err = t.tx.ExecContext(t.ctx,
			`INSERT INTO todos (id, work_id, owner_session, owner_assistant, origin, task_id, project, title,
			   state, reason, handed_to, created_at, updated_at, handed_off_at, closed_at, version)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
			 ON CONFLICT DO NOTHING`,
			next.ID, emptyOrNull(next.WorkID), next.Owner, next.OwnerAssistant, string(next.Origin),
			emptyOrNull(next.Task), next.Project, next.Title, string(next.State), next.Reason,
			emptyOrNull(next.HandedTo), next.CreatedAt.Unix(), next.UpdatedAt.Unix(),
			zeroOrUnix(next.HandedOffAt), zeroOrUnix(next.ClosedAt))
	} else {
		res, err = t.tx.ExecContext(t.ctx,
			`UPDATE todos SET work_id = ?, owner_session = ?, owner_assistant = ?, project = ?, title = ?,
			   state = ?, reason = ?, handed_to = ?, updated_at = ?, handed_off_at = ?, closed_at = ?,
			   version = version + 1
			 WHERE id = ? AND version = ?`,
			emptyOrNull(next.WorkID), next.Owner, next.OwnerAssistant, next.Project, next.Title,
			string(next.State), next.Reason, emptyOrNull(next.HandedTo), next.UpdatedAt.Unix(),
			zeroOrUnix(next.HandedOffAt), zeroOrUnix(next.ClosedAt), next.ID, prev.Version)
	}
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrConflict
	}
	if err := insertEvents(t.ctx, t.tx, events); err != nil {
		return err
	}
	t.wrote += 1 + int64(len(events))
	return nil
}

// Todo reads one to-do.
func (s *Store) Todo(ctx context.Context, id string) (TodoRow, error) {
	if err := reading(); err != nil {
		return TodoRow{}, err
	}
	return scanTodo(s.db.QueryRowContext(ctx, `SELECT `+todoColumns+` FROM todos WHERE id = ?`, id))
}

// TodoQuery is one page of one owner's to-dos, newest first.
type TodoQuery struct {
	Owner string
	// States narrows the page; empty is every state.
	States []work.TodoState
	// AfterCreated and AfterID are the last row of the page before (a
	// position, not an offset): rows strictly after it in the order.
	AfterCreated int64
	AfterID      string
	Limit        int
}

// Todos reads one page of an owner's to-dos, newest first.
func (s *Store) Todos(ctx context.Context, q TodoQuery) ([]TodoRow, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	query := `SELECT ` + todoColumns + ` FROM todos WHERE owner_session = ?`
	args := []any{q.Owner}
	if len(q.States) > 0 {
		query += ` AND state IN (` + strings.TrimSuffix(strings.Repeat("?,", len(q.States)), ",") + `)`
		for _, st := range q.States {
			args = append(args, string(st))
		}
	}
	if q.AfterID != "" {
		query += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
		args = append(args, q.AfterCreated, q.AfterCreated, q.AfterID)
	}
	query += ` ORDER BY created_at DESC, id DESC`
	if q.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, q.Limit)
	}
	return s.queryTodos(ctx, query, args...)
}

// TodoCounts is how many to-dos an owner has in each state.
func (s *Store) TodoCounts(ctx context.Context, owner string) (map[work.TodoState]int, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT state, COUNT(*) FROM todos WHERE owner_session = ? GROUP BY state`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[work.TodoState]int{}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return nil, err
		}
		out[work.TodoState(state)] = n
	}
	return out, rows.Err()
}

// OutstandingTodos reads every to-do still owed, oldest first. Its cost is the
// number still owed, never the length of the history: done and dropped rows
// are not read.
func (s *Store) OutstandingTodos(ctx context.Context) ([]TodoRow, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	return s.queryTodos(ctx, `SELECT `+todoColumns+` FROM todos WHERE state IN ('open','handed_off')
		ORDER BY created_at ASC, id ASC`)
}

// TasksWithoutTodo is every broker task with a root that has no to-do of
// the given origin, oldest first. The broker asks once when it starts, to make
// the to-dos a fact was recorded without: a row stored before the table
// existed, or by a writer that did not keep them.
//
// A task with no root owes no session anything and is left out here, in the
// query, so a start never decodes the detached history to be told so; the
// test is the protocol's own spelling, `root.session_id` in task.json. A row
// whose record is not JSON is left out too: its facts cannot be read, and
// unknown facts move nothing.
func (s *Store) TasksWithoutTodo(ctx context.Context, origin work.TodoOrigin) ([]string, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.id FROM broker_tasks t
		 WHERE NOT EXISTS (SELECT 1 FROM todos d WHERE d.origin = ? AND d.task_id = t.id)
		   AND json_valid(t.record)
		   AND COALESCE(json_extract(t.record, '$.root.session_id'), '') <> ''
		 ORDER BY t.created_at ASC, t.id ASC`, string(origin))
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

func (s *Store) queryTodos(ctx context.Context, query string, args ...any) ([]TodoRow, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TodoRow{}
	for rows.Next() {
		r, err := scanTodo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
