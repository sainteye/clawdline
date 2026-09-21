package http

import (
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
	s := &Server{ledger: transcript.NewLedger()}

	page := s.transcriptPage(item.ID, item, 200)
	if page.Evidence == contract.EvidenceNone || page.Note != "" {
		t.Errorf("a record not written yet reads as a failure: evidence %q, note %q", page.Evidence, page.Note)
	}
	if len(page.Entries) != 0 || page.Signature != "" {
		t.Errorf("a record not written yet has entries or a signature: %d, %q", len(page.Entries), page.Signature)
	}
	pathFree(t, "the transcript note", page.Note, path, home)

	row := s.usageRow(item)
	if row.Evidence != contract.EvidenceNone || row.TotalTokens != 0 {
		t.Errorf("a record not written yet has usage: %+v", row)
	}
	pathFree(t, "the usage note", row.Note, path, home)
}

// A record that is there and cannot be read is a failure, and says so — in
// words that name what went wrong and not where.
func TestAnUnreadableTranscriptFailsWithoutNamingItsPath(t *testing.T) {
	item, path, home := recordSession(t)
	unreadableRecord(t, path)
	s := &Server{ledger: transcript.NewLedger()}

	page := s.transcriptPage(item.ID, item, 200)
	if page.Evidence != contract.EvidenceNone || page.Note == "" {
		t.Errorf("an unreadable record is not a failure: evidence %q, note %q", page.Evidence, page.Note)
	}
	pathFree(t, "the transcript note", page.Note, path, home)

	row := s.usageRow(item)
	if row.Evidence != contract.EvidenceNone || row.Note == "" {
		t.Errorf("an unreadable record is not a failure in usage: %+v", row)
	}
	pathFree(t, "the usage note", row.Note, path, home)
}
