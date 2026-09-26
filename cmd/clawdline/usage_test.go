package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

// A session's bill as the daemon answers it: impl the most expensive, rules
// an upper bound, nothing spent on board, one gap.
const usageSessionAnswer = `{"conversation":"` + thinConversation + `","assistant":"claude","read_at":1790000000,"more":false,
"calls":12,"peak_context":150000,"compactions":1,"calls_above":2,
"above":{"input":0,"cache_write_1h":0,"cache_write_5m":0,"cache_read":0,"output":0,"total":0,"cost":0.5,"cost_known":true,"unpriced":0},
"bill":{"share_of":"cost",
 "total":{"input":0,"cache_write_1h":0,"cache_write_5m":0,"cache_read":0,"output":0,"total":1500000,"cost":10,"cost_known":true,"unpriced":0},
 "categories":[
  {"name":"board","share":0,"upper_bound":false,"tokens":{"input":0,"cache_write_1h":0,"cache_write_5m":0,"cache_read":0,"output":0,"total":0,"cost":0,"cost_known":true,"unpriced":0}},
  {"name":"rules","share":0.2,"upper_bound":true,"tokens":{"input":0,"cache_write_1h":0,"cache_write_5m":0,"cache_read":0,"output":0,"total":300000,"cost":2,"cost_known":true,"unpriced":0}},
  {"name":"impl","share":0.8,"upper_bound":false,"tokens":{"input":0,"cache_write_1h":0,"cache_write_5m":0,"cache_read":0,"output":0,"total":1200000,"cost":8,"cost_known":true,"unpriced":0}}
 ]},
"subagents":[],"gaps":[{"kind":"subagent","id":"agent-1","reason":"transcript_unreadable","counted":true}]}`

// With no flag it is this session's own bill, from the environment: a header
// line, then the categories by cost with rules marked, then the gaps.
func TestUsageReportsTheCallingSessionByDefault(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, usageSessionAnswer })
	var out, errs bytes.Buffer
	code := showUsage(&out, &errs, b, usageAsk{}, envOf(map[string]string{"CLAUDE_CODE_SESSION_ID": thinConversation}))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 1 || seen[0].Method != http.MethodGet || seen[0].EscapedPath != "/v1/usage/sessions/"+thinConversation ||
		seen[0].Token != thinToken {
		t.Fatalf("requests: %+v", seen)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	want := []string{
		"session " + thinConversation + ": 12 calls, peak context 150.0k, $10.00, 2 calls above 200k context ($0.50), 1 compactions",
		"  impl        80.0% of cost     1.20M tokens  $8.00",
		"  rules       20.0% of cost    300.0k tokens  $2.00  (upper bound)",
		"gap: subagent agent-1 transcript_unreadable (an earlier reading counted)",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("output:\n%s\nwant:\n%s", out.String(), strings.Join(want, "\n"))
	}
}

// --json prints the daemon's answer; --task and --item ask their own routes.
func TestUsageJSONAndTheOtherRoutes(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, usageSessionAnswer })
	var out, errs bytes.Buffer
	if code := showUsage(&out, &errs, b, usageAsk{Session: "abc", JSON: true}, envOf(nil)); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if !strings.Contains(out.String(), `"share_of": "cost"`) || !strings.Contains(out.String(), `"upper_bound": true`) {
		t.Fatalf("--json:\n%s", out.String())
	}
	for _, ask := range []usageAsk{{Task: "t-1", JSON: true}, {Item: "i-1", JSON: true}} {
		out.Reset()
		if code := showUsage(&out, &errs, b, ask, envOf(nil)); code != 0 {
			t.Fatalf("%+v: exit %d", ask, code)
		}
	}
	var paths []string
	for _, r := range s.requests() {
		paths = append(paths, r.EscapedPath)
	}
	if strings.Join(paths, " ") != "/v1/usage/sessions/abc /v1/usage/tasks/t-1 /v1/usage/items/i-1" {
		t.Fatalf("paths: %v", paths)
	}
}

// A session the ledger has not read says so in its header, not as an empty
// bill; a refusal is printed like every thin command's and exits 1; no
// conversation to name exits 2 before asking anything.
func TestUsageSaysNotYetReadAndRefusals(t *testing.T) {
	_, b := newStandIn(t, func(r *http.Request) (int, string) {
		if strings.HasSuffix(r.URL.Path, "/none") {
			return 404, `{"error":"unknown_session","detail":"The ledger has no reading of this conversation and no transcript names it."}`
		}
		return 200, `{"conversation":"new","reason":"not_yet_read","read_at":0,"more":false,"calls":0,"peak_context":0,
"compactions":0,"calls_above":0,"above":{},"bill":{"share_of":"cost","total":{"cost_known":true},"categories":[]},
"subagents":[],"gaps":[{"kind":"session","id":"new","reason":"not_yet_read","counted":false}]}`
	})
	var out, errs bytes.Buffer
	if code := showUsage(&out, &errs, b, usageAsk{Session: "new"}, envOf(nil)); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if want := "session new: not_yet_read — the ledger has no reading of it yet\n" +
		"gap: session new not_yet_read (nothing of it counted)\n"; out.String() != want {
		t.Fatalf("output:\n%q", out.String())
	}
	out.Reset()
	if code := showUsage(&out, &errs, b, usageAsk{Session: "none"}, envOf(nil)); code != 1 {
		t.Fatalf("a refusal exited %d", code)
	}
	if want := "clawdline usage: refused, 404 unknown_session: The ledger has no reading of this conversation and no transcript names it.\n"; errs.String() != want {
		t.Fatalf("stderr: %q", errs.String())
	}
	errs.Reset()
	if code := showUsage(&out, &errs, b, usageAsk{}, envOf(nil)); code != 2 || !strings.Contains(errs.String(), "CLAUDE_CODE_SESSION_ID") {
		t.Fatalf("no conversation: exit %d, %s", code, errs.String())
	}
}

