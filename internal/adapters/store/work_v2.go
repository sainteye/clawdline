package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
)

const workV2Schema = `
CREATE TABLE IF NOT EXISTS work_v2_items (
  id                TEXT PRIMARY KEY,
  project_id        TEXT NOT NULL,
  project_path      TEXT NOT NULL,
  kind              TEXT NOT NULL CHECK (kind IN ('feature','issue','epic','refactor','plan')),
  title             TEXT NOT NULL,
  description       TEXT NOT NULL,
  phase             TEXT NOT NULL CHECK (phase IN ('created','assigning','assigned','implementing','verifying','merging','deploying','done','cancelled')),
  condition         TEXT NOT NULL DEFAULT '' CHECK (condition IN ('','blocked','waiting_user','owner_required','owner_offline','evidence_unknown','assignment_failed','assigned_unnotified')),
  user_action       TEXT NOT NULL DEFAULT '',
  deployment_policy TEXT NOT NULL CHECK (deployment_policy IN ('required','not_required','agent_decides')),
  owner_session     TEXT NOT NULL DEFAULT '',
  created_by        TEXT NOT NULL,
  created_at        INTEGER NOT NULL,
  updated_at        INTEGER NOT NULL,
  closed_at         INTEGER,
  cycle             INTEGER NOT NULL DEFAULT 1,
  version           INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS work_v2_items_project ON work_v2_items(project_id, updated_at DESC, id);
CREATE INDEX IF NOT EXISTS work_v2_items_owner ON work_v2_items(owner_session, phase, updated_at DESC)
  WHERE owner_session <> '';
CREATE TABLE IF NOT EXISTS work_v2_assignments (
  id                 TEXT PRIMARY KEY,
  work_id            TEXT NOT NULL REFERENCES work_v2_items(id) ON DELETE CASCADE,
  mode               TEXT NOT NULL CHECK (mode IN ('existing_session','new_session')),
  session_id         TEXT NOT NULL DEFAULT '',
  terminal_id        TEXT NOT NULL DEFAULT '',
  assistant          TEXT NOT NULL DEFAULT '',
  model              TEXT NOT NULL DEFAULT '',
  state              TEXT NOT NULL CHECK (state IN ('assigning','active','released','failed')),
  human_actor        TEXT NOT NULL,
  root_assignment_id TEXT NOT NULL DEFAULT '',
  failure            TEXT NOT NULL DEFAULT '',
  created_at         INTEGER NOT NULL,
  updated_at         INTEGER NOT NULL,
  released_at        INTEGER
);
CREATE UNIQUE INDEX IF NOT EXISTS work_v2_one_active_assignment ON work_v2_assignments(work_id)
  WHERE state = 'active';
CREATE INDEX IF NOT EXISTS work_v2_assignment_session ON work_v2_assignments(session_id, state, updated_at DESC);
CREATE TABLE IF NOT EXISTS work_v2_documents (
  id         TEXT PRIMARY KEY,
  work_id    TEXT NOT NULL REFERENCES work_v2_items(id) ON DELETE CASCADE,
  role       TEXT NOT NULL CHECK (role IN ('spec','design','test','deploy','other')),
  title      TEXT NOT NULL,
  body       TEXT NOT NULL DEFAULT '',
  reference  TEXT NOT NULL DEFAULT '',
  position   INTEGER NOT NULL,
  version    INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS work_v2_documents_item ON work_v2_documents(work_id, position, id);
CREATE TABLE IF NOT EXISTS work_v2_images (
  id         TEXT PRIMARY KEY,
  work_id    TEXT NOT NULL REFERENCES work_v2_items(id) ON DELETE CASCADE,
  title      TEXT NOT NULL,
  media_type TEXT NOT NULL CHECK (media_type = 'image/png'),
  data       BLOB NOT NULL,
  byte_count INTEGER NOT NULL,
  width      INTEGER NOT NULL,
  height     INTEGER NOT NULL,
  position   INTEGER NOT NULL,
  created_by TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS work_v2_images_item ON work_v2_images(work_id, position, id);
CREATE TABLE IF NOT EXISTS work_v2_steps (
  id           TEXT PRIMARY KEY,
  work_id      TEXT NOT NULL REFERENCES work_v2_items(id) ON DELETE CASCADE,
  title        TEXT NOT NULL,
  done         INTEGER NOT NULL DEFAULT 0 CHECK (done IN (0,1)),
  position     INTEGER NOT NULL,
  created_by   TEXT NOT NULL,
  completed_by TEXT NOT NULL DEFAULT '',
  created_at   INTEGER NOT NULL,
  completed_at INTEGER,
  version      INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS work_v2_steps_item ON work_v2_steps(work_id, position, id);
CREATE TABLE IF NOT EXISTS work_v2_events (
  seq              INTEGER PRIMARY KEY AUTOINCREMENT,
  work_id          TEXT NOT NULL REFERENCES work_v2_items(id) ON DELETE CASCADE,
  kind             TEXT NOT NULL,
  actor            TEXT NOT NULL,
  previous_version INTEGER NOT NULL,
  next_version     INTEGER NOT NULL,
  payload          TEXT NOT NULL DEFAULT '{}',
  at               INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS work_v2_events_item ON work_v2_events(work_id, seq);
CREATE TRIGGER IF NOT EXISTS work_v2_events_no_update BEFORE UPDATE ON work_v2_events
  BEGIN SELECT RAISE(ABORT, 'work_v2_events_append_only'); END;
CREATE TRIGGER IF NOT EXISTS work_v2_events_no_delete BEFORE DELETE ON work_v2_events
  BEGIN SELECT RAISE(ABORT, 'work_v2_events_append_only'); END;
CREATE TABLE IF NOT EXISTS session_direct_todos (
  id           TEXT PRIMARY KEY,
  session_id   TEXT NOT NULL,
  text         TEXT NOT NULL,
  created_by   TEXT NOT NULL,
  created_at   INTEGER NOT NULL,
  sent_at      INTEGER,
  read_at      INTEGER,
  completed_at INTEGER,
  completed_by TEXT NOT NULL DEFAULT '',
  version      INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS session_direct_todos_owner ON session_direct_todos(session_id, completed_at, created_at, id);
CREATE TABLE IF NOT EXISTS session_direct_todo_images (
  id         TEXT PRIMARY KEY,
  todo_id    TEXT NOT NULL REFERENCES session_direct_todos(id) ON DELETE CASCADE,
  title      TEXT NOT NULL,
  media_type TEXT NOT NULL CHECK (media_type = 'image/png'),
  data       BLOB NOT NULL,
  byte_count INTEGER NOT NULL,
  width      INTEGER NOT NULL,
  height     INTEGER NOT NULL,
  position   INTEGER NOT NULL,
  created_by TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS session_direct_todo_images_todo ON session_direct_todo_images(todo_id, position, id);
CREATE TABLE IF NOT EXISTS work_v2_proposals (
  id                   TEXT PRIMARY KEY,
  project_id           TEXT NOT NULL,
  project_path         TEXT NOT NULL,
  kind                 TEXT NOT NULL CHECK (kind IN ('feature','issue','epic','refactor','plan')),
  title                TEXT NOT NULL,
  description          TEXT NOT NULL,
  reason               TEXT NOT NULL,
  suggested_acceptance TEXT NOT NULL DEFAULT '',
  session_id           TEXT NOT NULL,
  source_work_id       TEXT NOT NULL DEFAULT '',
  source_todo_id       TEXT NOT NULL DEFAULT '',
  state                TEXT NOT NULL CHECK (state IN ('pending','accepted','rejected')),
  accepted_work_id     TEXT NOT NULL DEFAULT '',
  created_at           INTEGER NOT NULL,
  resolved_at          INTEGER,
  version              INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS work_v2_proposals_state ON work_v2_proposals(state, created_at, id);
`

