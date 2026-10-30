package antigravitysession

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/antigravityacp"
	"github.com/sainteye/clawdline/internal/contract"
)

func projectToolFrame(t *testing.T, e *contract.TranscriptEntry, update string) {
	t.Helper()
	frame := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"11111111-1111-4111-8111-111111111111","update":` + update + `}}`
	u, err := antigravityacp.DecodeSessionUpdate([]byte(frame), "11111111-1111-4111-8111-111111111111")
	if err != nil || u.Tool == nil {
		t.Fatalf("decode: %v", err)
	}
	applyToolDelta(e, u.Tool)
}

func TestNativeToolDetailsStayReadableAndBoundToHistory(t *testing.T) {
	h, row, opts := persistentFixture(t)
	c, err := h.lookup(row)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("complete output <script> not markup 😀\n", 2000)
	c.mu.Lock()
	c.ordinal++
	e := contract.TranscriptEntry{Role: "tool", Activity: &contract.TranscriptActivity{Kind: "called", Result: text, ResultKnown: true, ToolCallID: "read-a"}}
	if err := c.saveEntry(c.ordinal, "read-a", e); err != nil {
		t.Fatal(err)
	}
	c.mu.Unlock()
	page, err := h.TranscriptPage(context.Background(), row, 200, 0, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.Entries[0].Detail == nil {
		t.Fatalf("large output was not compacted: %+v", page)
	}
	ref := page.Entries[0].Detail.Reference
	if len(page.Entries[0].Activity.Result) >= len(text) {
		t.Fatal("full body was sent before expansion")
	}
	full, err := h.ToolEntry(context.Background(), row, ref)
	if err != nil || full.Activity.Result != text {
		t.Fatalf("detail lost: %v", err)
	}
	other := row
	other.ID = "other-session"
	if _, err := h.ToolEntry(context.Background(), other, ref); err == nil {
		t.Fatal("cross-session reference accepted")
	}
	stopHost(t, h)
	h = reopenHost(t, opts)
	full, err = h.ToolEntry(context.Background(), row, ref)
	if err != nil || full.Activity.Result != text {
		t.Fatalf("restart lost body: %v", err)
	}
	c, err = h.lookup(row)
	if err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	full.Activity.Result = "updated"
	if err := c.saveEntry(c.ordinal, "read-a", full); err != nil {
		t.Fatal(err)
	}
	c.mu.Unlock()
	if _, err := h.ToolEntry(context.Background(), row, ref); err == nil {
		t.Fatal("stale content reference accepted")
	}
}

func TestNativeToolOutputSurvivesPartialUpdates(t *testing.T) {
	e := contract.TranscriptEntry{Role: "tool", Activity: &contract.TranscriptActivity{Kind: "called"}}
	projectToolFrame(t, &e, `{"sessionUpdate":"tool_call","toolCallId":"call-1","title":"Running view_file","kind":"read","rawInput":{"AbsolutePath":"/workspace/example.go","StartLine":12,"EndLine":20},"locations":[{"path":"/workspace/example.go","line":11}],"status":"in_progress"}`)
	projectToolFrame(t, &e, `{"sessionUpdate":"tool_call_update","toolCallId":"call-1","rawOutput":"package example\n","status":"completed"}`)
	projectToolFrame(t, &e, `{"sessionUpdate":"tool_call_update","toolCallId":"call-1","status":"completed"}`)
	projectToolFrame(t, &e, `{"sessionUpdate":"tool_call_update","toolCallId":"call-1","content":[],"status":"completed"}`)
	if e.Activity.Result != "package example\n" {
		t.Fatalf("native output was lost: %+v", e.Activity)
	}
	raw, _ := json.Marshal(e.Activity.Actions)
	if !strings.Contains(string(raw), `"line":12`) || !strings.Contains(string(raw), `"endLine":20`) {
		t.Fatalf("line range lost: %s", raw)
	}
}

func TestNativeExecOutputKeepsExitCodeAndUnknownOutput(t *testing.T) {
	for _, output := range []string{`{"commandLine":"check","exitCode":7,"combinedOutput":"check failed"}`, `{"futureResult":{"kept":true}}`, `""`} {
		e := contract.TranscriptEntry{Role: "tool", Activity: &contract.TranscriptActivity{Kind: "called", Result: "older result"}}
		projectToolFrame(t, &e, `{"sessionUpdate":"tool_call_update","toolCallId":"call-2","rawOutput":`+output+`,"status":"completed"}`)
		if output == `""` {
			if e.Activity.Result != "" {
				t.Fatal("explicit empty result did not replace old text")
			}
			continue
		}
		if strings.Contains(e.Activity.Result, "older result") || e.Activity.Result == "" {
			t.Fatalf("output missing: %+v", e.Activity)
		}
		if output[0] == '{' && !strings.Contains(e.Activity.Result, strings.Split(strings.TrimPrefix(output, `{"`), `"`)[0]) {
			t.Fatalf("structured output lost: %s", e.Activity.Result)
		}
	}
}

func TestNativeReadContentAndDiffAreNotDiscarded(t *testing.T) {
	e := contract.TranscriptEntry{Role: "tool", Activity: &contract.TranscriptActivity{Kind: "called"}}
	projectToolFrame(t, &e, `{"sessionUpdate":"tool_call_update","toolCallId":"call-3","content":[{"type":"content","content":{"type":"resource","resource":{"uri":"file:///workspace/a.go","text":"resource body"}}},{"type":"diff","path":"/workspace/a.go","oldText":"old\n","newText":"new\n"}]}`)
	if !strings.Contains(e.Activity.Result, "resource body") || len(e.FileChanges) != 1 || e.FileChanges[0].Content != "new\n" {
		t.Fatalf("structured content lost: %+v", e)
	}
}

func TestNativeStructuredOutputKeepsFieldsMissingFromRenderableContent(t *testing.T) {
	e := contract.TranscriptEntry{Role: "tool", Activity: &contract.TranscriptActivity{Kind: "called"}}
	projectToolFrame(t, &e, `{"sessionUpdate":"tool_call_update","toolCallId":"exec","rawOutput":{"exitCode":7,"combinedOutput":"failed"},"content":[{"type":"content","content":{"type":"text","text":"failed"}}]}`)
	if !strings.Contains(e.Activity.Result, `"exitCode": 7`) || !strings.Contains(e.Activity.Result, `"combinedOutput": "failed"`) {
		t.Fatal("renderable content erased native fields")
	}
}
