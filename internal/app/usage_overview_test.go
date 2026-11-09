package app

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOverviewConservesProjectsPartsAndCompletionTaskCohorts(t *testing.T) {
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
	h := newUsageHarness(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	h.u.Now = func() time.Time { return now }
	task := store.BrokerRow{ID: "t", Project: "example", Assistant: "claude", State: "success", CreatedAt: now.AddDate(0, 0, -2), UpdatedAt: now,
		Record: json.RawMessage(`{"spawned_at":"2026-10-17T12:00:00Z","finished_at":"2026-10-18T12:00:00Z","result":{"status":"success"}}`)}
	if err := h.store.SaveBrokerTask(ctx, task, nil); err != nil {
		t.Fatal(err)
	}
	for _, r := range []store.UsageRow{
		{Assistant: "claude", Conversation: "root", TaskID: "t", ReadAt: now, State: json.RawMessage(`{"daily_version":1,"daily":{"2026-10-17":{"input":10,"cache_read":20,"output":3}}}`)},
		{Assistant: "claude", Conversation: "sub", Parent: "root", ReadAt: now, State: json.RawMessage(`{"daily_version":1,"daily":{"2026-10-17":{"input":5,"output":2}}}`)},
		{Assistant: "codex", Conversation: "unowned", ReadAt: now, State: json.RawMessage(`{"daily_version":1,"daily":{"2026-10-18":{"input":7,"cache_write_1h":4},"2026-10-10":{"input":13}}}`)},
		{Assistant: "codex", Conversation: "legacy", ReadAt: now, State: json.RawMessage(`{"offset":100}`)},
	} {
		if err := h.store.SaveUsageRow(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := h.u.Overview(ctx, 7, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Totals.Tokens == nil || *got.Totals.Tokens != 51 || got.PreviousTotals.Tokens == nil || *got.PreviousTotals.Tokens != 13 {
		t.Fatalf("totals %+v / %+v", got.Totals, got.PreviousTotals)
	}
	p := got.Totals.Parts
	if p.Input+p.CacheRead+p.CacheWrite+p.Output != *got.Totals.Tokens {
		t.Fatal("parts differ")
	}
	sum := 0.0
	for _, p := range got.Projects {
		sum += p.Tokens
	}
	if sum != *got.Totals.Tokens {
		t.Fatal("projects differ")
	}
	if len(got.Daily) != 7 || len(got.PreviousDaily) != 7 || got.Daily[0].Tokens != nil || got.Daily[0].Coverage != "missing" {
		t.Fatalf("days %+v", got.Daily)
	}
	if got.Totals.Dispatches != 1 || got.Totals.DurationSamples != 1 || got.Totals.DurationMedianSeconds == nil || *got.Totals.DurationMedianSeconds != 86400 || got.Daily[5].Dispatches != 1 || got.Daily[5].DurationSamples != 0 || got.Daily[6].DurationSamples != 1 {
		t.Fatalf("cohorts %+v %+v", got.Totals, got.Daily)
	}
	if got.Coverage.State != "partial" || got.Coverage.SessionsRead != 3 || got.Coverage.SessionsTotal != 4 {
		t.Fatalf("coverage %+v", got.Coverage)
	}
	id := overviewProject("example").ID
	filtered, err := h.u.Overview(ctx, 7, id)
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Totals.Tokens == nil || *filtered.Totals.Tokens != 40 || len(filtered.ProjectOptions) != len(got.ProjectOptions) || len(filtered.Projects) != 1 {
		t.Fatalf("filtered %+v", filtered)
	}
	if _, err = h.u.Overview(ctx, 7, "unknown"); err != ErrUsageOverviewProject {
		t.Fatalf("unknown %v", err)
	}
	if _, err = h.u.Overview(ctx, 8, ""); err != ErrUsageOverviewDays {
		t.Fatalf("days %v", err)
	}
	raw, _ := json.Marshal(got)
	var wire map[string]any
	json.Unmarshal(raw, &wire)
	first := wire["daily"].([]any)[0].(map[string]any)
	if first["tokens"] != nil || first["duration_median_seconds"] != nil {
		t.Fatalf("null gap %s", raw)
	}
}

func TestOverviewCompleteCalendarWindowsRespectDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	old := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = old })
	h := newUsageHarness(t)
	h.u.Now = func() time.Time { return time.Date(2026, 3, 10, 12, 0, 0, 0, loc) }
	got, err := h.u.Overview(context.Background(), 7, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Until-got.Since != 7*86400-3600 || got.Daily[0].Date != "2026-03-03" || got.Daily[6].Date != "2026-03-09" || got.Totals.Tokens != nil || got.Coverage.State != "missing" {
		t.Fatalf("DST %+v", got)
	}
	long, err := h.u.Overview(context.Background(), 28, "")
	if err != nil || len(long.Daily) != 28 || len(long.PreviousDaily) != 28 {
		t.Fatalf("28 days %v", err)
	}
}

func TestOverviewBackgroundPassReplaysLegacyAndSurvivesRestart(t *testing.T) {
	h := newUsageHarness(t)
	now := time.Now()
	stamp := now.AddDate(0, 0, -1).Format(time.RFC3339)
	line := fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"id":"one","usage":{"input_tokens":100,"output_tokens":10},"content":[]}}`, stamp)
	path := h.session("daily")
	h.write(path, line, line)
	if err := h.store.SaveUsageRow(context.Background(), store.UsageRow{Assistant: "claude", Conversation: "daily", Path: path, ReadAt: now, State: json.RawMessage(`{"offset":9999}`)}); err != nil {
		t.Fatal(err)
	}
	h.pass()
	got, err := h.u.Overview(context.Background(), 7, "")
	if err != nil || got.Totals.Tokens == nil || *got.Totals.Tokens != 110 {
		t.Fatalf("first %+v %v", got.Totals, err)
	}
	h.open()
	h.pass()
	next, err := h.u.Overview(context.Background(), 7, "")
	if err != nil || next.Totals.Tokens == nil || *next.Totals.Tokens != 110 {
		t.Fatalf("restart %+v %v", next.Totals, err)
	}
	// GET remains usable with sources gone and never attempts another read.
	h.u.Home = "/not-a-home"
	next, err = h.u.Overview(context.Background(), 7, "")
	if err != nil || *next.Totals.Tokens != 110 {
		t.Fatal("GET depends on source")
	}
}

func TestOverviewAttributesUnownedLinkedWorktreesToTheirRepository(t *testing.T) {
	h := newUsageHarness(t)
	repo := t.TempDir()
	wt := t.TempDir()
	gitdir := filepath.Join(repo, ".git", "worktrees", "w")
	if err := os.MkdirAll(gitdir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+gitdir+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitdir, "commondir"), []byte("../..\n"), 0600); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"type":"assistant","cwd":%q,"timestamp":%q,"message":{"id":"one","usage":{"input_tokens":100,"output_tokens":10}}}`, wt, time.Now().AddDate(0, 0, -1).Format(time.RFC3339))
	h.write(h.session("unowned"), line)
	h.pass()
	canonical, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.u.Overview(context.Background(), 7, overviewProject(canonical).ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Totals.Tokens == nil || *got.Totals.Tokens != 110 || len(got.Projects) != 1 {
		t.Fatalf("worktree attribution %+v", got)
	}
}

