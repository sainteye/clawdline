package terminal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// The close a person presses (farewell.go). What it is checked against is the
// order — the assistant's own word, the wait, and only then the terminal —
// and the list of things it is not allowed to signal.

// fakeTerminal is one session as the ladder sees it. Each look answers the
// next reading, the last one for ever, and the clock moves only when it is
// looked at, so a test's time is the number of looks it took.
type fakeTerminal struct {
	sights   []farewellSight
	lookErr  error
	sendErr  error
	closeErr error
	sigErr   error
	noSignal bool

	calls []string
	clock time.Duration
	step  time.Duration
	looks int
}

func (f *fakeTerminal) ladder() farewell {
	l := farewell{
		look: func(context.Context, session.Session) (farewellSight, error) {
			f.calls = append(f.calls, "look")
			if f.lookErr != nil {
				return farewellSight{}, f.lookErr
			}
			i := min(f.looks, len(f.sights)-1)
			f.looks++
			f.clock += f.step
			return f.sights[i], nil
		},
		send: func(_ context.Context, _ session.Session, line string) error {
			f.calls = append(f.calls, "send "+line)
			return f.sendErr
		},
		close: func(context.Context, session.Session) error {
			f.calls = append(f.calls, "close")
			return f.closeErr
		},
		polite: 3 * time.Second, afterTerm: 2 * time.Second, afterKill: time.Second,
		tick: time.Millisecond,
		now:  func() time.Time { return time.Unix(0, 0).Add(f.clock) },
	}
	if !f.noSignal {
		l.signal = func(group int, step escalation) error {
			f.calls = append(f.calls, step.String())
			return f.sigErr
		}
	}
	return l
}

func claudeAt(pid int) farewellSight {
	return farewellSight{Job: true, Assistant: session.AssistantClaude,
		PID: pid, Start: time.Unix(1000, 0), Group: pid}
}

var claudeRow = session.Session{ID: "GUID-A", Backend: session.BackendITerm,
	TTY: "ttys031", PID: 400, Assistant: session.AssistantClaude}

// Each assistant leaves on its own word, and a row that is not an assistant
// has none.
func TestEachAssistantHasItsOwnQuitWord(t *testing.T) {
	for _, c := range []struct {
		assistant session.Assistant
		want      string
		has       bool
	}{
		{session.AssistantClaude, "/exit", true},
		{session.AssistantCodex, "/quit", true},
		{"", "", false},
		{"gemini", "", false},
	} {
		if got, ok := QuitLine(c.assistant); got != c.want || ok != c.has {
			t.Errorf("%q: %q %v, want %q %v", c.assistant, got, ok, c.want, c.has)
		}
	}
}

// The order, which is the whole of it: the word first, the terminal after it
// has left. Nothing is signalled when the polite word is enough.
func TestTheWordComesBeforeTheTerminalIsTaken(t *testing.T) {
	f := &fakeTerminal{sights: []farewellSight{claudeAt(400), {}}, step: time.Second}
	if err := f.ladder().say(context.Background(), claudeRow); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := strings.Join(f.calls, ","); got != "look,send /exit,look,close" {
		t.Fatalf("order: %s", got)
	}
}

// Codex is sent Codex's word. Each assistant refuses the other's, so this is
// not a detail.
func TestCodexIsSentItsOwnWord(t *testing.T) {
	f := &fakeTerminal{step: time.Second, sights: []farewellSight{
		{Job: true, Assistant: session.AssistantCodex, PID: 7, Start: time.Unix(1000, 0), Group: 7}, {}}}
	row := session.Session{ID: "%3", Backend: session.BackendTmux, PID: 7, Assistant: session.AssistantCodex}
	if err := f.ladder().say(context.Background(), row); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := strings.Join(f.calls, ","); got != "look,send /quit,look,close" {
		t.Fatalf("order: %s", got)
	}
}

// A terminal with nothing in front of it is closed at once and typed into not
// at all. This is the ordinary tab, and iTerm2 answers it in 0.15 s without
// asking anybody anything.
func TestAShellAtItsPromptIsClosedWithNothingTyped(t *testing.T) {
	f := &fakeTerminal{sights: []farewellSight{{}}, step: time.Second}
	if err := f.ladder().say(context.Background(), claudeRow); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := strings.Join(f.calls, ","); got != "look,close" {
		t.Fatalf("order: %s", got)
	}
}

