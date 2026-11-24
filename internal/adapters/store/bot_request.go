package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	br "github.com/sainteye/clawdline/internal/domain/botrequest"
)

// The Bot request card's tables (docs/bot-request-cards.md §4). They sit
// beside the Board's tables and are never joined into Board counts.
const botRequestSchema = `
CREATE TABLE IF NOT EXISTS bot_request_cards (
  id            TEXT PRIMARY KEY,
  project_id    TEXT NOT NULL,
  project_path  TEXT NOT NULL,
  kind          TEXT NOT NULL DEFAULT '',
  title         TEXT NOT NULL,
  description   TEXT NOT NULL DEFAULT '',
  authorities   TEXT NOT NULL,
  created_by    TEXT NOT NULL,
  baton_role    TEXT NOT NULL CHECK (baton_role IN ('maker','reviewer','integrator')),
  baton_bot     TEXT NOT NULL,
  chain         TEXT NOT NULL,
  board_item_id TEXT NOT NULL DEFAULT '',
  state         TEXT NOT NULL CHECK (state IN ('open','done','cancelled','merged')),
  waiting       TEXT NOT NULL DEFAULT '',
  merged_into   TEXT NOT NULL DEFAULT '',
  split_from    TEXT NOT NULL DEFAULT '',
  cycle         INTEGER NOT NULL DEFAULT 1,
  version       INTEGER NOT NULL DEFAULT 1,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  closed_at     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS bot_request_cards_state ON bot_request_cards(state, updated_at, id);
CREATE TABLE IF NOT EXISTS bot_request_checklist (
  card_id     TEXT NOT NULL REFERENCES bot_request_cards(id) ON DELETE CASCADE,
  key         TEXT NOT NULL,
  label       TEXT NOT NULL,
  state       TEXT NOT NULL CHECK (state IN ('open','done','not_needed')),
  note        TEXT NOT NULL DEFAULT '',
  reported_by TEXT NOT NULL DEFAULT '',
  reported_at INTEGER NOT NULL DEFAULT 0,
  position    INTEGER NOT NULL,
  PRIMARY KEY (card_id, key)
);
CREATE TABLE IF NOT EXISTS bot_request_verdicts (
  id           TEXT PRIMARY KEY,
  card_id      TEXT NOT NULL REFERENCES bot_request_cards(id) ON DELETE CASCADE,
  commit_id    TEXT NOT NULL DEFAULT '',
  verdict      TEXT NOT NULL CHECK (verdict IN ('pass','conditional_pass','reject')),
  text         TEXT NOT NULL DEFAULT '',
  reviewer_bot TEXT NOT NULL,
  at           INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS bot_request_verdicts_card ON bot_request_verdicts(card_id, at, id);
CREATE TABLE IF NOT EXISTS bot_request_events (
  id      INTEGER PRIMARY KEY AUTOINCREMENT,
  card_id TEXT NOT NULL REFERENCES bot_request_cards(id) ON DELETE CASCADE,
  kind    TEXT NOT NULL,
  actor   TEXT NOT NULL,
  detail  TEXT NOT NULL DEFAULT '',
  version INTEGER NOT NULL,
  at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS bot_request_events_card ON bot_request_events(card_id, id);
CREATE TABLE IF NOT EXISTS bot_request_receipts (
  actor   TEXT NOT NULL,
  key     TEXT NOT NULL,
  card_id TEXT NOT NULL,
  at      INTEGER NOT NULL,
  PRIMARY KEY (actor, key)
);
`

// Outbox effect kinds a card write queues in its own transaction. The
// subject is the effect's key, so one completion or one handoff has one row.
const (
	EffectBotRequestDone    = "bot_request.done"
	EffectBotRequestHandoff = "bot_request.handoff"
	// EffectBotRequestDoneNotice is the asking Bots' half of a completion.
	// It is its own row because a Bot notice can be retried and the person's
	// push cannot: one interrupted push must not resend the other.
	EffectBotRequestDoneNotice = "bot_request.done.notice"
)

