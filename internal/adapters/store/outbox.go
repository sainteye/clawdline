package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// The outbox: side effects recorded as intent in the transaction that owes
// them, and run after it commits (D08, G14).
//
// A tab to close, a message to type, a checkout to make — none of those may
// happen inside a transaction, because a transaction is the write right and
// the world does not roll back. And none of them may happen before the fact
// that owes them is durable, because a crash in between leaves an effect
// nobody recorded (the first broker made a task's worktree before the task
// existed). So the fact and a row here are one commit, and the effect runs
// afterwards, by whoever owns the row.
//
// An effect's life is `pending` → `started` → `done` | `failed` | `unknown`.
// `started` is committed before the effect is attempted, and it is what makes
// "exactly once" something this table can answer rather than hope:
//
//   - a row still `pending` after its owner died was never attempted, and the
//     next owner runs it — once;
//   - a row `started` after its owner died may or may not have happened. An
//     effect that can tell (a checkout that exists, a session already gone) is
//     run again and finds out; one that cannot — bytes typed into somebody's
//     terminal — is recorded `unknown` and not repeated, because typing it
//     twice is the failure and unknown is not permission (DG-7).
//
// Ownership is a handle's name: its pid and a nonce minted at Open. Another
// owner's row is taken over only when that owner is provably gone — the system
// answers that there is no such process, or it is this very pid under an
// earlier nonce, which is a process that has since been replaced by this one.

const outboxSchema = `
CREATE TABLE IF NOT EXISTS outbox (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  kind          TEXT    NOT NULL,
  subject       TEXT    NOT NULL,
  payload       TEXT    NOT NULL DEFAULT '',
  state         TEXT    NOT NULL,
  owner         TEXT    NOT NULL,
  attempts      INTEGER NOT NULL DEFAULT 0,
  created_at    INTEGER NOT NULL,
  started_at    INTEGER NOT NULL DEFAULT 0,
  finished_at   INTEGER NOT NULL DEFAULT 0,
  outcome       TEXT    NOT NULL DEFAULT '',
  receipt_scope TEXT    NOT NULL DEFAULT '',
  receipt_actor TEXT    NOT NULL DEFAULT '',
  receipt_key   TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS outbox_open ON outbox(state, id);
`

// Effect states.
const (
	EffectPending = "pending"
	EffectStarted = "started"
	EffectDone    = "done"
	EffectFailed  = "failed"
	EffectUnknown = "unknown"
)

// Effect is one row of the outbox.
type Effect struct {
	ID         int64
	Kind       string
	Subject    string
	Payload    json.RawMessage
	State      string
	Owner      string
	Attempts   int
	CreatedAt  time.Time
	StartedAt  time.Time
	FinishedAt time.Time
	Outcome    string
	// Receipt is the request receipt this effect answers, when a request is
	// waiting on it: finishing the effect completes the receipt in the same
	// transaction (D03).
	Receipt *ReceiptKey
}

// owner is a handle's name on the effects and receipts it holds.
type owner struct {
	pid   int
	nonce string
}

func newOwner() owner {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return owner{pid: os.Getpid(), nonce: hex.EncodeToString(buf)}
}

func (o owner) String() string { return strconv.Itoa(o.pid) + ":" + o.nonce }

// Owner is this handle's name, as it is written on what it owns.
func (s *Store) Owner() string { return s.owner.String() }

// ownerGone reports whether the owner named is provably not running. Anything
// that cannot be proved — a name this code cannot parse, a process table that
// will not answer — is not gone.
func (s *Store) ownerGone(name string) bool {
	if name == s.owner.String() {
		return false
	}
	pidText, _, ok := strings.Cut(name, ":")
	if !ok {
		return false
	}
	pid, err := strconv.Atoi(pidText)
	if err != nil || pid <= 0 {
		return false
	}
	if pid == s.owner.pid {
		// This pid under another nonce: an earlier handle of this process, or
		// an earlier process the system has since given this pid to. Either
		// way it is not running any more. (A process holding two handles at
		// once would take over its own work; the daemon holds one.)
		return true
	}
	gone, known := processGone(pid)
	return known && gone
}

