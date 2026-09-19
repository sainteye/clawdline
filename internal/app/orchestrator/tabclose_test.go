package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/terminal"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// What a task's end does to its child's tab (linger.go). Before this, a tmux
// child was closed three minutes after it finished and an iTerm2 child never
// was — so the session list on this Mac only grew — and a schedule's close_tab
// was parsed, stored and answered and acted on nowhere.

// itermChildCloser is an iTerm2 that records each child it is asked to close,
// with the end time it was given, and answers what the test sets.
type itermChildCloser struct {
	fakeLauncher
	calls  sync.Mutex
	asked  []itermCloseCall
	answer error
}

type itermCloseCall struct {
	id     string
	before time.Time
}

func (l *itermChildCloser) CloseITermChild(_ context.Context, id string, before time.Time) (bool, error) {
	l.calls.Lock()
	defer l.calls.Unlock()
	l.asked = append(l.asked, itermCloseCall{id, before})
	if l.answer != nil {
		return false, l.answer
	}
	return true, nil
}

func (l *itermChildCloser) closes() []itermCloseCall {
	l.calls.Lock()
	defer l.calls.Unlock()
	return append([]itermCloseCall(nil), l.asked...)
}

// settledChild stores a briefed child with a tab and settles it.
func settledChild(t *testing.T, b *Broker, ctx context.Context, id, backend, pane string, end State,
	change func(*Record)) Record {
	t.Helper()
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "t", State: StateBriefed,
		CreatedAt: b.now(), Claims: []string{}, ChildTerminalID: pane, ChildBackend: backend}
	if change != nil {
		change(&r)
	}
	if err := b.save(ctx, r, HashSecret("s"), "task.queued"); err != nil {
		t.Fatal(err)
	}
	settled, err := b.Settle(ctx, id, end, "done", nil)
	if err != nil {
		t.Fatal(err)
	}
	return settled
}

// aReading is one reading of the machine: the sessions, and whether each
// source answered completely. The root's own tab is always in it, so no
// reading here is one "with no terminals in it at all".
func aReading(itermComplete bool, sessions ...session.Session) reading {
	m := map[string]session.Session{"%1": {ID: "%1", Assistant: session.AssistantClaude}}
	for _, s := range sessions {
		m[s.ID] = s
	}
	return reading{sessions: m, sources: map[string]bool{"tmux": true, "iterm": itermComplete}}
}

func idleClaude(id string) session.Session {
	return session.Session{ID: id, Assistant: session.AssistantClaude, State: session.StateIdle}
}

func lingerOwed(t *testing.T, b *Broker, ctx context.Context, id string) (store.Linger, bool) {
	t.Helper()
	rows, err := b.Store.Lingers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range rows {
		if l.Task == id {
			return l, true
		}
	}
	return store.Linger{}, false
}

