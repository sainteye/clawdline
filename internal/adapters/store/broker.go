package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
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
	// Version is the row's compare-and-set counter: what a writer read, and
	// what its write must still find (D08). A new row is 0.
	Version int64
	// Texts is the task's long prose — its instructions, its summary — which
	// lives in its own table (D25, D26) so that nothing on the hot path reads
	// or decodes it. Nil on a row read without it (the beat's, the list's);
	// on a write, each field present is stored and an empty one removed.
	Texts map[string]string
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

// SaveBrokerTask writes one task's record, with the events that explain the
// change, in one transaction — **only if the row is still at row.Version**.
//
// It used to be an unconditional upsert, and that was the lost update this
// store exists to make impossible: the broker's own writers were held off by a
// mutex in one process, and anybody else — a CLI, a second daemon, a test
// harness on the same file — wrote over the row without being stopped or
// noticed (G13). Now a writer holding a stale copy is told ErrConflict and
// nothing is written. A row that does not exist yet is created at version 0.
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
	conflict := false
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		if row.UpdatedAt.IsZero() {
			row.UpdatedAt = time.Now()
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO broker_tasks
			   (id, project, repository, assistant, state, created_at, updated_at, secret_hash, record, version)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET
			   project=excluded.project, repository=excluded.repository,
			   assistant=excluded.assistant, state=excluded.state,
			   updated_at=excluded.updated_at, record=excluded.record,
			   version=broker_tasks.version + 1
			 WHERE broker_tasks.version = excluded.version`,
			row.ID, row.Project, row.Repository, row.Assistant, row.State,
			row.CreatedAt.Unix(), row.UpdatedAt.Unix(), row.SecretHash, string(row.Record), row.Version)
		if err != nil {
			return 0, err
		}
		if n, err := res.RowsAffected(); err != nil {
			return 0, err
		} else if n == 0 {
			conflict = true
			return 0, nil
		}
		changed := int64(1)
		if row.Texts != nil {
			n, err := writeTexts(ctx, tx, row.ID, row.Texts)
			if err != nil {
				return 0, err
			}
			changed += n
		}
		if notice != nil {
			if err := insertNotice(ctx, tx, *notice); err != nil {
				return 0, err
			}
			changed++
		}
		if err := insertEvents(ctx, tx, events); err != nil {
			return 0, err
		}
		return changed + int64(len(events)), nil
	})
	if err == nil && conflict {
		return ErrConflict
	}
	return err
}

// CreateBrokerTask writes a task that must not exist yet, with its events and
// the effects its creation owes (outbox.go), in one transaction, and answers
// ErrTaskExists without writing anything when the id is already taken. The
// effects come back with their ids, owned by this handle.
//
// It is apart from SaveBrokerTask because the two questions are different. A
// save rewrites the record a caller has just read; a create is the first write
// of an id, and must not let a dispatch that failed to read an existing row —
// a row it could not decode, a read that errored — replace that row with a new
// task under the same id.
func (s *Store) CreateBrokerTask(ctx context.Context, row BrokerRow, events []Event, effects ...Effect) ([]int64, error) {
	// A collision is a refusal, not a failed write: it is carried out of the
	// timed write rather than through it, so the store's health does not count
	// a caller's resend as the store failing.
	taken := false
	var ids []int64
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		if row.UpdatedAt.IsZero() {
			row.UpdatedAt = time.Now()
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO broker_tasks
			   (id, project, repository, assistant, state, created_at, updated_at, secret_hash, record, version)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
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
		changed := int64(1)
		if row.Texts != nil {
			n, err := writeTexts(ctx, tx, row.ID, row.Texts)
			if err != nil {
				return 0, err
			}
			changed += n
		}
		if err := insertEvents(ctx, tx, events); err != nil {
			return 0, err
		}
		for _, e := range effects {
			id, err := s.insertEffect(ctx, tx, e)
			if err != nil {
				return 0, err
			}
			ids = append(ids, id)
		}
		return changed + int64(len(events)+len(effects)), nil
	})
	if err == nil && taken {
		return nil, ErrTaskExists
	}
	return ids, err
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

const brokerColumns = `id, project, repository, assistant, state, created_at, updated_at, secret_hash, record, version`

// BrokerTask reads one task, long prose and all.
func (s *Store) BrokerTask(ctx context.Context, id string) (BrokerRow, error) {
	if err := reading(); err != nil {
		return BrokerRow{}, err
	}
	row, err := scanBroker(s.db.QueryRowContext(ctx,
		`SELECT `+brokerColumns+` FROM broker_tasks WHERE id = ?`, id))
	if err != nil {
		return BrokerRow{}, err
	}
	s.reads.records.Add(1)
	if row.Texts, err = s.readTexts(ctx, s.db, id); err != nil {
		return BrokerRow{}, err
	}
	return row, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanBroker(sc scanner) (BrokerRow, error) {
	var row BrokerRow
	var created, updated int64
	var record string
	err := sc.Scan(&row.ID, &row.Project, &row.Repository, &row.Assistant, &row.State,
		&created, &updated, &row.SecretHash, &record, &row.Version)
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
// repository, without its long prose. It is the whole table: nothing on a
// loop may call it (G33) — the beat reads BrokerTasksInState, and the lists
// read BrokerTaskHeads and fetch only the records they have not decoded.
func (s *Store) BrokerTasks(ctx context.Context, repository string) ([]BrokerRow, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	query := `SELECT ` + brokerColumns + ` FROM broker_tasks`
	args := []any{}
	if repository != "" {
		query += ` WHERE repository = ?`
		args = append(args, repository)
	}
	query += ` ORDER BY created_at DESC, id DESC`
	return s.queryBroker(ctx, query, args...)
}

// BrokerTasksInState reads the tasks whose state column is one of states,
// oldest first, without their long prose. It is the beat's read: its cost is
// the number of tasks in those states, not the length of the history (G33).
func (s *Store) BrokerTasksInState(ctx context.Context, states []string) ([]BrokerRow, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	if len(states) == 0 {
		return []BrokerRow{}, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(states)), ",")
	args := make([]any, len(states))
	for i, v := range states {
		args[i] = v
	}
	return s.queryBroker(ctx,
		`SELECT `+brokerColumns+` FROM broker_tasks WHERE state IN (`+marks+`) ORDER BY created_at ASC, id ASC`,
		args...)
}

func (s *Store) queryBroker(ctx context.Context, query string, args ...any) ([]BrokerRow, error) {
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
	s.reads.records.Add(int64(len(out)))
	return out, rows.Err()
}

// BrokerHead is a task row without its record: the columns lifted beside it,
// and the version that says whether a copy decoded earlier is still this row.
type BrokerHead struct {
	ID         string
	Project    string
	Repository string
	Assistant  string
	State      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Version    int64
}

// BrokerTaskHeads reads every row's head, newest first. No record is read and
// nothing is decoded: a reader that already holds a row at this version has
// nothing more to fetch.
func (s *Store) BrokerTaskHeads(ctx context.Context) ([]BrokerHead, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project, repository, assistant, state, created_at, updated_at, version
		 FROM broker_tasks ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BrokerHead{}
	for rows.Next() {
		var h BrokerHead
		var created, updated int64
		if err := rows.Scan(&h.ID, &h.Project, &h.Repository, &h.Assistant, &h.State,
			&created, &updated, &h.Version); err != nil {
			return nil, err
		}
		h.CreatedAt, h.UpdatedAt = time.Unix(created, 0), time.Unix(updated, 0)
		out = append(out, h)
	}
	return out, rows.Err()
}

// BrokerTaskRecords reads the rows named, without their long prose, keyed by
// id. An id with no row is absent from the answer.
func (s *Store) BrokerTaskRecords(ctx context.Context, ids []string) (map[string]BrokerRow, error) {
	out := map[string]BrokerRow{}
	for len(ids) > 0 {
		batch := ids
		if len(batch) > 256 {
			batch = batch[:256]
		}
		ids = ids[len(batch):]
		marks := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, len(batch))
		for i, v := range batch {
			args[i] = v
		}
		rows, err := s.queryBroker(ctx, `SELECT `+brokerColumns+` FROM broker_tasks WHERE id IN (`+marks+`)`, args...)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.ID] = r
		}
	}
	return out, nil
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
	if InsideWrite() {
		return out, false, ErrNestedWrite
	}
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
	if err := reading(); err != nil {
		return false, err
	}
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
	if InsideWrite() {
		return 0, 0, ErrNestedWrite
	}
	began := time.Now()
	defer func() { s.stats.record(time.Since(began), 1, err) }()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, classify(err)
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
