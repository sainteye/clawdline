package transcript

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Every fixture under testdata/ledger is synthetic: written for these tests,
// never copied from a real transcript.

var ledgerFixtures = []string{"claude.jsonl", "claude_plain.jsonl", "codex.jsonl"}

func fixture(name string) string { return filepath.Join("testdata", "ledger", name) }

func feedAll(t *testing.T, path string) *LedgerState {
	t.Helper()
	var s LedgerState
	for {
		res, err := s.Feed(path)
		if err != nil {
			t.Fatalf("feed %s: %v", path, err)
		}
		if !res.More {
			return &s
		}
	}
}

func near(a, b float64) bool { return math.Abs(a-b) <= 1e-6*math.Max(1, math.Abs(b)) }

func sumSpent(spent map[Category]Tokens) Tokens {
	var sum Tokens
	for _, c := range Categories {
		sum.add(spent[c])
	}
	return sum
}

func TestClassifyDecidesByWhatACallDoes(t *testing.T) {
	cases := []struct {
		name, tool, input string
		want              Category
	}{
		{"a curl to the daemon", "Bash", `{"command":"curl --fail-with-body -sS -X POST http://127.0.0.1:7727/v1/orchestrator/tasks/t/accepted -H \"X-Clawdline-Task-Secret: s\""}`, CategoryProtocol},
		{"a session report", "Bash", `{"command":"clawdline session report --summary done"}`, CategoryProtocol},
		{"an item's steps", "Bash", `{"command":"clawdline item steps 12"}`, CategoryBoard},
		{"a Root Assignment", "Bash", `{"command":"cat /example/root-assignments/r1/ASSIGNMENT.md"}`, CategoryBoard},
		{"the repository's rules", "Bash", `{"command":"cat AGENTS.md"}`, CategoryRules},
		{"a repository guard", "Bash", `{"command":"tools/check-private.sh"}`, CategoryRules},
		{"mentioning is not doing", "Bash", `{"command":"grep -rn /v1/orchestrator internal"}`, CategoryImpl},
		{"reading source", "Read", `{"file_path":"/example/repo/internal/app/x.go"}`, CategoryImpl},
		{"an edit", "Edit", `{"file_path":"/example/repo/AGENTS.md","old_string":"a","new_string":"b"}`, CategoryImpl},
		{"a subagent", "Agent", `{"prompt":"look"}`, CategoryDelegate},
		{"loading tools", "ToolSearch", `{"query":"select:Read"}`, CategoryHarness},
		{"asking the person", "AskUserQuestion", `{"questions":[]}`, CategoryTalk},
		{"reading the briefing", "Read", `{"file_path":"/example/tasks/t/CHILD.md"}`, CategoryProtocol},
		{"writing the result", "Write", `{"file_path":"/example/tasks/t/result.json.tmp","content":"{}"}`, CategoryProtocol},
		{"the clawdline skill", "Skill", `{"skill":"clawdline"}`, CategoryProtocol},
		{"a board read over HTTP", "Bash", `{"command":"curl -sS http://127.0.0.1:7727/v1/work/items/3"}`, CategoryBoard},
		{"the daemon's binary by path", "Bash", `{"command":"'/Applications/Clawdline Next.app/Contents/MacOS/clawdline' task finish --port 7727 /x"}`, CategoryProtocol},
		{"a guard after a build takes the command", "Bash", `{"command":"cd /r && go build ./... && tools/check-private.sh -history -new"}`, CategoryRules},
		{"a curl elsewhere", "Bash", `{"command":"curl -sS https://example.com/v1/orchestrator"}`, CategoryImpl},
		{"a quoted separator is not a command", "Bash", `{"command":"grep -n 'clawdline item|tools/check-x.sh' docs/a.md"}`, CategoryImpl},
		{"Codex exec_command", "exec_command", `{"cmd":"clawdline item show 4"}`, CategoryBoard},
		{"Codex shell vector", "shell", `{"command":["bash","-lc","cat CLAUDE.md"]}`, CategoryRules},
		{"a memory file", "Read", `{"file_path":"/example/.claude/projects/p/memory/MEMORY.md"}`, CategoryRules},
		{"an unknown tool", "mcp__docs__read", `{}`, CategoryOther},
	}
	for _, tc := range cases {
		if got := Classify(tc.tool, json.RawMessage(tc.input)); got != tc.want {
			t.Errorf("%s: Classify(%s, %s) = %s, want %s", tc.name, tc.tool, tc.input, got, tc.want)
		}
	}
}

