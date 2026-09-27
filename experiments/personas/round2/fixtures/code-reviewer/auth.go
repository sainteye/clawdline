package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strings"
)

type ctxKey int

const (
	userKey ctxKey = iota
	accountKey
)

// authenticate resolves the bearer token to a user id and stores it in the
// request context. Requests without a valid token are rejected with 401.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			writeErr(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		userID, err := s.store.UserForToken(r.Context(), hashToken(token))
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if err != nil {
			log.Printf("auth: %v", err)
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		ctx := context.WithValue(r.Context(), userKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireOwner loads the account named by the {id} path segment and rejects
// the request unless it belongs to the authenticated user. The loaded account
// is passed on in the request context.
func (s *Server) requireOwner(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		acct, err := s.store.GetAccount(r.Context(), id)
		if err != nil {
			writeStoreError(w, "load account", err)
			return
		}
		if acct.OwnerID != userFrom(r.Context()) {
			// Same answer as a missing account, so ids cannot be probed.
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		ctx := context.WithValue(r.Context(), accountKey, acct)
		next(w, r.WithContext(ctx))
	}
}

func userFrom(ctx context.Context) int64 {
	id, _ := ctx.Value(userKey).(int64)
	return id
}

func accountFrom(ctx context.Context) Account {
	acct, _ := ctx.Value(accountKey).(Account)
	return acct
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