// botRequestReceiptAge is how long a write's receipt answers a retry.
const botRequestReceiptAge = 24 * time.Hour

// keepBotRequestReceipt records a write's receipt inside its transaction.
// Expired receipts go first; when the window is still full the oldest live
// ones expire early to make room (botrequest.receipts, AtLimit expire). A
// full window never refuses the write itself: the cost is that a retry older
// than the last MaxReceipts keyed writes is no longer recognised as one, and
// its expected_version check is what refuses it then.
func keepBotRequestReceipt(ctx context.Context, tx *sql.Tx, actor, key, cardID string, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM bot_request_receipts WHERE at <= ?`, now.Add(-botRequestReceiptAge).UnixMilli()); err != nil {
		return err
	}
	var receipts int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM bot_request_receipts`).Scan(&receipts); err != nil {
		return err
	}
	if over := receipts - br.MaxReceipts + 1; over > 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM bot_request_receipts WHERE rowid IN
			(SELECT rowid FROM bot_request_receipts ORDER BY at, rowid LIMIT ?)`, over); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO bot_request_receipts (actor, key, card_id, at) VALUES (?, ?, ?, ?)`,
		actor, key, cardID, now.UnixMilli())
	return err
}

var ErrNoBotRequest = errors.New("no such bot request card")

func openBotRequest(db *sql.DB) error {
	_, err := db.Exec(botRequestSchema)
	return err
}

// BotRequestDonePayload is the payload of a bot_request.done effect: tell the
// person, and each asking Bot but the one that marked it done (§5).
type BotRequestDonePayload struct {
	CardID string   `json:"card_id"`
	Cycle  int64    `json:"cycle"`
	Title  string   `json:"title"`
	Person bool     `json:"person"`
	Bots   []string `json:"bots"`
	DoneBy string   `json:"done_by"`
}

// BotRequestHandoffPayload wakes the new holder with a turn whose authority is
// the card's (§7.2).
type BotRequestHandoffPayload struct {
	CardID  string `json:"card_id"`
	Version int64  `json:"version"`
	ToBot   string `json:"to_bot"`
	Role    string `json:"role"`
	Note    string `json:"note,omitempty"`
	From    string `json:"from"`
}

// BotRequestEvent is one row of a card's history.
type BotRequestEvent struct {
	ID      int64     `json:"id"`
	Kind    string    `json:"kind"`
	Actor   string    `json:"actor"`
	Detail  string    `json:"detail,omitempty"`
	Version int64     `json:"version"`
	At      time.Time `json:"at"`
}

// CreateBotRequest stores a new card, or, when a card with its id already
// exists (the same person message through another Bot, or a retry), adds the
// creating Bot to that card's chain and answers it unchanged otherwise.
func (s *Store) CreateBotRequest(ctx context.Context, c br.Card) (br.Card, bool, error) {
	var out br.Card
	created := false
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		existing, err := loadBotRequest(ctx, tx, c.ID)
		if err == nil {
			out = existing
			if !existing.JoinChain(c.CreatedBy) {
				return 0, nil
			}
			existing.Version++
			existing.UpdatedAt = c.CreatedAt
			if err := saveBotRequest(ctx, tx, existing, nil); err != nil {
				return 0, err
			}
			if err := appendBotRequestEvent(ctx, tx, existing, br.Event{Kind: br.EventChainJoined, Actor: "bot:" + c.CreatedBy, At: c.CreatedAt}); err != nil {
				return 0, err
			}
			out = existing
			return 1, nil
		}
		if !errors.Is(err, ErrNoBotRequest) {
			return 0, err
		}
		if err := insertBotRequest(ctx, tx, c); err != nil {
			return 0, err
		}
		if err := saveBotRequest(ctx, tx, c, nil); err != nil {
			return 0, err
		}
		if err := appendBotRequestEvent(ctx, tx, c, br.Event{Kind: br.EventCreated, Actor: "bot:" + c.CreatedBy, At: c.CreatedAt}); err != nil {
			return 0, err
		}
		out, created = c, true
		return 1, nil
	})
	return out, created, err
}

