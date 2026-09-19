package work

import (
	"strings"
	"testing"
	"time"
)

// The to-do rules, each transition from both sides (board-redesign §9 step 2):
// the fact has not arrived and nothing moves; the fact arrives and it moves,
// every time. Each case sits beside its control under the same conditions, so
// a rule that moves everything, or nothing, fails one of the two.

var todoT0 = time.Unix(1_789_700_000, 0)

func facts(mod func(*TaskFacts)) TaskFacts {
	f := TaskFacts{Task: "t1", Title: "work", Kind: "custom", Project: "/repo",
		Owner: "conv-root", OwnerAssistant: "claude", CreatedAt: todoT0, State: "briefed"}
	if mod != nil {
		mod(&f)
	}
	return f
}

func openTodo(t *testing.T) Todo {
	t.Helper()
	todo, ok := DispatchTodo(facts(nil), todoT0)
	if !ok || todo.State != TodoStateOpen || todo.Reason != ReasonDispatched {
		t.Fatalf("a live dispatched task leaves %+v, %v", todo, ok)
	}
	return todo
}

func TestADispatchLeavesItsRootOneTodo(t *testing.T) {
	todo := openTodo(t)
	if todo.ID != "dispatch:t1" || todo.Owner != "conv-root" || todo.Task != "t1" {
		t.Fatalf("todo = %+v", todo)
	}
	// Control: a task with no root — detached automation — owes nobody.
	if _, ok := DispatchTodo(facts(func(f *TaskFacts) { f.Owner = "" }), todoT0); ok {
		t.Fatal("a task with no root left a to-do")
	}
	// The same fact names the same to-do: applying it twice is one to-do.
	again, _ := DispatchTodo(facts(nil), todoT0.Add(time.Hour))
	if again.ID != todo.ID {
		t.Fatalf("the same dispatch named two to-dos: %s, %s", todo.ID, again.ID)
	}
}

func TestEachClosingFactClosesAndNothingElseDoes(t *testing.T) {
	cases := []struct {
		name   string
		before func(*TaskFacts) // the fact has not arrived
		after  func(*TaskFacts) // it has
		reason string
	}{
		{"landed", func(f *TaskFacts) { f.State, f.Ended, f.Landing = "success", true, "pending" },
			func(f *TaskFacts) { f.State, f.Ended, f.Landing = "success", true, "landed" }, ReasonLanded},
		{"nothing_to_land", func(f *TaskFacts) { f.State, f.Ended, f.Landing = "success", true, "pending" },
			func(f *TaskFacts) { f.State, f.Ended, f.Landing = "success", true, "nothing_to_land" }, ReasonNothingToLand},
		{"abandoned", func(f *TaskFacts) { f.State, f.Ended, f.Landing = "failure", true, "pending" },
			func(f *TaskFacts) { f.State, f.Ended, f.Landing = "failure", true, "abandoned" }, ReasonAbandoned},
		{"ended owing nothing", func(f *TaskFacts) { f.State = "briefed" },
			func(f *TaskFacts) { f.State, f.Ended = "success", true }, ReasonNothingOwed},
		{"a child that died owing nothing", func(f *TaskFacts) { f.State = "spawning" },
			func(f *TaskFacts) { f.State, f.Ended = "timeout", true }, ReasonNothingOwed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			todo := openTodo(t)
			if next, tr := Follow(todo, facts(c.before), todoT0.Add(time.Minute)); tr != nil || next.State != TodoStateOpen {
				t.Fatalf("before the fact: %+v %+v", next, tr)
			}
			next, tr := Follow(todo, facts(c.after), todoT0.Add(time.Minute))
			if tr == nil || next.State != TodoStateDone || next.Reason != c.reason || next.ClosedAt.IsZero() {
				t.Fatalf("after the fact: %+v %+v", next, tr)
			}
			// The fact again: nothing moves a second time.
			if again, tr := Follow(next, facts(c.after), todoT0.Add(2*time.Minute)); tr != nil || again != next {
				t.Fatalf("the fact twice moved it again: %+v", tr)
			}
		})
	}
}

// A task whose child died with a landing owed is still owed: the death is
// not a landing (board-redesign §9: "a child dies").
func TestAChildThatDiedOwingALandingStaysOwed(t *testing.T) {
	todo := openTodo(t)
	died := facts(func(f *TaskFacts) { f.State, f.Ended, f.Landing = "timeout", true, "pending" })
	if next, tr := Follow(todo, died, todoT0.Add(time.Hour)); tr != nil || next.State != TodoStateOpen {
		t.Fatalf("a dead child's pending landing closed its to-do: %+v", next)
	}
}

