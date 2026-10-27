package transcript

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func toolRow(result m) m {
	return codexItem(m{"type": "McpToolCall", "id": "call_synthetic", "server": "browser", "tool": "inspect", "arguments": m{"title": "Inspect page"}, "status": "completed", "result": result})
}

func TestToolResultUnwrapsBeforePreviewAndKeepsMixedContent(t *testing.T) {
	inner := m{"content": []m{{"type": "text", "text": "first\nsecond"}, {"type": "image", "data": "private-image"}, {"type": "resource", "resource": m{"text": "resource body"}}, {"type": "text", "text": "last"}}, "_meta": m{"codex/toolSurface": m{"screenshot": m{"url": "data:image/jpeg;base64,partial…100 chars truncated…"}}}}
	encoded, _ := json.Marshal(inner)
	path := writeRecord(t, toolRow(m{"content": []m{{"type": "text", "text": string(encoded)}}}))
	page, err := ReadCodexBefore(path, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	a := page.Entries[0].Activity
	r := a.ToolResult
	if r == nil || r.Images != 2 || r.UnknownBlocks != 1 || !r.SourceTruncated || !strings.HasPrefix(r.Preview, "first\nsecond\n\n") || !strings.HasSuffix(r.Preview, "\n\nlast") {
		t.Fatalf("projection: %+v", r)
	}
	if strings.Contains(r.Preview, "private-image") || !strings.Contains(r.Raw(), "private-image") {
		t.Fatal("image leaked into preview or raw lost")
	}
	if a.Result == nil || *a.Result != string(encoded) {
		t.Fatal("legacy view changed")
	}
	ref := r.ReferenceFor("session-A-execution-A")
	stored, err := ReadToolResult(path, ref, "session-A-execution-A")
	if err != nil || stored.Text() != r.Text() {
		t.Fatalf("stored read: %+v %v", stored, err)
	}
	for _, scope := range []string{"session-B-execution-A", "session-A-execution-B"} {
		if _, err := ReadToolResult(path, ref, scope); !errors.Is(err, ErrCursorStale) {
			t.Fatalf("cross-target read: %v", err)
		}
	}
	raw, _ := os.ReadFile(path)
	raw = []byte(strings.Replace(string(raw), "first", "other", 1))
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadToolResult(path, ref, "session-A-execution-A"); !errors.Is(err, ErrCursorStale) {
		t.Fatalf("edited row accepted: %v", err)
	}
}
func TestToolResultLeavesUnknownTextAloneAndClipsAtUTF8Boundary(t *testing.T) {
	for _, text := range []string{"{ multiple: true }", `{"not_content":"leave as JSON"}`, "EPERM: operation refused"} {
		raw, _ := json.Marshal(m{"content": []m{{"type": "text", "text": text}}})
		o, _ := rawObject(raw)
		r := projectToolResult(o)
		if r.Preview != text || r.Text() != text || r.SourceTruncated {
			t.Fatalf("plain output changed: %+v", r)
		}
	}
	text := strings.Repeat("界", 1000)
	raw, _ := json.Marshal(m{"content": []m{{"type": "text", "text": text}}})
	o, _ := rawObject(raw)
	r := projectToolResult(o)
	if len(r.Preview) > ToolPreviewBytesLimit || !utf8.ValidString(r.Preview) || r.Text() != text || r.TextBytes != len(text) {
		t.Fatalf("UTF-8 projection: %+v", r)
	}
}
func TestToolResultReadsOldRowsOutsideTheTailWithoutAcceptingPartialRows(t *testing.T) {
	path := writeRecord(t, toolRow(m{"content": []m{{"type": "text", "text": "old result"}}}))
	page, _ := ReadCodexBefore(path, 1, 0)
	ref := page.Entries[0].Activity.ToolResult.ReferenceFor("scope")
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	_, err := f.WriteString(strings.Repeat(" ", ReadBudget+1))
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadToolResult(path, ref, "scope"); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadToolResult(path, "1:"+strings.Repeat("0", 64), "scope"); !errors.Is(err, ErrCursorStale) {
		t.Fatalf("nonboundary: %v", err)
	}
}
