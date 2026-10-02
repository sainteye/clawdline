package transcript

import (
	"encoding/json"
	"testing"
)

// Every line here is written by hand; none is copied from a real transcript.

func TestClassifyKnowsAWaitFromWork(t *testing.T) {
	cases := []struct {
		name, tool, input string
		want              Category
	}{
		{"a background shell's output", "BashOutput", `{"bash_id":"b1"}`, CategoryWait},
		{"a background task's output", "TaskOutput", `{"task_id":"t1"}`, CategoryWait},
		{"a monitor", "Monitor", `{"until":"done"}`, CategoryWait},
		{"a bare sleep", "Bash", `{"command":"sleep 30"}`, CategoryWait},
		{"a sleep and a look at the log", "Bash", `{"command":"sleep 20; tail -5 /example/build.log"}`, CategoryWait},
		{"a sleep in a directory, quietly", "Bash", `{"command":"cd /example/repo && sleep 5 2>/dev/null && cat out.txt"}`, CategoryWait},
		{"a sleep after a build is the build", "Bash", `{"command":"go build ./... && sleep 1"}`, CategoryImpl},
		{"a sleep and a curl is a curl", "Bash", `{"command":"sleep 5; curl -s https://example.com/health"}`, CategoryImpl},
		{"a sleep that writes a file", "Bash", `{"command":"sleep 1; echo done > /example/flag"}`, CategoryImpl},
		{"a sleep before a protocol read is protocol", "Bash", `{"command":"sleep 2; cat /example/tasks/t/result.json"}`, CategoryProtocol},
		{"a sleep before a clawdline command is protocol", "Bash", `{"command":"sleep 30; clawdline task show t"}`, CategoryProtocol},
		{"a polling loop is not caught", "Bash", `{"command":"until grep -q done /example/log; do sleep 5; done"}`, CategoryImpl},
		{"echo alone is not a wait", "Bash", `{"command":"echo hello"}`, CategoryImpl},
		{"Codex sleep", "sleep", `{"duration_ms":30000}`, CategoryWait},
		{"Codex wait on a cell", "wait", `{"cell_id":"7","yield_time_ms":30000}`, CategoryWait},
		{"Codex wait on a subagent", "wait_agent", `{"timeout_ms":60000}`, CategoryWait},
		{"Codex exec_command sleep", "exec_command", `{"cmd":"sleep 30"}`, CategoryWait},
		{"Codex empty write_stdin", "write_stdin", `{"session_id":4,"chars":"","yield_time_ms":30000}`, CategoryWait},
		{"Codex write_stdin with no chars", "write_stdin", `{"session_id":4}`, CategoryWait},
		{"Codex write_stdin that types", "write_stdin", `{"session_id":4,"chars":"y\n"}`, CategoryOther},
	}
	for _, tc := range cases {
		if got := Classify(tc.tool, json.RawMessage(tc.input)); got != tc.want {
			t.Errorf("%s: Classify(%s, %s) = %s, want %s", tc.name, tc.tool, tc.input, got, tc.want)
		}
	}
}

func TestCodexExecIsWaitOnlyWhenEveryToolWaits(t *testing.T) {
	cases := []struct {
		name, script string
		want         Category
	}{
		{"a poll", `const r=await tools.write_stdin({session_id:12,chars:"",yield_time_ms:30000,max_output_tokens:2000});text(r);`, CategoryWait},
		{"a poll without chars", `text(await tools.write_stdin({session_id:12, yield_time_ms:30000}))`, CategoryWait},
		{"two polls", `await tools.write_stdin({session_id:1,chars:""}); await tools.write_stdin({session_id:2,chars:''});`, CategoryWait},
		{"a sleep", `const r = await tools.exec_command({cmd:"sleep 30"}); text(r.output);`, CategoryWait},
		{"a poll and a test run", `await tools.write_stdin({session_id:1,chars:""}); await tools.exec_command({cmd:"go test ./..."});`, CategoryImpl},
		{"a sleep and a patch", `await tools.exec_command({cmd:"sleep 1"}); await tools.apply_patch("*** Begin Patch");`, CategoryImpl},
		{"an interrupt", `await tools.write_stdin({session_id:1,chars:"\u0003",yield_time_ms:1000});`, CategoryOther},
		{"a poll and a board read", `await tools.write_stdin({session_id:1,chars:""}); await tools.exec_command({cmd:"clawdline item show 4"});`, CategoryBoard},
	}
	for _, tc := range cases {
		if got := classifyCodexExec(tc.script); got != tc.want {
			t.Errorf("%s: classifyCodexExec = %s, want %s", tc.name, got, tc.want)
		}
	}
}


