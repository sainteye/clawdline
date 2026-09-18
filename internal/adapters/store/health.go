package store

import (
	"context"
	"errors"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"modernc.org/sqlite"
)

// What the store says about itself.
//
// Every write the broker makes goes through timedWrite, which is the one place
// a write can be counted. That count is what makes "an observation is not a
// write" a measurement rather than a promise: five children being watched for
// an hour must leave `writes` where it was, and a reader can check that it did
// without trusting anybody's description of the code.
//
// The timings are the write transactions' own, begin to commit. With one
// connection (SetMaxOpenConns(1)) a slow transaction is the backpressure every
// other writer waits behind, so its p99 is the number worth watching.

// ringSize is how many recent write durations the p99 is taken over.
const ringSize = 256

type writeStats struct {
	mu        sync.Mutex
	writes    int64
	rows      int64
	failures  int64
	busy      int64
	lastErr   string
	lastErrAt time.Time
	lastAt    time.Time
	ring      [ringSize]time.Duration
	filled    int
	next      int

	// The storage failures (limits N2): writes the database itself refused,
	// as opposed to a writer that lost a race or asked from the wrong place.
	// failing is whether the newest write that reached the database was one
	// of them; it clears on the next write that commits.
	storage      int64
	failing      bool
	failingSince time.Time
	streak       int64
	storageErr   string
	storageErrAt time.Time
	// observe hears every change of failing. It is how the capacity beat
	// measures at once instead of up to a tick later.
	observe func(failing bool)
}

func newWriteStats() *writeStats { return &writeStats{} }

func (w *writeStats) record(d time.Duration, rows int64, err error) {
	if w == nil {
		return
	}
	w.mu.Lock()
	var turned, now bool
	defer func() {
		observe := w.observe
		w.mu.Unlock()
		if turned && observe != nil {
			observe(now)
		}
	}()
	if err != nil {
		w.failures++
		if isBusy(err) {
			w.busy++
		}
		w.lastErr = err.Error()
		w.lastErrAt = time.Now()
		if storageFailure(err) {
			w.storage++
			w.streak++
			w.storageErr = err.Error()
			w.storageErrAt = w.lastErrAt
			if !w.failing {
				w.failing, w.failingSince = true, w.lastErrAt
				turned, now = true, true
				// Said once, on the turn: every later failure is counted and
				// the recovery says how many there were. A disk that stays
				// full does not get to fill the log as well.
				log.Printf("store: a write failed and was not recorded: %v", err)
			}
		}
		return
	}
	if rows == 0 {
		// Nothing was written — an INSERT OR IGNORE that ignored, a CAS that
		// lost. Not a write, and counting it as one would hide exactly the
		// difference this counter exists to show.
		return
	}
	if w.failing {
		log.Printf("store: writing again after %d failed write(s) since %s", w.streak, w.failingSince.Format(time.RFC3339))
		w.failing, w.failingSince, w.streak = false, time.Time{}, 0
		turned, now = true, false
	}
	w.writes++
	w.rows += rows
	w.lastAt = time.Now()
	w.ring[w.next] = d
	w.next = (w.next + 1) % ringSize
	if w.filled < ringSize {
		w.filled++
	}
}

// storageFailure is an error that says the database could not be written:
// the disk is full, the file is read-only, gone, corrupt or not a database,
// the operating system failed an I/O. Those are the failures that lose a fact
// (limits N2), and the only ones that turn the store's row to failing.
//
// A busy writer (ErrBusy) and a lost compare-and-set (ErrConflict) wrote
// nothing and may be tried again; a constraint, a call from inside a write,
// a caller that went away, and a store's own refusal are about the request,
// not the disk. None of those is a storage failure, and counting them as one
// would turn /v1/health red for contention.
func storageFailure(err error) bool {
	if err == nil || errors.Is(err, ErrBusy) || errors.Is(err, ErrConflict) || errors.Is(err, ErrNestedWrite) {
		return false
	}
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	switch se.Code() & 0xff {
	case sqliteperm, sqlitenomem, sqlitereadonly, sqliteioerr, sqlitecorrupt,
		sqlitefull, sqlitecantopen, sqliteprotocol, sqlitenolfs, sqlitenotadb:
		return true
	}
	return false
}