// Every category's tokens add up to what the session measured, part by part,
// on every fixture.
func TestEveryCategorySumsToTheMeasuredCount(t *testing.T) {
	for _, name := range ledgerFixtures {
		s := feedAll(t, fixture(name))
		spent, measured := s.Totals()
		sum := sumSpent(spent)
		for _, part := range []struct {
			name      string
			got, want float64
		}{
			{"input", sum.Input, measured.Input},
			{"cache_write_1h", sum.CacheWrite1h, measured.CacheWrite1h},
			{"cache_write_5m", sum.CacheWrite5m, measured.CacheWrite5m},
			{"cache_read", sum.CacheRead, measured.CacheRead},
			{"output", sum.Output, measured.Output},
		} {
			if !near(part.got, part.want) {
				t.Errorf("%s: categories sum to %v %s, measured %v", name, part.got, part.name, part.want)
			}
		}
		if measured.Total() == 0 {
			t.Errorf("%s: measured nothing", name)
		}
	}
}

func TestAClaudeTranscriptIsReadIntoCalls(t *testing.T) {
	s := feedAll(t, fixture("claude.jsonl"))
	spent, measured := s.Totals()
	// The fixture's calls by hand: six of the session's own, msg_1 and msg_2
	// written twice in a row and msg_2 once more further down, and two of a
	// subagent's, msg_s1 written twice.
	want := Tokens{Input: 26, CacheWrite1h: 255_900, CacheWrite5m: 500, CacheRead: 354_019, Output: 1260}
	if !reflect.DeepEqual(measured, want) {
		t.Errorf("measured = %+v, want %+v", measured, want)
	}
	if s.Calls != 6 || s.SidechainCalls != 2 {
		t.Errorf("calls = %d, sidechain = %d; want 6 and 2", s.Calls, s.SidechainCalls)
	}
	if s.PeakContext != 234_508 || s.CallsAbove != 2 {
		t.Errorf("peak = %d, above = %d; want 234508 and 2", s.PeakContext, s.CallsAbove)
	}
	if s.Undecodable != 1 {
		t.Errorf("undecodable = %d, want 1", s.Undecodable)
	}
	// A subagent's whole bill is delegate: its 2,195 tokens, and the
	// session's own Agent call's output and the result it brought back.
	if d := spent[CategoryDelegate]; d.Total() < 2195+300 {
		t.Errorf("delegate = %v, want at least the subagent's 2195 and the Agent call's 300 output", d.Total())
	}
	for _, c := range []Category{CategoryProtocol, CategoryRules, CategoryImpl, CategoryBoard, CategoryHarness, CategoryTalk} {
		if spent[c].Total() <= 0 {
			t.Errorf("%s is empty: %+v", c, spent)
		}
	}
	// The first message of a child is protocol, not talk, and the curl of
	// its first call too: protocol holds at least that call's 120 output.
	if s.Base[CategoryProtocol] <= 0 || s.Base[CategoryTalk] != 0 {
		t.Errorf("the child's first message is protocol in the base: %+v", s.Base)
	}
	if spent[CategoryProtocol].Output < 120 {
		t.Errorf("protocol output = %v, want the first call's 120", spent[CategoryProtocol].Output)
	}
	if !spent[CategoryImpl].CostKnown() || spent[CategoryImpl].Cost <= 0 {
		t.Errorf("an Opus session's cost is known: %+v", spent[CategoryImpl])
	}
}

