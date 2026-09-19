package http

import (
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
)

// A task of this daemon's is tied to the session in its child terminal only
// when the broker's own evidence says so: that terminal, the task's
// assistant, and a process that was running between the tab's opening and
// the task's end. A later process in the same pane, or another assistant, is
// somebody else — the controls, each of which a looser rule would pass.
func TestAnOwnTaskIsTiedOnlyToItsOwnProcess(t *testing.T) {
	r := orchestrator.Record{ID: "t1", Assistant: "claude", State: orchestrator.StateSuccess, ChildTerminalID: "%3",
		CreatedAt: time.Unix(990, 0), SpawnedAt: time.Unix(1000, 0), FinishedAt: time.Unix(2000, 0),
		Landing: &orchestrator.Landing{State: orchestrator.LandingPending}}
	live := swiftstore.Live{TerminalID: "%3", TTY: "ttys003", Assistant: "claude", PID: 9,
		ProcessStart: time.Unix(1001, 0), ConversationID: "c"}

	got := ownTasks([]orchestrator.Record{r}, map[string]swiftstore.Live{"%3": live})[0]
	if !got.TranscriptProven || got.ChildPID == nil || *got.ChildPID != 9 || *got.ChildSession != "c" {
		t.Fatalf("the task's own child was not tied to it: %+v", got)
	}
	if got.Landing != nil {
		t.Fatalf("a pending landing was carried twice (the broker's obligations carry it, D01): %+v", got.Landing)
	}
	if got.ResultVerifiedAt == nil {
		t.Fatal("a finished task reads as one without a result")
	}

	for name, change := range map[string]func(*swiftstore.Live){
		"a process started after the task ended": func(l *swiftstore.Live) { l.ProcessStart = time.Unix(3000, 0) },
		"a process older than the tab":           func(l *swiftstore.Live) { l.ProcessStart = time.Unix(500, 0) },
		"another assistant":                      func(l *swiftstore.Live) { l.Assistant = "codex" },
		"an unbound conversation":                func(l *swiftstore.Live) { l.ConversationID = "" },
	} {
		l := live
		change(&l)
		got := ownTasks([]orchestrator.Record{r}, map[string]swiftstore.Live{"%3": l})[0]
		if got.TranscriptProven || got.ChildPID != nil {
			t.Errorf("%s was taken for the task's child", name)
		}
	}
}

// The coordination plane names sessions by conversation; a row is a terminal.
// A conversation exactly one terminal holds is named by it, and one two
// terminals claim names neither.
func TestOwnWaitsAreNamedByTheirTerminal(t *testing.T) {
	lives := []swiftstore.Live{
		{TerminalID: "%1", ConversationID: "owner"},
		{TerminalID: "%2", ConversationID: "waiter"},
		{TerminalID: "%3", ConversationID: "twice"},
		{TerminalID: "%4", ConversationID: "twice"},
	}
	w := store.WaitRow{ID: "w", Owner: "owner", CreatedAt: time.Unix(100, 0), Waiters: []store.WaiterRow{
		{Waiter: "waiter", CreatedAt: time.Unix(100, 0)},
		{Waiter: "twice", CreatedAt: time.Unix(100, 0)},
		{Waiter: "gone", CreatedAt: time.Unix(100, 0), CancelledAt: time.Unix(200, 0)},
	}}
	got := ownWaits([]store.WaitRow{w}, conversationTerminals(lives))[0]
	if got.OwnerSessionID != "%1" || got.Waiters[0].SessionID != "%2" || got.Waiters[1].SessionID != "twice" {
		t.Fatalf("names: owner %s, waiters %s %s", got.OwnerSessionID, got.Waiters[0].SessionID, got.Waiters[1].SessionID)
	}
	if got.Waiters[0].ReleaseDeliveredAt != nil || got.Waiters[2].ReleaseDeliveredAt == nil {
		t.Fatalf("an open waiter reads released, or a cancelled one waits: %+v", got.Waiters)
	}
}
