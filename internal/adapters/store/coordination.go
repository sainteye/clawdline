package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/coordinator"
)

// The coordination plane's tables (docs/design-decisions.md §6 W5): the
// machine role, the leases (D06 ②) and the file waits — in the broker's own
// file, because every one of them is decided together with a broker fact or
// beside one (D02, broker-design #7). The Swift app kept the role in its own
// JSON file with its own lock, which is why a role move and the task state it
// hands over could never be one transaction.

const coordinationSchema = `
CREATE TABLE IF NOT EXISTS coordinator (
  id              INTEGER PRIMARY KEY CHECK (id = 1),
  record_id       TEXT    NOT NULL,
  label           TEXT    NOT NULL,
  terminal_id     TEXT    NOT NULL,
  conversation_id TEXT    NOT NULL,
  assistant       TEXT    NOT NULL,
  pid             INTEGER NOT NULL,
  registered_at   INTEGER NOT NULL,
  rebound_at      INTEGER NOT NULL DEFAULT 0,
  generation      INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS leases (
  resource      TEXT    NOT NULL,
  key           TEXT    NOT NULL,
  lease_id      TEXT    NOT NULL,
  request_id    TEXT    NOT NULL,
  holder        TEXT    NOT NULL,
  reason        TEXT    NOT NULL DEFAULT '',
  session       TEXT    NOT NULL DEFAULT '',
  pid           INTEGER NOT NULL DEFAULT 0,
  process_start INTEGER NOT NULL DEFAULT 0,
  requested_at  INTEGER NOT NULL,
  acquired_at   INTEGER NOT NULL,
  renewed_at    INTEGER NOT NULL,
  phase         TEXT    NOT NULL DEFAULT '',
  PRIMARY KEY (resource, key)
);
CREATE TABLE IF NOT EXISTS lease_waiters (
  resource      TEXT    NOT NULL,
  key           TEXT    NOT NULL,
  request_id    TEXT    NOT NULL,
  holder        TEXT    NOT NULL,
  reason        TEXT    NOT NULL DEFAULT '',
  session       TEXT    NOT NULL DEFAULT '',
  pid           INTEGER NOT NULL DEFAULT 0,
  process_start INTEGER NOT NULL DEFAULT 0,
  requested_at  INTEGER NOT NULL,
  asked_at      INTEGER NOT NULL,
  PRIMARY KEY (resource, key, request_id)
);
CREATE TABLE IF NOT EXISTS waits (
  id                TEXT    PRIMARY KEY,
  repository        TEXT    NOT NULL,
  paths             TEXT    NOT NULL,
  owner             TEXT    NOT NULL,
  release_condition TEXT    NOT NULL,
  created_at        INTEGER NOT NULL,
  released_at       INTEGER NOT NULL DEFAULT 0,
  release_commit    TEXT    NOT NULL DEFAULT '',
  release_note      TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS waits_open ON waits(released_at, repository);
CREATE TABLE IF NOT EXISTS wait_waiters (
  wait_id              TEXT    NOT NULL,
  waiter               TEXT    NOT NULL,
  reason               TEXT    NOT NULL,
  created_at           INTEGER NOT NULL,
  request_delivered_at INTEGER NOT NULL DEFAULT 0,
  release_delivered_at INTEGER NOT NULL DEFAULT 0,
  cancelled_at         INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (wait_id, waiter)
);
`

