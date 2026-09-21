//go:build darwin

package terminal

import (
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// What the kernel says is in front of a terminal (tty_darwin.go), as the
// close a person presses reads it.

var (
	shellStart  = time.Unix(900, 0)
	claudeStart = time.Unix(1000, 0)
	loginProcs  = []ttyProc{
		{PID: 100, PPID: 1, PGID: 100, Comm: "login", Start: shellStart},
		{PID: 200, PPID: 100, PGID: 200, Comm: "-zsh", Start: shellStart},
	}
)

func withJob(job ...ttyProc) []ttyProc { return append(append([]ttyProc{}, loginProcs...), job...) }

// A tab whose shell is at its prompt holds nothing, and that is the one state
// a close may act on without asking anybody anything.
func TestAShellAtItsPromptHoldsNothing(t *testing.T) {
	if got := ttySightOf(loginProcs, 200, session.Session{}); got.Job || got.Assistant != "" {
		t.Fatalf("shell at its prompt: %+v", got)
	}
}

// An assistant in front is named, and the group to signal is the tty's
// foreground group, so the helpers it runs beside itself leave with it.
func TestAnAssistantInFrontIsNamedWithItsWholeGroup(t *testing.T) {
	procs := withJob(
		ttyProc{PID: 400, PPID: 200, PGID: 400, Comm: "claude", Start: claudeStart},
		ttyProc{PID: 401, PPID: 400, PGID: 400, Comm: "node", Start: claudeStart},
	)
	got := ttySightOf(procs, 400, session.Session{PID: 400, Assistant: session.AssistantClaude})
	if !got.Job || got.Assistant != session.AssistantClaude || got.PID != 400 || got.Group != 400 ||
		!got.Start.Equal(claudeStart) {
		t.Fatalf("claude in front: %+v", got)
	}
}

// **An assistant started through a wrapper reads as the wrapper**, because the
// kernel's short name is sixteen characters with no arguments. The row's own
// pid is the better evidence and wins: the process table classified it from a
// whole command line.
func TestTheRowsOwnPidNamesAnAssistantTheKernelsShortNameDoesNot(t *testing.T) {
	procs := withJob(ttyProc{PID: 400, PPID: 200, PGID: 400, Comm: "node", Start: claudeStart})
	got := ttySightOf(procs, 400, session.Session{PID: 400, Assistant: session.AssistantClaude})
	if got.Assistant != session.AssistantClaude || got.PID != 400 {
		t.Fatalf("a wrapped assistant: %+v", got)
	}
	// With no pid to go on, a name nothing recognises is not an assistant, and
	// the ladder will not type into it or signal it.
	if got := ttySightOf(procs, 400, session.Session{Assistant: session.AssistantClaude}); got.Assistant != "" {
		t.Fatalf("a name nothing recognises: %+v", got)
	}
}

// A command somebody started is a job and is not an assistant: the close will
// neither type into it nor signal it.
func TestACommandSomebodyStartedIsAJobAndNotAnAssistant(t *testing.T) {
	procs := withJob(ttyProc{PID: 700, PPID: 200, PGID: 700, Comm: "vim", Start: claudeStart})
	got := ttySightOf(procs, 700, session.Session{PID: 400, Assistant: session.AssistantClaude})
	if !got.Job || got.Assistant != "" {
		t.Fatalf("vim in front: %+v", got)
	}
	if _, why := ours(session.Session{PID: 400, Assistant: session.AssistantClaude}, got); why == "" {
		t.Fatal("a command somebody started was taken for the assistant")
	}
}

// The identity the escalation is held to is the process the close was asked
// about, even when the group's leader is something else.
func TestTheIdentityIsTheProcessTheCloseWasAskedAbout(t *testing.T) {
	procs := withJob(
		ttyProc{PID: 500, PPID: 200, PGID: 500, Comm: "zsh", Start: shellStart},
		ttyProc{PID: 501, PPID: 500, PGID: 500, Comm: "claude", Start: claudeStart},
	)
	got := ttySightOf(procs, 500, session.Session{PID: 501, Assistant: session.AssistantClaude})
	if got.PID != 501 || !got.Start.Equal(claudeStart) || got.Group != 500 {
		t.Fatalf("identity: %+v", got)
	}
}