func TestACompactionKeepsTheBaseAndCallsTheRestCompaction(t *testing.T) {
	s := feedAll(t, fixture("claude.jsonl"))
	if s.Compactions != 1 {
		t.Fatalf("compactions = %d, want 1", s.Compactions)
	}
	spent, _ := s.Totals()
	if spent[CategoryCompaction].Total() <= 0 {
		t.Errorf("compaction is empty after a compaction")
	}
	// The context after it is the base at its size and compaction above it.
	if !near(s.Segments.sum(), 40_808) {
		t.Errorf("segments sum to %v, want the last context 40808", s.Segments.sum())
	}
	if !near(s.Segments[CategoryRules], s.Base[CategoryRules]) {
		t.Errorf("rules after compaction = %v, base had %v", s.Segments[CategoryRules], s.Base[CategoryRules])
	}
}

func TestACodexRolloutIsReadIntoCalls(t *testing.T) {
	s := feedAll(t, fixture("codex.jsonl"))
	spent, measured := s.Totals()
	// Four token_counts, one of them written twice, and one with no usage.
	want := Tokens{Input: 15_400, CacheRead: 41_500, Output: 580}
	if !reflect.DeepEqual(measured, want) {
		t.Errorf("measured = %+v, want %+v", measured, want)
	}
	if s.Calls != 4 || s.Model != "gpt-5.6-sol" {
		t.Errorf("calls = %d, model = %q", s.Calls, s.Model)
	}
	// cat AGENTS.md is rules, the curl protocol, apply_patch impl.
	if spent[CategoryRules].Output != 150 || spent[CategoryProtocol].Output != 90 || spent[CategoryImpl].Output != 300 ||
		spent[CategoryTalk].Output != 40 {
		t.Errorf("outputs by category = rules %v protocol %v impl %v talk %v",
			spent[CategoryRules].Output, spent[CategoryProtocol].Output, spent[CategoryImpl].Output, spent[CategoryTalk].Output)
	}
	// A model with no price has an unknown cost, never a zero one.
	for _, c := range Categories {
		if spent[c].Total() > 0 && (spent[c].CostKnown() || spent[c].Cost != 0) {
			t.Errorf("%s: cost %v known=%v for a model with no price", c, spent[c].Cost, spent[c].CostKnown())
		}
	}
}

// Feeding a file in two halves, the state kept as JSON between them, gives
// exactly what feeding it once does — at every cut, mid-line included.
func TestTwoHalvesAreOneFeed(t *testing.T) {
	for _, name := range ledgerFixtures {
		data, err := os.ReadFile(fixture(name))
		if err != nil {
			t.Fatal(err)
		}
		once := feedAll(t, fixture(name))
		onceSpent, onceMeasured := once.Totals()
		for cut := 1; cut < len(data); cut += 1 + len(data)/41 {
			path := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(path, data[:cut], 0o600); err != nil {
				t.Fatal(err)
			}
			var first LedgerState
			if _, err := first.Feed(path); err != nil {
				t.Fatal(err)
			}
			kept, err := json.Marshal(first)
			if err != nil {
				t.Fatal(err)
			}
			var second LedgerState
			if err := json.Unmarshal(kept, &second); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if res, err := second.Feed(path); err != nil || res.Restarted {
				t.Fatalf("%s cut at %d: %v, restarted %v", name, cut, err, res.Restarted)
			}
			spent, measured := second.Totals()
			if !reflect.DeepEqual(spent, onceSpent) || measured != onceMeasured {
				t.Fatalf("%s cut at %d:\n halves %+v\n once   %+v", name, cut, spent, onceSpent)
			}
			if second.Offset != once.Offset || second.Calls != once.Calls || second.Compactions != once.Compactions ||
				!reflect.DeepEqual(second.Segments, once.Segments) {
				t.Fatalf("%s cut at %d: state differs: offset %d/%d calls %d/%d", name, cut,
					second.Offset, once.Offset, second.Calls, once.Calls)
			}
		}
	}
}