// A comparison as the daemon answers it: none and 300000 comparable, 60000
// too few, two tasks left out, and the one signal nothing records.
const compareAnswer = `{"since":1789200000,"until":1790409600,"min_tasks":5,"truncated":false,"excluded":2,"excluded_truncated":false,
"excluded_tasks":[{"task_id":"x1","reason":"codex"},{"task_id":"x2","reason":"window_unrecorded"}],
"not_recorded":[{"name":"finish_refusals","why":"nothing keeps them."}],
"groups":[
 {"group":"none","window":0,"tasks":5,"read_tasks":4,"sessions":4,"too_few":false,"cost_total":56,"cost_median_per_task":13,"cost_known":true,
  "calls_per_task":50,"compactions_per_task":0.25,"peak_context_median":500000,"peak_context_max":900000,"above_200k_share":0.5556,
  "ended":5,"running":0,"success":2,"failure":1,"timeout":1,"cancelled":0,"stalled":1,"lost":0,
  "success_rate":0.4,"failure_rate":0.2,"timeout_rate":0.2,"stalled_rate":0.2,"respawns":1},
 {"group":"60000","window":60000,"tasks":2,"read_tasks":2,"sessions":2,"too_few":true,"cost_total":5,"cost_median_per_task":2.5,"cost_known":false,
  "calls_per_task":10,"compactions_per_task":3,"peak_context_median":70000,"peak_context_max":80000,"above_200k_share":null,
  "ended":2,"running":0,"success":1,"failure":1,"timeout":0,"cancelled":0,"stalled":0,"lost":0,
  "success_rate":null,"failure_rate":null,"timeout_rate":null,"stalled_rate":null,"respawns":0},
 {"group":"300000","window":300000,"tasks":5,"read_tasks":4,"sessions":5,"too_few":false,"cost_total":28,"cost_median_per_task":7,"cost_known":true,
  "calls_per_task":40,"compactions_per_task":1.75,"peak_context_median":285000,"peak_context_max":310000,"above_200k_share":0.0714,
  "ended":4,"running":1,"success":3,"failure":0,"timeout":0,"cancelled":0,"stalled":0,"lost":1,
  "success_rate":0.75,"failure_rate":0,"timeout_rate":0,"stalled_rate":0,"respawns":0}]}`

// --compare-compaction asks its route with --since as its query and prints
// one table: a row per group, then what the answer could not compare.
func TestUsageCompareCompactionPrintsOneTable(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, compareAnswer })
	var out, errs bytes.Buffer
	if code := showCompactionComparison(&out, &errs, b, "30d", false); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 1 || seen[0].Method != http.MethodGet || seen[0].EscapedPath != "/v1/usage/compare-compaction" ||
		seen[0].Query != "since=30d" || seen[0].Token != thinToken {
		t.Fatalf("requests: %+v", seen)
	}
	want := strings.Join([]string{
		"child tasks created 2026-09-12 08:00Z – 2026-09-26 08:00Z, by the compaction window they were launched with",
		"   group  sessions  tasks  cost/task  calls/task  compactions/task  above-200k share  success rate  stalled  respawns",
		"    none         4      5     $13.00        50.0              0.25               56%           40%        1         1",
		"   60000         2      2     $2.50+        10.0              3.00           too few       too few        0         0",
		"  300000         5      5      $7.00        40.0              1.75                7%           75%        0         0",
		"cost/task is the median over the tasks the ledger has read; + means part of it has no price.",
		"none: 0 still running, 1 not read by the ledger yet",
		"60000: 2 tasks, fewer than 5 — too few to compare, so no percentage is shown",
		"300000: 1 still running, 1 not read by the ledger yet",
		"excluded: 2 tasks with no known window (1 codex, 1 window_unrecorded)",
		"not recorded: finish_refusals — nothing keeps them.",
	}, "\n") + "\n"
	if out.String() != want {
		t.Fatalf("output:\n%s\nwant:\n%s", out.String(), want)
	}

	// --json prints the answer; with no --since the route's default is asked.
	out.Reset()
	if code := showCompactionComparison(&out, &errs, b, "", true); code != 0 || !strings.Contains(out.String(), `"min_tasks": 5`) {
		t.Fatalf("--json: exit %d\n%s", code, out.String())
	}
	if last := s.requests()[1]; last.Query != "" {
		t.Fatalf("no --since asked %q", last.Query)
	}
}