func claudeCall(id string, read, write, output int, tools ...m) m {
	content := []any{}
	for _, t := range tools {
		content = append(content, t)
	}
	return m{"type": "assistant", "message": m{"id": id, "model": "claude-opus-5-5", "content": content,
		"usage": m{"input_tokens": 1, "cache_read_input_tokens": read, "cache_creation_input_tokens": write, "output_tokens": output}}}
}

func toolUse(id, name string, input m) m {
	return m{"type": "tool_use", "id": id, "name": name, "input": input}
}

func toolResult(id, text string) m {
	return m{"type": "user", "message": m{"content": []any{m{"type": "tool_result", "tool_use_id": id, "content": text}}}}
}

// A call that only waits pays its whole bill — the context it read again, its
// writes and its output — to wait, though the context it read was work. A call
// that waits and works is divided as before.
func TestAClaudeTurnThatOnlyWaitsIsWaitWhole(t *testing.T) {
	path := writeRecord(t,
		claudeCall("m1", 0, 100_000, 50, toolUse("t1", "Bash", m{"command": "go test ./...", "run_in_background": true})),
		toolResult("t1", "started in the background"),
		claudeCall("m2", 100_050, 20, 30, toolUse("t2", "BashOutput", m{"bash_id": "b1"})),
		toolResult("t2", "still running"),
		claudeCall("m3", 100_100, 20, 30, toolUse("t3", "Bash", m{"command": "sleep 30"})),
		toolResult("t3", ""),
		claudeCall("m4", 100_150, 20, 40, toolUse("t4", "BashOutput", m{"bash_id": "b1"}),
			toolUse("t5", "Read", m{"file_path": "/example/repo/x.go"})),
		toolResult("t4", "ok"),
		toolResult("t5", "package x"),
	)
	s := feedAll(t, path)
	spent, measured := s.Totals()
	wait := spent[CategoryWait]
	// m2 and m3 whole; of m4's reread only the share the waits' own small
	// results and outputs hold in the context.
	if want := 100_050.0 + 100_100; wait.CacheRead < want || wait.CacheRead > want+500 {
		t.Errorf("wait cache read = %v, want %v and the waits' small share of the mixed turn", wait.CacheRead, want)
	}
	if want := 30.0 + 30 + 20; !near(wait.Output, want) {
		t.Errorf("wait output = %v, want %v (two waiting turns and half the mixed one)", wait.Output, want)
	}
	if sum := sumSpent(spent); !near(sum.Total(), measured.Total()) {
		t.Errorf("categories sum to %v, measured %v", sum.Total(), measured.Total())
	}
}

