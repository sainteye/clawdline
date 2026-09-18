package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// The hand-over plane's tables (docs/design-decisions.md §6 W6): the child
// tabs owed a close (#26), the handoffs and Feature Roots this broker opened
// (broker-design B8), and what the reclamation sweep last decided about each
// checkout and task directory (D13, #46).
//
// All of them are in the broker's own file because each is decided beside a
// broker fact: a linger is owed by the settlement that writes it, and a
// reclamation is decided against the landing record (D01, D02).

const handoverSchema = `
CREATE TABLE IF NOT EXISTS broker_lingers (
  task_id    TEXT    PRIMARY KEY,
  backend    TEXT    NOT NULL,
  pane       TEXT    NOT NULL,
  session    TEXT    NOT NULL,
  deadline   INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS broker_handoffs (
  id         TEXT    PRIMARY KEY,
  state      TEXT    NOT NULL,
  record     TEXT    NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS broker_root_assignments (
  id         TEXT    PRIMARY KEY,
  state      TEXT    NOT NULL,
  record     TEXT    NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS broker_reclaim (
  task_id  TEXT    NOT NULL,
  subject  TEXT    NOT NULL,
  outcome  TEXT    NOT NULL,
  reason   TEXT    NOT NULL,
  bytes    INTEGER NOT NULL DEFAULT 0,
  detail   TEXT    NOT NULL DEFAULT '{}',
  first_at INTEGER NOT NULL,
  last_at  INTEGER NOT NULL,
  PRIMARY KEY (task_id, subject)
);
`

func openHandover(db *sql.DB) error {
	_, err := db.Exec(handoverSchema)
	return err
}

// --- lingers ---------------------------------------------------------------

// Linger is one finished child's tab, owed a close at Deadline (#26).
//
// It is stored because the Swift app kept it in memory, and "Seventeen of the
// eighteen tabs left standing had a restart inside their three minutes"
// (`3a7adb8e`). The row is written by the settlement that owes it, in the same
// transaction, and removed by the transaction that records the close as an
// effect — so a restart between the two finds either the row or the effect,
// never neither.
type Linger struct {
	Task      string
	Backend   string
	Pane      string
	Session   string
	Deadline  time.Time
	CreatedAt time.Time
}

// PutLinger records a tab owed a close, inside the transaction that settles
// its task. A second settlement of the same task cannot happen (it is
// terminal), so an existing row is left as it is.
func (t *Tx) PutLinger(l Linger) error {
	res, err := t.tx.ExecContext(t.ctx,
		`INSERT INTO broker_lingers (task_id, backend, pane, session, deadline, created_at)
		 VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(task_id) DO NOTHING`,
		l.Task, l.Backend, l.Pane, l.Session, l.Deadline.Unix(), l.CreatedAt.Unix())
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	t.wrote += n
	return nil
}

