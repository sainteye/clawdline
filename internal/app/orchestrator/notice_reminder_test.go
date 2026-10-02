package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/git"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// Failure injection for the resend. A completion notice used to be typed whole
// on every rung of its ladder, so a root that had the line on its screen and
// had not yet acknowledged it read the same ~1 KB again and again, each copy
// re-read on every later call of that conversation. Measured over 14 days: 30%
// of completion events reached their root at least twice, and every extra
// arrival was a whole copy.
//
// A notice typed once is now followed by one short reminder line, never by a
// second whole copy; one that never reached the root's screen is still typed
// whole, because then it is the first time the root sees it. Each test below
// breaks one link of that chain and asserts the one thing that must not
// happen whatever breaks: a completion event the root was never shown whole.

// typings records every line typed at a root, in order, and can fail the ones a
// test chooses.
type typings struct {
	mu    sync.Mutex
	lines []string
	fail  func(text string) error
}

func (y *typings) typer() func(context.Context, string, string) error {
	return func(_ context.Context, _ string, text string) error {
		y.mu.Lock()
		defer y.mu.Unlock()
		if y.fail != nil {
			if err := y.fail(text); err != nil {
				return err
			}
		}
		y.lines = append(y.lines, text)
		return nil
	}
}

// typed is a decoded line: its kind, its body, its task and notice ids.
type typed struct {
	raw, kind, body, task, notice string
}

func (y *typings) all(t *testing.T) []typed {
	t.Helper()
	y.mu.Lock()
	defer y.mu.Unlock()
	out := []typed{}
	for _, line := range y.lines {
		inner, ok := strings.CutPrefix(line, "<clawdline-notice>")
		inner, ok2 := strings.CutSuffix(inner, "</clawdline-notice>")
		if !ok || !ok2 {
			t.Fatalf("a line typed at the root is not a notice: %q", line)
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(inner), &obj); err != nil {
			t.Fatalf("a notice typed at the root is not JSON: %v\n%s", err, line)
		}
		x := typed{raw: line}
		x.kind, _ = obj["kind"].(string)
		x.body, _ = obj["body"].(string)
		x.notice, _ = obj["notice_id"].(string)
		if task, ok := obj["task"].(map[string]any); ok {
			x.task, _ = task["id"].(string)
		} else {
			x.task, _ = obj["task_id"].(string)
		}
		out = append(out, x)
	}
	return out
}

// whole answers whether a typed line is the whole first notice for r: its kind
// and the full sentence a person reads.
func whole(b *Broker, r Record, x typed) bool {
	return x.kind == NoticeKind(r) && x.task == r.ID && x.notice == r.Notice.ID &&
		x.body == b.FinishedLine(r, r.Notice.ID)
}

// reminder answers whether a typed line is a short reminder for r: its ids and
// the one command, and none of the first notice's sentence.
func reminder(b *Broker, r Record, x typed) bool {
	return x.kind == ReminderKind && x.task == r.ID && x.notice == r.Notice.ID &&
		!strings.Contains(x.raw, b.FinishedLine(r, r.Notice.ID)) &&
		strings.Contains(x.body, ShowCommand(r.ID, r.Notice.ID))
}

// notMissed is the assertion every test here ends with: the root was shown the
// whole notice for this event at least once.
func notMissed(t *testing.T, b *Broker, r Record, lines []typed) {
	t.Helper()
	for _, x := range lines {
		if whole(b, r, x) {
			return
		}
	}
	t.Fatalf("task %s's completion was never typed whole; typed: %+v", r.ID, lines)
}

func pumpAt(b *Broker, ctx context.Context, now *time.Time, at time.Time) {
	*now = at
	b.PumpNotices(ctx)
}