// openCoordination makes the tables and brings an older `coordinator` table
// to this shape.
//
// The older table had a column called `session_id` that held a terminal id —
// the second spelling of a session that #36 retires. It is renamed, not
// copied: the values in it were always terminal ids, and now the column says
// so. The columns the exact identity needs are added; a row from before them
// has none of them and reads as a record this daemon cannot vouch for
// (`unsupported`), which is refused rather than overwritten.
func openCoordination(db *sql.DB) error {
	if has, err := hasColumn(db, "coordinator", "session_id"); err != nil {
		return err
	} else if has {
		if done, err := hasColumn(db, "coordinator", "terminal_id"); err != nil {
			return err
		} else if !done {
			if _, err := db.Exec(`ALTER TABLE coordinator RENAME COLUMN session_id TO terminal_id`); err != nil {
				return fmt.Errorf("coordinator.session_id: %w", err)
			}
		}
	}
	if _, err := db.Exec(coordinationSchema); err != nil {
		return err
	}
	for _, step := range []struct{ column, ddl string }{
		{"tty", "ALTER TABLE coordinator ADD COLUMN tty TEXT NOT NULL DEFAULT ''"},
		{"process_start", "ALTER TABLE coordinator ADD COLUMN process_start INTEGER NOT NULL DEFAULT 0"},
		{"cwd", "ALTER TABLE coordinator ADD COLUMN cwd TEXT NOT NULL DEFAULT ''"},
		{"session_label", "ALTER TABLE coordinator ADD COLUMN session_label TEXT NOT NULL DEFAULT ''"},
		{"aliases", "ALTER TABLE coordinator ADD COLUMN aliases TEXT NOT NULL DEFAULT '[]'"},
	} {
		has, err := hasColumn(db, "coordinator", step.column)
		if err != nil {
			return err
		}
		if !has {
			if _, err := db.Exec(step.ddl); err != nil {
				return fmt.Errorf("coordinator.%s: %w", step.column, err)
			}
		}
	}
	return nil
}

// --- the machine role -------------------------------------------------------

// CoordinatorStatus is what the stored role is, as the Swift app's inspection
// names it: absent, ready, or one of the two ways a row can be there and not
// be a record this daemon can act on.
type CoordinatorStatus string

const (
	CoordinatorAbsent      CoordinatorStatus = "absent"
	CoordinatorReady       CoordinatorStatus = "ready"
	CoordinatorCorrupt     CoordinatorStatus = "corrupt"
	CoordinatorUnsupported CoordinatorStatus = "unsupported"
)

// ErrCoordinatorExists is an insert that found a role already stored.
var ErrCoordinatorExists = errors.New("a coordinator is already stored")

// Coordinator reads the machine role.
//
// A missing row, a row that cannot be decoded and a row an older version
// wrote must not project the same answer as "nobody has the role": the first
// is a fact and the others are ignorance. A read that failed is an error,
// never a nil record.
func (s *Store) Coordinator(ctx context.Context) (*coordinator.Record, CoordinatorStatus, error) {
	if err := reading(); err != nil {
		return nil, "", err
	}
	return scanCoordinator(s.db.QueryRowContext(ctx, coordinatorSelect))
}

const coordinatorSelect = `SELECT record_id, label, terminal_id, conversation_id, assistant, pid,
  registered_at, rebound_at, generation, tty, process_start, cwd, session_label, aliases
  FROM coordinator WHERE id = 1`

func scanCoordinator(row *sql.Row) (*coordinator.Record, CoordinatorStatus, error) {
	var r coordinator.Record
	var label, aliases string
	var registered, rebound, start int64
	err := row.Scan(&r.ID, &label, &r.TerminalID, &r.ConversationID, &r.Assistant, &r.PID,
		&registered, &rebound, &r.Generation, &r.TTY, &start, &r.CWD, &r.SessionLabel, &aliases)
	if err == sql.ErrNoRows {
		return nil, CoordinatorAbsent, nil
	}
	if err != nil {
		return nil, "", classify(err)
	}
	r.RegisteredAt = timeOrZero(registered)
	r.ReboundAt = timeOrZero(rebound)
	r.ProcessStart = timeOrZero(start)
	if err := json.Unmarshal([]byte(aliases), &r.Aliases); err != nil {
		return nil, CoordinatorCorrupt, nil
	}
	if !r.Valid() {
		// A row the first version of this daemon wrote has no process start
		// and an id made of two session names; it is not a record the rules
		// here can compare with anything.
		return nil, CoordinatorUnsupported, nil
	}
	return &r, CoordinatorReady, nil
}

// CoordinatorExpect is the compare-and-set a role write is made against: the
// role id and the generation the writer read. Nil means "there is no role".
type CoordinatorExpect struct {
	ID         string
	Generation int64
}

