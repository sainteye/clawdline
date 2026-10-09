package http

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// recordSession is a Claude session whose record would be at the returned
// path, under a made-up home. Nothing is written there.
func recordSession(t *testing.T) (session.Session, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	item := session.Session{ID: "%7", Assistant: session.AssistantClaude,
		CWD: filepath.Join(home, "code", "app"), ConversationID: "c6000003-0000-4000-8000-000000000003"}
	path := transcript.ClaudePath(home, item.CWD, item.ConversationID)
	return item, path, home
}

// unreadableRecord writes the record and takes its read permission away.
func unreadableRecord(t *testing.T, path string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"type":"user"}`+"\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
}

// pathFree fails when a note a person reads names the record's file or the
// home directory it is under. The page travels to paired devices over Cloud.
func pathFree(t *testing.T, what, note, path, home string) {
	t.Helper()
	for _, leak := range []string{path, home, filepath.Base(path)} {
		if strings.Contains(note, leak) {
			t.Errorf("%s names a path (%q): %q", what, leak, note)
		}
	}
}

// A session that has just started has not written its record yet: Claude
// Code creates the file with the first turn. That is a conversation with
// nothing in it, answered as the Swift app answers it — no entries, an empty
// signature — and not an error with the operating system's words in it.
func TestANewSessionsTranscriptIsEmptyNotAnError(t *testing.T) {
	item, path, home := recordSession(t)
	s := &Server{}

	page := s.transcriptPage(item.ID, item, 200)
	if page.Evidence == contract.EvidenceNone || page.Note != "" {
		t.Errorf("a record not written yet reads as a failure: evidence %q, note %q", page.Evidence, page.Note)
	}
	if len(page.Entries) != 0 || page.Signature != "" {
		t.Errorf("a record not written yet has entries or a signature: %d, %q", len(page.Entries), page.Signature)
	}
	pathFree(t, "the transcript note", page.Note, path, home)

}

// A record that is there and cannot be read is a failure, and says so — in
// words that name what went wrong and not where.
func TestAnUnreadableTranscriptFailsWithoutNamingItsPath(t *testing.T) {
	item, path, home := recordSession(t)
	unreadableRecord(t, path)
	s := &Server{}

	page := s.transcriptPage(item.ID, item, 200)
	if page.Evidence != contract.EvidenceNone || page.Note == "" {
		t.Errorf("an unreadable record is not a failure: evidence %q, note %q", page.Evidence, page.Note)
	}
	pathFree(t, "the transcript note", page.Note, path, home)

}

func TestTranscriptByteBudgetPagesDoNotSkipOlderEntries(t *testing.T) {
	item, path, _ := recordSession(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var record strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&record, `{"type":"user","timestamp":"2026-09-16T10:00:00Z","message":{"content":"%03d:%s"}}`+"\n", i, strings.Repeat("x", 2000))
	}
	if err := os.WriteFile(path, []byte(record.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	var pages [][]string
	before := int64(0)
	for {
		page := s.transcriptPageBefore(item.ID, item, 200, before)
		if page.Evidence != contract.EvidenceTranscript {
			t.Fatalf("page failed: %+v", page)
		}
		var texts []string
		for _, row := range page.Entries {
			texts = append(texts, row.Text[:3])
		}
		pages = append(pages, texts)
		if page.NextBefore == 0 {
			break
		}
		if before != 0 && page.NextBefore >= before {
			t.Fatalf("cursor did not move: %d -> %d", before, page.NextBefore)
		}
		before = page.NextBefore
	}
	var found []string
	for i := len(pages) - 1; i >= 0; i-- {
		found = append(found, pages[i]...)
	}
	if len(found) != 100 {
		t.Fatalf("got %d entries across pages, want 100", len(found))
	}
	for i, value := range found {
		if want := fmt.Sprintf("%03d", i); value != want {
			t.Fatalf("entry %d is %q, want %q", i, value, want)
		}
	}
}

// A page that read the record whole asks with nextAfter and gets only what
// was appended; a cursor the record no longer meets is stale, which the route
// answers 409 transcript_cursor_stale so the page reads the newest page whole.
func TestTranscriptAfterCursorReadsOnlyWhatWasAppended(t *testing.T) {
	item, path, _ := recordSession(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	row := func(text string) string {
		return `{"type":"user","timestamp":"2026-09-16T10:00:00Z","message":{"content":"` + text + `"}}` + "\n"
	}
	if err := os.WriteFile(path, []byte(row("one")+row("two")), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	whole := s.transcriptPageBefore(item.ID, item, 200, 0)
	if whole.NextAfter == 0 || len(whole.Entries) != 2 {
		t.Fatalf("a whole read carries no cursor: %+v", whole)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(row("three")); err != nil {
		t.Fatal(err)
	}
	f.Close()
	added, stale := s.transcriptPageAfter(item.ID, item, 200, whole.NextAfter)
	if stale || len(added.Entries) != 1 || added.Entries[0].Text != "three" || added.NextAfter <= whole.NextAfter ||
		added.Evidence != contract.EvidenceTranscript || added.NextBefore != 0 {
		t.Fatalf("appended page: stale %v, %+v", stale, added)
	}
	if _, stale := s.transcriptPageAfter(item.ID, item, 200, added.NextAfter+1); !stale {
		t.Fatal("a cursor past the end was not stale")
	}
	if _, stale := s.transcriptPageAfter(item.ID, item, 1, 0); !stale {
		t.Fatal("more appended rows than one page was not stale")
	}
	unlocated := item
	unlocated.ConversationID = ""
	if _, stale := s.transcriptPageAfter(item.ID, unlocated, 200, 0); !stale {
		t.Fatal("a session with no record to read was not stale")
	}
}

func TestTranscriptAfterCursorMustBeANonnegativeIntegerAlone(t *testing.T) {
	s := &Server{}
	for _, query := range []string{"after=-1", "after=x", "after=10&before=20"} {
		w := httptest.NewRecorder()
		s.transcriptRoute(w, httptest.NewRequest("GET", "/v1/transcript?session=%257&"+query, nil))
		if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_cursor") {
			t.Errorf("%s answered %d %s", query, w.Code, w.Body.String())
		}
	}
}