// (a) Nothing could be typed for a while — a terminal that refused the bytes.
// What finally reaches the root is the whole notice: nothing before it was on
// the root's screen, so it is the first time the root reads it.
func TestANoticeThatCouldNotBeTypedArrivesWholeWhenItFinallyCan(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	y := &typings{}
	refusals := 2
	y.fail = func(string) error {
		if refusals > 0 {
			refusals--
			return errors.New("the terminal refused the bytes")
		}
		return nil
	}
	b.Type = y.typer()
	r := finished(t, b, ctx, "ae000000-0000-4000-8000-000000000001")

	for i := 0; i < 3; i++ {
		pumpAt(b, ctx, &now, now.Add(retryCeiling))
	}
	lines := y.all(t)
	if len(lines) != 1 || !whole(b, r, lines[0]) {
		t.Fatalf("after two refusals the first line on the root's screen is %+v, want the whole notice", lines)
	}
	after, _, _ := b.Record(ctx, r.ID)
	if after.Notice.State != NoticeDelivered || after.Notice.Attempts != 3 || !after.Notice.ObservedAt.IsZero() {
		t.Fatalf("after the delivery: %+v", after.Notice)
	}
	notMissed(t, b, r, lines)
}

// (b) The whole notice reached the root and nobody has said they read it. The
// next rung types a reminder: one line, the ids and the command, and not the
// first notice's sentence over again.
func TestADeliveredNoticeIsRemindedNotRetyped(t *testing.T) {
	b, ctx := newTestBroker(t)
	start := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	now := start
	b.Clock = func() time.Time { return now }
	y := &typings{}
	b.Type = y.typer()
	r := finished(t, b, ctx, "ae000000-0000-4000-8000-000000000002")

	b.PumpNotices(ctx)
	pumpAt(b, ctx, &now, start.Add(ackWaitBase))
	lines := y.all(t)
	if len(lines) != 2 {
		t.Fatalf("typed %d lines, want the notice and one reminder", len(lines))
	}
	if !whole(b, r, lines[0]) {
		t.Fatalf("the first line is not the whole notice: %s", lines[0].raw)
	}
	if !reminder(b, r, lines[1]) {
		t.Fatalf("the second line is not a short reminder: %s", lines[1].raw)
	}
	if len(lines[1].raw) >= len(lines[0].raw) {
		t.Errorf("the reminder is %d characters against the notice's %d", len(lines[1].raw), len(lines[0].raw))
	}
	// Typing a reminder is a delivery, not an observation.
	after, _, _ := b.Record(ctx, r.ID)
	if after.Notice.State != NoticeDelivered || !after.Notice.ObservedAt.IsZero() || !after.Notice.AcknowledgedAt.IsZero() ||
		after.Notice.Attempts != 2 || after.Notice.NextRetryAt.Sub(now) != AckWaitDelay(2) {
		t.Fatalf("after the reminder: %+v", after.Notice)
	}
	notMissed(t, b, r, lines)
}

// (c) The root ran `clawdline task show`, which printed the task, and the ACK
// it sends once it has printed never arrived — the request failed on the way
// (cmd/clawdline TestTaskShowWhoseAckIsLostSaysSo is that command's side). The
// notice stays delivered and keeps climbing its ladder, and what each later
// rung types is the short reminder, never the whole notice again; nothing
// about having typed it counts as having been read.
func TestALostAckLeavesTheNoticeOnItsLadder(t *testing.T) {
	b, ctx := newTestBroker(t)
	start := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	now := start
	b.Clock = func() time.Time { return now }
	y := &typings{}
	b.Type = y.typer()
	r := finished(t, b, ctx, "ae000000-0000-4000-8000-000000000003")

	b.PumpNotices(ctx)
	// The ACK that would have ended it never reaches the broker; one that names
	// another notice is refused and changes nothing.
	if _, err := b.Acknowledge(ctx, r.ID, "ae000000-0000-4000-8000-0000000000ff"); err == nil {
		t.Fatal("an ACK for another notice was accepted")
	}
	pumpAt(b, ctx, &now, start.Add(ackWaitBase))
	pumpAt(b, ctx, &now, start.Add(ackWaitBase+AckWaitDelay(2)))
	lines := y.all(t)
	if len(lines) != 3 || !whole(b, r, lines[0]) || !reminder(b, r, lines[1]) || !reminder(b, r, lines[2]) {
		t.Fatalf("with the ACK lost the root was typed %+v, want the notice and two reminders", lines)
	}
	if len(lines[1].raw) >= len(lines[0].raw) {
		t.Errorf("the reminder after a lost ACK is %d characters against the notice's %d", len(lines[1].raw), len(lines[0].raw))
	}
	after, _, _ := b.Record(ctx, r.ID)
	if after.Notice.State != NoticeDelivered || !after.Notice.ObservedAt.IsZero() || after.Notice.Attempts != 3 {
		t.Fatalf("after a lost ACK: %+v", after.Notice)
	}
	notMissed(t, b, r, lines)
}

