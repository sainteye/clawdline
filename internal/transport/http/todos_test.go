package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/domain/work"
)

// The to-do list is the session's, read with the machine token that
// dispatched the work — never a device's, and never written through a route.
func TestASessionsTodosAreReadWithTheMachineToken(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	const conv = "379d443c-db38-4d17-8ce2-b273dc748ea0"
	id := "70d0e001-0000-4000-8000-000000000001"
	at := time.Unix(1_789_700_000, 0)
	todo := work.Todo{ID: work.TodoID(work.OriginDispatch, id), Origin: work.OriginDispatch, Task: id,
		Title: "t", Owner: conv, OwnerAssistant: "claude", State: work.TodoStateOpen,
		Reason: work.ReasonDispatched, CreatedAt: at, UpdatedAt: at}
	if _, err := st.CreateBrokerTaskTx(ctx, store.BrokerRow{ID: id, Project: "/p", Assistant: "claude",
		State: "briefed", CreatedAt: at, SecretHash: "h",
		Record: []byte(`{"task_id":"` + id + `","kind":"custom","state":"briefed"}`)}, nil, nil,
		func(tx *store.Tx) error { return tx.PutTodo(todo, nil) }); err != nil {
		t.Fatal(err)
	}
	s := &Server{broker: &orchestrator.Broker{Store: st, Tasks: taskdir.New(dir), Dir: dir}, store: st}
	get := func(target string, machine bool, method string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{machine: machine}))
		rec := httptest.NewRecorder()
		s.brokerSessionRoute(rec, req)
		return rec
	}
	path := "/v1/orchestrator/sessions/" + conv + "/todos"
	if rec := get(path, false, http.MethodGet); rec.Code != http.StatusForbidden {
		t.Fatalf("without the machine token: %d %s", rec.Code, rec.Body)
	}
	rec := get(path, true, http.MethodGet)
	if rec.Code != http.StatusOK {
		t.Fatalf("with it: %d %s", rec.Code, rec.Body)
	}
	var body struct {
		OK        bool           `json:"ok"`
		SessionID string         `json:"session_id"`
		State     string         `json:"state"`
		Counts    map[string]int `json:"counts"`
		Todos     []struct {
			ID         string   `json:"id"`
			TaskID     string   `json:"task_id"`
			WorkID     *string  `json:"work_id"`
			State      string   `json:"state"`
			Reason     string   `json:"reason"`
			ClosedAt   *int64   `json:"closed_at"`
			Escalation []string `json:"escalation"`
		} `json:"todos"`
		NextCursor *string `json:"next_cursor"`
		PageSize   int     `json:"page_size"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || body.SessionID != conv || body.State != "outstanding" || len(body.Todos) != 1 ||
		body.Counts["open"] != 1 || body.Counts["done"] != 0 || body.NextCursor != nil || body.PageSize == 0 {
		t.Fatalf("answer %s", rec.Body)
	}
	got := body.Todos[0]
	if got.ID != todo.ID || got.TaskID != id || got.WorkID != nil || got.State != "open" || got.ClosedAt != nil ||
		len(got.Escalation) != 1 || got.Escalation[0] != "cross_session" {
		t.Fatalf("row %s", rec.Body)
	}
	if rec := get(path+"?state=closed", true, http.MethodGet); rec.Code != http.StatusOK {
		t.Fatalf("closed: %d", rec.Code)
	}
	if rec := get(path+"?limit=5", true, http.MethodGet); rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown parameter answered %d", rec.Code)
	}
	// Nothing writes a to-do through a route.
	if rec := get(path, true, http.MethodPost); rec.Code != http.StatusNotFound {
		t.Fatalf("a POST answered %d", rec.Code)
	}
}
