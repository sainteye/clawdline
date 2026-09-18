package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/work"
)

// The board and the Backlog (board-redesign §3.2, design-decisions T3).
//
// Four tables beside the broker's, in the same file, so that a change of an
// item and the move that records it are one transaction, and the facts a
// change rests on — the broker's task rows — are read inside that same
// transaction (D02, DG-6):
//
//   - `work` is the identity: the id, the project and the title do not change
//     when the item moves;
//   - `board_items` and `backlog` are the two structures, each with its own
//     invariants in the schema. A work item has a row in at most one of them,
//     which two triggers hold (a row in neither is an item a person said
//     needs no following: its to-dos, T2, are all that track it);
//   - `moves` is every change, append-only — two more triggers refuse an
//     UPDATE or a DELETE — each with who made it and the fact it rests on.
//
// The derived display state is not here (D04): work.Derive computes it on
// every read from these rows and the broker's.

const workSchema = `
CREATE TABLE IF NOT EXISTS work (
  id          TEXT    PRIMARY KEY,
  project_id  TEXT    NOT NULL,
  title       TEXT    NOT NULL,
  acceptance  TEXT,
  created_at  INTEGER NOT NULL,
  created_by  TEXT    NOT NULL,
  placed_at   INTEGER NOT NULL,
  version     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS work_project ON work(project_id, created_at);
CREATE TABLE IF NOT EXISTS board_items (
  work_id       TEXT    PRIMARY KEY REFERENCES work(id),
  state         TEXT    NOT NULL CHECK (state IN ('active','awaiting_closure','done','dropped')),
  owner         TEXT    NOT NULL CHECK (owner <> ''),
  commitment    TEXT    NOT NULL CHECK (commitment IN ('dispatch','assigned','scheduled','decision','delivered')),
  cycle_since   INTEGER NOT NULL,
  evidence_at   INTEGER NOT NULL,
  asked_at      INTEGER,
  closed_reason TEXT    CHECK (closed_reason IN ('landed','accepted','unconfirmed','dropped')),
  closed_at     INTEGER,
  start_on      TEXT,
  CHECK ((state IN ('done','dropped')) = (closed_reason IS NOT NULL AND closed_at IS NOT NULL)),
  CHECK (state <> 'dropped' OR closed_reason = 'dropped'),
  CHECK (closed_reason <> 'dropped' OR state = 'dropped')
);
CREATE INDEX IF NOT EXISTS board_items_state ON board_items(state, closed_at);
CREATE TABLE IF NOT EXISTS backlog (
  work_id     TEXT    PRIMARY KEY REFERENCES work(id),
  state       TEXT    NOT NULL CHECK (state IN ('planned','dropped')),
  rank        INTEGER CHECK (rank IS NULL OR rank > 0),
  start_on    TEXT    CHECK (start_on IS NULL OR start_on GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
  reviewed_at INTEGER,
  dropped_at  INTEGER,
  CHECK ((state = 'dropped') = (dropped_at IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS backlog_order ON backlog(state, rank, work_id);
CREATE TABLE IF NOT EXISTS moves (
  seq      INTEGER PRIMARY KEY AUTOINCREMENT,
  work_id  TEXT    NOT NULL REFERENCES work(id),
  from_s   TEXT    NOT NULL CHECK (from_s IN ('todo','proposal','board','backlog','none')),
  to_s     TEXT    NOT NULL CHECK (to_s IN ('todo','proposal','board','backlog','none')),
  state    TEXT    NOT NULL,
  trigger  TEXT    NOT NULL CHECK (trigger <> ''),
  actor    TEXT    NOT NULL CHECK (actor <> ''),
  evidence TEXT    NOT NULL CHECK (json_valid(evidence)),
  at       INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS moves_work ON moves(work_id, seq);
CREATE TRIGGER IF NOT EXISTS work_one_place_board BEFORE INSERT ON board_items
  WHEN EXISTS (SELECT 1 FROM backlog WHERE work_id = NEW.work_id)
  BEGIN SELECT RAISE(ABORT, 'work_in_two_places'); END;
CREATE TRIGGER IF NOT EXISTS work_one_place_backlog BEFORE INSERT ON backlog
  WHEN EXISTS (SELECT 1 FROM board_items WHERE work_id = NEW.work_id)
  BEGIN SELECT RAISE(ABORT, 'work_in_two_places'); END;
CREATE TRIGGER IF NOT EXISTS moves_no_update BEFORE UPDATE ON moves
  BEGIN SELECT RAISE(ABORT, 'moves_append_only'); END;
CREATE TRIGGER IF NOT EXISTS moves_no_delete BEFORE DELETE ON moves
  BEGIN SELECT RAISE(ABORT, 'moves_append_only'); END;
CREATE INDEX IF NOT EXISTS broker_tasks_work ON broker_tasks(json_extract(record, '$.work_id'))
  WHERE json_valid(record);
`

