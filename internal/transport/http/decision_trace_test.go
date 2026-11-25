package http

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/auth"
)

func TestDecisionTraceLogsOnlyIdentifiers(t *testing.T) {
	var logged bytes.Buffer
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(&logged)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prev); log.SetFlags(flags) })

	post := func(body string, allowed bool) int {
		r := httptest.NewRequest(http.MethodPost, decisionTracePath, strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), accessKey{}, access{verdict: auth.Verdict{Allowed: allowed}}))
		w := httptest.NewRecorder()
		decisionTrace(w, r)
		return w.Code
	}
	good := `{"attempt":"dt-1","phase":"settled","bot_id":"bot-a","offer_id":"decision-abc","option":"decision-2","idempotency_key":"k-1","code":"","ms":812}`
	if got := post(good, true); got != http.StatusNoContent {
		t.Fatalf("a well-formed step answered %d", got)
	}
	want := "bots decision trace: attempt=dt-1 phase=settled bot=bot-a offer=decision-abc option=decision-2 key=k-1 code= ms=812\n"
	if logged.String() != want {
		t.Fatalf("logged %q, want %q", logged.String(), want)
	}
	logged.Reset()
	for name, body := range map[string]string{
		"reply text":    strings.Replace(good, `"code":""`, `"code":"先等 7c4e7ff3"`, 1),
		"unknown step":  strings.Replace(good, `"settled"`, `"typed"`, 1),
		"extra field":   strings.Replace(good, `"ms":812`, `"ms":812,"text":"hi"`, 1),
		"newline split": strings.Replace(good, `"bot-a"`, `"bot-a\nforged"`, 1),
		"too large":     strings.Replace(good, `"dt-1"`, `"`+strings.Repeat("a", maxDecisionTraceBodyBytes)+`"`, 1),
	} {
		if got := post(body, true); got < 400 {
			t.Errorf("%s: answered %d", name, got)
		}
	}
	if got := post(good, false); got != http.StatusUnauthorized {
		t.Errorf("an unauthenticated step answered %d", got)
	}
	if logged.Len() != 0 {
		t.Fatalf("a refused step was logged: %q", logged.String())
	}
}
