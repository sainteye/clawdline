package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// waitingRig is a Waiting on a real store, with a clock it moves by hand and a
// Run that counts what it was handed instead of pushing.
type waitingRig struct {
	t   *testing.T
	st  *store.Store
	dir string
	now time.Time
	w   *Waiting
	ran []int64
}

func newWaitingRig(t *testing.T) *waitingRig {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	r := &waitingRig{t: t, st: st, dir: dir, now: time.Date(2026, 9, 21, 11, 0, 0, 0, time.UTC)}
	r.w = r.watcher()
	return r
}

// watcher is a fresh Waiting on the same store: what a restarted daemon has.
func (r *waitingRig) watcher() *Waiting {
	return &Waiting{Store: r.st, Now: func() time.Time { return r.now },
		Run: func(_ context.Context, ids []int64) { r.ran = append(r.ran, ids...) }}
}

func (r *waitingRig) at(d time.Duration, rows ...session.Session) int {
	r.t.Helper()
	r.now = time.Date(2026, 9, 21, 11, 0, 0, 0, time.UTC).Add(d)
	n, err := r.w.Observe(context.Background(), session.Inventory{Sessions: rows, Complete: true,
		Sources: map[string]bool{"iterm": true, "tmux": true}})
	if err != nil {
		r.t.Fatalf("observe at %s: %v", d, err)
	}
	return n
}

// pushes is every push recorded for a session, from the store.
func (r *waitingRig) pushes(s session.Session) []store.Effect {
	r.t.Helper()
	got, err := r.st.Effects(context.Background(), orchestrator.EffectWaitingPush, WaitingKey(s))
	if err != nil {
		r.t.Fatal(err)
	}
	return got
}

func (r *waitingRig) phase(s session.Session) string {
	r.t.Helper()
	latest, err := r.st.LatestEvents(context.Background(), WaitingEvent, []string{WaitingKey(s)})
	if err != nil {
		r.t.Fatal(err)
	}
	e, ok := latest[WaitingKey(s)]
	if !ok {
		return ""
	}
	return waitingPhase(e)
}

func asking(id string) session.Session {
	return session.Session{ID: id, Backend: session.BackendITerm, Assistant: session.AssistantClaude,
		ConversationID: "conv-" + id, Label: "root " + id, State: session.StateWaiting,
		Menu: &session.Menu{Question: "要用哪一個？"}}
}

func doing(s session.Session, state session.State) session.Session {
	s.State, s.Menu = state, nil
	return s
}

