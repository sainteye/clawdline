package http

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"testing"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
)

// GET /v1/usage/work-units answers each unit's cursors and delta, read like
// the other usage routes; a query it does not read is refused, not ignored.
// A task whose start cursor was never taken is not a delta from zero
// presented as whole: it says cursor_missing and has no cost.
func TestUsageWorkUnitsAnswersTheUnitsAndTheirCursors(t *testing.T) {
	u := newUsageFixture(t)
	t.Cleanup(func() { workUnitsByServer.Delete(u.s) })
	u.seed()
	if err := u.s.workUnits().Record(context.Background(), app.WorkUnitEvent{Kind: app.WorkUnitTask, ID: "task-1",
		Edge: "end", Outcome: "failure", At: u.at}); err != nil {
		t.Fatal(err)
	}
	code, body := u.get("/v1/usage/work-units?since=1789000000", nil)
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, body)
	}
	var got contract.UsageWorkUnits
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Since != 1_789_000_000 || len(got.Units) != 1 {
		t.Fatalf("answer: %s", body)
	}
	unit := got.Units[0]
	if unit.Kind != contract.UsageWorkKindTask || unit.ID != "task-1" || unit.Outcome != "failure" ||
		unit.CostKnown || unit.Cost != nil || len(unit.Sessions) != 1 || len(unit.Cursors) != 2 {
		t.Fatalf("unit: %s", body)
	}
	missing := false
	for _, s := range unit.States {
		missing = missing || s == contract.UsageWorkStateCursorMissing
	}
	if !missing {
		t.Fatalf("states: %v", unit.States)
	}
	// sess-read's own 9 dollars' worth at ten tokens a dollar, and its subagent's 1.
	if math.Abs(unit.Tokens.Total-100) > 1e-9 {
		t.Fatalf("tokens: %+v", unit.Tokens)
	}
	var reading *contract.UsageWorkReading
	for _, c := range unit.Cursors {
		if c.Session == "sess-read" {
			reading = c.Reading
		}
	}
	if reading == nil || !reading.Present || reading.Calls != 15 || reading.PeakContext != 150_000 {
		t.Fatalf("reading: %s", body)
	}

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/v1/usage/work-units?since=14w", http.StatusBadRequest},
		{"/v1/usage/work-units?limit=3", http.StatusBadRequest},
		{"/v1/usage/work-units?since=1d&since=2d", http.StatusBadRequest},
	} {
		code, body := u.get(tc.path, nil)
		if code != tc.want {
			t.Errorf("%s: %d %s", tc.path, code, body)
		}
	}
	if code, _ := u.get("/v1/usage/work-units", map[string]string{}); code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", code)
	}
}
