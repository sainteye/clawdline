package swiftstore

import (
	"testing"
	"time"
)

var (
	ownChildLive = Live{TerminalID: "%5", TTY: "ttys005", Assistant: "claude", PID: 50,
		ProcessStart: time.Unix(1000, 0), ConversationID: "conv-child"}
	ownRootLive = Live{TerminalID: "%6", TTY: "ttys006", Assistant: "claude", PID: 60,
		ProcessStart: time.Unix(900, 0), ConversationID: "conv-root"}
)

func ownCloseInput(l Live) CloseInput {
	return CloseInput{Live: l, TerminalState: "idle", Bound: true, Matches: 1, InventoryComplete: true,
		InventoryObservedAt: time.Now(), Now: time.Now()}
}

func strp(v string) *string { return &v }

// ownFinishedTask is a task of this daemon's, finished, whose child is
// ownChildLive and whose root is ownRootLive.
func ownFinishedTask() Task {
	fin, start, pid := Seconds(2000), Seconds(1000), int64(50)
	return Task{ID: "abcd1234", State: "success", Title: "Own task", Assistant: "claude", Created: 990,
		FinishedAt: &fin, ResultVerifiedAt: &fin,
		ChildTerminal: strp("%5"), ChildTTY: strp("ttys005"), ChildPID: &pid, ChildProcStart: &start,
		ChildSession: strp("conv-child"), TranscriptProven: true,
		RootSession: strp("conv-root"), RootAssistant: strp("claude")}
}

// With the Swift store switched off, this daemon's own records alone give the
// child its delivery tick and its tab's name, and the store is not "unknown".
// The control is the same records over an unreadable store, which stays
// unknown.
func TestOwnRecordsAloneDrawTheRow(t *testing.T) {
	base := Snapshot{Source: SourceDisabled, CoordinatorKnown: true}
	snap := base.With(Own{Tasks: []Task{ownFinishedTask()}})

	w := snap.Work(ownChildLive, "idle")
	if w.State != "milestone_complete" || w.Disposition == nil || w.Disposition.Scope != "task" ||
		w.Disposition.TaskID != "abcd1234" || w.Disposition.Title != "Own task" {
		t.Fatalf("the child's delivery is not drawn from its own task: %+v %+v", w, w.Disposition)
	}
	if got := snap.TitleOf(ownChildLive, "", []Live{ownChildLive, ownRootLive}).Orchestrator; got != "Own task" {
		t.Fatalf("the tab is not named by its task: %q", got)
	}
	for _, r := range snap.Closeability(ownCloseInput(ownChildLive)).Reasons {
		if r.Code == "swift_store_unreadable" {
			t.Fatal("a switched-off store was read as unknown")
		}
	}

	unread := Snapshot{Source: SourceUnreadable}.With(Own{Tasks: []Task{ownFinishedTask()}})
	if c := unread.Closeability(ownCloseInput(ownChildLive)); c.State != "unknown" {
		t.Fatalf("control: an unreadable store with own records is %s", c.State)
	}

	// A live child keeps its root waiting and holds the root open.
	running := ownFinishedTask()
	running.ID, running.State, running.FinishedAt, running.ResultVerifiedAt = "live0001", "briefed", nil, nil
	snap = base.With(Own{Tasks: []Task{running}})
	if w := snap.Work(ownRootLive, "idle"); w.State != "waiting_session" {
		t.Fatalf("a root with a live child of its own reads %s", w.State)
	}
	c := snap.Closeability(ownCloseInput(ownRootLive))
	found := false
	for _, r := range c.Reasons {
		found = found || (r.Code == "live_descendant_task" && r.SubjectID == "live0001")
	}
	if c.State != "blocked" || !found {
		t.Fatalf("the root's live child does not hold it open: %s %v", c.State, c.Reasons)
	}
	// Control: without the overlay the same root owes nothing.
	if w := base.Work(ownRootLive, "idle"); w.State == "waiting_session" {
		t.Fatal("control: a root with no records is waiting")
	}
}

// A delivery the session reported here is its tick; one reported by another
// conversation in the same terminal is not. A wait registered here parks the
// waiter and is drawn on both rows.
func TestAnOwnDeliveryAndAnOwnWaitAreDrawn(t *testing.T) {
	base := Snapshot{Source: SourceAbsent, CoordinatorKnown: true}
	start, pid := Seconds(1000), int64(50)
	delivery := SessionDelivery{Identity: Identity{TerminalID: "%5", TTY: "ttys005", Assistant: strp("claude"),
		PID: &pid, ProcessStart: &start, ConversationID: strp("conv-child")}, Summary: "shipped", ReportedAt: 3000}

	w := base.With(Own{Deliveries: []SessionDelivery{delivery}}).Work(ownChildLive, "idle")
	if w.State != "milestone_complete" || w.Disposition == nil || w.Disposition.Scope != "session" ||
		w.Disposition.Title != "shipped" {
		t.Fatalf("the session's own delivery is not its tick: %+v", w)
	}
	other := delivery
	other.ConversationID = strp("somebody-else")
	if w := base.With(Own{Deliveries: []SessionDelivery{other}}).Work(ownChildLive, "idle"); w.State == "milestone_complete" {
		t.Fatal("control: another conversation's delivery was drawn on this row")
	}

	wait := CoordinationWait{ID: "w1", Repository: "/r", Paths: []string{"a.go"}, OwnerSessionID: "%6", Created: 100,
		Waiters: []CoordinationWaiter{{SessionID: "%5", Reason: "needs a.go", Created: 100}}}
	snap := base.With(Own{Waits: []CoordinationWait{wait}})
	if w := snap.Work(ownChildLive, "idle"); w.State != "waiting_session" {
		t.Fatalf("the waiter reads %s", w.State)
	}
	label := func(id string) string { return map[string]string{"%5": "child", "%6": "root"}[id] }
	if c := snap.Coordination("%5", label); c == nil || c.State != "waiting_on_session" || c.WaitingOn[0].OwnerLabel != "root" {
		t.Fatalf("the waiter's coordination: %+v", c)
	}
	if c := snap.Coordination("%6", label); c == nil || c.State != "has_waiters" || c.WaitedOnBy[0].WaiterLabel != "child" {
		t.Fatalf("the owner's coordination: %+v", c)
	}
	released := wait
	released.Waiters = []CoordinationWaiter{{SessionID: "%5", Created: 100, ReleaseDeliveredAt: &start}}
	if c := base.With(Own{Waits: []CoordinationWait{released}}).Coordination("%5", label); c != nil {
		t.Fatalf("control: a released waiter still waits: %+v", c)
	}
}

// The merge never writes into the reading it was given: that reading is a
// cached one, shared with every other caller.
func TestWithLeavesTheReadingAlone(t *testing.T) {
	tasks := make([]Task, 1, 4)
	tasks[0] = Task{ID: "swift"}
	base := Snapshot{Orchestrator: Orchestrator{Tasks: tasks}, Known: true}
	merged := base.With(Own{Tasks: []Task{{ID: "own"}, {ID: "swift"}}})
	if len(merged.Tasks) != 2 || merged.Tasks[1].ID != "own" {
		t.Fatalf("merged tasks: %+v", merged.Tasks)
	}
	if len(base.Tasks) != 1 || tasks[:2][1].ID == "own" {
		t.Fatal("the merge wrote into the cached reading's array")
	}
}
