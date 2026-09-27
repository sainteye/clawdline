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

// Server holds the dependencies of the HTTP handlers.
type Server struct {
	store       *Store
	notifier    *Notifier
	maxPageSize int
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
	Kind        string    `json:"kind"`
	Reference   string    `json:"reference,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func toEntryJSON(e Entry) entryJSON {
	return entryJSON{
		ID:          e.ID,
		AmountCents: e.AmountCents,
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

	limit := s.maxPageSize
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeErr(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = min(n, s.maxPageSize)
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
		log.Printf("list entries for account %d: %v", acct.ID, err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]entryJSON, 0, len(entries))
	for _, e := range entries {
		out = append(out, toEntryJSON(e))
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

	e, err := s.store.Deposit(r.Context(), acct.ID, req.AmountCents, req.Reference)
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "account not found")
		return
	}
	if err != nil {
		log.Printf("deposit to account %d: %v", acct.ID, err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	ev := Event{Type: "deposit", AccountID: acct.ID, AmountCents: e.AmountCents, Reference: e.Reference, At: e.CreatedAt}
	if err := s.notifier.Notify(r.Context(), ev); err != nil {
		log.Printf("notify deposit %d: %v", e.ID, err)
	}
	writeJSON(w, http.StatusCreated, toEntryJSON(e))
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

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
