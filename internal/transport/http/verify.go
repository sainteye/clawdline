package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
)

// Things waiting to be verified (docs/verifications.md,
// api/v1/verifications.schema.json).
//
//	GET    /v1/verifications                      the list
//	POST   /v1/verifications                      a new record
//	GET    /v1/verifications/{id}                 one, with its data source read now
//	DELETE /v1/verifications/{id}[?force=1]       a closed one, or an open one with force
//	POST   /v1/verifications/{id}/notes           a note
//	POST   /v1/verifications/{id}/criteria/{n}    mark a criterion
//	POST   /v1/verifications/{id}/close           accepted or rejected, with the reason
//
// The gate lets a paired device and this machine's orchestrator token through
// (machineScoped); a change needs a device that may send or that token
// (writePolicy). A note says who wrote it by the door it came in by: a device
// is the person, the orchestrator token is a session.
//
// A scheduled task writes its readout through its own task secret instead
// (verificationTaskNote, under /v1/orchestrator/tasks/).

func (s *Server) verifications() *app.Verifications {
	return &app.Verifications{Store: s.store, Usage: s.usageLedger()}
}

// StartVerifications plants the records this daemon owes (app.SeedVerifications).
// A failure is logged and nothing else stops: the person can still make the
// record by hand.
func (s *Server) StartVerifications(ctx context.Context) {
	if s.store == nil {
		return
	}
	planted, err := s.verifications().SeedVerifications(ctx)
	if err != nil {
		log.Printf("verifications: the seed could not be planted: %v", err)
		return
	}
	if planted {
		log.Printf("verifications: planted the compaction experiment's record")
	}
}

func (s *Server) verificationsRoute(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "This daemon has no store.")
		return
	}
	rest := strings.Trim(strings.TrimPrefix(routePath(r), "/v1/verifications"), "/")
	if rest == "" {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			s.verificationList(w, r)
		case http.MethodPost:
			s.verificationCreate(w, r)
		default:
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "The list is read with GET and added to with POST.")
		}
		return
	}
	parts := strings.Split(rest, "/")
	id := decodeSegment(parts[0])
	if !app.VerificationIDShape(id) {
		writeRefusal(w, http.StatusNotFound, "not_found", "That is not a verification id.")
		return
	}
	switch {
	case len(parts) == 1 && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		s.verificationGet(w, r, id)
	case len(parts) == 1 && r.Method == http.MethodDelete:
		s.verificationDelete(w, r, id)
	case len(parts) == 2 && parts[1] == "notes" && r.Method == http.MethodPost:
		s.verificationNote(w, r, id)
	case len(parts) == 3 && parts[1] == "criteria" && r.Method == http.MethodPost:
		s.verificationCriterion(w, r, id, parts[2])
	case len(parts) == 2 && parts[1] == "close" && r.Method == http.MethodPost:
		s.verificationClose(w, r, id)
	default:
		writeRefusal(w, http.StatusNotFound, "not_found", "That is not a verification route.")
	}
}

func (s *Server) verificationList(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) > 0 {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The list reads no query.")
		return
	}
	rows, err := s.verifications().List(r.Context())
	if err != nil {
		verificationFailure(w, err)
		return
	}
	out := contract.VerificationList{At: time.Now().Unix(), Verifications: []contract.Verification{}}
	for _, v := range rows {
		out.Verifications = append(out.Verifications, verificationRow(v))
	}
	writeJSON(w, out)
}

func (s *Server) verificationGet(w http.ResponseWriter, r *http.Request, id string) {
	if len(r.URL.Query()) > 0 {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "A verification is read with no query.")
		return
	}
	rec, data, err := s.verifications().Get(r.Context(), id)
	if err != nil {
		verificationFailure(w, err)
		return
	}
	out := contract.VerificationDetail{At: time.Now().Unix(), Verification: verificationRow(rec)}
	if data != nil {
		d := &contract.VerificationData{Kind: data.Kind, Error: data.Error}
		if data.Compaction != nil {
			c := usageComparison(*data.Compaction)
			d.CompactionCompare = &c
		}
		out.Data = d
	}
	writeJSON(w, out)
}

