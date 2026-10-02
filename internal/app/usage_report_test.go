package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// unit is a successful, wholly read synthetic unit in one stratum.
func unit(id string, cacheRead float64, duration int64) WorkUnitSample {
	return WorkUnitSample{Kind: WorkUnitTask, ID: id, WorkKind: "feature", Scope: WorkScopeSmall, Model: "model-a",
		Input: 10, CacheWrite: 100, CacheRead: cacheRead, Output: 50, Calls: 10, Cost: 1, CostKnown: true,
		DurationSeconds: duration, Ending: WorkEndingSuccess, Data: WorkDataComplete}
}

func units(prefix string, n int, cacheRead float64, duration int64) []WorkUnitSample {
	out := make([]WorkUnitSample, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, unit(fmt.Sprintf("%s%d", prefix, i), cacheRead, duration))
	}
	return out
}

func onlyGroup(t *testing.T, r WorkReport) WorkGroup {
	t.Helper()
	if len(r.Groups) != 1 {
		t.Fatalf("groups: %+v", r.Groups)
	}
	return r.Groups[0]
}

func TestWorkReportUnderTwentyComparableUnitsIsInsufficientEvidence(t *testing.T) {
	// 19 comparable in the trial, plus units that do not count toward the
	// twenty: a failure, a running one and a successful one never read.
	trial := units("t", 19, 100, 100)
	failed := unit("tf", 100, 100)
	failed.Ending = WorkEndingFailure
	running := unit("tr", 0, 0)
	running.Ending = WorkEndingRunning
	unread := unit("tu", 0, 100)
	unread.Data, unread.CostKnown = WorkDataNotYetRead, false
	trial = append(trial, failed, running, unread)
	r := FoldWorkReport(units("b", 25, 1000, 100), trial)
	g := onlyGroup(t, r)
	if g.Verdict != WorkVerdictInsufficient || r.Verdict != WorkVerdictInsufficient || len(g.Failed) != 0 {
		t.Fatalf("verdict %s / %s, failed %v", g.Verdict, r.Verdict, g.Failed)
	}
	if g.Trial.Units != 22 || g.Trial.Comparable != 19 || g.Trial.Unreadable != 1 || g.Trial.Running != 1 {
		t.Fatalf("trial counts: %+v", g.Trial)
	}
	// The figures are still shown; only the verdict is withheld.
	if g.CacheReadChange == nil || *g.CacheReadChange != -0.9 {
		t.Fatalf("change: %v", g.CacheReadChange)
	}
}

func TestWorkReportThresholdsAreDecidedExactlyAtTheBoundary(t *testing.T) {
	base := units("b", 20, 1000, 1000)
	for _, tc := range []struct {
		name      string
		cacheRead float64
		duration  int64
		verdict   string
		failed    []string
	}{
		{"exactly 30% down and 10% longer", 700, 1100, WorkVerdictMet, []string{}},
		{"just short of 30% down", 701, 1100, WorkVerdictNotMet, []string{WorkFailedCacheRead}},
		{"just past 10% longer", 700, 1101, WorkVerdictNotMet, []string{WorkFailedDuration}},
		{"both", 800, 1200, WorkVerdictNotMet, []string{WorkFailedCacheRead, WorkFailedDuration}},
	} {
		g := onlyGroup(t, FoldWorkReport(base, units("t", 20, tc.cacheRead, tc.duration)))
		if g.Verdict != tc.verdict || !reflect.DeepEqual(g.Failed, tc.failed) {
			t.Errorf("%s: %s %v", tc.name, g.Verdict, g.Failed)
		}
	}
}

func TestWorkReportAWorseGuardrailBlocksMet(t *testing.T) {
	base := units("b", 20, 1000, 1000)
	for _, tc := range []struct {
		name   string
		mutate func(*WorkUnitSample)
		failed string
	}{
		{"a failure", func(s *WorkUnitSample) { s.Ending = WorkEndingFailure }, WorkFailedFailureRate},
		{"a timeout", func(s *WorkUnitSample) { s.Ending = WorkEndingTimeout }, WorkFailedTimeoutStall},
		{"a stall", func(s *WorkUnitSample) { s.Ending = WorkEndingStalled }, WorkFailedTimeoutStall},
		{"a respawn", func(s *WorkUnitSample) { s.Respawn = true }, WorkFailedRedoRate},
	} {
		trial := units("t", 20, 500, 1000)
		extra := unit("tx", 500, 1000)
		tc.mutate(&extra)
		trial = append(trial, extra)
		g := onlyGroup(t, FoldWorkReport(base, trial))
		if g.Verdict != WorkVerdictNotMet || !slices.Contains(g.Failed, tc.failed) {
			t.Errorf("%s: %s %v", tc.name, g.Verdict, g.Failed)
		}
	}
	// The same rate in both periods is not worse.
	b := append(units("b", 20, 1000, 1000), func() WorkUnitSample { s := unit("bf", 1, 1); s.Ending = WorkEndingFailure; return s }())
	tr := append(units("t", 20, 500, 1000), func() WorkUnitSample { s := unit("tf", 1, 1); s.Ending = WorkEndingFailure; return s }())
	if g := onlyGroup(t, FoldWorkReport(b, tr)); g.Verdict != WorkVerdictMet {
		t.Fatalf("an equal failure rate: %s %v", g.Verdict, g.Failed)
	}
}