// The last index is on the broker's table and is the board's: a task is
// bound to a work item by the `work_id` its dispatch named (D36), and the
// sweep asks which tasks name an item. Without it that question reads every
// task ever dispatched (G33). It is partial on `json_valid` so that a row
// nobody can decode (D05 ②) is not indexed rather than refused.

// WorkOpenLimit is the most open work items — on the board and not closed,
// or planned in the Backlog — this store keeps. It is the register's
// `work.open` row: at it a new item is refused (ErrWorkFull), nothing is let
// go, because every one of them is a person's plan (board-redesign §5.3).
const WorkOpenLimit = 2_000

// ErrNoWork is "no work item has that id", distinct from a read that failed.
var ErrNoWork = errors.New("no such work item")

// ErrWorkFull is a new item refused at WorkOpenLimit.
var ErrWorkFull = errors.New("work items full")

func openWork(db *sql.DB) error {
	if _, err := db.Exec(workSchema); err != nil {
		return err
	}
	_, err := db.Exec(boardSettingsSchema)
	return err
}

// WorkMove is one row of `moves`.
type WorkMove struct {
	Seq      int64
	WorkID   string
	From     work.Place
	To       work.Place
	State    work.ItemState
	Trigger  string
	Actor    string
	Evidence json.RawMessage
	At       time.Time
}

// MoveOf is the row a change writes.
func MoveOf(id string, c work.Change, at time.Time) WorkMove {
	evidence := c.Evidence
	if evidence == nil {
		evidence = map[string]any{}
	}
	body, _ := json.Marshal(evidence)
	return WorkMove{WorkID: id, From: c.From, To: c.To, State: c.State, Trigger: c.Trigger, Actor: c.Actor,
		Evidence: body, At: at}
}

const workColumns = `w.id, w.project_id, w.title, COALESCE(w.acceptance, ''), w.created_at, w.created_by,
  w.placed_at, w.version,
  b.state, b.owner, b.commitment, b.cycle_since, b.evidence_at, b.asked_at, b.closed_reason, b.closed_at, b.start_on,
  k.state, k.rank, k.start_on, k.reviewed_at, k.dropped_at,
  (SELECT MIN(d.created_at) FROM decisions d WHERE d.work_id = w.id AND d.state = 'open')`

const workFrom = ` FROM work w LEFT JOIN board_items b ON b.work_id = w.id LEFT JOIN backlog k ON k.work_id = w.id`

func scanWork(sc scanner) (work.Item, error) {
	var it work.Item
	var created, placed int64
	var bState, bOwner, bCommit, bReason, bStart, kState, kStart sql.NullString
	var bSince, bEvidence, bAsked, bClosed, kRank, kReviewed, kDropped, decided sql.NullInt64
	err := sc.Scan(&it.ID, &it.Project, &it.Title, &it.Acceptance, &created, &it.CreatedBy, &placed, &it.Version,
		&bState, &bOwner, &bCommit, &bSince, &bEvidence, &bAsked, &bReason, &bClosed, &bStart,
		&kState, &kRank, &kStart, &kReviewed, &kDropped, &decided)
	if err == sql.ErrNoRows {
		return work.Item{}, ErrNoWork
	}
	if err != nil {
		return work.Item{}, err
	}
	it.CreatedAt, it.PlacedAt = time.Unix(created, 0), time.Unix(placed, 0)
	at := func(v sql.NullInt64) time.Time {
		if !v.Valid || v.Int64 == 0 {
			return time.Time{}
		}
		return time.Unix(v.Int64, 0)
	}
	// The oldest decision waiting for a person on this item (T4): read with
	// the item, never stored on it.
	it.DecisionSince = at(decided)
	switch {
	case bState.Valid:
		it.Place, it.State = work.PlaceBoard, work.ItemState(bState.String)
		it.Owner, it.Commitment = bOwner.String, work.Commitment(bCommit.String)
		it.Since, it.EvidenceAt, it.AskedAt = at(bSince), at(bEvidence), at(bAsked)
		it.ClosedReason, it.ClosedAt, it.StartOn = bReason.String, at(bClosed), bStart.String
	case kState.Valid:
		it.Place, it.State = work.PlaceBacklog, work.ItemState(kState.String)
		it.Rank, it.StartOn, it.ReviewedAt = kRank.Int64, kStart.String, at(kReviewed)
		if it.State == work.ItemDropped {
			it.ClosedReason, it.ClosedAt = work.ClosedDropped, at(kDropped)
		}
	default:
		it.Place = work.PlaceTodo
	}
	return it, nil
}

