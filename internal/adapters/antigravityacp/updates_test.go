package antigravityacp

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

const updateTestSession = "11111111-1111-4111-8111-111111111111"

func updateTestFrame(update string) []byte {
	return []byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"` + updateTestSession + `","update":` + update + `}}`)
}

func updateTestDecode(t *testing.T, update string) SessionUpdate {
	t.Helper()
	out, err := DecodeSessionUpdate(updateTestFrame(update), updateTestSession)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSessionUpdateNative141(t *testing.T) {
	// These two complete frames are copied byte-for-byte from the sanitized
	// native 1.4.1 pipe capture, not synthesized from an assumed schema. The
	// capture proves text/context decoding, not native tool compatibility.
	fixture, err := os.ReadFile("testdata/first-prompt-1.4.1.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	wantWire := string(updateTestFrame(`{"content":{"text":"CLAWDLINE_ACP_OK","type":"text"},"sessionUpdate":"agent_message_chunk"}`)) + "\n" +
		string(updateTestFrame(`{"used":13655,"size":1048576,"sessionUpdate":"usage_update"}`)) + "\n"
	if string(fixture) != wantWire {
		t.Fatal("native fixture bytes changed")
	}
	var chat strings.Builder
	var context *ContextUsage
	for _, frame := range bytes.Split(bytes.TrimSpace(fixture), []byte("\n")) {
		out, err := DecodeSessionUpdate(frame, updateTestSession)
		if err != nil {
			t.Fatal(err)
		}
		switch out.Kind {
		case "agent_message_chunk":
			chat.WriteString(out.Text)
		case "usage_update":
			context = out.Context
		default:
			t.Fatalf("unexpected native update: %+v", out)
		}
	}
	if chat.String() != "CLAWDLINE_ACP_OK" || !reflect.DeepEqual(context, &ContextUsage{13655, 1048576}) {
		t.Fatalf("chat=%q context=%+v", chat.String(), context)
	}
}

func TestSessionUpdateCallerChunkOrder(t *testing.T) {
	// This is the caller seam: append chunks to separate message and thought
	// streams in arrival order, preserving whitespace and multiline Unicode.
	var chat, thought strings.Builder
	frames := []string{
		`{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"think\n"}}`,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"first\n"}}`,
		`{"sessionUpdate":"usage_update","used":10,"size":100,"cost":{"amount":999,"currency":"USD"}}`,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"第二行 😀"}}`,
		`{"sessionUpdate":"plan","entries":[{"content":"not chat"}]}`,
		`{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"next"}}`,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":""}}`,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"image","data":"AA==","mimeType":"image/png"}}`,
	}
	for _, frame := range frames {
		out := updateTestDecode(t, frame)
		if out.Ignored {
			continue
		}
		switch out.Kind {
		case "agent_message_chunk":
			chat.WriteString(out.Text)
		case "agent_thought_chunk":
			thought.WriteString(out.Text)
		}
	}
	if chat.String() != "first\n第二行 😀" || thought.String() != "think\nnext" {
		t.Fatalf("chat=%q thought=%q", chat.String(), thought.String())
	}
}

func TestSessionUpdateToolPartialSynthetic(t *testing.T) {
	// Synthetic ACP tool fixtures. No native tool capture was supplied.
	minimal := updateTestDecode(t, `{"sessionUpdate":"tool_call","toolCallId":"minimal","title":"Inspect"}`).Tool
	if minimal.Kind != nil || minimal.Status != nil || minimal.Content != nil {
		t.Fatal("initial tool call invented optional fields")
	}
	start := updateTestDecode(t, `{"sessionUpdate":"tool_call","toolCallId":"tool-1","title":"Run checks","kind":"execute","status":"pending","content":[{"type":"content","content":{"type":"text","text":"start\n"}}],"locations":[{"path":"/workspace/a.go","line":0}],"rawInput":{"secret":"opaque-input"},"_meta":{"secret":"opaque-meta"}}`).Tool
	progress := updateTestDecode(t, `{"sessionUpdate":"tool_call_update","toolCallId":"tool-1","status":"in_progress"}`).Tool
	if progress.ToolCallID != "tool-1" || progress.Title != nil || progress.Kind != nil || progress.Content != nil || progress.Locations != nil || progress.Status == nil || *progress.Status != "in_progress" {
		t.Fatalf("partial update lost presence: %+v", progress)
	}
	// A renderer merges fields only when present. A status-only update must
	// preserve the prior title, content and source locations.
	if progress.Status != nil {
		start.Status = progress.Status
	}
	if progress.Content != nil {
		start.Content = progress.Content
	}
	if *start.Title != "Run checks" || *start.Kind != "execute" || *start.Status != "in_progress" ||
		*(*start.Content)[0].Content.Text != "start\n" || *(*start.Locations)[0].Line != 0 {
		t.Fatalf("merge erased prior state: %+v", start)
	}
	empty := updateTestDecode(t, `{"sessionUpdate":"tool_call_update","toolCallId":"tool-1","title":"","content":[],"locations":[]}`).Tool
	if empty.Title == nil || *empty.Title != "" || empty.Content == nil || len(*empty.Content) != 0 || empty.Locations == nil || len(*empty.Locations) != 0 || empty.Status != nil {
		t.Fatalf("explicit replacement lost: %+v", empty)
	}
	null := updateTestDecode(t, `{"sessionUpdate":"tool_call_update","toolCallId":"tool-1","title":null,"content":null,"status":null}`).Tool
	if null.Title != nil || null.Content != nil || null.Status != nil {
		t.Fatal("nullable partial fields must remain unspecified")
	}
	encoded, err := json.Marshal(start)
	if err != nil || bytes.Contains(encoded, []byte("opaque")) || bytes.Contains(encoded, []byte("secret")) {
		t.Fatalf("opaque metadata retained: %s, %v", encoded, err)
	}
}

func TestSessionUpdateToolContentSynthetic(t *testing.T) {
	out := updateTestDecode(t, `{"sessionUpdate":"tool_call_update","toolCallId":"tool-2","status":"completed","rawOutput":{"secret":"opaque-output"},"content":[{"type":"diff","path":"/workspace/a.go","oldText":null,"newText":"new\n"},{"type":"terminal","terminalId":"term-1"},{"type":"content","content":{"type":"resource","resource":{"uri":"file:///workspace/a.go","text":"body","mimeType":"text/plain","_meta":{"secret":"opaque"}}}},{"type":"content","content":{"type":"resource_link","uri":"file:///workspace/b.go","name":"b.go","description":"source","mimeType":"text/plain"}},{"type":"content","content":{"type":"image","data":"AA==","mimeType":"image/png"}},{"type":"content","content":{"type":"audio","data":"AA==","mimeType":"audio/wav"}},{"type":"vendor_extension","secret":"opaque"},{"type":"content","content":{"type":"vendor_extension","secret":"opaque"}}]}`)
	c := *out.Tool.Content
	if len(c) != 8 || c[0].Type != "diff" || *c[0].Path != "/workspace/a.go" || c[0].OldText != nil || *c[0].NewText != "new\n" ||
		*c[1].TerminalID != "term-1" || *c[2].Content.Resource.Text != "body" ||
		*c[3].Content.URI != "file:///workspace/b.go" || *c[3].Content.Name != "b.go" ||
		*c[4].Content.Data != "AA==" || *c[5].Content.MimeType != "audio/wav" || !c[6].Ignored || !c[7].Content.Ignored || out.Text != "" {
		t.Fatalf("structured tool content lost: %+v", c)
	}
	if out.Tool.Output == nil || !strings.Contains(*out.Tool.Output, "opaque-output") {
		t.Fatal("native output was discarded")
	}
	// Output is the requested result; arbitrary input and extension metadata
	// still have no projection. Exclude output when checking that boundary.
	out.Tool.Output = nil
	encoded, _ := json.Marshal(out)
	if bytes.Contains(encoded, []byte("opaque")) || bytes.Contains(encoded, []byte("rawOutput")) {
		t.Fatal("opaque tool data was normalized")
	}
	failed := updateTestDecode(t, `{"sessionUpdate":"tool_call_update","toolCallId":"tool-2","status":"failed"}`)
	if *failed.Tool.Status != "failed" || failed.Tool.ToolCallID != out.Tool.ToolCallID {
		t.Fatal("failed state or stable tool ID lost")
	}
}

func TestSessionUpdateIgnoredVariants(t *testing.T) {
	for _, kind := range []string{"plan", "available_commands_update", "current_mode_update", "config_option_update", "session_info_update"} {
		t.Run(kind, func(t *testing.T) {
			frame := updateTestFrame(`{"sessionUpdate":"` + kind + `","_meta":{"text":"not chat"}}`)
			out, err := DecodeSessionUpdate(frame, updateTestSession)
			if err != nil || !out.Ignored || out.Kind != kind || out.SessionID != updateTestSession || out.Text != "" || out.Context != nil || out.Tool != nil {
				t.Fatalf("ignored update=%+v err=%v", out, err)
			}
			_, err = DecodeSessionUpdate(frame, "other-session")
			var typed *UpdateDecodeError
			if !errors.As(err, &typed) || typed.Code != "wrong_session" {
				t.Fatalf("ignored variant bypassed session fence: %v", err)
			}
		})
	}
}

func TestSessionUpdateUserHistory(t *testing.T) {
	out := updateTestDecode(t, `{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"original user turn"}}`)
	if out.Ignored || out.Kind != "user_message_chunk" || out.Text != "original user turn" {
		t.Fatalf("history text lost: %+v", out)
	}
	_, err := DecodeSessionUpdate(updateTestFrame(`{"sessionUpdate":"user_message_chunk","content":{"type":"text"}}`), updateTestSession)
	if err == nil {
		t.Fatal("invalid history content accepted")
	}
}

func TestSessionUpdateContextOccupancy(t *testing.T) {
	// Replacement snapshots may decrease, exceed the nominal size, or be zero.
	// They never accumulate as billed/input/output tokens, nor retain cost/meta.
	var current *ContextUsage
	for _, value := range []string{`{"used":9007199254740993,"size":9007199254740994}`, `{"used":20,"size":10}`, `{"used":0,"size":0}`} {
		out := updateTestDecode(t, `{"sessionUpdate":"usage_update",`+strings.TrimPrefix(value, "{")+``)
		current = out.Context
		if out.Text != "" || out.Tool != nil || out.Ignored {
			t.Fatalf("usage became chat: %+v", out)
		}
		if strings.Contains(value, "9007199254740993") && current.Used != 9007199254740993 {
			t.Fatal("usage precision lost")
		}
	}
	if *current != (ContextUsage{}) {
		t.Fatal("context snapshots accumulated")
	}
}

func TestSessionUpdateRejectsMalformed(t *testing.T) {
	valid := string(updateTestFrame(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"safe"}}`))
	cases := []struct{ name, frame, session, code string }{
		{"owner", valid, "", "invalid_owner"},
		{"wrong session", valid, "different", "wrong_session"},
		{"prefix session", valid, updateTestSession + "-suffix", "wrong_session"},
		{"truncated", valid[:len(valid)-1], updateTestSession, "invalid_message"},
		{"trailing", valid + `{}`, updateTestSession, "invalid_message"},
		{"invalid utf8", strings.Replace(valid, "safe", string([]byte{0xff}), 1), updateTestSession, "invalid_message"},
		{"unpaired surrogate", strings.Replace(valid, "safe", `\ud800`, 1), updateTestSession, "invalid_message"},
		{"duplicate key", strings.Replace(valid, `"text":"safe"`, `"text":"safe","te\u0078t":"other"`, 1), updateTestSession, "invalid_message"},
		{"wrong method", strings.Replace(valid, "session/update", "other", 1), updateTestSession, "invalid_message"},
		{"wrong version", strings.Replace(valid, "2.0", "1.0", 1), updateTestSession, "invalid_message"},
		{"request", strings.Replace(valid, `"jsonrpc"`, `"id":1,"jsonrpc"`, 1), updateTestSession, "invalid_message"},
		{"response", strings.Replace(valid, `"jsonrpc"`, `"result":{},"jsonrpc"`, 1), updateTestSession, "invalid_message"},
		{"provider error", strings.Replace(valid, `"jsonrpc"`, `"error":{"message":"opaque"},"jsonrpc"`, 1), updateTestSession, "invalid_message"},
		{"null params", `{"jsonrpc":"2.0","method":"session/update","params":null}`, updateTestSession, "invalid_update"},
		{"array", `[]`, updateTestSession, "invalid_message"},
		{"missing session", strings.Replace(valid, `"sessionId"`, `"SessionId"`, 1), updateTestSession, "invalid_update"},
	}
	for _, payload := range []string{
		`null`, `[]`, `{}`, `{"sessionUpdate":null}`, `{"sessionUpdate":"agent_message_chunk","content":{"type":"text"}}`,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":12}}`,
		`{"sessionUpdate":"tool_call","toolCallId":"x","title":"x","kind":"read","status":7}`,
		`{"sessionUpdate":"tool_call_update","status":"failed"}`, `{"sessionUpdate":"tool_call_update","toolCallId":"x","content":{}}`,
		`{"sessionUpdate":"tool_call_update","toolCallId":"x","content":[{"type":"diff","path":"x"}]}`,
		`{"sessionUpdate":"tool_call_update","toolCallId":"x","locations":[{"path":"x","line":-1}]}`,
		`{"sessionUpdate":"tool_call_update","toolCallId":"x","content":[{"type":"terminal","terminalId":3}]}`,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"resource","resource":{"uri":"x","text":"x","blob":"x"}}}`,
		`{"sessionUpdate":"usage_update","used":0}`, `{"sessionUpdate":"usage_update","used":null,"size":1}`,
		`{"sessionUpdate":"usage_update","used":-1,"size":1}`, `{"sessionUpdate":"usage_update","used":1,"size":-1}`,
		`{"sessionUpdate":"usage_update","used":1.5,"size":1}`, `{"sessionUpdate":"usage_update","used":1e1,"size":10}`,
		`{"sessionUpdate":"usage_update","used":"1","size":1}`, `{"sessionUpdate":"usage_update","used":9223372036854775808,"size":1}`,
	} {
		cases = append(cases, struct{ name, frame, session, code string }{payload, string(updateTestFrame(payload)), updateTestSession, "invalid_update"})
	}
	cases = append(cases, struct{ name, frame, session, code string }{"unknown", string(updateTestFrame(`{"sessionUpdate":"unknown_vendor_update"}`)), updateTestSession, "unsupported_update"})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := DecodeSessionUpdate([]byte(tc.frame), tc.session)
			var typed *UpdateDecodeError
			if !errors.As(err, &typed) || typed.Code != tc.code || err.Error() != "antigravity_acp_"+tc.code || !reflect.DeepEqual(out, SessionUpdate{}) {
				t.Fatalf("got=%+v err=%v want code=%s", out, err, tc.code)
			}
		})
	}
}

func TestSessionUpdateOwnsDecodedValues(t *testing.T) {
	frame := updateTestFrame(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"\ud83d\ude00\\u1234"}}`)
	out, err := DecodeSessionUpdate(frame, updateTestSession)
	if err != nil {
		t.Fatal(err)
	}
	for i := range frame {
		frame[i] = 0
	}
	if out.Text != "😀\\u1234" || out.SessionID != updateTestSession {
		t.Fatalf("decoded values changed: %+v", out)
	}
}