// Marshal the broker's actual record type so changes to its stored wire cannot
// silently turn completion durations into missing samples.
func TestOverviewIncludesWorkCreatedBeforeTheWindowAndCompletedInsideIt(t *testing.T) {
	h := newUsageHarness(t)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	h.u.Now = func() time.Time { return now }
	finish := now.AddDate(0, 0, -1)
	record := orchestrator.Record{SpawnedAt: finish.Add(-time.Hour), FinishedAt: finish, Result: &taskdir.Result{Status: "success"}}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	err = h.store.SaveBrokerTask(context.Background(), store.BrokerRow{ID: "old-created", Project: "example", Assistant: "claude", State: "success", CreatedAt: now.AddDate(0, 0, -30), UpdatedAt: now, Record: raw}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.u.Overview(context.Background(), 7, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Totals.Dispatches != 0 || got.Totals.DurationSamples != 1 || got.Daily[6].DurationSamples != 1 || got.Totals.DurationMedianSeconds == nil || *got.Totals.DurationMedianSeconds != 3600 {
		t.Fatalf("completion cohort: %+v", got)
	}
}

func TestOverviewCountsAgentDispatchesWithoutBackgroundCallbacks(t *testing.T) {
	h := newUsageHarness(t)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	h.u.Now = func() time.Time { return now }
	at := now.AddDate(0, 0, -1)
	for _, kind := range []string{"callback", "implementation"} {
		raw, err := json.Marshal(orchestrator.Record{Kind: kind, SpawnedAt: at, FinishedAt: at.Add(10 * time.Minute), Result: &taskdir.Result{Status: "success"}})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.store.SaveBrokerTask(context.Background(), store.BrokerRow{ID: kind, Project: "example", Assistant: "claude", State: "success", CreatedAt: at, UpdatedAt: at, Record: raw}, nil); err != nil {
			t.Fatal(err)
		}
	}
	got, err := h.u.Overview(context.Background(), 7, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Totals.Dispatches != 1 || got.Totals.DurationSamples != 1 || got.Daily[6].Dispatches != 1 || got.Daily[6].DurationSamples != 1 {
		t.Fatalf("background callbacks are not agent dispatches: totals=%+v day=%+v", got.Totals, got.Daily[6])
	}
}

func TestOverviewReturnsHistoricalWorktreeUsageToItsSourceProject(t *testing.T) {
	h := newUsageHarness(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	h.u.Now = func() time.Time { return now }
	for _, task := range []store.BrokerRow{
		{ID: "first", Assistant: "claude", Project: "/example/source", Repository: "/example/source", Record: json.RawMessage(`{"child_conversation_id":"from-conversation","worktree":{"path":"/removed/first"}}`)},
		{ID: "next", Assistant: "codex", Project: "/removed/first", Repository: "/removed/first", Record: json.RawMessage(`{"worktree":{"path":"/removed/next"}}`)},
	} {
		task.CreatedAt, task.UpdatedAt, task.State = now.AddDate(0, 0, -1), now, "success"
		if err := h.store.SaveBrokerTask(ctx, task, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, session := range []struct{ id, assistant, task, parent, cwd string }{
		{"from-id", "claude", "first", "", "/removed/first"},
		{"from-conversation", "claude", "", "", "/removed/first"},
		{"from-path", "codex", "", "", "/removed/next/subdirectory"},
		{"native", "claude", "", "from-id", ""},
		{"nested-native", "claude", "", "native", ""},
		{"from-nested-task", "codex", "next", "", "/removed/next"},
		{"unknown", "codex", "", "", "/removed/orphan"},
		{"missing-task", "codex", "no-record", "", "/removed/first"},
	} {
		raw, _ := json.Marshal(map[string]any{"daily_version": 1, "daily_project": session.cwd,
			"daily": map[string]any{"2026-10-19": map[string]any{"input": 10}, "2026-10-10": map[string]any{"input": 5}}})
		if err := h.store.SaveUsageRow(ctx, store.UsageRow{Assistant: session.assistant, Conversation: session.id, TaskID: session.task, Parent: session.parent, State: raw}); err != nil {
			t.Fatal(err)
		}
	}
	// No transcript or checkout exists; attribution must survive restart and
	// recover existing summaries without replaying or duplicating token events.
	h.open()
	h.u.Now = func() time.Time { return now }
	all, err := h.u.Overview(ctx, 7, "")
	if err != nil {
		t.Fatal(err)
	}
	if all.Totals.Tokens == nil || *all.Totals.Tokens != 80 || *all.PreviousTotals.Tokens != 40 || len(all.Projects) != 2 || len(all.ProjectOptions) != 2 {
		t.Fatalf("historical totals or project options: %+v", all)
	}
	source, err := h.u.Overview(ctx, 7, overviewProject("/example/source").ID)
	if err != nil || source.Totals.Tokens == nil || *source.Totals.Tokens != 60 || *source.PreviousTotals.Tokens != 30 || source.Totals.Dispatches != 2 || len(source.Projects) != 1 || source.Projects[0].Name != "source" {
		t.Fatalf("source project: %+v, %v", source, err)
	}
	unknown, err := h.u.Overview(ctx, 7, "unattributed")
	if err != nil || unknown.Totals.Tokens == nil || *unknown.Totals.Tokens != 20 || *unknown.PreviousTotals.Tokens != 10 {
		t.Fatalf("unattributed: %+v, %v", unknown, err)
	}
	if *source.Totals.Tokens+*unknown.Totals.Tokens != *all.Totals.Tokens || *source.Daily[6].Tokens != 60 {
		t.Fatal("project filtering changed the included tokens")
	}
}

func TestOverviewBackfillsProjectIdentityWithoutReplayingMissingSources(t *testing.T) {
	h := newUsageHarness(t)
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := json.Marshal(transcript.LedgerState{DailyVersion: 1, ClassificationVersion: transcript.LedgerClassificationVersion,
		DailyProject: repo, Daily: map[string]transcript.Tokens{"2026-10-19": {Input: 23}}})
	old := store.UsageRow{Assistant: "claude", Conversation: "gone", State: state, Reason: store.UsageTranscriptMissing, Measured: json.RawMessage(`{"input":23}`)}
	f := usageFile{assistant: old.Assistant, conversation: old.Conversation, gone: true}
	if !usageDue(f, old, true) {
		t.Fatal("old attribution never becomes due when a source is gone")
	}
	row, _, err := h.u.read(f, old, true, time.Now())
	if err != nil || string(row.Measured) != string(old.Measured) || usageDue(f, row, true) {
		t.Fatalf("attribution must preserve billing and settle once: %+v, %v", row, err)
	}
	if err := h.store.SaveUsageRow(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	rows, err := h.store.UsageDaily(context.Background(), "2026-10-19", "2026-10-20")
	if err != nil || len(rows) != 1 || rows[0].Project != canonical || rows[0].Input != 23 {
		t.Fatalf("backfilled project: %+v, %v", rows, err)
	}
}

func TestOverviewAgentDistributionConservesFilteredTokens(t *testing.T) {
	h := newUsageHarness(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	h.u.Now = func() time.Time { return now }
	for _, sample := range []struct {
		assistant, conversation, task string
		tokens                        int
	}{
		{"claude", "main", "source", 10}, {"claude", "child", "source", 20},
		{"codex", "code", "source", 30}, {"antigravity", "native", "source", 40},
		{"gemini", "alias", "source", 5}, {"grok", "future", "other", 50}, {"new-agent", "next", "other", 0},
	} {
		raw := json.RawMessage(fmt.Sprintf(`{"daily_version":1,"daily":{"2026-10-19":{"input":%d,"output":2,"cache_read":3,"cache_write_5m":4},"2026-10-10":{"input":999}}}`, sample.tokens))
		if err := h.store.SaveUsageRow(ctx, store.UsageRow{Assistant: sample.assistant, Conversation: sample.conversation, TaskID: sample.task, ReadAt: now, State: raw}); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.store.SaveUsageRow(ctx, store.UsageRow{Assistant: "zero-agent", Conversation: "zero", TaskID: "other", ReadAt: now, State: json.RawMessage(`{"daily_version":1,"daily":{"2026-10-19":{"input":0}}}`)}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"source", "other"} {
		if err := h.store.SaveBrokerTask(ctx, store.BrokerRow{ID: id, Project: id, Assistant: "claude", CreatedAt: now, UpdatedAt: now, Record: json.RawMessage(`{}`)}, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, project := range []string{"", overviewProject("source").ID, "unattributed"} {
		got, err := h.u.Overview(ctx, 7, project)
		if err != nil {
			t.Fatal(err)
		}
		agents := map[string]*float64{}
		sum := 0.0
		for _, agent := range got.Agents {
			agents[agent.ID] = agent.Tokens
			if agent.Tokens != nil {
				sum += *agent.Tokens
			}
		}
		if got.Totals.Tokens != nil && sum != *got.Totals.Tokens {
			t.Fatalf("agent total %v != overview %+v", sum, got)
		}
		if project == "" && (len(agents) != 6 || *agents["gemini"] != 63 || *agents["claude"] != 48 || *agents["new-agent"] != 9 || agents["zero-agent"] == nil || *agents["zero-agent"] != 0) {
			t.Fatalf("all agents %+v", got.Agents)
		}
		if project == overviewProject("source").ID && (agents["grok"] != nil || *agents["gemini"] != 63 || sum != 150) {
			t.Fatalf("filtered agents %+v sum %v", got.Agents, sum)
		}
		if project == "unattributed" && (sum != 0 || len(agents) != 4 || agents["claude"] != nil) {
			t.Fatalf("missing agent became measured zero %+v", got.Agents)
		}
	}
}

func TestUsagePassDiscoversNativeGeminiAndReplacesSnapshots(t *testing.T) {
	h := newUsageHarness(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	h.u.Now = func() time.Time { return now }
	path, ok := transcript.AntigravityPath(h.home, "70000000-0000-4000-8000-000000000001")
	if !ok {
		t.Fatal("native path refused")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	line := `{"step_index":0,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-10-19T12:00:00Z","input_tokens":10,"cache_read_tokens":3,"output_tokens":2}` + "\n"
	h.write(path, line)
	for pass := 0; pass < 2; pass++ {
		h.pass()
		got, err := h.u.Overview(context.Background(), 7, "")
		if err != nil {
			t.Fatal(err)
		}
		if got.Totals.Tokens == nil || *got.Totals.Tokens != 15 || len(got.Projects) != 1 || got.Projects[0].ID != "unattributed" {
			t.Fatalf("native summary %+v", got)
		}
		if got.Agents[0].ID != "gemini" || got.Agents[0].Tokens == nil || *got.Agents[0].Tokens != 15 {
			t.Fatalf("native agent %+v", got.Agents)
		}
		h.open()
		h.u.Now = func() time.Time { return now }
	}
	h.write(path, `{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-10-19T13:00:00Z","input_tokens":4,"cache_read_tokens":0,"output_tokens":1}`+"\n")
	h.pass()
	got, err := h.u.Overview(context.Background(), 7, "")
	if err != nil || got.Totals.Tokens == nil || *got.Totals.Tokens != 20 {
		t.Fatalf("appended snapshot %+v %v", got, err)
	}
}
