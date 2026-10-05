//go:build darwin

package app

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// bareCodexTabs are two iTerm2 Codex tabs, each the only one in its
// directory showing the bare `<dir> (codex)` title. Every listing is
// counted, so a test can hold a second reading after its Refresh and before
// it observes its own rows.
type bareCodexTabs struct {
	*overlapHost
	mu      sync.Mutex
	calls   int
	second  chan struct{}
	release chan struct{}
}

func (h *bareCodexTabs) Inventory(context.Context) (session.Inventory, error) {
	h.mu.Lock()
	h.calls++
	hold := h.calls == 2 && h.second != nil
	h.mu.Unlock()
	if hold {
		close(h.second)
		<-h.release
	}
	return session.Inventory{Complete: true, Provenance: "iterm", Sessions: []session.Session{
		{ID: "ITERM-A", TTY: "ttys003", Backend: session.BackendITerm, PID: 42000,
			Assistant: session.AssistantCodex, CWD: "/code/demo", Label: "demo (codex)"},
		{ID: "ITERM-B", TTY: "ttys004", Backend: session.BackendITerm, PID: 42001,
			Assistant: session.AssistantCodex, CWD: "/code/other", Label: "⠸ other (codex)"},
	}}, nil
}

// pausingScreen holds the first capture of the first reading — which falls
// between its first row's identity and its second's — until another reading
// has refreshed the shared identity host and is held before observing rows.
type pausingScreen struct {
	once  sync.Once
	start func()
}

func (s *pausingScreen) Capture(context.Context, session.Session) (string, bool) {
	s.once.Do(s.start)
	return "", false
}

// unnamedCodexHome lays out one unnamed managed thread per cwd the way Codex
// 0.160 does: a rollout head, and a writer lock held until the test ends.
func unnamedCodexHome(t *testing.T, threads map[string]string) string {
	t.Helper()
	home := t.TempDir()
	codex := filepath.Join(home, ".codex")
	locks := filepath.Join(codex, "thread-writer-locks")
	sessions := filepath.Join(codex, "sessions", "2026", "10", "05")
	for _, dir := range []string{locks, sessions} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for id, cwd := range threads {
		if err := os.WriteFile(filepath.Join(sessions, "rollout-2026-10-05T11-44-55-"+id+".jsonl"), []byte(
			`{"type":"session_meta","payload":{"id":"`+id+`","session_id":"`+id+`","cwd":"`+cwd+`"}}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		lock, err := os.OpenFile(filepath.Join(locks, id+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { lock.Close() })
	}
	return home
}

const (
	demoThread  = "c0de000b-0000-4000-8000-00000000000b"
	otherThread = "c0de000c-0000-4000-8000-00000000000c"
)

// bothBound says whether a reading gave each bare tab its own unnamed thread.
func bothBound(inv session.Inventory) bool {
	want := map[string]string{"ITERM-A": demoThread, "ITERM-B": otherThread}
	if len(inv.Sessions) != len(want) {
		return false
	}
	for _, s := range inv.Sessions {
		if s.ConversationID != want[s.ID] || s.Binding != session.BindingLiveTitle {
			return false
		}
	}
	return true
}

// One reading's count of bare-title tabs is its own. On 1100a10e a second
// reading's Refresh, landing between the first reading's ObserveRows and the
// identity of its second row, cleared the count, and that row — alone in its
// directory — was left unbound ("conversation not started" on screen).
func TestAnotherReadingsRefreshDoesNotUnbindABareCodexTab(t *testing.T) {
	host := transcript.NewHost()
	host.Home = unnamedCodexHome(t, map[string]string{demoThread: "/code/demo", otherThread: "/code/other"})
	tabs := &bareCodexTabs{overlapHost: newOverlapHost(), second: make(chan struct{}), release: make(chan struct{})}
	screen := &pausingScreen{}
	in := Inventory{Identity: host, Terminals: []ports.TerminalHost{tabs}, Screen: screen}
	done := make(chan session.Inventory, 1)
	screen.start = func() {
		go func() { done <- in.Read(context.Background()) }()
		<-tabs.second
	}
	first := in.Read(context.Background())
	close(tabs.release)
	second := <-done
	if !bothBound(first) {
		t.Fatalf("the interrupted reading = %+v; want each bare tab bound to its own thread", first.Sessions)
	}
	if !bothBound(second) {
		t.Fatalf("the interrupting reading = %+v; want each bare tab bound to its own thread", second.Sessions)
	}
}

// The daemon reads its inventory from several places at once over one
// identity host (transport/http server.go). On 1100a10e a real machine bound
// a lone bare tab in 4 of 12 such reads, and `go test -race` reports the
// host's maps written by one reading's Refresh while another reads them.
func TestConcurrentReadsEachBindTheUnnamedCodexThread(t *testing.T) {
	host := transcript.NewHost()
	host.Home = unnamedCodexHome(t, map[string]string{demoThread: "/code/demo", otherThread: "/code/other"})
	in := Inventory{Identity: host, Terminals: []ports.TerminalHost{&bareCodexTabs{overlapHost: newOverlapHost()}}}

	const readers, reads = 6, 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	bound, total := 0, 0
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range reads {
				ok := bothBound(in.Read(context.Background()))
				mu.Lock()
				total++
				if ok {
					bound++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if bound != total {
		t.Fatalf("the bare tabs bound their unnamed threads in %d of %d concurrent reads", bound, total)
	}
}
