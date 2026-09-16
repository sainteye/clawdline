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

// Change is one edit to the obligation projection.
type Change struct {
	Open  *task.Obligation
	Close string // an obligation id
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
