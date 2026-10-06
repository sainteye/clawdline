//go:build darwin || linux

package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/git"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
)

// POST /v1/orchestrator/callbacks (brokerCallback, orchestrator.StartCallback).
func TestTheCallbackRouteStartsACommandForTheOrchestratorTokenOnly(t *testing.T) {
	s, p := ownTodoServer(t)
	s.broker.Git = git.New()
	dir := t.TempDir()
	body := func(extra string) string {
		return `{"task_id":"c5000000-0000-4000-8000-000000000001","title":"The deploy is live","argv":["true"],` +
			`"dir":"` + dir + `","root":{"session_id":"` + p.s.ConversationID + `","assistant":"claude"}` + extra + `}`
	}
	send := func(method, b string, who access) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/v1/orchestrator/callbacks", strings.NewReader(b))
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, who))
		rec := httptest.NewRecorder()
		s.brokerCallback(rec, req)
		return rec
	}
	machine := access{machine: true, verdict: auth.Verdict{Allowed: true}}
	device := access{verdict: auth.Verdict{Allowed: true, Device: "d1", Caps: auth.NewCaps(auth.Read, auth.Send)}}

	if rec := send(http.MethodGet, "", machine); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", rec.Code)
	}
	if rec := send(http.MethodPost, body(""), device); rec.Code != http.StatusForbidden {
		t.Fatalf("a paired device: %d %s", rec.Code, rec.Body)
	}
	// A field the route does not take — a credential passed as env among
	// them — is refused, not ignored.
	if rec := send(http.MethodPost, body(`,"secret_env":{"TOKEN":"x"}`), machine); rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown field: %d %s", rec.Code, rec.Body)
	}
	rec := send(http.MethodPost, body(""), machine)
	var got contract.BrokerDispatchResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Task.ID != "c5000000-0000-4000-8000-000000000001" {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	rec = send(http.MethodPost, body(""), machine)
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || !got.Replayed {
		t.Fatalf("a resend was not a replay: %d %s", rec.Code, rec.Body)
	}
}
