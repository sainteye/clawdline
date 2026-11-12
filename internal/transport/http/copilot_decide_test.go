package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/config"
)

func fakeDecision(t *testing.T, content string) *[]map[string]any {
	t.Helper()
	var seen []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer "+testCopilotKey {
			t.Errorf("decided at %s with %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		seen = append(seen, body)
		reply, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": content}}}})
		_, _ = w.Write(reply)
	}))
	was := copilotOpenAI
	copilotOpenAI = srv.URL
	t.Cleanup(func() { copilotOpenAI = was; srv.Close() })
	return &seen
}

var decisionSessions = []copilotDecisionSession{
	{ID: "%2", Label: "Prepare screenshots", State: "idle", Assistant: "codex"},
	{ID: "owned:acp:AAAA", Label: "Pick two outfits and make clean product photos", State: "idle", Assistant: "grok"},
}

func TestADecisionKeepsOnlySessionsFromTheMachinesOwnList(t *testing.T) {
	seen := fakeDecision(t, `{"id":"owned:acp:AAAA","candidates":["%2","%88"]}`)
	got, _, detail := decideCopilotSession(context.Background(), testCopilotKey, "the clothes photo one", decisionSessions)
	if detail != "" || got.ID != "owned:acp:AAAA" || len(got.Candidates) != 1 || got.Candidates[0] != "%2" || got.Many {
		t.Fatalf("%+v %q", got, detail)
	}
	sent := (*seen)[0]
	if sent["model"] != copilotDecisionModel {
		t.Fatalf("model %v", sent["model"])
	}
	msgs, _ := sent["messages"].([]any)
	user, _ := msgs[1].(map[string]any)
	if !strings.Contains(user["content"].(string), "the clothes photo one") || !strings.Contains(user["content"].(string), "owned:acp:AAAA") {
		t.Fatalf("the decision was not shown the sentence and the list: %v", user["content"])
	}

	// An id the model invents is not a decision.
	fakeDecision(t, `{"id":"%88","candidates":[]}`)
	got, _, _ = decideCopilotSession(context.Background(), testCopilotKey, "that one", decisionSessions)
	if got.ID != "" || len(got.Candidates) != 0 {
		t.Fatalf("an unknown id survived: %+v", got)
	}
}

func TestADecisionRefusesWithoutASentenceOrAKey(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	for body, code := range map[string]string{
		`{"said":""}`: "said_missing",
		`not json`:    "invalid_body",
		`{"said":"` + strings.Repeat("字", maxCopilotDecisionSaidRunes+1) + `"}`: "said_too_long",
		`{"said":"the clothes photo one"}`:                                      "copilot_key_missing",
	} {
		rec := copilotCall(t, s, s.copilotDecideRoute, http.MethodPost, "/v1/voice/copilot-decide", body)
		if !strings.Contains(rec.Body.String(), code) {
			t.Errorf("%.40s: %d %s, want %s", body, rec.Code, rec.Body, code)
		}
	}
}

func TestADecisionOnSeveralSessionsSaysSo(t *testing.T) {
	fakeDecision(t, `{"id":null,"candidates":["%2","owned:acp:AAAA"],"many":true}`)
	got, _, _ := decideCopilotSession(context.Background(), testCopilotKey, "what did the finished ones do", decisionSessions)
	if !got.Many || len(got.Candidates) != 2 {
		t.Fatalf("%+v", got)
	}
}
