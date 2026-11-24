package orchestrator

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// A briefing that was typed and never submitted (stall.go, tendHeld).
//
// On 2026-11-24 the briefing reached Claude Code's composer, the
// Enter after it was lost while auto mode redrew the composer, and the task
// sat `spawning` with the whole briefing in the input line. The stall watch
// counted a composer with text in it as not idle, so it neither nudged nor
// reported. One Enter by hand started it.

// heldScreen is that tab: Claude Code's framed composer holding the briefing.
func heldScreen(id string) string {
	return "\n" + stallRule + "\n❯ You are a Clawdline CHILD agent for task " + id +
		". Say this line first, verbatim: … Then read /tmp/tasks/" + id + "/CHILD.md and follow it exactly. " +
		"TASK_SECRET=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n" + stallRule +
		"\n  ⏵⏵ auto mode on (shift+tab to cycle)\n"
}

// enterPresses is the rig's Enter-on-held-words, as SubmitHeld presses it.
type enterPresses struct {
	mu      sync.Mutex
	pressed []string // the typed words each press named
	after   func(n int)
	err     error
}

func (e *enterPresses) press(_ context.Context, terminalID, typed string) error {
	e.mu.Lock()
	e.pressed = append(e.pressed, typed)
	n, after, err := len(e.pressed), e.after, e.err
	e.mu.Unlock()
	if after != nil {
		after(n)
	}
	return err
}

func (e *enterPresses) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.pressed)
}

func newHeldRig(t *testing.T, id string) (*stallRig, *enterPresses) {
	t.Helper()
	x := newStallRig(t, id)
	presses := &enterPresses{}
	x.b.SubmitHeld = presses.press
	x.show(heldScreen(id))
	return x, presses
}

// Fault injection 1: the Enter was lost once. The beat presses Enter on the
// held briefing — never types it again — the child starts and signs.
func TestABriefingHeldInTheComposerIsPressedAndTheChildSigns(t *testing.T) {
	id := "b1e10001-0000-4000-8000-000000000001"
	x, presses := newHeldRig(t, id)
	presses.after = func(int) { x.show(readingScreen) }
	x.run(2 * time.Minute)
	if presses.count() != 1 {
		t.Fatalf("a briefing held in the composer was pressed %d times, want 1", presses.count())
	}
	if got := presses.pressed[0]; got != id {
		t.Errorf("the press named %q as the held words, want the task id %q", got, id)
	}
	if got := x.nudges(); len(got) != 0 {
		t.Fatalf("typed %q at a composer that held the briefing; only Enter may be pressed", got)
	}
	r := x.record()
	if r.Briefing == nil || r.Briefing.HeldSince.IsZero() || r.Briefing.Enters != 1 || r.Briefing.LastEnterAt.IsZero() {
		t.Fatalf("the held briefing and its press are not on the record: %+v", r.Briefing)
	}
	if _, err := x.b.Accept(x.ctx, x.id, "s"); err != nil {
		t.Fatal(err)
	}
	x.run(3 * stallIdleLimit)
	if r := x.record(); r.State != StateBriefed || r.Verdict != "" {
		t.Fatalf("the pressed child that signed is %q (%q)", r.State, r.Verdict)
	}
	if presses.count() != 1 {
		t.Fatalf("pressed %d times in all, want 1", presses.count())
	}
}