// CommitCoordinator writes the role, only if it is still what the writer
// read, with the events that explain it, in one transaction (broker-design
// #7: `UPDATE coordinator SET … WHERE generation = ?`). A role that moved in
// between is ErrConflict and nothing is written; an insert over a stored row
// is ErrCoordinatorExists.
func (s *Store) CommitCoordinator(ctx context.Context, expect *CoordinatorExpect, next coordinator.Record, events []Event) error {
	aliases, err := json.Marshal(next.Aliases)
	if err != nil {
		return err
	}
	if next.Aliases == nil {
		aliases = []byte("[]")
	}
	var outcome error
	err = s.write(ctx, func(tx *sql.Tx) (int64, error) {
		args := []any{next.ID, coordinator.Label, next.TerminalID, next.ConversationID, next.Assistant, next.PID,
			unixOrZeroTime(next.RegisteredAt), unixOrZeroTime(next.ReboundAt), next.Generation,
			next.TTY, unixOrZeroTime(next.ProcessStart), next.CWD, next.SessionLabel, string(aliases)}
		var res sql.Result
		var err error
		if expect == nil {
			res, err = tx.ExecContext(ctx, `INSERT INTO coordinator
			   (id, record_id, label, terminal_id, conversation_id, assistant, pid, registered_at, rebound_at,
			    generation, tty, process_start, cwd, session_label, aliases)
			   VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			   ON CONFLICT(id) DO NOTHING`, args...)
		} else {
			res, err = tx.ExecContext(ctx, `UPDATE coordinator SET
			   record_id = ?, label = ?, terminal_id = ?, conversation_id = ?, assistant = ?, pid = ?,
			   registered_at = ?, rebound_at = ?, generation = ?, tty = ?, process_start = ?, cwd = ?,
			   session_label = ?, aliases = ?
			 WHERE id = 1 AND record_id = ? AND generation = ?`, append(args, expect.ID, expect.Generation)...)
		}
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		if n != 1 {
			outcome = ErrConflict
			if expect == nil {
				outcome = ErrCoordinatorExists
			}
			return 0, nil
		}
		if err := insertEvents(ctx, tx, events); err != nil {
			return 0, err
		}
		return n + int64(len(events)), nil
	})
	if err != nil {
		return err
	}
	return outcome
}

// --- leases (D06 ②) ---------------------------------------------------------

// LeaseRow is one holder or one waiter of a lease.
type LeaseRow struct {
	Resource  string
	Key       string
	RequestID string
	// LeaseID is set on a holder only.
	LeaseID string
	Holder  string
	Reason  string
	// Session is the conversation id whose presence proves the holder or
	// waiter alive; PID and ProcessStart are the process that does. Either,
	// both or neither may be given.
	Session      string
	PID          int
	ProcessStart time.Time
	RequestedAt  time.Time
	AcquiredAt   time.Time
	RenewedAt    time.Time
	// AskedAt is a waiter's last ask.
	AskedAt time.Time
	Phase   string
}

// LeaseState is one lease as a decision reads it: its holder, if any, and
// its waiters, oldest first.
type LeaseState struct {
	Resource string
	Key      string
	Holder   *LeaseRow
	Waiters  []LeaseRow
}

// LeaseChange is what a decision writes. The rows it names replace or remove
// the ones stored; nothing else about the lease is touched.
type LeaseChange struct {
	// SetHolder replaces the holder; ClearHolder removes it.
	SetHolder   *LeaseRow
	ClearHolder bool
	// PutWaiters are inserted or replaced by request id; DropWaiters removed.
	PutWaiters  []LeaseRow
	DropWaiters []string
	Events      []Event
}

func (c LeaseChange) empty() bool {
	return c.SetHolder == nil && !c.ClearHolder && len(c.PutWaiters) == 0 && len(c.DropWaiters) == 0 &&
		len(c.Events) == 0
}

const leaseColumns = `resource, key, request_id, lease_id, holder, reason, session, pid, process_start,
  requested_at, acquired_at, renewed_at, phase`

const waiterColumns = `resource, key, request_id, holder, reason, session, pid, process_start,
  requested_at, asked_at`

func scanHolder(sc scanner) (LeaseRow, error) {
	var r LeaseRow
	var start, requested, acquired, renewed int64
	err := sc.Scan(&r.Resource, &r.Key, &r.RequestID, &r.LeaseID, &r.Holder, &r.Reason, &r.Session, &r.PID,
		&start, &requested, &acquired, &renewed, &r.Phase)
	r.ProcessStart, r.RequestedAt, r.AcquiredAt, r.RenewedAt =
		timeOrZero(start), timeOrZero(requested), timeOrZero(acquired), timeOrZero(renewed)
	return r, err
}

