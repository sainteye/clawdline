package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
)

// The cursors a unit of work leaves in the token ledger (docs/token-ledger.md
// "One unit of work"): at a child task's admission and its end, and at a Board
// item's start and end of implementation, each session counted for the unit
// has its cumulative ledger reading copied here. A unit's cost is the
// difference, recomputed from these rows whenever it is asked.
//
// **Append-only and first wins.** One row per (unit kind, unit id, cycle,
// edge, session): a replayed dispatch, a settlement posted twice or a phase set
// twice inserts nothing. An item reopened after it closed is a new cycle, so its
// second implementation is new rows rather than an overwrite.
//
// **Totals only**, as usage_transcripts: a reading is token counts by part,
// calls, compactions, peak context, model names, sizes and times. No prompt,
// tool input, terminal output or transcript text is ever written here.
const workCursorSchema = `
CREATE TABLE IF NOT EXISTS usage_work_cursors (
  seq        INTEGER PRIMARY KEY AUTOINCREMENT,
  unit_kind  TEXT    NOT NULL,
  unit_id    TEXT    NOT NULL,
  cycle      TEXT    NOT NULL DEFAULT '',
  edge       TEXT    NOT NULL,
  session    TEXT    NOT NULL DEFAULT '',
  at         INTEGER NOT NULL,
  taken_at   INTEGER NOT NULL,
  outcome    TEXT    NOT NULL DEFAULT '',
  state      TEXT    NOT NULL DEFAULT '',
  reading    TEXT    NOT NULL DEFAULT '{}',
  UNIQUE (unit_kind, unit_id, cycle, edge, session)
);
CREATE INDEX IF NOT EXISTS usage_work_cursors_at ON usage_work_cursors(at);
CREATE INDEX IF NOT EXISTS usage_work_cursors_unit ON usage_work_cursors(unit_kind, unit_id, cycle);
`

func openWorkCursors(db *sql.DB) error {
	_, err := db.Exec(workCursorSchema)
	return err
}

// WorkCursorRowLimit is how many cursor rows the table keeps (capacity row
// usage.work_cursor_rows). Past it the oldest rows go: they are a journal of
// measurements, and the ledger itself still holds every session's cumulative
// totals, so what is lost is only how an old unit's cost was split.
const WorkCursorRowLimit = 50_000

// The edges a cursor is taken at.
const (
	WorkCursorStart = "start"
	WorkCursorEnd   = "end"
	// Release and Acquire are an item's owner changing mid-unit: the leaving
	// session's reading at release, the arriving one's at acquisition.
	WorkCursorRelease = "release"
	WorkCursorAcquire = "acquire"
	// Settled is a later reading of a session whose end (or release) cursor
	// found the ledger behind its transcript, as "settled:end" or
	// "settled:release"; the cursor it settles is never rewritten.
	WorkCursorSettled = "settled"
)

// WorkCursorMissing is the state of a unit's own marker row (session "") when
// the cursor for that edge could not be taken.
const WorkCursorMissing = "cursor_missing"

// WorkCursor is one row.
type WorkCursor struct {
	Seq      int64
	UnitKind string
	UnitID   string
	Cycle    string
	Edge     string
	// Session is the conversation the reading is of; empty is the unit's own
	// marker, which says the edge happened, its outcome and whether taking it
	// failed.
	Session string
	At      time.Time
	TakenAt time.Time
	Outcome string
	State   string
	Reading json.RawMessage
}

const workCursorColumns = `seq, unit_kind, unit_id, cycle, edge, session, at, taken_at, outcome, state, reading`