// BotRequestWrite is one write to one card. Apply changes the card in
// memory and answers the events it makes; no events means nothing changed.
type BotRequestWrite struct {
	CardID string
	// Expected is the version the writer read; 0 skips the check, which
	// only the daemon's own follow-ups (recording a delivery) use.
	Expected int64
	Actor    br.Actor
	// Key is the writer's idempotency key; empty for the daemon's own writes.
	Key   string
	Now   time.Time
	Apply func(*br.Card) ([]br.Event, error)
}

// BotRequestResult is what one write did.
type BotRequestResult struct {
	Card    br.Card `json:"card"`
	Changed bool    `json:"changed"`
	// Replayed: the idempotency key had been used already and nothing ran.
	Replayed bool `json:"replayed,omitempty"`
}

// UpdateBotRequest runs one write as one transaction: receipt, card, actor
// and version check, the next state, its events and any outbox effect.
func (s *Store) UpdateBotRequest(ctx context.Context, w BotRequestWrite) (BotRequestResult, error) {
	var res BotRequestResult
	if w.Now.IsZero() {
		w.Now = time.Now()
	}
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		actor := w.Actor.Name()
		if w.Key != "" {
			var card string
			err := tx.QueryRowContext(ctx, `SELECT card_id FROM bot_request_receipts WHERE actor = ? AND key = ? AND at > ?`,
				actor, w.Key, w.Now.Add(-botRequestReceiptAge).UnixMilli()).Scan(&card)
			switch {
			case err == nil:
				if card != w.CardID {
					return 0, &br.Refusal{Code: br.CodeInvalid, Field: "idempotency_key"}
				}
				c, err := loadBotRequest(ctx, tx, w.CardID)
				res = BotRequestResult{Card: c, Replayed: true}
				return 0, err
			case !errors.Is(err, sql.ErrNoRows):
				return 0, err
			}
		}
		prev, err := loadBotRequest(ctx, tx, w.CardID)
		if err != nil {
			return 0, err
		}
		if w.Expected != 0 && w.Expected != prev.Version {
			res.Card = prev
			return 0, &br.Refusal{Code: br.CodeVersionConflict, Field: "expected_version", Version: prev.Version}
		}
		next := prev
		next.Checklist = append([]br.Entry(nil), prev.Checklist...)
		next.Verdicts = append([]br.Verdict(nil), prev.Verdicts...)
		events, err := w.Apply(&next)
		if err != nil {
			res.Card = prev
			return 0, err
		}
		if len(events) == 0 {
			res.Card = prev
			return 0, nil
		}
		next.Version = prev.Version + 1
		next.UpdatedAt = w.Now
		if err := saveBotRequest(ctx, tx, next, prev.Verdicts); err != nil {
			return 0, err
		}
		for _, e := range events {
			if e.Actor == "" {
				e.Actor = actor
			}
			if e.At.IsZero() {
				e.At = w.Now
			}
			if err := appendBotRequestEvent(ctx, tx, next, e); err != nil {
				return 0, err
			}
		}
		if prev.State != br.StateDone && next.State == br.StateDone {
			if err := s.queueBotRequestDone(ctx, tx, next, w.Actor); err != nil {
				return 0, err
			}
		}
		if next.Baton != prev.Baton {
			if err := s.queueBotRequestHandoff(ctx, tx, next, actor, events); err != nil {
				return 0, err
			}
		}
		if w.Key != "" {
			if err := keepBotRequestReceipt(ctx, tx, actor, w.Key, w.CardID, w.Now); err != nil {
				return 0, err
			}
		}
		res = BotRequestResult{Card: next, Changed: true}
		return 1, nil
	})
	return res, err
}

