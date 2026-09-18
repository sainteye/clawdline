package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// The broker's own durable state.
//
// This is where the rewrite stops borrowing. The Swift app's orchestrator is
// this machine's broker today and keeps its record in `~/.config/clawdline`;
// this daemon reads that store and never writes it (plan.md §4). A task **this**
// daemon dispatches therefore has to exist entirely here, or the two apps would
// have two half-records of one piece of work and no way to say which is the
// one.
//
// One row per task, holding the wire record as JSON, with the few columns a
// query actually filters on lifted out beside it. The alternative — a column
// per protocol field — makes every protocol addition a migration on the
// machines that have been running longest, which is precisely where a missing
// column is least survivable.
//
// The secret is not in that JSON. Only its SHA-256 is stored, in its own
// column, so that a store somebody copies off this machine carries no
// credential and `SELECT record` can be printed in a bug report.

const brokerSchema = `
CREATE TABLE IF NOT EXISTS broker_tasks (
  id           TEXT    PRIMARY KEY,
  project      TEXT    NOT NULL,
  repository   TEXT    NOT NULL DEFAULT '',
  assistant    TEXT    NOT NULL,
  state        TEXT    NOT NULL,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL,
  secret_hash  TEXT    NOT NULL,
  record       TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS broker_tasks_project ON broker_tasks(repository, created_at);
CREATE INDEX IF NOT EXISTS broker_tasks_state ON broker_tasks(state);
CREATE TABLE IF NOT EXISTS broker_notes (
  id      INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT    NOT NULL,
  at      INTEGER NOT NULL,
  note    TEXT    NOT NULL,
  UNIQUE (task_id, note)
);
CREATE INDEX IF NOT EXISTS broker_notes_task ON broker_notes(task_id, id);
CREATE TABLE IF NOT EXISTS broker_notifications (
  id      INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT    NOT NULL,
  at      INTEGER NOT NULL,
  title   TEXT    NOT NULL,
  body    TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS broker_notifications_task ON broker_notifications(task_id, id);
`

// BrokerRow is one task as this store holds it.
type BrokerRow struct {
	ID         string
	Project    string
	Repository string
	Assistant  string
	State      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	SecretHash string
	Record     json.RawMessage
}

// ErrNoTask is "this store has never heard of that id". It is distinct from a
// read failure, which proves nothing about whether the task exists.
var ErrNoTask = errors.New("no such task")

// ErrTaskExists is a create that found a row already holding that id. It says
// nothing about whether that row is readable, and it changed nothing: a
// dispatch that could not read the row it collided with must not be able to
// write over it (docs/design-decisions.md D05 ②).
var ErrTaskExists = errors.New("a task with that id is already stored")

// openBroker creates the broker's tables. Called from Open, once.
func openBroker(db *sql.DB) error {
	if _, err := db.Exec(brokerSchema); err != nil {
		return err
	}
	if _, err := db.Exec(brokerNoticeSchema); err != nil {
		return err
	}
	return migrateBrokerNotices(db)
}

// SaveBrokerTask writes or replaces one task's record, with the events that
// explain the change, in one transaction.
//
// The events and the row move together for the same reason the rest of this
// store does: a projection that can be updated without its event is a
// projection that will be, and then replay produces a different machine from
// the one a reader saw.
func (s *Store) SaveBrokerTask(ctx context.Context, row BrokerRow, events []Event) error {
	return s.SaveBrokerTaskWithNotice(ctx, row, nil, events)
}

