package orchestrator

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// A child that was briefed and then sat still (stall.go).
//
// Measured on 2026-09-25: the broker typed a child its first line, the model
// answered a lone `<br>` in one second and ended its turn, and the tab sat at
// an empty composer for forty minutes. No receipt came, the task stayed
// `spawning`, nothing told its root, and it would have stayed that way until
// its 240-minute timeout. One line typed into the tab got it working at once.

const (
	stallRule = "────────────────────────────────────────────────────────────"
	// stalledScreen is that tab: the first line, the lone answer, an empty
	// composer in its frame.
	stalledScreen = "> You are a Clawdline CHILD agent for task … read CHILD.md …\n\n⏺ <br>\n\n" +
		stallRule + "\n❯ \n" + stallRule + "\n  ? for shortcuts\n"
	// readingScreen is a child that is slow and working: a live line with
	// its clock and the way out on it.
	readingScreen = "> You are a Clawdline CHILD agent for task …\n\n⏺ Reading CHILD.md\n\n" +
		"✻ Reading… (4m 12s · ↓ 1.2k tokens · esc to interrupt)\n\n" +
		stallRule + "\n❯ \n" + stallRule + "\n"
)

// stallRig is one spawning child in a tmux pane, a clock the test moves, and
// a screen the test chooses.
type stallRig struct {
	t      *testing.T
	b      *Broker
	ctx    context.Context
	id     string
	pane   string
	keys   *typedKeys
	mu     sync.Mutex
	now    time.Time
	screen string
}

func newStallRig(t *testing.T, id string) *stallRig {
	t.Helper()
	b, ctx := newTestBroker(t)
	x := &stallRig{t: t, b: b, ctx: ctx, id: id, pane: "%5", keys: &typedKeys{},
		now: time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC), screen: stalledScreen}
	x.wire(b)
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "stalled child", State: StateSpawning,
		CreatedAt: x.now, Claims: []string{}, TimeoutMinutes: 240, ChildTerminalID: x.pane, ChildBackend: "tmux",
		SpawnedAt: x.now, Root: &RootRef{SessionID: rootConversation, Assistant: "claude"}}
	if err := b.save(ctx, r, HashSecret("s"), "task.spawned"); err != nil {
		t.Fatal(err)
	}
	return x
}