// queueBotRequestDone writes the completion's one effect. Its subject is the
// cycle's key, and a key that already has a row is not written again, so a
// retry, a restart or a second path to done delivers once per cycle.
func (s *Store) queueBotRequestDone(ctx context.Context, tx *sql.Tx, c br.Card, by br.Actor) error {
	payload, err := json.Marshal(BotRequestDonePayload{
		CardID: c.ID, Cycle: c.Cycle, Title: c.Title, Person: true, Bots: c.NotifyTargets(by), DoneBy: by.Name(),
	})
	if err != nil {
		return err
	}
	if err := s.queueOnce(ctx, tx, EffectBotRequestDone, br.NotifyKey(c.ID, c.Cycle), payload); err != nil {
		return err
	}
	if len(c.NotifyTargets(by)) == 0 {
		return nil
	}
	return s.queueOnce(ctx, tx, EffectBotRequestDoneNotice, br.NotifyKey(c.ID, c.Cycle), payload)
}

func (s *Store) queueBotRequestHandoff(ctx context.Context, tx *sql.Tx, c br.Card, from string, events []br.Event) error {
	note := ""
	for _, e := range events {
		if e.Kind == br.EventHanded {
			note = e.Detail
		}
	}
	payload, err := json.Marshal(BotRequestHandoffPayload{
		CardID: c.ID, Version: c.Version, ToBot: c.Baton.Bot, Role: c.Baton.Role, Note: note, From: from,
	})
	if err != nil {
		return err
	}
	return s.queueOnce(ctx, tx, EffectBotRequestHandoff, "bot-request-handoff-"+c.ID+"-"+itoaStore(c.Version), payload)
}