// SaveBrokerTaskWithNotice is SaveBrokerTask that also opens the task's
// completion envelope, when notice is not nil, in the same transaction.
//
// A task that settled and the notice that tells its root are one fact: stored
// apart, a crash between them leaves either a finished task nobody will be
// told about or a notice about a task that has not finished. The insert fails
// on an envelope that already exists rather than replacing it — a second
// notice id for one task is the root being told to acknowledge something that
// no longer acknowledges anything.
func (s *Store) SaveBrokerTaskWithNotice(ctx context.Context, row BrokerRow, notice *BrokerNotice, events []Event) error {
	return s.timedWrite(func() (int64, error) {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return 0, err
		}
		defer tx.Rollback()
		now := time.Now()
		if row.UpdatedAt.IsZero() {
			row.UpdatedAt = now
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO broker_tasks
			   (id, project, repository, assistant, state, created_at, updated_at, secret_hash, record)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET
			   project=excluded.project, repository=excluded.repository,
			   assistant=excluded.assistant, state=excluded.state,
			   updated_at=excluded.updated_at, record=excluded.record`,
			row.ID, row.Project, row.Repository, row.Assistant, row.State,
			row.CreatedAt.Unix(), row.UpdatedAt.Unix(), row.SecretHash, string(row.Record)); err != nil {
			return 0, err
		}
		changed := int64(1)
		if notice != nil {
			if err := insertNotice(ctx, tx, *notice); err != nil {
				return 0, err
			}
			changed++
		}
		if err := insertEvents(ctx, tx, events); err != nil {
			return 0, err
		}
		if err := tx.Commit(); err != nil {
			return 0, err
		}
		return changed + int64(len(events)), nil
	})
}

// CreateBrokerTask writes a task that must not exist yet, with its events, in
// one transaction, and answers ErrTaskExists without writing anything when the
// id is already taken.
//
// It is apart from SaveBrokerTask because the two questions are different. A
// save rewrites the record a caller has just read; a create is the first write
// of an id, and the upsert under SaveBrokerTask would let a dispatch that
// failed to read an existing row — a row it could not decode, a read that
// errored — replace that row with a new task under the same id.
func (s *Store) CreateBrokerTask(ctx context.Context, row BrokerRow, events []Event) error {
	// A collision is a refusal, not a failed write: it is carried out of the
	// timed write rather than through it, so the store's health does not count
	// a caller's resend as the store failing.
	taken := false
	err := s.timedWrite(func() (int64, error) {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return 0, err
		}
		defer tx.Rollback()
		if row.UpdatedAt.IsZero() {
			row.UpdatedAt = time.Now()
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO broker_tasks
			   (id, project, repository, assistant, state, created_at, updated_at, secret_hash, record)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO NOTHING`,
			row.ID, row.Project, row.Repository, row.Assistant, row.State,
			row.CreatedAt.Unix(), row.UpdatedAt.Unix(), row.SecretHash, string(row.Record))
		if err != nil {
			return 0, err
		}
		if n, err := res.RowsAffected(); err != nil {
			return 0, err
		} else if n == 0 {
			taken = true
			return 0, nil
		}
		if err := insertEvents(ctx, tx, events); err != nil {
			return 0, err
		}
		if err := tx.Commit(); err != nil {
			return 0, err
		}
		return 1 + int64(len(events)), nil
	})
	if err == nil && taken {
		return ErrTaskExists
	}
	return err
}

// insertEvents appends events inside a transaction somebody else owns.
func insertEvents(ctx context.Context, tx *sql.Tx, events []Event) error {
	now := time.Now().Unix()
	for _, e := range events {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO events (at, kind, subject, payload) VALUES (?, ?, ?, ?)`,
			now, e.Kind, e.Subject, string(e.Payload)); err != nil {
			return err
		}
	}
	return nil
}

// BrokerTask reads one task.
func (s *Store) BrokerTask(ctx context.Context, id string) (BrokerRow, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, project, repository, assistant, state, created_at, updated_at, secret_hash, record
		 FROM broker_tasks WHERE id = ?`, id)
	return scanBroker(row)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanBroker(sc scanner) (BrokerRow, error) {
	var row BrokerRow
	var created, updated int64
	var record string
	err := sc.Scan(&row.ID, &row.Project, &row.Repository, &row.Assistant, &row.State,
		&created, &updated, &row.SecretHash, &record)
	if err == sql.ErrNoRows {
		return BrokerRow{}, ErrNoTask
	}
	if err != nil {
		return BrokerRow{}, err
	}
	row.CreatedAt = time.Unix(created, 0)
	row.UpdatedAt = time.Unix(updated, 0)
	row.Record = json.RawMessage(record)
	return row, nil
}

// BrokerTasks reads every task, newest first, optionally narrowed to one
// repository.
func (s *Store) BrokerTasks(ctx context.Context, repository string) ([]BrokerRow, error) {
	query := `SELECT id, project, repository, assistant, state, created_at, updated_at, secret_hash, record
	          FROM broker_tasks`
	args := []any{}
	if repository != "" {
		query += ` WHERE repository = ?`
		args = append(args, repository)
	}
	query += ` ORDER BY created_at DESC, id DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BrokerRow{}
	for rows.Next() {
		row, err := scanBroker(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// AppendBrokerNote records one progress note.
//
// `INSERT OR IGNORE` against the unique pair is the protocol's own rule: the
// same sentence twice is ignored rather than refused, because a child retrying
// a note it is unsure landed should not be told it did something wrong.
// The answer says which of the two happened, and for a new note its sequence
// number — the note's identity on the event stream, where a reader that has
// seen number n has seen every note up to it.
func (s *Store) AppendBrokerNote(ctx context.Context, taskID, note string) (BrokerNote, bool, error) {
	var out BrokerNote
	added := false
	err := s.timedWrite(func() (int64, error) {
		at := time.Now()
		res, err := s.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO broker_notes (task_id, at, note) VALUES (?, ?, ?)`,
			taskID, at.Unix(), note)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		if err != nil || n == 0 {
			return 0, err
		}
		seq, err := res.LastInsertId()
		if err != nil {
			return 0, err
		}
		added = true
		out = BrokerNote{Seq: seq, TaskID: taskID, At: time.Unix(at.Unix(), 0), Note: note}
		return n, nil
	})
	return out, added, err
}