const (
	WorkV2OpenLimit       = 2_000
	WorkV2PlanningLimit   = 1_000
	WorkV2AssignmentLimit = 4_000
	WorkV2DocumentLimit   = 32
	WorkV2ImageLimit      = 6
	WorkV2ImageItemLimit  = 15 << 20
	WorkV2ImageTotalLimit = 512 << 20
	WorkV2StepLimit       = 128
	DirectTodoV2Limit     = 500
	WorkV2ProposalLimit   = 500
)

var (
	ErrNoWorkV2                 = errors.New("no such v2 work item")
	ErrWorkV2Full               = errors.New("v2 work items full")
	ErrPlanningV2Full           = errors.New("v2 planning items full")
	ErrDirectTodoFull           = errors.New("direct session todos full")
	ErrDirectTodoImagesFull     = errors.New("direct session todo images full")
	ErrDirectTodoImageBytesFull = errors.New("direct session todo image bytes full")
	ErrWorkV2DocsFull           = errors.New("v2 work documents full")
	ErrWorkV2ImagesFull         = errors.New("v2 work images full")
	ErrWorkV2ImageBytesFull     = errors.New("v2 work image bytes full")
	ErrWorkV2StepsFull          = errors.New("v2 work steps full")
	ErrWorkV2ProposalsFull      = errors.New("v2 work proposals full")
)

func openWorkV2(db *sql.DB) error {
	if _, err := db.Exec(workV2Schema); err != nil {
		return err
	}
	has, err := hasColumn(db, "work_v2_items", "user_action")
	if err != nil {
		return err
	}
	if !has {
		_, err = db.Exec(`ALTER TABLE work_v2_items ADD COLUMN user_action TEXT NOT NULL DEFAULT ''`)
	}
	return err
}

type WorkV2Tx struct {
	ctx   context.Context
	tx    *sql.Tx
	s     *Store
	wrote int64
}

func (s *Store) WriteWorkV2(ctx context.Context, fn func(*WorkV2Tx) error) error {
	return s.write(ctx, func(tx *sql.Tx) (int64, error) {
		t := &WorkV2Tx{ctx: ctx, tx: tx, s: s}
		if err := fn(t); err != nil {
			return 0, err
		}
		return t.wrote, nil
	})
}

func (t *WorkV2Tx) CompleteReceipt(k ReceiptKey, a ReceiptAnswer) error {
	if err := t.s.completeReceipt(t.ctx, t.tx, k, a); err != nil {
		return err
	}
	t.wrote++
	return nil
}

// AddEffect records an external effect in the same transaction as the work
// change that owes it. The caller runs the returned id only after this write
// commits; a daemon that stops first leaves the durable row to recovery.
func (t *WorkV2Tx) AddEffect(e Effect) (int64, error) {
	id, err := t.s.insertEffect(t.ctx, t.tx, e)
	if err != nil {
		return 0, err
	}
	t.wrote++
	return id, nil
}

const workV2Columns = `id, project_id, project_path, kind, title, description, phase, condition, user_action,
  deployment_policy, owner_session, created_by, created_at, updated_at, closed_at, cycle, version`

const workV2ItemColumns = `i.id, i.project_id, i.project_path, i.kind, i.title, i.description, i.phase, i.condition, i.user_action,
  i.deployment_policy, i.owner_session, i.created_by, i.created_at, i.updated_at, i.closed_at, i.cycle, i.version`

