package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrNotFound is returned when a looked-up row does not exist.
	ErrNotFound = errors.New("not found")
	// ErrInsufficientFunds is returned when a debit would overdraw an account.
	ErrInsufficientFunds = errors.New("insufficient funds")
)

// lookupError says which row a failed lookup was for.
type lookupError struct {
	what string
	id   int64
	err  error
}

func (e *lookupError) Error() string {
	return fmt.Sprintf("get %s %d: %v", e.what, e.id, e.err)
}

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

// Transfer moves money between two accounts of the same currency.
type Transfer struct {
	ID            int64     `json:"id"`
	FromAccountID int64     `json:"from_account_id"`
	ToAccountID   int64     `json:"to_account_id"`
	AmountCents   int64     `json:"amount_cents"`
	Memo          string    `json:"memo,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
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
		return Account{}, &lookupError{what: "account", id: id, err: ErrNotFound}
	}
	if err != nil {
		return Account{}, &lookupError{what: "account", id: id, err: err}
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
		return nil, fmt.Errorf("list entries: %w", err)
	}
	return scanEntries(rows)
}

// scanEntries reads all rows into entries and closes rows.
func scanEntries(rows *sql.Rows) ([]Entry, error) {
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.AccountID, &e.AmountCents, &e.Kind, &e.Reference, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SentSince sums the transfers an account has sent since the given time.
func (s *Store) SentSince(ctx context.Context, accountID int64, since time.Time) (int64, error) {
	var sent int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(-amount_cents), 0)
		   FROM entries
		  WHERE account_id = ? AND kind = ? AND created_at >= ?`,
		accountID, "transfer_out", since.UTC()).Scan(&sent)
	if err != nil {
		return 0, fmt.Errorf("sum transfers of account %d: %w", accountID, err)
	}
	return sent, nil
}

// Credit adds money to an account and records the matching ledger entry of
// the given kind in one transaction.
func (s *Store) Credit(ctx context.Context, accountID, amountCents int64, kind, reference string) (Entry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()

	if err := adjustBalance(ctx, tx, accountID, amountCents); err != nil {
		return Entry{}, err
	}
	e, err := insertEntry(ctx, tx, Entry{
		AccountID:   accountID,
		AmountCents: amountCents,
		Kind:        kind,
		Reference:   reference,
	})
	if err != nil {
		return Entry{}, err
	}
	return e, tx.Commit()
}

// Withdraw debits an account for a payout.
func (s *Store) Withdraw(ctx context.Context, accountID, amountCents int64, reference string) (Entry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()

	if err := adjustBalance(ctx, tx, accountID, -amountCents); err != nil {
		return Entry{}, fmt.Errorf("debit account %d: %w", accountID, err)
	}
	e, err := insertEntry(ctx, tx, Entry{
		AccountID:   accountID,
		AmountCents: -amountCents,
		Kind:        "payout",
		Reference:   reference,
	})
	if err != nil {
		return Entry{}, err
	}
	return e, tx.Commit()
}

// CreateTransfer records a transfer, moves the money and writes one ledger
// entry on each side, all in one transaction.
func (s *Store) CreateTransfer(ctx context.Context, t Transfer) (Transfer, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Transfer{}, fmt.Errorf("begin transfer: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
			return
		}
		err = tx.Commit()
	}()

	t.CreatedAt = time.Now().UTC()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO transfers (from_account_id, to_account_id, amount_cents, memo, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		t.FromAccountID, t.ToAccountID, t.AmountCents, t.Memo, t.CreatedAt)
	if err != nil {
		return Transfer{}, fmt.Errorf("insert transfer: %w", err)
	}
	if t.ID, err = res.LastInsertId(); err != nil {
		return Transfer{}, err
	}

	if err := adjustBalance(ctx, tx, t.FromAccountID, -t.AmountCents); err != nil {
		return Transfer{}, fmt.Errorf("debit account %d: %w", t.FromAccountID, err)
	}
	if err := adjustBalance(ctx, tx, t.ToAccountID, t.AmountCents); err != nil {
		return Transfer{}, fmt.Errorf("credit account %d: %w", t.ToAccountID, err)
	}

	ref := transferReference(t.ID)
	for _, e := range []Entry{
		{AccountID: t.FromAccountID, AmountCents: -t.AmountCents, Kind: "transfer_out", Reference: ref},
		{AccountID: t.ToAccountID, AmountCents: t.AmountCents, Kind: "transfer_in", Reference: ref},
	} {
		if _, err := insertEntry(ctx, tx, e); err != nil {
			return Transfer{}, err
		}
	}
	return t, nil
}

func (s *Store) GetTransfer(ctx context.Context, id int64) (Transfer, error) {
	var t Transfer
	err := s.db.QueryRowContext(ctx,
		`SELECT id, from_account_id, to_account_id, amount_cents, memo, created_at FROM transfers WHERE id = ?`, id).
		Scan(&t.ID, &t.FromAccountID, &t.ToAccountID, &t.AmountCents, &t.Memo, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Transfer{}, &lookupError{what: "transfer", id: id, err: ErrNotFound}
	}
	if err != nil {
		return Transfer{}, &lookupError{what: "transfer", id: id, err: err}
	}
	return t, nil
}

// adjustBalance adds delta (which may be negative) to an account's balance.
func adjustBalance(ctx context.Context, tx *sql.Tx, accountID, delta int64) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE accounts SET balance_cents = balance_cents + ? WHERE id = ?`, delta, accountID)
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

func transferReference(id int64) string {
	return fmt.Sprintf("tr_%d", id)
}
