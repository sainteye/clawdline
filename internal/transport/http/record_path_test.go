package http

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// movedClaudeSession is a Claude session whose process now sits in a
// different directory from the one it was launched in. Claude Code filed its
// record under the launch directory, which only the registry still names; the
// process table reports where the process is now.
func movedClaudeSession(t *testing.T) (*transcript.Host, session.Session, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	launched := filepath.Join(home, "launched")
	moved := filepath.Join(home, "launched", "moved")
	const pid = 4242
	const conv = "0c0ffee0-0000-4000-8000-00000000c0de"
	registry := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(registry, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := `{"pid":4242,"sessionId":"` + conv + `","cwd":"` + launched + `"}`
	if err := os.WriteFile(filepath.Join(registry, strconv.Itoa(pid)+".json"), []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	record := transcript.ClaudePath(home, launched, conv)
	if err := os.MkdirAll(filepath.Dir(record), 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","timestamp":"2026-10-18T00:00:00.000Z","message":{"role":"user","content":"hello"}}` + "\n"
	if err := os.WriteFile(record, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	host := transcript.NewHost()
	host.Home = home
	host.Refresh()
	item := session.Session{ID: "%42", PID: pid, Assistant: session.AssistantClaude, CWD: moved, ConversationID: conv}
	return host, item, record, transcript.ClaudePath(home, moved, conv)
}

// One Session has one record. A Session that changed directory after it was
// launched is read from the launch directory's record by every reader in this
// package: the conversation page, the session's facts, the start screen's
// "already open" set, and the host's own readers.
func TestOneSessionHasOneRecord(t *testing.T) {
	host, item, launchedRecord, movedRecord := movedClaudeSession(t)
	s := &Server{inventory: app.Inventory{Identity: host}}
	if got := s.recordPath(item); got != launchedRecord {
		t.Fatalf("reader path = %q, want the registry's %q", got, launchedRecord)
	}
	if got := host.RecordPath(item); got != launchedRecord {
		t.Fatalf("host path = %q, want %q", got, launchedRecord)
	}
	if a := host.LastActivity(t.Context(), item); a.Reason != "" {
		t.Fatalf("host did not find the record: %+v", a)
	}
	page := s.transcriptPage(item.ID, item, 10)
	if page.Note != "" || len(page.Entries) == 0 {
		t.Fatalf("the conversation page did not read the record: %+v", page)
	}
	reading := s.startReadingFrom(session.Inventory{Sessions: []session.Session{item}})
	open := launchedRecord
	if resolved, err := filepath.EvalSymlinks(open); err == nil {
		open = resolved
	}
	if !reading.openClaude[open] || len(reading.openClaude) != 1 {
		t.Fatalf("start screen's open records = %v, want only %q", reading.openClaude, open)
	}
	if _, err := os.Stat(movedRecord); !os.IsNotExist(err) {
		t.Fatalf("the scanned directory should hold no record: %v", err)
	}
}

func TestRemoteControlUsesTheSameTranscriptAndLiveHistoryIdentity(t *testing.T) {
	host, item, record, _ := movedClaudeSession(t)
	item.ID, item.Backend = "claude-remote:4242:fixture", session.BackendClaudeRemote
	s := &Server{inventory: app.Inventory{Identity: host}}
	row := rowJSON(t, s.sessionRow(rowInput{item: item}))
	if row["backend"] != "claude_remote" || row["sessionId"] != item.ConversationID {
		t.Fatalf("remote row lost its local identity: %+v", row)
	}
	page := s.transcriptPage(item.ID, item, 10)
	if page.Note != "" || len(page.Entries) != 1 || page.Entries[0].Text != "hello" {
		t.Fatalf("remote history was not readable: %+v", page)
	}
	reading := s.startReadingFrom(session.Inventory{Sessions: []session.Session{item}})
	if resolved, err := filepath.EvalSymlinks(record); err == nil {
		record = resolved
	}
	if !reading.openClaude[record] {
		t.Fatal("the native history picker could resume an already running remote conversation")
	}
}

// Without a registry entry the scan's directory is the answer, as before;
// and a daemon whose identity port is not the host still finds records there.
func TestARecordFallsBackToTheScannedDirectory(t *testing.T) {
	_, item, _, movedRecord := movedClaudeSession(t)
	item.PID = 9999
	host := transcript.NewHost()
	host.Home = os.Getenv("HOME")
	host.Refresh()
	for name, s := range map[string]*Server{
		"host":    {inventory: app.Inventory{Identity: host}},
		"no host": {},
	} {
		if got := s.recordPath(item); got != movedRecord {
			t.Fatalf("%s: path = %q, want the scan's %q", name, got, movedRecord)
		}
	}
	none := item
	none.ConversationID = ""
	if got := (&Server{}).recordPath(none); got != "" {
		t.Fatalf("a session with no conversation id has a record: %q", got)
	}
}