func scanWorkV2(sc scanner) (work.ItemV2, error) {
	var i work.ItemV2
	var created, updated int64
	var closed sql.NullInt64
	err := sc.Scan(&i.ID, &i.ProjectID, &i.ProjectPath, &i.Kind, &i.Title, &i.Description, &i.Phase,
		&i.Condition, &i.UserAction, &i.DeploymentPolicy, &i.OwnerSession, &i.CreatedBy, &created, &updated, &closed,
		&i.Cycle, &i.Version)
	if err == sql.ErrNoRows {
		return work.ItemV2{}, ErrNoWorkV2
	}
	if err != nil {
		return work.ItemV2{}, err
	}
	i.CreatedAt, i.UpdatedAt = time.Unix(created, 0), time.Unix(updated, 0)
	if closed.Valid {
		i.ClosedAt = time.Unix(closed.Int64, 0)
	}
	return i, nil
}

func (t *WorkV2Tx) Item(id string) (work.ItemV2, error) {
	return scanWorkV2(t.tx.QueryRowContext(t.ctx, `SELECT `+workV2Columns+` FROM work_v2_items WHERE id = ?`, id))
}

func (t *WorkV2Tx) Tasks(id string) ([]BrokerRow, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+brokerColumns+` FROM broker_tasks
	  WHERE json_valid(record) AND json_extract(record, '$.work_id') = ? ORDER BY created_at,id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BrokerRow{}
	for rows.Next() {
		r, err := scanBroker(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) WorkV2Item(ctx context.Context, id string) (work.ItemV2, error) {
	if err := reading(); err != nil {
		return work.ItemV2{}, err
	}
	return scanWorkV2(s.db.QueryRowContext(ctx, `SELECT `+workV2Columns+` FROM work_v2_items WHERE id = ?`, id))
}

func (t *WorkV2Tx) count(where string, args ...any) (int64, error) {
	var n int64
	err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM work_v2_items WHERE `+where, args...).Scan(&n)
	return n, err
}

func (t *WorkV2Tx) CreateItem(i work.ItemV2, actor, payload string) error {
	where := `phase NOT IN ('done','cancelled') AND kind IN ('feature','issue')`
	limit, full := int64(WorkV2OpenLimit), ErrWorkV2Full
	if i.Planning() {
		where, limit, full = `kind IN ('epic','refactor','plan') AND phase NOT IN ('done','cancelled')`, WorkV2PlanningLimit, ErrPlanningV2Full
	}
	if n, err := t.count(where); err != nil {
		return err
	} else if n >= limit {
		return full
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_items
      (id, project_id, project_path, kind, title, description, phase, condition, user_action,
       deployment_policy, owner_session, created_by, created_at, updated_at, closed_at, cycle, version)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?)`,
		i.ID, i.ProjectID, i.ProjectPath, i.Kind, i.Title, i.Description, i.Phase, i.Condition,
		i.UserAction, i.DeploymentPolicy, i.OwnerSession, i.CreatedBy, i.CreatedAt.Unix(), i.UpdatedAt.Unix(), i.Cycle, i.Version)
	if err != nil {
		return err
	}
	t.wrote++
	return t.AppendEvent(work.EventV2{WorkID: i.ID, Kind: "item.created", Actor: actor,
		PreviousVersion: 0, NextVersion: i.Version, Payload: payload, At: i.CreatedAt})
}

func (t *WorkV2Tx) PutItem(prev, next work.ItemV2, kind, actor, payload string) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_items SET project_id=?, project_path=?, kind=?, title=?,
      description=?, phase=?, condition=?, user_action=?, deployment_policy=?, owner_session=?, updated_at=?, closed_at=?,
      cycle=?, version=version+1 WHERE id=? AND version=?`,
		next.ProjectID, next.ProjectPath, next.Kind, next.Title, next.Description, next.Phase, next.Condition,
		next.UserAction, next.DeploymentPolicy, next.OwnerSession, next.UpdatedAt.Unix(), zeroOrUnix(next.ClosedAt), next.Cycle,
		prev.ID, prev.Version)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrConflict
	}
	t.wrote++
	next.Version = prev.Version + 1
	return t.AppendEvent(work.EventV2{WorkID: prev.ID, Kind: kind, Actor: actor,
		PreviousVersion: prev.Version, NextVersion: next.Version, Payload: payload, At: next.UpdatedAt})
}

func (t *WorkV2Tx) AppendEvent(e work.EventV2) error {
	if strings.TrimSpace(e.Payload) == "" {
		e.Payload = "{}"
	}
	if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_events
      (work_id, kind, actor, previous_version, next_version, payload, at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.WorkID, e.Kind, e.Actor, e.PreviousVersion, e.NextVersion, e.Payload, e.At.Unix()); err != nil {
		return err
	}
	t.wrote++
	return nil
}

