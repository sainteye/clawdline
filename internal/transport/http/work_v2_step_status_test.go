package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestAgentStepCancellationHasDurableDistinctStateAndReplay(t *testing.T) {
	s, p, v := workV2AssignmentServer(t, session.StateWorking)
	_, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version, "existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	base := "/v1/work/v2/agent/items/" + v.Item.ID
	post := func(path, body, key string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		s.workV2Route(rec, agentWorkV2Request(http.MethodPost, base+path, body, key))
		return rec
	}
	body := `{"session_id":"` + p.s.ConversationID + `"}`
	rec := post("/steps", `{"session_id":"`+p.s.ConversationID+`","title":"取消不再需要的處理"}`, "add-cancel-step")
	if rec.Code != 201 {
		t.Fatalf("add: %d %s", rec.Code, rec.Body)
	}
	full, err := s.workV2().Item(context.Background(), v.Item.ID)
	if err != nil || len(full.Steps) != 1 {
		t.Fatalf("read: %+v %v", full.Steps, err)
	}
	path := "/steps/" + full.Steps[0].ID
	if r := post(path+"/cancel", body, "missing-reason"); r.Code == 200 {
		t.Fatal("cancelled without reason")
	}
	cancel := `{"session_id":"` + p.s.ConversationID + `","reason":"已與使用者確認不再需要"}`
	first := post(path+"/cancel", cancel, "cancel-step")
	if first.Code != 200 {
		t.Fatalf("cancel: %d %s", first.Code, first.Body)
	}
	again := post(path+"/cancel", cancel, "cancel-step")
	if again.Code != first.Code || again.Body.String() != first.Body.String() {
		t.Fatalf("replay: %d %s", again.Code, again.Body)
	}
	// All read paths project cancellation, including the batched Session view.
	full, err = s.workV2().Item(context.Background(), v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	relations, err := s.store.WorkV2Relations(context.Background(), []string{v.Item.ID})
	if err != nil {
		t.Fatal(err)
	}
	if rows := relations.Steps[v.Item.ID]; len(rows) != 1 || rows[0].Status() != "cancelled" || rows[0].Done {
		t.Fatalf("batch projection: %+v", rows)
	}
	wire := s.workV2ItemProjector(context.Background())(full)
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	var projected struct {
		Steps []workV2StepWire `json:"steps"`
	}
	if err := json.Unmarshal(data, &projected); err != nil {
		t.Fatal(err)
	}
	if len(projected.Steps) != 1 || projected.Steps[0].Status != "cancelled" || projected.Steps[0].Done || projected.Steps[0].CompletedAt != nil || projected.Steps[0].CancelledAt == nil || projected.Steps[0].CancelReason == "" {
		t.Fatalf("wire: %s", data)
	}
	stale := fmt.Sprintf(`{"session_id":%q,"expected_version":1}`, p.s.ConversationID)
	if r := post(path+"/reopen", stale, "stale-reopen"); r.Code != 409 {
		t.Fatalf("stale: %d %s", r.Code, r.Body)
	}
	if r := post(path+"/complete", body, "complete-cancelled"); r.Code == 200 {
		t.Fatal("completed cancelled step")
	}
	if r := post(path+"/reopen", body, "reopen-step"); r.Code != 200 {
		t.Fatalf("reopen: %d %s", r.Code, r.Body)
	}
	if r := post(path+"/complete", body, "complete-restored"); r.Code != 200 {
		t.Fatalf("complete restored: %d %s", r.Code, r.Body)
	}
	// The old cancellation receipt is a replay, not an instruction to cancel again.
	if r := post(path+"/cancel", cancel, "cancel-step"); r.Body.String() != first.Body.String() {
		t.Fatalf("old receipt changed: %s", r.Body)
	}
	full, err = s.workV2().Item(context.Background(), v.Item.ID)
	if err != nil || !full.Steps[0].Done || !full.Steps[0].CancelledAt.IsZero() {
		t.Fatalf("replay changed live state: %+v %v", full.Steps, err)
	}
}
