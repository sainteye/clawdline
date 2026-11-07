package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// An Agent that needs the person waits on a decision, not on free text: a
// user_action without a decision is refused by a code that names the route,
// the edit accepts the decision's id, and the person's answer through the
// existing decision route ends the wait — the item's event records the answer
// and the owning Session is told which option was chosen.
func TestAnAgentWaitsOnTheDecisionThePersonAnswers(t *testing.T) {
	s, p, v := workV2AssignmentServer(t, session.StateIdle)
	ctx := context.Background()
	assigned, err := s.assignWorkV2(ctx, v.Item.ID, "local", v.Item.Version, "existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.acts = nil
	p.mu.Unlock()
	machine := access{machine: true, verdict: auth.Verdict{Allowed: true}}
	person := access{verdict: auth.Verdict{Allowed: true, Local: true}}
	do := func(a access, route http.HandlerFunc, method, target, key string, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, target, strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, a))
		rec := httptest.NewRecorder()
		route(rec, req)
		return rec
	}
	edit := func(key string, body map[string]any) *httptest.ResponseRecorder {
		body["session_id"] = p.s.ConversationID
		return do(machine, s.workV2Route, http.MethodPatch, "/v1/work/v2/agent/items/"+v.Item.ID+"/edit", key, body)
	}

	refused := edit("free-text", map[string]any{"expected_version": assigned.Item.Version,
		"condition": "waiting_user", "user_action": "Confirm the release window."})
	if refused.Code != http.StatusUnprocessableEntity || codeOf(t, refused) != "waiting_user_requires_decision" ||
		!strings.Contains(refused.Body.String(), "POST /v1/orchestrator/decisions") {
		t.Fatalf("free-text wait: %d %s", refused.Code, refused.Body)
	}

	opened := do(machine, s.sessionDecisionsRoute, http.MethodPost, "/v1/orchestrator/decisions", "ask", map[string]any{
		"session_id": p.s.ConversationID, "work_id": v.Item.ID, "question": "Is the release window confirmed?",
		"options": []map[string]string{{"id": "done", "label": "I've done it"}, {"id": "cannot", "label": "I can't"}},
		"default": "cannot", "due_in_minutes": 60})
	if opened.Code != http.StatusCreated {
		t.Fatalf("open decision: %d %s", opened.Code, opened.Body)
	}
	var decision struct {
		Decision struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"decision"`
	}
	if err := json.Unmarshal(opened.Body.Bytes(), &decision); err != nil {
		t.Fatal(err)
	}

	linked := edit("wait", map[string]any{"expected_version": assigned.Item.Version,
		"condition": "waiting_user", "decision_id": decision.Decision.ID})
	if linked.Code != http.StatusOK {
		t.Fatalf("link decision: %d %s", linked.Code, linked.Body)
	}
	var waiting struct {
		Item workV2ItemWire `json:"item"`
	}
	if err := json.Unmarshal(linked.Body.Bytes(), &waiting); err != nil {
		t.Fatal(err)
	}
	if waiting.Item.Condition == nil || *waiting.Item.Condition != "waiting_user" ||
		waiting.Item.DecisionID != decision.Decision.ID || waiting.Item.UserAction != "" {
		t.Fatalf("waiting item = %+v", waiting.Item)
	}
	// The same key answers the same stored edit.
	replay := edit("wait", map[string]any{"expected_version": assigned.Item.Version,
		"condition": "waiting_user", "decision_id": decision.Decision.ID})
	if replay.Code != http.StatusOK || replay.Body.String() != linked.Body.String() {
		t.Fatalf("replayed edit: %d %s", replay.Code, replay.Body)
	}

	answered := do(person, s.workDecisionsRoute, http.MethodPost, "/v1/work/decisions/"+decision.Decision.ID, "answer",
		map[string]any{"answer": "done"})
	if answered.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", answered.Code, answered.Body)
	}
	after, err := s.workV2().Item(ctx, v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Item.Condition != "" || after.Item.DecisionID != "" {
		t.Fatalf("answered item still waits: %+v", after.Item)
	}
	recorded := false
	for _, e := range after.Events {
		if e.Kind == "decision.closed" && strings.Contains(e.Payload, `"answer":"done"`) &&
			strings.Contains(e.Payload, `"label":"I've done it"`) && strings.Contains(e.Payload, `"state":"answered"`) {
			recorded = true
		}
	}
	if !recorded {
		t.Fatalf("no decision.closed event with the answer: %+v", after.Events)
	}
	sent := strings.Join(p.done(), "\n")
	if !strings.Contains(sent, "answered decision "+decision.Decision.ID) || !strings.Contains(sent, `"I've done it"`) ||
		!strings.Contains(sent, "option done") {
		t.Fatalf("owner notice = %q", sent)
	}
}

// The person can dismiss a stale decision without choosing an option. A body
// that is absent (as the Cloud bridge sends it) means the same as an empty
// object; the item stops waiting and the owning Session is told it was
// dismissed, not answered.
func TestThePersonDismissesADecisionWithoutAnswering(t *testing.T) {
	s, p, v := workV2AssignmentServer(t, session.StateIdle)
	ctx := context.Background()
	assigned, err := s.assignWorkV2(ctx, v.Item.ID, "local", v.Item.Version, "existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.acts = nil
	p.mu.Unlock()
	machine := access{machine: true, verdict: auth.Verdict{Allowed: true}}
	person := access{verdict: auth.Verdict{Allowed: true, Local: true}}
	do := func(a access, route http.HandlerFunc, method, target, key, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, a))
		rec := httptest.NewRecorder()
		route(rec, req)
		return rec
	}
	opened := do(machine, s.sessionDecisionsRoute, http.MethodPost, "/v1/orchestrator/decisions", "ask",
		`{"session_id":"`+p.s.ConversationID+`","work_id":"`+v.Item.ID+`","question":"Run the paid test?",`+
			`"options":[{"id":"yes","label":"Approve"},{"id":"no","label":"No paid calls"}],"default":"no","due_in_minutes":60}`)
	if opened.Code != http.StatusCreated {
		t.Fatalf("open decision: %d %s", opened.Code, opened.Body)
	}
	var decision struct {
		Decision struct {
			ID string `json:"id"`
		} `json:"decision"`
	}
	if err := json.Unmarshal(opened.Body.Bytes(), &decision); err != nil {
		t.Fatal(err)
	}
	id := decision.Decision.ID
	linked := do(machine, s.workV2Route, http.MethodPatch, "/v1/work/v2/agent/items/"+v.Item.ID+"/edit", "wait",
		`{"session_id":"`+p.s.ConversationID+`","expected_version":`+jsonNumber(assigned.Item.Version)+
			`,"condition":"waiting_user","decision_id":"`+id+`"}`)
	if linked.Code != http.StatusOK {
		t.Fatalf("link decision: %d %s", linked.Code, linked.Body)
	}

	withAnswer := do(person, s.workDecisionsRoute, http.MethodPost, "/v1/work/decisions/"+id+"/dismiss", "d0", `{"answer":"yes"}`)
	if withAnswer.Code != http.StatusBadRequest {
		t.Fatalf("dismissal carrying an answer: %d %s", withAnswer.Code, withAnswer.Body)
	}
	wrongMethod := do(person, s.workDecisionsRoute, http.MethodGet, "/v1/work/decisions/"+id+"/dismiss", "", "")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET dismiss: %d %s", wrongMethod.Code, wrongMethod.Body)
	}
	dismissed := do(person, s.workDecisionsRoute, http.MethodPost, "/v1/work/decisions/"+id+"/dismiss", "d1", "")
	if dismissed.Code != http.StatusOK || !strings.Contains(dismissed.Body.String(), `"state":"withdrawn"`) {
		t.Fatalf("dismiss: %d %s", dismissed.Code, dismissed.Body)
	}
	after, err := s.workV2().Item(ctx, v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Item.Condition != "" || after.Item.DecisionID != "" {
		t.Fatalf("dismissed item still waits: %+v", after.Item)
	}
	sent := strings.Join(p.done(), "\n")
	if !strings.Contains(sent, "dismissed decision "+id) || strings.Contains(sent, "Answer:") {
		t.Fatalf("owner notice = %q", sent)
	}
	again := do(person, s.workDecisionsRoute, http.MethodPost, "/v1/work/decisions/"+id+"/dismiss", "d2", "{}")
	if again.Code != http.StatusConflict || codeOf(t, again) != "decision_withdrawn" {
		t.Fatalf("second dismissal: %d %s", again.Code, again.Body)
	}
}

func jsonNumber(n int64) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}
