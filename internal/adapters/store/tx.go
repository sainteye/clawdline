package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"modernc.org/sqlite"
)

// The write right (docs/design-decisions.md D08).
//
// A write is one `BEGIN IMMEDIATE` transaction: the read that decides, the
// decision and the write happen inside it, and SQLite's own lock is what keeps
// every other writer out — another goroutine, a CLI, a second daemon on the
// same file. It replaces the broker's in-process `writeMu`, which only ever
// held off writers in this process, while the upsert under it let anybody else
// write over a row without being stopped or noticed.
//
// Holding the write right is holding the only connection this handle has, so
// two rules come with it and both are enforced here rather than by naming:
//
//   - nothing inside the transaction may ask this store for anything else.
//     With one connection it would wait for itself for ever; here it is
//     refused at once with ErrNestedWrite.
//   - nothing inside it may reach outside the process — a terminal, git, a
//     push. InsideWrite is how the callers that do those things find out they
//     are being asked to from inside a transaction, and refuse (the broker's
//     effect guard). The store's own I/O is not "outside": it is what the lock
//     is protecting.

// ErrBusy is SQLite's "another connection holds the write lock", after this
// handle has waited busy_timeout for it. It says the store is contended, not
// broken: nothing was written, and the same request can be tried again.
var ErrBusy = errors.New("store busy: another writer holds the database")

// ErrConflict is a compare-and-set that found the row no longer at the
// version its writer read. Nothing was written; the writer's decision was
// about a row that has since changed.
var ErrConflict = errors.New("store conflict: the row changed since it was read")

// ErrNestedWrite is a store call made from inside a write transaction's own
// callback. It would deadlock on the one connection; it is refused instead.
var ErrNestedWrite = errors.New("store call from inside a write transaction")

// busyTimeoutMS is D25's `busy_timeout=1000`: how long a writer waits for
// another connection's write lock before it is told ErrBusy.
const busyTimeoutMS = 1000

// classify turns the driver's busy answer into ErrBusy, keeping its text.
func classify(err error) error {
	if err == nil || errors.Is(err, ErrBusy) {
		return err
	}
	var se *sqlite.Error
	if errors.As(err, &se) && se.Code()&0xff == 5 { // SQLITE_BUSY and its extended codes
		return fmt.Errorf("%w (%v)", ErrBusy, err)
	}
	msg := err.Error()
	if strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "database is locked") {
		return fmt.Errorf("%w (%v)", ErrBusy, err)
	}
	return err
}

// writers is every goroutine currently inside a write callback, across every
// handle in this process.
var writers sync.Map // goroutine id → struct{}

// goroutineID is the running goroutine's number, read from its own stack
// header ("goroutine 123 [running]:"). It is the only identity a callback
// cannot shed by capturing a different context, which is why the guard uses
// it and not a context value.
func goroutineID() int64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	field := bytes.TrimPrefix(buf[:n], []byte("goroutine "))
	if i := bytes.IndexByte(field, ' '); i > 0 {
		field = field[:i]
	}
	id, _ := strconv.ParseInt(string(field), 10, 64)
	return id
}

// InsideWrite reports whether the calling goroutine is inside a write
// transaction's callback.
func InsideWrite() bool {
	_, ok := writers.Load(goroutineID())
	return ok
}

// enterWrite marks the calling goroutine as holding the write right until the
// returned function runs.
func enterWrite() func() {
	id := goroutineID()
	writers.Store(id, struct{}{})
	return func() { writers.Delete(id) }
}

// write runs fn as one write transaction and records what it cost. fn answers
// how many rows it changed; zero means it wrote nothing, and a transaction
// that wrote nothing is rolled back rather than committed.
func (s *Store) write(ctx context.Context, fn func(tx *sql.Tx) (int64, error)) error {
	if InsideWrite() {
		return ErrNestedWrite
	}
	return s.timedWrite(func() (int64, error) {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return 0, classify(err)
		}
		defer tx.Rollback()
		leave := enterWrite()
		n, err := fn(tx)
		leave()
		if err != nil {
			return 0, classify(err)
		}
		if n == 0 {
			return 0, nil
		}
		if err := tx.Commit(); err != nil {
			return 0, classify(err)
		}
		return n, nil
	})
}

// reading refuses a read made from inside a write callback, which would wait
// on this handle's one connection for ever.
func reading() error {
	if InsideWrite() {
		return ErrNestedWrite
	}
	return nil
}