func TestATruncatedLastLineWaitsForTheRest(t *testing.T) {
	data, err := os.ReadFile(fixture("claude.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	s := feedAll(t, fixture("claude.jsonl"))
	last := strings.LastIndexByte(string(data), '\n') + 1
	if s.Offset != int64(last) {
		t.Fatalf("offset = %d, want %d: the unfinished last line is not consumed", s.Offset, last)
	}
	// The rest arrives: the call it carried is counted then.
	path := filepath.Join(t.TempDir(), "t.jsonl")
	whole := string(data[:last]) + `{"type":"assistant","isSidechain":false,"message":{"id":"msg_7","model":"claude-opus-5-5","content":[],"usage":{"input_tokens":1,"cache_creation_input_tokens":10,"cache_read_input_tokens":40808,"output_tokens":5}}}` + "\n"
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var grow LedgerState
	if _, err := grow.Feed(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(whole), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := grow.Feed(path); err != nil {
		t.Fatal(err)
	}
	if grow.Calls != 7 || grow.Offset != int64(len(whole)) {
		t.Errorf("after the rest: calls = %d, offset = %d/%d", grow.Calls, grow.Offset, len(whole))
	}
}

func TestAShrunkOrReplacedFileIsReadAgainFromZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	long, _ := os.ReadFile(fixture("claude.jsonl"))
	plain, _ := os.ReadFile(fixture("claude_plain.jsonl"))
	if err := os.WriteFile(path, long, 0o600); err != nil {
		t.Fatal(err)
	}
	var s LedgerState
	if _, err := s.Feed(path); err != nil {
		t.Fatal(err)
	}
	fresh := feedAll(t, fixture("claude_plain.jsonl"))
	freshSpent, freshMeasured := fresh.Totals()
	for _, step := range []struct {
		name string
		data []byte
	}{
		{"shrunk", plain},
		// The same size as what was read, with a different start.
		{"replaced", append(append([]byte{}, plain...), []byte(strings.Repeat("\n", len(long)-len(plain)))...)},
	} {
		if step.name == "replaced" {
			// Read the long file again first, so the replacement is not shorter.
			if err := os.WriteFile(path, long, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Feed(path); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(path, step.data, 0o600); err != nil {
			t.Fatal(err)
		}
		res, err := s.Feed(path)
		if err != nil || !res.Restarted {
			t.Fatalf("%s: restarted = %v, err %v", step.name, res.Restarted, err)
		}
		spent, measured := s.Totals()
		if !reflect.DeepEqual(spent, freshSpent) || measured != freshMeasured {
			t.Errorf("%s: after the restart %+v, want %+v", step.name, measured, freshMeasured)
		}
	}
	// Shrunk, the long file back, replaced.
	if s.Restarts != 3 {
		t.Errorf("restarts = %d, want 3", s.Restarts)
	}
}

func TestAnOverlongLineIsSkippedAndCounted(t *testing.T) {
	data, _ := os.ReadFile(fixture("claude_plain.jsonl"))
	long := `{"type":"user","message":{"content":"` + strings.Repeat("p", 5000) + `"}}` + "\n"
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, append([]byte(long), data...), 0o600); err != nil {
		t.Fatal(err)
	}
	// One Feed, the line under its limit.
	var s LedgerState
	if _, err := s.feed(path, 1<<20, 1000); err != nil {
		t.Fatal(err)
	}
	if s.Overlong != 1 || s.Calls != 3 {
		t.Errorf("overlong = %d, calls = %d; want 1 and 3", s.Overlong, s.Calls)
	}
	// Feeds smaller than the line: it is skipped across them, counted once.
	var small LedgerState
	for i := 0; i < 100 && small.Offset < int64(len(long)+len(data)); i++ {
		if _, err := small.feed(path, 700, 1000); err != nil {
			t.Fatal(err)
		}
	}
	if small.Overlong != 1 || small.Calls != 3 || small.Skipping {
		t.Errorf("across feeds: overlong = %d, calls = %d, skipping = %v", small.Overlong, small.Calls, small.Skipping)
	}
	spent, _ := small.Totals()
	sSpent, _ := s.Totals()
	if !reflect.DeepEqual(spent, sSpent) {
		t.Errorf("small feeds %+v, one feed %+v", spent, sSpent)
	}
}

func TestAFeedStopsAtItsLimitAndTheNextGoesOn(t *testing.T) {
	var s LedgerState
	res, err := s.feed(fixture("claude.jsonl"), 4096, ledgerLineLimit)
	// It finishes the line it is in — the fixture's first is 12 KB — and no more.
	if err != nil || !res.More || s.Offset != int64(strings.IndexByte(mustRead(t, fixture("claude.jsonl")), '\n')+1) {
		t.Fatalf("first feed: more %v offset %d err %v", res.More, s.Offset, err)
	}
	for i := 0; i < 1000 && res.More; i++ {
		if res, err = s.feed(fixture("claude.jsonl"), 4096, ledgerLineLimit); err != nil {
			t.Fatal(err)
		}
	}
	spent, _ := s.Totals()
	onceSpent, _ := feedAll(t, fixture("claude.jsonl")).Totals()
	if !reflect.DeepEqual(spent, onceSpent) {
		t.Errorf("limited feeds %+v, one feed %+v", spent, onceSpent)
	}
}

func TestTheBaseIsComposedOnlyFromASnapshot(t *testing.T) {
	s := feedAll(t, fixture("claude.jsonl"))
	c := s.Composition
	if c == nil {
		t.Fatal("no composition for a transcript with a prompt snapshot")
	}
	if c.Measured != 30_003 || !near(c.estimated(), 30_003) {
		t.Errorf("composition sums to %v, measured %d; want 30003", c.estimated(), c.Measured)
	}
	names := []string{}
	for _, f := range c.Instructions {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "AGENTS.md,CLAUDE.md" || len(c.Tools) != 2 || c.Tools[0].Name != "Bash" {
		t.Errorf("instructions %v, tools %+v", names, c.Tools)
	}
	if c.SystemPrompt <= c.Tools[0].Tokens || c.SkillListing <= 0 || c.MCP <= 0 || c.Other <= 0 {
		t.Errorf("composition = %+v", c)
	}
	if plain := feedAll(t, fixture("claude_plain.jsonl")); plain.Composition != nil {
		t.Errorf("a transcript with no snapshot has a composition: %+v", plain.Composition)
	}
}

func TestCostPricesCacheWritesByHowLongTheyAreKept(t *testing.T) {
	// Opus: $5 in, $25 out per million.
	cases := []struct {
		name string
		u    Summary
		want float64
		ok   bool
	}{
		{"1-hour writes at 2×", Summary{Model: "claude-opus-5", CacheWrite: 1_000_000, CacheWrite1h: 1_000_000}, 10, true},
		{"5-minute writes at 1.25×", Summary{Model: "claude-opus-5", CacheWrite: 1_000_000, CacheWrite5m: 1_000_000}, 6.25, true},
		{"a split", Summary{Model: "claude-opus-5", CacheWrite: 1_000_000, CacheWrite1h: 600_000, CacheWrite5m: 400_000}, 8.5, true},
		{"an unsplit write is 1-hour", Summary{Model: "claude-opus-5", CacheWrite: 1_000_000}, 10, true},
		{"reads at 0.1× and output", Summary{Model: "claude-opus-5", CacheRead: 1_000_000, Output: 1_000_000, Input: 1_000_000}, 30.5, true},
		{"an unknown model", Summary{Model: "gpt-5.6-sol", CacheWrite: 1_000_000, Input: 5}, 0, false},
	}
	for _, tc := range cases {
		got, ok := Cost(tc.u)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: Cost = %v, %v; want %v, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
	// The session card's Summary carries the split from the record.
	path := writeRecord(t, m{"type": "assistant", "message": m{"role": "assistant", "model": "claude-opus-5",
		"usage": m{"input_tokens": 0, "output_tokens": 0, "cache_read_input_tokens": 0, "cache_creation_input_tokens": 1_000_000,
			"cache_creation": m{"ephemeral_1h_input_tokens": 600_000, "ephemeral_5m_input_tokens": 400_000}}}})
	facts, err := NewRecordFacts().Read(path, "claude")
	if err != nil || facts.Usage == nil {
		t.Fatalf("facts %+v, %v", facts, err)
	}
	if u := facts.Usage; u.CacheWrite1h != 600_000 || u.CacheWrite5m != 400_000 || u.Cost != 8.5 {
		t.Errorf("summary = %+v, want the 600k/400k split priced at 8.5", u)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
