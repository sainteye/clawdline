//go:build darwin || linux

package terminal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// A session only the process table saw (process_close.go): an assistant on a
// tty no backend lists, closed by asking the process itself to leave.

var looseRow = session.Session{ID: "ttys011", Backend: session.BackendITerm,
	TTY: "ttys011", PID: 77548, Assistant: session.AssistantClaude}

// A tmux pane started straight into claude: the assistant is the root of its
// tty, its parent (the tmux server) is not on it, and there is no shell.
var paneRootedInClaude = []ttyProc{
	{PID: 77548, PPID: 77547, PGID: 77548, Comm: "2.1.283", Start: time.Unix(5000, 0)},
}

// The case this was written for: ttySightOf reads that tty as a shell at its
// prompt, which is nothing to end. The process sight finds the row's process
// in front, and gives the ladder a group to signal.
func TestAnAssistantThatIsTheRootOfItsTTYIsStillInFront(t *testing.T) {
	if got := ttySightOf(paneRootedInClaude, 77548, looseRow); got.Job {
		t.Fatalf("ttySightOf changed: it now sees a job on a tty rooted in claude (%+v)", got)
	}
	got := processSightOf(paneRootedInClaude, 77548, looseRow)
	want := farewellSight{Job: true, Assistant: session.AssistantClaude, PID: 77548,
		Start: time.Unix(5000, 0), Group: 77548}
	if got != want {
		t.Fatalf("processSightOf = %+v, want %+v", got, want)
	}
}

func TestAProcessNoLongerOnItsTTYIsGone(t *testing.T) {
	other := []ttyProc{{PID: 900, PPID: 1, PGID: 900, Comm: "zsh", Start: time.Unix(6000, 0)}}
	if got := processSightOf(other, 900, looseRow); !got.Gone {
		t.Fatalf("a tty without the row's pid = %+v, want gone", got)
	}
}

// Still on the tty but no longer in front: whatever is in front was brought
// there after the reading, and is not this close's to signal.
func TestAProcessPushedBehindSomethingElseIsNotOurs(t *testing.T) {
	procs := append([]ttyProc{}, paneRootedInClaude...)
	procs = append(procs, ttyProc{PID: 78000, PPID: 77548, PGID: 78000, Comm: "vim", Start: time.Unix(7000, 0)})
	sight := processSightOf(procs, 78000, looseRow)
	if _, why := ours(looseRow, sight); why == "" {
		t.Fatalf("a claude behind vim was read as ours to signal: %+v", sight)
	}
}

func TestARowWithNoPidIsNeverSignalled(t *testing.T) {
	row := looseRow
	row.PID = 0
	sight := processSightOf(paneRootedInClaude, 77548, row)
	if sight.Gone || sight.Group != 0 {
		t.Fatalf("a row with no pid = %+v, want present and unsignallable", sight)
	}
}

// The ladder: nothing typed, nothing closed, SIGTERM first, and done once the
// process has left.
func TestALooseSessionIsSignalledAndNotTypedInto(t *testing.T) {
	f := &fakeTerminal{sights: []farewellSight{claudeAt(77548), claudeAt(77548), {Gone: true}},
		step: 100 * time.Millisecond}
	l := f.ladder()
	l.polite = 0
	l.send = nil
	if err := l.leave(context.Background(), looseRow); err != nil {
		t.Fatalf("leave = %v", err)
	}
	want := []string{"look", "look", "SIGTERM", "look", "look"}
	if len(f.calls) < 3 || f.calls[2] != "SIGTERM" {
		t.Fatalf("calls = %v, want SIGTERM before anything else, like %v", f.calls, want)
	}
	for _, c := range f.calls {
		if c == "close" || len(c) > 4 && c[:4] == "send" {
			t.Fatalf("calls = %v: a loose session was typed into or closed", f.calls)
		}
	}
}

func TestALooseSessionThatWillNotLeaveIsKilledThenLeft(t *testing.T) {
	f := &fakeTerminal{sights: []farewellSight{claudeAt(77548)}, step: 500 * time.Millisecond}
	l := f.ladder()
	l.polite = 0
	err := l.leave(context.Background(), looseRow)
	var running StillRunning
	if !errors.As(err, &running) {
		t.Fatalf("leave = %v, want StillRunning", err)
	}
	var term, kill bool
	for _, c := range f.calls {
		term = term || c == "SIGTERM"
		kill = kill || c == "SIGKILL"
	}
	if !term || !kill {
		t.Fatalf("calls = %v, want SIGTERM and then SIGKILL", f.calls)
	}
}

func TestALooseSessionAlreadyGoneIsNothingThere(t *testing.T) {
	f := &fakeTerminal{sights: []farewellSight{{Gone: true}}}
	l := f.ladder()
	var unsent Unsent
	if err := l.leave(context.Background(), looseRow); !errors.As(err, &unsent) {
		t.Fatalf("leave = %v, want Unsent", err)
	}
}

func TestSomethingElseInFrontOfALooseSessionIsLeftAlone(t *testing.T) {
	f := &fakeTerminal{sights: []farewellSight{{Job: true, PID: 78000, Group: 78000}}}
	l := f.ladder()
	var occupied Occupied
	if err := l.leave(context.Background(), looseRow); !errors.As(err, &occupied) {
		t.Fatalf("leave = %v, want Occupied", err)
	}
	for _, c := range f.calls {
		if c == "SIGTERM" || c == "SIGKILL" {
			t.Fatalf("calls = %v: something that was not the row was signalled", f.calls)
		}
	}
}

// The claude that was the whole of its tmux pane takes the pane, the server
// and the pseudo-terminal with it when it leaves. The next look finds no
// device, and that is the close having worked.
func TestATTYThatNoLongerExistsIsGone(t *testing.T) {
	got, err := processSight("/dev/clawdline-no-such-tty-for-this-test", looseRow)
	if err != nil || !got.Gone {
		t.Fatalf("processSight of a missing tty = %+v, %v; want gone", got, err)
	}
}