func queryWorkV2(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, stmt string, args ...any) ([]work.ItemV2, error) {
	rows, err := q.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.ItemV2{}
	for rows.Next() {
		i, err := scanWorkV2(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) WorkV2Items(ctx context.Context, project, owner string, includeTerminal bool, limit int) ([]work.ItemV2, bool, error) {
	if err := reading(); err != nil {
		return nil, false, err
	}
	if limit <= 0 {
		return nil, false, fmt.Errorf("work v2 limit is required")
	}
	stmt := `SELECT ` + workV2Columns + ` FROM work_v2_items WHERE 1=1`
	args := []any{}
	if project != "" {
		stmt += ` AND project_id = ?`
		args = append(args, project)
	}
	if owner != "" {
		stmt += ` AND owner_session = ?`
		args = append(args, owner)
	}
	if !includeTerminal {
		stmt += ` AND phase NOT IN ('done','cancelled')`
	}
	stmt += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, limit+1)
	items, err := queryWorkV2(ctx, s.db, stmt, args...)
	if len(items) > limit {
		return items[:limit], true, err
	}
	return items, false, err
}

// CompletedWorkV2ForSession keeps a Session's own completed responsibility
// visible after completion releases owner_session. The assignment released in
// the same transaction as closed_at identifies the completing Session; an
// older assignee must not be offered somebody else's completion to retract.
func (s *Store) CompletedWorkV2ForSession(ctx context.Context, session string, limit int) ([]work.ItemV2, bool, error) {
	if err := reading(); err != nil {
		return nil, false, err
	}
	if session == "" || limit <= 0 {
		return nil, false, fmt.Errorf("session and work v2 limit are required")
	}
	rows, err := queryWorkV2(ctx, s.db, `SELECT `+workV2ItemColumns+` FROM work_v2_items i
    WHERE i.phase='done' AND EXISTS (
      SELECT 1 FROM work_v2_assignments a
      WHERE a.work_id=i.id AND a.session_id=? AND a.state='released' AND a.released_at=i.closed_at
    ) ORDER BY i.closed_at DESC, i.id DESC LIMIT ?`, session, limit+1)
	if len(rows) > limit {
		return rows[:limit], true, err
	}
	return rows, false, err
}

func scanAssignmentV2(sc scanner) (work.AssignmentV2, error) {
	var a work.AssignmentV2
	var created, updated int64
	var released sql.NullInt64
	err := sc.Scan(&a.ID, &a.WorkID, &a.Mode, &a.SessionID, &a.TerminalID, &a.Assistant, &a.Model,
		&a.State, &a.HumanActor, &a.RootAssignment, &a.Failure, &created, &updated, &released)
	if err != nil {
		return a, err
	}
	a.CreatedAt, a.UpdatedAt = time.Unix(created, 0), time.Unix(updated, 0)
	if released.Valid {
		a.ReleasedAt = time.Unix(released.Int64, 0)
	}
	return a, nil
}

const assignmentV2Columns = `id, work_id, mode, session_id, terminal_id, assistant, model, state,
  human_actor, root_assignment_id, failure, created_at, updated_at, released_at`

func (t *WorkV2Tx) ActiveAssignment(workID string) (work.AssignmentV2, error) {
	a, err := scanAssignmentV2(t.tx.QueryRowContext(t.ctx, `SELECT `+assignmentV2Columns+`
    FROM work_v2_assignments WHERE work_id=? AND state='active'`, workID))
	if err == sql.ErrNoRows {
		return work.AssignmentV2{}, nil
	}
	return a, err
}

// LatestAssignment returns the last assignment fact written for one item.
// rowid is the insertion order inside this append-only ledger; timestamps have
// one-second precision and cannot distinguish two assignments made together.
func (t *WorkV2Tx) LatestAssignment(workID string) (work.AssignmentV2, error) {
	a, err := scanAssignmentV2(t.tx.QueryRowContext(t.ctx, `SELECT `+assignmentV2Columns+`
    FROM work_v2_assignments WHERE work_id=? ORDER BY rowid DESC LIMIT 1`, workID))
	if err == sql.ErrNoRows {
		return work.AssignmentV2{}, nil
	}
	return a, err
}

func (t *WorkV2Tx) Assignment(id string) (work.AssignmentV2, error) {
	a, err := scanAssignmentV2(t.tx.QueryRowContext(t.ctx, `SELECT `+assignmentV2Columns+`
    FROM work_v2_assignments WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return work.AssignmentV2{}, errors.New("no such v2 assignment")
	}
	return a, err
}

func (t *WorkV2Tx) CreateAssignment(a work.AssignmentV2) error {
	if a.State == "assigning" {
		var n int64
		if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM work_v2_assignments WHERE state='assigning'`).Scan(&n); err != nil {
			return err
		} else if n >= WorkV2AssignmentLimit {
			return errors.New("work v2 assignments full")
		}
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_assignments
      (id, work_id, mode, session_id, terminal_id, assistant, model, state, human_actor,
       root_assignment_id, failure, created_at, updated_at, released_at)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.WorkID, a.Mode, a.SessionID, a.TerminalID, a.Assistant, a.Model, a.State, a.HumanActor,
		a.RootAssignment, a.Failure, a.CreatedAt.Unix(), a.UpdatedAt.Unix(), zeroOrUnix(a.ReleasedAt))
	if err == nil {
		t.wrote++
	}
	return err
}

func (t *WorkV2Tx) UpdateAssignment(a work.AssignmentV2) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_assignments SET session_id=?, terminal_id=?, assistant=?,
      model=?, state=?, root_assignment_id=?, failure=?, updated_at=?, released_at=? WHERE id=?`,
		a.SessionID, a.TerminalID, a.Assistant, a.Model, a.State, a.RootAssignment, a.Failure,
		a.UpdatedAt.Unix(), zeroOrUnix(a.ReleasedAt), a.ID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return errors.New("no such v2 assignment")
	}
	t.wrote++
	return nil
}

func (s *Store) WorkV2Assignments(ctx context.Context, workID string) ([]work.AssignmentV2, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+assignmentV2Columns+`
    FROM work_v2_assignments WHERE work_id=? ORDER BY created_at, id`, workID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.AssignmentV2{}
	for rows.Next() {
		a, err := scanAssignmentV2(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) WorkV2Events(ctx context.Context, workID string, after int64, limit int) ([]work.EventV2, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT seq, work_id, kind, actor, previous_version, next_version, payload, at
    FROM work_v2_events WHERE work_id=? AND seq>? ORDER BY seq LIMIT ?`, workID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.EventV2{}
	for rows.Next() {
		var e work.EventV2
		var at int64
		if err := rows.Scan(&e.Seq, &e.WorkID, &e.Kind, &e.Actor, &e.PreviousVersion, &e.NextVersion, &e.Payload, &at); err != nil {
			return nil, err
		}
		e.At = time.Unix(at, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (t *WorkV2Tx) AddDocument(d work.DocumentV2) error {
	var n int64
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM work_v2_documents WHERE work_id=?`, d.WorkID).Scan(&n); err != nil {
		return err
	} else if n >= WorkV2DocumentLimit {
		return ErrWorkV2DocsFull
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_documents
    (id,work_id,role,title,body,reference,position,version,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.WorkID, d.Role, d.Title, d.Body, d.Reference, d.Position, d.Version, d.CreatedAt.Unix(), d.UpdatedAt.Unix())
	if err == nil {
		t.wrote++
	}
	return err
}

func (s *Store) WorkV2Documents(ctx context.Context, workID string) ([]work.DocumentV2, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,work_id,role,title,body,reference,position,version,created_at,updated_at
    FROM work_v2_documents WHERE work_id=? ORDER BY position,id`, workID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.DocumentV2{}
	for rows.Next() {
		var d work.DocumentV2
		var created, updated int64
		if err := rows.Scan(&d.ID, &d.WorkID, &d.Role, &d.Title, &d.Body, &d.Reference, &d.Position,
			&d.Version, &created, &updated); err != nil {
			return nil, err
		}
		d.CreatedAt, d.UpdatedAt = time.Unix(created, 0), time.Unix(updated, 0)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (t *WorkV2Tx) AddImage(i work.ImageV2, data []byte) error {
	var count, itemBytes, totalBytes int64
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*),COALESCE(SUM(byte_count),0)
    FROM work_v2_images WHERE work_id=?`, i.WorkID).Scan(&count, &itemBytes); err != nil {
		return err
	}
	if count >= WorkV2ImageLimit {
		return ErrWorkV2ImagesFull
	}
	if int64(len(data)) > WorkV2ImageItemLimit-itemBytes {
		return ErrWorkV2ImageBytesFull
	}
	if err := t.tx.QueryRowContext(t.ctx, `SELECT
    COALESCE((SELECT SUM(byte_count) FROM work_v2_images),0) +
    COALESCE((SELECT SUM(byte_count) FROM session_direct_todo_images),0)`).Scan(&totalBytes); err != nil {
		return err
	}
	if int64(len(data)) > WorkV2ImageTotalLimit-totalBytes {
		return ErrWorkV2ImageBytesFull
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_images
    (id,work_id,title,media_type,data,byte_count,width,height,position,created_by,created_at)
    VALUES (?,?,?,?,?,?,?,?,?,?,?)`, i.ID, i.WorkID, i.Title, i.MediaType, data, len(data), i.Width,
		i.Height, i.Position, i.CreatedBy, i.CreatedAt.Unix())
	if err == nil {
		t.wrote++
	}
	return err
}

func scanWorkV2Image(sc scanner) (work.ImageV2, error) {
	var i work.ImageV2
	var created int64
	err := sc.Scan(&i.ID, &i.WorkID, &i.Title, &i.MediaType, &i.ByteCount, &i.Width, &i.Height,
		&i.Position, &i.CreatedBy, &created)
	if err != nil {
		return i, err
	}
	i.CreatedAt = time.Unix(created, 0)
	return i, nil
}

const workV2ImageColumns = `id,work_id,title,media_type,byte_count,width,height,position,created_by,created_at`

func (t *WorkV2Tx) Image(id string) (work.ImageV2, error) {
	return scanWorkV2Image(t.tx.QueryRowContext(t.ctx, `SELECT `+workV2ImageColumns+` FROM work_v2_images WHERE id=?`, id))
}

func (t *WorkV2Tx) DeleteImage(id string) error {
	res, err := t.tx.ExecContext(t.ctx, `DELETE FROM work_v2_images WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return sql.ErrNoRows
	}
	t.wrote++
	return nil
}

func (s *Store) WorkV2Images(ctx context.Context, workID string) ([]work.ImageV2, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+workV2ImageColumns+`
    FROM work_v2_images WHERE work_id=? ORDER BY position,id`, workID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.ImageV2{}
	for rows.Next() {
		i, err := scanWorkV2Image(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// WorkV2ImageBytes returns a durable Board or Session-to-do reference image
// without exposing the database or either BLOB column to the HTTP layer.
func (s *Store) WorkV2ImageBytes(ctx context.Context, id string) ([]byte, bool, error) {
	if err := reading(); err != nil {
		return nil, false, err
	}
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT data FROM work_v2_images WHERE id=?
    UNION ALL SELECT data FROM session_direct_todo_images WHERE id=? LIMIT 1`, id, id).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	return data, err == nil, err
}

func (t *WorkV2Tx) AddStep(s work.StepV2) error {
	var n int64
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM work_v2_steps WHERE work_id=?`, s.WorkID).Scan(&n); err != nil {
		return err
	} else if n >= WorkV2StepLimit {
		return ErrWorkV2StepsFull
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_steps
    (id,work_id,title,done,position,created_by,completed_by,created_at,completed_at,version)
    VALUES (?,?,?,?,?,?,?,?,?,?)`, s.ID, s.WorkID, s.Title, s.Done, s.Position, s.CreatedBy, s.CompletedBy,
		s.CreatedAt.Unix(), zeroOrUnix(s.CompletedAt), s.Version)
	if err == nil {
		t.wrote++
	}
	return err
}

func (t *WorkV2Tx) Step(id string) (work.StepV2, error) {
	var st work.StepV2
	var done bool
	var created int64
	var completed sql.NullInt64
	err := t.tx.QueryRowContext(t.ctx, `SELECT id,work_id,title,done,position,created_by,completed_by,created_at,completed_at,version
    FROM work_v2_steps WHERE id=?`, id).Scan(&st.ID, &st.WorkID, &st.Title, &done, &st.Position,
		&st.CreatedBy, &st.CompletedBy, &created, &completed, &st.Version)
	if err != nil {
		return st, err
	}
	st.Done, st.CreatedAt = done, time.Unix(created, 0)
	if completed.Valid {
		st.CompletedAt = time.Unix(completed.Int64, 0)
	}
	return st, nil
}

func (t *WorkV2Tx) Steps(workID string) ([]work.StepV2, error) {
	return queryWorkV2Steps(t.ctx, t.tx, workID)
}

func (t *WorkV2Tx) PutStep(prev, next work.StepV2) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_steps SET title=?,done=?,position=?,completed_by=?,completed_at=?,version=version+1
    WHERE id=? AND version=?`, next.Title, next.Done, next.Position, next.CompletedBy, zeroOrUnix(next.CompletedAt), prev.ID, prev.Version)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrConflict
	}
	t.wrote++
	return nil
}

func (s *Store) WorkV2Steps(ctx context.Context, workID string) ([]work.StepV2, error) {
	return queryWorkV2Steps(ctx, s.db, workID)
}

func queryWorkV2Steps(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, workID string) ([]work.StepV2, error) {
	rows, err := q.QueryContext(ctx, `SELECT id,work_id,title,done,position,created_by,completed_by,created_at,completed_at,version
    FROM work_v2_steps WHERE work_id=? ORDER BY position,id`, workID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.StepV2{}
	for rows.Next() {
		var st work.StepV2
		var done bool
		var created int64
		var completed sql.NullInt64
		if err := rows.Scan(&st.ID, &st.WorkID, &st.Title, &done, &st.Position, &st.CreatedBy,
			&st.CompletedBy, &created, &completed, &st.Version); err != nil {
			return nil, err
		}
		st.Done, st.CreatedAt = done, time.Unix(created, 0)
		if completed.Valid {
			st.CompletedAt = time.Unix(completed.Int64, 0)
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func scanDirectTodoV2(sc scanner) (work.DirectTodoV2, error) {
	var td work.DirectTodoV2
	var created int64
	var sent, read, completed sql.NullInt64
	err := sc.Scan(&td.ID, &td.SessionID, &td.Text, &td.CreatedBy, &created, &sent, &read, &completed,
		&td.CompletedBy, &td.Version)
	if err == sql.ErrNoRows {
		return td, errors.New("no such direct todo")
	}
	if err != nil {
		return td, err
	}
	td.CreatedAt = time.Unix(created, 0)
	if sent.Valid {
		td.SentAt = time.Unix(sent.Int64, 0)
	}
	if read.Valid {
		td.ReadAt = time.Unix(read.Int64, 0)
	}
	if completed.Valid {
		td.CompletedAt = time.Unix(completed.Int64, 0)
	}
	return td, nil
}

const directTodoV2Columns = `id,session_id,text,created_by,created_at,sent_at,read_at,completed_at,completed_by,version`

func (t *WorkV2Tx) CreateDirectTodo(td work.DirectTodoV2) error {
	var n int64
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM session_direct_todos
    WHERE session_id=? AND completed_at IS NULL`, td.SessionID).Scan(&n); err != nil {
		return err
	} else if n >= DirectTodoV2Limit {
		return ErrDirectTodoFull
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO session_direct_todos
    (id,session_id,text,created_by,created_at,version) VALUES (?,?,?,?,?,1)`,
		td.ID, td.SessionID, td.Text, td.CreatedBy, td.CreatedAt.Unix())
	if err == nil {
		t.wrote++
	}
	return err
}

func (t *WorkV2Tx) AddDirectTodoImage(i work.DirectTodoImageV2, data []byte) error {
	var count, todoBytes, totalBytes int64
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*),COALESCE(SUM(byte_count),0)
    FROM session_direct_todo_images WHERE todo_id=?`, i.TodoID).Scan(&count, &todoBytes); err != nil {
		return err
	}
	if count >= WorkV2ImageLimit {
		return ErrDirectTodoImagesFull
	}
	if int64(len(data)) > WorkV2ImageItemLimit-todoBytes {
		return ErrDirectTodoImageBytesFull
	}
	if err := t.tx.QueryRowContext(t.ctx, `SELECT
    COALESCE((SELECT SUM(byte_count) FROM work_v2_images),0) +
    COALESCE((SELECT SUM(byte_count) FROM session_direct_todo_images),0)`).Scan(&totalBytes); err != nil {
		return err
	}
	if int64(len(data)) > WorkV2ImageTotalLimit-totalBytes {
		return ErrDirectTodoImageBytesFull
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO session_direct_todo_images
    (id,todo_id,title,media_type,data,byte_count,width,height,position,created_by,created_at)
    VALUES (?,?,?,?,?,?,?,?,?,?,?)`, i.ID, i.TodoID, i.Title, i.MediaType, data, len(data), i.Width,
		i.Height, i.Position, i.CreatedBy, i.CreatedAt.Unix())
	if err == nil {
		t.wrote++
	}
	return err
}

func scanDirectTodoImageV2(sc scanner) (work.DirectTodoImageV2, error) {
	var i work.DirectTodoImageV2
	var created int64
	err := sc.Scan(&i.ID, &i.TodoID, &i.Title, &i.MediaType, &i.ByteCount, &i.Width, &i.Height,
		&i.Position, &i.CreatedBy, &created)
	if err != nil {
		return i, err
	}
	i.CreatedAt = time.Unix(created, 0)
	return i, nil
}

const directTodoImageV2Columns = `id,todo_id,title,media_type,byte_count,width,height,position,created_by,created_at`

func (s *Store) DirectTodoV2Images(ctx context.Context, todoID string) ([]work.DirectTodoImageV2, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+directTodoImageV2Columns+`
    FROM session_direct_todo_images WHERE todo_id=? ORDER BY position,id`, todoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.DirectTodoImageV2{}
	for rows.Next() {
		i, err := scanDirectTodoImageV2(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

type DirectTodoImagePayload struct {
	Image work.DirectTodoImageV2
	Data  []byte
}

func (s *Store) DirectTodoV2ImagePayloads(ctx context.Context, todoID string) ([]DirectTodoImagePayload, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+directTodoImageV2Columns+`,data
    FROM session_direct_todo_images WHERE todo_id=? ORDER BY position,id`, todoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DirectTodoImagePayload{}
	for rows.Next() {
		var p DirectTodoImagePayload
		var created int64
		if err := rows.Scan(&p.Image.ID, &p.Image.TodoID, &p.Image.Title, &p.Image.MediaType, &p.Image.ByteCount,
			&p.Image.Width, &p.Image.Height, &p.Image.Position, &p.Image.CreatedBy, &created, &p.Data); err != nil {
			return nil, err
		}
		p.Image.CreatedAt = time.Unix(created, 0)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (t *WorkV2Tx) DirectTodo(id string) (work.DirectTodoV2, error) {
	return scanDirectTodoV2(t.tx.QueryRowContext(t.ctx, `SELECT `+directTodoV2Columns+`
    FROM session_direct_todos WHERE id=?`, id))
}

func (t *WorkV2Tx) PutDirectTodo(prev, next work.DirectTodoV2) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE session_direct_todos SET sent_at=?,read_at=?,completed_at=?,
    completed_by=?,version=version+1 WHERE id=? AND version=?`, zeroOrUnix(next.SentAt), zeroOrUnix(next.ReadAt),
		zeroOrUnix(next.CompletedAt), next.CompletedBy, prev.ID, prev.Version)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrConflict
	}
	t.wrote++
	return nil
}

