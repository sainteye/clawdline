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
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
	// stats is the account of this process's writes; see health.go.
	stats *writeStats
	// reads is what this handle has handed back to readers (G33).
	reads readStats
	// owner names this handle on the effects it runs (outbox.go): the process
	// and a nonce minted at Open, so a restart is a different owner even if
	// the operating system hands it the same pid.
	owner owner
}

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

// schema is what a fresh store is made with.
//
// Five tables an older store may still hold are not in it, and nothing reads
// or writes them any more (docs/design-decisions.md D07, D38): `tasks`,
// `obligations` and `receipts`, which were the older dispatch skeleton's —
// a second table of tasks the broker's claims could not see, a second copy of
// what is owed, a second spelling of a receipt — and `board` and
// `board_commands`, a board revision and its command receipts that no route
// ever wrote. A store that has them keeps them, untouched: removing a table
// from somebody's database is not this daemon's decision to make on the way
// past, and an unread table harms nobody.
const schema = `
CREATE TABLE IF NOT EXISTS events (
  seq     INTEGER PRIMARY KEY AUTOINCREMENT,
  at      INTEGER NOT NULL,
  kind    TEXT    NOT NULL,
  subject TEXT    NOT NULL,
  payload TEXT    NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS schedules (
  id        TEXT    PRIMARY KEY,
  name      TEXT    NOT NULL,
  spec      TEXT    NOT NULL,
  assistant TEXT    NOT NULL,
  dir       TEXT    NOT NULL,
  brief     TEXT    NOT NULL,
  claims    TEXT    NOT NULL DEFAULT '[]',
  enabled   INTEGER NOT NULL DEFAULT 1,
  last_run   INTEGER NOT NULL DEFAULT 0,
  last_task  TEXT    NOT NULL DEFAULT '',
  first_seen INTEGER NOT NULL DEFAULT 0
);
`

// Open prepares the store under the daemon's own state root.
// migrate adds columns that CREATE TABLE IF NOT EXISTS cannot.
//
// A store that already exists keeps its old shape forever under that
// statement, so a column added later is missing exactly where it matters — on
// the machines that have been running longest. Each step is stated as what it
// wants rather than as a version number, so running it twice is a no-op and a
// store can arrive here from any age.
func migrate(db *sql.DB) error {
	steps := []struct {
		table, column, ddl string
	}{
		{"schedules", "first_seen", "ALTER TABLE schedules ADD COLUMN first_seen INTEGER NOT NULL DEFAULT 0"},
		// The row's version, for the compare-and-set every rewrite of a task
		// now is (D08, G13). A row from before it is version 0.
		{"broker_tasks", "version", "ALTER TABLE broker_tasks ADD COLUMN version INTEGER NOT NULL DEFAULT 0"},
	}
	for _, step := range steps {
		has, err := hasColumn(db, step.table, step.column)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := db.Exec(step.ddl); err != nil {
			return fmt.Errorf("%s.%s: %w", step.table, step.column, err)
		}
	}
	return nil
}

func hasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, DBFile)
	// Every setting rides on the DSN, so it is applied to each connection the
	// pool opens and not only to the first: a pragma run once with db.Exec is
	// lost the day the pool replaces that connection. `_txlock=immediate`
	// makes every transaction BEGIN IMMEDIATE — the write right is taken when
	// the transaction starts, before its first read, so a read that decides
	// and the write it decides on cannot be split by another writer (D08).
	// `_busy_timeout` is how long a writer waits for another connection's
	// lock before ErrBusy (D25).
	dsn := path + "?_txlock=immediate&_busy_timeout=" + strconv.Itoa(busyTimeoutMS) +
		"&_journal_mode=WAL&_synchronous=FULL&_foreign_keys=1"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection per handle. Writers in this process queue for it rather
	// than meeting each other as SQLITE_BUSY; writers in another process —
	// a CLI, a second daemon — meet this one at SQLite's lock, and whoever
	// waits past busy_timeout is told so (ErrBusy) rather than let through.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return nil, classify(err)
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	if err := openBroker(db); err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		return nil, err
	}
	if err := openSchedules(db); err != nil {
		return nil, err
	}
	if err := openSnippets(db); err != nil {
		return nil, err
	}
	if err := openW2(db); err != nil {
		return nil, err
	}
	if err := openTodos(db); err != nil {
		return nil, err
	}
	if err := openCoordination(db); err != nil {
		return nil, err
	}
	if err := openWork(db); err != nil {
		return nil, err
	}
	if err := openWorkV2(db); err != nil {
		return nil, err
	}
	if err := openHandover(db); err != nil {
		return nil, err
	}
	if err := openParticipation(db); err != nil {
		return nil, err
	}
	if err := openCapacityPush(db); err != nil {
		return nil, err
	}
	if err := openLatestEvents(db); err != nil {
		return nil, err
	}
	// The directory is already 0700, but the files carry receipts and the
	// subjects of somebody's work, and defence in depth is two lines here.
	// SQLite creates them through the process umask, which is not ours to
	// assume.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Chmod(path+suffix, 0o600)
	}
	return &Store{db: db, stats: newWriteStats(), owner: newOwner()}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Append records one thing that happened.
//
// It is deliberately not Commit. Commit is for a command: it carries a
// receipt so a retry replays rather than repeats. This is for a fact that has
// already occurred somewhere this process cannot undo — bytes typed into a
// terminal, a session taken away — and a fact has no retry semantics. Writing
// it through Commit would offer an idempotency this side cannot honour.
func (s *Store) Append(ctx context.Context, e Event) error {
	return s.timedWrite(func() (int64, error) {
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO events (at, kind, subject, payload) VALUES (?, ?, ?, ?)`,
			time.Now().Unix(), e.Kind, e.Subject, string(e.Payload))
		if err != nil {
			return 0, err
		}
		return 1, nil
	})
}

// Counts is what `doctor` reports about the store: how many facts it holds,
// and how many tasks the broker has recorded.
func (s *Store) Counts(ctx context.Context) (events, tasks int, err error) {
	q := func(sql string) (int, error) {
		var n int
		e := s.db.QueryRowContext(ctx, sql).Scan(&n)
		return n, e
	}
	if events, err = q(`SELECT COUNT(*) FROM events`); err != nil {
		return
	}
	tasks, err = q(`SELECT COUNT(*) FROM broker_tasks`)
	return
}
