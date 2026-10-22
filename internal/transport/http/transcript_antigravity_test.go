package http

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestAntigravityHTTPUsesNativeParserAndPreservesToolFailure(t *testing.T) {
	home := t.TempDir()
	path, ok := transcript.AntigravityPath(home, "74000000-0000-4000-8000-000000000001")
	if !ok {
		t.Fatal("invalid synthetic path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-10-01T12:00:00Z","content":"<USER_REQUEST>Hello</USER_REQUEST><ADDITIONAL_METADATA>private</ADDITIONAL_METADATA>"}` + "\n" +
		`{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-10-01T12:00:01Z","tool_calls":[{"name":"edit_file","args":{"Path":"sample.txt"}}]}` + "\n" +
		`{"step_index":2,"source":"MODEL","type":"GENERIC","status":"INVALID","created_at":"2026-10-01T12:00:02Z","content":"Native output","error":"Synthetic failure"}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	page, err := readTranscriptBefore(path, session.AssistantAntigravity, 200, 0)
	if err != nil || len(page.Entries) != 3 {
		t.Fatalf("native page=%+v %v", page, err)
	}
	user, call, result := transcriptEntry(page.Entries[0]), transcriptEntry(page.Entries[1]), transcriptEntry(page.Entries[2])
	if user.Role != "user" || user.Text != "Hello" || call.Tool != "edit_file" || result.Role != "tool" || result.Tool != "" || result.Activity == nil || result.Activity.Status != "INVALID" || !strings.Contains(result.Text, "Synthetic failure") {
		t.Fatalf("wire lost native roles/status: %+v %+v %+v", user, call, result)
	}
	if _, err := readTranscriptBefore(path, session.Assistant("unknown"), 200, 0); err == nil {
		t.Fatal("unknown provider fell through to Claude")
	}
	if _, err := readTranscriptBefore(filepath.Join(home, "transcript.jsonl"), session.AssistantAntigravity, 200, 0); err == nil {
		t.Fatal("provisional path accepted")
	}
}

func TestAntigravityHTTPUnknownBindingDoesNotChooseNearbyRecord(t *testing.T) {
	item := session.Session{ID: "%synthetic", Assistant: session.AssistantAntigravity, Binding: session.BindingUnreadable, CWD: t.TempDir()}
	page := (&Server{}).transcriptPage(item.ID, item, 200)
	if page.Evidence != "none" || page.Note == "" || len(page.Entries) != 0 {
		t.Fatalf("unknown binding read another record: %+v", page)
	}
}
