package http

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestExpandedToolBodiesReadTheirExactStoredContent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	item := session.Session{ID: "%7", Assistant: session.AssistantCodex, PID: os.Getpid(), ConversationID: "synthetic-detail"}
	path := filepath.Join(home, ".codex", "sessions", "rollout-"+item.ConversationID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("界", 700) + " opaque output retained"
	patch := "@@ -1,1 +1,30 @@\n-old\n" + strings.Repeat("+new\n", 30)
	rows := []map[string]any{
		{"type": "CommandExecution", "command": []string{"run", "fixture"}, "aggregated_output": long, "exit_code": 0},
		{"type": "FileChange", "changes": map[string]any{"/fixture/file.go": map[string]any{"type": "update", "unified_diff": patch}}},
	}
	var original []byte
	for _, entry := range rows {
		raw, _ := json.Marshal(map[string]any{"type": "event_msg", "payload": map[string]any{"type": "item_completed", "item": entry}})
		original = append(append(original, raw...), '\n')
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{executionMachineID: "machine-fixture"}
	full := s.transcriptPageBeforeView(item.ID, item, 20, 0, true)
	compact := s.transcriptPageBeforeProjection(item.ID, item, 20, 0, true, true)
	if len(full.Entries) != 3 || len(compact.Entries) != 3 {
		t.Fatalf("entries changed: %d/%d", len(full.Entries), len(compact.Entries))
	}
	text, change := compact.Entries[1], compact.Entries[2]
	if text.Detail == nil || !utf8.ValidString(text.Text) || len([]rune(text.Text)) != transcriptToolPreviewRunesLimit || text.Text == full.Entries[1].Text {
		t.Fatal("plain long output was not withheld as a UTF-8 preview")
	}
	if change.Detail == nil || change.Detail.LineCount != 32 || change.FileChanges[0].UnifiedDiff != "" || change.FileChanges[0].Stats.Additions != 30 || change.FileChanges[0].Stats.Deletions != 1 {
		t.Fatalf("patch summary: %+v", change)
	}
	if full.Entries[2].FileChanges[0].UnifiedDiff != patch {
		t.Fatal("legacy patch changed")
	}
	read := func(ref string, item session.Session) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.transcriptEntryDetail(w, httptest.NewRequest("GET", "/v1/transcript", nil), item.ID, item, "entry-v1:"+ref)
		return w
	}
	for _, i := range []int{1, 2} {
		ref := compact.Entries[i].Detail.Reference
		w := read(ref, item)
		var body struct {
			Reference string                   `json:"reference"`
			Entry     contract.TranscriptEntry `json:"entry"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		want, _ := json.Marshal(full.Entries[i])
		got, _ := json.Marshal(body.Entry)
		if w.Code != 200 || body.Reference != ref || string(want) != string(got) {
			t.Fatalf("detail did not reproduce complete entry: %d", w.Code)
		}
	}

	live := paneServer(t, &pane{s: item})
	live.executionMachineID = "machine-fixture"
	first := httptest.NewRecorder()
	live.transcriptRoute(first, httptest.NewRequest("GET", "/v1/transcript?session=%257&view=tools-v2", nil))
	var routed contract.TranscriptPage
	if err := json.Unmarshal(first.Body.Bytes(), &routed); err != nil || first.Code != 200 || routed.Entries[1].Detail == nil {
		t.Fatalf("versioned route: %d %v", first.Code, err)
	}
	same := httptest.NewRecorder()
	live.transcriptRoute(same, httptest.NewRequest("GET", "/v1/transcript?session=%257&view=tools-v2:"+routed.Version, nil))
	var held map[string]any
	json.Unmarshal(same.Body.Bytes(), &held)
	if held["unchanged"] != true {
		t.Fatalf("conditional v2 page repeated: %d", same.Code)
	}
	for _, other := range []session.Session{
		{ID: "%8", Assistant: item.Assistant, PID: item.PID, ConversationID: item.ConversationID},
		{ID: item.ID, Assistant: item.Assistant, PID: 0, ConversationID: item.ConversationID},
		{ID: item.ID, Assistant: item.Assistant, PID: item.PID, ConversationID: "replacement"},
	} {
		if w := read(text.Detail.Reference, other); w.Code != 409 {
			t.Fatalf("other execution accepted: %d", w.Code)
		}
	}
	s.executionMachineID = "other-machine"
	if w := read(text.Detail.Reference, item); w.Code != 409 {
		t.Fatalf("other machine accepted: %d", w.Code)
	}
	s.executionMachineID = "machine-fixture"
	// Appending preserves an old reference; replacing the same row does not.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("{}\n")
	f.Close()
	if w := read(text.Detail.Reference, item); w.Code != 200 {
		t.Fatal("append invalidated retained output")
	}
	replaced := strings.Replace(string(original), "opaque output retained", "edited output retained", 1)
	if err := os.WriteFile(path, []byte(replaced), 0600); err != nil {
		t.Fatal(err)
	}
	if w := read(text.Detail.Reference, item); w.Code != 409 {
		t.Fatalf("replaced source accepted: %d", w.Code)
	}
}

func TestToolDetailsKeepShortContentQuestionsAndExactDiffThreshold(t *testing.T) {
	for _, text := range []string{"short", strings.Repeat("s", transcriptInlineToolBytesLimit), "\x01ask\x01" + strings.Repeat("q", 2000)} {
		row := contract.TranscriptEntry{Role: "tool", Text: text}
		got := compactEntryDetails(row, transcript.Entry{}, "scope")
		if got.Detail != nil || got.Text != text {
			t.Fatal("short output or question was made lazy")
		}
	}
}

func TestSmallDiffStaysInlineAndLargeContentIsNotRemovedByEncodingPrefix(t *testing.T) {
	for _, lines := range []int{18, 19} {
		row := contract.TranscriptEntry{Role: "tool", FileChanges: []contract.TranscriptFileChange{{Path: "file", Kind: "add", Content: strings.Repeat("line\n", lines)}}}
		got := compactEntryDetails(row, transcript.Entry{}, "scope")
		if (got.Detail != nil) != (lines > 18) {
			t.Fatalf("wrong folding threshold at %d", lines)
		}
	}
	row := contract.TranscriptEntry{Role: "tool", Text: "ID3" + strings.Repeat("opaque", 400)}
	got := compactEntryDetails(row, transcript.Entry{}, "scope")
	if got.Detail == nil || !strings.HasPrefix(got.Text, "ID3") {
		t.Fatal("opaque encoded output was suppressed")
	}
}

func TestClaudeStoredToolOutputKeepsEveryEntryOfItsNativeRow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	item := session.Session{ID: "%8", Assistant: session.AssistantClaude, PID: os.Getpid(), ConversationID: "synthetic-claude", CWD: "/fixture"}
	path := transcript.ClaudePath(home, item.CWD, item.ConversationID)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("opaque output", 150)
	raw, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "tool_result", "content": "short result"},
		map[string]any{"type": "tool_result", "content": text},
	}}})
	if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{executionMachineID: "machine-fixture"}
	compact := s.transcriptPageBeforeProjection(item.ID, item, 20, 0, true, true)
	if len(compact.Entries) != 2 || compact.Entries[0].Detail != nil || compact.Entries[1].Detail == nil {
		t.Fatalf("Claude projection: %+v", compact)
	}
	w := httptest.NewRecorder()
	s.transcriptEntryDetail(w, httptest.NewRequest("GET", "/v1/transcript", nil), item.ID, item, "entry-v1:"+compact.Entries[1].Detail.Reference)
	var body struct {
		Entry contract.TranscriptEntry `json:"entry"`
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != 200 || body.Entry.Text != strings.TrimSpace(text) {
		t.Fatalf("Claude detail: %d", w.Code)
	}
}
