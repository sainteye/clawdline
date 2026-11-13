package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// `clawdline session smart-title` runs with this machine's orchestrator token.
// On 2026-11-13 it was first sent to /v1/sessions/<id>/smart-title, which
// answered 403 to that token: the command could never rename anything.
func TestAnAgentSmartNamesItsSessionThroughTheOrchestratorRoute(t *testing.T) {
	p := touchPane{&pane{s: session.Session{ID: "%19", Backend: session.BackendTmux, CWD: t.TempDir(),
		Assistant: session.AssistantClaude, State: session.StateIdle}, screen: "> "}}
	s, handler, _, machine := wholeServer(t, p, p)
	s.firstSessionRequest = func(session.Session) (string, error) { return "Swap the API and finish the UI", nil }
	s.nameSession = func(context.Context, string, string) (string, error) { return "Swap the API and finish the UI", nil }
	ask := func(path, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Host = "127.0.0.1:7757"
		req.Header.Set("X-Clawdline-Orchestrator", machine)
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := ask("/v1/sessions/%2519/smart-title", "k1"); rec.Code != http.StatusForbidden {
		t.Fatalf("the paired-device route answered the agent token %d %s", rec.Code, rec.Body)
	}
	if rec := ask("/v1/orchestrator/sessions/%2519/smart-title", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("a smart-title without its key answered %d %s", rec.Code, rec.Body)
	}
	rec := ask("/v1/orchestrator/sessions/%2519/smart-title", "k2")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Swap the API and finish the UI") {
		t.Fatalf("the agent route did not name the Session: %d %s", rec.Code, rec.Body)
	}
}
