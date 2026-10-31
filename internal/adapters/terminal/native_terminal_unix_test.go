//go:build darwin || linux

package terminal

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestNativeTerminalIdentityRequiresTheLiveForegroundAssistant(t *testing.T) {
	start := time.Unix(100, 0)
	s := session.Session{ID: "%1", Backend: session.BackendTmux, Assistant: session.AssistantClaude, PID: 22, ExpectedStart: start}
	processes := []ttyProc{{PID: 1, PGID: 1, Comm: "zsh", Start: start}, {PID: 22, PPID: 1, PGID: 22, Comm: "claude", Start: start}}
	identity, err := nativeTerminalIdentityOf(s, processes, 22, nil)
	if err != nil || identity == "" {
		t.Fatalf("%s %v", identity, err)
	}
	if _, err := nativeTerminalIdentityOf(s, processes[1:], 22, nil); err != nil {
		t.Fatalf("a CLI rooted directly in the tty: %v", err)
	}
	for _, change := range []string{"shell", "different-assistant", "new-birth", "different-pid", "missing-birth"} {
		t.Run(change, func(t *testing.T) {
			next := append([]ttyProc(nil), processes...)
			switch change {
			case "shell":
				next[1].Comm = "zsh"
			case "different-assistant":
				next[1].Comm = "codex"
			case "new-birth":
				next[1].Start = start.Add(time.Second)
			case "different-pid":
				next[1].PID = 23
			case "missing-birth":
				next[1].Start = time.Time{}
			}
			if _, err := nativeTerminalIdentityOf(s, next, 22, nil); err == nil {
				t.Fatal("accepted a replacement foreground process")
			}
		})
	}
}

func TestNativeTerminalRecognizesTheVersionedClaudeExecutable(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Unix(100, 0)
	s := session.Session{ID: "%1", Backend: session.BackendTmux, Assistant: session.AssistantClaude, PID: 22, ExpectedStart: start}
	procs := []ttyProc{{PID: 22, PGID: 22, Comm: "2.1.314", Start: start}}
	for _, tc := range []struct {
		name, image      string
		denied, accepted bool
	}{
		{name: "standard native installation", image: filepath.Join(home, ".local", "share", "claude", "versions", "2.1.314"), accepted: true},
		{name: "another install root", image: "/synthetic/versions/2.1.314"},
		{name: "different version", image: filepath.Join(home, ".local", "share", "claude", "versions", "2.1.315")},
		{name: "a shell", image: "/bin/zsh"},
		{name: "unreadable image", denied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity, err := nativeTerminalIdentityOf(s, procs, 22, func(pid int) (string, error) {
				if pid != 22 {
					t.Fatalf("looked up another PID: %d", pid)
				}
				if tc.denied {
					return "", errors.New("denied")
				}
				return tc.image, nil
			})
			if tc.accepted && (err != nil || identity == "") {
				t.Fatalf("native Claude was refused: %v", err)
			}
			if !tc.accepted && err == nil {
				t.Fatal("accepted an unverified versioned executable")
			}
		})
	}
}

func TestNativeTerminalExecutableReadsTheRunningImage(t *testing.T) {
	want, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want, err = filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := nativeTerminalExecutable(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	got, err = filepath.EvalSymlinks(got)
	if err != nil || got != want {
		t.Fatalf("kernel image %q, want %q: %v", got, want, err)
	}
	if _, err = nativeTerminalExecutable(-1); err == nil {
		t.Fatal("accepted a missing process")
	}
}
