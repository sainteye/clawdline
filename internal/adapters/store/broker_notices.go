package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// The completion notice, in a ledger of its own.
//
// It used to live inside the task's record, which made every change to it a
// rewrite of the whole task: an attempt, a deferral, an acknowledgement, each
// one reading the record, changing one field of the notice and writing every
// other field back as it had read them. That is safe only while one lock
// serialises all of them, and it is the shape in which an ACK can be written
// over by an attempt that read the record a moment before the ACK arrived.
//
// Here a notice is a row, and every change to it is a compare-and-set against
// the state and attempt count its writer saw. An ACK and an attempt that race
// cannot both win: whichever commits second finds the row no longer says what
// it read, and changes nothing. The task row is not touched at all.
//
// Timestamps are Unix seconds, and 0 means "never", as in every other table
// here; the wire says seconds too, so nothing is lost that anybody reads.

const brokerNoticeSchema = `
CREATE TABLE IF NOT EXISTS broker_notices (
  notice_id       TEXT    PRIMARY KEY,
  task_id         TEXT    NOT NULL UNIQUE,
  state           TEXT    NOT NULL,
  attempts        INTEGER NOT NULL DEFAULT 0,
  created_at      INTEGER NOT NULL,
  last_attempt_at INTEGER NOT NULL DEFAULT 0,
  next_retry_at   INTEGER NOT NULL DEFAULT 0,
  delivered_at    INTEGER NOT NULL DEFAULT 0,
  observed_at     INTEGER NOT NULL DEFAULT 0,
  acknowledged_at INTEGER NOT NULL DEFAULT 0,
  dead_letter_at  INTEGER NOT NULL DEFAULT 0,
  recipient       TEXT    NOT NULL DEFAULT '',
  error_code      TEXT    NOT NULL DEFAULT '',
  error_message   TEXT    NOT NULL DEFAULT '',
  error_at        INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS broker_notices_due ON broker_notices(state, next_retry_at);
`

// BrokerNotice is one completion envelope as this store holds it.
type BrokerNotice struct {
	ID             string
	TaskID         string
	State          string
	Attempts       int
	CreatedAt      time.Time
	LastAttemptAt  time.Time
	NextRetryAt    time.Time
	DeliveredAt    time.Time
	ObservedAt     time.Time
	AcknowledgedAt time.Time
	DeadLetterAt   time.Time
	Recipient      string
	ErrorCode      string
	ErrorMessage   string
	ErrorAt        time.Time
}

// NoticeExpect is what a writer read before it decided: the one transition it
// is allowed to move the notice out of.
type NoticeExpect struct {
	State    string
	Attempts int
}

// ErrNoNotice is "this task has no completion envelope", which is a different
// answer from a read that failed.
var ErrNoNotice = errors.New("no completion notice")

func unixOrZeroTime(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func timeOrZero(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(v, 0)
}

const noticeColumns = `notice_id, task_id, state, attempts, created_at, last_attempt_at, next_retry_at,
  delivered_at, observed_at, acknowledged_at, dead_letter_at, recipient, error_code, error_message, error_at`

func scanNotice(sc scanner) (BrokerNotice, error) {
	var n BrokerNotice
	var created, last, next, delivered, observed, acked, dead, errAt int64
	err := sc.Scan(&n.ID, &n.TaskID, &n.State, &n.Attempts, &created, &last, &next,
		&delivered, &observed, &acked, &dead, &n.Recipient, &n.ErrorCode, &n.ErrorMessage, &errAt)
	if err == sql.ErrNoRows {
		return BrokerNotice{}, ErrNoNotice
	}
	if err != nil {
		return BrokerNotice{}, err
	}
	n.CreatedAt = timeOrZero(created)
	n.LastAttemptAt = timeOrZero(last)
	n.NextRetryAt = timeOrZero(next)
	n.DeliveredAt = timeOrZero(delivered)
	n.ObservedAt = timeOrZero(observed)
	n.AcknowledgedAt = timeOrZero(acked)
	n.DeadLetterAt = timeOrZero(dead)
	n.ErrorAt = timeOrZero(errAt)
	return n, nil
}

func noticeArgs(n BrokerNotice) []any {
	return []any{n.State, n.Attempts, unixOrZeroTime(n.CreatedAt), unixOrZeroTime(n.LastAttemptAt),
		unixOrZeroTime(n.NextRetryAt), unixOrZeroTime(n.DeliveredAt), unixOrZeroTime(n.ObservedAt),
		unixOrZeroTime(n.AcknowledgedAt), unixOrZeroTime(n.DeadLetterAt), n.Recipient,
		n.ErrorCode, n.ErrorMessage, unixOrZeroTime(n.ErrorAt)}
}

func insertNotice(ctx context.Context, tx *sql.Tx, n BrokerNotice) error {
	args := append([]any{n.ID, n.TaskID}, noticeArgs(n)...)
	_, err := tx.ExecContext(ctx,
		`INSERT INTO broker_notices (`+noticeColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, args...)
	return err
}

// BrokerNotice reads one task's completion envelope.
func (s *Store) BrokerNotice(ctx context.Context, taskID string) (BrokerNotice, error) {
	if err := reading(); err != nil {
		return BrokerNotice{}, err
	}
	return scanNotice(s.db.QueryRowContext(ctx,
		`SELECT `+noticeColumns+` FROM broker_notices WHERE task_id = ?`, taskID))
}

// BrokerNotices reads every envelope, keyed by task, for a list that joins
// them onto the task rows without asking once per row.
func (s *Store) BrokerNotices(ctx context.Context) (map[string]BrokerNotice, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+noticeColumns+` FROM broker_notices`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]BrokerNotice{}
	for rows.Next() {
		n, err := scanNotice(rows)
		if err != nil {
			return nil, err
		}
		out[n.TaskID] = n
	}
	return out, rows.Err()
}