func (t *WorkV2Tx) DeleteDirectTodo(id, session string) (bool, error) {
	res, err := t.tx.ExecContext(t.ctx, `DELETE FROM session_direct_todos WHERE id=? AND session_id=?`, id, session)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err == nil && n > 0 {
		t.wrote++
	}
	return n > 0, err
}

func (s *Store) DirectTodosV2(ctx context.Context, session string, includeCompleted bool, markRead bool, limit int) ([]work.DirectTodoV2, bool, error) {
	var out []work.DirectTodoV2
	var truncated bool
	err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		stmt := `SELECT ` + directTodoV2Columns + ` FROM session_direct_todos WHERE session_id=?`
		if !includeCompleted {
			stmt += ` AND completed_at IS NULL`
			stmt += ` ORDER BY created_at,id LIMIT ?`
		} else {
			// Open work remains the actionable prefix in creation order. Completed
			// rows follow newest first so the bounded person view keeps the most
			// useful confirmations without hiding an old open row.
			stmt += ` ORDER BY completed_at IS NOT NULL,
        CASE WHEN completed_at IS NULL THEN created_at END,
        CASE WHEN completed_at IS NOT NULL THEN completed_at END DESC,id LIMIT ?`
		}
		rows, err := tx.tx.QueryContext(ctx, stmt, session, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			td, err := scanDirectTodoV2(rows)
			if err != nil {
				return err
			}
			out = append(out, td)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(out) > limit {
			out, truncated = out[:limit], true
		}
		if !markRead {
			return nil
		}
		now := time.Now().Unix()
		res, err := tx.tx.ExecContext(ctx, `UPDATE session_direct_todos SET read_at=?,version=version+1
      WHERE session_id=? AND completed_at IS NULL AND read_at IS NULL`, now, session)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			tx.wrote += n
			for n := range out {
				if out[n].ReadAt.IsZero() {
					out[n].ReadAt = time.Unix(now, 0)
					out[n].Version++
				}
			}
		}
		return nil
	})
	return out, truncated, err
}

