package work

import (
	"errors"
	"testing"
	"time"
)

// The board's rules, as pure functions. Every automatic rule has both halves:
// the fact missing and nothing moving, the fact arriving and the item moving.
// The three broken shapes of board-redesign §5.4 (#3a, #3b, #3c) each have a
// test that runs the rules twice — as they are, and with the one rule that
// prevents the shape taken out — so that the same run shows the test can go
// red (DG-8).

var bt0 = time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

func policy() Policy {
	p := DefaultPolicy()
	p.Location = time.UTC
	return p
}

func boardItem(state ItemState) Item {
	return Item{ID: "w1", Project: "/p", Title: "the work", CreatedAt: bt0, CreatedBy: "user",
		Place: PlaceBoard, State: state, Owner: "root-conv", Commitment: CommitAssigned,
		Since: bt0, EvidenceAt: bt0, PlacedAt: bt0}
}

func backlogItem() Item {
	return Item{ID: "w1", Project: "/p", Title: "the work", CreatedAt: bt0, CreatedBy: "user",
		Place: PlaceBacklog, State: ItemPlanned, PlacedAt: bt0}
}

func task(id string, created time.Time, state, landing string) TaskFacts {
	f := TaskFacts{Task: id, WorkID: "w1", CreatedAt: created, State: state, Owner: "root-conv",
		Ended: state != "queued" && state != "spawning" && state != "briefed", Landing: landing}
	if f.Ended {
		f.FinishedAt = created.Add(time.Hour)
	}
	if landing == "landed" || landing == "nothing_to_land" {
		f.LandedAt = created.Add(2 * time.Hour)
	}
	return f
}

// without is the rules with one taken out: the control group.
func rulesWithout(code string) []SweepRule {
	out := []SweepRule{}
	for _, r := range SweepRules() {
		if r.Code != code {
			out = append(out, r)
		}
	}
	return out
}

func TestDeriveReadsTheFactsAndNothingElse(t *testing.T) {
	p := policy()
	now := bt0.Add(24 * time.Hour)
	cases := []struct {
		name   string
		item   Item
		tasks  []TaskFacts
		state  ItemState
		reason string
		landed bool
	}{
		{"nothing dispatched", boardItem(ItemActive), nil, ItemActive, ReasonNoDispatch, false},
		{"a task runs", boardItem(ItemActive), []TaskFacts{task("a", bt0, "briefed", "")}, ItemActive, ReasonTaskRunning, false},
		{"delivered, not landed", boardItem(ItemActive), []TaskFacts{task("a", bt0, "success", "pending")}, ItemAwaitingClosure, ReasonDeliveredUnlanded, false},
		{"delivered, landing abandoned", boardItem(ItemActive), []TaskFacts{task("a", bt0, "success", "abandoned")}, ItemAwaitingClosure, ReasonDeliveredUnlanded, false},
		{"landed", boardItem(ItemActive), []TaskFacts{task("a", bt0, "success", "landed")}, ItemDone, ClosedLanded, true},
		{"one landed, a retry failed", boardItem(ItemActive), []TaskFacts{task("a", bt0, "failure", ""), task("b", bt0.Add(time.Minute), "success", "landed")}, ItemDone, ClosedLanded, true},
		{"one landed, one delivered and not", boardItem(ItemActive), []TaskFacts{task("a", bt0, "success", "landed"), task("b", bt0, "success", "pending")}, ItemAwaitingClosure, ReasonDeliveredUnlanded, false},
		{"every attempt failed", boardItem(ItemActive), []TaskFacts{task("a", bt0, "timeout", "")}, ItemActive, ReasonAttemptsFailed, false},
		{"dropped by a person", func() Item { i := boardItem(ItemDropped); i.ClosedReason, i.ClosedAt = ClosedDropped, bt0; return i }(), []TaskFacts{task("a", bt0, "briefed", "")}, ItemDropped, ClosedDropped, false},
		{"in the Backlog", backlogItem(), []TaskFacts{task("a", bt0.Add(-time.Hour), "briefed", "")}, ItemPlanned, ReasonPlanned, false},
	}
	for _, c := range cases {
		d := Derive(c.item, c.tasks, p, now)
		if d.State != c.state || d.Reason != c.reason || d.Landed != c.landed {
			t.Errorf("%s: got %s/%s landed=%v, want %s/%s landed=%v", c.name, d.State, d.Reason, d.Landed, c.state, c.reason, c.landed)
		}
	}
}