// AddWorkCursors inserts rows, each only when no row has its key yet, and
// answers how many were new. With a marker (a row whose session is empty) the
// rows go in only when the marker itself is new: an edge already taken is not
// taken again, so a replayed start cannot give a session that joined the unit
// later a starting reading it never had. Past WorkCursorRowLimit the oldest
// rows go in the same transaction.
func (s *Store) AddWorkCursors(ctx context.Context, marker *WorkCursor, rows []WorkCursor) (int64, error) {
	all := rows
	if marker != nil {
		all = append([]WorkCursor{*marker}, rows...)
	}
	if len(all) == 0 {
		return 0, nil
	}
	for _, r := range all {
		if r.UnitKind == "" || r.UnitID == "" || r.Edge == "" {
			return 0, errors.New("a work cursor needs its unit and edge")
		}
	}
	var added int64
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		added = 0
		for i, r := range all {
			reading := string(r.Reading)
			if reading == "" {
				reading = "{}"
			}
			res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO usage_work_cursors
        (unit_kind, unit_id, cycle, edge, session, at, taken_at, outcome, state, reading)
        VALUES (?,?,?,?,?,?,?,?,?,?)`, r.UnitKind, r.UnitID, r.Cycle, r.Edge, r.Session,
				r.At.UnixNano(), r.TakenAt.UnixNano(), r.Outcome, r.State, reading)
			if err != nil {
				return 0, err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return 0, err
			}
			if i == 0 && marker != nil && n == 0 {
				return 0, nil
			}
			added += n
		}
		if added == 0 {
			return 0, nil
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM usage_work_cursors WHERE seq <=
        (SELECT MAX(seq) FROM usage_work_cursors) - ?`, WorkCursorRowLimit); err != nil {
			return 0, err
		}
		return added, nil
	})
	return added, err
}

