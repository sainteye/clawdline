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

// Where a person takes part (board-redesign §4, §8; design-decisions T4).
//
// Three tables beside the board's, in the same file, so that a person's
// answer, the work item it makes and the move that records it are one
// transaction (D02):
//
//   - `proposals` is every proposal and its answer. The server's answer to
//     "may the session ask" is a column, fixed when the row is made, so what
//     was decided and what the session then did can be counted against each
//     other (§4.4, `ask_true` and `asked_inline` in /v1/diagnostics). At most
//     one proposal of a line of work is pending: an index says so, not a
//     reader hoping;
//   - `decisions` is every question a session put to a person, with the
//     answer that stands if nobody gives one and when (DG-10);
//   - `digests` is the daily and weekly digest, one row each, keyed by the day
//     or the week, so a digest is written once however many passes see it
//     due.
//
// Answered proposals and closed decisions are a person's answers — evidence —
// and are kept, like `moves`, on store.db. What waits for a person is bounded
// by its own register row (proposals.open, decisions.open), and each has an
// exit that needs nobody: a proposal's wait, a decision's due.

const participationSchema = `
CREATE TABLE IF NOT EXISTS proposals (
  id              TEXT    PRIMARY KEY,
  work_id         TEXT    NOT NULL,
  task_id         TEXT,
  session         TEXT    NOT NULL CHECK (session <> ''),
  source          TEXT    NOT NULL CHECK (source <> ''),
  project         TEXT    NOT NULL,
  title           TEXT    NOT NULL,
  signals         TEXT    NOT NULL CHECK (json_valid(signals)),
  effects         TEXT    NOT NULL CHECK (json_valid(effects)),
  ask             INTEGER NOT NULL CHECK (ask IN (0, 1)),
  ask_reason      TEXT    NOT NULL CHECK (ask_reason IN
                    ('human_present','human_absent','proposal_budget_exhausted','proposal_from_child','made_by_rule')),
  channel         TEXT    NOT NULL CHECK (channel IN ('session','to_confirm')),
  question        TEXT    NOT NULL DEFAULT '',
  state           TEXT    NOT NULL CHECK (state IN ('pending','answered','expired')),
  answer          TEXT    CHECK (answer IN ('track','later','no')),
  answered_by     TEXT,
  answered_at     INTEGER,
  created_at      INTEGER NOT NULL,
  expires_at      INTEGER NOT NULL,
  asked_inline_at INTEGER,
  version         INTEGER NOT NULL DEFAULT 0,
  CHECK ((state = 'answered') = (answer IS NOT NULL AND answered_at IS NOT NULL)),
  CHECK (ask = (channel = 'session'))
);
CREATE INDEX IF NOT EXISTS proposals_work ON proposals(work_id, created_at);
CREATE INDEX IF NOT EXISTS proposals_task ON proposals(task_id) WHERE task_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS proposals_state ON proposals(state, created_at);
CREATE INDEX IF NOT EXISTS proposals_asks ON proposals(ask, session, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS proposals_one_pending ON proposals(work_id) WHERE state = 'pending';
CREATE TABLE IF NOT EXISTS decisions (
  id             TEXT    PRIMARY KEY,
  session        TEXT    NOT NULL CHECK (session <> ''),
  work_id        TEXT,
  task_id        TEXT,
  project        TEXT    NOT NULL DEFAULT '',
  question       TEXT    NOT NULL CHECK (question <> ''),
  options        TEXT    NOT NULL CHECK (json_valid(options)),
  default_option TEXT    NOT NULL CHECK (default_option <> ''),
  blocking       INTEGER NOT NULL CHECK (blocking IN (0, 1)),
  state          TEXT    NOT NULL CHECK (state IN ('open','answered','defaulted')),
  answer         TEXT,
  answered_by    TEXT,
  answered_at    INTEGER,
  created_at     INTEGER NOT NULL,
  due_at         INTEGER NOT NULL,
  push           TEXT    NOT NULL CHECK (push IN
                   ('none','pending','sent','not_subscribed','over_budget','failed','unknown')),
  pushed_at      INTEGER,
  version        INTEGER NOT NULL DEFAULT 0,
  CHECK ((state = 'open') = (answer IS NULL)),
  CHECK (blocking = 1 OR push = 'none')
);
CREATE INDEX IF NOT EXISTS decisions_state ON decisions(state, due_at);
CREATE INDEX IF NOT EXISTS decisions_work ON decisions(work_id, state) WHERE work_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS decisions_push ON decisions(push, pushed_at);
CREATE TABLE IF NOT EXISTS digests (
  key        TEXT    PRIMARY KEY,
  kind       TEXT    NOT NULL CHECK (kind IN ('daily','weekly')),
  from_at    INTEGER NOT NULL,
  to_at      INTEGER NOT NULL,
  body       TEXT    NOT NULL CHECK (json_valid(body)),
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS digests_kind ON digests(kind, from_at);
CREATE INDEX IF NOT EXISTS moves_at ON moves(at);
`