// The rule itself: nothing while a person could still be about to answer,
// one push when the question has stood ten minutes, and nothing again on any
// sweep after — one stop, one notification.
func TestAQuestionIsPushedOnceWhenItHasStoodTenMinutes(t *testing.T) {
	r := newWaitingRig(t)
	s := asking("A")
	for _, d := range []time.Duration{0, time.Minute, 9*time.Minute + 59*time.Second} {
		if n := r.at(d, s); n != 0 {
			t.Fatalf("pushed at %s, before the question had stood ten minutes", d)
		}
	}
	if n := r.at(10*time.Minute, s); n != 1 {
		t.Fatalf("at ten minutes pushed %d, want 1", n)
	}
	for d := 10*time.Minute + 15*time.Second; d < 3*time.Hour; d += 15 * time.Second {
		if n := r.at(d, s); n != 0 {
			t.Fatalf("pushed the same stop again at %s", d)
		}
	}
	got := r.pushes(s)
	if len(got) != 1 || len(r.ran) != 1 || r.ran[0] != got[0].ID {
		t.Fatalf("recorded %d pushes and ran %v, want the one", len(got), r.ran)
	}
	var p orchestrator.WaitingPush
	if err := json.Unmarshal(got[0].Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.Terminal != "A" || p.Tag != "waiting-A" || p.Title != "等你回答：root A" ||
		!strings.HasPrefix(p.Body, "停在一個問題上 10 分鐘了。") || !strings.Contains(p.Body, "要用哪一個？") {
		t.Fatalf("the push says %+v", p)
	}
	if ph := r.phase(s); ph != WaitingPushed {
		t.Fatalf("the stop is recorded %q, want pushed", ph)
	}
}

// What was pushed is in the store, so a daemon that comes back while the same
// question still stands reads it back instead of pushing it a second time —
// even though its own clock for the stop starts again from nothing.
func TestARestartDoesNotPushTheSameStopAgain(t *testing.T) {
	r := newWaitingRig(t)
	s := asking("A")
	r.at(0, s)
	if r.at(10*time.Minute, s) != 1 {
		t.Fatal("the first daemon did not push")
	}
	r.w = r.watcher()
	for d := 30 * time.Minute; d < 2*time.Hour; d += time.Minute {
		if n := r.at(d, s); n != 0 {
			t.Fatalf("the restarted daemon pushed the same stop at %s", d)
		}
	}
	if got := r.pushes(s); len(got) != 1 {
		t.Fatalf("%d pushes recorded, want 1", len(got))
	}
}

// A stop ends when a reading sees the session doing anything else, and the
// end is written down: the next question in the same session is a new stop,
// and it is pushed on its own ten minutes, restart or no restart.
func TestAnAnsweredQuestionEndsTheStopAndTheNextOneIsItsOwn(t *testing.T) {
	r := newWaitingRig(t)
	s := asking("A")
	r.at(0, s)
	r.at(10*time.Minute, s)
	r.at(12*time.Minute, doing(s, session.StateWorking))
	if ph := r.phase(s); ph != WaitingEnded {
		t.Fatalf("the answered stop is recorded %q, want ended", ph)
	}
	r.w = r.watcher()
	r.at(20*time.Minute, s)
	if n := r.at(29*time.Minute, s); n != 0 {
		t.Fatal("the second question was pushed before its own ten minutes")
	}
	if n := r.at(30*time.Minute, s); n != 1 {
		t.Fatal("the second question was never pushed")
	}
	if got := r.pushes(s); len(got) != 2 {
		t.Fatalf("%d pushes recorded, want 2", len(got))
	}
}

// Only a reading that could see a session ends its stop. `unknown` is not an
// answer, and a row missing from a reading that could not see its terminal is
// not a row gone — pushing again after either would be a second notification
// for a question nobody answered.
func TestWhatCannotBeSeenEndsNothing(t *testing.T) {
	r := newWaitingRig(t)
	s := asking("A")
	r.at(0, s)
	r.at(10*time.Minute, s)
	r.at(11*time.Minute, doing(s, session.StateUnknown))
	r.now = r.now.Add(time.Minute)
	if _, err := r.w.Observe(context.Background(), session.Inventory{Complete: false,
		Sources: map[string]bool{"tmux": true}}); err != nil {
		t.Fatal(err)
	}
	if ph := r.phase(s); ph != WaitingPushed {
		t.Fatalf("an unreadable stop was recorded %q", ph)
	}
	for d := 13 * time.Minute; d < 40*time.Minute; d += time.Minute {
		if n := r.at(d, s); n != 0 {
			t.Fatalf("pushed again at %s after a reading that saw nothing", d)
		}
	}
	// And a reading that could see iTerm, without the row, is the row gone.
	r.at(41 * time.Minute)
	if ph := r.phase(s); ph != WaitingEnded {
		t.Fatalf("a session gone from a complete reading is recorded %q, want ended", ph)
	}
}

// The budget: a wave of sessions stopping at once is one fact, and past the
// hour's allowance each further stop is written down over_budget and never
// pushed — not held and sent later, when it would be about something older.
func TestAWaveOfStopsIsHeldToTheHoursBudget(t *testing.T) {
	r := newWaitingRig(t)
	r.w.HourLimit = 2
	rows := []session.Session{asking("A"), asking("B"), asking("C")}
	r.at(0, rows...)
	if n := r.at(10*time.Minute, rows...); n != 2 {
		t.Fatalf("pushed %d, want the budget's 2", n)
	}
	over := 0
	for _, s := range rows {
		if r.phase(s) == WaitingOverBudget {
			over++
		}
	}
	if over != 1 {
		t.Fatalf("%d stops recorded over_budget, want 1", over)
	}
	for d := 11 * time.Minute; d < 3*time.Hour; d += 5 * time.Minute {
		if n := r.at(d, rows...); n != 0 {
			t.Fatalf("pushed %d at %s; an over-budget stop is never pushed later", n, d)
		}
	}
	// An hour on, a new stop has the allowance back.
	d := asking("D")
	r.at(3*time.Hour, append(rows, d)...)
	if n := r.at(3*time.Hour+10*time.Minute, append(rows, d)...); n != 1 {
		t.Fatalf("a new stop an hour later pushed %d, want 1", n)
	}
}

// A dispatched child's tab: while its task is going, the push says how long
// the task has left, which is the clock nobody watching that tab can see. Once
// its task is over, nobody is behind it to be blocked and it says nothing.
func TestAChildSaysHowLongItsTaskHasLeftAndAFinishedOneSaysNothing(t *testing.T) {
	r := newWaitingRig(t)
	start := r.now
	r.w.Role = func(_ context.Context, terminal string) (WaitingRole, bool) {
		switch terminal {
		case "live":
			return WaitingRole{Child: true, Live: true, Title: "修好通知", Deadline: start.Add(40 * time.Minute)}, true
		case "done":
			return WaitingRole{Child: true, Live: false, Title: "做完的"}, true
		}
		return WaitingRole{}, false
	}
	live, done := asking("live"), asking("done")
	r.at(0, live, done)
	if n := r.at(10*time.Minute, live, done); n != 1 {
		t.Fatalf("pushed %d, want the live child's 1", n)
	}
	var p orchestrator.WaitingPush
	_ = json.Unmarshal(r.pushes(live)[0].Payload, &p)
	if p.Title != "等你回答：修好通知" || !strings.Contains(p.Body, "這個 task 的時限還剩 30 分鐘") {
		t.Fatalf("the live child's push says %+v", p)
	}
	if len(r.pushes(done)) != 0 || r.phase(done) != WaitingSilent {
		t.Fatalf("the finished child's tab: %d pushes, recorded %q", len(r.pushes(done)), r.phase(done))
	}
}

// The person's switch. Off, nothing is decided, so a question still standing
// when it is turned back on is pushed then — not lost, and not pushed twice.
func TestTheSwitchHoldsThePushAndTurningItOnSendsIt(t *testing.T) {
	r := newWaitingRig(t)
	on := false
	r.w.Enabled = func() bool { return on }
	s := asking("A")
	r.at(0, s)
	if n := r.at(20*time.Minute, s); n != 0 || len(r.pushes(s)) != 0 {
		t.Fatal("pushed with the switch off")
	}
	on = true
	if n := r.at(21*time.Minute, s); n != 1 {
		t.Fatal("not pushed when the switch came back on")
	}
	if n := r.at(22*time.Minute, s); n != 0 {
		t.Fatal("pushed twice")
	}
}

// An override may only lower the rule's numbers, never raise them.
func TestTheWaitAndTheBudgetMayOnlyBeLowered(t *testing.T) {
	w := &Waiting{After: time.Hour, HourLimit: 100}
	if w.after() != maxUnseenWait || w.hourLimit() != waitingPushHourLimit {
		t.Fatalf("raised to %s and %d", w.after(), w.hourLimit())
	}
	w = &Waiting{After: time.Minute, HourLimit: 1}
	if w.after() != time.Minute || w.hourLimit() != 1 {
		t.Fatalf("lowered to %s and %d", w.after(), w.hourLimit())
	}
}

// The words stay inside the push's bounds whatever the session is called and
// whatever it asked.
func TestAStopsWordsFitThePush(t *testing.T) {
	s := asking("A")
	s.Label = strings.Repeat("很長的名字", 40)
	s.Menu = &session.Menu{Question: strings.Repeat("問題", 400)}
	title, body := waitingText(s, WaitingRole{}, false, 12*time.Minute, time.Now())
	if utf8.RuneCountInString(title) > 80 || utf8.RuneCountInString(body) > 500 {
		t.Fatalf("title %d and body %d characters", utf8.RuneCountInString(title), utf8.RuneCountInString(body))
	}
	s.Label, s.Menu = "", nil
	title, _ = waitingText(s, WaitingRole{}, false, 12*time.Minute, time.Now())
	if title != "等你回答：A" {
		t.Fatalf("a session with no name is titled %q", title)
	}
}

// faultyWaitingStore is the real store with its writes and reads failing on
// command: the failure injection for the two writes a stop depends on.
type faultyWaitingStore struct {
	*store.Store
	failIntent, failAppend, failRead bool
}

var errInjected = errors.New("injected store failure")

func (f *faultyWaitingStore) RecordIntent(ctx context.Context, ev []store.Event, ef []store.Effect) ([]int64, error) {
	if f.failIntent {
		return nil, errInjected
	}
	return f.Store.RecordIntent(ctx, ev, ef)
}

func (f *faultyWaitingStore) Append(ctx context.Context, e store.Event) error {
	if f.failAppend {
		return errInjected
	}
	return f.Store.Append(ctx, e)
}

func (f *faultyWaitingStore) LatestEvents(ctx context.Context, kind string, subjects []string) (map[string]store.Event, error) {
	if f.failRead {
		return nil, errInjected
	}
	return f.Store.LatestEvents(ctx, kind, subjects)
}

// A store that cannot say what was decided, or cannot write the decision
// down, decides nothing: the sweep answers the error, pushes nothing, and the
// next sweep that can write pushes the stop — once. A push sent without its
// record would be the one a restart sends again.
func TestAStopTheStoreCouldNotRecordIsPushedWhenItCanOnce(t *testing.T) {
	r := newWaitingRig(t)
	f := &faultyWaitingStore{Store: r.st, failRead: true}
	r.w.Store = f
	s := asking("A")
	r.at(0, s)
	r.now = r.now.Add(10 * time.Minute)
	observe := func() (int, error) {
		return r.w.Observe(context.Background(), session.Inventory{Sessions: []session.Session{s}, Complete: true})
	}
	if n, err := observe(); !errors.Is(err, errInjected) || n != 0 {
		t.Fatalf("an unreadable record: pushed %d, %v", n, err)
	}
	f.failRead, f.failIntent = false, true
	if n, err := observe(); !errors.Is(err, errInjected) || n != 0 || len(r.ran) != 0 {
		t.Fatalf("an unwritable record: pushed %d, ran %v, %v", n, r.ran, err)
	}
	f.failIntent = false
	if n, err := observe(); err != nil || n != 1 {
		t.Fatalf("once the store recovered: pushed %d, %v", n, err)
	}
	if n, _ := observe(); n != 0 || len(r.pushes(s)) != 1 {
		t.Fatalf("pushed again: %d recorded", len(r.pushes(s)))
	}
}

// An end that could not be written is written again by the next sweep. Were
// it dropped, the next question in that session would read as the old stop,
// already pushed, and never be pushed at all.
func TestAnEndTheStoreCouldNotRecordIsWrittenOnTheNextSweep(t *testing.T) {
	r := newWaitingRig(t)
	f := &faultyWaitingStore{Store: r.st}
	r.w.Store = f
	s := asking("A")
	r.at(0, s)
	r.at(10*time.Minute, s)
	f.failAppend = true
	r.now = r.now.Add(time.Minute)
	if _, err := r.w.Observe(context.Background(), session.Inventory{
		Sessions: []session.Session{doing(s, session.StateIdle)}, Complete: true}); !errors.Is(err, errInjected) {
		t.Fatalf("the failed end answered %v", err)
	}
	if ph := r.phase(s); ph != WaitingPushed {
		t.Fatalf("recorded %q before the end could be written", ph)
	}
	f.failAppend = false
	r.at(12*time.Minute, doing(s, session.StateIdle))
	if ph := r.phase(s); ph != WaitingEnded {
		t.Fatalf("the retried end is recorded %q", ph)
	}
	r.w = r.watcher()
	r.at(20*time.Minute, s)
	if n := r.at(30*time.Minute, s); n != 1 {
		t.Fatal("the next question was taken for the old stop")
	}
}