func (s *Store) WorkV2Counts(ctx context.Context) (map[string]int64, error) {
	out := map[string]int64{}
	for name, q := range map[string]string{
		"items": `SELECT COUNT(*) FROM work_v2_items`, "assignments": `SELECT COUNT(*) FROM work_v2_assignments`,
		"documents": `SELECT COUNT(*) FROM work_v2_documents`, "steps": `SELECT COUNT(*) FROM work_v2_steps`,
		"images": `SELECT COUNT(*) FROM work_v2_images`, "todo_images": `SELECT COUNT(*) FROM session_direct_todo_images`,
		"events": `SELECT COUNT(*) FROM work_v2_events`, "direct_todos": `SELECT COUNT(*) FROM session_direct_todos`,
		"proposals": `SELECT COUNT(*) FROM work_v2_proposals`,
	} {
		var n int64
		if err := s.db.QueryRowContext(ctx, q).Scan(&n); err != nil {
			return nil, err
		}
		out[name] = n
	}
	return out, nil
}

func scanProposalV2(sc scanner) (work.ProposalV2, error) {
	var p work.ProposalV2
	var created int64
	var resolved sql.NullInt64
	err := sc.Scan(&p.ID, &p.ProjectID, &p.ProjectPath, &p.Kind, &p.Title, &p.Description, &p.Reason,
		&p.SuggestedAcceptance, &p.SessionID, &p.SourceWorkID, &p.SourceTodoID, &p.State,
		&p.AcceptedWorkID, &created, &resolved, &p.Version)
	if err != nil {
		return p, err
	}
	p.CreatedAt = time.Unix(created, 0)
	if resolved.Valid {
		p.ResolvedAt = time.Unix(resolved.Int64, 0)
	}
	return p, nil
}

