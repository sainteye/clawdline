package orchestrator

import (
	"errors"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/session"
	"github.com/sainteye/clawdline-go/internal/domain/task"
)

// D01 for closeability (W4): what a session still owes is read off the
// broker's landing record. A root whose child delivered into the shared tree
// reads blocked until that landing is recorded, and reads safe the moment it
// is — with nothing copied anywhere in between. Before W4 the session list and
// the close action read the `obligations` table, which no broker task ever
// wrote, so the same root read safe from the start.
func TestARootOwesItsChildsPendingLanding(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "d4000001-0000-4000-8000-000000000001"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "t", State: StateBriefed,
		CreatedAt: time.Now(), Claims: []string{"a.go"},
		Root: &RootRef{SessionID: rootConversation, Assistant: "claude"}}
	if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	root := session.Session{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation}
	other := session.Session{ID: "%2", Assistant: session.AssistantClaude, ConversationID: "0ther000-0000-4000-8000-000000000000"}
	machine := []session.Session{root, other}

	// Running, it owes nothing yet: delivered is not the question until it is.
	if owed, err := b.Owed(ctx, machine); err != nil || len(owed) != 0 {
		t.Fatalf("a running child: %+v (%v)", owed, err)
	}
	if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}
	owed, err := b.Owed(ctx, machine)
	if err != nil || len(owed) != 1 || owed[0].Kind != task.KindLanding || owed[0].Subject != id ||
		owed[0].Mover != (task.Mover{Kind: task.MoverThisSession, ID: "%1"}) {
		t.Fatalf("a delivered child: %+v (%v)", owed, err)
	}
	if c := task.Closeability("%1", owed, err); c.State != task.CloseBlocked || len(c.Reasons) != 1 {
		t.Fatalf("the root reads %s with %d reason(s), want blocked by the landing", c.State, len(c.Reasons))
	}
	// The control: the session beside it owes nothing, from the same list.
	if c := task.Closeability("%2", owed, err); c.State != task.CloseSafe {
		t.Fatalf("another session reads %s", c.State)
	}

	if _, err := b.Land(ctx, id, LandingRequest{State: string(LandingAbandoned), Note: "superseded", Machine: true}); err != nil {
		t.Fatal(err)
	}
	owed, err = b.Owed(ctx, machine)
	if err != nil || len(owed) != 0 {
		t.Fatalf("after the landing was recorded: %+v (%v)", owed, err)
	}
	if c := task.Closeability("%1", owed, err); c.State != task.CloseSafe {
		t.Fatalf("the root still reads %s", c.State)
	}
}

// A landing whose root is in no terminal is still owed, by that conversation;
// a scheduled run's landing is owed by a person, because no session can be
// asked for it. Neither blocks a session it does not belong to.
func TestAPendingLandingNamesWhoMustMove(t *testing.T) {
	b, ctx := newTestBroker(t)
	owned := "d4000002-0000-4000-8000-000000000002"
	scheduled := "d4000003-0000-4000-8000-000000000003"
	for _, r := range []Record{
		{Protocol: Protocol, ID: owned, Assistant: "claude", Title: "t", State: StateBriefed,
			CreatedAt: time.Now(), Claims: []string{"a.go"},
			Root: &RootRef{SessionID: rootConversation, Assistant: "claude"}},
		{Protocol: Protocol, ID: scheduled, Assistant: "claude", Title: "t", State: StateBriefed,
			CreatedAt: time.Now(), Claims: []string{"b.go"}, ScheduleID: "5c000001-0000-4000-8000-000000000001"},
	} {
		if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Settle(ctx, r.ID, StateSuccess, "done", nil); err != nil {
			t.Fatal(err)
		}
	}
	owed, err := b.Owed(ctx, nil)
	if err != nil || len(owed) != 2 {
		t.Fatalf("owed: %+v (%v)", owed, err)
	}
	movers := map[string]task.Mover{}
	for _, o := range owed {
		movers[o.Subject] = o.Mover
	}
	if movers[owned] != (task.Mover{Kind: task.MoverThisSession, ID: rootConversation}) {
		t.Errorf("an absent root's landing is owed by %+v", movers[owned])
	}
	if movers[scheduled] != (task.Mover{Kind: task.MoverPerson}) {
		t.Errorf("a scheduled run's landing is owed by %+v", movers[scheduled])
	}
	if c := task.Closeability("%1", owed, nil); c.State != task.CloseSafe {
		t.Errorf("a terminal that proves neither reads %s", c.State)
	}
}

// A ledger with a row it cannot decode cannot say what is owed: that row may
// be the landing. The answer is unknown, never "owes nothing" (DG-7).
func TestAnUnreadableRowMakesWhatIsOwedUnknown(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "d4000004-0000-4000-8000-000000000004"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "t", State: StateBriefed,
		CreatedAt: time.Now(), Claims: []string{"a.go"},
		Root: &RootRef{SessionID: rootConversation, Assistant: "claude"}}
	if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	corrupt(t, b.Dir, id)
	owed, err := b.Owed(ctx, nil)
	var incomplete ErrOwedIncomplete
	if !errors.As(err, &incomplete) || incomplete.Unreadable != 1 || owed != nil {
		t.Fatalf("owed %+v, err %v; want the incomplete answer", owed, err)
	}
	if c := task.Closeability("%1", owed, err); c.State != task.CloseUnknown {
		t.Fatalf("closeability over an incomplete ledger: %s", c.State)
	}
}
