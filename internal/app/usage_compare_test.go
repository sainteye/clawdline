package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// compareHarness is a store seeded with child tasks' broker records and their
// sessions' ledger rows, every one synthetic.
type compareHarness struct {
	t   *testing.T
	st  *store.Store
	u   *UsageLedger
	now time.Time
}

func newCompareHarness(t *testing.T) *compareHarness {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Unix(1_790_400_000, 0)
	u := NewUsageLedger(st, t.TempDir())
	u.Now = func() time.Time { return now }
	return &compareHarness{t: t, st: st, u: u, now: now}
}

// compareTask is one child task's record, as the broker keeps it.
type compareTask struct {
	id, assistant, state string
	// window is the record's auto_compact_window: nil leaves it out.
	window    *int64
	launched  bool
	stalled   bool
	respawnOf string
	age       time.Duration
}

func win(n int64) *int64 { return &n }

func (h *compareHarness) task(c compareTask) {
	h.t.Helper()
	if c.assistant == "" {
		c.assistant = "claude"
	}
	at := h.now.Add(-c.age - time.Hour)
	rec := map[string]any{"clawdline_protocol": 1, "task_id": c.id, "assistant": c.assistant, "state": c.state,
		"created_at": at}
	if c.window != nil {
		rec["auto_compact_window"] = *c.window
	}
	if c.launched {
		rec["spawned_at"] = at.Add(time.Second)
	}
	if c.stalled {
		rec["stall"] = map[string]any{"nudged_at": at.Add(5 * time.Minute), "reported_at": at.Add(10 * time.Minute)}
	}
	if c.respawnOf != "" {
		rec["respawn_of"] = c.respawnOf
	}
	body, _ := json.Marshal(rec)
	row := store.BrokerRow{ID: c.id, Project: "/p", Assistant: c.assistant, State: c.state, CreatedAt: at, UpdatedAt: at,
		SecretHash: "h", Record: body}
	if err := h.st.SaveBrokerTask(context.Background(), row, nil); err != nil {
		h.t.Fatal(err)
	}
}

// session stores one read session of a task: cost is its own cost, all impl;
// above what its calls past 200k cost; sub a subagent's cost, if any.
func (h *compareHarness) session(conversation, taskID string, cost, above, sub float64, calls, compactions, peak int64) {
	h.t.Helper()
	save := func(r store.UsageRow, cost float64, state map[string]any) {
		spent := map[string]map[string]float64{"impl": {"input": cost * 10, "cost": cost}}
		r.Spent, _ = json.Marshal(spent)
		r.Measured, _ = json.Marshal(map[string]float64{"input": cost * 10})
		r.State, _ = json.Marshal(state)
		r.Path, r.ReadAt, r.OpeningRead = "/nowhere/"+r.Conversation+".jsonl", h.now, true
		if err := h.st.SaveUsageRow(context.Background(), r); err != nil {
			h.t.Fatal(err)
		}
	}
	save(store.UsageRow{Assistant: "claude", Conversation: conversation, TaskID: taskID}, cost,
		map[string]any{"calls": calls, "compactions": compactions, "peak_context": peak, "above": map[string]any{"cost": above}})
	if sub > 0 {
		save(store.UsageRow{Assistant: "claude", Conversation: conversation + "-agent", Parent: conversation}, sub,
			map[string]any{"calls": 1, "peak_context": 1000})
	}
}

func (h *compareHarness) compare() CompactionComparison {
	h.t.Helper()
	got, err := h.u.CompareCompaction(context.Background(), h.now.Add(-CompareDefaultSince))
	if err != nil {
		h.t.Fatal(err)
	}
	return got
}

func groupOf(t *testing.T, c CompactionComparison, window int64) CompactionGroup {
	t.Helper()
	for _, g := range c.Groups {
		if g.Window == window {
			return g
		}
	}
	t.Fatalf("no group for window %d in %+v", window, c.Groups)
	return CompactionGroup{}
}

