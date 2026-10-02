package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// stuckClock is a clock a test moves by hand.
type stuckClock struct{ at time.Time }

func (c *stuckClock) now() time.Time { return c.at }

func stuckBroker(t *testing.T) (*Broker, context.Context, *stuckClock, *[]RootAssignment) {
	t.Helper()
	b, ctx := newTestBroker(t)
	clock := &stuckClock{at: time.Unix(1_790_000_000, 0)}
	b.Clock = clock.now
	settled := &[]RootAssignment{}
	b.RootAssignmentSettled = func(_ context.Context, a RootAssignment) { *settled = append(*settled, a) }
	return b, ctx, clock, settled
}

func seedAssignment(t *testing.T, b *Broker, ctx context.Context, id, state string) {
	t.Helper()
	a := RootAssignment{ID: id, State: state, Assistant: "claude", Label: "Board item",
		CreatedAt: b.now().Unix(), Executor: &openedSession{TerminalID: "%90"}}
	if err := b.createOpened(ctx, store.TableRootAssignments, id, state, a, "root_assignment.accepted", b.now()); err != nil {
		t.Fatal(err)
	}
}

func seedHandoff(t *testing.T, b *Broker, ctx context.Context, id, state string) {
	t.Helper()
	h := Handoff{ID: id, State: state, Assistant: "claude", CreatedAt: b.now().Unix()}
	if err := b.createOpened(ctx, store.TableHandoffs, id, state, h, "handoff.opening", b.now()); err != nil {
		t.Fatal(err)
	}
}

