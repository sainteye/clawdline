package transcript

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"strconv"
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
	if r == nil || r.Images != 0 || r.UnknownBlocks != 1 || r.SourceTruncated || !strings.HasPrefix(r.Preview, "first\nsecond\n\n") || !strings.HasSuffix(r.Preview, "\n\nlast") {
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

func toolImageFixture(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 1))); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestAutomaticScreenshotsDoNotBecomeReadableImagesOrTextWarnings(t *testing.T) {
	for _, data := range []string{toolImageFixture(t), "partial…100 chars truncated…"} {
		raw, _ := json.Marshal(m{"content": []m{{"type": "text", "text": "Readable result"}}, "_meta": m{"codex/toolSurface": m{"screenshot": m{"url": "data:image/png;base64," + data}}}})
		obj, _ := rawObject(raw)
		result := projectToolResult(obj)
		if result.Images != 0 || result.SourceTruncated || result.Text() != "Readable result" {
			t.Fatalf("automatic metadata reached visible content: %+v", result)
		}
		if !strings.Contains(result.Raw(), data) {
			t.Fatal("stored native metadata was lost")
		}
		if _, ok := result.ImageURL(0); ok {
			t.Fatal("automatic screenshot is still readable through image route")
		}
	}
}

func TestOnlyCompleteExplicitToolImagesAreOffered(t *testing.T) {
	good := toolImageFixture(t)
	broken := good[:len(good)/2] + "…100 chars truncated…" + good[len(good)/2:]
	raw, _ := json.Marshal(m{"content": []m{{"type": "image", "mimeType": "image/png", "data": broken}, {"type": "image", "mimeType": "image/png", "data": good}, {"type": "image", "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString([]byte("not a raster"))}, {"type": "image", "mimeType": "image/svg+xml", "data": base64.StdEncoding.EncodeToString([]byte("<svg/>"))}}, "_meta": m{"codex/toolSurface": m{"screenshot": m{"url": "data:image/png;base64," + good}}}})
	obj, _ := rawObject(raw)
	result := projectToolResult(obj)
	if result.Images != 1 || result.SourceTruncated {
		t.Fatalf("incomplete images were advertised: %+v", result)
	}
	if url, ok := result.ImageURL(0); !ok || url != "data:image/png;base64,"+good {
		t.Fatal("complete explicit output was lost")
	}
	if !strings.Contains(result.Raw(), broken) {
		t.Fatal("unavailable explicit output was removed from native record")
	}
}

func TestLegacyImageIndexesCannotSelectAReprojectedResult(t *testing.T) {
	path := writeRecord(t, toolRow(m{"content": []m{{"type": "image", "mimeType": "image/png", "data": toolImageFixture(t)}}}))
	page, _ := ReadCodexBefore(path, 1, 0)
	result := page.Entries[0].Activity.ToolResult
	offset, _ := strconv.ParseInt(strings.Split(result.Reference, ":")[0], 10, 64)
	old := toolReference(offset, "scope"+"\x00"+result.ToolID+"\x00"+result.Raw())
	if _, err := ReadToolResult(path, old, "scope"); !errors.Is(err, ErrCursorStale) {
		t.Fatalf("old projection reference accepted: %v", err)
	}
}

func TestVisibleTextAndRawSourceClippingAreIndependent(t *testing.T) {
	for _, text := range []string{"complete text", "first…100 chars truncated…last"} {
		raw, _ := json.Marshal(m{"content": []m{{"type": "text", "text": text}}, "_meta": m{"codex/toolSurface": m{"screenshot": m{"url": "data:image/png;base64,partial…100 chars truncated…"}}}})
		obj, _ := rawObject(raw)
		result := projectToolResult(obj)
		if result.SourceTruncatedFor("text") != strings.Contains(text, " chars truncated") {
			t.Fatal("text warning follows hidden metadata")
		}
		if !result.SourceTruncatedFor("raw") {
			t.Fatal("raw source warning was lost")
		}
	}
}
