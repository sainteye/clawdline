package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Request receipts: the one spelling of idempotency (D03, G15).
//
// `(scope, actor, key) → (digest, answer)`. A feature declares only its scope.
// The same key with the same request is the original answer replayed; the
// same key with a different request is refused; a key whose window has passed
// is refused as expired and **not run again**, because a resend that arrives
// a day late is still a resend. Receipts expire by time, never by count: a
// window that is full refuses the new request (429) rather than evicting a
// receipt somebody may still retry against — eviction by count is how a
// retry becomes a second effect (limits §3.2).
//
// Before this there were several spellings: the old skeleton's
// `receipts(command_id)` (W4 retires it with the rest of that path, D07), a
// messages route that checked the key was present and stored nothing, and
// start, resume and voice answers kept in memory for ten minutes, gone at the
// next restart.
//
// An expired receipt keeps its key and digest and loses its answer — a
// tombstone of a few dozen bytes, which is what lets a late resend be told
// "expired" rather than be taken for a new request.

const receiptsSchema = `
CREATE TABLE IF NOT EXISTS request_receipts (
  scope        TEXT    NOT NULL,
  actor        TEXT    NOT NULL,
  key          TEXT    NOT NULL,
  digest       TEXT    NOT NULL,
  state        TEXT    NOT NULL,
  status       INTEGER NOT NULL DEFAULT 0,
  body         BLOB,
  owner        TEXT    NOT NULL DEFAULT '',
  created_at   INTEGER NOT NULL,
  completed_at INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (scope, actor, key)
);
CREATE INDEX IF NOT EXISTS request_receipts_window ON request_receipts(scope, state, created_at);
`

// ReceiptLimit is how many receipts one scope may hold inside its window. It
// is the register's `store.receipts` row: at it, a new request in that scope
// is refused with a Retry-After until the oldest receipt ages out.
const ReceiptLimit = 4096

// ReceiptWindow is how long a receipt answers a resend, unless its scope says
// otherwise (D03: 24 hours by default).
const ReceiptWindow = 24 * time.Hour

// ReceiptKey names one request.
type ReceiptKey struct {
	Scope string
	Actor string
	Key   string
}

// ReceiptPolicy is one scope's window and limit.
type ReceiptPolicy struct {
	Window time.Duration
	Limit  int
}

func (p ReceiptPolicy) window() time.Duration {
	if p.Window > 0 {
		return p.Window
	}
	return ReceiptWindow
}

func (p ReceiptPolicy) limit() int {
	if p.Limit > 0 && p.Limit < ReceiptLimit {
		return p.Limit
	}
	return ReceiptLimit
}

// ReceiptOutcome is what a claim found.
type ReceiptOutcome string

const (
	// ReceiptNew: nobody has asked this before; this handle now owns
	// answering it and must Complete or Release it.
	ReceiptNew ReceiptOutcome = "new"
	// ReceiptReplay: this exact request was answered; Answer is that answer.
	ReceiptReplay ReceiptOutcome = "replay"
	// ReceiptMismatch: the key was used for a different request.
	ReceiptMismatch ReceiptOutcome = "mismatch"
	// ReceiptPending: the same request is being answered right now by a
	// holder that is still running.
	ReceiptPending ReceiptOutcome = "pending"
	// ReceiptExpired: the key's window has passed; nothing is run again.
	ReceiptExpired ReceiptOutcome = "expired"
	// ReceiptFull: the scope holds its limit inside the window.
	ReceiptFull ReceiptOutcome = "full"
	// ReceiptOrphaned: the request was being answered by a holder that is
	// provably gone, so whether it took effect is unknown. This handle now
	// owns the receipt and must complete it with an answer that says so; it
	// must not run the request.
	ReceiptOrphaned ReceiptOutcome = "orphaned"
)

// ReceiptClaim is the answer to ClaimReceipt.
type ReceiptClaim struct {
	Outcome    ReceiptOutcome
	Answer     ReceiptAnswer
	RetryAfter time.Duration
}