func rate(p *float64) float64 {
	if p == nil {
		return -1
	}
	return *p
}

// seed is two comparable groups — none and 300000, five tasks each — a third
// with too few tasks, three tasks with no known window, and one outside the
// range.
func (h *compareHarness) seed() {
	none, early, tiny := win(0), win(300_000), win(60_000)
	for _, c := range []compareTask{
		{id: "n1", state: "success", window: none, launched: true},
		{id: "n2", state: "success", window: none, launched: true, respawnOf: "n0"},
		{id: "n3", state: "failure", window: none, launched: true},
		{id: "n4", state: "timeout", window: none, launched: true},
		{id: "n5", state: "spawn_failed", window: none, launched: true, stalled: true},
		{id: "e1", state: "success", window: early, launched: true},
		{id: "e2", state: "success", window: early, launched: true},
		{id: "e3", state: "success", window: early, launched: true},
		{id: "e4", state: "spawn_failed", window: early, launched: true},
		{id: "e5", state: "briefed", window: early, launched: true},
		{id: "s1", state: "success", window: tiny, launched: true},
		{id: "s2", state: "failure", window: tiny, launched: true},
		{id: "x-codex", assistant: "codex", state: "success", launched: true},
		{id: "x-queued", state: "spawn_failed"},
		{id: "x-old-record", state: "success", launched: true},
		{id: "x-before-range", state: "success", window: early, launched: true, age: 20 * 24 * time.Hour},
	} {
		h.task(c)
	}
	// none: 10, 12 (a session and a subagent's 2), 14, 20; n5 stalled with
	// no reading. 300000: 6, 8 (two sessions), 10, e4 lost unread, e5 4.
	h.session("c-n1", "n1", 10, 6, 0, 40, 0, 400_000)
	h.session("c-n2", "n2", 10, 5, 2, 30, 0, 350_000)
	h.session("c-n3", "n3", 14, 7, 0, 50, 1, 600_000)
	h.session("c-n4", "n4", 20, 12, 0, 80, 0, 900_000)
	h.session("c-e1", "e1", 6, 0, 0, 40, 2, 290_000)
	h.session("c-e2a", "e2", 4, 0, 0, 20, 1, 280_000)
	h.session("c-e2b", "e2", 4, 1, 0, 20, 1, 250_000)
	h.session("c-e3", "e3", 10, 1, 0, 60, 3, 310_000)
	h.session("c-e5", "e5", 4, 0, 0, 10, 0, 100_000)
	h.session("c-s1", "s1", 3, 1, 0, 10, 0, 100_000)
	h.session("c-x", "x-codex", 50, 0, 0, 10, 0, 100_000)
	h.session("c-before", "x-before-range", 99, 0, 0, 10, 0, 100_000)
}

