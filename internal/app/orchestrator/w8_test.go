package orchestrator

import (
	"context"
	"testing"
	"time"
)

// W8: the routes a machine needs before the Swift app can be stopped — the
// landing list and a root's own notification, as far as the broker decides
// them.

// The landing list names every pending landing once, oldest first, with its
// write set and who owes it; an undecodable record refuses the list rather
// than shortening it.
func TestTheLandingListNamesEveryPendingLanding(t *testing.T) {
	b, ctx := newTestBroker(t)
	older := "e8000001-0000-4000-8000-000000000001"
	newer := "e8000002-0000-4000-8000-000000000002"
	quiet := "e8000003-0000-4000-8000-000000000003"
	for i, id := range []string{older, newer, quiet} {
		claims := []string{"a.go"}
		if id == quiet {
			// Nothing reserved and no checkout: nothing to land.
			claims = []string{}
		}
		r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "t", State: StateBriefed,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second), Claims: claims,
			Root: &RootRef{SessionID: rootConversation, Assistant: "claude", Label: "root"}}
		if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Settle(ctx, id, StateSuccess, "done", nil); err != nil {
			t.Fatal(err)
		}
	}
	rd := LandingReading{Processes: true, Sessions: []LandingSession{
		{TerminalID: "%1", Assistant: "claude", ConversationID: rootConversation, WorkState: "working"},
	}}
	rows, err := b.PendingLandings(ctx, rd)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Record.ID != older || rows[1].Record.ID != newer {
		ids := []string{}
		for _, r := range rows {
			ids = append(ids, r.Record.ID)
		}
		t.Fatalf("rows %v, want the two that owe a landing, oldest first", ids)
	}
	first := rows[0]
	if len(first.Paths) != 1 || first.Paths[0] != "a.go" || first.RootKey == "" {
		t.Fatalf("paths %v root key %q", first.Paths, first.RootKey)
	}
	if first.Ownership.Subject != "root" || first.Ownership.Status != OwnershipObservedWorking ||
		first.Ownership.Reason != "exact_root_observation" || first.Ownership.WorkState != "working" {
		t.Fatalf("ownership %+v", first.Ownership)
	}

	// Control: the same list with one record nobody can decode is not a
	// shorter list. Read by a broker that has not decoded it before — a
	// restarted daemon — because this one keeps the rows it has decoded.
	corrupt(t, b.Dir, newer)
	restarted := &Broker{Store: b.Store, Tasks: b.Tasks, Dir: b.Dir}
	if _, err := restarted.PendingLandings(ctx, rd); refusalCode(err) != "landings_incomplete" {
		t.Fatalf("an unreadable record: %v, want landings_incomplete", err)
	}
}

// Who owes a landing is placed by an exact match only, and "not running" is
// said only where the reading can prove it.
func TestALandingsOwnerIsPlacedOnlyOnExactEvidence(t *testing.T) {
	root := &RootRef{SessionID: rootConversation, Assistant: "claude"}
	done := Record{ID: "r", State: StateSuccess, Root: root}
	running := Record{ID: "x", State: StateBriefed, Assistant: "claude", ChildTerminalID: "%7", Root: root}
	here := LandingSession{TerminalID: "%1", Assistant: "claude", ConversationID: rootConversation, WorkState: "ready"}
	other := LandingSession{TerminalID: "%2", Assistant: "claude", ConversationID: "0ther000-0000-4000-8000-000000000000", WorkState: "working"}
	child := LandingSession{TerminalID: "%7", Assistant: "claude", ConversationID: "c41d0000-0000-4000-8000-000000000000", WorkState: "waiting_you"}
	cases := []struct {
		name           string
		r              Record
		rd             LandingReading
		status, reason string
	}{
		{"root here, idle", done, LandingReading{Sessions: []LandingSession{here, other}}, OwnershipObservedReady, "exact_root_observation"},
		{"root twice", done, LandingReading{Sessions: []LandingSession{here, here}}, OwnershipUnknown, "root_observation_ambiguous"},
		{"root absent, table complete, all named", done, LandingReading{Processes: true, Sessions: []LandingSession{other}}, OwnershipNotObserved, "root_absent_from_complete_inventory"},
		// Controls for the one absence verdict: each missing condition turns it back to unknown.
		{"root absent, table incomplete", done, LandingReading{Sessions: []LandingSession{other}}, OwnershipUnknown, "session_inventory_incomplete"},
		{"root absent, someone unnamed", done, LandingReading{Processes: true, Anonymous: true, Sessions: []LandingSession{other}}, OwnershipUnknown, "session_inventory_incomplete"},
		{"no root recorded", Record{ID: "n", State: StateSuccess}, LandingReading{Processes: true}, OwnershipUnknown, "root_identity_missing"},
		{"executor seen", running, LandingReading{Sessions: []LandingSession{child}}, OwnershipObservedOther, "exact_executor_observation"},
		{"executor not seen", running, LandingReading{Processes: true, Sessions: []LandingSession{other}}, OwnershipTaskStillLive, "live_task_without_exact_executor_observation"},
	}
	for _, c := range cases {
		got := LandingOwnershipOf(c.r, c.rd)
		if got.Status != c.status || got.Reason != c.reason {
			t.Errorf("%s: %s/%s, want %s/%s", c.name, got.Status, got.Reason, c.status, c.reason)
		}
	}
}

