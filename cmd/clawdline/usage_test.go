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
