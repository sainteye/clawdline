//go:build darwin

package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// Closing a child's iTerm2 tab (iterm_close_darwin.go). Measured on this Mac
// before the change: a close of a tab with a job running put iTerm2's "Close
// tab? This tab is running …" on the person's screen, ran past its ten-second
// limit, and was reported as a failure — while the tab closed later, when
// somebody answered.

// The look finds the session past a window that will not list, with its tty;
// a session it does not find past such a window is not seen, and only a
// complete walk says gone.
func TestTheITermFindSaysThereNotSeenOrGone(t *testing.T) {
	run := runInModel(t, itermFindScript, []string{"GUID-A"})
	if run.Answer["found"] != true || run.Answer["tty"] != "/dev/ttys031" || len(run.Writes) != 0 {
		t.Fatalf("find: %+v", run)
	}
	for _, c := range []struct {
		id       string
		complete bool
		want     sighting
		tty      string
	}{
		{"GUID-A", false, sightingThere, "/dev/ttys031"},
		{"GUID-GONE", false, sightingUnknown, ""},
		{"GUID-GONE", true, sightingGone, ""},
	} {
		run := runInModelWith(t, itermFindScript, []string{c.id}, c.complete)
		raw, _ := json.Marshal(run.Answer)
		if got, tty := readSighting(raw); got != c.want || tty != c.tty {
			t.Errorf("%s (complete %v): %v %q, want %v %q", c.id, c.complete, got, tty, c.want, c.tty)
		}
	}
	if got, _ := readSighting([]byte(`{"running":false,"found":false,"unreadable":0}`)); got != sightingGone {
		t.Errorf("an iTerm2 that is not running: %v", got)
	}
	if got, _ := readSighting([]byte(`not json`)); got != sightingUnknown {
		t.Errorf("an unreadable answer: %v", got)
	}
}

// A close iTerm2 did not answer is looked for again. Gone is a close; still
// there, or not seen, is Unconfirmed — never a failure to be retried and
// never a success.
func TestAnUnansweredCloseIsLookedForAndNotCountedUntilGone(t *testing.T) {
	asked := Failure{Attention: true, Message: "iTerm2 did not answer in time."}
	script := func(seen ...sighting) (func(context.Context, string) sighting, *int) {
		n := 0
		return func(context.Context, string) sighting {
			s := seen[len(seen)-1]
			if n < len(seen) {
				s = seen[n]
			}
			n++
			return s
		}, &n
	}
	look, n := script(sightingThere, sightingGone)
	if err := settleUnansweredClose(context.Background(), "GUID-A", asked, look, 3, time.Millisecond); err != nil || *n != 2 {
		t.Fatalf("gone on the second look: %v after %d looks", err, *n)
	}
	var unconfirmed Unconfirmed
	look, n = script(sightingThere)
	err := settleUnansweredClose(context.Background(), "GUID-A", asked, look, 3, time.Millisecond)
	if !errors.As(err, &unconfirmed) || !strings.Contains(err.Error(), "still open") || *n != 3 {
		t.Fatalf("still there: %T %v after %d looks", err, err, *n)
	}
	look, _ = script(sightingUnknown)
	err = settleUnansweredClose(context.Background(), "GUID-A", asked, look, 3, time.Millisecond)
	if !errors.As(err, &unconfirmed) || !strings.Contains(err.Error(), "could not be seen") {
		t.Fatalf("not seen: %T %v", err, err)
	}
	var failure Failure
	if errors.As(err, &failure) {
		t.Fatal("an unconfirmed close reads as a failure, which a caller retries")
	}
}

// fakeTab is one iTerm2 tab and its tty as the ladder sees them. Each call to
// processes answers the next reading, the last one for ever.
type fakeTab struct {
	seen     sighting
	readings [][]ttyProc
	fg       []int
	gone     bool
	calls    []string
	closeErr error
}

func (f *fakeTab) ender() itermEnder {
	n := 0
	return itermEnder{
		find: func(context.Context, string) (sighting, string) {
			f.calls = append(f.calls, "find")
			if f.seen == sightingThere {
				return f.seen, "/dev/ttys031"
			}
			return f.seen, ""
		},
		processes: func(string) ([]ttyProc, int, error) {
			f.calls = append(f.calls, "processes")
			i := n
			if i >= len(f.readings) {
				i = len(f.readings) - 1
			}
			n++
			return f.readings[i], f.fg[i], nil
		},
		signal: func(pgid int) error {
			f.calls = append(f.calls, "term")
			return nil
		},
		gone:  func(int) bool { return f.gone },
		close: func(context.Context, string) error { f.calls = append(f.calls, "close"); return f.closeErr },
		wait:  20 * time.Millisecond,
		tick:  time.Millisecond,
	}
}

var ended = time.Date(2026, 9, 19, 6, 0, 0, 0, time.UTC)

// The tty as iTerm2's default profile makes it: login, the shell, and — while
// the child runs — the assistant in front.
func childTTY(front string, started time.Time) ([]ttyProc, int) {
	procs := []ttyProc{
		{PID: 100, PPID: 1, PGID: 100, Comm: "login", Start: ended.Add(-time.Hour)},
		{PID: 101, PPID: 100, PGID: 101, Comm: "zsh", Start: ended.Add(-time.Hour)},
	}
	if front == "" {
		return procs, 101
	}
	procs = append(procs, ttyProc{PID: 102, PPID: 101, PGID: 102, Comm: front, Start: started})
	return procs, 102
}

func (f *fakeTab) reading(front string, started time.Time) {
	procs, fg := childTTY(front, started)
	f.readings = append(f.readings, procs)
	f.fg = append(f.fg, fg)
}

func (f *fakeTab) said() string { return strings.Join(f.calls, ",") }