func TestWorkReportUnreadableAndUnpricedUnitsAreNeverZero(t *testing.T) {
	base := units("b", 20, 1000, 1000)
	trial := units("t", 20, 1000, 1000)
	// Ten units the ledger has not read, or read only in part, whose parts
	// are zero or low: counted as unreadable, and kept out of every median.
	for i := 0; i < 10; i++ {
		s := unit(fmt.Sprintf("u%d", i), 0, 1000)
		s.Data, s.CostKnown = WorkDataNotYetRead, false
		if i%2 == 1 {
			s.Data, s.CacheRead = WorkDataLedgerBehind, 3
		}
		trial = append(trial, s)
	}
	g := onlyGroup(t, FoldWorkReport(base, trial))
	if g.Trial.Units != 30 || g.Trial.Unreadable != 10 || g.Trial.Comparable != 20 || *g.Trial.CacheReadMedian != 1000 {
		t.Fatalf("trial: %+v", g.Trial)
	}
	// An unpriced unit is readable: its tokens count, its cost is unknown.
	codex := unit("c", 1000, 1000)
	codex.Data, codex.Cost, codex.CostKnown = WorkDataUnpriced, 0, false
	g = onlyGroup(t, FoldWorkReport(base, append(units("t", 19, 1000, 1000), codex)))
	if g.Trial.Comparable != 20 || g.Trial.CostKnown || g.Trial.Unreadable != 0 {
		t.Fatalf("unpriced: %+v", g.Trial)
	}
	// And a period with nothing comparable has no medians, not zero ones.
	g = onlyGroup(t, FoldWorkReport(base, nil))
	if g.Trial.CacheReadMedian != nil || g.Trial.DurationP75 != nil || g.Trial.FailureRate != nil || g.Trial.CostKnown {
		t.Fatalf("empty trial: %+v", g.Trial)
	}
}

func TestWorkReportMedianIntervalIsExactAndDeterministic(t *testing.T) {
	var v []float64
	for i := 1; i <= 20; i++ {
		v = append(v, float64(i))
	}
	// n = 20: P(B ≤ 5) = 0.0207, so [x(6), x(15)] covers 95.86%; P(B ≤ 6)
	// = 0.0577 would cover only 88.5%.
	got := medianInterval(v)
	if got == nil || got.Low != 6 || got.High != 15 || got.Coverage < 0.958 || got.Coverage > 0.959 {
		t.Fatalf("interval: %+v", got)
	}
	shuffled := slices.Clone(v)
	rand.New(rand.NewSource(7)).Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	if again := medianInterval(shuffled); !reflect.DeepEqual(again, got) {
		t.Fatalf("the same values in another order: %+v", again)
	}
	if medianInterval(v[:5]) != nil {
		t.Fatal("five values have no 95% interval")
	}
	if got := medianInterval(v[:6]); got == nil || got.Low != 1 || got.High != 6 {
		t.Fatalf("six values: %+v", got)
	}
	if p := percentileNearestRank([]int64{40, 10, 30, 20}, 75); p != 30 {
		t.Fatalf("p75 of four: %d", p)
	}
}

