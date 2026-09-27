package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

// maxPayoutCents is the largest single payout the API accepts.
const maxPayoutCents = 25_000_000

// ServerOptions are the tunables of the HTTP layer.
type ServerOptions struct {
	MaxPageSize int
	NotifyWait  time.Duration
	DailyLimit  int64
	Location    *time.Location
}

// Server holds the dependencies of the HTTP handlers.
type Server struct {
	store    *Store
	notifier *Notifier
	payouts  *PayoutClient
	opts     ServerOptions
}

type accountJSON struct {
	ID           int64     `json:"id"`
	Currency     string    `json:"currency"`
	BalanceCents int64     `json:"balance_cents"`
	CreatedAt    time.Time `json:"created_at"`
}

type entryJSON struct {
	ID          int64     `json:"id"`
	AmountCents int64     `json:"amount_cents"`
	Amount      string    `json:"amount"`
	Kind        string    `json:"kind"`
	Reference   string    `json:"reference,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func toEntryJSON(e Entry, currency string) entryJSON {
	return entryJSON{
		ID:          e.ID,
		AmountCents: e.AmountCents,
		Amount:      FormatAmount(e.AmountCents, currency),
		Kind:        e.Kind,
		Reference:   e.Reference,
		CreatedAt:   e.CreatedAt,
	}
}

func (s *Server) getAccount(w http.ResponseWriter, r *http.Request) {
	acct := accountFrom(r.Context())
	writeJSON(w, http.StatusOK, accountJSON{
		ID:           acct.ID,
		Currency:     acct.Currency,
		BalanceCents: acct.BalanceCents,
		CreatedAt:    acct.CreatedAt,
	})
}

func (s *Server) listEntries(w http.ResponseWriter, r *http.Request) {
	acct := accountFrom(r.Context())
	q := r.URL.Query()

	limit := s.opts.MaxPageSize
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeErr(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = min(n, s.opts.MaxPageSize)
	}
	var before int64
	if v := q.Get("before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			writeErr(w, http.StatusBadRequest, "before must be a positive entry id")
			return
		}
		before = n
	}

	entries, err := s.store.ListEntries(r.Context(), acct.ID, before, limit)
	if err != nil {
		writeStoreError(w, "list entries", err)
		return
	}
	out := make([]entryJSON, 0, len(entries))
	for _, e := range entries {
		out = append(out, toEntryJSON(e, acct.Currency))
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}

type depositRequest struct {
	AmountCents int64  `json:"amount_cents"`
	Reference   string `json:"reference"`
}

func (s *Server) createDeposit(w http.ResponseWriter, r *http.Request) {
	acct := accountFrom(r.Context())
	var req depositRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.AmountCents <= 0 {
		writeErr(w, http.StatusBadRequest, "amount_cents must be positive")
		return
	}

	e, err := s.store.Credit(r.Context(), acct.ID, req.AmountCents, "deposit", req.Reference)
	if err != nil {
		writeStoreError(w, "deposit", err)
		return
	}
	s.notify(Event{Type: "deposit", AccountID: acct.ID, AmountCents: e.AmountCents, Currency: acct.Currency, Reference: e.Reference, At: e.CreatedAt})
	writeJSON(w, http.StatusCreated, toEntryJSON(e, acct.Currency))
}

type payoutRequest struct {
	AmountCents int64  `json:"amount_cents"`
	Destination string `json:"destination"`
}

func (s *Server) createPayout(w http.ResponseWriter, r *http.Request) {
	acct := accountFrom(r.Context())
	var req payoutRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.AmountCents <= 0 || req.AmountCents > maxPayoutCents {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("amount_cents must be between 1 and %d", maxPayoutCents))
		return
	}
	if req.Destination == "" {
		writeErr(w, http.StatusBadRequest, "destination is required")
		return
	}
	if acct.BalanceCents < req.AmountCents {
		writeStoreError(w, "payout", ErrInsufficientFunds)
		return
	}

	ref, err := newReference("po")
	if err != nil {
		writeStoreError(w, "payout reference", err)
		return
	}
	e, err := s.store.Withdraw(r.Context(), acct.ID, req.AmountCents, ref)
	if err != nil {
		writeStoreError(w, "payout debit", err)
		return
	}

	res, err := s.payouts.Send(r.Context(), PayoutRequest{
		AmountCents: req.AmountCents,
		Currency:    acct.Currency,
		Destination: req.Destination,
		Reference:   ref,
	})
	if err != nil {
		log.Printf("payout %s for account %d failed: %v", ref, acct.ID, err)
		if _, rerr := s.store.Credit(r.Context(), acct.ID, req.AmountCents, "payout_reversal", ref); rerr != nil {
			log.Printf("payout %s: reversal failed, manual fix needed: %v", ref, rerr)
		}
		writeErr(w, http.StatusBadGateway, "payout provider did not accept the payout")
		return
	}

	s.notify(Event{Type: "payout", AccountID: acct.ID, AmountCents: e.AmountCents, Currency: acct.Currency, Reference: ref, At: e.CreatedAt})
	writeJSON(w, http.StatusAccepted, map[string]any{
		"entry":       toEntryJSON(e, acct.Currency),
		"provider_id": res.ProviderID,
		"status":      res.Status,
	})
}

// notify sends ev to the webhook, waiting at most opts.NotifyWait.
func (s *Server) notify(ev Event) {
	if err := s.notifier.NotifyWithin(ev, s.opts.NotifyWait); err != nil {
		log.Printf("notify %s for account %d: %v", ev.Type, ev.AccountID, err)
	}
}

var errTimeout = errors.New("timed out")

// waitOrTimeout returns the result from done, or errTimeout if it takes
// longer than d.
func waitOrTimeout(done <-chan error, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case err := <-done:
		return err
	case <-t.C:
		return errTimeout
	}
}

// pathID parses the {id} path segment.
func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id in path")
	}
	return id, nil
}

// decodeJSON reads a small JSON body into dst and rejects unknown fields.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}

// writeCreated answers 201 with v and the location of the new resource.
func writeCreated(w http.ResponseWriter, location string, v any) {
	w.Header().Set("Location", location)
	writeJSON(w, http.StatusCreated, v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeStoreError maps an error from the store (or a check built on it) to
// an HTTP response. Unexpected errors are logged with op and reported as 500.
func writeStoreError(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrInsufficientFunds):
		writeErr(w, http.StatusConflict, "insufficient funds")
	case errors.Is(err, ErrDailyLimit):
		writeErr(w, http.StatusUnprocessableEntity, "daily transfer limit reached")
	default:
		log.Printf("%s: %v", op, err)
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}
