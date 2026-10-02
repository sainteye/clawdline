package transcript

import "testing"

// A Codex exec wrapper carries the actual tool name and arguments inside its
// JavaScript input. The paired output belongs to the same category by call ID.
func TestCodexExecAttributesNestedBoardAndProtocolCalls(t *testing.T) {
	path := writeRecord(t,
		m{"type": "turn_context", "payload": m{"type": "turn_context", "model": "gpt-5.6-sol"}},
		m{"type": "response_item", "payload": m{"type": "custom_tool_call", "name": "exec", "call_id": "board-1",
			"input": `const r = await tools.exec_command({cmd:"clawdline item show 4"}); text(r.output);`}},
		m{"type": "response_item", "payload": m{"type": "custom_tool_call", "name": "exec", "call_id": "protocol-1",
			"input": `const r = await tools.exec_command({cmd:"clawdline task accept --port 7727 /example/task"}); text(r.output);`}},
		m{"type": "event_msg", "payload": m{"type": "token_count", "info": m{
			"last_token_usage":  m{"input_tokens": 1000, "cached_input_tokens": 0, "output_tokens": 200, "total_tokens": 1200},
			"total_token_usage": m{"total_tokens": 1200},
		}}},
		m{"type": "response_item", "payload": m{"type": "custom_tool_call_output", "call_id": "board-1",
			"output": `{"output":"Board item 4 has three synthetic steps with synthetic descriptions."}`}},
		m{"type": "response_item", "payload": m{"type": "custom_tool_call_output", "call_id": "protocol-1",
			"output": `{"output":"Signed: the broker has the synthetic receipt for this task."}`}},
		m{"type": "event_msg", "payload": m{"type": "token_count", "info": m{
			"last_token_usage":  m{"input_tokens": 1000, "cached_input_tokens": 0, "output_tokens": 100, "total_tokens": 1100},
			"total_token_usage": m{"total_tokens": 2300},
		}}},
	)
	s := feedAll(t, path)
	spent, measured := s.Totals()
	for _, category := range []Category{CategoryBoard, CategoryProtocol} {
		if spent[category].Output <= 0 {
			t.Errorf("%s = %+v; want the nested call's output", category, spent[category])
		}
	}
	if other := spent[CategoryOther].Total(); other >= measured.Total()*0.20 {
		t.Errorf("other = %v of measured %v; want less than 20%%", other, measured.Total())
	}
}
