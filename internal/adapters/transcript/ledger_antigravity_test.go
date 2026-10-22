package transcript

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAntigravityLedgerCountsEachNativePlannerOnceAcrossRestarts(t *testing.T) {
	_, path := antigravityTestPath(t)
	initial := antigravityTestUser(0, "synthetic ledger sample") + antigravityTestPlanner(1, "first", 11, 3, 2)
	writeAntigravityTest(t, path, initial)
	var state LedgerState
	if _, err := state.Feed(path); err != nil {
		t.Fatal(err)
	}
	if state.Assistant != "antigravity" || state.Calls != 1 || state.Measured.Input != 11 || state.Measured.Output != 3 || state.Measured.CacheRead != 2 {
		t.Fatalf("state=%+v", state)
	}
	if _, err := state.Feed(path); err != nil {
		t.Fatal(err)
	}
	if state.Calls != 1 || state.Measured.Total() != 16 {
		t.Fatal("repeated snapshot accumulated counters")
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var restored LedgerState
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	writeAntigravityTest(t, path, initial+antigravityTestRow(2, "MODEL", "GENERIC", "synthetic output", nil)+antigravityTestPlanner(3, "second", 5, 7, 0))
	res, err := restored.Feed(path)
	if err != nil || res.Restarted || restored.Calls != 2 || restored.Measured.Total() != 28 {
		t.Fatalf("append=%+v state=%+v %v", res, restored, err)
	}
	spent, measured := restored.Totals()
	var summed Tokens
	for _, token := range spent {
		summed.add(token)
	}
	if summed.Total() != measured.Total() || summed.CostKnown() || restored.Composition != nil || restored.Model != "" {
		t.Fatalf("fabricated attribution/price: spent=%+v measured=%+v", summed, measured)
	}
	if strings.Contains(string(encoded), "synthetic ledger sample") || strings.Contains(string(encoded), "synthetic output") {
		t.Fatal("ledger retained transcript text")
	}
}

func TestAntigravityLedgerRewritesBeyondTheHeadInvalidateTotals(t *testing.T) {
	_, path := antigravityTestPath(t)
	prefix := antigravityTestUser(0, strings.Repeat("padding", fingerprintBytes))
	writeAntigravityTest(t, path, prefix+antigravityTestPlanner(1, "first", 10, 2, 0))
	var state LedgerState
	if _, err := state.Feed(path); err != nil {
		t.Fatal(err)
	}
	writeAntigravityTest(t, path, prefix+antigravityTestPlanner(1, "first", 90, 2, 0))
	res, err := state.Feed(path)
	if err != nil || !res.Restarted || state.Restarts != 1 || state.Measured.Input != 90 || state.Calls != 1 {
		t.Fatalf("rewrite=%+v state=%+v %v", res, state, err)
	}
	prior := state.Measured
	writeAntigravityTest(t, path, prefix+antigravityTestPlanner(1, "first", 90, 2, 0)+antigravityTestPlanner(1, "duplicate", 1, 1, 0))
	if _, err := state.Feed(path); err == nil || state.Measured != prior {
		t.Fatalf("duplicate became a successful partial bill: %+v %v", state, err)
	}
}

func TestAntigravityLedgerUnknownAndZeroAreDifferent(t *testing.T) {
	_, path := antigravityTestPath(t)
	writeAntigravityTest(t, path, antigravityTestRow(0, "MODEL", "PLANNER_RESPONSE", "absent usage", nil))
	state := LedgerState{Assistant: "antigravity"}
	_, err := state.Feed(path)
	var typed *AntigravityRecordError
	if !errors.As(err, &typed) || typed.Code != "usage_unavailable" {
		t.Fatalf("unknown usage became zero: %+v %v", state, err)
	}
	writeAntigravityTest(t, path, antigravityTestPlanner(0, "zero", 0, 0, 0))
	if _, err := state.Feed(path); err != nil {
		t.Fatal(err)
	}
	if state.Calls != 1 || state.Measured.Total() != 0 {
		t.Fatalf("zero not counted: %+v", state)
	}
}

func TestAntigravityLedgerDoesNotTreatACopiedNativeLogAsClaude(t *testing.T) {
	path := filepath.Join(t.TempDir(), "copied.jsonl")
	writeAntigravityTest(t, path, antigravityTestPlanner(0, "one", 1, 1, 0))
	var state LedgerState
	_, err := state.Feed(path)
	var typed *AntigravityRecordError
	if !errors.As(err, &typed) || typed.Code != "path_invalid" || state.Offset != 0 {
		t.Fatalf("native log fell through: %+v %v", state, err)
	}
}

func TestAntigravityLedgerDailyUsesNativeCreatedAt(t *testing.T) {
	_, path := antigravityTestPath(t)
	writeAntigravityTest(t, path, antigravityTestPlanner(0, "one", 2, 3, 0))
	var state LedgerState
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	if res, err := state.FeedDaily(path, now); err != nil || res.More {
		t.Fatalf("daily=%+v %v", res, err)
	}
	day := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC).In(time.Local).Format("2006-01-02")
	if state.DailyUnknown || state.Daily[day].Total() != 5 || state.DailyProject != "" {
		t.Fatalf("daily=%+v", state)
	}
	state.DailyVersion = 0
	res, err := state.FeedDaily(path, now)
	if err != nil || res.More || state.Daily[day].Total() != 5 {
		t.Fatalf("backfill=%+v state=%+v %v", res, state, err)
	}
}