func (s *Store) insertEffect(ctx context.Context, tx *sql.Tx, e Effect) (int64, error) {
	var scope, actor, key string
	if e.Receipt != nil {
		scope, actor, key = e.Receipt.Scope, e.Receipt.Actor, e.Receipt.Key
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO outbox (kind, subject, payload, state, owner, created_at, receipt_scope, receipt_actor, receipt_key)
		 VALUES (?, ?, ?, 'pending', ?, ?, ?, ?, ?)`,
		e.Kind, e.Subject, string(e.Payload), s.owner.String(), time.Now().Unix(), scope, actor, key)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const effectColumns = `id, kind, subject, payload, state, owner, attempts, created_at, started_at, finished_at,
  outcome, receipt_scope, receipt_actor, receipt_key`

func scanEffect(sc scanner) (Effect, error) {
	var e Effect
	var payload, scope, actor, key string
	var created, started, finished int64
	if err := sc.Scan(&e.ID, &e.Kind, &e.Subject, &payload, &e.State, &e.Owner, &e.Attempts,
		&created, &started, &finished, &e.Outcome, &scope, &actor, &key); err != nil {
		return Effect{}, err
	}
	e.Payload = json.RawMessage(payload)
	e.CreatedAt, e.StartedAt, e.FinishedAt = timeOrZero(created), timeOrZero(started), timeOrZero(finished)
	if scope != "" {
		e.Receipt = &ReceiptKey{Scope: scope, Actor: actor, Key: key}
	}
	return e, nil
}

// ErrEffectTaken is an effect this handle does not own, or one that is no
// longer in the state its caller read — somebody else has it, or it is over.
var ErrEffectTaken = errors.New("that effect is not this handle's to run")

// StartEffect marks one of this handle's effects `started`, committed, before
// the caller attempts it. It answers the effect as it was just before —
// Attempts tells a caller that took the row over whether an earlier owner had
// already begun it.
func (s *Store) StartEffect(ctx context.Context, id int64) (Effect, error) {
	var before Effect
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		e, err := scanEffect(tx.QueryRowContext(ctx, `SELECT `+effectColumns+` FROM outbox WHERE id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrEffectTaken
		}
		if err != nil {
			return 0, err
		}
		if e.Owner != s.owner.String() || (e.State != EffectPending && e.State != EffectStarted) {
			return 0, ErrEffectTaken
		}
		before = e
		_, err = tx.ExecContext(ctx,
			`UPDATE outbox SET state = 'started', attempts = attempts + 1, started_at = ? WHERE id = ?`,
			time.Now().Unix(), id)
		return 1, err
	})
	return before, err
}

// ReceiptAnswer is the answer a request receipt is completed with.
type ReceiptAnswer struct {
	Status int
	Body   []byte
}