func (s *Server) verificationCreate(w http.ResponseWriter, r *http.Request) {
	var body contract.VerificationCreate
	if !decodeStrict(w, r, &body) {
		return
	}
	in := app.VerificationInput{Title: body.Title, Why: body.Why, Criteria: body.Criteria, ScheduleID: body.ScheduleID}
	if body.StartedAt != 0 {
		in.StartedAt = time.Unix(body.StartedAt, 0)
	}
	if body.DueAt != 0 {
		in.DueAt = time.Unix(body.DueAt, 0)
	}
	if body.Source != nil {
		in.Source = &app.VerificationSource{Kind: body.Source.Kind, Since: body.Source.Since}
	}
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	rec, created, err := s.verifications().Create(r.Context(), in, key)
	if err != nil {
		verificationFailure(w, err)
		return
	}
	if created {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(contract.VerificationDetail{At: time.Now().Unix(), Verification: verificationRow(rec)})
		return
	}
	writeJSON(w, contract.VerificationDetail{At: time.Now().Unix(), Verification: verificationRow(rec)})
}

func (s *Server) verificationNote(w http.ResponseWriter, r *http.Request, id string) {
	var body contract.VerificationNoteCreate
	if !decodeStrict(w, r, &body) {
		return
	}
	by := app.VerificationAuthor{Kind: app.AuthorPerson}
	if machineAuthed(r) {
		by = app.VerificationAuthor{Kind: app.AuthorSession, Name: strings.TrimSpace(body.Session)}
	} else if body.Session != "" {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "A paired device writes as the person and names no session.")
		return
	}
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	if _, err := s.verifications().AppendNote(r.Context(), id, body.Text, by, key); err != nil {
		verificationFailure(w, err)
		return
	}
	s.verificationAfterWrite(w, r, id)
}

func (s *Server) verificationCriterion(w http.ResponseWriter, r *http.Request, id, raw string) {
	index, err := strconv.Atoi(raw)
	if err != nil || index < 0 || strconv.Itoa(index) != raw {
		writeRefusal(w, http.StatusNotFound, "not_found", "A criterion is named by its index, from 0.")
		return
	}
	var body contract.VerificationCriterionSet
	if !decodeStrict(w, r, &body) {
		return
	}
	if err := s.verifications().SetCriterion(r.Context(), id, index, string(body.State)); err != nil {
		verificationFailure(w, err)
		return
	}
	s.verificationAfterWrite(w, r, id)
}

func (s *Server) verificationClose(w http.ResponseWriter, r *http.Request, id string) {
	var body contract.VerificationClose
	if !decodeStrict(w, r, &body) {
		return
	}
	if err := s.verifications().Close(r.Context(), id, string(body.Status), body.Reason); err != nil {
		verificationFailure(w, err)
		return
	}
	s.verificationAfterWrite(w, r, id)
}

