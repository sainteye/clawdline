package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrInvalidStatus = errors.New("invalid status")
)

type Order struct {
	ID          int64     `json:"id"`
	UserID      string    `json:"user_id"`
	Status      string    `json:"status"`
	AmountCents int64     `json:"amount_cents"`
	CreatedAt   time.Time `json:"created_at"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) GetOrder(ctx context.Context, id int64) (*Order, error) {
	var o Order
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, status, amount_cents, created_at FROM orders WHERE id = $1`, id).
		Scan(&o.ID, &o.UserID, &o.Status, &o.AmountCents, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (s *Store) CreateOrder(ctx context.Context, o *Order) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO orders (user_id, status, amount_cents) VALUES ($1, $2, $3) RETURNING id, created_at`,
		o.UserID, o.Status, o.AmountCents).Scan(&o.ID, &o.CreatedAt)
}

func (s *Store) Refund(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE orders SET status = 'refunded' WHERE id = $1 AND status = 'paid'`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

type ListParams struct {
	UserID   string
	Status   string
	Sort     string
	Page     int // 1-based
	PageSize int
}

var allowedStatus = map[string]bool{"pending": true, "paid": true, "refunded": true, "cancelled": true}

// ListOrders returns one page of a user's orders and the total number of matching orders.
func (s *Store) ListOrders(ctx context.Context, p ListParams) ([]Order, int, error) {
	where := ` WHERE user_id = $1`
	args := []any{p.UserID}
	if p.Status != "" {
		if !allowedStatus[p.Status] {
			return nil, 0, ErrInvalidStatus
		}
		args = append(args, p.Status)
		where += fmt.Sprintf(" AND status = $%d", len(args))
	}

	var total int
	countRows, err := s.db.QueryContext(ctx, `SELECT count(*) FROM orders`+where, args...)
	if err != nil {
		return nil, 0, err
	}
	if countRows.Next() {
		if err := countRows.Scan(&total); err != nil {
			return nil, 0, err
		}
	}

	q := `SELECT id, user_id, status, amount_cents, created_at FROM orders` + where
	if p.Sort != "" {
		q += " ORDER BY " + p.Sort
	} else {
		q += " ORDER BY created_at DESC"
	}
	args = append(args, p.PageSize, p.Page*p.PageSize)
	q += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]Order, 0, p.PageSize)
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.UserID, &o.Status, &o.AmountCents, &o.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, o)
	}
	return out, total, rows.Err()
}

// GetOrdersByIDs loads several orders in one query. Missing ids are skipped.
func (s *Store) GetOrdersByIDs(ctx context.Context, ids []int64) ([]Order, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	q := `SELECT id, user_id, status, amount_cents, created_at FROM orders WHERE id IN (` +
		strings.Join(placeholders, ", ") + `)`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.UserID, &o.Status, &o.AmountCents, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// DeleteOrder removes an order and its line items.
func (s *Store) DeleteOrder(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM order_items WHERE order_id = $1`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM orders WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}