// Lingers reads every tab still owed a close, soonest first.
func (s *Store) Lingers(ctx context.Context) ([]Linger, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id, backend, pane, session, deadline, created_at FROM broker_lingers ORDER BY deadline, task_id`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := []Linger{}
	for rows.Next() {
		var l Linger
		var deadline, created int64
		if err := rows.Scan(&l.Task, &l.Backend, &l.Pane, &l.Session, &deadline, &created); err != nil {
			return nil, classify(err)
		}
		l.Deadline, l.CreatedAt = time.Unix(deadline, 0), time.Unix(created, 0)
		out = append(out, l)
	}
	return out, classify(rows.Err())
}

// ErrNoLinger is a linger that is no longer owed — another pass or broker
// already took it.
var ErrNoLinger = errors.New("no such linger")

// TakeLinger removes a linger and, in the same transaction, records what it
// became: the close effect it owes (effects non-empty) or the event that says
// nothing is left to close. A linger somebody else already took is
// ErrNoLinger, and nothing is recorded.
func (s *Store) TakeLinger(ctx context.Context, task string, events []Event, effects []Effect) ([]int64, error) {
	var ids []int64
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		res, err := tx.ExecContext(ctx, `DELETE FROM broker_lingers WHERE task_id = ?`, task)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return 0, ErrNoLinger
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
		return 1 + int64(len(events)+len(effects)), nil
	})
	return ids, err
}

// --- handoffs and Feature Roots ---------------------------------------------

// Opened is a handoff or a Feature Root as the store keeps it: an id, the
// state column a list is filtered by, and the record the broker owns the shape
// of. The two routes keep the same four columns in two tables, because they
// are two things with two lifecycles and a reader must never find one in a
// list of the other (the dispatch role contract's "Root Assignment … carries
// no child, handoff … lineage").
type Opened struct {
	ID        string
	State     string
	Record    json.RawMessage
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ErrOpenedExists is a create that found the id taken.
var ErrOpenedExists = errors.New("already stored")

// ErrNoOpened is a read of an id this store has never held.
var ErrNoOpened = errors.New("not stored")

// The two tables. Constants, never a caller's string, because they are
// spliced into SQL.
const (
	TableHandoffs        = "broker_handoffs"
	TableRootAssignments = "broker_root_assignments"
)

func openedTable(name string) (string, error) {
	switch name {
	case TableHandoffs, TableRootAssignments:
		return name, nil
	}
	return "", errors.New("not a hand-over table: " + name)
}

// CreateOpened stores a new row with the events that announce it.
func (s *Store) CreateOpened(ctx context.Context, table string, o Opened, events []Event) error {
	name, err := openedTable(table)
	if err != nil {
		return err
	}
	return s.write(ctx, func(tx *sql.Tx) (int64, error) {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO `+name+` (id, state, record, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO NOTHING`,
			o.ID, o.State, string(o.Record), o.CreatedAt.Unix(), o.UpdatedAt.Unix())
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return 0, ErrOpenedExists
		}
		if err := insertEvents(ctx, tx, events); err != nil {
			return 0, err
		}
		return 1 + int64(len(events)), nil
	})
}