// The order the facts arrive in is not a fact: the answer is the same for
// every order (board-redesign §9: "the landing arrives before the result").
func TestTheOrderOfFactsDoesNotMatter(t *testing.T) {
	type step struct {
		ended   bool
		landing string
	}
	// Every order the broker admits a task's end and its landing record in.
	// A settled landing needs an ended task, and never moves back to pending.
	orders := [][]step{
		{{false, "pending"}, {true, "pending"}, {true, "landed"}},                   // named while running
		{{true, "pending"}, {true, "landed"}},                                       // opened by the settlement
		{{true, ""}, {true, "pending"}, {true, "landed"}},                           // opened after it ended owing none
		{{true, ""}, {true, "landed"}},                                              // recorded landed straight away
		{{false, "pending"}, {true, "pending"}, {true, "landed"}, {true, "landed"}}, // and sent twice
	}
	for i, order := range orders {
		todo := openTodo(t)
		for _, s := range order {
			f := facts(func(f *TaskFacts) {
				f.Ended, f.Landing = s.ended, s.landing
				if s.ended {
					f.State = "success"
				}
			})
			todo, _ = Follow(todo, f, todoT0.Add(time.Minute))
		}
		if todo.State != TodoStateDone || todo.Reason != ReasonLanded {
			t.Fatalf("order %d ends %s/%s", i, todo.State, todo.Reason)
		}
	}
}

func TestAnObligationOpenedAfterClosingReopens(t *testing.T) {
	todo := openTodo(t)
	ended := facts(func(f *TaskFacts) { f.State, f.Ended = "success", true })
	done, _ := Follow(todo, ended, todoT0.Add(time.Minute))
	// Control: the same facts again leave it closed.
	if next, tr := Follow(done, ended, todoT0.Add(2*time.Minute)); tr != nil || next.State != TodoStateDone {
		t.Fatalf("closed without a new fact reopened: %+v", next)
	}
	owing := facts(func(f *TaskFacts) { f.State, f.Ended, f.Landing = "success", true, "pending" })
	next, tr := Follow(done, owing, todoT0.Add(3*time.Minute))
	if tr == nil || next.State != TodoStateOpen || next.Reason != ReasonObligationOpened || !next.ClosedAt.IsZero() {
		t.Fatalf("a landing obligation opened after closing left %+v", next)
	}
	// A to-do closed by a landing never reopens: that landing is settled.
	landed, _ := Follow(todo, facts(func(f *TaskFacts) { f.State, f.Ended, f.Landing = "success", true, "landed" }),
		todoT0.Add(time.Minute))
	if again, tr := Follow(landed, owing, todoT0.Add(4*time.Minute)); tr != nil || again.State != TodoStateDone {
		t.Fatalf("a landed to-do reopened: %+v", again)
	}
}

func TestAnOwnerPositivelyGoneHandsItOff(t *testing.T) {
	todo := openTodo(t)
	for _, l := range []Liveness{Live, Unknown} {
		if next, tr := Tend(todo, l, todoT0.Add(time.Hour)); tr != nil || next.State != TodoStateOpen {
			t.Fatalf("an owner read as %s handed it off: %+v", l, next)
		}
	}
	next, tr := Tend(todo, Gone, todoT0.Add(time.Hour))
	if tr == nil || next.State != TodoStateHandedOff || next.Reason != ReasonOwnerGone ||
		!next.HandedOffAt.Equal(todoT0.Add(time.Hour)) || next.HandedTo != "" {
		t.Fatalf("an owner positively gone left %+v", next)
	}
	if !next.State.Outstanding() || next.State.Bucket() != TodoLive {
		t.Fatal("a handed-off to-do is still owed")
	}
}

func TestAnOwnerWhoComesBackTakesItBack(t *testing.T) {
	gone, _ := Tend(openTodo(t), Gone, todoT0.Add(time.Hour))
	for _, l := range []Liveness{Gone, Unknown} {
		if next, tr := Tend(gone, l, todoT0.Add(2*time.Hour)); tr != nil || next.State != TodoStateHandedOff {
			t.Fatalf("an owner read as %s took it back: %+v", l, next)
		}
	}
	next, tr := Tend(gone, Live, todoT0.Add(2*time.Hour))
	if tr == nil || next.State != TodoStateOpen || next.Reason != ReasonOwnerReturned || !next.HandedOffAt.IsZero() {
		t.Fatalf("an owner back again left %+v", next)
	}
}

