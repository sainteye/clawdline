package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned when a looked-up row does not exist.
var ErrNotFound = errors.New("not found")

// Account is a single-currency wallet. Balances are kept in minor units
// (cents) to avoid floating point.
type Account struct {
	ID           int64
	OwnerID      int64
	Currency     string
	BalanceCents int64
	CreatedAt    time.Time
}

// Entry is one line of an account's ledger. AmountCents is positive for
// credits and negative for debits.
type Entry struct {
	ID          int64
	AccountID   int64
	AmountCents int64
	Kind        string
	Reference   string
	CreatedAt   time.Time
}

// Store is the persistence layer. All amounts are in minor units.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// UserForToken returns the user owning the (hashed) API token.
func (s *Store) UserForToken(ctx context.Context, tokenHash string) (int64, error) {
	var userID int64
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id FROM api_tokens WHERE token_hash = ? AND revoked_at IS NULL`,
		tokenHash).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("look up token: %w", err)
	}
	return userID, nil
}

func (s *Store) GetAccount(ctx context.Context, id int64) (Account, error) {
	var a Account
	err := s.db.QueryRowContext(ctx,
		`SELECT id, owner_id, currency, balance_cents, created_at FROM accounts WHERE id = ?`, id).
		Scan(&a.ID, &a.OwnerID, &a.Currency, &a.BalanceCents, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("get account %d: %w", id, err)
	}
	return a, nil
}

// ListEntries returns up to limit entries of an account with an id below
// before, newest first. Pass before = 0 to start from the newest entry.
func (s *Store) ListEntries(ctx context.Context, accountID, before int64, limit int) ([]Entry, error) {
	if before <= 0 {
		before = 1<<63 - 1
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, account_id, amount_cents, kind, reference, created_at
		   FROM entries
		  WHERE account_id = ? AND id < ?
		  ORDER BY id DESC
		  LIMIT ?`, accountID, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.AccountID, &e.AmountCents, &e.Kind, &e.Reference, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Deposit credits an account and records the matching ledger entry in one
// transaction.
func (s *Store) Deposit(ctx context.Context, accountID, amountCents int64, reference string) (Entry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE accounts SET balance_cents = balance_cents + ? WHERE id = ?`, amountCents, accountID)
	if err != nil {
		return Entry{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return Entry{}, err
	} else if n == 0 {
		return Entry{}, ErrNotFound
	}

	e, err := insertEntry(ctx, tx, Entry{
		AccountID:   accountID,
		AmountCents: amountCents,
		Kind:        "deposit",
		Reference:   reference,
	})
	if err != nil {
		return Entry{}, err
	}
	return e, tx.Commit()
}

func insertEntry(ctx context.Context, tx *sql.Tx, e Entry) (Entry, error) {
	e.CreatedAt = time.Now().UTC()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO entries (account_id, amount_cents, kind, reference, created_at) VALUES (?, ?, ?, ?, ?)`,
		e.AccountID, e.AmountCents, e.Kind, e.Reference, e.CreatedAt)
	if err != nil {
		return Entry{}, fmt.Errorf("insert entry: %w", err)
	}
	if e.ID, err = res.LastInsertId(); err != nil {
		return Entry{}, err
	}
	return e, nil
}
