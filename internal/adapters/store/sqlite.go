// Package store is the durable side of the domain: an event log, the
// projections derived from it, and the receipts that make a retry idempotent.
//
// SQLite rather than a directory of JSON files because the three writes that
// must happen together — the events, the projection they change, and the
// receipt that says the command was accepted — are one transaction or they are
// a race. The Swift app keeps ten JSON files and two SQLite ledgers with their
// own write paths, and a crash between two of them leaves a state nobody
// designed.
//
// The driver is pure Go so that CGO stays off and one CI machine can still
// produce every platform's binary.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/sainteye/clawdline-go/internal/domain/coordinator"
	"github.com/sainteye/clawdline-go/internal/domain/schedule"
	"github.com/sainteye/clawdline-go/internal/domain/task"
)

type Store struct{ db *sql.DB }

// Event is one persisted fact. Events are never edited and never deleted: a
// correction is another event, so that replay produces the same state a reader
// saw at the time.
type Event struct {
	Seq     int64           `json:"seq"`
	At      time.Time       `json:"at"`
	Kind    string          `json:"kind"`
	Subject string          `json:"subject"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Receipt is the durable answer to "did this command already happen".
type Receipt struct {
	CommandID string    `json:"command_id"`
	At        time.Time `json:"at"`
	Seq       int64     `json:"seq"`
	// Replayed is true when this command had already been accepted. A retry
	// gets the original answer rather than a second effect.
	Replayed bool `json:"replayed"`
}

const schema = `
CREATE TABLE IF NOT EXISTS events (
  seq     INTEGER PRIMARY KEY AUTOINCREMENT,
  at      INTEGER NOT NULL,
  kind    TEXT    NOT NULL,
  subject TEXT    NOT NULL,
  payload TEXT    NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS receipts (
  command_id TEXT    PRIMARY KEY,
  at         INTEGER NOT NULL,
  seq        INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS obligations (
  id         TEXT    PRIMARY KEY,
  kind       TEXT    NOT NULL,
  subject    TEXT    NOT NULL,
  mover_kind TEXT    NOT NULL,
  mover_id   TEXT    NOT NULL DEFAULT '',
  opened_at  INTEGER NOT NULL,
  evidence   TEXT    NOT NULL,
  note       TEXT    NOT NULL DEFAULT '',
  closed_at  INTEGER
);
CREATE INDEX IF NOT EXISTS obligations_open ON obligations(closed_at, opened_at);
CREATE TABLE IF NOT EXISTS tasks (
  id         TEXT    PRIMARY KEY,
  assistant  TEXT    NOT NULL,
  project    TEXT    NOT NULL,
  claims     TEXT    NOT NULL DEFAULT '[]',
  state      TEXT    NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS tasks_state ON tasks(state);
CREATE TABLE IF NOT EXISTS board (
  id       INTEGER PRIMARY KEY CHECK (id = 1),
  revision INTEGER NOT NULL
);
INSERT OR IGNORE INTO board (id, revision) VALUES (1, 0);
CREATE TABLE IF NOT EXISTS schedules (
  id        TEXT    PRIMARY KEY,
  name      TEXT    NOT NULL,
  spec      TEXT    NOT NULL,
  assistant TEXT    NOT NULL,
  dir       TEXT    NOT NULL,
  brief     TEXT    NOT NULL,
  claims    TEXT    NOT NULL DEFAULT '[]',
  enabled   INTEGER NOT NULL DEFAULT 1,
  last_run  INTEGER NOT NULL DEFAULT 0,
  last_task TEXT    NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS coordinator (
  id              INTEGER PRIMARY KEY CHECK (id = 1),
  record_id       TEXT    NOT NULL,
  label           TEXT    NOT NULL,
  session_id      TEXT    NOT NULL,
  conversation_id TEXT    NOT NULL,
  assistant       TEXT    NOT NULL,
  pid             INTEGER NOT NULL,
  registered_at   INTEGER NOT NULL,
  rebound_at      INTEGER NOT NULL DEFAULT 0,
  generation      INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS board_commands (
  request_id  TEXT    PRIMARY KEY,
  fingerprint TEXT    NOT NULL,
  revision    INTEGER NOT NULL,
  at          INTEGER NOT NULL
);
`

// Open prepares the store under the daemon's own state root.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "clawdline.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One writer. The whole point of this store is that the three writes below
	// cannot interleave with anybody else's.
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=FULL",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	// The directory is already 0700, but the files carry receipts and the
	// subjects of somebody's work, and defence in depth is two lines here.
	// SQLite creates them through the process umask, which is not ours to
	// assume.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Chmod(path+suffix, 0o600)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Change is one edit to a projection.
type Change struct {
	Open    *task.Obligation
	Close   string     // an obligation id
	Task    *task.Task // insert or replace a task row
	SetTask [2]string  // {id, state}
}

// Commit records events, applies the projection they imply, and writes the
// receipt — all in one transaction.
//
// A command already accepted writes nothing and returns its original receipt.
// That is what makes a retry safe, and it is why the caller's acknowledgement
// means the intent committed rather than that the work finished: the effects
// those events imply run afterwards, in reactors, and report their own results
// back as further commands.
func (s *Store) Commit(ctx context.Context, commandID string, events []Event, changes []Change) (Receipt, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Receipt{}, err
	}
	defer tx.Rollback()

	var at int64
	var seq int64
	err = tx.QueryRowContext(ctx,
		`SELECT at, seq FROM receipts WHERE command_id = ?`, commandID).Scan(&at, &seq)
	if err == nil {
		return Receipt{CommandID: commandID, At: time.Unix(at, 0), Seq: seq, Replayed: true}, nil
	}
	if err != sql.ErrNoRows {
		return Receipt{}, err
	}

	now := time.Now()
	last := int64(0)
	for _, e := range events {
		payload := string(e.Payload)
		res, err := tx.ExecContext(ctx,
			`INSERT INTO events (at, kind, subject, payload) VALUES (?, ?, ?, ?)`,
			now.Unix(), e.Kind, e.Subject, payload)
		if err != nil {
			return Receipt{}, err
		}
		if last, err = res.LastInsertId(); err != nil {
			return Receipt{}, err
		}
	}

	for _, c := range changes {
		switch {
		case c.Open != nil:
			o := c.Open
			if _, err := tx.ExecContext(ctx,
				`INSERT OR REPLACE INTO obligations
				 (id, kind, subject, mover_kind, mover_id, opened_at, evidence, note, closed_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
				o.ID, string(o.Kind), o.Subject, string(o.Mover.Kind), o.Mover.ID,
				o.OpenedAt.Unix(), string(o.Evidence), o.Note); err != nil {
				return Receipt{}, err
			}
		case c.Task != nil:
			claims, _ := json.Marshal(c.Task.Claims)
			if _, err := tx.ExecContext(ctx,
				`INSERT OR REPLACE INTO tasks
				 (id, assistant, project, claims, state, created_at)
				 VALUES (?, ?, ?, ?, ?, ?)`,
				c.Task.ID, string(c.Task.Assistant), c.Task.ProjectDir,
				string(claims), string(c.Task.State), c.Task.CreatedAt.Unix()); err != nil {
				return Receipt{}, err
			}
		case c.SetTask[0] != "":
			if _, err := tx.ExecContext(ctx,
				`UPDATE tasks SET state = ? WHERE id = ?`, c.SetTask[1], c.SetTask[0]); err != nil {
				return Receipt{}, err
			}
		case c.Close != "":
			if _, err := tx.ExecContext(ctx,
				`UPDATE obligations SET closed_at = ? WHERE id = ? AND closed_at IS NULL`,
				now.Unix(), c.Close); err != nil {
				return Receipt{}, err
			}
		}
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO receipts (command_id, at, seq) VALUES (?, ?, ?)`,
		commandID, now.Unix(), last); err != nil {
		return Receipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return Receipt{}, err
	}
	return Receipt{CommandID: commandID, At: now, Seq: last}, nil
}

// OpenObligations returns everything still owed, oldest first.
func (s *Store) OpenObligations(ctx context.Context) ([]task.Obligation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, kind, subject, mover_kind, mover_id, opened_at, evidence, note
		 FROM obligations WHERE closed_at IS NULL ORDER BY opened_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []task.Obligation{}
	for rows.Next() {
		var o task.Obligation
		var kind, moverKind, evidence string
		var openedAt int64
		if err := rows.Scan(&o.ID, &kind, &o.Subject, &moverKind, &o.Mover.ID,
			&openedAt, &evidence, &o.Note); err != nil {
			return nil, err
		}
		o.Kind = task.Kind(kind)
		o.Mover.Kind = task.MoverKind(moverKind)
		o.Evidence = task.Evidence(evidence)
		o.OpenedAt = time.Unix(openedAt, 0)
		out = append(out, o)
	}
	return out, rows.Err()
}

// LiveTasks returns the tasks that have not reached a terminal state.
//
// This is what a dispatch is arbitrated against. It reads the projection rather
// than replaying the log, because the question — "is anybody already writing
// there" — is about now, and a reader that has to replay history to answer it
// will be asked to skip the replay the first time it is slow.
func (s *Store) LiveTasks(ctx context.Context) ([]task.Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, assistant, project, claims, state, created_at FROM tasks
		 WHERE state IN ('queued','spawning','briefed') ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []task.Task{}
	for rows.Next() {
		var t task.Task
		var assistant, claims, state string
		var created int64
		if err := rows.Scan(&t.ID, &assistant, &t.ProjectDir, &claims, &state, &created); err != nil {
			return nil, err
		}
		t.Assistant = task.Assistant(assistant)
		t.State = task.State(state)
		t.CreatedAt = time.Unix(created, 0)
		_ = json.Unmarshal([]byte(claims), &t.Claims)
		out = append(out, t)
	}
	return out, rows.Err()
}

// BoardRevision is the number a writer must have read before it may write.
func (s *Store) BoardRevision(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT revision FROM board WHERE id = 1`).Scan(&n)
	return n, err
}

// BoardSeen returns the request ids already applied, with what each one asked
// for, so a retry can be told apart from a reused name.
func (s *Store) BoardSeen(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT request_id, fingerprint FROM board_commands`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, fp string
		if err := rows.Scan(&id, &fp); err != nil {
			return nil, err
		}
		out[id] = fp
	}
	return out, rows.Err()
}

// ApplyBoardCommand records a command and moves the revision, in one
// transaction with the events it produced.
func (s *Store) ApplyBoardCommand(ctx context.Context, requestID, fingerprint string, events []Event) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM board WHERE id = 1`).Scan(&revision); err != nil {
		return 0, err
	}
	revision++
	if _, err := tx.ExecContext(ctx, `UPDATE board SET revision = ? WHERE id = 1`, revision); err != nil {
		return 0, err
	}
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO board_commands (request_id, fingerprint, revision, at) VALUES (?, ?, ?, ?)`,
		requestID, fingerprint, revision, now); err != nil {
		return 0, err
	}
	for _, e := range events {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO events (at, kind, subject, payload) VALUES (?, ?, ?, ?)`,
			now, e.Kind, e.Subject, string(e.Payload)); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return revision, nil
}

// SaveSchedule stores or replaces one schedule.
func (s *Store) SaveSchedule(ctx context.Context, sc schedule.Schedule) error {
	claims, _ := json.Marshal(sc.Claims)
	enabled := 0
	if sc.Enabled {
		enabled = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO schedules
		 (id, name, spec, assistant, dir, brief, claims, enabled, last_run, last_task)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sc.ID, sc.Name, sc.When.String(), sc.Assistant, sc.Dir, sc.Brief,
		string(claims), enabled, unixOrZero(sc.LastRun), sc.LastTask)
	return err
}

// Schedules returns everything stored.
func (s *Store) Schedules(ctx context.Context) ([]schedule.Schedule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, spec, assistant, dir, brief, claims, enabled, last_run, last_task
		 FROM schedules ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []schedule.Schedule{}
	for rows.Next() {
		var sc schedule.Schedule
		var spec, claims string
		var enabled, last int64
		if err := rows.Scan(&sc.ID, &sc.Name, &spec, &sc.Assistant, &sc.Dir,
			&sc.Brief, &claims, &enabled, &last, &sc.LastTask); err != nil {
			return nil, err
		}
		when, err := schedule.ParseWhen(spec)
		if err != nil {
			// A schedule whose spelling we cannot read is disabled rather than
			// guessed at: firing it on a guessed clock is worse than not
			// firing it and saying so.
			sc.Enabled = false
			out = append(out, sc)
			continue
		}
		sc.When = when
		sc.Enabled = enabled == 1
		if last > 0 {
			sc.LastRun = time.Unix(last, 0)
		}
		_ = json.Unmarshal([]byte(claims), &sc.Claims)
		out = append(out, sc)
	}
	return out, rows.Err()
}

// unixOrZero keeps a never-run schedule at 0 rather than at the Unix value of
// year one, which reads as a timestamp 63 billion seconds in the past.
func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// MarkScheduleTask records which task a run produced.
func (s *Store) MarkScheduleTask(ctx context.Context, id, taskID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE schedules SET last_task = ? WHERE id = ?`, taskID, id)
	return err
}

// MarkScheduleRun records that a schedule fired.
func (s *Store) MarkScheduleRun(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE schedules SET last_run = ? WHERE id = ?`, at.Unix(), id)
	return err
}