func (s *Store) queueOnce(ctx context.Context, tx *sql.Tx, kind, subject string, payload []byte) error {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE kind = ? AND subject = ?`, kind, subject).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := s.insertEffect(ctx, tx, Effect{Kind: kind, Subject: subject, Payload: payload})
	return err
}

// BotRequestCheckTarget names one card of a batch tick and the version read.
type BotRequestCheckTarget struct {
	CardID   string `json:"card_id"`
	Expected int64  `json:"expected_version"`
}

// BotRequestCheckResult is one card's answer in a batch tick.
type BotRequestCheckResult struct {
	CardID  string `json:"card_id"`
	Outcome string `json:"outcome"` // ok, unchanged, refused
	Code    string `json:"code,omitempty"`
	Field   string `json:"field,omitempty"`
	Version int64  `json:"version,omitempty"`
}

// CheckBotRequests applies the same marks to several cards (§5, batch tick).
// Each card is its own transaction and answers on its own; the idempotency
// key is scoped per card, so a retried batch writes nothing.
func (s *Store) CheckBotRequests(ctx context.Context, a br.Actor, key string, targets []BotRequestCheckTarget, marks []br.Mark, now time.Time) ([]BotRequestCheckResult, error) {
	if len(targets) == 0 {
		return nil, &br.Refusal{Code: br.CodeInvalid, Field: "cards"}
	}
	out := make([]BotRequestCheckResult, 0, len(targets))
	for _, t := range targets {
		k := ""
		if key != "" {
			k = key + "/" + t.CardID
		}
		res, err := s.UpdateBotRequest(ctx, BotRequestWrite{CardID: t.CardID, Expected: t.Expected, Actor: a, Key: k, Now: now,
			Apply: func(c *br.Card) ([]br.Event, error) {
				changed, err := c.Check(a, marks, now)
				if err != nil || !changed {
					return nil, err
				}
				return []br.Event{{Kind: br.EventChecklistMark, Detail: markDetail(marks)}}, nil
			}})
		r := BotRequestCheckResult{CardID: t.CardID, Version: res.Card.Version}
		switch {
		case err != nil:
			var ref *br.Refusal
			if errors.As(err, &ref) {
				r.Outcome, r.Code, r.Field = "refused", ref.Code, ref.Field
				if ref.Version != 0 {
					r.Version = ref.Version
				}
			} else if errors.Is(err, ErrNoBotRequest) {
				r.Outcome, r.Code = "refused", br.CodeNotFound
			} else {
				return out, err
			}
		case res.Changed:
			r.Outcome = "ok"
		default:
			r.Outcome = "unchanged"
		}
		out = append(out, r)
	}
	return out, nil
}

func markDetail(marks []br.Mark) string {
	parts := make([]string, 0, len(marks))
	for _, m := range marks {
		parts = append(parts, br.EntryKey(m.Label)+"="+m.State)
	}
	return strings.Join(parts, ",")
}

// BotRequestCard reads one card with its checklist and verdicts.
func (s *Store) BotRequestCard(ctx context.Context, id string) (br.Card, error) {
	if err := reading(); err != nil {
		return br.Card{}, err
	}
	tx, err := s.rd.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return br.Card{}, err
	}
	defer tx.Rollback()
	return loadBotRequest(ctx, tx, id)
}

// BotRequestEvents reads a card's history, oldest first.
func (s *Store) BotRequestEvents(ctx context.Context, id string) ([]BotRequestEvent, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.rd.QueryContext(ctx, `SELECT id, kind, actor, detail, version, at FROM bot_request_events WHERE card_id = ? ORDER BY id ASC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BotRequestEvent{}
	for rows.Next() {
		var e BotRequestEvent
		var at int64
		if err := rows.Scan(&e.ID, &e.Kind, &e.Actor, &e.Detail, &e.Version, &at); err != nil {
			return nil, err
		}
		e.At = time.UnixMilli(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// BotRequestFilter narrows a list. Empty fields do not filter.
type BotRequestFilter struct {
	Bot       string
	ProjectID string
	State     string
}

// BotRequestCards lists cards, most recently updated first, at most limit
// (bounded by botrequest.page_rows). The cursor is the last row's id; more
// answers whether another page exists.
func (s *Store) BotRequestCards(ctx context.Context, f BotRequestFilter, cursor string, limit int) ([]br.Card, string, error) {
	if err := reading(); err != nil {
		return nil, "", err
	}
	if limit <= 0 || limit > br.MaxPageRows {
		limit = br.MaxPageRows
	}
	q := `SELECT id FROM bot_request_cards WHERE 1=1`
	var args []any
	if f.State != "" {
		q += ` AND state = ?`
		args = append(args, f.State)
	}
	if f.ProjectID != "" {
		q += ` AND project_id = ?`
		args = append(args, f.ProjectID)
	}
	if f.Bot != "" {
		q += ` AND (baton_bot = ? OR EXISTS (SELECT 1 FROM json_each(chain) WHERE value = ?))`
		args = append(args, f.Bot, f.Bot)
	}
	if cursor != "" {
		var at int64
		if err := s.rd.QueryRowContext(ctx, `SELECT updated_at FROM bot_request_cards WHERE id = ?`, cursor).Scan(&at); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, "", &br.Refusal{Code: br.CodeInvalid, Field: "cursor"}
			}
			return nil, "", err
		}
		q += ` AND (updated_at < ? OR (updated_at = ? AND id < ?))`
		args = append(args, at, at, cursor)
	}
	q += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, limit+1)
	tx, err := s.rd.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, "", err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, "", err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(ids) > limit {
		ids = ids[:limit]
		next = ids[limit-1]
	}
	out := make([]br.Card, 0, len(ids))
	for _, id := range ids {
		c, err := loadBotRequest(ctx, tx, id)
		if err != nil {
			return nil, "", err
		}
		out = append(out, c)
	}
	return out, next, nil
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func loadBotRequest(ctx context.Context, q queryer, id string) (br.Card, error) {
	var c br.Card
	var auth, chain string
	var created, updated, closed int64
	err := q.QueryRowContext(ctx, `SELECT id, project_id, project_path, kind, title, description, authorities, created_by,
	  baton_role, baton_bot, chain, board_item_id, state, waiting, merged_into, split_from, cycle, version,
	  created_at, updated_at, closed_at FROM bot_request_cards WHERE id = ?`, id).Scan(
		&c.ID, &c.ProjectID, &c.ProjectPath, &c.Kind, &c.Title, &c.Description, &auth, &c.CreatedBy,
		&c.Baton.Role, &c.Baton.Bot, &chain, &c.BoardItemID, &c.State, &c.Waiting, &c.MergedInto, &c.SplitFrom,
		&c.Cycle, &c.Version, &created, &updated, &closed)
	if errors.Is(err, sql.ErrNoRows) {
		return br.Card{}, ErrNoBotRequest
	}
	if err != nil {
		return br.Card{}, err
	}
	if err := json.Unmarshal([]byte(auth), &c.Authorities); err != nil {
		return br.Card{}, err
	}
	if err := json.Unmarshal([]byte(chain), &c.Chain); err != nil {
		return br.Card{}, err
	}
	c.CreatedAt, c.UpdatedAt = time.UnixMilli(created), time.UnixMilli(updated)
	if closed != 0 {
		c.ClosedAt = time.UnixMilli(closed)
	}
	rows, err := q.QueryContext(ctx, `SELECT key, label, state, note, reported_by, reported_at FROM bot_request_checklist WHERE card_id = ? ORDER BY position`, id)
	if err != nil {
		return br.Card{}, err
	}
	for rows.Next() {
		var e br.Entry
		var at int64
		if err := rows.Scan(&e.Key, &e.Label, &e.State, &e.Note, &e.ReportedBy, &at); err != nil {
			rows.Close()
			return br.Card{}, err
		}
		if at != 0 {
			e.ReportedAt = time.UnixMilli(at)
		}
		c.Checklist = append(c.Checklist, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return br.Card{}, err
	}
	rows, err = q.QueryContext(ctx, `SELECT id, commit_id, verdict, text, reviewer_bot, at FROM bot_request_verdicts WHERE card_id = ? ORDER BY at, id`, id)
	if err != nil {
		return br.Card{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var v br.Verdict
		var at int64
		if err := rows.Scan(&v.ID, &v.Commit, &v.Verdict, &v.Text, &v.ReviewerBot, &at); err != nil {
			return br.Card{}, err
		}
		v.At = time.UnixMilli(at)
		c.Verdicts = append(c.Verdicts, v)
	}
	return c, rows.Err()
}

func insertBotRequest(ctx context.Context, tx *sql.Tx, c br.Card) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO bot_request_cards (id, project_id, project_path, kind, title, description,
	  authorities, created_by, baton_role, baton_bot, chain, board_item_id, state, cycle, version, created_at, updated_at)
	  VALUES (?, ?, ?, ?, ?, ?, '[]', ?, ?, ?, '[]', ?, ?, ?, ?, ?, ?)`,
		c.ID, c.ProjectID, c.ProjectPath, c.Kind, c.Title, c.Description, c.CreatedBy, c.Baton.Role, c.Baton.Bot,
		c.BoardItemID, c.State, c.Cycle, c.Version, c.CreatedAt.UnixMilli(), c.UpdatedAt.UnixMilli())
	return err
}

// saveBotRequest writes a card's row, its whole checklist, and the verdicts
// it has that prev did not. Verdicts are never edited.
func saveBotRequest(ctx context.Context, tx *sql.Tx, c br.Card, prev []br.Verdict) error {
	auth, err := json.Marshal(c.Authorities)
	if err != nil {
		return err
	}
	chain, err := json.Marshal(c.Chain)
	if err != nil {
		return err
	}
	var closed int64
	if !c.ClosedAt.IsZero() {
		closed = c.ClosedAt.UnixMilli()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bot_request_cards SET kind = ?, title = ?, description = ?, authorities = ?,
	  baton_role = ?, baton_bot = ?, chain = ?, board_item_id = ?, state = ?, waiting = ?, merged_into = ?, split_from = ?,
	  cycle = ?, version = ?, updated_at = ?, closed_at = ? WHERE id = ?`,
		c.Kind, c.Title, c.Description, string(auth), c.Baton.Role, c.Baton.Bot, string(chain), c.BoardItemID,
		c.State, c.Waiting, c.MergedInto, c.SplitFrom, c.Cycle, c.Version, c.UpdatedAt.UnixMilli(), closed, c.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM bot_request_checklist WHERE card_id = ?`, c.ID); err != nil {
		return err
	}
	for i, e := range c.Checklist {
		var at int64
		if !e.ReportedAt.IsZero() {
			at = e.ReportedAt.UnixMilli()
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO bot_request_checklist (card_id, key, label, state, note, reported_by, reported_at, position)
		  VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, c.ID, e.Key, e.Label, e.State, e.Note, e.ReportedBy, at, i); err != nil {
			return err
		}
	}
	for _, v := range c.Verdicts[len(prev):] {
		if _, err := tx.ExecContext(ctx, `INSERT INTO bot_request_verdicts (id, card_id, commit_id, verdict, text, reviewer_bot, at)
		  VALUES (?, ?, ?, ?, ?, ?, ?)`, v.ID, c.ID, v.Commit, v.Verdict, v.Text, v.ReviewerBot, v.At.UnixMilli()); err != nil {
			return err
		}
	}
	return nil
}

func appendBotRequestEvent(ctx context.Context, tx *sql.Tx, c br.Card, e br.Event) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO bot_request_events (card_id, kind, actor, detail, version, at) VALUES (?, ?, ?, ?, ?, ?)`,
		c.ID, e.Kind, e.Actor, e.Detail, c.Version, e.At.UnixMilli())
	return err
}

func itoaStore(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// JoinBotRequestChain adds bot to an open card's chain because a Bot in the
// chain messaged it with the card (docs/bot-request-cards.md §8.2). A Bot
// already in the chain changes nothing, so a retried message_bot writes once.
func (s *Store) JoinBotRequestChain(ctx context.Context, cardID, by, bot string, now time.Time) (br.Card, error) {
	var out br.Card
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		c, err := loadBotRequest(ctx, tx, cardID)
		if err != nil {
			return 0, err
		}
		out = c
		if c.State != br.StateOpen {
			return 0, &br.Refusal{Code: br.CodeClosed, Field: "state", Version: c.Version}
		}
		if !c.InChain(by) {
			return 0, &br.Refusal{Code: br.CodeForbidden, Field: "chain", Version: c.Version}
		}
		if !c.JoinChain(bot) {
			return 0, nil
		}
		c.Version++
		c.UpdatedAt = now
		if err := saveBotRequest(ctx, tx, c, c.Verdicts); err != nil {
			return 0, err
		}
		if err := appendBotRequestEvent(ctx, tx, c, br.Event{Kind: br.EventChainJoined, Actor: "bot:" + by, Detail: bot, At: now}); err != nil {
			return 0, err
		}
		out = c
		return 1, nil
	})
	return out, err
}