const proposalV2Columns = `id,project_id,project_path,kind,title,description,reason,suggested_acceptance,
session_id,source_work_id,source_todo_id,state,accepted_work_id,created_at,resolved_at,version`

func (t *WorkV2Tx) CreateProposal(p work.ProposalV2) error {
	var n int64
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM work_v2_proposals WHERE state='pending'`).Scan(&n); err != nil {
		return err
	}
	if n >= WorkV2ProposalLimit {
		return ErrWorkV2ProposalsFull
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_proposals
    (id,project_id,project_path,kind,title,description,reason,suggested_acceptance,session_id,source_work_id,source_todo_id,state,accepted_work_id,created_at,version)
    VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,1)`, p.ID, p.ProjectID, p.ProjectPath, p.Kind, p.Title, p.Description,
		p.Reason, p.SuggestedAcceptance, p.SessionID, p.SourceWorkID, p.SourceTodoID, p.State, p.AcceptedWorkID, p.CreatedAt.Unix())
	if err == nil {
		t.wrote++
	}
	return err
}

func (t *WorkV2Tx) Proposal(id string) (work.ProposalV2, error) {
	return scanProposalV2(t.tx.QueryRowContext(t.ctx, `SELECT `+proposalV2Columns+` FROM work_v2_proposals WHERE id=?`, id))
}