// TestTheComparisonGroupsTasksByTheWindowTheyWereLaunchedWith: one group per
// window, `none` first, each with its tasks, sessions and bill figures.
func TestTheComparisonGroupsTasksByTheWindowTheyWereLaunchedWith(t *testing.T) {
	h := newCompareHarness(t)
	h.seed()
	got := h.compare()
	var windows []int64
	for _, g := range got.Groups {
		windows = append(windows, g.Window)
	}
	if len(windows) != 3 || windows[0] != 0 || windows[1] != 60_000 || windows[2] != 300_000 {
		t.Fatalf("groups %v, wanted none, 60000, 300000", windows)
	}
	none := groupOf(t, got, 0)
	if none.Tasks != 5 || none.Read != 4 || none.Sessions != 4 {
		t.Fatalf("none: %d tasks, %d read, %d sessions; wanted 5, 4, 4", none.Tasks, none.Read, none.Sessions)
	}
	// 10, 12, 14, 20: n2's subagent is part of its task's cost.
	if !near(none.CostTotal, 56) || !near(none.CostMedian, 13) || !none.CostKnown {
		t.Fatalf("none cost: total %v median %v known %v; wanted 56, 13, true", none.CostTotal, none.CostMedian, none.CostKnown)
	}
	if !near(none.CallsPerTask, 50) || !near(none.CompactionsPerTask, 0.25) {
		t.Fatalf("none: %v calls/task, %v compactions/task; wanted 50, 0.25", none.CallsPerTask, none.CompactionsPerTask)
	}
	if none.PeakMedian != 500_000 || none.PeakMax != 900_000 {
		t.Fatalf("none peak: median %d max %d", none.PeakMedian, none.PeakMax)
	}
	// Above 200k: 6+5+7+12 of the sessions' own 10+10+14+20 — the subagent's
	// 2 is not a call of the session's and is not in the denominator.
	if !near(rate(none.AboveShare), 30.0/54.0) {
		t.Fatalf("none above share %v, wanted %v", rate(none.AboveShare), 30.0/54.0)
	}
	early := groupOf(t, got, 300_000)
	if early.Tasks != 5 || early.Read != 4 || early.Sessions != 5 {
		t.Fatalf("300000: %d tasks, %d read, %d sessions; wanted 5, 4, 5", early.Tasks, early.Read, early.Sessions)
	}
	// e1 6, e2 8, e3 10, e5 4: a task with two sessions is one task.
	if !near(early.CostTotal, 28) || !near(early.CostMedian, 7) {
		t.Fatalf("300000 cost: total %v median %v; wanted 28, 7", early.CostTotal, early.CostMedian)
	}
	if !near(early.CompactionsPerTask, 1.75) || early.PeakMax != 310_000 {
		t.Fatalf("300000: %v compactions/task, peak max %d", early.CompactionsPerTask, early.PeakMax)
	}
	if !near(rate(early.AboveShare), 2.0/28.0) {
		t.Fatalf("300000 above share %v", rate(early.AboveShare))
	}
}

// TestTheComparisonCountsAndNamesWhatItLeavesOut: a Codex task, one never
// launched and one whose record predates the window are counted and named;
// a task before the range is not in the answer at all.
func TestTheComparisonCountsAndNamesWhatItLeavesOut(t *testing.T) {
	h := newCompareHarness(t)
	h.seed()
	got := h.compare()
	if got.Excluded != 3 || got.ExcludedTruncated {
		t.Fatalf("excluded %d (truncated %v), wanted 3: %+v", got.Excluded, got.ExcludedTruncated, got.ExcludedTasks)
	}
	want := map[string]string{"x-codex": CompareExcludedCodex, "x-queued": CompareExcludedNotLaunched,
		"x-old-record": CompareExcludedUnrecorded}
	for _, e := range got.ExcludedTasks {
		if want[e.TaskID] != e.Reason {
			t.Fatalf("%s excluded as %q, wanted %q", e.TaskID, e.Reason, want[e.TaskID])
		}
		delete(want, e.TaskID)
	}
	if len(want) != 0 {
		t.Fatalf("not named: %v", want)
	}
	for _, g := range got.Groups {
		if g.CostTotal >= 99 {
			t.Fatalf("a task created before the range was counted: %+v", g)
		}
	}
	if !got.Since.Equal(h.now.Add(-CompareDefaultSince)) || !got.Until.Equal(h.now) || got.MinTasks != compareTooFewLimit {
		t.Fatalf("range %v…%v min %d", got.Since, got.Until, got.MinTasks)
	}
}

