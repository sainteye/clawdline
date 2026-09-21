package http

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/analytics"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
)

// The ledger's one job is that three different silences look different. These
// tests are written against that: what must never appear is a `0` standing for
// a figure nobody recorded.

func ledgerRow(task string, tokens [4]*int64) analytics.Row {
	return analytics.Row{IntervalKey: task + "#1", TaskID: task, Tokens: tokens}
}

func n(v int64) *int64 { return &v }

func reviewOf(t *testing.T, verdict string, findings int) json.RawMessage {
	t.Helper()
	axes := []map[string]any{}
	for i, name := range []string{"specification", "repository_invariants", "runtime_failure_behavior"} {
		list := []map[string]any{}
		if i == 0 {
			for f := 0; f < findings; f++ {
				list = append(list, map[string]any{
					"id": "finding-" + string(rune('a'+f)), "severity": "blocking",
					"summary": "a summary", "evidence": []string{"file.go:1"},
				})
			}
		}
		status := "pass"
		if len(list) > 0 {
			status = "findings"
		}
		axes = append(axes, map[string]any{"axis": name, "status": status, "findings": list})
	}
	raw, err := json.Marshal(map[string]any{"verdict": verdict, "axes": axes})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// A Feature nobody reviewed says "no record", and a Feature reviewed clean says
// zero findings. The two are the same number on a naive page and must not be
// the same answer here.
func TestALedgerTellsNoReviewFromAReviewThatFoundNothing(t *testing.T) {
	at := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	records := []orchestrator.Record{
		{
			ID: "task-unreviewed", CreatedAt: at, FinishedAt: at,
			Graph:  &orchestrator.Graph{ID: "graph-quiet", Destination: "The quiet one", CurrentNode: "build", Nodes: []orchestrator.GraphNode{{ID: "build", Kind: "delivery"}}},
			Result: &taskdir.Result{Status: "success"},
		},
		{
			ID: "task-reviewed", CreatedAt: at, FinishedAt: at,
			Graph:  &orchestrator.Graph{ID: "graph-clean", Destination: "The clean one", CurrentNode: "read", Nodes: []orchestrator.GraphNode{{ID: "read", Kind: "review"}}},
			Result: &taskdir.Result{Status: "success", Review: reviewOf(t, "safe_to_land", 0)},
		},
	}
	ledger := buildLedger(records, nil, 4000, 200)
	byID := map[string]contract.LedgerFeature{}
	for _, f := range ledger.Features {
		if f.GraphID != nil {
			byID[*f.GraphID] = f
		}
	}
	quiet, clean := byID["graph-quiet"], byID["graph-clean"]
	if quiet.Findings.State != contract.LedgerStateAbsent || quiet.Findings.Total != nil {
		t.Errorf("a Feature with no review receipt: %v %v", quiet.Findings.State, quiet.Findings.Total)
	}
	if clean.Findings.State != contract.LedgerStatePresent || clean.Findings.Total == nil || *clean.Findings.Total != 0 {
		t.Errorf("a Feature reviewed clean: %v %v", clean.Findings.State, clean.Findings.Total)
	}
	if clean.Label == nil || *clean.Label != "The clean one" {
		t.Errorf("the Feature's label is its graph's destination: %v", clean.Label)
	}
}

// Rows that measured nothing are `unknown`, not zero; rows that measured part
// of themselves are a floor, not a total.
func TestLedgerTokensSayUnknownAndFloorRatherThanANumber(t *testing.T) {
	at := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	graph := &orchestrator.Graph{ID: "g", Destination: "d", CurrentNode: "build",
		Nodes: []orchestrator.GraphNode{{ID: "build", Kind: "delivery"}}}
	records := []orchestrator.Record{{ID: "task", CreatedAt: at, FinishedAt: at, Graph: graph}}

	unmeasured := buildLedger(records, []analytics.Row{ledgerRow("task", [4]*int64{})}, 4000, 200)
	got := unmeasured.Features[0].Tokens.Implementation
	if got.State != contract.LedgerStateUnknown || got.Total != nil || got.Rows != 1 {
		t.Errorf("rows that measured nothing: %v total=%v rows=%d", got.State, got.Total, got.Rows)
	}

	partial := buildLedger(records, []analytics.Row{ledgerRow("task", [4]*int64{n(10), nil, n(5), nil})}, 4000, 200)
	got = partial.Features[0].Tokens.Implementation
	if got.State != contract.LedgerStatePresent || got.Total != nil || got.Measured != 15 || got.IncompleteRows != 1 {
		t.Errorf("a row that measured part of itself: %v total=%v measured=%d incomplete=%d",
			got.State, got.Total, got.Measured, got.IncompleteRows)
	}

	whole := buildLedger(records, []analytics.Row{ledgerRow("task", [4]*int64{n(1), n(2), n(3), n(4)})}, 4000, 200)
	got = whole.Features[0].Tokens.Implementation
	if got.State != contract.LedgerStatePresent || got.Total == nil || *got.Total != 10 {
		t.Errorf("a row that measured all of itself: %v %v", got.State, got.Total)
	}
}

// A row whose task named no side of the work is its own bucket, and a row whose
// task named no Feature is the block above the list — two different gaps, and
// neither is added to the implementation figure.
func TestLedgerKeepsUndeclaredAndUnattributedApart(t *testing.T) {
	at := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	records := []orchestrator.Record{
		{ID: "graphed", CreatedAt: at, FinishedAt: at,
			Graph: &orchestrator.Graph{ID: "g", Destination: "d", CurrentNode: "nowhere"}},
		{ID: "loose", CreatedAt: at, FinishedAt: at},
	}
	ledger := buildLedger(records, []analytics.Row{
		ledgerRow("graphed", [4]*int64{n(7), n(0), n(0), n(0)}),
		ledgerRow("loose", [4]*int64{n(9), n(0), n(0), n(0)}),
	}, 4000, 200)

	feature := ledger.Features[0]
	if feature.Tokens.Implementation.Rows != 0 {
		t.Errorf("a task whose graph names no current node declares no side: %d rows landed in implementation",
			feature.Tokens.Implementation.Rows)
	}
	if feature.Tokens.Undeclared.Total == nil || *feature.Tokens.Undeclared.Total != 7 {
		t.Errorf("the undeclared bucket: %v", feature.Tokens.Undeclared.Total)
	}
	if ledger.Unattributed == nil || ledger.Unattributed.GraphID != nil {
		t.Fatalf("the block that names no Feature: %+v", ledger.Unattributed)
	}
	if ledger.Unattributed.Tokens.Undeclared.Total == nil || *ledger.Unattributed.Tokens.Undeclared.Total != 9 {
		t.Errorf("a row whose task is on no graph: %v", ledger.Unattributed.Tokens.Undeclared.Total)
	}
}

// The two ceilings are told apart, and the count of what was found survives the
// list being cut: a Mac shown only one of the two reads the larger number over
// the smaller list.
func TestALedgerPastItsCeilingSaysHowManyItFound(t *testing.T) {
	at := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	records := []orchestrator.Record{}
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		records = append(records, orchestrator.Record{
			ID: id, CreatedAt: at.Add(time.Duration(i) * time.Minute), FinishedAt: at.Add(time.Duration(i) * time.Minute),
			Graph: &orchestrator.Graph{ID: "graph-" + id, Destination: id, CurrentNode: "build",
				Nodes: []orchestrator.GraphNode{{ID: "build", Kind: "delivery"}}},
		})
	}
	ledger := buildLedger(records, nil, 4000, 2)
	if ledger.Read.FeaturesFound != 5 || ledger.Read.FeaturesListed != 2 || len(ledger.Features) != 2 {
		t.Errorf("found %d, listed %d, drew %d", ledger.Read.FeaturesFound, ledger.Read.FeaturesListed, len(ledger.Features))
	}
	if ledger.Read.Truncated {
		t.Error("a cut list is not a cut scan")
	}
	cut := buildLedger(records, nil, 3, 200)
	if !cut.Read.Truncated || cut.Read.FeaturesFound != 3 {
		t.Errorf("a cut scan: truncated=%v found=%d", cut.Read.Truncated, cut.Read.FeaturesFound)
	}
}

