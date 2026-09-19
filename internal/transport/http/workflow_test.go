package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
)

// W8: the routes the Swift app's sessions still call, as this daemon answers
// them before that app is stopped.

func w8Server(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &Server{broker: &orchestrator.Broker{Store: st, Tasks: taskdir.New(dir), Dir: dir}, store: st}
}

func w8Request(method, target, body string, machine bool, header map[string]string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range header {
		req.Header.Set(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), accessKey{}, access{machine: machine}))
}

// The helper's begin and deliver are answered, not refused, and nothing is
// recorded for them — and the answer cannot be read as the Swift app's
// receipt, which was a 202 with `workflow_<operation>_recorded` and an item.
// Before W8 this path was "404 that is not a session action", which the
// helper prints as a broken step in every session still running it.
func TestTheRetiredWorkflowStepIsAnsweredAndRecordsNothing(t *testing.T) {
	s := w8Server(t)
	writesBefore := s.store.Stats().Writes
	key := map[string]string{"Idempotency-Key": "k-1", "Content-Type": "application/json"}
	for _, body := range []string{
		`{"operation":"begin","run_id":"run-1","classification":"new_work","title":"t","type":"task","phase":"output"}`,
		`{"operation":"deliver","run_id":"run-1","disposition":"delivered","summary":"s","next_action":"none","remaining":[]}`,
	} {
		rec := httptest.NewRecorder()
		s.brokerSessionRoute(rec, w8Request(http.MethodPost, "/v1/orchestrator/sessions/%2542/workflow", body, true, key))
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		wf, _ := got["workflow"].(map[string]any)
		if got["ok"] != true || wf["code"] != "workflow_retired" || wf["recorded"] != false ||
			wf["authority"] != "none" || wf["status"] != float64(200) || wf["run_id"] != "run-1" ||
			wf["outbox_pending"] != float64(0) {
			t.Fatalf("answer %s", rec.Body)
		}
		// The keys the helper's readers look for are present, and empty.
		for _, k := range []string{"item_id", "project_id"} {
			if v, present := wf[k]; !present || v != nil {
				t.Fatalf("%s: %v (present %v)", k, v, present)
			}
		}
		if strings.Contains(rec.Body.String(), "_recorded") {
			t.Fatalf("an answer that reads as a receipt: %s", rec.Body)
		}
	}
	if w := s.store.Stats().Writes; w != writesBefore {
		t.Fatalf("the retired step wrote %d time(s)", w-writesBefore)
	}
	if d := s.retired.diagnostics(); d.Calls != 2 || d.LastAt == 0 {
		t.Fatalf("counted %+v", d)
	}

	// What the Swift route refused is still refused, by the same sentence.
	rec := httptest.NewRecorder()
	s.brokerSessionRoute(rec, w8Request(http.MethodPost, "/v1/orchestrator/sessions/%2542/workflow", `{}`, true, nil))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Idempotency-Key are required") {
		t.Fatalf("no key: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	s.brokerSessionRoute(rec, w8Request(http.MethodPost, "/v1/orchestrator/sessions/%2542/workflow", `not json`, true, key))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("not json: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	s.brokerSessionRoute(rec, w8Request(http.MethodPost, "/v1/orchestrator/sessions/%2542/workflow", `{}`, false, key))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no machine credential: %d %s", rec.Code, rec.Body)
	}
	if d := s.retired.diagnostics(); d.Calls != 2 {
		t.Fatalf("a refused command was counted: %+v", d)
	}
}

// The address book is the orchestrator token's; the durable-report promotion
// this daemon does not keep is refused by name, not by the fallback.
func TestTheAddressBookAndThePromotionAreRefusedByName(t *testing.T) {
	s := w8Server(t)
	rec := httptest.NewRecorder()
	s.brokerAddressBook(rec, w8Request(http.MethodGet, "/v1/orchestrator/sessions", "", false, nil))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "a wait can name") {
		t.Fatalf("address book without the token: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	s.brokerDurableReportPromotion(rec, w8Request(http.MethodPost, "/v1/orchestrator/durable-reports/promotions", `{}`, true, nil))
	if rec.Code != http.StatusNotImplemented || !strings.Contains(rec.Body.String(), `"durable_report_promotion_unsupported"`) {
		t.Fatalf("promotion: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	s.brokerDurableReportPromotion(rec, w8Request(http.MethodPost, "/v1/orchestrator/durable-reports/promotions", `{}`, false, nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("promotion without the token: %d %s", rec.Code, rec.Body)
	}
}