// (d) The daemon restarts between rungs. What it knows about a notice is the
// ledger's, so one that reached its root before the restart is reminded after
// it, and one that never did is still typed whole.
func TestARestartRemindsWhatWasDeliveredAndTypesWhatWasNot(t *testing.T) {
	dir := t.TempDir()
	open := func() (*Broker, *store.Store) {
		st, err := store.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		return &Broker{Store: st, Tasks: taskdir.New(dir), Git: git.New(), Dir: dir,
			Live: func(context.Context) []session.Session {
				return []session.Session{{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation}}
			}}, st
	}
	ctx := context.Background()
	start := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	now := start
	b, st := open()
	b.Clock = func() time.Time { return now }
	before := &typings{}
	b.Type = before.typer()
	sent := finished(t, b, ctx, "ae000000-0000-4000-8000-000000000004")
	unsent := finished(t, b, ctx, "ae000000-0000-4000-8000-000000000005")
	// The second task's line never goes, before the restart.
	before.fail = func(text string) error {
		if strings.Contains(text, unsent.ID) {
			return errors.New("the terminal refused the bytes")
		}
		return nil
	}
	b.PumpNotices(ctx)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	b, st = open()
	t.Cleanup(func() { _ = st.Close() })
	b.Clock = func() time.Time { return now }
	after := &typings{}
	b.Type = after.typer()
	pumpAt(b, ctx, &now, start.Add(ackWaitBase))

	lines := after.all(t)
	var forSent, forUnsent []typed
	for _, x := range lines {
		switch x.task {
		case sent.ID:
			forSent = append(forSent, x)
		case unsent.ID:
			forUnsent = append(forUnsent, x)
		}
	}
	if len(forSent) != 1 || !reminder(b, sent, forSent[0]) {
		t.Fatalf("after the restart the delivered notice was typed %+v, want one reminder", forSent)
	}
	if len(forUnsent) != 1 || !whole(b, unsent, forUnsent[0]) {
		t.Fatalf("after the restart the undelivered notice was typed %+v, want it whole", forUnsent)
	}
	notMissed(t, b, sent, before.all(t))
	notMissed(t, b, unsent, lines)
}

// (e) A reminder is per notice. Two tasks finishing are two events, and each is
// typed whole the first time, whatever the other one's state.
func TestEveryNewNoticeIsTypedWhole(t *testing.T) {
	b, ctx := newTestBroker(t)
	start := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	now := start
	b.Clock = func() time.Time { return now }
	y := &typings{}
	b.Type = y.typer()
	first := finished(t, b, ctx, "ae000000-0000-4000-8000-000000000006")
	b.PumpNotices(ctx)
	second := finished(t, b, ctx, "ae000000-0000-4000-8000-000000000007")
	pumpAt(b, ctx, &now, start.Add(time.Second))

	lines := y.all(t)
	if len(lines) != 2 || !whole(b, first, lines[0]) || !whole(b, second, lines[1]) {
		t.Fatalf("two finished tasks were typed %+v, want each whole", lines)
	}
	notMissed(t, b, first, lines)
	notMissed(t, b, second, lines)
}