func TestWorkReportOverallVerdictNeedsEveryJudgedGroup(t *testing.T) {
	other := func(s []WorkUnitSample) []WorkUnitSample {
		for i := range s {
			s[i].WorkKind = "bugfix"
		}
		return s
	}
	met := append(units("b", 20, 1000, 1000), other(units("bo", 3, 1000, 1000))...)
	trial := append(units("t", 20, 500, 1000), other(units("to", 3, 2000, 1000))...)
	r := FoldWorkReport(met, trial)
	if r.Verdict != WorkVerdictMet || len(r.Groups) != 2 || r.Groups[0].Verdict != WorkVerdictInsufficient {
		t.Fatalf("one met, one too small: %s %+v", r.Verdict, r.Groups)
	}
	base := append(units("b", 20, 1000, 1000), other(units("bo", 20, 1000, 1000))...)
	trial = append(units("t", 20, 500, 1000), other(units("to", 20, 2000, 1000))...)
	if r := FoldWorkReport(base, trial); r.Verdict != WorkVerdictNotMet {
		t.Fatalf("one met, one not: %s %s", r.Verdict, r.Why)
	}
	if len(WorkNotRecorded) != 3 || len(r.NotRecorded) != 3 {
		t.Fatalf("not recorded: %+v", r.NotRecorded)
	}
}

// The samples read off the store: strata from the record, the bill from the
// ledger, respawns both ways, and a task with no reading as not_yet_read.
func TestWorkSamplesReadStrataBillAndEnding(t *testing.T) {
	h := newCompareHarness(t)
	ctx := context.Background()
	save := func(id, state string, rec map[string]any, age time.Duration) {
		at := h.now.Add(-age)
		rec["task_id"], rec["state"], rec["created_at"], rec["assistant"] = id, state, at, "claude"
		body, _ := json.Marshal(rec)
		row := store.BrokerRow{ID: id, Project: "/p", Assistant: "claude", State: state, CreatedAt: at, UpdatedAt: at,
			SecretHash: "h", Record: body}
		if err := h.st.SaveBrokerTask(ctx, row, nil); err != nil {
			t.Fatal(err)
		}
	}
	save("a", "spawn_failed", map[string]any{"kind": "feature", "claims": []string{"web/x.ts", "cmd/clawdline/x.go"}}, 3*time.Hour)
	save("b", "success", map[string]any{"respawn_of": "a", "claims": []string{"internal/a", "internal/b", "internal/c", "internal/d"},
		"finished_at": h.now.Add(-time.Hour)}, 2*time.Hour)
	save("old", "success", map[string]any{}, 30*24*time.Hour)
	row := func(conv, task, model string, readAt time.Time) {
		r := store.UsageRow{Assistant: "claude", Conversation: conv, TaskID: task, Path: "/nowhere/" + conv + ".jsonl",
			ReadAt: readAt, OpeningRead: true}
		r.Spent, _ = json.Marshal(map[string]map[string]float64{"impl": {"cache_read": 5000, "output": 20, "cost": 2}})
		r.Measured, _ = json.Marshal(map[string]float64{"cache_read": 5000, "output": 20})
		r.State, _ = json.Marshal(map[string]any{"model": model, "calls": 7, "calls_above": 1})
		if err := h.st.SaveUsageRow(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	row("b1", "b", "model-a", h.now)
	row("b2", "b", "model-b", h.now)

	got, err := h.u.WorkSamplesBetween(ctx, h.now.Add(-CompareDefaultSince), h.now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Samples) != 2 || got.DefinitionVersion != WorkReportDefinitionVersion {
		t.Fatalf("samples: %+v", got)
	}
	b, a := got.Samples[0], got.Samples[1]
	if a.ID != "a" || a.WorkKind != "feature" || a.Scope != WorkScopeSmall || !a.CrossEnd || a.Ending != WorkEndingLost ||
		!a.Respawned || a.Respawn || a.Data != WorkDataNotYetRead || a.CostKnown || a.Model != WorkModelUnknown {
		t.Fatalf("a: %+v", a)
	}
	if b.ID != "b" || b.WorkKind != WorkKindUnspecified || b.Scope != WorkScopeMedium || b.CrossEnd || !b.Respawn ||
		b.Ending != WorkEndingSuccess || b.DurationSeconds != 3600 || b.Model != WorkModelMixed || b.Data != WorkDataMixedModels ||
		b.CacheRead != 10000 || b.Calls != 14 || b.CallsAbove != 2 || b.Cost != 4 || !b.CostKnown {
		t.Fatalf("b: %+v", b)
	}
	// A range that ends before b began leaves it out.
	early, err := h.u.WorkSamplesSince(ctx, "14d", fmt.Sprint(h.now.Add(-150*time.Minute).Unix()))
	if err != nil || len(early.Samples) != 1 || early.Samples[0].ID != "a" {
		t.Fatalf("early: %+v %v", early, err)
	}
	if _, err := h.u.WorkSamplesSince(ctx, "1d", fmt.Sprint(h.now.Add(-48*time.Hour).Unix())); err != ErrWorkUntil {
		t.Fatalf("an until before since: %v", err)
	}
}