// UpdateOpened changes one row as it is now: change is given the stored row
// inside the write transaction and answers the row to write, or nil to leave
// it alone.
func (s *Store) UpdateOpened(ctx context.Context, table, id string, change func(Opened) (*Opened, []Event, error)) (Opened, error) {
	name, err := openedTable(table)
	if err != nil {
		return Opened{}, err
	}
	var out Opened
	err = s.write(ctx, func(tx *sql.Tx) (int64, error) {
		cur, err := scanOpened(tx.QueryRowContext(ctx,
			`SELECT id, state, record, created_at, updated_at FROM `+name+` WHERE id = ?`, id))
		if err != nil {
			return 0, err
		}
		out = cur
		next, events, err := change(cur)
		if err != nil || next == nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE `+name+` SET state = ?, record = ?, updated_at = ? WHERE id = ?`,
			next.State, string(next.Record), next.UpdatedAt.Unix(), id); err != nil {
			return 0, err
		}
		if err := insertEvents(ctx, tx, events); err != nil {
			return 0, err
		}
		out = *next
		return 1 + int64(len(events)), nil
	})
	return out, err
}

// ReadOpened reads one row.
func (s *Store) ReadOpened(ctx context.Context, table, id string) (Opened, error) {
	name, err := openedTable(table)
	if err != nil {
		return Opened{}, err
	}
	if err := reading(); err != nil {
		return Opened{}, err
	}
	return scanOpened(s.db.QueryRowContext(ctx,
		`SELECT id, state, record, created_at, updated_at FROM `+name+` WHERE id = ?`, id))
}

// ListOpened reads the newest rows first, at most limit of them.
func (s *Store) ListOpened(ctx context.Context, table string, limit int) ([]Opened, error) {
	name, err := openedTable(table)
	if err != nil {
		return nil, err
	}
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, state, record, created_at, updated_at FROM `+name+` ORDER BY created_at DESC, id LIMIT ?`, limit)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := []Opened{}
	for rows.Next() {
		o, err := scanOpened(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, classify(rows.Err())
}

func scanOpened(sc scanner) (Opened, error) {
	var o Opened
	var record string
	var created, updated int64
	err := sc.Scan(&o.ID, &o.State, &record, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Opened{}, ErrNoOpened
	}
	if err != nil {
		return Opened{}, classify(err)
	}
	o.Record = json.RawMessage(record)
	o.CreatedAt, o.UpdatedAt = time.Unix(created, 0), time.Unix(updated, 0)
	return o, nil
}

// --- reclamation -------------------------------------------------------------

// Reclaim is what the sweep last decided about one task's checkout or task
// directory, and since when it has decided that.
//
// It exists for #46: the Swift app re-discovered the same checkouts it could
// not remove every six hours and wrote `worktree.kept` again each time — 3,699
// events from 158 tasks, one of them 188 times. Here a decision is said once:
// the event is written only when a subject's outcome or reason changes, and
// the row carries how long it has stood.
type Reclaim struct {
	Task    string
	Subject string
	Outcome string
	Reason  string
	Bytes   int64
	Detail  json.RawMessage
	FirstAt time.Time
	LastAt  time.Time
}

// MarkReclaim records a decision about one subject. The event is written only
// when the decision is new — a subject never decided, or one whose outcome or
// reason changed — and fresh says whether it was.
func (s *Store) MarkReclaim(ctx context.Context, r Reclaim, event *Event) (fresh bool, err error) {
	detail := string(r.Detail)
	if detail == "" {
		detail = "{}"
	}
	err = s.write(ctx, func(tx *sql.Tx) (int64, error) {
		var outcome, reason string
		err := tx.QueryRowContext(ctx,
			`SELECT outcome, reason FROM broker_reclaim WHERE task_id = ? AND subject = ?`, r.Task, r.Subject).
			Scan(&outcome, &reason)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			fresh = true
			_, err = tx.ExecContext(ctx,
				`INSERT INTO broker_reclaim (task_id, subject, outcome, reason, bytes, detail, first_at, last_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				r.Task, r.Subject, r.Outcome, r.Reason, r.Bytes, detail, r.LastAt.Unix(), r.LastAt.Unix())
		case err != nil:
			return 0, err
		case outcome != r.Outcome || reason != r.Reason:
			fresh = true
			_, err = tx.ExecContext(ctx,
				`UPDATE broker_reclaim SET outcome = ?, reason = ?, bytes = ?, detail = ?, first_at = ?, last_at = ?
				 WHERE task_id = ? AND subject = ?`,
				r.Outcome, r.Reason, r.Bytes, detail, r.LastAt.Unix(), r.LastAt.Unix(), r.Task, r.Subject)
		default:
			_, err = tx.ExecContext(ctx,
				`UPDATE broker_reclaim SET bytes = ?, detail = ?, last_at = ? WHERE task_id = ? AND subject = ?`,
				r.Bytes, detail, r.LastAt.Unix(), r.Task, r.Subject)
		}
		if err != nil {
			return 0, err
		}
		if fresh && event != nil {
			if err := insertEvents(ctx, tx, []Event{*event}); err != nil {
				return 0, err
			}
		}
		return 1, nil
	})
	return fresh, err
}

// Reclaims reads every decision, the longest-standing first.
func (s *Store) Reclaims(ctx context.Context) ([]Reclaim, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id, subject, outcome, reason, bytes, detail, first_at, last_at FROM broker_reclaim
		 ORDER BY first_at, task_id, subject`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := []Reclaim{}
	for rows.Next() {
		var r Reclaim
		var detail string
		var first, last int64
		if err := rows.Scan(&r.Task, &r.Subject, &r.Outcome, &r.Reason, &r.Bytes, &detail, &first, &last); err != nil {
			return nil, classify(err)
		}
		r.Detail = json.RawMessage(detail)
		r.FirstAt, r.LastAt = time.Unix(first, 0), time.Unix(last, 0)
		out = append(out, r)
	}
	return out, classify(rows.Err())
}

// ErrNoReclaim is a subject the sweep has never decided about.
var ErrNoReclaim = errors.New("no reclamation decision")

// ReclaimOf reads the standing decision about one subject.
func (s *Store) ReclaimOf(ctx context.Context, task, subject string) (Reclaim, error) {
	if err := reading(); err != nil {
		return Reclaim{}, err
	}
	var r Reclaim
	var detail string
	var first, last int64
	err := s.db.QueryRowContext(ctx,
		`SELECT task_id, subject, outcome, reason, bytes, detail, first_at, last_at FROM broker_reclaim
		 WHERE task_id = ? AND subject = ?`, task, subject).
		Scan(&r.Task, &r.Subject, &r.Outcome, &r.Reason, &r.Bytes, &detail, &first, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return Reclaim{}, ErrNoReclaim
	}
	if err != nil {
		return Reclaim{}, classify(err)
	}
	r.Detail = json.RawMessage(detail)
	r.FirstAt, r.LastAt = time.Unix(first, 0), time.Unix(last, 0)
	return r, nil
}