// WorkTx is what a change to the board may do while it holds the write
// right. Every method runs inside the one transaction.
type WorkTx struct {
	ctx   context.Context
	tx    *sql.Tx
	s     *Store
	wrote int64
}

// Item reads one work item as the transaction sees it.
func (t *WorkTx) Item(id string) (work.Item, error) {
	return scanWork(t.tx.QueryRowContext(t.ctx, `SELECT `+workColumns+workFrom+` WHERE w.id = ?`, id))
}

// Tasks reads every broker task bound to a work item, as the transaction
// sees them: the facts a change is decided from.
func (t *WorkTx) Tasks(id string) ([]BrokerRow, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+brokerColumns+` FROM broker_tasks
		WHERE json_valid(record) AND json_extract(record, '$.work_id') = ? ORDER BY created_at, id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BrokerRow{}
	for rows.Next() {
		r, err := scanBroker(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// OpenCount is how many items are open: on the board and not closed, or
// planned in the Backlog.
func (t *WorkTx) OpenCount() (int64, error) {
	var n int64
	err := t.tx.QueryRowContext(t.ctx, openCountQuery).Scan(&n)
	return n, err
}

const openCountQuery = `SELECT
  (SELECT COUNT(*) FROM board_items WHERE state IN ('active','awaiting_closure')) +
  (SELECT COUNT(*) FROM backlog WHERE state = 'planned')`

// Create writes a new item in its first place with the move that says who
// made it. At limit open items it writes nothing and answers ErrWorkFull.
func (t *WorkTx) Create(it work.Item, m WorkMove, limit int64) error {
	if n, err := t.OpenCount(); err != nil {
		return err
	} else if limit > 0 && n >= limit {
		return ErrWorkFull
	}
	if _, err := t.tx.ExecContext(t.ctx,
		`INSERT INTO work (id, project_id, title, acceptance, created_at, created_by, placed_at, version)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0)`,
		it.ID, it.Project, it.Title, emptyOrNull(it.Acceptance), it.CreatedAt.Unix(), it.CreatedBy,
		it.PlacedAt.Unix()); err != nil {
		return err
	}
	if err := t.place(it); err != nil {
		return err
	}
	return t.appendMove(m)
}

// Put writes an item's change and its move, only if the item is still at the
// version prev was read at: a writer holding a stale copy is told
// ErrConflict and nothing is written.
func (t *WorkTx) Put(prev, next work.Item, m WorkMove) error {
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE work SET placed_at = ?, version = version + 1 WHERE id = ? AND version = ?`,
		next.PlacedAt.Unix(), next.ID, prev.Version)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrConflict
	}
	for _, q := range []string{`DELETE FROM board_items WHERE work_id = ?`, `DELETE FROM backlog WHERE work_id = ?`} {
		if _, err := t.tx.ExecContext(t.ctx, q, next.ID); err != nil {
			return err
		}
	}
	if err := t.place(next); err != nil {
		return err
	}
	return t.appendMove(m)
}