// (f) Reminding does not change the budget: eight typings in all, the first
// whole and the rest reminders, and then dead letter.
func TestRemindersSpendTheSameBudgetAndEndInDeadLetter(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	y := &typings{}
	b.Type = y.typer()
	r := finished(t, b, ctx, "ae000000-0000-4000-8000-000000000008")

	for i := 0; i < AttemptLimit+4; i++ {
		b.PumpNotices(ctx)
		now = now.Add(maxAckWait + time.Second)
	}
	lines := y.all(t)
	if len(lines) != AttemptLimit {
		t.Fatalf("typed %d lines, want %d", len(lines), AttemptLimit)
	}
	if !whole(b, r, lines[0]) {
		t.Fatalf("the first line is not the whole notice: %s", lines[0].raw)
	}
	for i, x := range lines[1:] {
		if !reminder(b, r, x) {
			t.Fatalf("line %d is not a reminder: %s", i+2, x.raw)
		}
	}
	after, _, _ := b.Record(ctx, r.ID)
	if after.Notice.State != NoticeDeadLetter || after.Notice.Attempts != AttemptLimit || !after.Notice.ObservedAt.IsZero() {
		t.Fatalf("after the budget: %+v", after.Notice)
	}
	notMissed(t, b, r, lines)
}

// The size this file exists for, measured rather than asserted from memory:
// each case is a task as a root is told about it, and `v2` is the length of
// the version 2 notice the broker typed for that same record at commit
// 1e25de0c, with a tasks directory under a home directory. A committed branch
// is the common case, and it is the one that has to come in at half. That
// version 2 had already lost its separate ack step (about 70 characters), so
// the same branch with leftovers comes in at 51% and is held to two thirds.
func TestTheFirstNoticeAndTheReminderAreShort(t *testing.T) {
	b := &Broker{Tasks: taskdir.New("/Users/someone/.config/clawdline-next/tasks")}
	id := "ae000000-0000-4000-8000-000000000009"
	wt := &Worktree{Branch: "clawdline/task/" + id,
		Path: "/Users/someone/.config/clawdline-next/worktrees/clawdline-3b9e26c1/" + id}
	title := "Settings page keeps the chosen theme after reload"
	n := &Notice{ID: "ae000000-0000-4000-8000-00000000000a"}
	carried := &Landing{State: LandingPending, Settlement: SettlementCarried}
	cases := []struct {
		name string
		r    Record
		v2   int
		half bool
	}{
		{"plain", Record{ID: id, Title: title, State: StateSuccess, Notice: n}, 777, false},
		{"committed branch", Record{ID: id, Title: title, State: StateSuccess, Notice: n, Worktree: wt,
			Landing: carried}, 1058, true},
		{"committed branch, 2 leftovers", Record{ID: id, Title: title, State: StateSuccess, Notice: n,
			Worktree: wt, Landing: carried, Result: &taskdir.Result{Status: "success",
				Leftovers: []work.Leftover{{Title: "a"}, {Title: "b"}}}}, 1087, false},
		{"empty branch", Record{ID: id, Title: title, State: StateSuccess, Notice: n, Worktree: wt,
			Landing: &Landing{State: LandingPending, Settlement: SettlementEmpty}}, 1073, false},
		{"timeout", Record{ID: id, Title: title, State: StateTimeout, Notice: n, Claims: []string{"a/"}}, 822, false},
	}
	for _, c := range cases {
		wire, err := b.NoticeWire(context.Background(), c.r)
		if err != nil {
			t.Fatal(err)
		}
		short, err := ReminderWire(c.r)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%-30s version 2 %4d, first notice %4d (%2d%%), reminder %d", c.name, c.v2, len(wire),
			100*len(wire)/c.v2, len(short))
		if c.half && len(wire)*2 > c.v2 {
			t.Errorf("%s: the first notice is %d characters against version 2's %d", c.name, len(wire), c.v2)
		}
		if len(wire)*3 > c.v2*2 {
			t.Errorf("%s: the first notice is %d characters against version 2's %d", c.name, len(wire), c.v2)
		}
		// A resend was a whole version 2 copy; the reminder is to be under half
		// of the shortest one.
		if len(short)*2 > 777 {
			t.Errorf("%s: the reminder is %d characters", c.name, len(short))
		}
	}
}