// wire points a broker — the first, or one standing in for a daemon that
// restarted — at the rig's clock, screen, keys and machine.
func (x *stallRig) wire(b *Broker) {
	b.Clock = func() time.Time {
		x.mu.Lock()
		defer x.mu.Unlock()
		return x.now
	}
	b.Type = x.keys.Type
	b.Launcher = &fakeLauncher{}
	b.Screen = func(_ context.Context, id string) (string, bool) {
		x.mu.Lock()
		defer x.mu.Unlock()
		if id != x.pane {
			return "", false
		}
		return x.screen, true
	}
	b.Reading = func(context.Context) session.Inventory {
		return session.Inventory{Complete: true, Sources: map[string]bool{"tmux": true},
			Sessions: []session.Session{
				{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
				{ID: x.pane, Backend: session.BackendTmux, Assistant: session.AssistantClaude},
			}}
	}
}

func (x *stallRig) show(screen string) {
	x.mu.Lock()
	x.screen = screen
	x.mu.Unlock()
}

// run moves the clock on by d in thirty-second beats, one pass each.
func (x *stallRig) run(d time.Duration) {
	for end := x.now.Add(d); x.now.Before(end); {
		x.mu.Lock()
		x.now = x.now.Add(30 * time.Second)
		x.mu.Unlock()
		x.b.Pass(x.ctx)
	}
}

func (x *stallRig) record() Record {
	x.t.Helper()
	r, _, err := x.b.Record(x.ctx, x.id)
	if err != nil {
		x.t.Fatal(err)
	}
	return r
}

func (x *stallRig) nudges() []string {
	x.keys.mu.Lock()
	defer x.keys.mu.Unlock()
	return append([]string(nil), x.keys.lines[x.pane]...)
}

func (x *stallRig) events(kind string) int {
	x.t.Helper()
	n, err := x.b.Store.EventCount(x.ctx, kind)
	if err != nil {
		x.t.Fatal(err)
	}
	return n
}

// The incident: nudged once, naming its CHILD.md and never the secret, then
// reported to its root as stalled — long before its timeout.
func TestAChildIdleAfterItsBriefingIsNudgedOnceThenReportedStalled(t *testing.T) {
	x := newStallRig(t, "5a110001-0000-4000-8000-000000000001")

	x.run(stallIdleLimit - time.Minute)
	if got := x.nudges(); len(got) != 0 {
		t.Fatalf("nudged after %v of idle, before its interval: %q", stallIdleLimit-time.Minute, got)
	}
	x.run(2 * time.Minute)
	got := x.nudges()
	if len(got) != 1 {
		t.Fatalf("a child idle past its interval was typed %d lines, want one nudge: %q", len(got), got)
	}
	childMD := filepath.Join(x.b.Tasks.Path(x.id), "CHILD.md")
	if !strings.Contains(got[0], childMD) {
		t.Errorf("the nudge %q does not name %s", got[0], childMD)
	}
	if strings.Contains(got[0], "TASK_SECRET") || strings.ContainsAny(got[0], "\n\r") {
		t.Errorf("the nudge %q carries the secret's name or a line break", got[0])
	}
	if n := x.events("task.nudged"); n != 1 {
		t.Errorf("task.nudged events = %d, want 1", n)
	}
	if r := x.record(); r.State != StateSpawning {
		t.Fatalf("a nudged child is %q, want still spawning", r.State)
	}

	x.run(stallIdleLimit + time.Minute)
	r := x.record()
	if r.State != StateSpawnFailed || !strings.Contains(r.Verdict, "stalled") {
		t.Fatalf("still idle after the nudge: state %q, verdict %q; want spawn_failed saying stalled", r.State, r.Verdict)
	}
	if r.Notice == nil {
		t.Fatal("the root was not given a notice")
	}
	wire, err := x.b.NoticeWire(x.ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wire, `"kind":"task_stalled"`) || !strings.Contains(wire, "respawn") {
		t.Errorf("the notice does not say stalled, or how to retry it: %s", wire)
	}
	if !r.FinishedAt.Before(r.CreatedAt.Add(30 * time.Minute)) {
		t.Errorf("reported at %v, which is waiting for the timeout", r.FinishedAt)
	}

	x.run(30 * time.Minute)
	if got := x.nudges(); len(got) != 1 {
		t.Fatalf("typed %d lines at the child in all, want exactly one nudge: %q", len(got), got)
	}
}

// A child reading files for a long time shows a live line; it is never typed at.
func TestASlowChildThatIsWorkingIsLeftAlone(t *testing.T) {
	x := newStallRig(t, "5a110002-0000-4000-8000-000000000002")
	x.show(readingScreen)
	x.run(3 * stallIdleLimit)
	if got := x.nudges(); len(got) != 0 {
		t.Fatalf("a working child was typed at: %q", got)
	}
	if r := x.record(); r.State != StateSpawning || r.Verdict != "" {
		t.Fatalf("a working child is %q (%q)", r.State, r.Verdict)
	}
	// Idle only between two turns: the clock starts again from the last
	// working reading, not from the first idle one.
	x.show(stalledScreen)
	x.run(stallIdleLimit - time.Minute)
	x.show(readingScreen)
	x.run(time.Minute)
	x.show(stalledScreen)
	x.run(stallIdleLimit - time.Minute)
	if got := x.nudges(); len(got) != 0 {
		t.Fatalf("idle time on both sides of a working reading was added up: %q", got)
	}
}

// A tab holding a dialog would take the nudge as its answer.
func TestAChildShowingAMenuIsNotTypedAt(t *testing.T) {
	x := newStallRig(t, "5a110003-0000-4000-8000-000000000003")
	x.show(screen(t, "claude-trust"))
	x.run(3 * stallIdleLimit)
	if got := x.nudges(); len(got) != 0 {
		t.Fatalf("a child at a dialog was typed at: %q", got)
	}
	if n := x.events("task.nudged"); n != 0 {
		t.Fatalf("task.nudged events = %d at a dialog", n)
	}
	// The spawn clock ends a tab holding a dialog at four minutes, before the
	// nudge's interval is up, so the beat above never reaches the stall's own
	// reading. That reading is asked directly: a menu is never idle to it,
	// though the trust dialog's `❯ No, exit` is a caret session.ReadState
	// calls an idle prompt.
	rd := reading{sessions: map[string]session.Session{x.pane: {ID: x.pane}}}
	r := Record{ID: x.id, Assistant: "claude", ChildTerminalID: x.pane}
	for _, name := range []string{"claude-trust", "claude-composer"} {
		x.show(screen(t, name))
		if x.b.childIdle(x.ctx, rd, r) {
			t.Errorf("%s reads as an idle child", name)
		}
	}
	x.show(stalledScreen)
	if !x.b.childIdle(x.ctx, rd, r) {
		t.Error("the stalled child's screen does not read as idle")
	}
}

// A child the nudge woke signs for its briefing, and is never reported.
func TestAChildThatSignsAfterTheNudgeIsNotReported(t *testing.T) {
	x := newStallRig(t, "5a110004-0000-4000-8000-000000000004")
	x.run(stallIdleLimit + time.Minute)
	if got := x.nudges(); len(got) != 1 {
		t.Fatalf("typed %d lines, want the nudge", len(got))
	}
	if _, err := x.b.Accept(x.ctx, x.id, "s"); err != nil {
		t.Fatal(err)
	}
	// Signed, and then idle at its prompt between two steps of its own.
	x.run(3 * stallIdleLimit)
	r := x.record()
	if r.State != StateBriefed || r.Verdict != "" || r.Notice != nil {
		t.Fatalf("a child that signed was reported: state %q, verdict %q", r.State, r.Verdict)
	}
	if got := x.nudges(); len(got) != 1 {
		t.Fatalf("typed %d lines, want only the one nudge", len(got))
	}
}

// The nudge is recorded before it is typed, so a daemon that restarts — and
// forgets every screen it read — does not nudge the same child again.
func TestANudgeIsNeverRepeatedAcrossARestart(t *testing.T) {
	x := newStallRig(t, "5a110005-0000-4000-8000-000000000005")
	x.run(stallIdleLimit + time.Minute)
	if got := x.nudges(); len(got) != 1 {
		t.Fatalf("typed %d lines, want the nudge", len(got))
	}
	restarted := &Broker{Store: x.b.Store, Tasks: x.b.Tasks, Git: x.b.Git, Dir: x.b.Dir, Live: x.b.Live}
	x.wire(restarted)
	x.b = restarted
	x.run(stallIdleLimit + time.Minute)
	if got := x.nudges(); len(got) != 1 {
		t.Fatalf("a restarted daemon nudged again: %q", got)
	}
	if r := x.record(); r.State != StateSpawnFailed {
		t.Fatalf("after the restart the child is %q, want reported", r.State)
	}
}