// DueBrokerNotices is every open envelope whose next attempt is due, oldest
// deadline first, at most limit of them.
func (s *Store) DueBrokerNotices(ctx context.Context, now time.Time, limit int) ([]BrokerNotice, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+noticeColumns+` FROM broker_notices
		 WHERE state IN ('pending', 'delivered') AND next_retry_at <= ?
		 ORDER BY next_retry_at ASC, created_at ASC LIMIT ?`, now.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BrokerNotice{}
	for rows.Next() {
		n, err := scanNotice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// UpdateBrokerNotice moves one envelope, only if it is still in the state and
// at the attempt count its writer read, with the event that explains the move,
// in one transaction.
//
// `applied` false is not an error. It is the answer "somebody else moved it
// first", and the caller's decision — made about a notice that no longer
// exists in that form — is discarded rather than written over theirs.
func (s *Store) UpdateBrokerNotice(ctx context.Context, expect NoticeExpect, next BrokerNotice, events []Event) (applied bool, err error) {
	err = s.write(ctx, func(tx *sql.Tx) (int64, error) {
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
		if err != nil {
			return 0, err
		}
		if n != 1 {
			return 0, nil
		}
		if err := insertEvents(ctx, tx, events); err != nil {
			return 0, err
		}
		applied = true
		return n + int64(len(events)), nil
	})
	return applied, err
}

// NoticeCounts is the ledger's shape at one moment, for /v1/diagnostics.
type NoticeCounts struct {
	Pending       int
	Delivered     int
	DeadLetter    int
	Acknowledged  int
	OldestPending time.Time
}

// BrokerNoticeCounts reads the ledger's totals.
func (s *Store) BrokerNoticeCounts(ctx context.Context) (NoticeCounts, error) {
	var out NoticeCounts
	rows, err := s.db.QueryContext(ctx, `SELECT state, COUNT(*) FROM broker_notices GROUP BY state`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			rows.Close()
			return out, err
		}
		switch state {
		case "pending":
			out.Pending = n
		case "delivered":
			out.Delivered = n
		case "dead_letter":
			out.DeadLetter = n
		case "acknowledged":
			out.Acknowledged = n
		}
	}
	rows.Close()
	var oldest sql.NullInt64
	if err := s.db.QueryRowContext(ctx,
		`SELECT MIN(created_at) FROM broker_notices WHERE state IN ('pending', 'delivered')`).Scan(&oldest); err != nil {
		return out, err
	}
	if oldest.Valid {
		out.OldestPending = timeOrZero(oldest.Int64)
	}
	return out, rows.Err()
}

// legacyNotice is the envelope as the first broker wave kept it: inside the
// task's record, under `notice`, with RFC 3339 times.
type legacyNotice struct {
	ID             string    `json:"notice_id"`
	State          string    `json:"state"`
	Attempts       int       `json:"attempts"`
	CreatedAt      time.Time `json:"created_at"`
	LastAttemptAt  time.Time `json:"last_attempt_at"`
	NextRetryAt    time.Time `json:"next_retry_at"`
	DeliveredAt    time.Time `json:"transport_delivered_at"`
	ObservedAt     time.Time `json:"observed_at"`
	AcknowledgedAt time.Time `json:"acknowledged_at"`
	DeadLetterAt   time.Time `json:"dead_letter_at"`
	Recipient      string    `json:"recipient"`
	LastError      *struct {
		Code    string    `json:"code"`
		Message string    `json:"message"`
		At      time.Time `json:"at"`
	} `json:"last_error"`
}

// migrateBrokerNotices moves every envelope still inside a task record into
// the ledger, and takes it out of the record, in one transaction per task.
//
// Stated as what it wants rather than as a version: a record with no `notice`
// key is skipped, so running this on every open is a no-op once it has run,
// and a store written by the first wave arrives here with its notices intact.
// A record whose notice cannot be read is left exactly as it was — a notice
// somebody is owed is not dropped because this code could not parse it.
func migrateBrokerNotices(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, record FROM broker_tasks WHERE record LIKE '%"notice":%'`)
	if err != nil {
		return err
	}
	type pending struct {
		id     string
		record string
	}
	todo := []pending{}
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.record); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, p)
	}
	rows.Close()
	for _, p := range todo {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(p.record), &fields); err != nil {
			continue
		}
		raw, ok := fields["notice"]
		if !ok {
			continue
		}
		delete(fields, "notice")
		stripped, err := json.Marshal(fields)
		if err != nil {
			continue
		}
		var old legacyNotice
		if string(raw) != "null" {
			if err := json.Unmarshal(raw, &old); err != nil || old.ID == "" {
				continue
			}
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if old.ID != "" {
			n := BrokerNotice{
				ID: old.ID, TaskID: p.id, State: old.State, Attempts: old.Attempts,
				CreatedAt: old.CreatedAt, LastAttemptAt: old.LastAttemptAt, NextRetryAt: old.NextRetryAt,
				DeliveredAt: old.DeliveredAt, ObservedAt: old.ObservedAt,
				AcknowledgedAt: old.AcknowledgedAt, DeadLetterAt: old.DeadLetterAt,
				Recipient: old.Recipient,
			}
			if old.LastError != nil {
				n.ErrorCode, n.ErrorMessage, n.ErrorAt = old.LastError.Code, old.LastError.Message, old.LastError.At
			}
			args := append([]any{n.ID, n.TaskID}, noticeArgs(n)...)
			if _, err := tx.Exec(`INSERT OR IGNORE INTO broker_notices (`+noticeColumns+`)
			   VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, args...); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if _, err := tx.Exec(`UPDATE broker_tasks SET record = ? WHERE id = ?`, string(stripped), p.id); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