// Coordinator returns the machine role's binding, if one exists.
//
// A missing record, a corrupt one and one written by a version we cannot read
// must not project the same tuple as "nobody has the role": the first is a fact
// and the others are ignorance. A read error is returned as an error, never as
// a nil record.
func (s *Store) Coordinator(ctx context.Context) (*coordinator.Record, error) {
	var r coordinator.Record
	var registered, rebound int64
	err := s.db.QueryRowContext(ctx,
		`SELECT record_id, label, session_id, conversation_id, assistant, pid,
		        registered_at, rebound_at, generation
		 FROM coordinator WHERE id = 1`).
		Scan(&r.ID, &r.Label, &r.SessionID, &r.ConversationID, &r.Assistant, &r.PID,
			&registered, &rebound, &r.Generation)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.RegisteredAt = time.Unix(registered, 0)
	if rebound > 0 {
		r.ReboundAt = time.Unix(rebound, 0)
	}
	return &r, nil
}

// SaveCoordinator writes the binding.
func (s *Store) SaveCoordinator(ctx context.Context, r coordinator.Record) error {
	rebound := int64(0)
	if !r.ReboundAt.IsZero() {
		rebound = r.ReboundAt.Unix()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO coordinator
		 (id, record_id, label, session_id, conversation_id, assistant, pid,
		  registered_at, rebound_at, generation)
		 VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Label, r.SessionID, r.ConversationID, r.Assistant, r.PID,
		r.RegisteredAt.Unix(), rebound, r.Generation)
	return err
}

// Counts is what `doctor` reports about the store.
func (s *Store) Counts(ctx context.Context) (events, receipts, open int, err error) {
	q := func(sql string) (int, error) {
		var n int
		e := s.db.QueryRowContext(ctx, sql).Scan(&n)
		return n, e
	}
	if events, err = q(`SELECT COUNT(*) FROM events`); err != nil {
		return
	}
	if receipts, err = q(`SELECT COUNT(*) FROM receipts`); err != nil {
		return
	}
	open, err = q(`SELECT COUNT(*) FROM obligations WHERE closed_at IS NULL`)
	return
}
