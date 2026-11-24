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
)

// A briefing that was typed and never submitted (stall.go, tendHeld).
//
// Task bb9f44d8, 2026-11-24: the briefing reached Claude Code's composer, the
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
	x.run(time.Duration(briefHeldSecondsLimit)*time.Second + 2*time.Minute)
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
	x.run(time.Duration(briefHeldSecondsLimit)*time.Second + 2*time.Minute)
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
	if got := capacity.Default(capacity.DispatchBriefHeldSeconds); got != briefHeldSecondsLimit {
		t.Errorf("%s registers %d, the broker waits %d", capacity.DispatchBriefHeldSeconds, got, briefHeldSecondsLimit)
	}
}
