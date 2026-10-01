package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func attentionWire(t *testing.T, s *Server, items ...session.Session) []map[string]any {
	t.Helper()
	snapshot := s.sessionsPayloadFrom(t.Context(), session.Inventory{Complete: true, Provenance: "test", Sessions: items})
	out := make([]map[string]any, len(snapshot.Sessions))
	for i, row := range snapshot.Sessions {
		data, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestSessionAttentionCountsFollowNoteActionsAndDistinguishZero(t *testing.T) {
	s, source, target := humanInterventionTestServer(t)
	row := session.Session{ID: "pane-target", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		ConversationID: target, Binding: session.BindingCommandLine, State: session.StateIdle}
	if count := attentionWire(t, s, row)[0]["attention_count"]; count != float64(0) {
		t.Fatalf("successful empty count = %v, want JSON zero", count)
	}
	create := httptest.NewRecorder()
	s.workV2Route(create, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/human-interventions", humanInterventionBody(source), "attention-create"))
	if create.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", create.Code, create.Body)
	}
	var result struct {
		Note humanInterventionWire `json:"note"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if count := attentionWire(t, s, row)[0]["attention_count"]; count != float64(1) {
		t.Fatalf("open count = %v", count)
	}
	path := "/v1/work/v2/human-interventions/conversation:" + target + "/" + result.Note.ID
	resolve := httptest.NewRecorder()
	s.workV2Route(resolve, personWorkV2Request(http.MethodPost, path+"/resolve", fmt.Sprintf(`{"expected_version":%d}`, result.Note.Version), "attention-resolve"))
	if resolve.Code != http.StatusOK {
		t.Fatalf("resolve: %d %s", resolve.Code, resolve.Body)
	}
	if err := json.Unmarshal(resolve.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if count := attentionWire(t, s, row)[0]["attention_count"]; count != float64(0) {
		t.Fatalf("resolved count = %v", count)
	}
	reopen := httptest.NewRecorder()
	s.workV2Route(reopen, personWorkV2Request(http.MethodPost, path+"/reopen", fmt.Sprintf(`{"expected_version":%d}`, result.Note.Version), "attention-reopen"))
	if reopen.Code != http.StatusOK {
		t.Fatalf("reopen: %d %s", reopen.Code, reopen.Body)
	}
	if count := attentionWire(t, s, row)[0]["attention_count"]; count != float64(1) {
		t.Fatalf("reopened count = %v", count)
	}
}

func TestSessionAttentionDoesNotClaimAmbiguousOrStaleIdentity(t *testing.T) {
	s, _, target := humanInterventionTestServer(t)
	base := session.Session{ID: "a", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		ConversationID: target, Binding: session.BindingCommandLine, State: session.StateIdle}
	withoutCount := func(label string, items ...session.Session) {
		t.Helper()
		for _, row := range attentionWire(t, s, items...) {
			if _, ok := row["attention_count"]; ok {
				t.Fatalf("%s claimed a count: %v", label, row)
			}
		}
	}
	otherAssistant := base
	otherAssistant.ID = "b"
	otherAssistant.Assistant = session.AssistantCodex
	withoutCount("cross-assistant duplicate", base, otherAssistant)
	sameAssistant := base
	sameAssistant.ID = "c"
	withoutCount("same-assistant duplicate", base, sameAssistant)
	unbound := base
	unbound.ConversationID = ""
	unbound.Binding = session.BindingNoRecord
	withoutCount("unbound", unbound)
	unverified := base
	unverified.Observation = session.Observation{Freshness: session.FreshnessUnverified}
	withoutCount("unverified", unverified)
	missing := base
	missing.Observation = session.Observation{Freshness: session.FreshnessMissing}
	withoutCount("missing", missing)
	if err := s.store.Close(); err != nil {
		t.Fatal(err)
	}
	withoutCount("failed note read", base)
}