// The last index is on the board's move log and is the digest's: a day's
// moves are read by their time, and without it that read is every move ever
// made.

// ProposalOpenLimit is the most proposals that may wait for a person at once:
// the register's `proposals.open`. At it a new proposal is refused, and the
// work it was about stays with its to-dos — the answer a person who never
// saw it would have got anyway.
const ProposalOpenLimit = 500

// DecisionOpenLimit is the most decisions that may wait for a person at
// once: the register's `decisions.open`. At it a new decision is refused in
// the answer to the session that asked; none already asked is let go.
const DecisionOpenLimit = 256

// DigestKeepLimit is how many digests are kept: about two years of daily and
// weekly ones. Past it the oldest is let go in the transaction that writes
// the newest; every fact a digest summarised is still in `moves`, the
// proposals and the decisions.
const DigestKeepLimit = 800

// ErrNoProposal and ErrNoDecision are "no row has that id", distinct from a
// read that failed.
var (
	ErrNoProposal = errors.New("no such proposal")
	ErrNoDecision = errors.New("no such decision")
	// ErrProposalsFull and ErrDecisionsFull are a new row refused at its
	// register row's limit.
	ErrProposalsFull = errors.New("proposals full")
	ErrDecisionsFull = errors.New("decisions full")
)

func openParticipation(db *sql.DB) error {
	_, err := db.Exec(participationSchema)
	return err
}

// ——— Proposals ———

const proposalColumns = `id, work_id, COALESCE(task_id, ''), session, source, project, title, signals, effects,
  ask, ask_reason, channel, question, state, COALESCE(answer, ''), COALESCE(answered_by, ''),
  COALESCE(answered_at, 0), created_at, expires_at, COALESCE(asked_inline_at, 0), version`

func scanProposal(sc scanner) (work.Proposal, error) {
	var p work.Proposal
	var signals, effects, channel, state, answer string
	var ask int
	var answered, created, expires, asked int64
	err := sc.Scan(&p.ID, &p.WorkID, &p.TaskID, &p.Session, &p.Source, &p.Project, &p.Title, &signals, &effects,
		&ask, &p.AskReason, &channel, &p.Question, &state, &answer, &p.AnsweredBy, &answered, &created, &expires,
		&asked, &p.Version)
	if err == sql.ErrNoRows {
		return work.Proposal{}, ErrNoProposal
	}
	if err != nil {
		return work.Proposal{}, err
	}
	p.Signals, p.Effects = []work.Signal{}, []work.Effect{}
	if err := json.Unmarshal([]byte(signals), &p.Signals); err != nil {
		return work.Proposal{}, err
	}
	if err := json.Unmarshal([]byte(effects), &p.Effects); err != nil {
		return work.Proposal{}, err
	}
	p.Ask, p.Channel, p.State, p.Answer = ask == 1, work.Channel(channel), work.ProposalState(state), work.Answer(answer)
	p.AnsweredAt, p.CreatedAt, p.ExpiresAt, p.AskedInlineAt = unixOrZero(answered), time.Unix(created, 0),
		time.Unix(expires, 0), unixOrZero(asked)
	return p, nil
}

// Proposal reads one proposal as the transaction sees it.
func (t *WorkTx) Proposal(id string) (work.Proposal, error) {
	return scanProposal(t.tx.QueryRowContext(t.ctx, `SELECT `+proposalColumns+` FROM proposals WHERE id = ?`, id))
}