// ClaimReceipt asks whether a request has been seen, and reserves it for this
// handle when it has not — in one transaction, so two copies of one request
// arriving together cannot both be told "new".
func (s *Store) ClaimReceipt(ctx context.Context, k ReceiptKey, digest string, p ReceiptPolicy, now time.Time) (ReceiptClaim, error) {
	var claim ReceiptClaim
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		var stored, state, holder string
		var status int
		var body []byte
		var created int64
		err := tx.QueryRowContext(ctx,
			`SELECT digest, state, status, body, owner, created_at FROM request_receipts
			 WHERE scope = ? AND actor = ? AND key = ?`, k.Scope, k.Actor, k.Key).
			Scan(&stored, &state, &status, &body, &holder, &created)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			floor := now.Add(-p.window()).Unix()
			var live int
			var oldest sql.NullInt64
			if err := tx.QueryRowContext(ctx,
				`SELECT COUNT(*), MIN(created_at) FROM request_receipts
				 WHERE scope = ? AND state IN ('pending', 'complete') AND created_at >= ?`,
				k.Scope, floor).Scan(&live, &oldest); err != nil {
				return 0, err
			}
			if live >= p.limit() {
				claim.Outcome = ReceiptFull
				claim.RetryAfter = time.Second
				if oldest.Valid {
					if wait := time.Unix(oldest.Int64, 0).Add(p.window()).Sub(now); wait > claim.RetryAfter {
						claim.RetryAfter = wait
					}
				}
				return 0, nil
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO request_receipts (scope, actor, key, digest, state, owner, created_at)
				 VALUES (?, ?, ?, ?, 'pending', ?, ?)`,
				k.Scope, k.Actor, k.Key, digest, s.owner.String(), now.Unix()); err != nil {
				return 0, err
			}
			claim.Outcome = ReceiptNew
			return 1, nil
		case err != nil:
			return 0, err
		}
		if stored != digest {
			claim.Outcome = ReceiptMismatch
			return 0, nil
		}
		expired := time.Unix(created, 0).Add(p.window()).Before(now)
		switch state {
		case "expired":
			claim.Outcome = ReceiptExpired
			return 0, nil
		case "complete":
			if expired {
				claim.Outcome = ReceiptExpired
				return tombstone(ctx, tx, k)
			}
			claim.Outcome = ReceiptReplay
			claim.Answer = ReceiptAnswer{Status: status, Body: body}
			return 0, nil
		}
		// Pending. A holder that is still running is answering it; so is the
		// outbox, when the request's effect is recorded and not yet finished —
		// the next broker runs it and completes this receipt (outbox.go), and
		// calling it unknown here would race that and lose.
		var open int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM outbox WHERE receipt_scope = ? AND receipt_actor = ? AND receipt_key = ?
			   AND state IN ('pending', 'started')`, k.Scope, k.Actor, k.Key).Scan(&open); err != nil {
			return 0, err
		}
		if open > 0 || !s.ownerGone(holder) {
			claim.Outcome = ReceiptPending
			claim.RetryAfter = time.Second
			return 0, nil
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE request_receipts SET owner = ? WHERE scope = ? AND actor = ? AND key = ? AND owner = ?`,
			s.owner.String(), k.Scope, k.Actor, k.Key, holder); err != nil {
			return 0, err
		}
		claim.Outcome = ReceiptOrphaned
		return 1, nil
	})
	return claim, err
}

func tombstone(ctx context.Context, tx *sql.Tx, k ReceiptKey) (int64, error) {
	res, err := tx.ExecContext(ctx,
		`UPDATE request_receipts SET state = 'expired', status = 0, body = NULL
		 WHERE scope = ? AND actor = ? AND key = ? AND state = 'complete'`, k.Scope, k.Actor, k.Key)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// completeReceipt files the answer to a request this handle holds, inside a
// transaction somebody else owns — the one that made the change the answer
// reports (D03).
func (s *Store) completeReceipt(ctx context.Context, tx *sql.Tx, k ReceiptKey, a ReceiptAnswer) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE request_receipts SET state = 'complete', status = ?, body = ?, completed_at = ?
		 WHERE scope = ? AND actor = ? AND key = ? AND state = 'pending' AND owner = ?`,
		a.Status, a.Body, time.Now().Unix(), k.Scope, k.Actor, k.Key, s.owner.String())
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrConflict
	}
	return nil
}

// CompleteReceipt files the answer to a request this handle holds, on its
// own, for a request whose effect had no other state to change with it.
func (s *Store) CompleteReceipt(ctx context.Context, k ReceiptKey, a ReceiptAnswer) error {
	return s.write(ctx, func(tx *sql.Tx) (int64, error) {
		return 1, s.completeReceipt(ctx, tx, k, a)
	})
}

// ReleaseReceipt gives up a reservation this handle holds without filing an
// answer: the request did nothing — it was refused for a reason about this
// moment, or its caller left before it began — and a resend should be run.
func (s *Store) ReleaseReceipt(ctx context.Context, k ReceiptKey) error {
	return s.write(ctx, func(tx *sql.Tx) (int64, error) {
		res, err := tx.ExecContext(ctx,
			`DELETE FROM request_receipts WHERE scope = ? AND actor = ? AND key = ? AND state = 'pending' AND owner = ?`,
			k.Scope, k.Actor, k.Key, s.owner.String())
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	})
}

// ExpireReceipts turns every answered receipt older than the window into a
// tombstone — in one scope, or in every scope when scope is empty — and
// answers how many it turned.
func (s *Store) ExpireReceipts(ctx context.Context, scope string, p ReceiptPolicy, now time.Time) (int64, error) {
	var n int64
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		res, err := tx.ExecContext(ctx,
			`UPDATE request_receipts SET state = 'expired', status = 0, body = NULL
			 WHERE (? = '' OR scope = ?) AND state = 'complete' AND created_at < ?`,
			scope, scope, now.Add(-p.window()).Unix())
		if err != nil {
			return 0, err
		}
		n, err = res.RowsAffected()
		return n, err
	})
	return n, err
}

// ReceiptUse is one scope's receipts inside its window, and its tombstones.
type ReceiptUse struct {
	Scope      string
	Live       int
	Tombstones int
}

// ReceiptUses reads every scope's use, for the capacity row.
func (s *Store) ReceiptUses(ctx context.Context, window time.Duration, now time.Time) ([]ReceiptUse, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT scope,
		        SUM(CASE WHEN state IN ('pending', 'complete') AND created_at >= ? THEN 1 ELSE 0 END),
		        SUM(CASE WHEN state = 'expired' THEN 1 ELSE 0 END)
		 FROM request_receipts GROUP BY scope ORDER BY scope`, now.Add(-window).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReceiptUse{}
	for rows.Next() {
		var u ReceiptUse
		if err := rows.Scan(&u.Scope, &u.Live, &u.Tombstones); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