// verificationDelete takes one record by its id and nothing else: there is
// no route that deletes more than one.
func (s *Server) verificationDelete(w http.ResponseWriter, r *http.Request, id string) {
	force := false
	for key, values := range r.URL.Query() {
		if key != "force" || len(values) != 1 || (values[0] != "1" && values[0] != "true") {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "A delete reads one query field, force=1.")
			return
		}
		force = true
	}
	if err := s.verifications().Delete(r.Context(), id, force); err != nil {
		verificationFailure(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// verificationAfterWrite answers a write on one record with the record as it
// now stands, without its data source: that is read when it is asked for.
func (s *Server) verificationAfterWrite(w http.ResponseWriter, r *http.Request, id string) {
	rec, found, err := s.store.Verification(r.Context(), id)
	if err != nil {
		verificationFailure(w, err)
		return
	}
	if !found {
		writeRefusal(w, http.StatusNotFound, "not_found", "There is no verification "+id+".")
		return
	}
	writeJSON(w, contract.VerificationDetail{At: time.Now().Unix(), Verification: verificationRow(rec)})
}

// verificationTaskNote is POST /v1/orchestrator/tasks/{task}/verification-note:
// a task a schedule started writes its readout on a record linked to that
// schedule, with its own secret. It is the whole of what the secret opens
// here — no other verification route takes one.
func (s *Server) verificationTaskNote(w http.ResponseWriter, r *http.Request, task string) {
	if s.store == nil {
		writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "This daemon has no store.")
		return
	}
	record, _, err := s.broker.Authenticate(r.Context(), task, taskSecret(r))
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	var body contract.VerificationTaskNote
	if !decodeStrict(w, r, &body) {
		return
	}
	if !app.VerificationIDShape(body.Verification) {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "verification names one record by its id.")
		return
	}
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	note, err := s.verifications().AppendScheduledNote(r.Context(), record.ScheduleID, task,
		body.Verification, body.Text, key)
	if err != nil {
		verificationFailure(w, err)
		return
	}
	writeJSON(w, contract.VerificationNoteResult{OK: true, Verification: body.Verification, Note: noteRow(note)})
}

// decodeStrict reads one JSON object with no field the contract does not
// name: a field this daemon would drop is refused by name instead.
func decodeStrict(w http.ResponseWriter, r *http.Request, into any) bool {
	var raw bytes.Buffer
	if _, err := raw.ReadFrom(r.Body); err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The body could not be read.")
		return false
	}
	dec := json.NewDecoder(&raw)
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The body is not what this route reads: "+err.Error()+".")
		return false
	}
	if dec.More() {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The body is one JSON object.")
		return false
	}
	return true
}

// idempotencyKey is the optional Idempotency-Key a write may carry: the
// Cloud bridge sends one on every command, and a repeat answers the first.
func idempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) > 200 {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "An Idempotency-Key is at most 200 characters.")
		return "", false
	}
	return key, true
}

func verificationFailure(w http.ResponseWriter, err error) {
	var refusal *app.VerificationError
	if errors.As(err, &refusal) {
		status := http.StatusBadRequest
		switch refusal.Code {
		case "not_found":
			status = http.StatusNotFound
		case "verification_limit_reached", "notes_limit_reached", "verification_closed", "verification_open":
			status = http.StatusConflict
		case "schedule_mismatch", "not_scheduled":
			status = http.StatusForbidden
		}
		writeRefusal(w, status, refusal.Code, refusal.Message)
		return
	}
	if errors.Is(err, store.ErrBusy) {
		writeRefusal(w, http.StatusServiceUnavailable, "store_busy", "The store is busy; try again.")
		return
	}
	log.Printf("verifications: %v", err)
	writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "The verifications could not be read.")
}

func verificationRow(v store.Verification) contract.Verification {
	out := contract.Verification{ID: v.ID, Title: v.Title, Why: v.Why, StartedAt: v.StartedAt, DueAt: v.DueAt,
		Criteria: []contract.VerificationCriterion{}, ScheduleID: v.ScheduleID, Notes: []contract.VerificationNote{},
		Status: contract.VerificationStatus(v.Status), CloseReason: v.CloseReason, ClosedAt: v.ClosedAt,
		Seed: v.Seed, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
	if v.SourceKind != "" {
		out.Source = &contract.VerificationSource{Kind: v.SourceKind, Since: v.SourceSince}
	}
	for _, c := range v.Criteria {
		out.Criteria = append(out.Criteria, contract.VerificationCriterion{Index: int64(c.Index), Text: c.Text,
			State: contract.VerificationCriterionState(c.State), UpdatedAt: c.UpdatedAt})
	}
	for _, n := range v.Notes {
		out.Notes = append(out.Notes, noteRow(n))
	}
	return out
}

func noteRow(n store.VerificationNote) contract.VerificationNote {
	return contract.VerificationNote{ID: n.ID, At: n.At, AuthorKind: contract.VerificationAuthorKind(n.AuthorKind),
		Author: n.Author, Text: n.Text}
}