// BotRequestOccupancy is what the capacity rows of botrequest.* read: the
// stored cards, the receipts still inside their age, and the longest
// merged_into walk from any merged card to the card that absorbed it.
type BotRequestOccupancy struct {
	Cards        int64
	LiveReceipts int64
	MergeHops    int64
}

// BotRequestOccupancy reads the store's current values. The merge walk stops
// one hop past botrequest.merge_hops, so a cycle cannot make it run forever.
func (s *Store) BotRequestOccupancy(ctx context.Context, now time.Time) (BotRequestOccupancy, error) {
	var o BotRequestOccupancy
	if err := reading(); err != nil {
		return o, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bot_request_cards`).Scan(&o.Cards); err != nil {
		return o, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bot_request_receipts WHERE at > ?`,
		now.Add(-botRequestReceiptAge).UnixMilli()).Scan(&o.LiveReceipts); err != nil {
		return o, err
	}
	err := s.db.QueryRowContext(ctx, `WITH RECURSIVE walk(next, depth) AS (
		SELECT merged_into, 1 FROM bot_request_cards WHERE state = 'merged' AND merged_into <> ''
		UNION ALL
		SELECT c.merged_into, w.depth + 1 FROM walk w JOIN bot_request_cards c ON c.id = w.next
		WHERE c.state = 'merged' AND c.merged_into <> '' AND w.depth <= ?
	) SELECT coalesce(max(depth), 0) FROM walk`, br.MaxMergeHops).Scan(&o.MergeHops)
	return o, err
}