// A session the backend no longer has closed nothing, and that is not a
// failure: the reading the caller acted on is one moment behind the machine.
func TestASessionThatIsGoneClosesNothing(t *testing.T) {
	f := &fakeTerminal{sights: []farewellSight{{Gone: true}}, step: time.Second}
	err := f.ladder().say(context.Background(), claudeRow)
	var unsent Unsent
	if !errors.As(err, &unsent) {
		t.Fatalf("gone: %T %v", err, err)
	}
	if got := strings.Join(f.calls, ","); got != "look" {
		t.Fatalf("order: %s", got)
	}
}

// **The list of things this close may not end.** None of them is typed into
// and none of them is signalled; the close is still asked for, because taking
// a tab away is what the person meant, and whatever the terminal says about
// it is reported as it is.
func TestWhatIsNotOursIsNeverTypedIntoOrSignalled(t *testing.T) {
	for _, c := range []struct {
		name  string
		row   session.Session
		sight farewellSight
	}{
		{"a row that is not an assistant",
			session.Session{ID: "GUID-A", TTY: "ttys031"},
			farewellSight{Job: true, PID: 400, Group: 400}},
		{"a command somebody started",
			claudeRow,
			farewellSight{Job: true, PID: 900, Group: 900}},
		{"a different assistant in that terminal now",
			claudeRow,
			farewellSight{Job: true, Assistant: session.AssistantCodex, PID: 400, Group: 400}},
		{"a process newer than the reading this close is acting on",
			claudeRow,
			farewellSight{Job: true, Assistant: session.AssistantClaude, PID: 901, Group: 901}},
	} {
		f := &fakeTerminal{sights: []farewellSight{c.sight}, step: time.Second}
		if err := f.ladder().say(context.Background(), c.row); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := strings.Join(f.calls, ","); got != "look,close" {
			t.Fatalf("%s: %s", c.name, got)
		}
	}
}

// An assistant that has not left when the polite wait is up is asked by the
// kernel, then told, and then the terminal is left open. Elapsed time is never
// evidence of absence.
func TestAnAssistantThatWillNotLeaveIsAskedThenToldThenLeftAlone(t *testing.T) {
	f := &fakeTerminal{sights: []farewellSight{claudeAt(400)}, step: time.Second}
	err := f.ladder().say(context.Background(), claudeRow)
	var running StillRunning
	if !errors.As(err, &running) {
		t.Fatalf("still running: %T %v", err, err)
	}
	got := strings.Join(f.calls, ",")
	want := "look,send /exit,look,look,look,SIGTERM,look,look,SIGKILL,look"
	if got != want {
		t.Fatalf("rungs:\n got %s\nwant %s", got, want)
	}
	if strings.Contains(got, "close") {
		t.Fatal("the terminal was taken with the assistant still in it")
	}
}

// A machine that cannot read which process it is has no rung past the word.
// It says so rather than signalling something it cannot name.
func TestWithoutAProcessToNameTheLadderStopsAtTheWord(t *testing.T) {
	f := &fakeTerminal{noSignal: true, step: time.Second, sights: []farewellSight{
		{Job: true, Assistant: session.AssistantClaude}}}
	err := f.ladder().say(context.Background(), claudeRow)
	var running StillRunning
	if !errors.As(err, &running) || !strings.Contains(err.Error(), "cannot read which process") {
		t.Fatalf("no signal: %T %v", err, err)
	}
	if strings.Contains(strings.Join(f.calls, ","), "close") {
		t.Fatal("the terminal was taken with the assistant still in it")
	}
}

// The quit word that could not be typed, on a machine with nothing to signal,
// is its own refusal: the session is still running and nothing was taken.
func TestAWordThatWouldNotBeTypedIsItsOwnRefusal(t *testing.T) {
	f := &fakeTerminal{noSignal: true, sendErr: Unsent{Why: "iTerm2 is not running"},
		step: time.Second, sights: []farewellSight{{Job: true, Assistant: session.AssistantClaude}}}
	err := f.ladder().say(context.Background(), claudeRow)
	var refused QuitRefused
	if !errors.As(err, &refused) || !strings.Contains(err.Error(), "/exit") {
		t.Fatalf("quit refused: %T %v", err, err)
	}
	if got := strings.Join(f.calls, ","); got != "look,send /exit" {
		t.Fatalf("order: %s", got)
	}
}

// A word that would not be typed is not a reason to leave a session running
// when the process itself can still be asked.
func TestAWordThatWouldNotBeTypedStillReachesTheSignalRungs(t *testing.T) {
	f := &fakeTerminal{sendErr: Unsent{Why: "the composer never showed it"}, step: time.Second,
		sights: []farewellSight{claudeAt(400), claudeAt(400), claudeAt(400), claudeAt(400), {}}}
	if err := f.ladder().say(context.Background(), claudeRow); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := strings.Join(f.calls, ","); !strings.Contains(got, "SIGTERM") || !strings.HasSuffix(got, "close") {
		t.Fatalf("rungs: %s", got)
	}
}