// WorkCursorCount is how many rows the table holds.
func (s *Store) WorkCursorCount(ctx context.Context) (int64, error) {
	if err := reading(); err != nil {
		return 0, err
	}
	var n int64
	err := s.rd.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_work_cursors`).Scan(&n)
	return n, classify(err)
}

// WorkCursorUnit names one unit cycle.
type WorkCursorUnit struct {
	Kind, ID, Cycle string
}

// WorkCursorsBetween is every row of the units that have any row at or after
// since and, when until is not zero, at or before it: whole units, so a unit
// that started before since and ended inside the range has its start. At most
// limit units are read, the most recent first; more says some were left out.
func (s *Store) WorkCursorsBetween(ctx context.Context, since, until time.Time, limit int) ([]WorkCursor, bool, error) {
	if err := reading(); err != nil {
		return nil, false, err
	}
	end := int64(1<<63 - 1)
	if !until.IsZero() {
		end = until.UnixNano()
	}
	units, err := s.rd.QueryContext(ctx, `SELECT unit_kind, unit_id, cycle, MAX(at) AS last FROM usage_work_cursors
    WHERE at >= ? AND at <= ? GROUP BY unit_kind, unit_id, cycle ORDER BY last DESC, unit_kind, unit_id, cycle LIMIT ?`,
		since.UnixNano(), end, limit+1)
	if err != nil {
		return nil, false, classify(err)
	}
	var keys []WorkCursorUnit
	for units.Next() {
		var k WorkCursorUnit
		var last int64
		if err := units.Scan(&k.Kind, &k.ID, &k.Cycle, &last); err != nil {
			units.Close()
			return nil, false, err
		}
		keys = append(keys, k)
	}
	if err := units.Err(); err != nil {
		units.Close()
		return nil, false, err
	}
	units.Close()
	more := len(keys) > limit
	if more {
		keys = keys[:limit]
	}
	var out []WorkCursor
	for _, k := range keys {
		rows, err := s.WorkCursorsOf(ctx, k)
		if err != nil {
			return nil, false, err
		}
		out = append(out, rows...)
	}
	return out, more, nil
}

// WorkCursorsOf is every row of one unit cycle, in the order they were added.
func (s *Store) WorkCursorsOf(ctx context.Context, k WorkCursorUnit) ([]WorkCursor, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.rd.QueryContext(ctx, `SELECT `+workCursorColumns+` FROM usage_work_cursors
    WHERE unit_kind=? AND unit_id=? AND cycle=? ORDER BY seq`, k.Kind, k.ID, k.Cycle)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	return scanWorkCursors(rows)
}

// WorkCursorsAwaitingSettlement is the end and release rows whose state says
// the ledger was behind and that have no settled row yet, oldest first, at
// most limit of them.
func (s *Store) WorkCursorsAwaitingSettlement(ctx context.Context, behind string, limit int) ([]WorkCursor, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.rd.QueryContext(ctx, `SELECT `+prefixed("c.", workCursorColumns)+` FROM usage_work_cursors c
    WHERE c.edge IN (?, ?) AND c.state = ? AND c.session <> '' AND NOT EXISTS (
      SELECT 1 FROM usage_work_cursors s WHERE s.unit_kind=c.unit_kind AND s.unit_id=c.unit_id
        AND s.cycle=c.cycle AND s.edge=? || ':' || c.edge AND s.session=c.session)
    ORDER BY c.seq LIMIT ?`, WorkCursorEnd, WorkCursorRelease, behind, WorkCursorSettled, limit)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	return scanWorkCursors(rows)
}

// WorkCursorEndsWithoutSessions is the end markers of one unit kind taken at
// or after since whose end counted no session and that no settling pass has
// added one to, newest first, at most limit of them: a child that ended
// before the ledger first read its transcript.
func (s *Store) WorkCursorEndsWithoutSessions(ctx context.Context, kind string, since time.Time, limit int) ([]WorkCursor, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.rd.QueryContext(ctx, `SELECT `+prefixed("c.", workCursorColumns)+` FROM usage_work_cursors c
    WHERE c.unit_kind = ? AND c.edge = ? AND c.session = '' AND c.state = '' AND c.at >= ? AND NOT EXISTS (
      SELECT 1 FROM usage_work_cursors s WHERE s.unit_kind=c.unit_kind AND s.unit_id=c.unit_id
        AND s.cycle=c.cycle AND s.session <> '' AND s.edge IN (?, ?))
    ORDER BY c.seq DESC LIMIT ?`, kind, WorkCursorEnd, since.UnixNano(), WorkCursorEnd,
		WorkCursorSettled+":"+WorkCursorEnd, limit)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	return scanWorkCursors(rows)
}

func prefixed(p, columns string) string {
	parts := strings.Split(columns, ",")
	for i, c := range parts {
		parts[i] = p + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}

func scanWorkCursors(rows *sql.Rows) ([]WorkCursor, error) {
	var out []WorkCursor
	for rows.Next() {
		var c WorkCursor
		var at, taken int64
		var reading string
		if err := rows.Scan(&c.Seq, &c.UnitKind, &c.UnitID, &c.Cycle, &c.Edge, &c.Session, &at, &taken,
			&c.Outcome, &c.State, &reading); err != nil {
			return nil, err
		}
		c.At, c.TakenAt, c.Reading = time.Unix(0, at), time.Unix(0, taken), json.RawMessage(reading)
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------- what a Board write changed ----------

// WorkV2Change is one item or step a committed Board write changed: the row as
// it was and as it is.
type WorkV2Change struct {
	Prev, Next         work.ItemV2
	Step               bool
	PrevStep, NextStep work.StepV2
}

var workV2Observers sync.Map // *Store -> func([]WorkV2Change)

// ObserveWorkV2 has fn told, after every Board write commits, which items and
// steps it changed. fn runs on the writer's goroutine after the commit and
// must not block or write to this store; a nil fn stops the telling.
func (s *Store) ObserveWorkV2(fn func([]WorkV2Change)) {
	if fn == nil {
		workV2Observers.Delete(s)
		return
	}
	workV2Observers.Store(s, fn)
}

func (s *Store) toldWorkV2(changes []WorkV2Change) {
	if len(changes) == 0 {
		return
	}
	if fn, ok := workV2Observers.Load(s); ok {
		fn.(func([]WorkV2Change))(changes)
	}
}
