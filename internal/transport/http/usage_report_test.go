package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/contract"
)

// GET /v1/usage/work-samples answers one raw sample per child task created in
// the range, with its strata and its bill; a task the ledger has not read is
// not_yet_read, never zero tokens. A query it does not read is refused.
func TestUsageWorkSamplesAnswersRawSamples(t *testing.T) {
	u := newUsageFixture(t)
	u.seed()
	task := store.BrokerRow{ID: "task-1", Project: "/p", Assistant: "claude", State: "success", CreatedAt: u.at,
		UpdatedAt: u.at, SecretHash: "h", Record: json.RawMessage(`{"task_id":"task-1","kind":"feature","assistant":"claude",` +
			`"state":"success","claims":["web/console/src/a.ts","internal/app/a.go"],"created_at":"2026-09-21T00:00:00Z",` +
			`"finished_at":"2026-09-21T00:10:00Z"}`)}
	if err := u.s.store.SaveBrokerTask(context.Background(), task, nil); err != nil {
		t.Fatal(err)
	}
	code, body := u.get("/v1/usage/work-samples?since=1789000000", nil)
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, body)
	}
	var got contract.UsageWorkSamples
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Since != 1_789_000_000 || got.DefinitionVersion != 1 || len(got.Samples) != 2 {
		t.Fatalf("answer: %s", body)
	}
	byID := map[string]contract.UsageWorkSample{}
	for _, s := range got.Samples {
		byID[s.ID] = s
	}
	s := byID["task-1"]
	if s.Kind != "task" || s.WorkKind != "feature" || s.Scope != "1-3" || !s.CrossEnd || s.Ending != contract.UsageWorkEndingSuccess ||
		s.DurationSeconds != 600 || s.Calls != 12 || s.CallsAbove != 2 || s.Data != contract.UsageWorkDataComplete || !s.CostKnown {
		t.Fatalf("task-1: %+v", s)
	}
	unread := byID["task-2"]
	if unread.Data != contract.UsageWorkDataNotYetRead || unread.CostKnown || unread.Ending != contract.UsageWorkEndingRunning ||
		unread.WorkKind != "unspecified" {
		t.Fatalf("task-2: %+v", unread)
	}
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/v1/usage/work-samples?until=14d", http.StatusBadRequest},
		{"/v1/usage/work-samples?since=1789000000&until=1788000000", http.StatusBadRequest},
		{"/v1/usage/work-samples?limit=3", http.StatusBadRequest},
		{"/v1/usage/work-samples?until=1&until=2", http.StatusBadRequest},
	} {
		code, body := u.get(tc.path, nil)
		if code != tc.want {
			t.Errorf("%s: %d %s", tc.path, code, body)
		}
	}
	if code, body := u.get("/v1/usage/work-samples?since=1789000000&until=1789500000", nil); code != http.StatusOK ||
		!strings.Contains(string(body), `"samples":[]`) {
		t.Fatalf("an empty range: %d %s", code, body)
	}
}