// The native capture is reduced to an explicit field allowlist. Session/tool IDs
// and the three probe paths are synthetic; all field shapes and event order were
// compared locally with the captured 1.4.1 file-read turn. The provider attempted
// three paths for one prompt, including two paths other than the fixture's full
// path. This is evidence of the native sequence, not a one-read guarantee.
func TestSessionUpdateNativeRead141(t *testing.T) {
	data, err := os.ReadFile("testdata/native-read-1.4.1.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*ToolCallDelta{}
	var text strings.Builder
	var usage *ContextUsage
	completedAfterText := 0
	for _, frame := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		update, err := DecodeSessionUpdate(frame, updateTestSession)
		if err != nil {
			t.Fatal(err)
		}
		switch update.Kind {
		case "tool_call":
			tool := update.Tool
			if tool.Title == nil || *tool.Title != "Running view_file" || tool.Kind == nil || *tool.Kind != "read" || tool.Status == nil || *tool.Status != "in_progress" {
				t.Fatalf("native start lost: %+v", tool)
			}
			tools[tool.ToolCallID] = tool
		case "tool_call_update":
			delta := update.Tool
			earlier := tools[delta.ToolCallID]
			if earlier == nil || delta.Title != nil || delta.Kind != nil || delta.Content != nil || delta.Status == nil || *delta.Status != "completed" {
				t.Fatalf("native completion lost: %+v", delta)
			}
			earlier.Status = delta.Status
			if text.Len() > 0 {
				completedAfterText++
			}
		case "agent_message_chunk":
			text.WriteString(update.Text)
		case "usage_update":
			usage = update.Context
		default:
			t.Fatalf("unexpected captured update: %s", update.Kind)
		}
	}
	if strings.TrimSpace(text.String()) != "CLAWDLINE_NATIVE_READ_OK" || len(tools) != 3 || completedAfterText != 3 || usage == nil || usage.Used <= 0 || usage.Size != 1048576 {
		t.Fatalf("reply=%q tools=%d late completions=%d context=%+v", text.String(), len(tools), completedAfterText, usage)
	}
	for id, tool := range tools {
		if *tool.Title != "Running view_file" || *tool.Kind != "read" || *tool.Status != "completed" || tool.Locations == nil || len(*tool.Locations) != 1 {
			t.Fatalf("partial completion erased native tool %s: %+v", id, tool)
		}
	}
}
