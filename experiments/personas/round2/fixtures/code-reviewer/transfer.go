package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// ErrDailyLimit is returned when a transfer would take an account over its
// daily transfer limit.
var ErrDailyLimit = errors.New("daily transfer limit exceeded")

type transferRequest struct {
	ToAccountID int64  `json:"to_account_id"`
	AmountCents int64  `json:"amount_cents"`
	Memo        string `json:"memo"`
}

// createTransfer moves money from the account in the path to another account
// of the same currency.
func (s *Server) createTransfer(w http.ResponseWriter, r *http.Request) {
	from := accountFrom(r.Context())
	var req transferRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.AmountCents <= 0 || req.ToAccountID == from.ID {
		writeErr(w, http.StatusBadRequest, "need a positive amount_cents and a different to_account_id")
		return
	}

	to, err := s.store.GetAccount(r.Context(), req.ToAccountID)
	if err != nil {
		writeStoreError(w, "load destination account", err)
		return
	}
	if to.Currency != from.Currency {
		writeErr(w, http.StatusUnprocessableEntity, "accounts use different currencies")
		return
	}
	if from.BalanceCents < req.AmountCents {
		writeStoreError(w, "transfer", ErrInsufficientFunds)
		return
	}
	if err := s.checkDailyLimit(r.Context(), from.ID, req.AmountCents); err != nil {
		writeStoreError(w, "check daily limit", err)
		return
	}

	t, err := s.store.CreateTransfer(r.Context(), Transfer{
		FromAccountID: from.ID,
		ToAccountID:   to.ID,
		AmountCents:   req.AmountCents,
		Memo:          req.Memo,
	})
	if err != nil {
		writeStoreError(w, "create transfer", err)
		return
	}

	ref := transferReference(t.ID)
	s.notify(Event{Type: "transfer_out", AccountID: from.ID, AmountCents: -t.AmountCents, Currency: from.Currency, Reference: ref, At: t.CreatedAt})
	s.notify(Event{Type: "transfer_in", AccountID: to.ID, AmountCents: t.AmountCents, Currency: to.Currency, Reference: ref, At: t.CreatedAt})

	writeCreated(w, r.URL.JoinPath(strconv.FormatInt(t.ID, 10)).Path, t)
}

func (s *Server) getTransfer(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	t, err := s.store.GetTransfer(r.Context(), id)
	if err != nil {
		writeStoreError(w, "get transfer", err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// checkDailyLimit returns ErrDailyLimit if sending amount now would take the
// account over its limit for the current business day.
func (s *Server) checkDailyLimit(ctx context.Context, accountID, amount int64) error {
	if s.opts.DailyLimit <= 0 {
		return nil
	}
	since := startOfBusinessDay(time.Now(), s.opts.Location)
	sent, err := s.store.SentSince(ctx, accountID, since)
	if err != nil {
		return err
	}
	if sent+amount > s.opts.DailyLimit {
		return ErrDailyLimit
	}
	return nil
}

// newReference returns a random reference such as "po_3f9a0c2b1d4e5f60".
func newReference(prefix string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(b[:]), nil
}

// goResult runs f on its own goroutine and delivers its result on the
// returned channel.
func goResult(f func() error) <-chan error {
	ch := make(chan error)
	go func() { ch <- f() }()
	return ch
}
