package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
)

// Durable request receipts for the routes that open things (D03, G15).
//
// Starting a session, resuming one and transcribing a recording used to file
// their answers in a map in this process for ten minutes: a retry after a
// restart — the moment a client is most likely to be unsure its request
// landed — found nothing and opened a second tab. They file them in the
// store's one receipt table now, under their own scope, with the request's
// digest beside the key.

// Receipt scopes of the routes in this package.
const (
	scopePlaces = "places"
	scopeVoice  = "voice"
)

// receiptWait is how long a request whose twin is still being answered waits
// for that answer before it is told to ask again. Opening a tab takes seconds
// and a transcription tens of them.
const receiptWait = 90 * time.Second

// requestDigest is the digest a receipt is filed under.
func requestDigest(parts ...[]byte) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(strconv.Itoa(len(p))))
		h.Write([]byte{0})
		h.Write(p)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// receipted answers one request under its receipt: the first time by running
// body, every later time with what that first time answered. file says which
// answers are kept; one that is not — about this moment rather than the
// request, a full queue — gives the reservation back so a retry is run.
func (s *Server) receipted(w http.ResponseWriter, r *http.Request, k store.ReceiptKey, digest string,
	file func(status int) bool, body func(http.ResponseWriter)) {
	s.receiptedFor(w, r, k, digest, store.ReceiptPolicy{}, file, body)
}

// receiptedFor is `receipted` with a scope's own policy. A scope whose
// requests are frequent keeps a shorter window: the limit is per scope inside
// its window, and a window long enough to fill it would refuse new requests
// rather than answering retries (see scopeSessions).
func (s *Server) receiptedFor(w http.ResponseWriter, r *http.Request, k store.ReceiptKey, digest string,
	policy store.ReceiptPolicy, file func(status int) bool, body func(http.ResponseWriter)) {
	ctx := r.Context()
	deadline := time.Now().Add(receiptWait)
	for {
		claim, err := s.store.ClaimReceipt(ctx, k, digest, policy, time.Now())
		if err != nil {
			if errors.Is(err, store.ErrBusy) {
				w.Header().Set("Retry-After", "1")
				writeAuthRefusal(w, http.StatusServiceUnavailable, "store_busy",
					"The store was held by another writer; nothing was done. Retry.")
				return
			}
			writeAuthRefusal(w, http.StatusServiceUnavailable, "store_unavailable",
				"The request's receipt could not be read; nothing was done.")
			return
		}
		switch claim.Outcome {
		case store.ReceiptNew:
		case store.ReceiptReplay:
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Idempotent-Replayed", "true")
			w.WriteHeader(claim.Answer.Status)
			_, _ = w.Write(claim.Answer.Body)
			return
		case store.ReceiptMismatch:
			writeAuthRefusal(w, http.StatusConflict, "idempotency_key_reused",
				"This Idempotency-Key was already used for a different request; nothing was done. Use a new key.")
			return
		case store.ReceiptExpired:
			writeAuthRefusal(w, http.StatusConflict, "receipt_expired",
				"This Idempotency-Key's window has passed. The request was answered once and is not carried out again.")
			return
		case store.ReceiptFull:
			w.Header().Set("Retry-After", strconv.Itoa(int(claim.RetryAfter/time.Second)+1))
			writeAuthRefusal(w, http.StatusTooManyRequests, "receipts_full",
				"This route holds as many answered requests as it keeps inside their window; nothing was done.")
			return
		case store.ReceiptOrphaned:
			rec := &recorder{header: http.Header{}}
			writeAuthRefusal(rec, http.StatusConflict, "request_outcome_unknown",
				"The daemon stopped while this request was being carried out, so whether it took effect is unknown. "+
					"It was not repeated; look before asking again under a new key.")
			_ = s.store.CompleteReceipt(context.WithoutCancel(ctx), k, store.ReceiptAnswer{Status: rec.status, Body: rec.body.Bytes()})
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(rec.status)
			_, _ = w.Write(rec.body.Bytes())
			return
		case store.ReceiptPending:
			if time.Now().After(deadline) {
				w.Header().Set("Retry-After", "1")
				writeAuthRefusal(w, http.StatusConflict, "request_in_progress",
					"The same request is still being carried out; ask again for its answer.")
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
			continue
		}
		break
	}
	rec := &recorder{header: http.Header{}}
	body(rec)
	// Filed, released and answered even if the caller has gone: the answer
	// is the request's, not the connection's.
	keep := context.WithoutCancel(ctx)
	if rec.status == 0 || !file(rec.status) {
		// Nothing was written — the caller left while it queued — or the
		// answer is about this moment: the key is given back.
		_ = s.store.ReleaseReceipt(keep, k)
		if rec.status == 0 {
			return
		}
	} else if err := s.store.CompleteReceipt(keep, k, store.ReceiptAnswer{Status: rec.status, Body: rec.body.Bytes()}); err != nil {
		// The effect happened and its answer could not be filed; the caller
		// still gets it, and a retry finds the reservation this process holds.
		rec.header.Set("Idempotent-Unfiled", "true")
	}
	for key, vs := range rec.header {
		w.Header()[key] = vs
	}
	w.WriteHeader(rec.status)
	_, _ = w.Write(rec.body.Bytes())
}