func scanWaiter(sc scanner) (LeaseRow, error) {
	var r LeaseRow
	var start, requested, asked int64
	err := sc.Scan(&r.Resource, &r.Key, &r.RequestID, &r.Holder, &r.Reason, &r.Session, &r.PID,
		&start, &requested, &asked)
	r.ProcessStart, r.RequestedAt, r.AskedAt = timeOrZero(start), timeOrZero(requested), timeOrZero(asked)
	return r, err
}

func readLease(ctx context.Context, q querier, resource, key string) (LeaseState, error) {
	st := LeaseState{Resource: resource, Key: key}
	rows, err := q.QueryContext(ctx, `SELECT `+leaseColumns+` FROM leases WHERE resource = ? AND key = ?`, resource, key)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		h, err := scanHolder(rows)
		if err != nil {
			rows.Close()
			return st, err
		}
		st.Holder = &h
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return st, err
	}
	rows, err = q.QueryContext(ctx, `SELECT `+waiterColumns+` FROM lease_waiters
	   WHERE resource = ? AND key = ? ORDER BY requested_at, request_id`, resource, key)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		w, err := scanWaiter(rows)
		if err != nil {
			return st, err
		}
		st.Waiters = append(st.Waiters, w)
	}
	return st, rows.Err()
}

// Lease reads one lease outside any write.
func (s *Store) Lease(ctx context.Context, resource, key string) (LeaseState, error) {
	if err := reading(); err != nil {
		return LeaseState{}, err
	}
	st, err := readLease(ctx, s.db, resource, key)
	return st, classify(err)
}