func (t *WorkV2Tx) ResolveProposal(prev work.ProposalV2, state, accepted string, at time.Time) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_proposals SET state=?,accepted_work_id=?,resolved_at=?,version=version+1 WHERE id=? AND version=?`,
		state, accepted, at.Unix(), prev.ID, prev.Version)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrConflict
	}
	t.wrote++
	return nil
}

func (s *Store) WorkV2Proposals(ctx context.Context, state string, limit int) ([]work.ProposalV2, bool, error) {
	query := `SELECT ` + proposalV2Columns + ` FROM work_v2_proposals`
	args := []any{}
	if state != "" {
		query += ` WHERE state=?`
		args = append(args, state)
	}
	query += ` ORDER BY created_at,id LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []work.ProposalV2{}
	for rows.Next() {
		p, err := scanProposalV2(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	truncated := len(out) > limit
	if truncated {
		out = out[:limit]
	}
	return out, truncated, nil
}

// WorkV2CapacityCounts returns the bounded cardinalities used by diagnostics.
// Per-owner bounds report the fullest owner rather than the total table.
func (s *Store) WorkV2CapacityCounts(ctx context.Context) (map[string]int64, error) {
	out := map[string]int64{}
	queries := map[string]string{
		"open":               `SELECT COUNT(*) FROM work_v2_items WHERE closed_at IS NULL`,
		"planning":           `SELECT COUNT(*) FROM work_v2_items WHERE kind IN ('epic','refactor','plan') AND closed_at IS NULL`,
		"assignments":        `SELECT COUNT(*) FROM work_v2_assignments`,
		"documents_per_item": `SELECT COALESCE(MAX(n),0) FROM (SELECT COUNT(*) n FROM work_v2_documents GROUP BY work_id)`,
		"images_per_item": `SELECT COALESCE(MAX(n),0) FROM (
      SELECT COUNT(*) n FROM work_v2_images GROUP BY work_id
      UNION ALL SELECT COUNT(*) n FROM session_direct_todo_images GROUP BY todo_id)`,
		"image_bytes_max": `SELECT COALESCE(MAX(byte_count),0) FROM (
      SELECT byte_count FROM work_v2_images UNION ALL SELECT byte_count FROM session_direct_todo_images)`,
		"image_bytes_per_item": `SELECT COALESCE(MAX(n),0) FROM (
      SELECT SUM(byte_count) n FROM work_v2_images GROUP BY work_id
      UNION ALL SELECT SUM(byte_count) n FROM session_direct_todo_images GROUP BY todo_id)`,
		"image_bytes_total": `SELECT COALESCE(SUM(byte_count),0) FROM (
      SELECT byte_count FROM work_v2_images UNION ALL SELECT byte_count FROM session_direct_todo_images)`,
		"steps_per_item": `SELECT COALESCE(MAX(n),0) FROM (SELECT COUNT(*) n FROM work_v2_steps GROUP BY work_id)`,
		"direct_todos":   `SELECT COALESCE(MAX(n),0) FROM (SELECT COUNT(*) n FROM session_direct_todos WHERE completed_at IS NULL GROUP BY session_id)`,
		"proposals":      `SELECT COUNT(*) FROM work_v2_proposals WHERE state='pending'`,
	}
	for name, query := range queries {
		var n int64
		if err := s.db.QueryRowContext(ctx, query).Scan(&n); err != nil {
			return nil, err
		}
		out[name] = n
	}
	return out, nil
}

// ResetWorkV1 deletes only the daemon's v1 work-system tables. It is never
// called by Open or migrate: the administrative HTTP/CLI boundary must first
// show and bind a dry-run confirmation to these counts.
func (s *Store) WorkV1Counts(ctx context.Context) (map[string]int64, error) {
	counts := map[string]int64{}
	for _, table := range []string{"proposals", "decisions", "digests", "todos", "moves", "board_items", "backlog", "work"} {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
			return nil, err
		}
		if exists == 0 {
			continue
		}
		var n int64
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
			return nil, err
		}
		counts[table] = n
	}
	return counts, nil
}

func (s *Store) ResetWorkV1(ctx context.Context) (map[string]int64, error) {
	counts := map[string]int64{}
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		// V1 protected its move history from ordinary deletion. This explicit
		// administrative transaction removes and restores that guard around the
		// exact reset; no other writer can enter between BEGIN IMMEDIATE and
		// commit.
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS moves_no_delete`); err != nil {
			return 0, err
		}
		tables := []string{"proposals", "decisions", "digests", "todos", "moves", "board_items", "backlog", "work"}
		var wrote int64
		for _, table := range tables {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
				return 0, err
			}
			if exists == 0 {
				continue
			}
			var n int64
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
				return 0, err
			}
			counts[table] = n
			res, err := tx.ExecContext(ctx, `DELETE FROM `+table)
			if err != nil {
				return 0, fmt.Errorf("clear %s: %w", table, err)
			}
			if changed, _ := res.RowsAffected(); changed > 0 {
				wrote += changed
			}
		}
		if _, err := tx.ExecContext(ctx, `CREATE TRIGGER IF NOT EXISTS moves_no_delete BEFORE DELETE ON moves
          BEGIN SELECT RAISE(ABORT, 'moves_append_only'); END`); err != nil {
			return 0, err
		}
		return wrote, nil
	})
	return counts, err
}
