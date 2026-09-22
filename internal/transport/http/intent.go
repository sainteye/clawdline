package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/planner"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

const (
	// One sentence, not a document. Five minutes of speech is comfortably below it.
	intentLimit = 4 << 10
	// One model turn running and one waiting; the third is told to retry.
	intentQueueLimit = 2
	// A stopped CLI cannot hold the queue forever.
	intentTimeLimit = 30 * time.Second
	// One hosted request can wait behind one admitted turn, run both of its
	// own attempts, and still receive the answer across the relay.
	intentCloudWaitLimit = 130 * time.Second
)

func (s *Server) intentRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Planning a sentence is a POST.")
		return
	}
	if !maySend(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "This device may read, and not send.")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That needs an Idempotency-Key header.")
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "That body could not be read.")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	k := store.ReceiptKey{Scope: scopeIntent, Actor: accessOf(r).verdict.Device, Key: key}
	s.receipted(w, r, k, requestDigest(raw), func(status int) bool {
		return status < 500 && status != http.StatusTooManyRequests
	}, func(w http.ResponseWriter) { s.planSentence(w, r) })
}

func (s *Server) planSentence(w http.ResponseWriter, r *http.Request) {
	var body contract.IntentRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "That needs text: what the person said.")
		return
	}
	limit := int(CapacityLimit(capacity.IntentRequestBytes))
	if len([]byte(body.Text)) > limit {
		writeRefusal(w, http.StatusBadRequest, "bad_request",
			fmt.Sprintf("That was %d bytes and the limit is %d. This plans a sentence, not a document.", len([]byte(body.Text)), limit))
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "That text was empty.")
		return
	}
	if !s.enterIntent() {
		writeRefusal(w, http.StatusTooManyRequests, "busy", "This machine is already working out two of these. Try again in a moment.")
		return
	}
	defer s.leaveIntent()
	// Two requests may be admitted (one turn and one waiter), but running two
	// local model CLIs at once makes both slower and spends quota concurrently.
	s.intentRun.Lock()
	defer s.intentRun.Unlock()

	places, assistants := s.inputsForIntent(r.Context())
	started := time.Now()
	draft, err := s.planner()(r.Context(), text, places, assistants)
	ms := time.Since(started).Milliseconds()
	device := accessOf(r).verdict.Device
	if errors.Is(err, planner.ErrNoPlanner) {
		log.Printf("audit intent.plan device=%s ms=%d ok=0 why=no_planner", device, ms)
		writeRefusal(w, http.StatusServiceUnavailable, "no_planner", "This machine has neither claude nor codex on it, so there is nothing here to work out what you meant with.")
		return
	}
	if err != nil {
		log.Printf("audit intent.plan device=%s ms=%d ok=0 why=failed", device, ms)
		writeRefusal(w, http.StatusBadGateway, "plan_failed", "The planner did not come back with anything usable. Try saying it again, or start the session by hand.")
		return
	}
	place := "none"
	if draft.PlaceID != nil {
		place = *draft.PlaceID
	}
	log.Printf("audit intent.plan device=%s ms=%d place=%s assistant=%s confidence=%.2f ok=1",
		device, ms, place, draft.Assistant, draft.Confidence)
	writeJSON(w, contract.IntentResult{Draft: draft, Ms: ms})
}

func (s *Server) planner() func(context.Context, string, []planner.Place, []string) (contract.IntentDraft, error) {
	if s.planIntent != nil {
		return s.planIntent
	}
	p := planner.New()
	return p.Draft
}

func (s *Server) inputsForIntent(ctx context.Context) ([]planner.Place, []string) {
	if s.intentContext != nil {
		return s.intentContext(ctx)
	}
	readers := s.projectReaders()
	rows := readers.places.List(s.liveDirectories(ctx), 40)
	places := make([]planner.Place, 0, len(rows))
	for _, row := range rows {
		places = append(places, planner.Place{ID: row.ID, Label: row.Label, Path: row.Path})
	}
	assistants := []string{}
	for _, assistant := range installedAssistants() {
		assistants = append(assistants, assistant.ID)
	}
	return places, assistants
}

func (s *Server) enterIntent() bool {
	s.intentMu.Lock()
	defer s.intentMu.Unlock()
	if int64(s.intentQueued) >= CapacityLimit(capacity.IntentPlannerQueue) {
		return false
	}
	s.intentQueued++
	return true
}

func (s *Server) leaveIntent() {
	s.intentMu.Lock()
	s.intentQueued--
	s.intentMu.Unlock()
}