// HasBrokerNote answers whether this exact sentence is already recorded for
// the task, without writing anything. The beat asks it before offering a note
// it re-read from progress.json: re-reading an unchanged file is an
// observation, and an observation is not a reason to open a write.
func (s *Store) HasBrokerNote(ctx context.Context, taskID, note string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM broker_notes WHERE task_id = ? AND note = ?`, taskID, note).Scan(&n)
	return n > 0, err
}

// BrokerNote is one note as it is read back.
type BrokerNote struct {
	// Seq is the note's row id: increasing, never reused.
	Seq    int64
	TaskID string
	At     time.Time
	Note   string
}

// BrokerNotes returns the newest `limit` notes for a task, oldest first.
func (s *Store) BrokerNotes(ctx context.Context, taskID string, limit int) ([]BrokerNote, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, task_id, at, note FROM (
		   SELECT id, task_id, at, note FROM broker_notes WHERE task_id = ? ORDER BY id DESC LIMIT ?
		 ) ORDER BY id ASC`, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BrokerNote{}
	for rows.Next() {
		var n BrokerNote
		var at int64
		if err := rows.Scan(&n.Seq, &n.TaskID, &at, &n.Note); err != nil {
			return nil, err
		}
		n.At = time.Unix(at, 0)
		out = append(out, n)
	}
	return out, rows.Err()
}

// RecordNotification stores one push a task sent and answers how many it has
// now sent, and how many this machine has sent in the last hour.
//
// Both counts come back from the one insert because they are the two caps the
// protocol states, and a caller that has to ask for them separately will
// check one of them against a moment the other has already moved past.
func (s *Store) RecordNotification(ctx context.Context, taskID, title, body string) (perTask, perHour int, err error) {
	began := time.Now()
	defer func() { s.stats.record(time.Since(began), 1, err) }()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	now := time.Now()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO broker_notifications (task_id, at, title, body) VALUES (?, ?, ?, ?)`,
		taskID, now.Unix(), title, body); err != nil {
		return 0, 0, err
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM broker_notifications WHERE task_id = ?`, taskID).Scan(&perTask); err != nil {
		return 0, 0, err
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM broker_notifications WHERE at >= ?`,
		now.Add(-time.Hour).Unix()).Scan(&perHour); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return perTask, perHour, nil
}

// NotificationCounts reads the same two numbers without sending anything, for
// the check that happens before the push is attempted.
func (s *Store) NotificationCounts(ctx context.Context, taskID string) (perTask, perHour int, err error) {
	if err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM broker_notifications WHERE task_id = ?`, taskID).Scan(&perTask); err != nil {
		return 0, 0, err
	}
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM broker_notifications WHERE at >= ?`,
		time.Now().Add(-time.Hour).Unix()).Scan(&perHour)
	return perTask, perHour, err
}
