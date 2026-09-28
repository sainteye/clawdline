//go:build darwin

package transcript

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
	"golang.org/x/sys/unix"
)

// Codex 0.157.1 can leave the TUI process with no rollout descriptor at all:
// its managed app-server owns the rollout and writer lock instead. The live
// lock, rollout head, session index and terminal title together still name one
// conversation without correlating timestamps or choosing among cwd peers.
func TestAManagedCodexThreadBindsItsITermSession(t *testing.T) {
	home := t.TempDir()
	id := "c0de0001-0000-4000-8000-000000000001"
	codex := filepath.Join(home, ".codex")
	locks := filepath.Join(codex, "thread-writer-locks")
	sessions := filepath.Join(codex, "sessions", "2026", "09", "28")
	if err := os.MkdirAll(locks, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codex, "session_index.jsonl"), []byte(
		`{"id":"`+id+`","thread_name":"Inspect the queue"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rollout := filepath.Join(sessions, "rollout-2026-09-28T11-44-55-"+id+".jsonl")
	if err := os.WriteFile(rollout, []byte(
		`{"type":"session_meta","payload":{"id":"`+id+`","session_id":"`+id+`","cwd":"/code/demo"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(locks, id+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN) //nolint:errcheck -- the temporary file is closing too

	h := NewHost()
	h.Home = home
	h.Refresh()
	got, ok := h.ForSession(context.Background(), session.Session{
		ID: "ITERM-GUID", TTY: "ttys005", Backend: session.BackendITerm,
		PID: 42000, Assistant: session.AssistantCodex, CWD: "/code/demo",
		Label: "Inspect the queue | demo (codex)", Binding: session.BindingNoRecord,
	})
	if !ok || got.ConversationID != id {
		t.Fatalf("identity = %+v, ok %v; want the managed conversation", got, ok)
	}
	if got.Binding != session.Binding("live_title") {
		t.Fatalf("binding = %q, want live_title", got.Binding)
	}
}

func TestAStaleCodexWriterLockDoesNotBindATerminal(t *testing.T) {
	home := t.TempDir()
	id := "c0de0002-0000-4000-8000-000000000002"
	codex := filepath.Join(home, ".codex")
	locks := filepath.Join(codex, "thread-writer-locks")
	sessions := filepath.Join(codex, "sessions", "2026", "09", "28")
	if err := os.MkdirAll(locks, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codex, "session_index.jsonl"), []byte(
		`{"id":"`+id+`","thread_name":"Old title"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessions, "rollout-2026-09-28T11-44-55-"+id+".jsonl"), []byte(
		`{"type":"session_meta","payload":{"id":"`+id+`","session_id":"`+id+`","cwd":"/code/demo"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The file exists after a crash but nobody holds its lock.
	if err := os.WriteFile(filepath.Join(locks, id+".lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	h := NewHost()
	h.Home = home
	h.Refresh()
	_, ok := h.ForSession(context.Background(), session.Session{
		Backend: session.BackendITerm, Assistant: session.AssistantCodex,
		CWD: "/code/demo", Label: "Old title | demo (codex)",
	})
	if ok {
		t.Fatal("a stale unlocked writer file bound a live terminal")
	}
}
