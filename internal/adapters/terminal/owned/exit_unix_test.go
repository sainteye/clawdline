//go:build !windows

package owned

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/terminal"
)

// logged is a Server's log, kept for a test.
type logged struct {
	mu    sync.Mutex
	lines []string
}

func (l *logged) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logged) about(id terminal.ID) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range l.lines {
		if strings.Contains(line, string(id)) {
			out = append(out, line)
		}
	}
	return out
}

var shellPIDLine = regexp.MustCompile(`^pid=([0-9]+)$`)

// shellPID is the pid of the shell itself, as the shell says it.
func shellPID(t *testing.T, s *Server, id terminal.ID) int {
	t.Helper()
	keys(t, s, id, "echo pid=$$\r")
	var pid int
	waitFrame(t, s, id, "the shell's pid", func(f terminal.Frame) bool {
		for _, line := range plain(f) {
			if m := shellPIDLine.FindStringSubmatch(line); m != nil {
				pid, _ = strconv.Atoi(m[1])
				return true
			}
		}
		return false
	})
	return pid
}

// gone waits until the terminal reads as closed, the way a viewer finds out.
func gone(t *testing.T, s *Server, id terminal.ID) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := s.Frame(context.Background(), id); isCode(err, terminal.CodeClosed) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s never closed", id)
}

// A shell that ends by itself leaves one line in the daemon's log, with the
// terminal's id and the status the shell ended with, so `exit` and a kill
// can be told apart afterwards; one the daemon closes leaves none. Nothing
// typed and nothing on the screen is in the line.
func TestAShellThatEndsByItselfLeavesOneLogLine(t *testing.T) {
	s := newServer(t, testDir(t))
	var log logged
	s.logf = log.logf
	ctx := context.Background()

	exited := open(t, s, 80, 24)
	keys(t, s, exited.ID, "echo secret-typed-words\r")
	waitFrame(t, s, exited.ID, "the echo", func(f terminal.Frame) bool { return hasLine(f, "secret-typed-words") })
	keys(t, s, exited.ID, "exit 3\r")
	gone(t, s, exited.ID)

	killed := open(t, s, 80, 24)
	pid := shellPID(t, s, killed.ID)
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	gone(t, s, killed.ID)

	closed := open(t, s, 80, 24)
	shellPID(t, s, closed.ID)
	server, err := raw(t, s, "display-message", "-p", "#{pid}")
	if err != nil {
		t.Fatal(err)
	}
	serverPID, err := strconv.Atoi(server)
	if err != nil || serverPID <= 0 {
		t.Fatalf("invalid owned server PID %q: %v", server, err)
	}
	if err := s.Close(ctx, closed.ID); err != nil {
		t.Fatal(err)
	}
	// Closing the last session can overlap the private server's exit. Wait
	// for this captured server to be gone, so List tests stable absence
	// rather than racing its closing connection.
	// Never retry the read or classify an unreachable server as empty.
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := syscall.Kill(serverPID, 0)
		if errors.Is(err, syscall.ESRCH) {
			break
		}
		if err != nil {
			t.Fatalf("owned server liveness: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("owned server %d did not exit after its last session closed", serverPID)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := s.List(ctx); err != nil {
		t.Fatal(err)
	}

	lines := log.about(exited.ID)
	if len(lines) != 1 || !strings.Contains(lines[0], "status 3") {
		t.Fatalf("exit 3 logged %q", lines)
	}
	if strings.Contains(lines[0], "secret") {
		t.Fatalf("the line carries what was typed: %q", lines[0])
	}
	lines = log.about(killed.ID)
	if len(lines) != 1 || !strings.Contains(lines[0], "status 137") || !strings.Contains(lines[0], "signal 9") {
		t.Fatalf("kill -9 logged %q", lines)
	}
	if lines := log.about(closed.ID); len(lines) != 0 {
		t.Fatalf("a terminal the daemon closed logged %q", lines)
	}
	// Read again, nothing more: the record is taken when it is logged.
	if _, err := s.List(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(log.about(exited.ID)) + len(log.about(killed.ID)); n != 2 {
		t.Fatalf("%d lines after a second read; want 2", n)
	}
	t.Logf("%s", strings.Join(append(log.about(exited.ID), log.about(killed.ID)...), "\n"))
}
