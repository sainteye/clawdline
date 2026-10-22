package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

type revisionBindingFixture struct{ binding ports.AntigravityBinding }

func (f revisionBindingFixture) AntigravityBinding(context.Context, session.Session) (ports.AntigravityBinding, error) {
	return f.binding, nil
}

func TestAntigravityHTTPRevisionFencesNativePages(t *testing.T) {
	home := t.TempDir()
	conversation := "74000000-0000-4000-8000-000000000003"
	path, _ := transcript.AntigravityPath(home, conversation)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	row := func(index int, text string) string {
		return fmt.Sprintf(`{"step_index":%d,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-10-01T12:00:00Z","content":"<USER_REQUEST>%s</USER_REQUEST>"}`+"\n", index, text)
	}
	data := row(0, "first") + row(1, "second") + row(2, "third")
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(data)
	p := waitingPane("%fixture-native", "")
	p.s.Assistant, p.s.ConversationID = session.AssistantAntigravity, conversation
	host := transcript.NewHost()
	host.Home = home
	host.SetAntigravityBindings(revisionBindingFixture{ports.AntigravityBinding{
		ConversationID: conversation, TranscriptPath: path, Verified: true,
		Process: ports.AntigravityProcess{TerminalID: p.s.ID}}})
	s := paneServer(t, p)
	s.inventory.Identity = host
	server := httptest.NewServer(http.HandlerFunc(s.transcriptRoute))
	defer server.Close()
	read := func(query string, status int) contract.TranscriptPage {
		t.Helper()
		response, err := server.Client().Get(server.URL + "/v1/transcript?session=" + url.QueryEscape(p.s.ID) + "&limit=1" + query)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != status {
			t.Fatalf("%s status=%d want=%d", query, response.StatusCode, status)
		}
		var page contract.TranscriptPage
		if status == http.StatusOK {
			if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
				t.Fatal(err)
			}
		}
		return page
	}
	newest := read("", http.StatusOK)
	if newest.NextBefore == 0 || !strings.HasPrefix(newest.Signature, "agy1:") {
		t.Fatalf("native page: %+v", newest)
	}
	before := fmt.Sprintf("&before=%d&revision=%s", newest.NextBefore, url.QueryEscape(newest.Signature))
	older := read(before, http.StatusOK)
	if len(older.Entries) != 1 || older.Entries[0].Text != "second" {
		t.Fatalf("older: %+v", older)
	}
	read(fmt.Sprintf("&before=%d", newest.NextBefore), http.StatusConflict)
	read(fmt.Sprintf("&before=%d&revision=invalid", newest.NextBefore), http.StatusConflict)
	write(data + row(3, "fourth"))
	appended := read(fmt.Sprintf("&after=%d&revision=%s", newest.NextAfter, url.QueryEscape(newest.Signature)), http.StatusOK)
	if len(appended.Entries) != 1 || appended.Entries[0].Text != "fourth" {
		t.Fatalf("appended: %+v", appended)
	}
	// Older native pages require the exact full snapshot, including appended bytes.
	read(before, http.StatusConflict)
	write(strings.Replace(data, "first", "other", 1))
	read(before, http.StatusConflict)
	write(row(0, "short"))
	read(before, http.StatusConflict)
	write(data)
	current := read("", http.StatusOK)
	replacement := path + ".replacement"
	if err := os.WriteFile(replacement, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	read(fmt.Sprintf("&before=%d&revision=%s", current.NextBefore, url.QueryEscape(current.Signature)), http.StatusConflict)
}

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