// FinishEffect records how one of this handle's started effects ended, with
// the events that say so and — when the effect answers a request — the
// request's receipt, all in one transaction.
func (s *Store) FinishEffect(ctx context.Context, id int64, state, outcome string, events []Event, answer *ReceiptAnswer) error {
	switch state {
	case EffectDone, EffectFailed, EffectUnknown:
	default:
		return fmt.Errorf("effect cannot finish as %q", state)
	}
	return s.write(ctx, func(tx *sql.Tx) (int64, error) {
		e, err := scanEffect(tx.QueryRowContext(ctx, `SELECT `+effectColumns+` FROM outbox WHERE id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrEffectTaken
		}
		if err != nil {
			return 0, err
		}
		if e.Owner != s.owner.String() || (e.State != EffectStarted && !(state == EffectUnknown && e.State == EffectPending)) {
			return 0, ErrEffectTaken
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE outbox SET state = ?, outcome = ?, finished_at = ? WHERE id = ?`,
			state, outcome, time.Now().Unix(), id); err != nil {
			return 0, err
		}
		changed := int64(1)
		if e.Receipt != nil && answer != nil {
			if err := s.completeReceipt(ctx, tx, *e.Receipt, *answer); err != nil {
				return 0, err
			}
			changed++
		}
		if err := insertEvents(ctx, tx, events); err != nil {
			return 0, err
		}
		return changed + int64(len(events)), nil
	})
}

// AdoptEffects takes over every unfinished effect whose owner is provably
// gone, and answers them — this handle's now — oldest first. Its own
// unfinished effects are not included: they belong to a caller in this
// process that is running them.
func (s *Store) AdoptEffects(ctx context.Context) ([]Effect, error) {
	var out []Effect
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		rows, err := tx.QueryContext(ctx,
			`SELECT `+effectColumns+` FROM outbox WHERE state IN ('pending', 'started') ORDER BY id ASC`)
		if err != nil {
			return 0, err
		}
		var open []Effect
		for rows.Next() {
			e, err := scanEffect(rows)
			if err != nil {
				rows.Close()
				return 0, err
			}
			open = append(open, e)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return 0, err
		}
		changed := int64(0)
		for _, e := range open {
			if !s.ownerGone(e.Owner) {
				continue
			}
			res, err := tx.ExecContext(ctx,
				`UPDATE outbox SET owner = ? WHERE id = ? AND owner = ?`, s.owner.String(), e.ID, e.Owner)
			if err != nil {
				return 0, err
			}
			if n, _ := res.RowsAffected(); n == 1 {
				// The request the effect answers moves with it: finishing the
				// effect completes that receipt, and only its holder may.
				if e.Receipt != nil {
					if _, err := tx.ExecContext(ctx,
						`UPDATE request_receipts SET owner = ? WHERE scope = ? AND actor = ? AND key = ? AND state = 'pending'`,
						s.owner.String(), e.Receipt.Scope, e.Receipt.Actor, e.Receipt.Key); err != nil {
						return 0, err
					}
				}
				e.Owner = s.owner.String()
				out = append(out, e)
				changed++
			}
		}
		return changed, nil
	})
	return out, err
}

// OutboxCounts is the outbox's shape at one moment, for /v1/diagnostics.
type OutboxCounts struct {
	Pending, Started, Done, Failed, Unknown int
	OldestOpen                              time.Time
}

// OutboxCounts reads the outbox's totals.
func (s *Store) OutboxCounts(ctx context.Context) (OutboxCounts, error) {
	var out OutboxCounts
	if err := reading(); err != nil {
		return out, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT state, COUNT(*) FROM outbox GROUP BY state`)
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
		case EffectPending:
			out.Pending = n
		case EffectStarted:
			out.Started = n
		case EffectDone:
			out.Done = n
		case EffectFailed:
			out.Failed = n
		case EffectUnknown:
			out.Unknown = n
		}
	}
	rows.Close()
	var oldest sql.NullInt64
	if err := s.db.QueryRowContext(ctx,
		`SELECT MIN(created_at) FROM outbox WHERE state IN ('pending', 'started')`).Scan(&oldest); err != nil {
		return out, err
	}
	if oldest.Valid {
		out.OldestOpen = timeOrZero(oldest.Int64)
	}
	return out, nil
}

// Effects reads every effect of one kind about one subject, oldest first —
// for the tests and the diagnostics that ask what became of one intent.
func (s *Store) Effects(ctx context.Context, kind, subject string) ([]Effect, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+effectColumns+` FROM outbox WHERE kind = ? AND subject = ? ORDER BY id ASC`, kind, subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Effect{}
	for rows.Next() {
		e, err := scanEffect(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// OwnerGone reports whether the store handle named — a task's dispatcher, an
// effect's owner — is provably not running any more.
func (s *Store) OwnerGone(name string) bool { return s.ownerGone(name) }

// RecordIntent records events and the effects they owe, in one transaction,
// for an intent that belongs to no task row — a message one session sends
// another. The effects come back with their ids, this handle's to run.
func (s *Store) RecordIntent(ctx context.Context, events []Event, effects []Effect) ([]int64, error) {
	var ids []int64
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
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
		return int64(len(events) + len(effects)), nil
	})
	return ids, err
}