// place writes the item's row in the structure it is in.
func (t *WorkTx) place(it work.Item) error {
	var err error
	switch it.Place {
	case work.PlaceBoard:
		_, err = t.tx.ExecContext(t.ctx,
			`INSERT INTO board_items (work_id, state, owner, commitment, cycle_since, evidence_at, asked_at,
			   closed_reason, closed_at, start_on) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			it.ID, string(it.State), it.Owner, string(it.Commitment), it.Since.Unix(), it.EvidenceAt.Unix(),
			zeroOrUnix(it.AskedAt), emptyOrNull(it.ClosedReason), zeroOrUnix(it.ClosedAt), emptyOrNull(it.StartOn))
	case work.PlaceBacklog:
		var rank any
		if it.Rank > 0 {
			rank = it.Rank
		}
		var dropped any
		if it.State == work.ItemDropped {
			dropped = it.ClosedAt.Unix()
		}
		_, err = t.tx.ExecContext(t.ctx,
			`INSERT INTO backlog (work_id, state, rank, start_on, reviewed_at, dropped_at) VALUES (?, ?, ?, ?, ?, ?)`,
			it.ID, string(it.State), rank, emptyOrNull(it.StartOn), zeroOrUnix(it.ReviewedAt), dropped)
	case work.PlaceTodo:
	default:
		return errors.New("a work item is placed on the board, in the Backlog or with its to-dos")
	}
	if err == nil {
		t.wrote++
	}
	return err
}

func (t *WorkTx) appendMove(m WorkMove) error {
	if _, err := t.tx.ExecContext(t.ctx,
		`INSERT INTO moves (work_id, from_s, to_s, state, trigger, actor, evidence, at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.WorkID, string(m.From), string(m.To), string(m.State), m.Trigger, m.Actor, string(m.Evidence),
		m.At.Unix()); err != nil {
		return err
	}
	t.wrote += 2
	return nil
}

// CompleteReceipt files a command's answer in the transaction that made the
// change it answers (D03): the change and its receipt are one fact.
func (t *WorkTx) CompleteReceipt(k ReceiptKey, a ReceiptAnswer) error {
	if err := t.s.completeReceipt(t.ctx, t.tx, k, a); err != nil {
		return err
	}
	t.wrote++
	return nil
}

// WriteWork runs fn as one write transaction. fn answering an error writes
// nothing; fn that wrote nothing commits nothing.
func (s *Store) WriteWork(ctx context.Context, fn func(*WorkTx) error) error {
	return s.write(ctx, func(tx *sql.Tx) (int64, error) {
		t := &WorkTx{ctx: ctx, tx: tx, s: s}
		if err := fn(t); err != nil {
			return 0, err
		}
		return t.wrote, nil
	})
}

// WorkItem reads one work item.
func (s *Store) WorkItem(ctx context.Context, id string) (work.Item, error) {
	if err := reading(); err != nil {
		return work.Item{}, err
	}
	return scanWork(s.db.QueryRowContext(ctx, `SELECT `+workColumns+workFrom+` WHERE w.id = ?`, id))
}

// WorkBoard reads the board's items for a project (every project when it is
// empty): every open one, and the closed ones closed at or after since. Its
// cost is the open items — bounded by WorkOpenLimit — and the recently
// closed, never the history. At most limit rows; the second answer says
// there were more.
func (s *Store) WorkBoard(ctx context.Context, project string, since time.Time, limit int) ([]work.Item, bool, error) {
	if err := reading(); err != nil {
		return nil, false, err
	}
	q := `SELECT ` + workColumns + workFrom + ` WHERE b.work_id IS NOT NULL
	  AND (b.state IN ('active','awaiting_closure') OR b.closed_at >= ?)`
	args := []any{since.Unix()}
	if project != "" {
		q += ` AND w.project_id = ?`
		args = append(args, project)
	}
	q += ` ORDER BY w.placed_at DESC, w.id DESC LIMIT ?`
	args = append(args, limit+1)
	items, err := s.queryWork(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	if len(items) > limit {
		return items[:limit], true, nil
	}
	return items, false, nil
}

// BacklogQuery is one page of the planned Backlog, in its order: ranked
// first, lowest rank first, then unranked, oldest first.
type BacklogQuery struct {
	Project string
	// After is the last row of the page before, as BacklogCursor wrote it.
	AfterRank    int64
	AfterCreated int64
	AfterID      string
	Limit        int
}

// backlogOrderKey is the Backlog's order as one comparable value: a rank, or
// a number past every rank for the unranked.
const backlogOrderKey = `COALESCE(k.rank, 9223372036854775807)`

// WorkBacklog reads one page of planned Backlog items.
func (s *Store) WorkBacklog(ctx context.Context, q BacklogQuery) ([]work.Item, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	query := `SELECT ` + workColumns + workFrom + ` WHERE k.state = 'planned'`
	args := []any{}
	if q.Project != "" {
		query += ` AND w.project_id = ?`
		args = append(args, q.Project)
	}
	if q.AfterID != "" {
		query += ` AND (` + backlogOrderKey + ` > ? OR (` + backlogOrderKey + ` = ? AND (w.created_at > ? OR (w.created_at = ? AND w.id > ?))))`
		args = append(args, q.AfterRank, q.AfterRank, q.AfterCreated, q.AfterCreated, q.AfterID)
	}
	query += ` ORDER BY ` + backlogOrderKey + `, w.created_at, w.id LIMIT ?`
	args = append(args, q.Limit)
	return s.queryWork(ctx, query, args...)
}

// BacklogCounts is how many Backlog items there are in each state.
func (s *Store) BacklogCounts(ctx context.Context, project string) (map[work.ItemState]int, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	q := `SELECT k.state, COUNT(*) FROM backlog k JOIN work w ON w.id = k.work_id`
	args := []any{}
	if project != "" {
		q += ` WHERE w.project_id = ?`
		args = append(args, project)
	}
	rows, err := s.db.QueryContext(ctx, q+` GROUP BY k.state`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[work.ItemState]int{}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return nil, err
		}
		out[work.ItemState(state)] = n
	}
	return out, rows.Err()
}

// WorkMoves reads one page of an item's moves, oldest first, after seq.
func (s *Store) WorkMoves(ctx context.Context, id string, after int64, limit int) ([]WorkMove, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq, work_id, from_s, to_s, state, trigger, actor, evidence, at FROM moves
		 WHERE work_id = ? AND seq > ? ORDER BY seq LIMIT ?`, id, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkMove{}
	for rows.Next() {
		var m WorkMove
		var from, to, state, evidence string
		var at int64
		if err := rows.Scan(&m.Seq, &m.WorkID, &from, &to, &state, &m.Trigger, &m.Actor, &evidence, &at); err != nil {
			return nil, err
		}
		m.From, m.To, m.State = work.Place(from), work.Place(to), work.ItemState(state)
		m.Evidence, m.At = json.RawMessage(evidence), time.Unix(at, 0)
		out = append(out, m)
	}
	return out, rows.Err()
}

// WorkTasks reads the broker tasks bound to each of the given work items.
func (s *Store) WorkTasks(ctx context.Context, ids []string) (map[string][]BrokerRow, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	out := map[string][]BrokerRow{}
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
		rows, err := s.queryBroker(ctx, `SELECT `+brokerColumns+` FROM broker_tasks
			WHERE json_valid(record) AND json_extract(record, '$.work_id') IN (`+marks+`)
			ORDER BY created_at, id`, args...)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			var bound struct {
				WorkID string `json:"work_id"`
			}
			if json.Unmarshal(r.Record, &bound) == nil && bound.WorkID != "" {
				out[bound.WorkID] = append(out[bound.WorkID], r)
			}
		}
	}
	return out, nil
}

// WorkSweepCandidates is every item an automatic rule might move: the open
// board items, the closed ones a task was bound to after they closed, and the
// planned Backlog items with a start date or with a task bound after they
// were placed there. Its cost is the open items, not the history (G33).
func (s *Store) WorkSweepCandidates(ctx context.Context) ([]string, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT work_id FROM board_items WHERE state IN ('active','awaiting_closure')
		UNION
		SELECT b.work_id FROM board_items b WHERE b.state = 'done' AND EXISTS (
		  SELECT 1 FROM broker_tasks t WHERE json_valid(t.record)
		    AND json_extract(t.record, '$.work_id') = b.work_id AND t.created_at >= b.closed_at)
		UNION
		SELECT k.work_id FROM backlog k JOIN work w ON w.id = k.work_id
		WHERE k.state = 'planned' AND (k.start_on IS NOT NULL OR EXISTS (
		  SELECT 1 FROM broker_tasks t WHERE json_valid(t.record)
		    AND json_extract(t.record, '$.work_id') = k.work_id AND t.created_at >= w.placed_at))`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// WorkOpenCount is the register's `work.open` reading.
func (s *Store) WorkOpenCount(ctx context.Context) (int64, error) {
	if err := reading(); err != nil {
		return 0, err
	}
	var n int64
	err := s.db.QueryRowContext(ctx, openCountQuery).Scan(&n)
	return n, err
}

func (s *Store) queryWork(ctx context.Context, query string, args ...any) ([]work.Item, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.Item{}
	for rows.Next() {
		it, err := scanWork(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