// Fault injection 2: every Enter is lost. The presses stop at their bound,
// and within the held limit the task is spawn_failed with a task_stalled
// notice for its root — never `spawning` until its timeout.
func TestABriefingThatNeverLeavesTheComposerEndsSpawnFailed(t *testing.T) {
	id := "b1e10002-0000-4000-8000-000000000002"
	x, presses := newHeldRig(t, id)
	x.run(time.Duration(briefHeldGraceSecondsLimit)*time.Second + 3*time.Minute)
	if presses.count() != briefEnterLimit {
		t.Fatalf("pressed %d times, want the bound %d", presses.count(), briefEnterLimit)
	}
	if got := x.nudges(); len(got) != 0 {
		t.Fatalf("typed %q into a composer holding the briefing", got)
	}
	r := x.record()
	if r.State != StateSpawnFailed || !strings.Contains(r.Verdict, "stalled") || !strings.Contains(r.Verdict, "Enter") {
		t.Fatalf("a briefing never submitted ended %q with %q; want spawn_failed naming the held briefing", r.State, r.Verdict)
	}
	if r.Notice == nil {
		t.Fatal("the root was not given a notice")
	}
	wire, err := x.b.NoticeWire(x.ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wire, `"kind":"task_stalled"`) {
		t.Errorf("the notice is not task_stalled: %s", wire)
	}
	if n := x.events("task.brief.enter"); n != briefEnterLimit {
		t.Errorf("task.brief.enter events = %d, want %d", n, briefEnterLimit)
	}
	x.run(10 * time.Minute)
	if presses.count() != briefEnterLimit {
		t.Fatalf("pressed again after the task ended: %d", presses.count())
	}
}

// A press that failed is recorded, counts against the bound, and the task
// still ends rather than waiting.
func TestAPressThatFailsIsRecordedAndTheTaskStillEnds(t *testing.T) {
	id := "b1e10003-0000-4000-8000-000000000003"
	x, presses := newHeldRig(t, id)
	presses.err = errors.New("input_moved")
	x.run(time.Duration(briefHeldGraceSecondsLimit)*time.Second + 3*time.Minute)
	r := x.record()
	if r.Briefing == nil || !strings.Contains(r.Briefing.EnterError, "input_moved") {
		t.Fatalf("the failed press is not on the record: %+v", r.Briefing)
	}
	if r.State != StateSpawnFailed {
		t.Fatalf("a task whose presses failed is %q, want spawn_failed", r.State)
	}
}

// Fault injection 3: a screen that is not a composer holding the briefing is
// never pressed — a dialog, a composer with other words, a working child.
func TestAScreenThatDoesNotHoldTheBriefingIsNeverPressed(t *testing.T) {
	for name, shown := range map[string]func(*testing.T, string) string{
		"a dialog": func(t *testing.T, _ string) string { return screen(t, "claude-trust") },
		"other words": func(*testing.T, string) string {
			return "\n" + stallRule + "\n❯ something else entirely\n" + stallRule + "\n"
		},
		"working": func(*testing.T, string) string { return readingScreen },
		"an unframed caret": func(_ *testing.T, id string) string {
			return "❯ You are a Clawdline CHILD agent for task " + id + "\n"
		},
	} {
		t.Run(name, func(t *testing.T) {
			id := "b1e10004-0000-4000-8000-000000000004"
			x, presses := newHeldRig(t, id)
			x.show(shown(t, id))
			x.run(3 * time.Minute)
			if presses.count() != 0 {
				t.Fatalf("%s was pressed %d times", name, presses.count())
			}
		})
	}
}

// What the briefing's typing answered is recorded as stages.
func TestTheBriefingStagesFollowWhatTheSendAnswered(t *testing.T) {
	now := time.Date(2026, 11, 24, 10, 44, 0, 0, time.UTC)
	if b := briefingStages(nil, now); b == nil || !b.ShownAt.Equal(now) || !b.EnteredAt.Equal(now) || !b.HeldSince.IsZero() {
		t.Errorf("a submitted briefing: %+v", b)
	}
	if b := briefingStages(terminal.HeldAfterEnter{Enters: 4}, now); b == nil || !b.EnteredAt.Equal(now) || !b.HeldSince.Equal(now) {
		t.Errorf("a briefing held after Enter: %+v", b)
	}
	if b := briefingStages(terminal.Unsubmitted{Why: "not shown"}, now); b != nil {
		t.Errorf("a briefing never seen arriving is recorded as shown: %+v", b)
	}
}

