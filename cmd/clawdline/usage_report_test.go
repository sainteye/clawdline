package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
)

// workSamplesFixture is a work-samples answer of n synthetic units, every
// one successful and read whole.
func workSamplesFixture(prefix string, since, until int64, n int, cacheRead float64) contract.UsageWorkSamples {
	ws := contract.UsageWorkSamples{DefinitionVersion: app.WorkReportDefinitionVersion, Since: since, Until: until,
		GeneratedAt: until, Samples: []contract.UsageWorkSample{}}
	for i := 0; i < n; i++ {
		ws.Samples = append(ws.Samples, contract.UsageWorkSample{Kind: "task", ID: fmt.Sprintf("%s%d", prefix, i),
			WorkKind: "feature", Scope: "1-3", Model: "model-a", CacheRead: cacheRead + float64(i), Output: 10,
			Calls: 4, Cost: 1, CostKnown: true, CreatedAt: since + 60, EndedAt: since + 660, DurationSeconds: 600,
			Ending: contract.UsageWorkEndingSuccess, Data: contract.UsageWorkDataComplete})
	}
	// One the ledger never read: counted, unreadable, never zero tokens.
	ws.Samples = append(ws.Samples, contract.UsageWorkSample{Kind: "task", ID: prefix + "-unread", WorkKind: "feature",
		Scope: "1-3", Model: "model-a", CreatedAt: since + 60, Ending: contract.UsageWorkEndingRunning,
		Data: contract.UsageWorkDataNotYetRead})
	return ws
}

// A frozen baseline reads back as it was written, recomputes the same report,
// is never written over, and is refused when frozen under another definition.
func TestAFrozenWorkBaselineRoundTripsAndRecomputesTheSameReport(t *testing.T) {
	baseline := workSamplesFixture("b", 1_789_000_000, 1_790_200_000, 24, 1000)
	trial := workSamplesFixture("t", 1_790_200_000, 1_791_000_000, 22, 600)
	path := filepath.Join(t.TempDir(), "usage-baselines", "2026-10-02.json")
	if err := writeWorkBaseline(path, baseline); err != nil {
		t.Fatal(err)
	}
	read, err := readWorkBaseline(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(read, baseline) {
		t.Fatalf("read back:\n%+v\nwrote:\n%+v", read, baseline)
	}
	want := recomputeWorkReport(baseline, trial)
	got := recomputeWorkReport(read, trial)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recomputed differently")
	}
	g := want.Report.Groups[0]
	if want.Report.Verdict != app.WorkVerdictMet || g.Baseline.Units != 25 || g.Baseline.Unreadable != 1 || g.Baseline.Comparable != 24 {
		t.Fatalf("report: %+v", want.Report)
	}
	if err := writeWorkBaseline(path, trial); err == nil || !strings.Contains(err.Error(), "already holds") {
		t.Fatalf("written over: %v", err)
	}
	if again, _ := readWorkBaseline(path); !reflect.DeepEqual(again, baseline) {
		t.Fatal("the frozen file changed")
	}

	other := baseline
	other.DefinitionVersion = app.WorkReportDefinitionVersion + 1
	body, _ := json.Marshal(other)
	otherPath := filepath.Join(t.TempDir(), "other.json")
	if err := os.WriteFile(otherPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readWorkBaseline(otherPath); err == nil || !strings.Contains(err.Error(), "definition") {
		t.Fatalf("another definition: %v", err)
	}

	var out bytes.Buffer
	writeWorkReport(&out, want)
	t.Log("\n" + out.String())
	for _, line := range []string{"feature/1-3 claims/model-a/one-end", "verdict: met", "overall: met",
		"not recorded: missed_handoff", "not recorded: human_rescue", "not recorded: deploy_rollback", "25 (1)"} {
		if !strings.Contains(out.String(), line) {
			t.Errorf("the report lacks %q:\n%s", line, out.String())
		}
	}
}
