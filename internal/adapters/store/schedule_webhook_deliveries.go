package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrWebhookDeliveryConflict = errors.New("schedule webhook delivery conflict")

// ScheduleWebhookDelivery is the daemon's crash journal for one Cloud
// delivery. PendingReceipt is already-encoded JSON so a retry resends exactly
// the same version and bytes after a restart.
type ScheduleWebhookDelivery struct {
	Version                  int             `json:"clawdline_schedule_webhook_delivery"`
	DeliveryID               string          `json:"delivery_id"`
	DeliveryDigest           string          `json:"delivery_digest"`
	HookID                   string          `json:"hook_id"`
	HookGeneration           int             `json:"hook_generation"`
	ScheduleID               string          `json:"schedule_id,omitempty"`
	PreflightOutcome         string          `json:"preflight_outcome,omitempty"`
	State                    string          `json:"state"`
	TaskID                   string          `json:"task_id,omitempty"`
	LastReceiptVersion       int             `json:"last_receipt_version"`
	CloudAcknowledgedThrough int             `json:"cloud_acknowledged_through"`
	LeaseToken               string          `json:"lease_token"`
	LeaseRevision            int             `json:"lease_revision"`
	PendingReceipt           json.RawMessage `json:"pending_receipt,omitempty"`
	RetryAt                  int64           `json:"retry_at,omitempty"`
	CreatedAt                int64           `json:"created_at"`
	UpdatedAt                int64           `json:"updated_at"`
	TerminalAt               int64           `json:"terminal_at,omitempty"`
}

// ReserveScheduleWebhookDelivery records the stable task id before any
// receipt or dispatch. A replay of the same delivery returns the existing row;
// the same id with different bytes is never allowed to replace it.
func (s *Store) ReserveScheduleWebhookDelivery(ctx context.Context, fresh ScheduleWebhookDelivery) (ScheduleWebhookDelivery, bool, error) {
	body, err := json.Marshal(fresh)
	if err != nil {
		return ScheduleWebhookDelivery{}, false, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schedule_webhook_deliveries
		(delivery_id, delivery_digest, state, updated_at, body) VALUES (?, ?, ?, ?, ?)`,
		fresh.DeliveryID, fresh.DeliveryDigest, fresh.State, fresh.UpdatedAt, string(body))
	if err != nil {
		return ScheduleWebhookDelivery{}, false, err
	}
	created, _ := res.RowsAffected()
	row, err := s.ScheduleWebhookDelivery(ctx, fresh.DeliveryID)
	if err != nil {
		return ScheduleWebhookDelivery{}, false, err
	}
	if row.DeliveryDigest != fresh.DeliveryDigest || row.HookID != fresh.HookID {
		return ScheduleWebhookDelivery{}, false, ErrWebhookDeliveryConflict
	}
	return row, created == 1, nil
}

func (s *Store) ScheduleWebhookDelivery(ctx context.Context, id string) (ScheduleWebhookDelivery, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT body FROM schedule_webhook_deliveries WHERE delivery_id = ?`, id).Scan(&raw)
	if err != nil {
		return ScheduleWebhookDelivery{}, err
	}
	var row ScheduleWebhookDelivery
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		return ScheduleWebhookDelivery{}, err
	}
	return row, nil
}

func (s *Store) SaveScheduleWebhookDelivery(ctx context.Context, row ScheduleWebhookDelivery) error {
	body, err := json.Marshal(row)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE schedule_webhook_deliveries
		SET state = ?, updated_at = ?, body = ? WHERE delivery_id = ? AND delivery_digest = ?`,
		row.State, row.UpdatedAt, string(body), row.DeliveryID, row.DeliveryDigest)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrWebhookDeliveryConflict
	}
	return nil
}

func (s *Store) ScheduleWebhookDeliveriesToResume(ctx context.Context) ([]ScheduleWebhookDelivery, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT body FROM schedule_webhook_deliveries
		ORDER BY updated_at, delivery_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScheduleWebhookDelivery
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var row ScheduleWebhookDelivery
		if err := json.Unmarshal([]byte(raw), &row); err != nil {
			return nil, err
		}
		if row.TerminalAt == 0 || len(row.PendingReceipt) != 0 {
			out = append(out, row)
		}
	}
	return out, rows.Err()
}

func (s *Store) CleanupScheduleWebhookDeliveries(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM schedule_webhook_deliveries
		WHERE updated_at < ? AND state IN ('schedule_dispatch_refused','task_execution_terminal','outcome_unknown','expired','canceled')`, before.Unix())
	return err
}