// TestAGroupWithTooFewTasksShowsNoPercentage: two tasks say too_few, and no
// share or rate is shown for them; the counts still are.
func TestAGroupWithTooFewTasksShowsNoPercentage(t *testing.T) {
	h := newCompareHarness(t)
	h.seed()
	tiny := groupOf(t, h.compare(), 60_000)
	if !tiny.TooFew || tiny.Tasks != 2 {
		t.Fatalf("60000: too_few %v with %d tasks", tiny.TooFew, tiny.Tasks)
	}
	for name, p := range map[string]*float64{"above": tiny.AboveShare, "success": tiny.SuccessRate,
		"failure": tiny.FailureRate, "timeout": tiny.TimeoutRate, "stalled": tiny.StalledRate} {
		if p != nil {
			t.Fatalf("a group of %d shows a %s share of %v", tiny.Tasks, name, *p)
		}
	}
	if tiny.Success != 1 || tiny.Failure != 1 || !near(tiny.CostTotal, 3) {
		t.Fatalf("the counts went with the percentages: %+v", tiny)
	}
	if none := groupOf(t, h.compare(), 0); none.TooFew || none.SuccessRate == nil {
		t.Fatalf("five tasks are enough: %+v", none)
	}
}

// TestEachQualityProxyIsReadFromTheTaskRecords: success, failure, timeout,
// stalled (spawn_failed with the stall report) apart from any other
// spawn_failed, a task still running outside every rate, and respawns.
func TestEachQualityProxyIsReadFromTheTaskRecords(t *testing.T) {
	h := newCompareHarness(t)
	h.seed()
	got := h.compare()
	none := groupOf(t, got, 0)
	if none.Ended != 5 || none.Success != 2 || none.Failure != 1 || none.Timeout != 1 || none.Stalled != 1 || none.Lost != 0 {
		t.Fatalf("none endings: %+v", none)
	}
	if !near(rate(none.SuccessRate), 0.4) || !near(rate(none.FailureRate), 0.2) ||
		!near(rate(none.TimeoutRate), 0.2) || !near(rate(none.StalledRate), 0.2) {
		t.Fatalf("none rates: %v %v %v %v", rate(none.SuccessRate), rate(none.FailureRate), rate(none.TimeoutRate), rate(none.StalledRate))
	}
	if none.Respawns != 1 {
		t.Fatalf("none respawns %d, wanted 1", none.Respawns)
	}
	early := groupOf(t, got, 300_000)
	if early.Ended != 4 || early.Running != 1 || early.Success != 3 || early.Stalled != 0 || early.Lost != 1 {
		t.Fatalf("300000 endings: %+v", early)
	}
	if !near(rate(early.SuccessRate), 0.75) || !near(rate(early.StalledRate), 0) || early.Respawns != 0 {
		t.Fatalf("300000 rates: success %v stalled %v respawns %d", rate(early.SuccessRate), rate(early.StalledRate), early.Respawns)
	}
	// Nothing records a finish refusal, so it is named, not counted as zero.
	if len(got.NotRecorded) != 1 || got.NotRecorded[0].Name != "finish_refusals" || got.NotRecorded[0].Why == "" {
		t.Fatalf("not recorded: %+v", got.NotRecorded)
	}
}

// TestSinceIsDaysHoursOrAUnixTime.
func TestSinceIsDaysHoursOrAUnixTime(t *testing.T) {
	now := time.Unix(1_790_400_000, 0)
	for raw, want := range map[string]time.Time{
		"":           now.Add(-14 * 24 * time.Hour),
		"14d":        now.Add(-14 * 24 * time.Hour),
		"36h":        now.Add(-36 * time.Hour),
		"1790000000": time.Unix(1_790_000_000, 0),
	} {
		got, err := ParseCompareSince(raw, now)
		if err != nil || !got.Equal(want) {
			t.Fatalf("%q: %v %v, wanted %v", raw, got, err, want)
		}
	}
	for _, raw := range []string{"d", "0d", "-3d", "14w", "1.5d", "3651d", "abc", "14 d", "9999999999999"} {
		if _, err := ParseCompareSince(raw, now); !errors.Is(err, ErrCompareSince) {
			t.Fatalf("%q was taken: %v", raw, err)
		}
	}
}
