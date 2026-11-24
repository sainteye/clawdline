package http

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A child's /complete is a task-secret route: the gate admits it without a
// person and puts that unallowed verdict in the request context. Read as a
// person's, it is refused as pa_revoked — the outcome every Bot review turn
// recorded on 2026-11-24. The broker's delivery hands the host the same
// request context without its values, which reads as the host's own.
func TestAChildsCompletionRequestLendsTheBotHostNoPerson(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	var inRequest, delivered error
	h := f.s.gate().wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, inRequest = f.s.projectAssistantSource(r.Context(), f.key, true)
		_, delivered = f.s.projectAssistantSource(orchestrator.HostContext(r.Context()), f.key, true)
	}))
	r := httptest.NewRequest("POST", "/v1/orchestrator/tasks/synthetic/complete", strings.NewReader("{}"))
	r.Host = "127.0.0.1"
	r.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(httptest.NewRecorder(), r)
	var failure *pa.Failure
	if pa.Code(inRequest) != pa.Revoked || !errors.As(inRequest, &failure) || failure.Field != "gate_proof" {
		t.Fatalf("the request's own verdict was not refused as the incident's pa_revoked: %v", inRequest)
	}
	if delivered != nil {
		t.Fatalf("the host's delivery still read the child's request as a person: %v", delivered)
	}
}