// PriorProposals reads every proposal of a line of work — by its work id, or
// by the dispatch it was made about — oldest first.
func (t *WorkTx) PriorProposals(workID, taskID string) ([]work.Proposal, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+proposalColumns+` FROM proposals
		WHERE work_id = ? OR (? <> '' AND task_id = ?) ORDER BY created_at, id`, workID, taskID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.Proposal{}
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AskCounts is how many proposals the server let be asked in the
// conversation: by this session since turn, and by anybody since day.
func (t *WorkTx) AskCounts(session string, turn, day time.Time) (inTurn, inDay int, err error) {
	if !turn.IsZero() {
		if err = t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM proposals
			WHERE ask = 1 AND session = ? AND created_at >= ?`, session, turn.Unix()).Scan(&inTurn); err != nil {
			return 0, 0, err
		}
	}
	err = t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM proposals WHERE ask = 1 AND created_at >= ?`,
		day.Unix()).Scan(&inDay)
	return inTurn, inDay, err
}

// PutProposal writes a proposal. With prev nil it is new, and refused with
// ErrProposalsFull at limit pending; otherwise it is a compare-and-set against
// prev's version.
func (t *WorkTx) PutProposal(next work.Proposal, prev *work.Proposal, limit int64) error {
	signals, _ := json.Marshal(nonNilSignals(next.Signals))
	effects, _ := json.Marshal(nonNilEffects(next.Effects))
	ask := 0
	if next.Ask {
		ask = 1
	}
	var res sql.Result
	var err error
	if prev == nil {
		var pending int64
		if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM proposals WHERE state = 'pending'`).
			Scan(&pending); err != nil {
			return err
		}
		if limit > 0 && pending >= limit {
			return ErrProposalsFull
		}
		res, err = t.tx.ExecContext(t.ctx, `INSERT INTO proposals (id, work_id, task_id, session, source, project,
			title, signals, effects, ask, ask_reason, channel, question, state, answer, answered_by, answered_at,
			created_at, expires_at, asked_inline_at, version) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			next.ID, next.WorkID, emptyOrNull(next.TaskID), next.Session, next.Source, next.Project, next.Title,
			string(signals), string(effects), ask, next.AskReason, string(next.Channel), next.Question,
			string(next.State), emptyOrNull(string(next.Answer)), emptyOrNull(next.AnsweredBy),
			zeroOrUnix(next.AnsweredAt), next.CreatedAt.Unix(), next.ExpiresAt.Unix(), zeroOrUnix(next.AskedInlineAt))
	} else {
		res, err = t.tx.ExecContext(t.ctx, `UPDATE proposals SET state = ?, answer = ?, answered_by = ?,
			answered_at = ?, asked_inline_at = ?, version = version + 1 WHERE id = ? AND version = ?`,
			string(next.State), emptyOrNull(string(next.Answer)), emptyOrNull(next.AnsweredBy),
			zeroOrUnix(next.AnsweredAt), zeroOrUnix(next.AskedInlineAt), next.ID, prev.Version)
	}
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrConflict
	}
	t.wrote++
	return nil
}

func nonNilSignals(v []work.Signal) []work.Signal {
	if v == nil {
		return []work.Signal{}
	}
	return v
}

func nonNilEffects(v []work.Effect) []work.Effect {
	if v == nil {
		return []work.Effect{}
	}
	return v
}

// BrokerTask reads one broker task as the transaction sees it.
func (t *WorkTx) BrokerTask(id string) (BrokerRow, error) {
	return scanBroker(t.tx.QueryRowContext(t.ctx, `SELECT `+brokerColumns+` FROM broker_tasks WHERE id = ?`, id))
}

// TodosOf reads the to-dos the given tasks left, as the transaction sees
// them.
func (t *WorkTx) TodosOf(tasks []string) ([]work.Todo, error) {
	out := []work.Todo{}
	if len(tasks) == 0 {
		return out, nil
	}
	args := make([]any, len(tasks))
	for i, v := range tasks {
		args[i] = v
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+todoColumns+` FROM todos WHERE task_id IN (`+
		strings.TrimSuffix(strings.Repeat("?,", len(tasks)), ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanTodo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r.Todo)
	}
	return out, rows.Err()
}

// ProposalQuery is one page of proposals, newest first.
type ProposalQuery struct {
	// State narrows the page; empty is every state.
	State   work.ProposalState
	Project string
	Session string
	// AfterCreated and AfterID are the last row of the page before.
	AfterCreated int64
	AfterID      string
	Limit        int
}