// Leases reads every lease that has a holder or a waiter.
func (s *Store) Leases(ctx context.Context) ([]LeaseState, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT resource, key FROM leases
	   UNION SELECT resource, key FROM lease_waiters ORDER BY 1, 2`)
	if err != nil {
		return nil, classify(err)
	}
	type rk struct{ r, k string }
	var keys []rk
	for rows.Next() {
		var x rk
		if err := rows.Scan(&x.r, &x.k); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, x)
	}
	rows.Close()
	out := make([]LeaseState, 0, len(keys))
	for _, x := range keys {
		st, err := readLease(ctx, s.db, x.r, x.k)
		if err != nil {
			return nil, classify(err)
		}
		out = append(out, st)
	}
	return out, nil
}

// DecideLease runs decide over one lease inside the write right and writes
// what it answers: the read that decides and the write it decides on cannot
// be split by another writer (D08). decide must not reach outside the
// process; whatever it needs to know about the world it is handed.
func (s *Store) DecideLease(ctx context.Context, resource, key string, decide func(LeaseState) (LeaseChange, error)) error {
	var decided error
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		st, err := readLease(ctx, tx, resource, key)
		if err != nil {
			return 0, err
		}
		change, err := decide(st)
		if err != nil {
			decided = err
			return 0, nil
		}
		if change.empty() {
			return 0, nil
		}
		n := int64(0)
		if change.ClearHolder || change.SetHolder != nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM leases WHERE resource = ? AND key = ?`, resource, key); err != nil {
				return 0, err
			}
			n++
		}
		if h := change.SetHolder; h != nil {
			if _, err := tx.ExecContext(ctx, `INSERT INTO leases (`+leaseColumns+`)
			   VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				resource, key, h.RequestID, h.LeaseID, h.Holder, h.Reason, h.Session, h.PID,
				unixOrZeroTime(h.ProcessStart), unixOrZeroTime(h.RequestedAt), unixOrZeroTime(h.AcquiredAt),
				unixOrZeroTime(h.RenewedAt), h.Phase); err != nil {
				return 0, err
			}
			n++
		}
		for _, id := range change.DropWaiters {
			if _, err := tx.ExecContext(ctx, `DELETE FROM lease_waiters WHERE resource = ? AND key = ? AND request_id = ?`,
				resource, key, id); err != nil {
				return 0, err
			}
			n++
		}
		for _, w := range change.PutWaiters {
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO lease_waiters (`+waiterColumns+`)
			   VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				resource, key, w.RequestID, w.Holder, w.Reason, w.Session, w.PID,
				unixOrZeroTime(w.ProcessStart), unixOrZeroTime(w.RequestedAt), unixOrZeroTime(w.AskedAt)); err != nil {
				return 0, err
			}
			n++
		}
		if err := insertEvents(ctx, tx, change.Events); err != nil {
			return 0, err
		}
		return n + int64(len(change.Events)), nil
	})
	if err != nil {
		return err
	}
	return decided
}

// ProcessGone asks the system whether a process exists: gone is positive
// ("no such process"), and known false is a system that would not say —
// which is never a reason to treat the process as gone.
func ProcessGone(pid int) (gone, known bool) {
	if pid <= 0 {
		return false, false
	}
	return processGone(pid)
}

// --- file waits ---------------------------------------------------------------

// WaitRow is one file wait: a repository's paths held by an owner session,
// and the sessions waiting for it to let them go.
type WaitRow struct {
	ID               string
	Repository       string
	Paths            []string
	Owner            string
	ReleaseCondition string
	CreatedAt        time.Time
	ReleasedAt       time.Time
	ReleaseCommit    string
	ReleaseNote      string
	Waiters          []WaiterRow
}

// WaiterRow is one session waiting on a wait.
type WaiterRow struct {
	Waiter             string
	Reason             string
	CreatedAt          time.Time
	RequestDeliveredAt time.Time
	ReleaseDeliveredAt time.Time
	CancelledAt        time.Time
}

// Open reports whether the waiter still waits: not cancelled, and not told
// of a release.
func (w WaiterRow) Open() bool { return w.CancelledAt.IsZero() && w.ReleaseDeliveredAt.IsZero() }

// OpenWaits is every wait not yet fully released, with its waiters.
func (s *Store) OpenWaits(ctx context.Context) ([]WaitRow, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	return openWaits(ctx, s.db)
}

func openWaits(ctx context.Context, q querier) ([]WaitRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, repository, paths, owner, release_condition, created_at,
	   released_at, release_commit, release_note FROM waits WHERE released_at = 0 ORDER BY created_at, id`)
	if err != nil {
		return nil, classify(err)
	}
	var out []WaitRow
	for rows.Next() {
		var w WaitRow
		var paths string
		var created, released int64
		if err := rows.Scan(&w.ID, &w.Repository, &paths, &w.Owner, &w.ReleaseCondition, &created,
			&released, &w.ReleaseCommit, &w.ReleaseNote); err != nil {
			rows.Close()
			return nil, err
		}
		_ = json.Unmarshal([]byte(paths), &w.Paths)
		w.CreatedAt, w.ReleasedAt = timeOrZero(created), timeOrZero(released)
		out = append(out, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		ws, err := waitersOf(ctx, q, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Waiters = ws
	}
	return out, nil
}

func waitersOf(ctx context.Context, q querier, id string) ([]WaiterRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT waiter, reason, created_at, request_delivered_at,
	   release_delivered_at, cancelled_at FROM wait_waiters WHERE wait_id = ? ORDER BY created_at, waiter`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WaiterRow
	for rows.Next() {
		var w WaiterRow
		var created, req, rel, cancelled int64
		if err := rows.Scan(&w.Waiter, &w.Reason, &created, &req, &rel, &cancelled); err != nil {
			return nil, err
		}
		w.CreatedAt, w.RequestDeliveredAt = timeOrZero(created), timeOrZero(req)
		w.ReleaseDeliveredAt, w.CancelledAt = timeOrZero(rel), timeOrZero(cancelled)
		out = append(out, w)
	}
	return out, rows.Err()
}

// WaitChange is what a wait decision writes.
type WaitChange struct {
	// Put inserts or replaces the wait row itself (not its waiters).
	Put *WaitRow
	// PutWaiters inserts or replaces waiters of the wait named by WaitID.
	WaitID     string
	PutWaiters []WaiterRow
	Events     []Event
	Effects    []Effect
}

// DecideWaits runs decide over every open wait inside the write right, and
// writes what it answers, with the effects it owes recorded as intent (D08).
// The effect ids come back, this handle's to run.
func (s *Store) DecideWaits(ctx context.Context, decide func(open []WaitRow) (WaitChange, error)) ([]int64, error) {
	var decided error
	var ids []int64
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		open, err := openWaits(ctx, tx)
		if err != nil {
			return 0, err
		}
		change, err := decide(open)
		if err != nil {
			decided = err
			return 0, nil
		}
		n := int64(0)
		if w := change.Put; w != nil {
			paths, _ := json.Marshal(w.Paths)
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO waits
			   (id, repository, paths, owner, release_condition, created_at, released_at, release_commit, release_note)
			   VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				w.ID, w.Repository, string(paths), w.Owner, w.ReleaseCondition, unixOrZeroTime(w.CreatedAt),
				unixOrZeroTime(w.ReleasedAt), w.ReleaseCommit, w.ReleaseNote); err != nil {
				return 0, err
			}
			n++
		}
		for _, w := range change.PutWaiters {
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO wait_waiters
			   (wait_id, waiter, reason, created_at, request_delivered_at, release_delivered_at, cancelled_at)
			   VALUES (?, ?, ?, ?, ?, ?, ?)`,
				change.WaitID, w.Waiter, w.Reason, unixOrZeroTime(w.CreatedAt), unixOrZeroTime(w.RequestDeliveredAt),
				unixOrZeroTime(w.ReleaseDeliveredAt), unixOrZeroTime(w.CancelledAt)); err != nil {
				return 0, err
			}
			n++
		}
		if err := insertEvents(ctx, tx, change.Events); err != nil {
			return 0, err
		}
		for _, e := range change.Effects {
			id, err := s.insertEffect(ctx, tx, e)
			if err != nil {
				return 0, err
			}
			ids = append(ids, id)
		}
		return n + int64(len(change.Events)+len(change.Effects)), nil
	})
	if err != nil {
		return nil, err
	}
	return ids, decided
}

