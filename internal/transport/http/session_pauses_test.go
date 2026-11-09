package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestPeerSessionCanCoordinateWithoutClawdfather(t *testing.T) {
	const requester = "11111111-1111-4111-8111-111111111111"
	const target = "22222222-2222-4222-8222-222222222222"
	const unrelated = "44444444-4444-4444-8444-444444444444"
	const request = "33333333-3333-4333-8333-333333333333"
	p := &pane{s: session.Session{ID: "%2", Backend: session.BackendTmux, Assistant: session.AssistantCodex, ConversationID: target}}
	s := paneServer(t, p)
	s.broker.Live = func(context.Context) []session.Session {
		return []session.Session{
			p.s, {ID: "%1", Assistant: session.AssistantCodex, ConversationID: requester},
			{ID: "%3", Assistant: session.AssistantClaude, ConversationID: unrelated},
		}
	}
	lines := 0
	s.broker.Type = func(context.Context, string, string) error { lines++; return nil }
	f, _ := newGateFixture(t)
	handler := f.g.wrap(http.HandlerFunc(s.sessionPausesRoute))
	send := func(path string, body map[string]string, want int) {
		t.Helper()
		raw, _ := json.Marshal(body)
		rec := (call{path: path, body: string(raw), headers: map[string]string{"Content-Type": "application/json", "X-Clawdline-Orchestrator": f.machine}}).do(handler)
		if rec.Code != want {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	send("/v1/orchestrator/pauses", map[string]string{"request_id": request, "requester_session_id": requester, "target_session_id": target, "reason": "Prepare a maintenance window", "wake_condition": "lease:daemon_restart"}, 200)
	send("/v1/orchestrator/pauses/"+request+"/observed", map[string]string{"session_id": target}, 200)
	send("/v1/orchestrator/pauses/"+request+"/safe", map[string]string{"session_id": target}, 200)
	for _, actor := range []string{target, unrelated} {
		send("/v1/orchestrator/pauses/"+request+"/wake", map[string]string{"session_id": actor}, 403)
		send("/v1/orchestrator/pauses/"+request+"/retry", map[string]string{"session_id": actor}, 403)
	}
	send("/v1/orchestrator/pauses/"+unrelated+"/wake", map[string]string{"session_id": requester}, 404)
	send("/v1/orchestrator/pauses/"+request+"/retry", map[string]string{"session_id": requester}, 200)
	send("/v1/orchestrator/pauses/"+request+"/wake", map[string]string{"session_id": requester}, 200)
	send("/v1/orchestrator/pauses/"+request+"/resumed", map[string]string{"session_id": target}, 200)
	if lines != 2 {
		t.Fatalf("notice count = %d", lines)
	}
}