// SQLite's primary result codes that mean the database could not be written
// (https://sqlite.org/rescode.html).
const (
	sqliteperm     = 3
	sqlitenomem    = 7
	sqlitereadonly = 8
	sqliteioerr    = 10
	sqlitecorrupt  = 11
	sqlitefull     = 13
	sqlitecantopen = 14
	sqliteprotocol = 15
	sqlitenolfs    = 22
	sqlitenotadb   = 26
)

// ObserveFailing hands f every change of the store's failing state: true when
// a write the database refused follows one that committed, false when a write
// commits again. f runs on the writer's goroutine after the store has let go
// of its own lock, and must not block.
func (s *Store) ObserveFailing(f func(failing bool)) {
	if s.stats == nil {
		return
	}
	s.stats.mu.Lock()
	s.stats.observe = f
	s.stats.mu.Unlock()
}

// isBusy is SQLite's "another connection holds the write lock". It is counted
// on its own because it is the one failure that says the store is contended
// rather than broken.
func isBusy(err error) bool {
	if errors.Is(err, ErrBusy) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "database is locked")
}

// timedWrite runs one write transaction and records what it cost. fn answers
// how many rows it changed; zero means it wrote nothing.
func (s *Store) timedWrite(fn func() (int64, error)) error {
	began := time.Now()
	rows, err := fn()
	s.stats.record(time.Since(began), rows, err)
	return err
}

// WriteStats is the store's account of its own writes since this process
// opened it.
type WriteStats struct {
	// Writes is how many write transactions committed something.
	Writes int64
	// Rows is how many rows those transactions changed, events included.
	Rows     int64
	Failures int64
	Busy     int64
	P99      time.Duration
	Last     time.Time
	LastErr  string
	ErrAt    time.Time
	// StorageFailures is how many writes the database refused for a storage
	// reason; Failing is whether the newest write that reached it was one,
	// since FailingSince. StorageErr is the newest such refusal's words.
	StorageFailures int64
	Failing         bool
	FailingSince    time.Time
	StorageErr      string
	StorageErrAt    time.Time
}

// Stats reads the write account.
func (s *Store) Stats() WriteStats {
	w := s.stats
	if w == nil {
		return WriteStats{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	out := WriteStats{
		Writes: w.writes, Rows: w.rows, Failures: w.failures, Busy: w.busy,
		Last: w.lastAt, LastErr: w.lastErr, ErrAt: w.lastErrAt,
		StorageFailures: w.storage, Failing: w.failing, FailingSince: w.failingSince,
		StorageErr: w.storageErr, StorageErrAt: w.storageErrAt,
	}
	if w.filled > 0 {
		sample := make([]time.Duration, w.filled)
		copy(sample, w.ring[:w.filled])
		sort.Slice(sample, func(i, j int) bool { return sample[i] < sample[j] })
		idx := (len(sample)*99 + 99) / 100
		if idx > len(sample) {
			idx = len(sample)
		}
		out.P99 = sample[idx-1]
	}
	return out
}

// Health is whether the store answers at all, and how much it holds.
type Health struct {
	Ready bool
	Err   string
	Tasks int
	// Changes is SQLite's own count of rows this process's connection has
	// changed since it opened, across every table and every writer — not just
	// the broker's. It is the number that would move if anything wrote.
	Changes int64
}

// Health asks the store two cheap questions. A store that cannot answer them
// is `ready: false` with its own sentence, never an empty one: an unreadable
// store and a store with no tasks are different facts.
func (s *Store) Health(ctx context.Context) Health {
	var h Health
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM broker_tasks`).Scan(&h.Tasks); err != nil {
		h.Err = err.Error()
		return h
	}
	if err := s.db.QueryRowContext(ctx, `SELECT total_changes()`).Scan(&h.Changes); err != nil {
		h.Err = err.Error()
		return h
	}
	h.Ready = true
	return h
}

// EventCount is how many events of one kind the log holds, or of every kind
// when kind is empty. For the checks that a fact was recorded exactly once,
// and that an observation was not recorded at all.
func (s *Store) EventCount(ctx context.Context, kind string) (int, error) {
	var n int
	var err error
	if kind == "" {
		err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&n)
	} else {
		err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE kind = ?`, kind).Scan(&n)
	}
	return n, err
}