// Something that came to the front while the assistant left is not the
// assistant, and the terminal is left open rather than hung up under it.
func TestSomethingElseAtTheFrontLeavesTheTerminalOpen(t *testing.T) {
	f := &fakeTerminal{step: time.Second, sights: []farewellSight{
		claudeAt(400), {Job: true, PID: 901, Group: 901}}}
	err := f.ladder().say(context.Background(), claudeRow)
	var occupied Occupied
	if !errors.As(err, &occupied) {
		t.Fatalf("occupied: %T %v", err, err)
	}
	if strings.Contains(strings.Join(f.calls, ","), "close") {
		t.Fatal("the terminal was taken with something else in it")
	}
}

// A process that changed between two rungs never inherits the previous one's
// history: a pid that does not match is plainly different, and the same pid
// with a different start instant is pid reuse and is just as different.
func TestAProcessThatChangedBetweenRungsIsNotSignalled(t *testing.T) {
	reused := claudeAt(400)
	reused.Start = time.Unix(2000, 0)
	f := &fakeTerminal{step: time.Second, sights: []farewellSight{
		claudeAt(400), claudeAt(400), claudeAt(400), claudeAt(400), reused}}
	err := f.ladder().say(context.Background(), claudeRow)
	var occupied Occupied
	if !errors.As(err, &occupied) || !strings.Contains(err.Error(), "SIGKILL") {
		t.Fatalf("pid reuse: %T %v", err, err)
	}
	calls := strings.Join(f.calls, ",")
	if strings.Count(calls, "SIGTERM") != 1 || strings.Contains(calls, "SIGKILL,") || strings.Contains(calls, "close") {
		t.Fatalf("rungs: %s", calls)
	}
}

// A terminal that could not be read has proved nothing, and an unreadable
// terminal is not an empty one: nothing is typed, signalled or closed.
func TestAnUnreadableTerminalClosesNothing(t *testing.T) {
	f := &fakeTerminal{lookErr: errors.New("no process on /dev/ttys031")}
	err := f.ladder().say(context.Background(), claudeRow)
	var unreadable Unreadable
	if !errors.As(err, &unreadable) {
		t.Fatalf("unreadable: %T %v", err, err)
	}
	if got := strings.Join(f.calls, ","); got != "look" {
		t.Fatalf("order: %s", got)
	}
}

// A terminal that became unreadable after the word is still not closed: the
// ladder's last step rests on a reading, and it did not get one.
func TestATerminalThatBecomesUnreadableAfterTheWordIsLeftOpen(t *testing.T) {
	f := &fakeTerminal{step: time.Second, sights: []farewellSight{claudeAt(400)}}
	l := f.ladder()
	looked := 0
	inner := l.look
	l.look = func(ctx context.Context, s session.Session) (farewellSight, error) {
		looked++
		if looked > 1 {
			f.calls = append(f.calls, "look")
			return farewellSight{}, errors.New("the foreground group changed while it was read")
		}
		return inner(ctx, s)
	}
	err := l.say(context.Background(), claudeRow)
	var unreadable Unreadable
	if !errors.As(err, &unreadable) || !strings.Contains(err.Error(), "/exit") {
		t.Fatalf("unreadable after the word: %T %v", err, err)
	}
	if strings.Contains(strings.Join(f.calls, ","), "close") {
		t.Fatal("a terminal nobody could read was taken away")
	}
}

// The close the backend refused is the backend's own error, carried out as it
// is: the ladder decides what may be closed, never what a refusal means.
func TestTheBackendsOwnCloseRefusalIsCarriedOut(t *testing.T) {
	f := &fakeTerminal{sights: []farewellSight{{}}, closeErr: Unconfirmed{Attention: true, Why: "a sheet"}}
	err := f.ladder().say(context.Background(), claudeRow)
	var unconfirmed Unconfirmed
	if !errors.As(err, &unconfirmed) || !unconfirmed.Attention {
		t.Fatalf("close refusal: %T %v", err, err)
	}
}

// A process name is an assistant only when it is the program, as the process
// table's own classification spells it.
func TestAProcessNameIsAnAssistantOnlyWhenItIsTheProgram(t *testing.T) {
	for comm, want := range map[string]session.Assistant{
		"claude": session.AssistantClaude,
		"codex":  session.AssistantCodex,
		"node":   "",
		"zsh":    "",
		"":       "",
	} {
		if got := assistantOfComm(comm); got != want {
			t.Errorf("%q: %q, want %q", comm, got, want)
		}
	}
}