// A task that ended before a rework belongs to the earlier stay: the item
// does not fall straight back into the closure queue because of it.
func TestAReworkStartsANewStay(t *testing.T) {
	p := policy()
	it := boardItem(ItemAwaitingClosure)
	tasks := []TaskFacts{task("a", bt0, "success", "pending")}
	c, err := Decide(it, tasks, Command{Op: OpRework, Actor: "user"}, p, bt0.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	next := c.Apply(it, bt0.Add(3*time.Hour))
	if d := Derive(next, tasks, p, bt0.Add(4*time.Hour)); d.State != ItemActive || d.Reason != ReasonNoDispatch {
		t.Fatalf("after a rework: %s/%s", d.State, d.Reason)
	}
	if _, ok := Next(SweepRules(), next, tasks, p, bt0.Add(4*time.Hour)); ok {
		t.Fatalf("a rework was undone by the delivery it rejected")
	}
}

// #3a 瑣碎無人管理: an item nobody's facts touch is not rewritten by the
// machine. The rules answer no change for an item whose facts did not
// change, however often they are asked, so a sweep writes nothing — the old
// board wrote 4,124 reconciliations, 75.5% of them planning⇄execution.
// And the rule that sends a quiet item back, taken out, leaves a stalled item
// on a person's board for ever: the other half of #3a.
func TestBrokenShape3aNothingSitsOnTheBoardUnmanaged(t *testing.T) {
	p := policy()
	it := boardItem(ItemActive)
	// Asked a hundred times inside the window, with nothing new: nothing.
	for h := 0; h < 70; h++ {
		if c, ok := Next(SweepRules(), it, nil, p, bt0.Add(time.Duration(h)*time.Hour)); ok {
			t.Fatalf("hour %d: the rules moved an item nothing touched: %+v", h, c)
		}
	}
	// Past the window it leaves the board, once, and says why.
	late := bt0.Add(p.Stall + time.Minute)
	c, ok := Next(SweepRules(), it, nil, p, late)
	if !ok || c.To != PlaceBacklog || c.Trigger != TriggerStalled || c.Actor != ActorRule || c.Evidence["stall_at"] == nil {
		t.Fatalf("a quiet item past the window: %+v %v", c, ok)
	}
	moved := c.Apply(it, late)
	// And in the Backlog nothing brings it back that was there before.
	for h := 0; h < 24*30; h += 6 {
		if c, ok := Next(SweepRules(), moved, nil, p, late.Add(time.Duration(h)*time.Hour)); ok {
			t.Fatalf("the Backlog item moved again with no new fact: %+v", c)
		}
	}
	// Control: without the stall rule the item stays on the board for ever.
	if _, ok := Next(rulesWithout(TriggerStalled), it, nil, p, bt0.Add(30*24*time.Hour)); ok {
		t.Fatal("control: something else moves a stalled item; the test above does not rest on the stall rule")
	}
}

// #3b 開始後沒人收尾、看不到進度: a dispatch that names the work item brings it
// onto the board — its progress is on the item, not on a twin card — and a
// commitment that goes quiet for three days goes back to the Backlog rather
// than sitting as "in progress". A running task keeps it where it is: only
// facts decide, never a declared span (there is no span input at all).
func TestBrokenShape3bADispatchBindsAndSilenceIsSaidOutLoud(t *testing.T) {
	p := policy()
	it := backlogItem()
	// A task bound before the item was placed in the Backlog does not count:
	// it is why it came back, not a new commitment.
	old := task("old", bt0.Add(-time.Hour), "failure", "")
	if _, ok := Next(SweepRules(), it, []TaskFacts{old}, p, bt0.Add(time.Minute)); ok {
		t.Fatal("an old task brought a Backlog item back")
	}
	fresh := task("fresh", bt0.Add(time.Hour), "briefed", "")
	fresh.Owner = "root-conv"
	c, ok := Next(SweepRules(), it, []TaskFacts{old, fresh}, p, bt0.Add(time.Hour+time.Second))
	if !ok || c.To != PlaceBoard || c.Trigger != TriggerDispatched || c.Actor != "root:root-conv" ||
		c.Commitment != CommitDispatch || c.Owner != "root-conv" || c.Evidence["task"] != "fresh" {
		t.Fatalf("a dispatch naming the item: %+v %v", c, ok)
	}
	on := c.Apply(it, bt0.Add(time.Hour+time.Second))
	tasks := []TaskFacts{old, fresh}
	if d := Derive(on, tasks, p, bt0.Add(2*time.Hour)); d.State != ItemActive || d.Reason != ReasonTaskRunning ||
		d.Tasks.Live != 1 || d.Tasks.Total != 2 {
		t.Fatalf("the item shows its progress: %+v", d)
	}
	// While the task runs, no clock moves it, however long it runs.
	if c, ok := Next(SweepRules(), on, tasks, p, bt0.Add(10*24*time.Hour)); ok {
		t.Fatalf("a running task's item moved: %+v", c)
	}
	// The task fails; three days later the item goes back, and says so.
	failed := fresh
	failed.State, failed.Ended, failed.FinishedAt = "failure", true, bt0.Add(2*time.Hour)
	tasks = []TaskFacts{old, failed}
	if _, ok := Next(SweepRules(), on, tasks, p, bt0.Add(2*time.Hour+p.Stall-time.Minute)); ok {
		t.Fatal("moved before the window")
	}
	c, ok = Next(SweepRules(), on, tasks, p, bt0.Add(2*time.Hour+p.Stall+time.Minute))
	if !ok || c.Trigger != TriggerStalled || c.To != PlaceBacklog {
		t.Fatalf("a silent commitment: %+v %v", c, ok)
	}
	// Control: without the dispatch rule the work's progress never reaches
	// the item a person reads — the 12.1% binding of the old board.
	if _, ok := Next(rulesWithout(TriggerDispatched), it, []TaskFacts{old, fresh}, p, bt0.Add(2*time.Hour)); ok {
		t.Fatal("control: something else binds a dispatch; the test above does not rest on the dispatch rule")
	}
}

// #3c 做完了但看板沒更新: the broker's landing record closes the item, with
// nobody pressing anything; a delivery with no landing goes to the closure
// queue, where a person can accept it — and accepting never says landed.
func TestBrokenShape3cALandingClosesTheItem(t *testing.T) {
	p := policy()
	it := boardItem(ItemActive)
	delivered := task("a", bt0, "success", "pending")
	c, ok := Next(SweepRules(), it, []TaskFacts{delivered}, p, bt0.Add(2*time.Hour))
	if !ok || c.State != ItemAwaitingClosure || c.Trigger != TriggerDelivered || c.Actor != ActorBroker {
		t.Fatalf("a delivery: %+v %v", c, ok)
	}
	waiting := c.Apply(it, bt0.Add(2*time.Hour))
	landed := task("a", bt0, "success", "landed")
	c, ok = Next(SweepRules(), waiting, []TaskFacts{landed}, p, bt0.Add(3*time.Hour))
	if !ok || c.State != ItemDone || c.ClosedReason != ClosedLanded || c.Trigger != TriggerLanded || c.Actor != ActorBroker {
		t.Fatalf("a landing: %+v %v", c, ok)
	}
	done := c.Apply(waiting, bt0.Add(3*time.Hour))
	if _, ok := Next(SweepRules(), done, []TaskFacts{landed}, p, bt0.Add(30*24*time.Hour)); ok {
		t.Fatal("a landed item moved again")
	}
	// An item that landed straight from active closes in one step too.
	if c, ok := Next(SweepRules(), it, []TaskFacts{landed}, p, bt0.Add(3*time.Hour)); !ok || c.ClosedReason != ClosedLanded {
		t.Fatalf("landed from active: %+v %v", c, ok)
	}
	// Control: without the landing rule the item waits for ever — the old
	// board's `closed` 0 of 787.
	if c, ok := Next(rulesWithout(TriggerLanded), waiting, []TaskFacts{landed}, p, bt0.Add(30*24*time.Hour)); ok && c.ClosedReason == ClosedLanded {
		t.Fatal("control: something else closes a landed item; the test above does not rest on the landing rule")
	}
}

// D31: a person's decision is evidence, and never the broker's. Accepting a
// delivery closes the item as accepted — the item is not landed, and the
// derived state says so; there is no command that lands anything.
func TestAPersonCannotMarkWhatDidNotLandAsLanded(t *testing.T) {
	p := policy()
	it := boardItem(ItemAwaitingClosure)
	tasks := []TaskFacts{task("a", bt0, "success", "pending")}
	c, err := Decide(it, tasks, Command{Op: OpAccept, Actor: "user"}, p, bt0.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if c.State != ItemDone || c.ClosedReason != ClosedAccepted || c.Evidence["landed"] != false {
		t.Fatalf("accept: %+v", c)
	}
	accepted := c.Apply(it, bt0.Add(4*time.Hour))
	if d := Derive(accepted, tasks, p, bt0.Add(5*time.Hour)); d.Landed || d.Reason != ClosedAccepted {
		t.Fatalf("an accepted delivery reads as landed: %+v", d)
	}
	for _, word := range []string{"land", "landed", "mark_landed", "set_state", "close", "complete", "transition"} {
		_, err := ParseOp(word)
		var r *Refusal
		if !errors.As(err, &r) || r.Code != "landing_is_broker_fact" || r.Status != 422 {
			t.Errorf("%q answered %v", word, err)
		}
	}
	// Nothing is accepted while it is still being done, or twice.
	running := []TaskFacts{task("a", bt0, "briefed", "")}
	if _, err := Decide(boardItem(ItemActive), running, Command{Op: OpAccept, Actor: "user"}, p, bt0); !refusedAs(err, "not_awaiting_closure") {
		t.Fatalf("accept while running: %v", err)
	}
	if _, err := Decide(accepted, tasks, Command{Op: OpAccept, Actor: "user"}, p, bt0.Add(6*time.Hour)); !refusedAs(err, "already_closed") {
		t.Fatalf("accept twice: %v", err)
	}
	// A later landing of the same delivery does not rewrite the person's
	// decision into the broker's: the item stays accepted, and the task's
	// own landing is on the task.
	later := []TaskFacts{task("a", bt0, "success", "landed")}
	if c, ok := Next(SweepRules(), accepted, later, p, bt0.Add(7*time.Hour)); ok {
		t.Fatalf("a landing rewrote an accepted item: %+v", c)
	}
}

func refusedAs(err error, code string) bool {
	var r *Refusal
	return errors.As(err, &r) && r.Code == code
}

// A closed item reopens on a new dispatch, and only on one.
func TestANewDispatchReopensAClosedItem(t *testing.T) {
	p := policy()
	done := boardItem(ItemDone)
	done.ClosedReason, done.ClosedAt = ClosedLanded, bt0.Add(3*time.Hour)
	landed := task("a", bt0, "success", "landed")
	if _, ok := Next(SweepRules(), done, []TaskFacts{landed}, p, bt0.Add(4*time.Hour)); ok {
		t.Fatal("a closed item moved with no new task")
	}
	again := task("b", bt0.Add(5*time.Hour), "briefed", "")
	c, ok := Next(SweepRules(), done, []TaskFacts{landed, again}, p, bt0.Add(5*time.Hour))
	if !ok || c.State != ItemActive || c.Trigger != TriggerRedispatch || c.Actor != "root:root-conv" {
		t.Fatalf("a new dispatch: %+v %v", c, ok)
	}
	open := c.Apply(done, bt0.Add(5*time.Hour))
	if open.ClosedReason != "" || !open.ClosedAt.IsZero() || !open.Since.Equal(bt0.Add(3*time.Hour)) {
		t.Fatalf("reopened: %+v", open)
	}
	// The old landing belongs to the earlier stay: the new task decides.
	if d := Derive(open, []TaskFacts{landed, again}, p, bt0.Add(6*time.Hour)); d.State != ItemActive || d.Tasks.Total != 2 {
		t.Fatalf("after reopening: %+v", d)
	}
}

// Asked and unanswered, a delivery closes as unconfirmed after the wait — and
// not before anybody was asked (T4 asks; until it does, nothing closes this
// way).
func TestAnUnansweredDeliveryClosesOnlyAfterSomebodyWasAsked(t *testing.T) {
	p := policy()
	it := boardItem(ItemAwaitingClosure)
	tasks := []TaskFacts{task("a", bt0, "success", "pending")}
	if _, ok := Next(SweepRules(), it, tasks, p, bt0.Add(60*24*time.Hour)); ok {
		t.Fatal("closed without anybody being asked")
	}
	it.AskedAt = bt0.Add(3 * 24 * time.Hour)
	if _, ok := Next(SweepRules(), it, tasks, p, it.AskedAt.Add(p.ClosureWait-time.Minute)); ok {
		t.Fatal("closed before the wait")
	}
	c, ok := Next(SweepRules(), it, tasks, p, it.AskedAt.Add(p.ClosureWait+time.Minute))
	if !ok || c.ClosedReason != ClosedUnconfirmed || c.Actor != ActorRule {
		t.Fatalf("after the wait: %+v %v", c, ok)
	}
	if d := Derive(c.Apply(it, it.AskedAt.Add(p.ClosureWait+time.Minute)), tasks, p, it.AskedAt.Add(p.ClosureWait+time.Hour)); d.Landed {
		t.Fatal("an unconfirmed delivery reads as landed")
	}
}

// A start date inside the short window is a commitment; a missed one hands the
// item back without bouncing it.
func TestAStartDateCommitsAndAMissedOneComesBack(t *testing.T) {
	p := policy()
	it := backlogItem()
	it.StartOn = "2026-09-30" // twelve days out
	if _, ok := Next(SweepRules(), it, nil, p, bt0); ok {
		t.Fatal("a date outside the window moved the item")
	}
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	c, ok := Next(SweepRules(), it, nil, p, now)
	if !ok || c.To != PlaceBoard || c.Commitment != CommitScheduled || c.Trigger != TriggerStartSoon {
		t.Fatalf("inside the window: %+v %v", c, ok)
	}
	on := c.Apply(it, now)
	if s, shown := SectionOf(on, Derive(on, nil, p, now), p, now); s != SectionScheduled || !shown {
		t.Fatalf("section %s %v", s, shown)
	}
	// The day after the date, nothing dispatched: still on the board.
	if _, ok := Next(SweepRules(), on, nil, p, time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)); ok {
		t.Fatal("missed a day early")
	}
	missedAt := time.Date(2026, 10, 2, 1, 0, 0, 0, time.UTC)
	c, ok = Next(SweepRules(), on, nil, p, missedAt)
	if !ok || c.Trigger != TriggerMissed || c.To != PlaceBacklog {
		t.Fatalf("missed: %+v %v", c, ok)
	}
	back := c.Apply(on, missedAt)
	if back.StartOn != "" {
		t.Fatalf("the spent date stayed: %q", back.StartOn)
	}
	if c, ok := Next(SweepRules(), back, nil, p, missedAt.Add(time.Hour)); ok {
		t.Fatalf("a missed date bounced the item: %+v", c)
	}
}

// A person's commands move the item only from where they apply.
func TestCommandsApplyOnlyWhereTheyMeanSomething(t *testing.T) {
	p := policy()
	now := bt0.Add(time.Hour)
	bl := backlogItem()
	c, err := Decide(bl, nil, Command{Op: OpStart, Actor: "user", Owner: "root-conv"}, p, now)
	if err != nil || c.To != PlaceBoard || c.Owner != "root-conv" || c.Commitment != CommitAssigned {
		t.Fatalf("start: %+v %v", c, err)
	}
	if _, err := Decide(boardItem(ItemActive), nil, Command{Op: OpStart, Actor: "user"}, p, now); !refusedAs(err, "already_on_board") {
		t.Fatalf("start on the board: %v", err)
	}
	if c, err := Decide(boardItem(ItemActive), nil, Command{Op: OpDefer, Actor: "user"}, p, now); err != nil ||
		c.To != PlaceBacklog || c.State != ItemPlanned {
		t.Fatalf("defer: %+v %v", c, err)
	}
	if c, err := Decide(boardItem(ItemActive), nil, Command{Op: OpUntrack, Actor: "user"}, p, now); err != nil || c.To != PlaceTodo {
		t.Fatalf("untrack: %+v %v", c, err)
	}
	todo := boardItem(ItemActive)
	todo = Change{To: PlaceTodo}.Apply(todo, now)
	if c, err := Decide(todo, nil, Command{Op: OpTrack, Actor: "user"}, p, now); err != nil || c.To != PlaceBoard || c.Owner != OwnerUser {
		t.Fatalf("track: %+v %v", c, err)
	}
	if c, err := Decide(bl, nil, Command{Op: OpDrop, Actor: "user"}, p, now); err != nil || c.State != ItemDropped || c.To != PlaceBacklog {
		t.Fatalf("drop from the Backlog: %+v %v", c, err)
	}
	if c, err := Decide(boardItem(ItemActive), nil, Command{Op: OpDrop, Actor: "user"}, p, now); err != nil || c.ClosedReason != ClosedDropped {
		t.Fatalf("drop from the board: %+v %v", c, err)
	}
	if _, err := Decide(boardItem(ItemActive), nil, Command{Op: OpHandover, Actor: "user"}, p, now); !refusedAs(err, "owner_required") {
		t.Fatalf("handover with nobody: %v", err)
	}
	rank := int64(3)
	if c, err := Decide(bl, nil, Command{Op: OpRank, Actor: "user", Rank: &rank}, p, now); err != nil || *c.Rank != 3 || !c.Reviewed {
		t.Fatalf("rank: %+v %v", c, err)
	}
	if c, err := Decide(bl, nil, Command{Op: OpSchedule, Actor: "user", StartOn: "2026-12-01"}, p, now); err != nil || c.To != PlaceBacklog || *c.StartOn != "2026-12-01" {
		t.Fatalf("schedule far: %+v %v", c, err)
	}
	if c, err := Decide(bl, nil, Command{Op: OpSchedule, Actor: "user", StartOn: "2026-09-20"}, p, now); err != nil || c.To != PlaceBoard || c.Commitment != CommitScheduled {
		t.Fatalf("schedule near: %+v %v", c, err)
	}
	if _, err := Decide(bl, nil, Command{Op: OpSchedule, Actor: "user", StartOn: "next week"}, p, now); !refusedAs(err, "invalid_start_on") {
		t.Fatalf("a date that is not one: %v", err)
	}
	if _, err := ParseOp("frobnicate"); !refusedAs(err, "unknown_operation") {
		t.Fatalf("unknown: %v", err)
	}
}