// The bounds are the registered ones.
func TestTheHeldBriefingBoundsAreRegistered(t *testing.T) {
	if got := capacity.Default(capacity.DispatchBriefEnterPresses); got != briefEnterLimit {
		t.Errorf("%s registers %d, the broker presses %d", capacity.DispatchBriefEnterPresses, got, briefEnterLimit)
	}
	if got := capacity.Default(capacity.DispatchBriefHeldGraceSeconds); got != briefHeldGraceSecondsLimit {
		t.Errorf("%s registers %d, the broker waits %d", capacity.DispatchBriefHeldGraceSeconds, got, briefHeldGraceSecondsLimit)
	}
}

// Fuse's probe (a): the held briefing is covered by Claude Code's trust
// dialog for five minutes. When somebody answers it the briefing is still in
// the composer, and it is pressed — the dialog's minutes are not the
// briefing's, so the task is not ended without a single press.
func TestABriefingHeldBehindADialogIsStillPressedWhenTheDialogGoes(t *testing.T) {
	id := "b1e10005-0000-4000-8000-000000000005"
	x, presses := newHeldRig(t, id)
	x.run(30 * time.Second) // seen held once
	x.show(screen(t, "claude-trust"))
	x.run(5 * time.Minute)
	if r := x.record(); r.State != StateSpawning {
		t.Fatalf("a briefing behind a dialog ended %q (%q) before the dialog was answered", r.State, r.Verdict)
	}
	x.show(heldScreen(id))
	presses.after = func(int) { x.show(readingScreen) }
	x.run(time.Minute)
	if presses.count() != 1 {
		t.Fatalf("after the dialog the held briefing was pressed %d times, want 1", presses.count())
	}
	if r := x.record(); r.State != StateSpawning {
		t.Fatalf("the briefing uncovered after a dialog ended %q (%q); want it pressed and left spawning", r.State, r.Verdict)
	}
	if _, err := x.b.Accept(x.ctx, x.id, "s"); err != nil {
		t.Fatal(err)
	}
	if r := x.record(); r.State != StateBriefed {
		t.Fatalf("the pressed child that signed is %q", r.State)
	}
}

// Fuse's probe (b): the daemon stops for three minutes right after it first
// saw the briefing held — a dev install's restart. The restarted daemon
// presses it; the minutes it was down are not time the briefing was given.
func TestABriefingHeldAcrossADaemonRestartIsPressedAfterIt(t *testing.T) {
	id := "b1e10006-0000-4000-8000-000000000006"
	x, presses := newHeldRig(t, id)
	x.run(30 * time.Second)
	if r := x.record(); r.Briefing == nil || r.Briefing.HeldSince.IsZero() {
		t.Fatalf("the held briefing was not recorded: %+v", r.Briefing)
	}
	x.mu.Lock()
	x.now = x.now.Add(3 * time.Minute) // down: no beat runs
	x.mu.Unlock()
	restarted := &Broker{Store: x.b.Store, Tasks: x.b.Tasks, Git: x.b.Git, Dir: x.b.Dir, Live: x.b.Live,
		SubmitHeld: presses.press}
	x.wire(restarted)
	x.b = restarted
	presses.after = func(int) { x.show(readingScreen) }
	x.run(time.Minute)
	r := x.record()
	if presses.count() != 1 || r.State != StateSpawning {
		t.Fatalf("after a restart the held briefing was pressed %d times and is %q (%q); want 1 press, still spawning",
			presses.count(), r.State, r.Verdict)
	}
}

