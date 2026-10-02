package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
)

// POST /v1/orchestrator/tasks/<id>/cancel: the root Session, or the person,
// and nobody else (brokerCancel, orchestrator.CancelTask).

func seedCancellable(t *testing.T, s *Server, id, root string) {
	t.Helper()
	r := orchestrator.Record{Protocol: orchestrator.Protocol, ID: id, Kind: "custom", Assistant: "claude",
		Title: "wrong brief", ProjectDir: "/p", State: orchestrator.StateBriefed, CreatedAt: time.Now(),
		Claims: []string{}, Root: &orchestrator.RootRef{SessionID: root, Assistant: "claude"}}
	body, _ := json.Marshal(r)
	if _, err := s.store.CreateBrokerTask(context.Background(), store.BrokerRow{ID: id, Project: "/p",
		Assistant: "claude", State: string(r.State), CreatedAt: r.CreatedAt,
		SecretHash: orchestrator.HashSecret("s"), Record: body}, nil); err != nil {
		t.Fatal(err)
	}
}

func cancelRequest(id, body, key string, who access) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/orchestrator/tasks/"+id+"/cancel", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	return req.WithContext(context.WithValue(req.Context(), accessKey{}, who))
}

func cancelRefusalOf(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error == nil {
		t.Fatalf("not a refusal: %d %s", rec.Code, rec.Body)
	}
	return body.Error
}

func TestTheCancelRouteAdmitsTheRootAndThePersonOnly(t *testing.T) {
	s, p := ownTodoServer(t)
	root := p.s.ConversationID
	session := access{machine: true, verdict: auth.Verdict{Allowed: true}}
	const id = "c4000000-0000-4000-8000-000000000001"
	seedCancellable(t, s, id, root)

	send := func(id, body, key string, who access) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.orchestratorTaskRoute(rec, cancelRequest(id, body, key, who))
		return rec
	}

	// Another Session is refused, with a code and a sentence that names who may.
	rec := send(id, `{"reason":"mine now","session_id":"20000000-0000-4000-8000-000000000009"}`, "k1", session)
	if e := cancelRefusalOf(t, rec); rec.Code != http.StatusForbidden || e["code"] != "not_task_root" ||
		!strings.Contains(e["message"].(string), root) {
		t.Fatalf("another Session: %d %v", rec.Code, e)
	}
	// A paired device that may only read is refused before the broker is asked.
	reader := access{verdict: auth.Verdict{Allowed: true, Device: "d1", Caps: auth.NewCaps(auth.Read)}}
	if rec := send(id, `{"reason":"r"}`, "k1", reader); rec.Code != http.StatusForbidden {
		t.Fatalf("a read-only device: %d %s", rec.Code, rec.Body)
	}
	// A field the route does not take is refused, not ignored.
	if rec := send(id, `{"reason":"r","state":"success"}`, "k1", session); rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown field: %d %s", rec.Code, rec.Body)
	}

	// The root.
	rec = send(id, `{"reason":"wrong brief","session_id":"`+root+`"}`, "k1", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("the root: %d %s", rec.Code, rec.Body)
	}
	var got contract.BrokerCancelResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || !got.OK || got.Replayed ||
		got.Task.State != contract.TaskStateCancelled || got.Task.Verdict != "Cancelled by its root: wrong brief" {
		t.Fatalf("the root's answer: %+v %v", got, err)
	}

	// The same request again is the same success, replayed.
	rec = send(id, `{"reason":"wrong brief","session_id":"`+root+`"}`, "k1", session)
	if err := json.Unmarshal(rec.Body.Bytes(), &got); rec.Code != http.StatusOK || err != nil || !got.Replayed {
		t.Fatalf("the resend: %d %s", rec.Code, rec.Body)
	}
	// A different one is a conflict that names the state.
	rec = send(id, `{"reason":"wrong brief","session_id":"`+root+`"}`, "k2", session)
	if e := cancelRefusalOf(t, rec); rec.Code != http.StatusConflict || e["code"] != "task_already_terminal" ||
		e["state"] != "cancelled" {
		t.Fatalf("a second cancel: %d %v", rec.Code, e)
	}

	// The person, from a paired device that may send — and naming a Session
	// is not how a person speaks.
	const other = "c4000000-0000-4000-8000-000000000002"
	seedCancellable(t, s, other, root)
	sender := access{verdict: auth.Verdict{Allowed: true, Device: "d2", Caps: auth.NewCaps(auth.Read, auth.Send)}}
	if rec := send(other, `{"reason":"r","session_id":"`+root+`"}`, "k3", sender); rec.Code != http.StatusBadRequest {
		t.Fatalf("a person naming a Session: %d %s", rec.Code, rec.Body)
	}
	rec = send(other, `{"reason":"not needed"}`, "k3", sender)
	if err := json.Unmarshal(rec.Body.Bytes(), &got); rec.Code != http.StatusOK || err != nil ||
		got.Task.Verdict != "Cancelled by the person: not needed" {
		t.Fatalf("the person: %d %s", rec.Code, rec.Body)
	}
}
