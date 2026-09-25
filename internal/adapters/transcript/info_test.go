package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The expectations here are the Swift app's: `TranscriptPaths.slug`,
// `Transcript.title(ofTranscript:)`, `SessionInfo.recordFacts`,
// `Orchestrator.cost(of:)` and `Shells`.

func TestProjectSlugDashesEveryUTF16UnitThatIsNotAlphanumeric(t *testing.T) {
	cases := map[string]string{
		"/Users/a/code/clawdline-go": "-Users-a-code-clawdline-go",
		"/Users/a/my_app.v2":         "-Users-a-my-app-v2",
		"/x/é":                       "-x--",
		"/x/😀":                       "-x---",
	}
	for in, want := range cases {
		if got := ProjectSlug(in); got != want {
			t.Errorf("ProjectSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTitleIsTheLastRenameAndOtherwiseTheLastAITitle(t *testing.T) {
	path := writeRecord(t,
		m{"type": "ai-title", "aiTitle": "First"},
		m{"type": "custom-title", "customTitle": "Renamed"},
		m{"type": "ai-title", "aiTitle": "Later"},
	)
	title, custom := NewTitles().Read(path)
	if title != "Renamed" || custom != "Renamed" {
		t.Fatalf("got (%q, %q), want the rename", title, custom)
	}

	path = writeRecord(t, m{"type": "ai-title", "aiTitle": "First"}, m{"type": "ai-title", "aiTitle": "Later"})
	if title, custom := NewTitles().Read(path); title != "Later" || custom != "" {
		t.Fatalf("got (%q, %q), want the last aiTitle and no rename", title, custom)
	}
}

func TestARenameBeforeTheTailIsStillFound(t *testing.T) {
	filler := strings.Repeat("x", 64_000)
	rows := []any{m{"type": "custom-title", "customTitle": "Kept"}}
	for range 10 {
		rows = append(rows, claudeRow("user", filler))
	}
	rows = append(rows, m{"type": "ai-title", "aiTitle": "Recent"})
	path := writeRecord(t, rows...)
	if st, _ := os.Stat(path); st.Size() <= titleTail {
		t.Fatalf("fixture is %d bytes, not past the tail", st.Size())
	}
	if title, _ := NewTitles().Read(path); title != "Kept" {
		t.Fatalf("title = %q, want the early rename", title)
	}
}

func assistantTurn(model string, in, out, read, write int) m {
	return m{"type": "assistant", "message": m{"role": "assistant", "model": model, "content": []m{{"type": "text", "text": "ok"}},
		"usage": m{"input_tokens": in, "output_tokens": out, "cache_read_input_tokens": read, "cache_creation_input_tokens": write}}}
}

func TestClaudeFactsSumEveryTurnAndPriceThem(t *testing.T) {
	path := writeRecord(t,
		claudeRow("user", "go"),
		assistantTurn("claude-opus-5", 1000, 2000, 1_000_000, 100_000),
		assistantTurn("claude-opus-5", 1000, 0, 0, 0),
	)
	facts, err := NewRecordFacts().Read(path, "claude")
	if err != nil {
		t.Fatal(err)
	}
	u := facts.Usage
	if u == nil || u.Input != 2000 || u.Output != 2000 || u.CacheRead != 1_000_000 || u.CacheWrite != 100_000 || u.Total != 1_104_000 {
		t.Fatalf("usage = %+v", u)
	}
	// 2000×5 + 2000×25 + 1e6×0.5 + 1e5×10 = 1,560,000 per million: a write
	// the record does not split is a 1-hour one, at twice the input price.
	if !u.HasCost || u.Cost != 1.56 {
		t.Fatalf("cost = %v (%v), want 1.56", u.Cost, u.HasCost)
	}
	if facts.Model != "claude-opus-5" {
		t.Fatalf("model = %q", facts.Model)
	}
}

func TestAClaudeRecordPastTheReadLimitHasAModelAndNoTotal(t *testing.T) {
	facts := claudeFacts([]byte(`{"type":"assistant","message":{"model":"claude-sonnet-5","usage":{"input_tokens":1}}}`), false)
	if facts.Usage != nil {
		t.Fatalf("usage = %+v, want none for a partial read", facts.Usage)
	}
	if facts.Model != "claude-sonnet-5" {
		t.Fatalf("model = %q", facts.Model)
	}
}

func TestAModelSwitchAfterTheLastTurnIsTheModel(t *testing.T) {
	path := writeRecord(t,
		assistantTurn("claude-opus-5", 1, 1, 0, 0),
		claudeRow("user", "<command-name>/model</command-name>\n<command-args>sonnet</command-args>"),
		claudeRow("user", "<local-command-stdout>Set model to \x1b[1mSonnet 5\x1b[22m</local-command-stdout>"),
		assistantTurn("<synthetic>", 0, 0, 0, 0),
	)
	facts, err := NewRecordFacts().Read(path, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if facts.Model != "claude-sonnet-5" {
		t.Fatalf("model = %q, want the switch", facts.Model)
	}
	// The last counted turn is the refusal, which names no priced model.
	if facts.Usage == nil || facts.Usage.Model != "<synthetic>" || facts.Usage.HasCost {
		t.Fatalf("usage = %+v", facts.Usage)
	}
}

func TestCodexFactsReadTheRunningTotalAndHaveNoPrice(t *testing.T) {
	path := writeRecord(t,
		m{"type": "turn_context", "payload": m{"type": "turn_context", "model": "gpt-5.6-sol"}},
		m{"type": "event_msg", "payload": m{"type": "token_count", "info": m{"total_token_usage": m{
			"input_tokens": 100, "output_tokens": 20, "cached_input_tokens": 50, "total_tokens": 120}}}},
	)
	facts, err := NewRecordFacts().Read(path, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if facts.Model != "gpt-5.6-sol" || facts.Usage == nil || facts.Usage.Total != 120 || facts.Usage.HasCost {
		t.Fatalf("facts = %+v usage = %+v", facts, facts.Usage)
	}
}

func TestClaudeSessionCostIsTheStatusLinesOwnTotal(t *testing.T) {
	home := statusCache(t, "abc", `{"cost":{"total_cost_usd":208.37865}}`)
	if status := ReadClaudeStatusLine(home, "abc"); !status.HasCost || status.CostUsd != 208.37865 {
		t.Fatalf("cost = %v, %v", status.CostUsd, status.HasCost)
	}
	if status := ReadClaudeStatusLine(home, "missing"); status.HasCost {
		t.Fatal("a session with no cache has no cost")
	}
}

// A file is a running command only when its id was announced as a background
// command and nothing has written an ending under it.
func TestShellsNeedAnAnnouncementAndNoEnding(t *testing.T) {
	root := t.TempDir()
	transcript := filepath.Join(root, "projects", "-p", "sess.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, row := range []any{
		claudeRow("assistant", []m{{"type": "tool_use", "id": "call1", "name": "Bash",
			"input": m{"command": "go test ./...\n  -run X", "description": "Run tests", "run_in_background": true}}}),
		claudeRow("user", []m{{"type": "tool_result", "tool_use_id": "call1",
			"content": "Command running in background with ID: run1. Output is being written to: /tmp/x"}}),
		claudeRow("assistant", []m{{"type": "tool_use", "id": "call2", "name": "Bash",
			"input": m{"command": "make", "run_in_background": true}}}),
		claudeRow("user", []m{{"type": "tool_result", "tool_use_id": "call2",
			"content": "Command running in background with ID: done1"}}),
	} {
		line, _ := jsonLine(row)
		b.WriteString(line)
	}
	if err := os.WriteFile(transcript, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	s := NewShells()
	s.Root = filepath.Join(root, "tmp")
	tasks := s.Folder(transcript)
	if err := os.MkdirAll(tasks, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(tasks, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("run1.output", "compiling\nok  pkg 0.2s\n")
	write("done1.output", "built\n[exited with code 0]\n")
	write("interrupted.output", "partial\n") // a foreground command that was cancelled

	got := s.Running(transcript)
	if len(got) != 1 {
		t.Fatalf("got %d shells: %+v", len(got), got)
	}
	sh := got[0]
	if sh.ID != "run1" || sh.Command != "go test ./... -run X" || sh.What != "Run tests" || sh.Doing != "ok  pkg 0.2s" {
		t.Fatalf("shell = %+v", sh)
	}

	// An ending arrives: the command is over.
	later := time.Now().Add(time.Second)
	write("run1.output", "compiling\nok  pkg 0.2s\n[exited with code 0]\n")
	_ = os.Chtimes(filepath.Join(tasks, "run1.output"), later, later)
	if got := s.Running(transcript); len(got) != 0 {
		t.Fatalf("finished command still reported: %+v", got)
	}
}

func jsonLine(v any) (string, bool) {
	line, ok := encodeJSON(v)
	return line + "\n", ok
}