// OpenWaitCount is how many waits are not yet fully released, for the
// capacity register (`waits.open`).
func (s *Store) OpenWaitCount(ctx context.Context) (int, error) {
	if err := reading(); err != nil {
		return 0, err
	}
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM waits WHERE released_at = 0`).Scan(&n)
	return n, classify(err)
}

// --- completion notices, for the dead-letter path -----------------------------

// CompletionNotices reads the completion ledger for the manual path a dead
// letter needs (GET /v1/orchestrator/completions): every notice not yet
// acknowledged, or with all true every notice created since `since`.
func (s *Store) CompletionNotices(ctx context.Context, since time.Time, all bool, limit int) ([]BrokerNotice, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	q := `SELECT ` + noticeColumns + ` FROM broker_notices WHERE state != 'acknowledged' ORDER BY created_at DESC LIMIT ?`
	args := []any{limit}
	if all {
		q = `SELECT ` + noticeColumns + ` FROM broker_notices WHERE created_at >= ? ORDER BY created_at DESC LIMIT ?`
		args = []any{unixOrZeroTime(since), limit}
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	var out []BrokerNotice
	for rows.Next() {
		n, err := scanNotice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// UpdateBrokerNoticeWith is UpdateBrokerNotice that also records the effects
// the move owes — a dead letter's push to the person — as intent in the same
// transaction (D08, D24). The ids come back only when the move applied.
func (s *Store) UpdateBrokerNoticeWith(ctx context.Context, expect NoticeExpect, next BrokerNotice, events []Event, effects []Effect) (bool, []int64, error) {
	applied := false
	var ids []int64
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		args := append(noticeArgs(next), next.ID, next.TaskID, expect.State, expect.Attempts)
		res, err := tx.ExecContext(ctx,
			`UPDATE broker_notices SET
			   state = ?, attempts = ?, created_at = ?, last_attempt_at = ?, next_retry_at = ?,
			   delivered_at = ?, observed_at = ?, acknowledged_at = ?, dead_letter_at = ?,
			   recipient = ?, error_code = ?, error_message = ?, error_at = ?
			 WHERE notice_id = ? AND task_id = ? AND state = ? AND attempts = ?`, args...)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		if err != nil || n != 1 {
			return 0, err
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
		applied = true
		return n + int64(len(events)+len(effects)), nil
	})
	if !applied {
		ids = nil
	}
	return applied, ids, err
}