// Proposals reads one page of proposals, newest first.
func (s *Store) Proposals(ctx context.Context, q ProposalQuery) ([]work.Proposal, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	query := `SELECT ` + proposalColumns + ` FROM proposals WHERE 1 = 1`
	args := []any{}
	if q.State != "" {
		query += ` AND state = ?`
		args = append(args, string(q.State))
	}
	if q.Project != "" {
		query += ` AND project = ?`
		args = append(args, q.Project)
	}
	if q.Session != "" {
		query += ` AND session = ?`
		args = append(args, q.Session)
	}
	if q.AfterID != "" {
		query += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
		args = append(args, q.AfterCreated, q.AfterCreated, q.AfterID)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, q.Limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.Proposal{}
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProposalRow reads one proposal.
func (s *Store) ProposalRow(ctx context.Context, id string) (work.Proposal, error) {
	if err := reading(); err != nil {
		return work.Proposal{}, err
	}
	return scanProposal(s.db.QueryRowContext(ctx, `SELECT `+proposalColumns+` FROM proposals WHERE id = ?`, id))
}

// ProposalTally is what /v1/diagnostics says about proposals, counted from
// the rows — durable, and the same numbers after a restart.
type ProposalTally struct {
	// AskTrue is the proposals the server let be asked in the conversation;
	// AskFalse the ones it put in the "to confirm" area.
	AskTrue  int64
	AskFalse int64
	// AskedInline is the proposals a session reported asking in the
	// conversation, and Unprompted those of them the server had said not to
	// ask: a briefing not followed.
	AskedInline int64
	Unprompted  int64
	// ByState is every proposal by its state.
	ByState map[work.ProposalState]int64
	// OldestPending is when the proposal waiting longest was made.
	OldestPending time.Time
}

// ProposalTally counts every proposal.
func (s *Store) ProposalTally(ctx context.Context) (ProposalTally, error) {
	if err := reading(); err != nil {
		return ProposalTally{}, err
	}
	out := ProposalTally{ByState: map[work.ProposalState]int64{}}
	var oldest sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(ask = 1), 0), COALESCE(SUM(ask = 0), 0),
		COALESCE(SUM(asked_inline_at IS NOT NULL), 0),
		COALESCE(SUM(asked_inline_at IS NOT NULL AND ask = 0), 0),
		(SELECT MIN(created_at) FROM proposals WHERE state = 'pending')
		FROM proposals`).Scan(&out.AskTrue, &out.AskFalse, &out.AskedInline, &out.Unprompted, &oldest); err != nil {
		return ProposalTally{}, err
	}
	if oldest.Valid {
		out.OldestPending = time.Unix(oldest.Int64, 0)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT state, COUNT(*) FROM proposals GROUP BY state`)
	if err != nil {
		return ProposalTally{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var n int64
		if err := rows.Scan(&state, &n); err != nil {
			return ProposalTally{}, err
		}
		out.ByState[work.ProposalState(state)] = n
	}
	return out, rows.Err()
}

// UnproposedLine is a named line of work the rules may propose: a work id an
// owed to-do carries, with no work item and no proposal yet, and one of its
// to-dos to speak for it.
type UnproposedLine struct {
	WorkID  string
	Task    string
	Owner   string
	Title   string
	Project string
}

// UnproposedLines reads every named line of work with an owed to-do, no work
// item and no proposal, at most limit. Its cost is the to-dos still owed.
func (s *Store) UnproposedLines(ctx context.Context, limit int) ([]UnproposedLine, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT t.work_id, MIN(t.task_id), MIN(t.owner_session), MIN(t.title),
		MIN(t.project) FROM todos t
		WHERE t.state IN ('open','handed_off') AND t.work_id IS NOT NULL AND t.task_id IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM work w WHERE w.id = t.work_id)
		  AND NOT EXISTS (SELECT 1 FROM proposals p WHERE p.work_id = t.work_id)
		GROUP BY t.work_id ORDER BY MIN(t.created_at), t.work_id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UnproposedLine{}
	for rows.Next() {
		var l UnproposedLine
		if err := rows.Scan(&l.WorkID, &l.Task, &l.Owner, &l.Title, &l.Project); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// DueProposals is the ids of pending proposals whose wait has passed.
func (s *Store) DueProposals(ctx context.Context, now time.Time) ([]string, error) {
	return s.ids(ctx, `SELECT id FROM proposals WHERE state = 'pending' AND expires_at <= ? ORDER BY expires_at, id`,
		now.Unix())
}

// ——— Decisions ———

const decisionColumns = `id, session, COALESCE(work_id, ''), COALESCE(task_id, ''), project, question, options,
  default_option, blocking, state, COALESCE(answer, ''), COALESCE(answered_by, ''), COALESCE(answered_at, 0),
  created_at, due_at, push, COALESCE(pushed_at, 0), version`

func scanDecision(sc scanner) (work.Decision, error) {
	var d work.Decision
	var options, state, push string
	var blocking int
	var answered, created, due, pushed int64
	err := sc.Scan(&d.ID, &d.Session, &d.WorkID, &d.TaskID, &d.Project, &d.Question, &options, &d.Default,
		&blocking, &state, &d.Answer, &d.AnsweredBy, &answered, &created, &due, &push, &pushed, &d.Version)
	if err == sql.ErrNoRows {
		return work.Decision{}, ErrNoDecision
	}
	if err != nil {
		return work.Decision{}, err
	}
	if err := json.Unmarshal([]byte(options), &d.Options); err != nil {
		return work.Decision{}, err
	}
	d.Blocking, d.State, d.Push = blocking == 1, work.DecisionState(state), work.PushState(push)
	d.AnsweredAt, d.CreatedAt, d.DueAt, d.PushedAt = unixOrZero(answered), time.Unix(created, 0),
		time.Unix(due, 0), unixOrZero(pushed)
	return d, nil
}

// Decision reads one decision as the transaction sees it.
func (t *WorkTx) Decision(id string) (work.Decision, error) {
	return scanDecision(t.tx.QueryRowContext(t.ctx, `SELECT `+decisionColumns+` FROM decisions WHERE id = ?`, id))
}

// PushesSince is how many decisions were pushed at or after since.
func (t *WorkTx) PushesSince(since time.Time) (int, error) {
	var n int
	err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM decisions WHERE push = 'sent' AND pushed_at >= ?`,
		since.Unix()).Scan(&n)
	return n, err
}

// PutDecision writes a decision. With prev nil it is new, and refused with
// ErrDecisionsFull at limit open; otherwise it is a compare-and-set.
func (t *WorkTx) PutDecision(next work.Decision, prev *work.Decision, limit int64) error {
	var res sql.Result
	var err error
	if prev == nil {
		var open int64
		if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM decisions WHERE state = 'open'`).
			Scan(&open); err != nil {
			return err
		}
		if limit > 0 && open >= limit {
			return ErrDecisionsFull
		}
		options, _ := json.Marshal(next.Options)
		blocking := 0
		if next.Blocking {
			blocking = 1
		}
		res, err = t.tx.ExecContext(t.ctx, `INSERT INTO decisions (id, session, work_id, task_id, project, question,
			options, default_option, blocking, state, answer, answered_by, answered_at, created_at, due_at, push,
			pushed_at, version) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			next.ID, next.Session, emptyOrNull(next.WorkID), emptyOrNull(next.TaskID), next.Project, next.Question,
			string(options), next.Default, blocking, string(next.State), emptyOrNull(next.Answer),
			emptyOrNull(next.AnsweredBy), zeroOrUnix(next.AnsweredAt), next.CreatedAt.Unix(), next.DueAt.Unix(),
			string(next.Push), zeroOrUnix(next.PushedAt))
	} else {
		res, err = t.tx.ExecContext(t.ctx, `UPDATE decisions SET state = ?, answer = ?, answered_by = ?,
			answered_at = ?, push = ?, pushed_at = ?, version = version + 1 WHERE id = ? AND version = ?`,
			string(next.State), emptyOrNull(next.Answer), emptyOrNull(next.AnsweredBy), zeroOrUnix(next.AnsweredAt),
			string(next.Push), zeroOrUnix(next.PushedAt), next.ID, prev.Version)
	}
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrConflict
	}
	t.wrote++
	return nil
}

// DecisionQuery is one page of decisions, newest first.
type DecisionQuery struct {
	State        work.DecisionState
	Session      string
	AfterCreated int64
	AfterID      string
	Limit        int
}

// Decisions reads one page of decisions, newest first.
func (s *Store) Decisions(ctx context.Context, q DecisionQuery) ([]work.Decision, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	query := `SELECT ` + decisionColumns + ` FROM decisions WHERE 1 = 1`
	args := []any{}
	if q.State != "" {
		query += ` AND state = ?`
		args = append(args, string(q.State))
	}
	if q.Session != "" {
		query += ` AND session = ?`
		args = append(args, q.Session)
	}
	if q.AfterID != "" {
		query += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
		args = append(args, q.AfterCreated, q.AfterCreated, q.AfterID)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, q.Limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.Decision{}
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DecisionRow reads one decision.
func (s *Store) DecisionRow(ctx context.Context, id string) (work.Decision, error) {
	if err := reading(); err != nil {
		return work.Decision{}, err
	}
	return scanDecision(s.db.QueryRowContext(ctx, `SELECT `+decisionColumns+` FROM decisions WHERE id = ?`, id))
}

// DecisionCounts is how many decisions are in each state.
func (s *Store) DecisionCounts(ctx context.Context) (map[work.DecisionState]int64, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT state, COUNT(*) FROM decisions GROUP BY state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[work.DecisionState]int64{}
	for rows.Next() {
		var state string
		var n int64
		if err := rows.Scan(&state, &n); err != nil {
			return nil, err
		}
		out[work.DecisionState(state)] = n
	}
	return out, rows.Err()
}

// DueDecisions is the ids of open decisions whose due has passed.
func (s *Store) DueDecisions(ctx context.Context, now time.Time) ([]string, error) {
	return s.ids(ctx, `SELECT id FROM decisions WHERE state = 'open' AND due_at <= ? ORDER BY due_at, id`, now.Unix())
}

// StalePushes is the ids of decisions whose push was recorded as pending
// before since and never given an outcome.
func (s *Store) StalePushes(ctx context.Context, since time.Time) ([]string, error) {
	return s.ids(ctx, `SELECT id FROM decisions WHERE push = 'pending' AND created_at < ? ORDER BY created_at, id`,
		since.Unix())
}

func (s *Store) ids(ctx context.Context, query string, args ...any) ([]string, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
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

// ——— Digests ———

// DigestRow is one digest as stored.
type DigestRow struct {
	Key       string
	Kind      work.DigestKind
	From      time.Time
	To        time.Time
	Body      json.RawMessage
	CreatedAt time.Time
}

// PutDigest writes a digest once: false when its key is already written.
// Past limit digests the oldest are let go in the same transaction.
func (t *WorkTx) PutDigest(d DigestRow, limit int64) (bool, error) {
	res, err := t.tx.ExecContext(t.ctx, `INSERT INTO digests (key, kind, from_at, to_at, body, created_at)
		VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(key) DO NOTHING`,
		d.Key, string(d.Kind), d.From.Unix(), d.To.Unix(), string(d.Body), d.CreatedAt.Unix())
	if err != nil {
		return false, err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return false, err
	}
	t.wrote++
	if limit > 0 {
		if _, err := t.tx.ExecContext(t.ctx, `DELETE FROM digests WHERE key IN (
			SELECT key FROM digests ORDER BY created_at DESC, key DESC LIMIT -1 OFFSET ?)`, limit); err != nil {
			return false, err
		}
	}
	return true, nil
}

// HasDigest says a digest's key is already written.
func (s *Store) HasDigest(ctx context.Context, key string) (bool, error) {
	if err := reading(); err != nil {
		return false, err
	}
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM digests WHERE key = ?`, key).Scan(&n)
	return n > 0, err
}

// Digests reads one page of digests of a kind (every kind when empty), newest
// first, whose window began before beforeFrom (every one when it is zero).
func (s *Store) Digests(ctx context.Context, kind work.DigestKind, beforeFrom int64, limit int) ([]DigestRow, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	query := `SELECT key, kind, from_at, to_at, body, created_at FROM digests WHERE 1 = 1`
	args := []any{}
	if kind != "" {
		query += ` AND kind = ?`
		args = append(args, string(kind))
	}
	if beforeFrom > 0 {
		query += ` AND from_at < ?`
		args = append(args, beforeFrom)
	}
	query += ` ORDER BY from_at DESC, key DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DigestRow{}
	for rows.Next() {
		var d DigestRow
		var kind, body string
		var from, to, created int64
		if err := rows.Scan(&d.Key, &kind, &from, &to, &body, &created); err != nil {
			return nil, err
		}
		d.Kind, d.From, d.To, d.CreatedAt = work.DigestKind(kind), time.Unix(from, 0), time.Unix(to, 0),
			time.Unix(created, 0)
		d.Body = json.RawMessage(body)
		out = append(out, d)
	}
	return out, rows.Err()
}

// DigestCount is how many digests are kept: the register's `work.digests`.
func (s *Store) DigestCount(ctx context.Context) (int64, error) {
	if err := reading(); err != nil {
		return 0, err
	}
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM digests`).Scan(&n)
	return n, err
}

