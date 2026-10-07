package store

import (
	"context"
	"database/sql"
	"time"
)

// PauseRow is the receiver's durable coordination receipt. Delivery is never
// confused with observation or the receiver's safe-point acknowledgement.
type PauseRow struct {
	Target, ID, Requester, Reason, WakeCondition string
	AcceptedAt, DeliveredAt, ObservedAt, SafeAt  time.Time
	WakeRequestedAt, WakeDeliveredAt, ResumedAt  time.Time
	DeliveryError, WakeError                     string
}

const pauseColumns = `target_session, request_id, requester_session, reason, wake_condition,
 accepted_at, delivered_at, observed_at, safe_at, wake_requested_at, wake_delivered_at,
 resumed_at, delivery_error, wake_error`

func scanPause(row interface{ Scan(...any) error }) (PauseRow, error) {
	var p PauseRow
	var accepted, delivered, observed, safe, wakeRequested, wakeDelivered, resumed int64
	err := row.Scan(&p.Target, &p.ID, &p.Requester, &p.Reason, &p.WakeCondition,
		&accepted, &delivered, &observed, &safe, &wakeRequested, &wakeDelivered,
		&resumed, &p.DeliveryError, &p.WakeError)
	for _, v := range []struct {
		n   int64
		out *time.Time
	}{
		{accepted, &p.AcceptedAt}, {delivered, &p.DeliveredAt}, {observed, &p.ObservedAt},
		{safe, &p.SafeAt}, {wakeRequested, &p.WakeRequestedAt},
		{wakeDelivered, &p.WakeDeliveredAt}, {resumed, &p.ResumedAt},
	} {
		if v.n > 0 {
			*v.out = time.Unix(v.n, 0).UTC()
		}
	}
	return p, err
}

func (s *Store) Pauses(ctx context.Context) ([]PauseRow, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+pauseColumns+` FROM session_pauses ORDER BY accepted_at DESC, target_session`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := []PauseRow{}
	for rows.Next() {
		p, err := scanPause(rows)
		if err != nil {
			return nil, classify(err)
		}
		out = append(out, p)
	}
	return out, classify(rows.Err())
}

func (s *Store) Pause(ctx context.Context, id string) (PauseRow, error) {
	if err := reading(); err != nil {
		return PauseRow{}, err
	}
	p, err := scanPause(s.db.QueryRowContext(ctx, `SELECT `+pauseColumns+` FROM session_pauses WHERE request_id = ?`, id))
	return p, classify(err)
}

// DecidePause serializes a pause transition and its delivery effect in one
// transaction. The callback is pure; terminal work runs only after commit.
func (s *Store) DecidePause(ctx context.Context, target string,
	decide func(*PauseRow) (*PauseRow, []Effect, error)) (PauseRow, []int64, error) {
	var out PauseRow
	var ids []int64
	var rejected error
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		current, err := scanPause(tx.QueryRowContext(ctx, `SELECT `+pauseColumns+` FROM session_pauses WHERE target_session = ?`, target))
		if err != nil && err != sql.ErrNoRows {
			return 0, err
		}
		var prior *PauseRow
		if err == nil {
			prior = &current
		}
		next, effects, refusal := decide(prior)
		if refusal != nil {
			rejected = refusal
			return 0, nil
		}
		if next == nil {
			if prior != nil {
				out = *prior
			}
			return 0, nil
		}
		_, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO session_pauses (`+pauseColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			next.Target, next.ID, next.Requester, next.Reason, next.WakeCondition,
			unixOrZeroTime(next.AcceptedAt), unixOrZeroTime(next.DeliveredAt),
			unixOrZeroTime(next.ObservedAt), unixOrZeroTime(next.SafeAt),
			unixOrZeroTime(next.WakeRequestedAt), unixOrZeroTime(next.WakeDeliveredAt),
			unixOrZeroTime(next.ResumedAt), next.DeliveryError, next.WakeError)
		if err != nil {
			return 0, err
		}
		out = *next
		for _, e := range effects {
			// A retry can race another retry while terminal delivery is still
			// running. The open effect check shares this write transaction.
			var open int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE kind = ? AND subject = ?
			  AND payload = ? AND state IN ('pending', 'started')`,
				e.Kind, e.Subject, string(e.Payload)).Scan(&open); err != nil {
				return 0, err
			}
			if open > 0 {
				continue
			}
			id, err := s.insertEffect(ctx, tx, e)
			if err != nil {
				return 0, err
			}
			ids = append(ids, id)
		}
		return 1 + int64(len(effects)), nil
	})
	if err != nil {
		return PauseRow{}, nil, err
	}
	return out, ids, rejected
}
