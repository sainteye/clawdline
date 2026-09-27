package main

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrNotFound = errors.New("not found")

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
