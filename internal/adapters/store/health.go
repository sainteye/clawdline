package store

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
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
}

func newWriteStats() *writeStats { return &writeStats{} }

func (w *writeStats) record(d time.Duration, rows int64, err error) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err != nil {
		w.failures++
		if isBusy(err) {
			w.busy++
		}
		w.lastErr = err.Error()
		w.lastErrAt = time.Now()
		return
	}
	if rows == 0 {
		// Nothing was written — an INSERT OR IGNORE that ignored, a CAS that
		// lost. Not a write, and counting it as one would hide exactly the
		// difference this counter exists to show.
		return
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

// isBusy is SQLite's "another connection holds the write lock". It is counted
// on its own because it is the one failure that says the store is contended
// rather than broken.
func isBusy(err error) bool {
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