// A verification receipt whose last run did not pass is counted red, including
// a word this build has never seen: a run nobody can classify did not pass.
func TestLedgerCountsAVerificationThatDidNotPassAsRed(t *testing.T) {
	at := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	graph := func(id string) *orchestrator.Graph {
		return &orchestrator.Graph{ID: id, Destination: id, CurrentNode: "build",
			Nodes: []orchestrator.GraphNode{{ID: "build", Kind: "delivery"}}}
	}
	records := []orchestrator.Record{
		{ID: "one", CreatedAt: at, FinishedAt: at, Graph: graph("g"),
			Result: &taskdir.Result{Status: "success", Verify: &taskdir.Verification{Runs: 2, Seconds: 30, Last: "pass"}}},
		{ID: "two", CreatedAt: at, FinishedAt: at, Graph: graph("g"),
			Result: &taskdir.Result{Status: "success", Verify: &taskdir.Verification{Runs: 1, Seconds: 9, Last: "inconclusive"}}},
	}
	v := buildLedger(records, nil, 4000, 200).Features[0].Verification
	if v.State != contract.LedgerStatePresent || v.Runs != 3 || v.Seconds != 39 || v.EndedRed != 1 || v.Receipts != 2 {
		t.Errorf("verification: %+v", v)
	}
}
