package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/schedulewebhook"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestAnAgentCreatesAWebhookOnlyOnThePersonsRun(t *testing.T) {
	s := cloudStandIn(t)
	const conversation = "c6000003-0000-4000-8000-000000000003"
	run, err := s.server.runs().Issue(context.Background(), session.Session{ID: "terminal-3", ConversationID: conversation}, "local", "")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.server.scheduleBook().CreateWebhook = func(ctx context.Context, key string) (schedulewebhook.Created, error) {
		calls++
		if key != "create-one" {
			t.Fatalf("key = %q", key)
		}
		var made schedulewebhook.Created
		made.Hook.ID = "swh_test"
		made.PublicURL = "https://api.example.test/secret"
		return made, nil
	}
	path := "/v1/orchestrator/schedule-webhooks/create"
	for _, body := range []map[string]any{{}, {"session_id": conversation, "via": map[string]any{"run": "invented"}}, {"session_id": "c6000003-0000-4000-8000-000000000004", "via": map[string]any{"run": run.ID}}} {
		r := s.orchestratorWrite(t, http.MethodPost, path, "create-one", body)
		if r.Code < 400 || calls != 0 {
			t.Fatalf("unauthorized create: %d calls=%d", r.Code, calls)
		}
	}
	body := map[string]any{"session_id": conversation, "via": map[string]any{"run": run.ID}}
	r := s.orchestratorWrite(t, http.MethodPost, path, "create-one", body)
	if r.Code != 200 || calls != 1 || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("create: %d %s calls=%d", r.Code, r.Body, calls)
	}
	var made schedulewebhook.Created
	if json.Unmarshal(r.Body.Bytes(), &made) != nil || !strings.Contains(made.PublicURL, "/secret") {
		t.Fatal("missing direct secret response")
	}
	// The response is not receipted locally: Cloud owns idempotency and must
	// replay without its secret, even after a daemon restart.
	s.orchestratorWrite(t, http.MethodPost, path, "create-one", body)
	if calls != 2 {
		t.Fatal("creation secret was cached in a local receipt")
	}
}