func TestNobodyTakingItForTheGraceDropsIt(t *testing.T) {
	at := todoT0.Add(time.Hour)
	gone, _ := Tend(openTodo(t), Gone, at)
	// Not yet: a minute short of the grace.
	if next, tr := Tend(gone, Gone, at.Add(UnownedGrace-time.Minute)); tr != nil || next.State != TodoStateHandedOff {
		t.Fatalf("dropped before the grace: %+v", next)
	}
	// Past the grace, but nobody can say the owner is still gone: kept (DG-7).
	if next, tr := Tend(gone, Unknown, at.Add(UnownedGrace+time.Hour)); tr != nil || next.State != TodoStateHandedOff {
		t.Fatalf("dropped on an unknown reading: %+v", next)
	}
	next, tr := Tend(gone, Gone, at.Add(UnownedGrace))
	if tr == nil || next.State != TodoStateDropped || next.Reason != ReasonUnowned || next.ClosedAt.IsZero() {
		t.Fatalf("past the grace, still gone, left %+v", next)
	}
	if next.State.Bucket() != TodoDone {
		t.Fatal("a dropped to-do is not over")
	}
	// A landing that arrives afterwards is truer than "nobody took it".
	landed, tr := Follow(next, facts(func(f *TaskFacts) { f.State, f.Ended, f.Landing = "success", true, "landed" }),
		at.Add(UnownedGrace+time.Hour))
	if tr == nil || landed.State != TodoStateDone || landed.Reason != ReasonLanded {
		t.Fatalf("a landing after the drop left %+v", landed)
	}
	// And presence never moves a to-do that is over.
	for _, l := range []Liveness{Live, Gone, Unknown} {
		if _, tr := Tend(landed, l, at.Add(48*time.Hour)); tr != nil {
			t.Fatalf("presence %s moved a closed to-do", l)
		}
	}
}

func TestAClosingFactOutranksAHandoff(t *testing.T) {
	gone, _ := Tend(openTodo(t), Gone, todoT0.Add(time.Hour))
	next, tr := Follow(gone, facts(func(f *TaskFacts) { f.State, f.Ended, f.Landing = "success", true, "abandoned" }),
		todoT0.Add(2*time.Hour))
	if tr == nil || next.State != TodoStateDone || next.Reason != ReasonAbandoned {
		t.Fatalf("a handed-off to-do whose landing settled is %+v", next)
	}
}

func TestEscalationIsOnlyReported(t *testing.T) {
	todo := openTodo(t)
	f := facts(nil)
	got := Escalation(todo, f, todoT0.Add(time.Hour))
	if len(got) != 1 || got[0] != SignalCrossSession {
		t.Fatalf("an unbound dispatch carries %v, want cross_session", got)
	}
	// Bound to a work item: it is tracked already, nothing to ask.
	bound := todo
	bound.WorkID = "0f0f0f0f-0000-4000-8000-000000000001"
	if got := Escalation(bound, f, todoT0.Add(time.Hour)); len(got) != 0 {
		t.Fatalf("a bound to-do carries %v", got)
	}
	// I2 only past the line.
	if got := Escalation(bound, f, todoT0.Add(LongLived)); len(got) != 0 {
		t.Fatalf("at exactly the line: %v", got)
	}
	if got := Escalation(bound, f, todoT0.Add(LongLived+time.Second)); len(got) != 1 || got[0] != SignalLongLived {
		t.Fatalf("past the line: %v", got)
	}
	// A second failed attempt, and its control: the first.
	failed := facts(func(f *TaskFacts) { f.State, f.Ended, f.Landing, f.Attempt = "spawn_failed", true, "pending", 1 })
	if got := Escalation(bound, failed, todoT0.Add(time.Hour)); len(got) != 1 || got[0] != SignalRepeatedFailure {
		t.Fatalf("a respawn that failed too carries %v", got)
	}
	first := failed
	first.Attempt = 0
	if got := Escalation(bound, first, todoT0.Add(time.Hour)); len(got) != 0 {
		t.Fatalf("a first failure carries %v", got)
	}
	// Never for a step of other work, and never for a to-do that is over.
	for _, kind := range []string{"review", "code-review", "test_fix", "correction", "question", "clarification"} {
		if got := Escalation(todo, facts(func(f *TaskFacts) { f.Kind = kind }), todoT0.Add(48*time.Hour)); len(got) != 0 {
			t.Fatalf("kind %q carries %v", kind, got)
		}
	}
	if got := Escalation(todo, facts(func(f *TaskFacts) { f.Kind = "reviewer-onboarding" }), todoT0.Add(time.Hour)); len(got) == 0 {
		t.Fatal("a kind that only contains a step's letters was read as a step")
	}
	done, _ := Follow(todo, facts(func(f *TaskFacts) { f.State, f.Ended = "success", true }), todoT0.Add(time.Minute))
	if got := Escalation(done, f, todoT0.Add(48*time.Hour)); len(got) != 0 {
		t.Fatalf("a closed to-do carries %v", got)
	}
}

// The to-do vocabulary is the tracks' vocabulary: the projection of old cards
// and the broker's own list put a to-do in the same place.
func TestTodoStatesLandOnTheTodoTrack(t *testing.T) {
	for _, s := range TodoStates {
		if s.Bucket().Track() != TrackTodo {
			t.Fatalf("%s lands on %s", s, s.Bucket().Track())
		}
	}
	if strings.Join([]string{string(TodoStateOpen.Bucket()), string(TodoStateDropped.Bucket())}, ",") !=
		"todo.live,todo.done" {
		t.Fatal("owed is live and over is done")
	}
}
