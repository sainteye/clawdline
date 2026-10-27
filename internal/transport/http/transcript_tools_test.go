package http

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"image"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestCompactToolPageReadsTheSameStoredResultAndRejectsOtherExecutions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	item := session.Session{ID: "%7", Assistant: session.AssistantCodex, PID: os.Getpid(), ConversationID: "synthetic-conversation"}
	path := filepath.Join(home, ".codex", "sessions", "rollout-"+item.ConversationID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	inner := `{"content":[{"type":"text","text":"first\nsecond"}],"_meta":{"codex/toolSurface":{"screenshot":{"url":"data:image/jpeg;base64,partial…100 chars truncated…"}}}}`
	row := map[string]any{"type": "event_msg", "payload": map[string]any{"type": "item_completed", "item": map[string]any{"type": "McpToolCall", "arguments": map[string]any{"title": "Inspect"}, "status": "completed", "result": map[string]any{"content": []map[string]any{{"type": "text", "text": inner}}}}}}
	raw, _ := json.Marshal(row)
	if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	old := s.transcriptPageBefore(item.ID, item, 20, 0)
	compact := s.transcriptPageBeforeView(item.ID, item, 20, 0, true)
	if len(compact.Entries) != 1 || compact.Entries[0].Activity.ToolResult == nil {
		t.Fatalf("compact page: %+v", compact)
	}
	result := compact.Entries[0].Activity.ToolResult
	if result.Preview != "first\nsecond" || result.Images != 0 || compact.Entries[0].Activity.Result != "" || old.Entries[0].Activity.Result != inner {
		t.Fatal("projection lost text or leaked envelope")
	}
	ref := result.Reference
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/transcript", nil)
	s.transcriptToolRead(w, r, item.ID, item, "tool-v1:"+ref+":text")
	var detail map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || detail["text"] != "first\nsecond" || detail["reference"] != ref {
		t.Fatalf("detail: %d %v", w.Code, detail)
	}
	if detail["sourceTruncated"] != false {
		t.Fatal("metadata-only clipping warned on text")
	}
	w = httptest.NewRecorder()
	s.transcriptToolRead(w, r, item.ID, item, "tool-v1:"+ref+":raw")
	var rawDetail map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &rawDetail)
	if rawDetail["sourceTruncated"] != true {
		t.Fatal("native view lost its clipping warning")
	}
	for _, other := range []session.Session{{ID: "%8", Assistant: item.Assistant, PID: item.PID, ConversationID: item.ConversationID}, {ID: item.ID, Assistant: item.Assistant, PID: 0, ConversationID: item.ConversationID}, {ID: item.ID, Assistant: item.Assistant, PID: item.PID, ConversationID: "replacement"}} {
		w = httptest.NewRecorder()
		s.transcriptToolRead(w, r, other.ID, other, "tool-v1:"+ref+":raw")
		if w.Code != 409 {
			t.Fatalf("cross target %q: %d", other.ID, w.Code)
		}
	}
	if transcriptVersion(compact, "machine-A") == transcriptVersion(compact, "machine-B") {
		t.Fatal("conditional version crossed machines")
	}
	c, known, valid := transcriptView("tools-v1z:" + strings.Repeat("a", 64))
	if !c || !valid || len(known) != 64 {
		t.Fatal("conditional view rejected")
	}
	for _, bad := range []string{"other", "tools-v1:short", "tools-v1:" + strings.Repeat("z", 64)} {
		if _, _, valid := transcriptView(bad); valid {
			t.Fatalf("invalid view accepted: %s", bad)
		}
	}
	live := paneServer(t, &pane{s: item})
	initial := httptest.NewRecorder()
	live.transcriptRoute(initial, httptest.NewRequest("GET", "/v1/transcript?session=%257&view=tools-v1", nil))
	var initialPage contract.TranscriptPage
	if err := json.Unmarshal(initial.Body.Bytes(), &initialPage); err != nil || initialPage.Version == "" {
		t.Fatalf("initial: %d %s %v", initial.Code, initial.Body.String(), err)
	}
	same := httptest.NewRecorder()
	live.transcriptRoute(same, httptest.NewRequest("GET", "/v1/transcript?session=%257&view=tools-v1:"+initialPage.Version, nil))
	var reuse map[string]any
	_ = json.Unmarshal(same.Body.Bytes(), &reuse)
	if same.Code != 200 || reuse["unchanged"] != true || reuse["entries"] != nil {
		t.Fatalf("unchanged page repeated: %d %v", same.Code, reuse)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = f.WriteString(`{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","content":[{"type":"text","text":"new turn"}]}}}` + "\n")
	f.Close()
	changed := httptest.NewRecorder()
	live.transcriptRoute(changed, httptest.NewRequest("GET", "/v1/transcript?session=%257&view=tools-v1:"+initialPage.Version, nil))
	var changedPage contract.TranscriptPage
	_ = json.Unmarshal(changed.Body.Bytes(), &changedPage)
	if changedPage.Version == initialPage.Version || len(changedPage.Entries) != 2 {
		t.Fatalf("changed page reused: %s", changed.Body.String())
	}

	var _ contract.TranscriptPage = compact
}

func TestStoredToolImagesAreLazyAndNeverRepairTruncatedBase64(t *testing.T) {
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 64, 32))); err != nil {
		t.Fatal(err)
	}
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngData.Bytes())
	path := filepath.Join(t.TempDir(), "record.jsonl")
	for _, stored := range []string{url, url[:len(url)/2] + "…100 chars truncated…" + url[len(url)/2:]} {
		inner := map[string]any{"content": []any{map[string]any{"type": "image", "mimeType": "image/png", "data": strings.TrimPrefix(stored, "data:image/png;base64,")}}}
		innerRaw, _ := json.Marshal(inner)
		row := map[string]any{"type": "event_msg", "payload": map[string]any{"type": "item_completed", "item": map[string]any{"type": "McpToolCall", "arguments": map[string]any{"title": "Screenshot"}, "result": map[string]any{"content": []map[string]any{{"type": "text", "text": string(innerRaw)}}}}}}
		raw, _ := json.Marshal(row)
		if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		page, err := transcript.ReadCodexBefore(path, 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		ref := page.Entries[0].Activity.ToolResult.ReferenceFor("scope")
		result, err := transcript.ReadToolResult(path, ref, "scope")
		if err != nil {
			t.Fatal(err)
		}
		expectedImages := 1
		if stored != url {
			expectedImages = 0
		}
		if result.Images != expectedImages || strings.Contains(result.Preview, "base64") {
			t.Fatal("image in first paint")
		}
		for _, variant := range []string{"image-0-thumb", "image-0-original"} {
			w := httptest.NewRecorder()
			(&Server{}).transcriptToolImage(w, httptest.NewRequest("GET", "/", nil), "s", result, variant)
			if stored != url {
				if w.Code != 404 {
					t.Fatalf("clipped image accepted: %d", w.Code)
				}
				continue
			}
			if w.Code != 200 {
				t.Fatalf("valid image refused: %d %s", w.Code, w.Body.String())
			}
			var body map[string]any
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			dataURL := body["dataURL"].(string)
			data, _ := base64.StdEncoding.DecodeString(strings.SplitN(dataURL, ",", 2)[1])
			cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
			if err != nil || cfg.Width != 64 || cfg.Height != 32 {
				t.Fatalf("image: %+v %s %v", cfg, format, err)
			}
			if variant == "image-0-original" && !bytes.Equal(data, pngData.Bytes()) {
				t.Fatal("original changed")
			}
		}
	}
}
