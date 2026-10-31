//go:build darwin || linux

package terminal

import (
	"github.com/sainteye/clawdline/internal/domain/session"
	"testing"
	"time"
)

func TestNativeTerminalIdentityRequiresTheLiveForegroundAssistant(t *testing.T) {
	start := time.Unix(100, 0)
	s := session.Session{ID: "%1", Backend: session.BackendTmux, Assistant: session.AssistantClaude, PID: 22, ExpectedStart: start}
	processes := []ttyProc{{PID: 1, PGID: 1, Comm: "zsh", Start: start}, {PID: 22, PPID: 1, PGID: 22, Comm: "claude", Start: start}}
	identity, err := nativeTerminalIdentityOf(s, processes, 22)
	if err != nil || identity == "" {
		t.Fatalf("%s %v", identity, err)
	}
	if _, err := nativeTerminalIdentityOf(s, processes[1:], 22); err != nil {
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
			if _, err := nativeTerminalIdentityOf(s, next, 22); err == nil {
				t.Fatal("accepted a replacement foreground process")
			}
		})
	}
}