// The held task ends only after its last press has had the grace to show
// what it did, and never before every press was made.
func TestAHeldBriefingEndsOnlyAGraceAfterItsLastPress(t *testing.T) {
	id := "b1e10007-0000-4000-8000-000000000007"
	x, presses := newHeldRig(t, id)
	for presses.count() < briefEnterLimit {
		if r := x.record(); r.State != StateSpawning {
			t.Fatalf("ended %q after %d presses, before the bound %d", r.State, presses.count(), briefEnterLimit)
		}
		x.run(30 * time.Second)
	}
	last := x.record().Briefing.LastEnterAt
	for x.now.Before(last.Add(time.Duration(briefHeldGraceSecondsLimit)*time.Second - 30*time.Second)) {
		x.run(30 * time.Second)
		if r := x.record(); r.State != StateSpawning {
			t.Fatalf("ended %v after its last press, inside the %ds grace", x.now.Sub(last), briefHeldGraceSecondsLimit)
		}
	}
	x.run(time.Minute)
	r := x.record()
	if r.State != StateSpawnFailed {
		t.Fatalf("still %q a grace after the last press", r.State)
	}
	if !strings.Contains(r.Verdict, "3 times") || strings.Contains(r.Verdict, "0 more times") {
		t.Errorf("the verdict does not count the presses that were made: %q", r.Verdict)
	}
}

// The verdict says what happened: how many presses, a dialog that covered
// it, a daemon that was down while it was held.
func TestTheHeldVerdictSaysWhatHappened(t *testing.T) {
	id := "b1e10008-0000-4000-8000-000000000008"
	x, _ := newHeldRig(t, id)
	x.run(30 * time.Second)
	x.show(screen(t, "claude-trust"))
	x.run(2 * time.Minute)
	x.show(heldScreen(id))
	x.mu.Lock()
	x.now = x.now.Add(4 * time.Minute) // down: no beat runs
	x.mu.Unlock()
	restarted := &Broker{Store: x.b.Store, Tasks: x.b.Tasks, Git: x.b.Git, Dir: x.b.Dir, Live: x.b.Live,
		SubmitHeld: x.b.SubmitHeld}
	x.wire(restarted)
	x.b = restarted
	x.run(5 * time.Minute)
	r := x.record()
	if r.State != StateSpawnFailed {
		t.Fatalf("state %q", r.State)
	}
	for _, want := range []string{"pressed Enter on it 3 times", "dialog", "not running"} {
		if !strings.Contains(r.Verdict, want) {
			t.Errorf("the verdict does not say %q: %q", want, r.Verdict)
		}
	}
	if r.Briefing == nil || r.Briefing.CoveredAt.IsZero() || r.Briefing.ResumedAt.IsZero() {
		t.Errorf("the dialog and the restart are not on the record: %+v", r.Briefing)
	}
}

// Item 2: a send that answered HeldAfterEnter is a held briefing on the
// record, not a spawn error, so a child that is later pressed and signs has
// no failure reason left behind.
func TestAHeldSendIsNotASpawnError(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.Clock = steppingClock(time.Date(2026, 11, 24, 4, 0, 0, 0, time.UTC), 50*time.Second)
	b.Launcher = &openingLauncher{pane: "%75"}
	b.Type = func(context.Context, string, string) error { return terminal.HeldAfterEnter{Enters: 4} }
	b.Live = func(context.Context) []session.Session {
		return []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
			{ID: "%75", Backend: session.BackendTmux, Assistant: session.AssistantClaude},
		}
	}
	out, err := dispatchOne(t, b, ctx, "c8000009-0000-4000-8000-000000000009")
	if err != nil {
		t.Fatal(err)
	}
	if out.Record.State != StateSpawning || out.Record.Briefing == nil || out.Record.Briefing.HeldSince.IsZero() {
		t.Fatalf("a held send: state %q briefing %+v", out.Record.State, out.Record.Briefing)
	}
	if out.Record.SpawnError != "" {
		t.Errorf("a held send was recorded as a spawn error: %q", out.Record.SpawnError)
	}
	if _, err := b.Accept(ctx, out.Record.ID, w1Secret); err != nil {
		t.Fatal(err)
	}
	r, _, err := b.Record(ctx, out.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.SpawnError != "" {
		t.Errorf("after the child signed the record still carries %q", r.SpawnError)
	}
}
