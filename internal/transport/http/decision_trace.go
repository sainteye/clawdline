package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The Bots decision card's send path, step by step, in the daemon log.
//
// On 2026-11-25 a press on a decision card spun without ending while the
// answer did reach the chat, and nothing on this machine said which step
// stalled: the local console's answer request is not logged and the card's own
// state lives only in the page. The console posts one small event per step of
// the card (press, request start, settle, failure, unmount while sending) to
// decisionTracePath, and the answer route logs its own arrival and outcome
// (logDecisionAnswer). Both carry the command's idempotency key, so one grep
// joins the page's view to the daemon's. Only ids, step names, codes and
// durations are accepted: never the reply text.
const decisionTracePath = "/v1/project-assistants/decision-trace"

// maxDecisionTraceBodyBytes bounds one trace event (`bots.decision_trace_bytes`).
const maxDecisionTraceBodyBytes = 2 << 10

var decisionTracePhases = map[string]bool{
	"press": true, "request": true, "settled": true, "failed": true,
	"check": true, "unmounted": true, "timeout": true,
}

type decisionTraceEvent struct {
	Attempt        string `json:"attempt"`
	Phase          string `json:"phase"`
	BotID          string `json:"bot_id"`
	OfferID        string `json:"offer_id"`
	Option         string `json:"option"`
	IdempotencyKey string `json:"idempotency_key"`
	Code           string `json:"code"`
	Ms             int64  `json:"ms"`
}

// traceToken admits an identifier-shaped value and nothing else, so a log line
// can neither carry text the person wrote nor be split by what it records. The
// body bound keeps every token, and so the line, under 2 KiB.
func traceToken(s string) bool {
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.:", c)) {
			return false
		}
	}
	return true
}

func (e decisionTraceEvent) valid() bool {
	if !decisionTracePhases[e.Phase] || e.Attempt == "" || e.OfferID == "" || e.Ms < 0 || e.Ms > 24*60*60*1000 {
		return false
	}
	for _, s := range []string{e.Attempt, e.BotID, e.OfferID, e.Option, e.IdempotencyKey, e.Code} {
		if !traceToken(s) {
			return false
		}
	}
	return true
}

func decisionTrace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "A decision trace is sent with POST.")
		return
	}
	if !accessOf(r).verdict.Allowed {
		writeAuthRefusal(w, http.StatusUnauthorized, "unauthorized", unauthorizedMessage)
		return
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(http.MaxBytesReader(w, r.Body, maxDecisionTraceBodyBytes)); err != nil {
		writeAuthRefusal(w, http.StatusRequestEntityTooLarge, "body_too_large", "A decision trace is at most 2 KiB.")
		return
	}
	var e decisionTraceEvent
	d := json.NewDecoder(&buf)
	d.DisallowUnknownFields()
	if err := d.Decode(&e); err != nil || !e.valid() {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "A decision trace names a known step and identifiers only.")
		return
	}
	log.Printf("bots decision trace: attempt=%s phase=%s bot=%s offer=%s option=%s key=%s code=%s ms=%d",
		e.Attempt, e.Phase, e.BotID, e.OfferID, e.Option, e.IdempotencyKey, e.Code, e.Ms)
	w.WriteHeader(http.StatusNoContent)
}

// logDecisionAnswer records one answer request as the route saw it: once on
// arrival, so a request the service never returns from still leaves a line,
// and once on return with how long the service held it and what it said.
func logDecisionAnswer(stage, bot, offer, option, key string, typed bool, ms int64, state string, err error) {
	code, field := "", ""
	var f *pa.Failure
	if errors.As(err, &f) {
		code, field = string(f.Code), f.Field
	} else if err != nil {
		code = string(pa.Code(err))
	}
	log.Printf("bots answer %s: bot=%s offer=%s option=%s typed=%t key=%s ms=%d state=%s code=%s field=%s",
		stage, bot, offer, option, typed, key, ms, state, code, field)
}