func latestEvent(t *testing.T, b *Broker, ctx context.Context, kind, id string) (map[string]any, bool) {
	t.Helper()
	events, err := b.Store.LatestEvents(ctx, kind, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	e, ok := events[id]
	if !ok {
		return nil, false
	}
	var body map[string]any
	_ = json.Unmarshal(e.Payload, &body)
	return body, true
}

// The policy is one table, and every end has a row in it.
func TestTheTabPolicyIsOneTable(t *testing.T) {
	linger := 3 * time.Minute
	plain := Record{}
	sched := func(closeTab string) Record {
		return Record{ScheduleID: "5c000000-0000-4000-8000-000000000001", ScheduleCloseTab: closeTab}
	}
	cases := []struct {
		name   string
		r      Record
		end    State
		linger time.Duration
		want   TabPlan
	}{
		{"a success lingers", plain, StateSuccess, linger, TabPlan{TabRuleLinger, true, linger}},
		{"a failure lingers", plain, StateFailure, linger, TabPlan{TabRuleLinger, true, linger}},
		{"a timeout is left open", plain, StateTimeout, linger, TabPlan{TabRuleUnfinished, false, 0}},
		{"a cancel is left open", plain, StateCancelled, linger, TabPlan{TabRuleUnfinished, false, 0}},
		{"a negative linger keeps it", plain, StateSuccess, -1, TabPlan{TabRuleLingerOff, false, 0}},
		{"a child that never started is closed", plain, StateSpawnFailed, -1, TabPlan{TabRuleSpawnFailed, true, 0}},
		{"on_success after a success", sched("on_success"), StateSuccess, linger, TabPlan{TabRuleScheduleOnSuccess, true, 0}},
		{"on_success after a failure", sched("on_success"), StateFailure, linger, TabPlan{TabRuleScheduleOnSuccess, false, 0}},
		{"no close_tab is on_success", sched(""), StateSuccess, -1, TabPlan{TabRuleScheduleOnSuccess, true, 0}},
		{"always after a timeout", sched("always"), StateTimeout, linger, TabPlan{TabRuleScheduleAlways, true, 0}},
		{"always after a failure", sched("always"), StateFailure, -1, TabPlan{TabRuleScheduleAlways, true, 0}},
		{"never after a success", sched("never"), StateSuccess, linger, TabPlan{TabRuleScheduleNever, false, 0}},
	}
	for _, c := range cases {
		if got := tabPolicy(c.r, c.end, c.linger); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

// An iTerm2 child is owed a close like a tmux one, and closed on a reading
// whose iTerm2 half did not answer completely: the tab itself was seen, by its
// id, at rest. The end time travels with the close, so a job in the tab is
// ended only when it began before the task ended.
func TestAFinishedITermChildIsClosedEvenWhenITermCannotListEveryWindow(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 19, 6, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	launcher := &itermChildCloser{}
	b.Launcher = launcher
	b.ChildLinger = func() time.Duration { return 3 * time.Minute }
	id := "7ab00001-0000-4000-8000-000000000001"
	r := settledChild(t, b, ctx, id, "iterm", "GUID-1", StateSuccess, nil)

	l, owed := lingerOwed(t, b, ctx, id)
	if !owed || l.Backend != "iterm" || !l.Deadline.Equal(now.Add(3*time.Minute)) {
		t.Fatalf("a finished iTerm2 child owes no close: %+v %v", l, owed)
	}
	partial := aReading(false, idleClaude("GUID-1"))
	if n := b.closeLingers(ctx, partial); n != 0 || len(launcher.closes()) != 0 {
		t.Fatal("closed before its linger was over")
	}
	now = now.Add(3*time.Minute + time.Second)
	if n := b.closeLingers(ctx, partial); n != 1 {
		t.Fatalf("closed %d", n)
	}
	asked := launcher.closes()
	if len(asked) != 1 || asked[0].id != "GUID-1" || asked[0].before.Unix() != r.FinishedAt.Unix() {
		t.Fatalf("asked %+v, want GUID-1 with the task's end", asked)
	}
	if _, owed := lingerOwed(t, b, ctx, id); owed {
		t.Fatal("the close is still owed")
	}
}

// "Not seen" is not "gone": an iTerm2 tab absent from a reading whose iTerm2
// half skipped a window is waited for; only a complete iTerm2 answer drops it,
// and dropping it closes nothing.
func TestAnITermTabThatWasNotSeenIsNotCalledGone(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 19, 6, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	launcher := &itermChildCloser{}
	b.Launcher = launcher
	b.ChildLinger = func() time.Duration { return 0 }
	b.lingerStarted = now.Add(-time.Minute) // a broker that was already running
	id := "7ab00002-0000-4000-8000-000000000002"
	settledChild(t, b, ctx, id, "iterm", "GUID-2", StateFailure, nil)
	now = now.Add(time.Second)

	b.closeLingers(ctx, aReading(false))
	if _, owed := lingerOwed(t, b, ctx, id); !owed {
		t.Fatal("a tab an incomplete iTerm2 reading did not see was dropped as gone")
	}
	b.closeLingers(ctx, aReading(true))
	if _, owed := lingerOwed(t, b, ctx, id); owed {
		t.Fatal("a complete iTerm2 reading without the tab did not drop it")
	}
	if _, said := latestEvent(t, b, ctx, "task.child.linger.gone", id); !said {
		t.Fatal("no event says the tab was already gone")
	}
	if len(launcher.closes()) != 0 {
		t.Fatal("a tab that is gone was closed")
	}
}

// A tab somebody used after its task ended is theirs. Seen at rest, then
// working again, is a turn the child did not start; a different assistant or
// a different conversation in it is not the child at all. Each is left open,
// with the reason, and never closed.
func TestATabUsedAfterItsTaskEndedIsLeftOpen(t *testing.T) {
	b, ctx := newTestBroker(t)
	start := time.Date(2026, 9, 19, 6, 0, 0, 0, time.UTC)
	now := start
	b.Clock = func() time.Time { return now }
	launcher := &fakeLauncher{}
	b.Launcher = launcher
	b.ChildLinger = func() time.Duration { return 3 * time.Minute }
	resumed := "7ab00003-0000-4000-8000-000000000003"
	other := "7ab00004-0000-4000-8000-000000000004"
	moved := "7ab00005-0000-4000-8000-000000000005"
	settledChild(t, b, ctx, resumed, "tmux", "%41", StateSuccess, nil)
	settledChild(t, b, ctx, other, "tmux", "%42", StateSuccess, nil)
	settledChild(t, b, ctx, moved, "tmux", "%43", StateSuccess, nil)

	conv := func(s session.Session, c string) session.Session { s.ConversationID = c; return s }
	working := idleClaude("%41")
	working.State = session.StateWorking
	now = start.Add(10 * time.Second)
	b.closeLingers(ctx, aReading(true, idleClaude("%41"), idleClaude("%42"), conv(idleClaude("%43"), "conv-a")))
	now = start.Add(time.Minute)
	b.closeLingers(ctx, aReading(true, working, idleClaude("%42"), conv(idleClaude("%43"), "conv-a")))
	now = start.Add(3*time.Minute + time.Second)
	codex := session.Session{ID: "%42", Assistant: session.AssistantCodex, State: session.StateIdle}
	b.closeLingers(ctx, aReading(true, idleClaude("%41"), codex, conv(idleClaude("%43"), "conv-b")))

	if len(launcher.closed) != 0 {
		t.Fatalf("closed %v, a tab somebody else is using", launcher.closed)
	}
	for id, want := range map[string]string{resumed: "a turn began", other: "different assistant", moved: "another conversation"} {
		body, said := latestEvent(t, b, ctx, "task.child.linger.left", id)
		if why, _ := body["why"].(string); !said || !strings.Contains(why, want) {
			t.Errorf("%s: left event %v, want the reason %q", id, body, want)
		}
		if _, owed := lingerOwed(t, b, ctx, id); owed {
			t.Errorf("%s is still owed a close", id)
		}
	}
}

// A schedule's close_tab is what its run's end does to the tab: never keeps
// it, on_success closes a success only, always closes whatever the end — each
// as soon as the tab is at rest, as the Swift app's scheduledCloseAt did.
func TestAScheduledRunFollowsItsCloseTab(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 19, 6, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	b.Launcher = &fakeLauncher{}
	b.ChildLinger = func() time.Duration { return 3 * time.Minute }
	cases := []struct {
		id, closeTab string
		end          State
		owed         bool
	}{
		{"7ab00010-0000-4000-8000-000000000010", "never", StateSuccess, false},
		{"7ab00011-0000-4000-8000-000000000011", "on_success", StateFailure, false},
		{"7ab00012-0000-4000-8000-000000000012", "on_success", StateSuccess, true},
		{"7ab00013-0000-4000-8000-000000000013", "", StateSuccess, true},
		{"7ab00014-0000-4000-8000-000000000014", "always", StateTimeout, true},
		{"7ab00015-0000-4000-8000-000000000015", "always", StateFailure, true},
	}
	for i, c := range cases {
		pane := "%5" + string(rune('0'+i))
		settledChild(t, b, ctx, c.id, "tmux", pane, c.end, func(r *Record) {
			r.ScheduleID, r.ScheduleTitle, r.ScheduleCloseTab = "5c000000-0000-4000-8000-000000000001", "daily", c.closeTab
		})
		l, owed := lingerOwed(t, b, ctx, c.id)
		if owed != c.owed {
			t.Errorf("close_tab %q after %s: owed %v, want %v", c.closeTab, c.end, owed, c.owed)
			continue
		}
		if owed && !l.Deadline.Equal(now) {
			t.Errorf("close_tab %q: due %v, want at once", c.closeTab, l.Deadline)
		}
	}
}

// A schedule's close_tab reaches the task its run becomes, and a respawn of
// that run keeps it.
func TestAScheduledDispatchCarriesItsCloseTab(t *testing.T) {
	b, ctx := newTestBroker(t)
	project := t.TempDir()
	out, err := b.DispatchScheduled(ctx, ScheduledRun{
		TaskID: "7ab00020-0000-4000-8000-000000000020", ScheduleID: "5c000000-0000-4000-8000-000000000002",
		Title: "nightly", CloseTab: "never",
		Template: map[string]any{"assistant": "claude", "title": "nightly", "instructions": "x",
			"project_dir": project, "claims": []string{}, "timeout_minutes": 5},
	})
	if out.Absent {
		t.Fatalf("the run was refused before it was recorded: %v", err)
	}
	r, _, rerr := b.Record(ctx, "7ab00020-0000-4000-8000-000000000020")
	if rerr != nil {
		t.Fatalf("no record: %v (dispatch said %v)", rerr, err)
	}
	if r.ScheduleCloseTab != "never" {
		t.Fatalf("the record's close_tab is %q", r.ScheduleCloseTab)
	}
	if o := scheduleOf(r); o == nil || o.CloseTab != "never" {
		t.Fatalf("a respawn of the run would carry %+v", o)
	}
}

// The settlement says what it did to the tab and by which rule, so "why is
// this tab still open" is answered in the store.
func TestTheSettlementSaysWhatItDidToTheTab(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.Launcher = &fakeLauncher{}
	b.ChildLinger = func() time.Duration { return 3 * time.Minute }
	id := "7ab00030-0000-4000-8000-000000000030"
	settledChild(t, b, ctx, id, "tmux", "%61", StateTimeout, nil)
	body, _ := latestEvent(t, b, ctx, "task.timeout", id)
	tab, _ := body["tab"].(map[string]any)
	if tab["rule"] != TabRuleUnfinished || tab["close"] != false {
		t.Fatalf("the settlement's event says %v", body)
	}
}

// CHILD.md states the rule before the work starts, for each way it can end.
func TestTheBriefingSaysWhatTheEndDoesToTheTab(t *testing.T) {
	b, _ := newTestBroker(t)
	b.ChildLinger = func() time.Duration { return 3 * time.Minute }
	r := Record{ID: "7ab00040-0000-4000-8000-000000000040", Title: "t", TimeoutMinutes: 30}
	brief := b.ChildBrief(r, "/tmp")
	for _, want := range []string{
		"## What happens to this tab when the task ends",
		"`orchestrator_child_linger` = 180 seconds",
		"- success: closed about 180 seconds after it ends (`child_linger`);",
		"- timeout: left open (`unfinished_left_open`);",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("CHILD.md does not say %q", want)
		}
	}
	r.ScheduleID, r.ScheduleTitle, r.ScheduleCloseTab = "5c000000-0000-4000-8000-000000000003", "daily", "never"
	brief = b.ChildBrief(r, "/tmp")
	if !strings.Contains(brief, "`close_tab: never`") || !strings.Contains(brief, "- success: left open (`schedule_never`);") {
		t.Errorf("a scheduled CHILD.md does not state its close_tab:\n%s", brief)
	}
}

// An iTerm2 close that was not answered is recorded as unknown — not done, not
// failed — and is not tried again. One iTerm2 close is made per pass, and none
// for a minute after one went unanswered, so a busy iTerm2 holds the beat
// once rather than once per tab.
func TestAnUnansweredITermCloseIsUnknownAndQuietsTheNext(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 19, 6, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	launcher := &itermChildCloser{answer: terminal.Unconfirmed{Why: "iTerm2 did not answer in time."}}
	b.Launcher = launcher
	b.ChildLinger = func() time.Duration { return 0 }
	b.lingerStarted = now.Add(-time.Minute) // a broker that was already running
	first := "7ab00050-0000-4000-8000-000000000050"
	second := "7ab00051-0000-4000-8000-000000000051"
	settledChild(t, b, ctx, first, "iterm", "GUID-A", StateSuccess, nil)
	settledChild(t, b, ctx, second, "iterm", "GUID-B", StateSuccess, nil)
	now = now.Add(time.Second)
	both := aReading(false, idleClaude("GUID-A"), idleClaude("GUID-B"))

	if n := b.closeLingers(ctx, both); n != 0 || len(launcher.closes()) != 1 {
		t.Fatalf("closed %d, asked %+v: want one unanswered close", n, launcher.closes())
	}
	asked := launcher.closes()[0].id
	subject := map[string]string{"GUID-A": first, "GUID-B": second}[asked]
	effects, _ := b.Store.Effects(ctx, EffectCloseChild, subject)
	if len(effects) != 1 || effects[0].State != store.EffectUnknown {
		t.Fatalf("the unanswered close was recorded as %+v", effects)
	}
	b.closeLingers(ctx, both)
	if len(launcher.closes()) != 1 {
		t.Fatal("another iTerm2 close was made while iTerm2 was not answering")
	}
	now = now.Add(itermCloseQuiet + time.Second)
	launcher.answer = nil
	if n := b.closeLingers(ctx, both); n != 1 || len(launcher.closes()) != 2 || launcher.closes()[1].id == asked {
		t.Fatalf("after the quiet: closed %d, asked %+v", n, launcher.closes())
	}
}

// One iTerm2 close per pass, even when each answers: a close ends the child
// first and can hold the beat for seconds, so eight children finishing
// together are closed over eight passes rather than in one long one.
func TestOneITermCloseIsMadePerPass(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 19, 6, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	launcher := &itermChildCloser{}
	b.Launcher = launcher
	b.ChildLinger = func() time.Duration { return 0 }
	b.lingerStarted = now.Add(-time.Minute) // a broker that was already running
	settledChild(t, b, ctx, "7ab00070-0000-4000-8000-000000000070", "iterm", "GUID-C", StateSuccess, nil)
	settledChild(t, b, ctx, "7ab00071-0000-4000-8000-000000000071", "iterm", "GUID-D", StateSuccess, nil)
	now = now.Add(time.Second)
	both := aReading(false, idleClaude("GUID-C"), idleClaude("GUID-D"))
	if n := b.closeLingers(ctx, both); n != 1 || len(launcher.closes()) != 1 {
		t.Fatalf("first pass closed %d, asked %+v", n, launcher.closes())
	}
	if n := b.closeLingers(ctx, both); n != 1 || len(launcher.closes()) != 2 {
		t.Fatalf("second pass closed %d, asked %+v", n, launcher.closes())
	}
}

// A linger nothing can decide — a tab a reading never answers for — is let go
// a day after its deadline, with the reason, rather than asked about for ever.
func TestALingerNothingCanDecideIsLetGoAfterADay(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 19, 6, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	launcher := &fakeLauncher{}
	b.Launcher = launcher
	b.ChildLinger = func() time.Duration { return 0 }
	b.lingerStarted = now.Add(-time.Minute) // a broker that was already running
	id := "7ab00060-0000-4000-8000-000000000060"
	settledChild(t, b, ctx, id, "tmux", "%71", StateSuccess, nil)
	unanswered := reading{sessions: map[string]session.Session{"%1": {ID: "%1"}}, sources: map[string]bool{"tmux": false}}
	now = now.Add(time.Hour)
	b.closeLingers(ctx, unanswered)
	if _, owed := lingerOwed(t, b, ctx, id); !owed {
		t.Fatal("let go within the day")
	}
	now = now.Add(lingerGiveUp)
	b.closeLingers(ctx, unanswered)
	if _, owed := lingerOwed(t, b, ctx, id); owed {
		t.Fatal("still owed a day after its deadline")
	}
	if _, said := latestEvent(t, b, ctx, "task.child.linger.expired", id); !said || len(launcher.closed) != 0 {
		t.Fatalf("expired without its event, or closed: %v", launcher.closed)
	}
}