// The child still running in its tab is ended first — it is the job iTerm2
// would ask a person about — and the tab closed once the shell is in front.
func TestAChildsJobIsEndedBeforeItsTabIsClosed(t *testing.T) {
	f := &fakeTab{seen: sightingThere, gone: true}
	f.reading("claude", ended.Add(-30*time.Minute))
	f.reading("", time.Time{})
	if err := f.ender().end(context.Background(), "GUID-A", ended); err != nil {
		t.Fatal(err)
	}
	if f.said() != "find,processes,term,processes,close" {
		t.Fatalf("the ladder went %s", f.said())
	}
}

// A shell at its prompt has nothing to end: the tab is closed as it is.
func TestATabAtItsShellPromptIsClosedWithoutASignal(t *testing.T) {
	f := &fakeTab{seen: sightingThere}
	f.reading("", time.Time{})
	if err := f.ender().end(context.Background(), "GUID-A", ended); err != nil || f.said() != "find,processes,close" {
		t.Fatalf("%v: %s", err, f.said())
	}
}

// A job that began after the task ended is somebody else's — a person who went
// on in the tab — and is never signalled; neither is one whose start the
// kernel would not give, nor any job when the task's end is not known.
func TestAJobThatIsNotProvablyTheChildsKeepsItsTab(t *testing.T) {
	for name, c := range map[string]struct {
		started time.Time
		ended   time.Time
	}{
		"started after the end":       {ended.Add(time.Minute), ended},
		"started in the last second":  {ended.Add(-500 * time.Millisecond), ended},
		"no start time":               {time.Time{}, ended},
		"the task's end is not known": {ended.Add(-time.Hour), time.Time{}},
	} {
		f := &fakeTab{seen: sightingThere, gone: true}
		f.reading("vim", c.started)
		err := f.ender().end(context.Background(), "GUID-A", c.ended)
		var failure Failure
		if !errors.As(err, &failure) || !strings.Contains(err.Error(), "left open") {
			t.Errorf("%s: %T %v", name, err, err)
		}
		if strings.Contains(f.said(), "term") || strings.Contains(f.said(), "close") {
			t.Errorf("%s: the ladder went %s", name, f.said())
		}
	}
}

// A child that does not leave when asked keeps its tab: closing it then would
// put iTerm2's question on the person's screen.
func TestAChildThatDoesNotLeaveKeepsItsTab(t *testing.T) {
	f := &fakeTab{seen: sightingThere, gone: false}
	f.reading("claude", ended.Add(-time.Hour))
	err := f.ender().end(context.Background(), "GUID-A", ended)
	if err == nil || !strings.Contains(err.Error(), "did not leave") || strings.Contains(f.said(), "close") {
		t.Fatalf("%v: %s", err, f.said())
	}
}

// Something that comes to the front while the child leaves is not the child's
// either, and the tab is left open.
func TestAJobThatComesUpAsTheChildLeavesKeepsTheTab(t *testing.T) {
	f := &fakeTab{seen: sightingThere, gone: true}
	f.reading("claude", ended.Add(-time.Hour))
	f.reading("vim", ended.Add(time.Minute))
	err := f.ender().end(context.Background(), "GUID-A", ended)
	if err == nil || !strings.Contains(err.Error(), "came to the front") || strings.Contains(f.said(), "close") {
		t.Fatalf("%v: %s", err, f.said())
	}
}

// A session that is gone, or not seen, is Unsent: nothing on its tty is read,
// signalled or closed.
func TestASessionThatIsNotThereIsNotTouched(t *testing.T) {
	for _, seen := range []sighting{sightingGone, sightingUnknown} {
		f := &fakeTab{seen: seen}
		f.reading("claude", ended.Add(-time.Hour))
		err := f.ender().end(context.Background(), "GUID-A", ended)
		var unsent Unsent
		if !errors.As(err, &unsent) || f.said() != "find" {
			t.Errorf("sighting %v: %T %v, %s", seen, err, err, f.said())
		}
	}
}

// Which group in front of a tty is a job. The shell — the root of the tty's
// tree, or login's child — is not; the assistant, a command, or a shell
// started inside the shell is.
func TestWhatIsInFrontOfATTYIsAJob(t *testing.T) {
	at := ended.Add(-time.Hour)
	login := ttyProc{PID: 100, PPID: 1, PGID: 100, Comm: "login", Start: at}
	zsh := ttyProc{PID: 101, PPID: 100, PGID: 101, Comm: "zsh", Start: at}
	job := ttyProc{PID: 102, PPID: 101, PGID: 102, Comm: "claude", Start: at}
	bare := ttyProc{PID: 200, PPID: 1, PGID: 200, Comm: "zsh", Start: at}
	under := ttyProc{PID: 201, PPID: 200, PGID: 201, Comm: "sleep", Start: at}
	nested := ttyProc{PID: 103, PPID: 101, PGID: 103, Comm: "zsh", Start: at}
	for name, c := range map[string]struct {
		procs []ttyProc
		fg    int
		job   bool
	}{
		"login's shell at its prompt":  {[]ttyProc{login, zsh}, 101, false},
		"the assistant in front":       {[]ttyProc{login, zsh, job}, 102, true},
		"a shell iTerm2 ran directly":  {[]ttyProc{bare}, 200, false},
		"a command under that shell":   {[]ttyProc{bare, under}, 201, true},
		"a shell started in the shell": {[]ttyProc{login, zsh, nested}, 103, true},
		"a leader that has left":       {[]ttyProc{login, zsh, {PID: 104, PPID: 1, PGID: 105, Comm: "x"}}, 105, true},
	} {
		if got := len(foregroundJob(c.procs, c.fg)) > 0; got != c.job {
			t.Errorf("%s: job %v, want %v", name, got, c.job)
		}
	}
}