func assignmentState(t *testing.T, b *Broker, ctx context.Context, id string) RootAssignment {
	t.Helper()
	a, err := b.RootAssignmentByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func handoffState(t *testing.T, b *Broker, ctx context.Context, id string) Handoff {
	t.Helper()
	o, err := b.Store.ReadOpened(ctx, store.TableHandoffs, id)
	if err != nil {
		t.Fatal(err)
	}
	h, err := decodeHandoff(o)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func events(t *testing.T, b *Broker, ctx context.Context, kind string) int {
	t.Helper()
	n, err := b.Store.EventCount(ctx, kind)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// The row G6 found: a Root Assignment at terminal_opened since a daemon
// stopped mid-opening, and a handoff at opening for the same reason.
func TestAnOpeningLeftForMoreThanThirtyMinutesIsFailed(t *testing.T) {
	b, ctx, clock, settled := stuckBroker(t)
	seedAssignment(t, b, ctx, "a-stuck", AssignmentTerminalOpened)
	seedHandoff(t, b, ctx, "h-stuck", HandoffOpening)

	clock.at = clock.at.Add(30*time.Minute + time.Second)
	if p := b.Pass(ctx); p.StuckOpenings != 2 {
		t.Fatalf("the pass failed %d stuck openings, want 2", p.StuckOpenings)
	}

	a := assignmentState(t, b, ctx, "a-stuck")
	if a.State != AssignmentFailed || !strings.HasPrefix(a.Failure, ReasonTerminalOpenTimeout+": ") {
		t.Fatalf("the stuck assignment is %q (failure %q), want failed with %s", a.State, a.Failure, ReasonTerminalOpenTimeout)
	}
	if len(*settled) != 1 || (*settled)[0].ID != "a-stuck" || (*settled)[0].State != AssignmentFailed {
		t.Fatalf("the Board was told %+v, want the failed assignment once", *settled)
	}
	h := handoffState(t, b, ctx, "h-stuck")
	if h.State != HandoffSpawnFailed || !strings.HasPrefix(h.Failure, ReasonHandoffOpenTimeout+": ") {
		t.Fatalf("the stuck handoff is %q (failure %q), want spawn_failed with %s", h.State, h.Failure, ReasonHandoffOpenTimeout)
	}
	if n := events(t, b, ctx, "root_assignment."+ReasonTerminalOpenTimeout); n != 1 {
		t.Fatalf("%d terminal_open_timeout events, want 1", n)
	}
	if n := events(t, b, ctx, "handoff."+ReasonHandoffOpenTimeout); n != 1 {
		t.Fatalf("%d handoff_open_timeout events, want 1", n)
	}

	// A second sweep over rows already failed does nothing.
	clock.at = clock.at.Add(time.Hour)
	if p := b.Pass(ctx); p.StuckOpenings != 0 {
		t.Fatalf("a second pass failed %d openings again", p.StuckOpenings)
	}
	if n := events(t, b, ctx, "root_assignment."+ReasonTerminalOpenTimeout); n != 1 {
		t.Fatalf("a second pass recorded the timeout again: %d events", n)
	}
	if n := events(t, b, ctx, "handoff."+ReasonHandoffOpenTimeout); n != 1 {
		t.Fatalf("a second pass recorded the handoff timeout again: %d events", n)
	}
	if len(*settled) != 1 {
		t.Fatalf("a second pass told the Board again: %+v", *settled)
	}
}

func TestAnOpeningTwentyNineMinutesOldIsLeftAlone(t *testing.T) {
	b, ctx, clock, settled := stuckBroker(t)
	seedAssignment(t, b, ctx, "a-young", AssignmentTerminalOpened)
	seedHandoff(t, b, ctx, "h-young", HandoffOpening)

	clock.at = clock.at.Add(29 * time.Minute)
	if n := b.failStuckOpenings(ctx); n != 0 {
		t.Fatalf("%d openings failed at 29 minutes", n)
	}
	if a := assignmentState(t, b, ctx, "a-young"); a.State != AssignmentTerminalOpened || a.Failure != "" {
		t.Fatalf("a 29-minute assignment became %q (%q)", a.State, a.Failure)
	}
	if h := handoffState(t, b, ctx, "h-young"); h.State != HandoffOpening || h.Failure != "" {
		t.Fatalf("a 29-minute handoff became %q (%q)", h.State, h.Failure)
	}
	if len(*settled) != 0 {
		t.Fatalf("the Board was told about a row that is still opening: %+v", *settled)
	}
}

// A row that moved on, or that its opening request touched, is not
// overwritten: the age that counts is the row's last change, read inside the
// write.
func TestAnOpeningThatMovedOnIsNotOverwritten(t *testing.T) {
	b, ctx, clock, settled := stuckBroker(t)
	seedAssignment(t, b, ctx, "a-briefed", AssignmentTerminalOpened)
	seedAssignment(t, b, ctx, "a-touched", AssignmentTerminalOpened)
	seedHandoff(t, b, ctx, "h-delivered", HandoffOpening)

	clock.at = clock.at.Add(20 * time.Minute)
	if _, err := b.updateAssignment(ctx, "a-briefed", "root_assignment.briefed", func(x *RootAssignment) {
		x.State, x.BriefedAt = AssignmentBriefed, b.now().Unix()
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.updateAssignment(ctx, "a-touched", "root_assignment.briefing", func(x *RootAssignment) {
		x.BriefAttemptedAt = b.now().Unix()
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.updateHandoff(ctx, "h-delivered", "handoff.delivered", func(x *Handoff) {
		x.State, x.DeliveredAt = HandoffDelivered, b.now().Unix()
	}); err != nil {
		t.Fatal(err)
	}

	// 40 minutes after they were opened, 20 after they last changed.
	clock.at = clock.at.Add(20 * time.Minute)
	if n := b.failStuckOpenings(ctx); n != 0 {
		t.Fatalf("%d rows that moved on were failed", n)
	}
	if a := assignmentState(t, b, ctx, "a-briefed"); a.State != AssignmentBriefed || a.Failure != "" {
		t.Fatalf("a briefed assignment became %q (%q)", a.State, a.Failure)
	}
	if a := assignmentState(t, b, ctx, "a-touched"); a.State != AssignmentTerminalOpened || a.Failure != "" {
		t.Fatalf("an assignment touched 20 minutes ago became %q (%q)", a.State, a.Failure)
	}
	if h := handoffState(t, b, ctx, "h-delivered"); h.State != HandoffDelivered || h.Failure != "" {
		t.Fatalf("a delivered handoff became %q (%q)", h.State, h.Failure)
	}

	// Long after: the briefed and delivered rows still stand; the touched
	// one that never moved on is failed.
	clock.at = clock.at.Add(24 * time.Hour)
	if n := b.failStuckOpenings(ctx); n != 1 {
		t.Fatalf("%d rows failed a day later, want only the one still opening", n)
	}
	if a := assignmentState(t, b, ctx, "a-briefed"); a.State != AssignmentBriefed {
		t.Fatalf("a briefed assignment became %q a day later", a.State)
	}
	if h := handoffState(t, b, ctx, "h-delivered"); h.State != HandoffDelivered {
		t.Fatalf("a delivered handoff became %q a day later", h.State)
	}
	if len(*settled) != 1 || (*settled)[0].ID != "a-touched" {
		t.Fatalf("the Board was told %+v, want only a-touched", *settled)
	}
}