// DigestMove is one move in a digest's window, with the item's title.
type DigestMove struct {
	WorkMove
	Title string
}

// MovesBetween reads the moves made in [from, to), oldest first, at most
// limit; the second answer is how many there were in all.
func (s *Store) MovesBetween(ctx context.Context, from, to time.Time, limit int) ([]DigestMove, int, error) {
	if err := reading(); err != nil {
		return nil, 0, err
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM moves WHERE at >= ? AND at < ?`,
		from.Unix(), to.Unix()).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.seq, m.work_id, m.from_s, m.to_s, m.state, m.trigger, m.actor,
		m.evidence, m.at, w.title FROM moves m JOIN work w ON w.id = m.work_id
		WHERE m.at >= ? AND m.at < ? ORDER BY m.seq LIMIT ?`, from.Unix(), to.Unix(), limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []DigestMove{}
	for rows.Next() {
		var m DigestMove
		var fromS, toS, state, evidence string
		var at int64
		if err := rows.Scan(&m.Seq, &m.WorkID, &fromS, &toS, &state, &m.Trigger, &m.Actor, &evidence, &at,
			&m.Title); err != nil {
			return nil, 0, err
		}
		m.From, m.To, m.State = work.Place(fromS), work.Place(toS), work.ItemState(state)
		m.Evidence, m.At = json.RawMessage(evidence), time.Unix(at, 0)
		out = append(out, m)
	}
	return out, total, rows.Err()
}

// ProposalsClosedBetween reads the proposals that expired, or were answered,
// in [from, to), at most limit.
func (s *Store) ProposalsClosedBetween(ctx context.Context, state work.ProposalState, from, to time.Time,
	limit int) ([]work.Proposal, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	column := "answered_at"
	if state == work.ProposalExpired {
		column = "expires_at"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+proposalColumns+` FROM proposals WHERE state = ? AND `+column+
		` >= ? AND `+column+` < ? ORDER BY `+column+`, id LIMIT ?`, string(state), from.Unix(), to.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.Proposal{}
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DecisionsClosedBetween reads the decisions in state closed in [from, to).
func (s *Store) DecisionsClosedBetween(ctx context.Context, state work.DecisionState, from, to time.Time,
	limit int) ([]work.Decision, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+decisionColumns+` FROM decisions WHERE state = ?
		AND answered_at >= ? AND answered_at < ? ORDER BY answered_at, id LIMIT ?`,
		string(state), from.Unix(), to.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []work.Decision{}
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// HandedOffTodos is how many to-dos are owed by a session that is gone and
// nobody has taken: §6's "N to-dos have no one".
func (s *Store) HandedOffTodos(ctx context.Context) (int64, error) {
	if err := reading(); err != nil {
		return 0, err
	}
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM todos WHERE state = 'handed_off'`).Scan(&n)
	return n, err
}

// StaleBacklog reads planned Backlog items nobody has looked at since
// before, oldest first, at most limit.
func (s *Store) StaleBacklog(ctx context.Context, before time.Time, limit int) ([]work.Item, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	return s.queryWork(ctx, `SELECT `+workColumns+workFrom+` WHERE k.state = 'planned'
		AND MAX(COALESCE(k.reviewed_at, 0), w.placed_at) < ? ORDER BY w.placed_at, w.id LIMIT ?`,
		before.Unix(), limit)
}