// A root's own notification: the person's switch, the fields, a machine with
// nothing subscribed, and the one hourly allowance it shares with every
// child's.
func TestARootsNotificationSpendsTheSameHourlyAllowance(t *testing.T) {
	b, ctx := newTestBroker(t)
	if _, err := b.MachineNotify(ctx, "t", "b", ""); refusalCode(err) != "not_subscribed" {
		t.Fatalf("no push at all: %v", err)
	}
	var pushed []string
	b.Push = func(_ context.Context, title, _, terminal, tag string) (int, int, error) {
		pushed = append(pushed, title+"|"+terminal+"|"+tag)
		return 1, 0, nil
	}
	if _, err := b.MachineNotify(ctx, "", "b", ""); refusalCode(err) != "bad_request" {
		t.Fatalf("an empty title: %v", err)
	}
	out, err := b.MachineNotify(ctx, "landed", "W8 is on main", "%4")
	if err != nil || out.Sent != 1 {
		t.Fatalf("sent %+v, %v", out, err)
	}
	if len(pushed) != 1 || pushed[0] != "Clawdline: landed|%4|agent-root" {
		t.Fatalf("pushed %v", pushed)
	}
	perTask, perHour, err := b.Store.NotificationCounts(ctx, machineNotifyTask)
	if err != nil || perTask != 1 || perHour != 1 {
		t.Fatalf("recorded under no task: %d, this hour %d, %v", perTask, perHour, err)
	}

	// Twenty-nine more from children fill the hour; the root's next is refused.
	for i := 0; i < notifyHourLimit-1; i++ {
		if _, _, err := b.Store.RecordNotification(ctx, "e8000009-0000-4000-8000-000000000009", "t", "b"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.MachineNotify(ctx, "t", "b", ""); refusalCode(err) != "rate_limited" {
		t.Fatalf("a full hour: %v", err)
	}

	// The switch is asked first, of both routes.
	b.NotifyEnabled = func() bool { return false }
	if _, err := b.MachineNotify(ctx, "t", "b", ""); refusalCode(err) != "agent_notify_disabled" {
		t.Fatalf("switched off, root: %v", err)
	}
	id := "e800000a-0000-4000-8000-00000000000a"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "t", State: StateBriefed,
		CreatedAt: time.Now(), Claims: []string{}, Root: &RootRef{SessionID: rootConversation, Assistant: "claude"}}
	if err := b.save(ctx, r, HashSecret("s"), "task.queued"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AgentNotify(ctx, id, "s", "t", "b"); refusalCode(err) != "agent_notify_disabled" {
		t.Fatalf("switched off, child: %v", err)
	}
	if len(pushed) != 1 {
		t.Fatalf("pushed while refused: %v", pushed)
	}
}