// Claude Code writes one call over several rows: a waiting turn whose tool use
// comes in a later row than its usage is still a waiting turn, and a state
// kept as JSON between the rows reads on to the same totals.
func TestAWaitDecidedOnALaterRowSurvivesAStoredState(t *testing.T) {
	rows := []any{
		claudeCall("m1", 0, 50_000, 10, toolUse("t1", "Bash", m{"command": "go build ./..."})),
		toolResult("t1", "ok"),
		claudeCall("m2", 50_010, 10, 5),
		claudeCall("m2", 50_010, 10, 25, toolUse("t2", "TaskOutput", m{"task_id": "x"})),
		toolResult("t2", "still running"),
		claudeCall("m3", 50_050, 10, 10, toolUse("t3", "Edit", m{"file_path": "/example/x.go"})),
	}
	whole := feedAll(t, writeRecord(t, rows...))
	wantSpent, wantMeasured := whole.Totals()
	if got := wantSpent[CategoryWait].CacheRead; !near(got, 50_010) {
		t.Fatalf("wait cache read = %v, want the waiting turn's 50010", got)
	}

	first := writeRecord(t, rows[:3]...)
	s := feedAll(t, first)
	if s.Open == nil || len(s.Open.Input) == 0 {
		t.Fatalf("the open call's input is not held back: %+v", s.Open)
	}
	if got := s.Spent[CategoryWait]; got.Total() != 0 {
		t.Fatalf("wait charged before the call's actions were known: %+v", got)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var back LedgerState
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	full := writeRecord(t, rows...)
	back.HeadLen, back.HeadSum = 0, "" // another temporary file, the same bytes at its start
	for {
		res, err := back.Feed(full)
		if err != nil {
			t.Fatal(err)
		}
		if res.Restarted {
			t.Fatal("the second half restarted")
		}
		if !res.More {
			break
		}
	}
	gotSpent, gotMeasured := back.Totals()
	if !near(gotMeasured.Total(), wantMeasured.Total()) {
		t.Fatalf("measured %v, want %v", gotMeasured.Total(), wantMeasured.Total())
	}
	for _, c := range Categories {
		if !near(gotSpent[c].Total(), wantSpent[c].Total()) {
			t.Errorf("%s = %v after a stored state, %v in one feed", c, gotSpent[c].Total(), wantSpent[c].Total())
		}
	}
}

// A state stored before calls held their input back has its open call's
// input in Spent already: reading on must not count it again.
func TestAnOpenCallFromAnOlderStateIsNotCountedTwice(t *testing.T) {
	s := LedgerState{
		Calls:    1,
		Measured: Tokens{CacheRead: 1000, Input: 10},
		Spent:    map[Category]Tokens{CategoryImpl: {CacheRead: 1000, Input: 10}},
		Open:     &ledgerCall{ID: "m1", Output: 20, Actions: Sizes{CategoryWait: 1}},
	}
	spent, measured := s.Totals()
	if sum := sumSpent(spent); !near(sum.Total(), measured.Total()) {
		t.Fatalf("categories sum to %v, measured %v", sum.Total(), measured.Total())
	}
	s.closeOpen()
	if sum := sumSpent(s.Spent); !near(sum.Total(), s.Measured.Total()) {
		t.Fatalf("after closing: categories sum to %v, measured %v", sum.Total(), s.Measured.Total())
	}
}

// A Codex turn that only polls a running command is wait, cached input and
// all; the turn that started the command stays impl.
func TestACodexPollingTurnIsWaitWhole(t *testing.T) {
	poll := `const r=await tools.write_stdin({session_id:7,chars:"",yield_time_ms:30000});text(r);`
	tokens := func(input, cached, output, total int) m {
		return m{"type": "event_msg", "payload": m{"type": "token_count", "info": m{
			"last_token_usage":  m{"input_tokens": input, "cached_input_tokens": cached, "output_tokens": output},
			"total_token_usage": m{"total_tokens": total},
		}}}
	}
	exec := func(id, script string) m {
		return m{"type": "response_item", "payload": m{"type": "custom_tool_call", "name": "exec", "call_id": id, "input": script}}
	}
	output := func(id, text string) m {
		return m{"type": "response_item", "payload": m{"type": "custom_tool_call_output", "call_id": id, "output": text}}
	}
	path := writeRecord(t,
		m{"type": "turn_context", "payload": m{"type": "turn_context", "model": "gpt-example"}},
		exec("c1", `await tools.exec_command({cmd:"go test ./..."});`),
		tokens(140_000, 0, 50, 140_050),
		output("c1", "Process running with session ID 7"),
		exec("c2", poll),
		tokens(140_100, 140_000, 20, 280_170),
		output("c2", "still running"),
		exec("c3", poll),
		tokens(140_150, 140_100, 20, 420_340),
		output("c3", "ok"),
		exec("c4", `await tools.apply_patch("*** Begin Patch");`),
		tokens(140_200, 140_150, 30, 560_570),
	)
	s := feedAll(t, path)
	spent, measured := s.Totals()
	if want, got := 140_000.0+140_100, spent[CategoryWait].CacheRead; got < want || got > want+500 {
		t.Errorf("wait cache read = %v, want %v and the polls' small share of the patch turn", got, want)
	}
	if want := 40.0; !near(spent[CategoryWait].Output, want) {
		t.Errorf("wait output = %v, want %v", spent[CategoryWait].Output, want)
	}
	if spent[CategoryImpl].Output < 80 {
		t.Errorf("impl output = %v; the turns that started and patched are work", spent[CategoryImpl].Output)
	}
	if sum := sumSpent(spent); !near(sum.Total(), measured.Total()) {
		t.Errorf("categories sum to %v, measured %v", sum.Total(), measured.Total())
	}
}
