package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The expectations here are the Swift app's, from Sources/Transcript.swift and
// Sources/Codex.swift: the console that draws these rows is a copy of that
// app's, and reads their meaning, not just their shape.

func writeRecord(t *testing.T, rows ...any) string {
	t.Helper()
	var b strings.Builder
	for _, r := range rows {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	path := filepath.Join(t.TempDir(), "record.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

type m = map[string]any

func claudeRow(typ string, content any, extra ...m) m {
	row := m{"type": typ, "timestamp": "2026-09-16T10:00:00.250Z", "message": m{"role": typ, "content": content}}
	for _, e := range extra {
		for k, v := range e {
			row[k] = v
		}
	}
	return row
}

func TestClaudeToolRowsCarryTheirSubject(t *testing.T) {
	path := writeRecord(t,
		claudeRow("user", "Look at the build"),
		claudeRow("assistant", []m{
			{"type": "text", "text": "Checking."},
			{"type": "tool_use", "name": "Bash", "input": m{"command": "go build ./...\ngo vet ./...", "description": "Build"}},
			{"type": "tool_use", "name": "Read", "input": m{"file_path": "/repo/main.go"}},
			{"type": "tool_use", "name": "Write", "input": m{"file_path": "/repo/new.go", "content": "package main\n"}},
		}),
		claudeRow("user", []m{
			{"type": "tool_result", "content": "\x1b[31mFAIL\x1b[0m  ./cmd\nmore"},
			{"type": "tool_result", "content": []m{{"type": "text", "text": "line one"}, {"type": "text", "text": "two"}}},
			{"type": "tool_result", "content": "///"},
		}),
		claudeRow("user", []m{{"type": "tool_result", "content": "File created"}},
			m{"toolUseResult": m{"type": "create", "filePath": "/repo/new.go", "content": "package main\n"}}),
		claudeRow("assistant", []m{{"type": "thinking", "thinking": "", "signature": ""}}),
		m{"type": "assistant", "isSidechain": true, "message": m{"content": "an agent talking"}},
	)
	page, err := ReadClaude(path, 50)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, e := range page.Entries {
		got = append(got, e.Kind+"|"+e.Tool+"|"+e.Text)
	}
	want := []string{
		"user||Look at the build",
		"assistant||Checking.",
		"tool|Bash|go build ./...",
		"tool|Read|/repo/main.go",
		"tool|Write|/repo/new.go",
		"toolResult||FAIL  ./cmd",
		"toolResult||line one two",
		"toolResult||///",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("entries\n got: %q\nwant: %q", got, want)
	}
	if fc := page.Entries[4].FileChanges; len(fc) != 1 || fc[0].Kind != "write" || *fc[0].Content != "package main\n" {
		t.Fatalf("Write carries the whole file: %+v", fc)
	}
	if page.Entries[0].At != 1789552800 {
		t.Fatalf("at: %d", page.Entries[0].At)
	}
}

func TestDedicatedClaudeAgentKeepsSidechainTurns(t *testing.T) {
	path := writeRecord(t,
		claudeRow("assistant", []m{{"type": "text", "text": "background answer"}}, m{"isSidechain": true}),
	)
	parent, err := ReadClaude(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(parent.Entries) != 0 {
		t.Fatalf("parent read kept %d sidechain entries", len(parent.Entries))
	}
	agent, err := ReadClaudeAgent(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(agent.Entries) != 1 || agent.Entries[0].Text != "background answer" {
		t.Fatalf("agent entries %+v", agent.Entries)
	}
}

func TestClaudeQuestionCarriesItsOptions(t *testing.T) {
	path := writeRecord(t, claudeRow("assistant", []m{{"type": "tool_use", "name": AskTool, "input": m{
		"questions": []m{{"question": " Which one? ", "header": "Pick", "multiSelect": false,
			"options": []m{{"label": "A <b>", "description": "first"}, {"label": ""}, {"label": "B"}}}},
	}}}))
	page, err := ReadClaude(path, 10)
	if err != nil || len(page.Entries) != 1 {
		t.Fatalf("%v %+v", err, page.Entries)
	}
	want := AskMarker + `[{"h":"Pick","o":[{"d":"first","l":"A <b>"},{"l":"B"}],"q":"Which one?"}]`
	if page.Entries[0].Text != want {
		t.Fatalf("ask payload\n got: %q\nwant: %q", page.Entries[0].Text, want)
	}
}

func TestClaudeSlashCommandAndQueuedInput(t *testing.T) {
	path := writeRecord(t,
		claudeRow("user", "<command-name>/model</command-name><command-args>fable</command-args>"),
		claudeRow("user", "<local-command-stdout>Set model to \x1b[1mFable\x1b[22m</local-command-stdout>"),
		m{"type": "queue-operation", "operation": "enqueue", "timestamp": "2026-09-16T10:00:01.000Z",
			"content": "also this <system-reminder>noise</system-reminder> [Image #1]"},
		m{"type": "system", "content": "<command-name>/clear</command-name>", "timestamp": "2026-09-16T10:00:02.000Z"},
		claudeRow("user", "<system-reminder>only machinery</system-reminder>"),
	)
	page, _ := ReadClaude(path, 10)
	got := []string{}
	for _, e := range page.Entries {
		got = append(got, e.Kind+"|"+e.Text)
	}
	want := "user|/model fable\ntoolResult|Set model to Fable\nuser|also this\nuser|/clear"
	if strings.Join(got, "\n") != want {
		t.Fatalf("got %q", got)
	}
	if page.Entries[2].ImageCount != 1 {
		t.Fatalf("a queued marker counts as an image: %d", page.Entries[2].ImageCount)
	}
}

func TestClaudeSubagentHandBackKeepsItsSpeakerAndReport(t *testing.T) {
	raw := `<agent-message from="agent-fixture">
[Subagent hand-back]
This framing explains how the report is delivered.
Its wording is not part of the report.

    ## Finding

    The report stays visible.
      Its own indentation stays relative.
</agent-message>`
	path := writeRecord(t, claudeRow("user", raw))
	page, err := ReadClaude(path, 10)
	if err != nil || len(page.Entries) != 1 {
		t.Fatalf("%v %+v", err, page.Entries)
	}
	got := page.Entries[0]
	if got.Kind != KindAgent || got.Source != "agent-fixture" {
		t.Fatalf("speaker: %+v", got)
	}
	want := "## Finding\n\nThe report stays visible.\n  Its own indentation stays relative."
	if got.Text != want {
		t.Fatalf("report\n got: %q\nwant: %q", got.Text, want)
	}
}

func TestClaudeSubagentEnvelopeFallsBackWithoutDeletingWords(t *testing.T) {
	unframed := `<agent-message from="agent-fallback">
[Subagent hand-back]
Harness wording.

Report without the promised indentation.
</agent-message>`
	unknown := `<future-agent-message from="agent-future">leave every byte</future-agent-message>`
	path := writeRecord(t,
		m{"type": "queue-operation", "operation": "enqueue", "content": unframed},
		claudeRow("user", unknown),
	)
	page, err := ReadClaude(path, 10)
	if err != nil || len(page.Entries) != 2 {
		t.Fatalf("%v %+v", err, page.Entries)
	}
	if got := page.Entries[0]; got.Kind != KindAgent || got.Source != "agent-fallback" || got.Text != strings.Trim(unframed[len(`<agent-message from="agent-fallback">`):len(unframed)-len(`</agent-message>`)], "\r\n") {
		t.Fatalf("known envelope fallback: %+v", got)
	}
	if got := page.Entries[1]; got.Kind != KindUser || got.Text != unknown {
		t.Fatalf("unknown envelope changed: %+v", got)
	}
}

func codexItem(item m) m {
	return m{"timestamp": "2026-09-16T00:36:44.416Z", "type": "event_msg",
		"payload": m{"type": "item_completed", "item": item}}
}

func TestCodexItems(t *testing.T) {
	path := writeRecord(t,
		m{"type": "response_item", "payload": m{"type": "reasoning"}},
		codexItem(m{"type": "UserMessage", "content": []m{{"type": "text", "text": "hello"}}}),
		codexItem(m{"type": "CommandExecution", "command": []string{"/bin/zsh", "-lc", "sed -n '1,5p' a.go"},
			"parsed_cmd": []m{{"type": "read", "cmd": "sed -n '1,5p' a.go", "name": "a.go", "path": "a.go"}},
			"status":     "completed", "duration": m{"secs": 0, "nanos": 2833}}),
		codexItem(m{"type": "CommandExecution", "command": []string{"/bin/zsh", "-lc", "make test"},
			"parsed_cmd":        []m{{"type": "unknown", "cmd": "make test"}},
			"aggregated_output": "", "exit_code": 2}),
		codexItem(m{"type": "FileChange", "changes": m{
			"/r/b.go": m{"type": "update", "unified_diff": "@@ -1 +1 @@\n-a\n+b\n"},
			"/r/a.go": m{"type": "add", "content": "x"},
		}}),
		codexItem(m{"type": "McpToolCall", "server": "browser", "tool": "connect",
			"arguments": m{"title": "Connect\nmore"}, "status": "completed",
			"result": m{"isError": true, "content": []m{{"type": "text", "text": "refused"}}}}),
		codexItem(m{"type": "AgentMessage", "content": []m{{"type": "Text", "text": " done "}}}),
		codexItem(m{"type": "Reasoning"}),
	)
	page, err := ReadCodex(path, 50)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, e := range page.Entries {
		got = append(got, e.Kind+"|"+e.Tool+"|"+e.Text)
	}
	want := []string{
		"user||hello",
		"tool|shell|Read a.go",
		"tool|shell|make test",
		"toolResult||exit 2",
		"tool|edit|a.go, b.go",
		"tool|browser.connect|Connect",
		"assistant||done",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("entries\n got: %q\nwant: %q", got, want)
	}
	explored := page.Entries[1].Activity
	if explored == nil || explored.Kind != "explored" || *explored.Status != "completed" ||
		explored.DurationMs == nil || *explored.DurationMs != 0 || len(explored.Actions) != 1 {
		t.Fatalf("explored activity: %+v", explored)
	}
	if fc := page.Entries[4].FileChanges; len(fc) != 2 || fc[0].Path != "/r/a.go" || fc[1].UnifiedDiff == nil {
		t.Fatalf("file changes sorted by path: %+v", fc)
	}
	called := page.Entries[5].Activity
	if called == nil || called.Kind != "called" || *called.Status != "failed" || *called.Result != "refused" ||
		called.DurationMs != nil {
		t.Fatalf("called activity: %+v", called)
	}
}

func TestCodexPlanIsReadAsLiterals(t *testing.T) {
	input := `const p = [{step:"Inspect <unsafe>",status:"completed"},{step:"Implement cards",status:"in_progress"},{step:"Verify",status:"pending"}]; const r = await tools.update_plan({explanation:"Now",plan:p}); text(r)`
	row := func(input string) m {
		return m{"timestamp": "2026-08-30T15:31:02.125Z", "type": "response_item",
			"payload": m{"type": "custom_tool_call", "name": "exec", "input": input}}
	}
	page, _ := ReadCodex(writeRecord(t,
		row(input),
		row(strings.Replace(input, "plan:p", "plan:makePlan()", 1)),
		row(`await tools.update_plan({plan:[{step:"a]",status:"pending"}]})`),
	), 10)
	if len(page.Entries) != 2 {
		t.Fatalf("a computed plan is refused, literal ones kept: %+v", page.Entries)
	}
	plan := page.Entries[0].Plan
	if page.Entries[0].Tool != "plan" || page.Entries[0].Text != "Updated Plan" || len(plan) != 3 ||
		plan[0].Step != "Inspect <unsafe>" || plan[1].Status != "inProgress" {
		t.Fatalf("plan: %+v", page.Entries[0])
	}
	if p := page.Entries[1].Plan; len(p) != 1 || p[0].Step != "a]" {
		t.Fatalf("a bracket inside a string is text: %+v", p)
	}
}

func TestNoticeAndSessionMessage(t *testing.T) {
	notice := `<clawdline-notice>{"audience":"parent","body":"Task done","child_may_still_write":false,"claims_released":true,"kind":"task_finished","outstanding":0,"protocol":"clawdline.notice","result_path":"/tmp/r.json","state":"success","task":{"id":"t1","title":"Do it"},"version":1}</clawdline-notice>`
	message := `<clawdline-message>{"body":"hi","kind":"session_message","protocol":"clawdline.message","source":{"assistant":"codex","id":"%1","label":"Root"},"version":1}</clawdline-message>`
	path := writeRecord(t,
		claudeRow("user", notice),
		claudeRow("user", message),
		claudeRow("user", strings.Replace(notice, `"outstanding":0,`, ``, 1)),
	)
	page, _ := ReadClaude(path, 10)
	if len(page.Entries) != 3 {
		t.Fatalf("%+v", page.Entries)
	}
	n := page.Entries[0]
	if n.Kind != KindNotice || n.Text != "Task done" || n.Notice.Task.Title != "Do it" || n.Notice.Audience != "parent" ||
		!n.Notice.ClaimsReleased {
		t.Fatalf("notice: %+v %+v", n, n.Notice)
	}
	msg := page.Entries[1]
	if msg.Kind != KindMessage || msg.Text != "hi" || msg.Source != "Root" || msg.SourceMode != "clawdline" ||
		msg.SourceAssistant != "codex" {
		t.Fatalf("message: %+v", msg)
	}
	if page.Entries[2].Kind != KindUser {
		t.Fatalf("a notice missing a required key is somebody's text: %+v", page.Entries[2])
	}
}

func TestTailWindowDropsItsCutLine(t *testing.T) {
	// A first row longer than the whole window: whatever of it the window
	// holds is a fragment, and must not be read as a turn.
	big := claudeRow("user", strings.Repeat("x", ReadBudget))
	path := writeRecord(t, big, claudeRow("user", "second"), claudeRow("user", "third"))
	page, _ := ReadClaude(path, 10)
	if len(page.Entries) != 2 || page.Entries[0].Text != "second" {
		t.Fatalf("%d entries", len(page.Entries))
	}
	page, _ = ReadClaude(path, 1)
	if len(page.Entries) != 1 || page.Entries[0].Text != "third" {
		t.Fatalf("limit keeps the newest: %+v", page.Entries)
	}
}

// limits N17: the read looks at only the record's last ReadBudget bytes. When
// that window runs out before the page is full, the page says how much was
// never read; the Swift app cuts the same window and says nothing, so its page
// reads as if the conversation began at its first entry.
func TestAReadWindowThatRunsOutSaysHowMuchWasNotRead(t *testing.T) {
	var b strings.Builder
	write := func(row m) {
		line, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	write(claudeRow("user", "the first thing anybody said"))
	// Rows that yield no entry, past the window: what a long tool-heavy
	// conversation's middle is to this reader.
	filler := m{"type": "progress", "data": strings.Repeat("x", 4000)}
	for b.Len() < ReadBudget+(1<<20) {
		write(filler)
	}
	write(claudeRow("user", "the newest question"))
	write(claudeRow("assistant", []m{{"type": "text", "text": "the newest answer"}}))
	path := filepath.Join(t.TempDir(), "long.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	size := int64(b.Len())

	page, err := ReadClaude(path, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("read %d entries, want the two inside the window", len(page.Entries))
	}
	if page.Unread < size-ReadBudget || page.Unread > size {
		t.Fatalf("unread %d of %d bytes; want at least the %d before the window", page.Unread, size, size-ReadBudget)
	}

	// Asked for no more than the window holds: the page is what was asked
	// for, and nothing is owed.
	if page, _ := ReadClaude(path, 2); page.Unread != 0 || len(page.Entries) != 2 {
		t.Fatalf("a full page reads unread=%d entries=%d", page.Unread, len(page.Entries))
	}
	// A record the window covers whole has nothing unread.
	short := writeRecord(t, claudeRow("user", "hello"))
	if page, _ := ReadClaude(short, 200); page.Unread != 0 || len(page.Entries) != 1 {
		t.Fatalf("a short record reads unread=%d entries=%d", page.Unread, len(page.Entries))
	}
}

// A message Clawdline relays is typed in as a bracketed paste, so Claude Code
// records it wearing a pair of paste marks. The marks are Claude Code's; the
// words between them are the person's, and only the marks come off.
func TestPasteMarksComeOffAndTheirContentStays(t *testing.T) {
	path := writeRecord(t,
		claudeRow("user", `<pasted_content id="a6cc">另外 T3 這個 Session 看不到</pasted_content id="a6cc">`),
		m{"type": "queue-operation", "operation": "enqueue", "timestamp": "2026-09-16T10:00:01.000Z",
			"content": `<pasted_content id="b1">排隊中的那一句</pasted_content id="b1">`},
		claudeRow("user", `前面的話</pasted_content id="c2">`),
		// The negative control: the word, and no tag. Nothing here is a mark,
		// so nothing here may be taken off.
		claudeRow("user", "the wrapper is called pasted_content and I want to keep saying so"),
	)
	page, _ := ReadClaude(path, 10)
	got := []string{}
	for _, e := range page.Entries {
		got = append(got, e.Kind+"|"+e.Text)
	}
	want := strings.Join([]string{
		"user|另外 T3 這個 Session 看不到",
		"user|排隊中的那一句",
		"user|前面的話",
		"user|the wrapper is called pasted_content and I want to keep saying so",
	}, "\n")
	if strings.Join(got, "\n") != want {
		t.Fatalf("got %q", got)
	}
}

func TestClaudeBashModeReadsAsTheLineThatWasTyped(t *testing.T) {
	path := writeRecord(t,
		claudeRow("user", "<bash-input> ls -la</bash-input>"),
		claudeRow("user", "<bash-stdout>total 0\n\x1b[1mREADME.md\x1b[0m -&gt; docs</bash-stdout><bash-stderr>ls: cannot access &#39;x&#39;</bash-stderr>"),
		claudeRow("user", []m{{"type": "text", "text": "<bash-input>git status</bash-input>"}}),
		claudeRow("user", "<bash-stdout></bash-stdout><bash-stderr>\n</bash-stderr>"),
	)
	page, err := ReadClaude(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, e := range page.Entries {
		got = append(got, e.Kind+"|"+e.Text)
		if strings.Contains(e.Text, "<bash-") {
			t.Fatalf("raw markup reached an entry: %q", e.Text)
		}
	}
	want := "user|! ls -la\ntoolResult|total 0\nREADME.md -> docs\nls: cannot access 'x'\nuser|!git status"
	if strings.Join(got, "\n") != want {
		t.Fatalf("got %q", got)
	}
}

func TestFirstUserOfABashModeSessionIsTheTypedLine(t *testing.T) {
	path := writeRecord(t,
		claudeRow("user", "<bash-input>git status</bash-input>"),
		claudeRow("user", "<bash-stdout>clean</bash-stdout><bash-stderr></bash-stderr>"),
	)
	got, err := FirstUser(path, "claude")
	if err != nil || got != "!git status" {
		t.Fatalf("first user = %q, %v", got, err)
	}
}
