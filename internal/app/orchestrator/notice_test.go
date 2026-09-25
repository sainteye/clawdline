package orchestrator

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// The completion notice's two ladders, its holds, and the evidence that ends it.
//
// All of it from one report: the same "task finished" arrived four and five
// times in the root's tab while that root was busy integrating the child it was
// about, and the person watching asked whether the work had been dispatched
// twice. A second delivery, working on the Cloud line, measured the same thing
// from the other side — seven re-typings before an ACK for an envelope that was
// never broken.

// The screens the hold turns on: an idle input line as Claude Code actually
// draws one — with its own `Try "…"` hint in it — the same line with half a
// sentence somebody is still writing, and the line while a message it has not
// read waits behind it.
const (
	idleComposer = `  ctrl+x ctrl+s to send now

────────────────────────────────────────────────────────────────────────────────
❯ Try "fix lint errors"
────────────────────────────────────────────────────────────────────────────────
  ▀ ▄ ▀ 943e0000-0000-4000-8000-000000000001`

	draftedComposer = `  ctrl+x ctrl+s to send now

────────────────────────────────────────────────────────────────────────────────
❯ so about the landing, I think we should
────────────────────────────────────────────────────────────────────────────────
  ▀ ▄ ▀ 943e0000-0000-4000-8000-000000000001`

	queuedComposer = `  ctrl+x ctrl+s to send now

────────────────────────────────────────────────────────────────────────────────
❯ Press up to edit queued messages
────────────────────────────────────────────────────────────────────────────────
  ▀ ▄ ▀ 943e0000-0000-4000-8000-000000000001`
)

// The bug as the person saw it. On one ladder the second copy was typed five
// seconds after the first, the third ten seconds after that, and five of them
// fitted inside two minutes — none of which is a root reading anything, because
// a root mid-turn has not reached the line yet. The delivered wait is its own
// ladder now, and its rungs widen.
func TestADeliveredNoticeIsNotTypedAgainInsideAMinute(t *testing.T) {
	b, ctx := newTestBroker(t)
	start := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	now := start
	b.Clock = func() time.Time { return now }
	typed := 0
	b.Type = func(context.Context, string, string) error { typed++; return nil }
	r := finished(t, b, ctx, "c6f30000-0000-4000-8000-000000000001")

	if b.PumpNotices(ctx); typed != 1 {
		t.Fatalf("the first delivery typed %d lines", typed)
	}
	after, _, _ := b.Record(ctx, r.ID)
	if got := after.Notice.NextRetryAt.Sub(start); got != ackWaitBase {
		t.Fatalf("the next typing is armed %s after the first, want %s", got, ackWaitBase)
	}
	// The old ladder's first four deadlines, counted from the first delivery.
	for _, elapsed := range []time.Duration{5 * time.Second, 15 * time.Second, 35 * time.Second, 75 * time.Second} {
		now = start.Add(elapsed)
		b.PumpNotices(ctx)
		if typed != 1 {
			t.Fatalf("the notice was typed again %s after the first copy", elapsed)
		}
	}
	// And the rungs widen: two minutes, then four, then eight, then sixteen,
	// then the ceiling. Each one is checked a second early as well, because a
	// ladder that is only ever asked after its deadline is not a ladder.
	spent := time.Duration(0)
	for i, gap := range []time.Duration{ackWaitBase, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, maxAckWait} {
		spent += gap
		now = start.Add(spent - time.Second)
		b.PumpNotices(ctx)
		if typed != i+1 {
			t.Fatalf("copy %d arrived a second before its %s rung", i+2, gap)
		}
		now = start.Add(spent)
		b.PumpNotices(ctx)
		if typed != i+2 {
			t.Fatalf("copy %d did not arrive on its %s rung: typed %d", i+2, gap, typed)
		}
	}
	// Six copies, and the person's first two minutes hold one of them.
	if spent < 60*time.Minute {
		t.Fatalf("six copies of one notice fit inside %s", spent)
	}
}

// The ladder ends. Eight attempts and then dead letter, whatever the root is
// doing — and the envelope nobody took is on the list a person reads.
func TestADeliveredNoticeStopsAtItsLimitAndIsListed(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	typed := 0
	b.Type = func(context.Context, string, string) error { typed++; return nil }
	r := finished(t, b, ctx, "c6f30000-0000-4000-8000-000000000002")

	for i := 0; i < AttemptLimit+4; i++ {
		b.PumpNotices(ctx)
		now = now.Add(maxAckWait + time.Second)
	}
	if typed != AttemptLimit {
		t.Fatalf("a root that never acknowledged was typed at %d times, want %d", typed, AttemptLimit)
	}
	after, _, _ := b.Record(ctx, r.ID)
	if after.Notice.State != NoticeDeadLetter || after.Notice.Attempts != AttemptLimit {
		t.Fatalf("after the budget: %+v", after.Notice)
	}
	list, err := b.Completions(ctx, false)
	if err != nil || len(list) != 1 || list[0].Notice.State != NoticeDeadLetter {
		t.Fatalf("what nobody acknowledged: %v %+v", err, list)
	}
}

// A root that landed the child's work has read that the child finished: nothing
// else could tell it where that work went. So the landing ends the resend, and
// the ACK it was asked for is not asked for twice.
func TestALandingByTheRootEndsTheResend(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	typed := 0
	b.Type = func(context.Context, string, string) error { typed++; return nil }
	r := finished(t, b, ctx, "c6f30000-0000-4000-8000-000000000003")
	b.PumpNotices(ctx)
	if typed != 1 {
		t.Fatalf("the first delivery typed %d lines", typed)
	}

	// The child's own credential is not the root's voice: a landing signed with
	// the task secret proves nothing about whether the root read anything.
	if _, err := b.Land(ctx, r.ID, LandingRequest{State: string(LandingPending), Secret: "s",
		Note: "the child says it is still in the tree"}); err != nil {
		t.Fatalf("a pending landing from the child: %v", err)
	}
	mid, _, _ := b.Record(ctx, r.ID)
	if mid.Notice.State == NoticeAcknowledged {
		t.Fatalf("a landing signed with the task secret closed the root's notice: %+v", mid.Notice)
	}

	now = now.Add(ackWaitBase)
	if _, err := b.Land(ctx, r.ID, LandingRequest{State: string(LandingNothingToLand), Machine: true,
		Note: "read it, nothing to land"}); err != nil {
		t.Fatalf("landing: %v", err)
	}
	after, _, _ := b.Record(ctx, r.ID)
	if after.Notice.State != NoticeAcknowledged || after.Notice.ObservedAt.IsZero() ||
		!after.Notice.AcknowledgedAt.Equal(now) {
		t.Fatalf("after the root landed it: %+v", after.Notice)
	}
	// And it is not typed again, at its rung or ever after.
	for _, elapsed := range []time.Duration{ackWaitBase, 4 * time.Minute, maxAckWait, 3 * maxAckWait} {
		now = now.Add(elapsed)
		b.PumpNotices(ctx)
	}
	if typed != 1 {
		t.Fatalf("a landed task was announced %d times", typed)
	}
	// An ACK afterwards is an honest no-op rather than a refusal.
	if changed, err := b.Acknowledge(ctx, r.ID, after.Notice.ID); err != nil || changed {
		t.Fatalf("acknowledging what a landing already closed: %v %v", changed, err)
	}
}

// The other way a typed line does damage. A send is a paste and then Return, so
// a notice typed at a root whose person was half way through a sentence is
// appended to that sentence and submitted with it.
func TestADraftInTheComposerHoldsTheNotice(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	typed := 0
	b.Type = func(context.Context, string, string) error { typed++; return nil }
	drafting := true
	b.Screen = func(context.Context, string) (string, bool) {
		if drafting {
			return draftedComposer, true
		}
		return idleComposer, true
	}
	r := finished(t, b, ctx, "c6f30000-0000-4000-8000-000000000004")

	b.PumpNotices(ctx)
	if typed != 0 {
		t.Fatal("the notice was typed onto a draft")
	}
	held, _, _ := b.Record(ctx, r.ID)
	// A hold is not an attempt: holding cost the notice none of its budget.
	if held.Notice.Attempts != 0 || held.Notice.State != NoticePending {
		t.Fatalf("holding spent the budget: %+v", held.Notice)
	}
	// And it is not silence: the reason is on the record, where the list a
	// person reads shows it.
	if held.Notice.LastError == nil || held.Notice.LastError.Code != holdComposer.Code {
		t.Fatalf("a hold with no reason on it: %+v", held.Notice.LastError)
	}
	list, err := b.Completions(ctx, false)
	if err != nil || len(list) != 1 || list[0].Notice.LastError == nil ||
		list[0].Notice.LastError.Code != holdComposer.Code {
		t.Fatalf("what this machine is sitting on: %v %+v", err, list)
	}

	// It goes the moment the composer is clear.
	drafting = false
	now = now.Add(noticeHoldStep)
	b.PumpNotices(ctx)
	if typed != 1 {
		t.Fatalf("after the draft was sent, typed %d", typed)
	}
	if after, _, _ := b.Record(ctx, r.ID); after.Notice.State != NoticeDelivered ||
		after.Notice.Attempts != 1 || after.Notice.LastError != nil {
		t.Fatalf("after the hold: %+v", after.Notice)
	}
}

// A hold cannot be for ever, and typing anyway is the thing it exists to stop.
// So at the limit it spends an attempt, the budget ends the sequence as it does
// for any other reason nothing was typed, and the push tells the person which
// reason it was — not that eight copies were sent, because none was.
func TestAHeldNoticeIsBoundedAndSaysWhyInItsPush(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	typed := 0
	b.Type = func(context.Context, string, string) error { typed++; return nil }
	b.Screen = func(context.Context, string) (string, bool) { return draftedComposer, true }
	var mu sync.Mutex
	bodies := []string{}
	done := make(chan struct{}, 4)
	b.pushed = func() { done <- struct{}{} }
	b.Push = func(_ context.Context, _, body, _, _ string) (int, int, error) {
		mu.Lock()
		defer mu.Unlock()
		bodies = append(bodies, body)
		return 1, 0, nil
	}
	r := finished(t, b, ctx, "c6f30000-0000-4000-8000-000000000005")

	// The first pass holds; every pass a whole hold later spends an attempt.
	b.PumpNotices(ctx)
	if first, _, _ := b.Record(ctx, r.ID); first.Notice.Attempts != 0 {
		t.Fatalf("the first look spent an attempt: %+v", first.Notice)
	}
	for i := 0; i < AttemptLimit; i++ {
		now = now.Add(maxNoticeHold + time.Second)
		b.PumpNotices(ctx)
	}
	if typed != 0 {
		t.Fatalf("a held notice was typed %d times", typed)
	}
	after, _, _ := b.Record(ctx, r.ID)
	if after.Notice.State != NoticeDeadLetter || after.Notice.Attempts != AttemptLimit {
		t.Fatalf("a hold that never ended: %+v", after.Notice)
	}
	if after.Notice.LastError == nil || after.Notice.LastError.Code != holdComposer.Code {
		t.Fatalf("the dead letter does not say what held it: %+v", after.Notice.LastError)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the dead letter's push never finished")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 || !strings.Contains(bodies[0], heldReason(holdComposer.Code)) {
		t.Fatalf("the push does not say why nothing was typed: %+v", bodies)
	}
	if strings.Contains(bodies[0], "都沒有被收下") {
		t.Fatalf("the push says the notice was sent and refused, and it was never typed: %q", bodies[0])
	}
}

// The shape the person actually saw. A root mid-turn keeps what was typed at it
// in a queue and reads it when the turn ends, and the composer says so — so a
// second copy typed while the first still waits is one more line for the root to
// read and no more information. Whether that matters depends on whose message is
// waiting, and the notice answers that: one never delivered queues behind
// somebody else's message, which is how it gets read at all.
func TestACopyAlreadyWaitingIsNotTypedOverAndAFirstOneStillGoes(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	typed := 0
	b.Type = func(context.Context, string, string) error { typed++; return nil }
	b.Screen = func(context.Context, string) (string, bool) { return queuedComposer, true }

	// Nothing has been delivered yet, and the message waiting is not this one.
	first := finished(t, b, ctx, "c6f30000-0000-4000-8000-000000000006")
	b.PumpNotices(ctx)
	if typed != 1 {
		t.Fatalf("a first delivery was held behind somebody else's queued message: typed %d", typed)
	}

	// Now the copy in that queue is this notice, and the ladder's next rung is
	// not a reason to put another one behind it.
	now = now.Add(ackWaitBase)
	b.PumpNotices(ctx)
	if typed != 1 {
		t.Fatalf("a second copy was typed behind the first: typed %d", typed)
	}
	held, _, _ := b.Record(ctx, first.ID)
	if held.Notice.LastError == nil || held.Notice.LastError.Code != holdQueued.Code {
		t.Fatalf("the hold does not say a copy is already waiting: %+v", held.Notice.LastError)
	}
	if held.Notice.Attempts != 1 {
		t.Fatalf("holding behind its own copy spent an attempt: %+v", held.Notice)
	}
}

// rootAt is a live reading whose one session is the root in the state given,
// so a test can turn the root idle, working or waiting between passes.
func rootAt(state *session.State) func(context.Context) []session.Session {
	return func(context.Context) []session.Session {
		return []session.Session{{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation,
			State: *state}}
	}
}

// The incident this was written for (2026-09-25). A root showed a question from
// 19:54 to 22:08 while the person was away; its child finished at 20:07. Every
// look found the menu and rightly typed nothing — but each ten minutes of
// holding spent an attempt, so at 21:27 the notice was a dead letter, and when
// the person answered the root carried on not knowing. A person answering a
// question is the normal case, not a failure: the menu must not walk a notice
// to dead letter, and the line goes the moment the menu is gone.
func TestAMenuHeldPastTheOldLadderStillGetsTheNoticeOnceItIsGone(t *testing.T) {
	b, ctx := newTestBroker(t)
	start := time.Date(2026, 9, 25, 20, 7, 0, 0, time.UTC)
	now := start
	b.Clock = func() time.Time { return now }
	typed := 0
	b.Type = func(context.Context, string, string) error { typed++; return nil }
	choosing := true
	b.Choosing = func(context.Context, string) bool { return choosing }
	r := finished(t, b, ctx, "c6f30000-0000-4000-8000-000000000011")

	// Two hours on the menu: longer than eight ten-minute holds by forty
	// minutes, which is where the old code gave up.
	for now.Sub(start) < 2*time.Hour {
		b.PumpNotices(ctx)
		now = now.Add(5 * time.Minute)
	}
	if typed != 0 {
		t.Fatalf("the notice was typed into a menu %d times", typed)
	}
	held, _, _ := b.Record(ctx, r.ID)
	if held.Notice.State != NoticePending || held.Notice.Attempts != 0 {
		t.Fatalf("two hours on a menu spent the notice's budget: %+v", held.Notice)
	}
	if held.Notice.LastError == nil || held.Notice.LastError.Code != holdChoosing.Code ||
		!held.Notice.LastError.At.Equal(start) {
		t.Fatalf("the hold does not say why, or since when: %+v", held.Notice.LastError)
	}

	choosing = false
	b.PumpNotices(ctx)
	if typed != 1 {
		t.Fatalf("the menu was answered and the notice was typed %d times", typed)
	}
	if after, _, _ := b.Record(ctx, r.ID); after.Notice.State != NoticeDelivered || after.Notice.Attempts != 1 {
		t.Fatalf("after the menu: %+v", after.Notice)
	}
}

// Free is not for ever. A menu nobody answers for longer than the ceiling
// gives up on the notice in one step — dead letter, with the reason and the
// ceiling on it, so the push says the root was waiting for an answer and the
// list says for how long — rather than spending attempts that were never made.
// And then the root turning idle types it once more.
func TestAMenuHoldHasACeilingAndSaysSo(t *testing.T) {
	b, ctx := newTestBroker(t)
	start := time.Date(2026, 9, 25, 20, 7, 0, 0, time.UTC)
	now := start
	b.Clock = func() time.Time { return now }
	typed := 0
	b.Type = func(context.Context, string, string) error { typed++; return nil }
	choosing := true
	b.Choosing = func(context.Context, string) bool { return choosing }
	state := session.StateWaiting
	b.Live = rootAt(&state)
	var mu sync.Mutex
	bodies := []string{}
	done := make(chan struct{}, 4)
	b.pushed = func() { done <- struct{}{} }
	b.Push = func(_ context.Context, _, body, _, _ string) (int, int, error) {
		mu.Lock()
		defer mu.Unlock()
		bodies = append(bodies, body)
		return 1, 0, nil
	}
	r := finished(t, b, ctx, "c6f30000-0000-4000-8000-000000000012")

	for now.Sub(start) < maxChoosingHold-time.Minute {
		b.PumpNotices(ctx)
		now = now.Add(30 * time.Minute)
	}
	if before, _, _ := b.Record(ctx, r.ID); before.Notice.State != NoticePending {
		t.Fatalf("given up before the ceiling: %+v", before.Notice)
	}
	now = start.Add(maxChoosingHold)
	b.PumpNotices(ctx)
	after, _, _ := b.Record(ctx, r.ID)
	if after.Notice.State != NoticeDeadLetter || after.Notice.Attempts != 0 || typed != 0 {
		t.Fatalf("at the ceiling: typed %d, %+v", typed, after.Notice)
	}
	if e := after.Notice.LastError; e == nil || e.Code != holdChoosing.Code ||
		!strings.Contains(e.Message, maxChoosingHold.String()) {
		t.Fatalf("the dead letter does not say it hit the ceiling: %+v", e)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the dead letter's push never finished")
	}
	mu.Lock()
	if len(bodies) != 1 || !strings.Contains(bodies[0], heldReason(holdChoosing.Code)) {
		t.Fatalf("the push does not say the root was waiting on an answer: %+v", bodies)
	}
	mu.Unlock()

	// Still on the menu: nothing. Answered and working: nothing. Idle: once.
	now = now.Add(time.Minute)
	b.PumpNotices(ctx)
	choosing, state = false, session.StateWorking
	now = now.Add(time.Minute)
	b.PumpNotices(ctx)
	if typed != 0 {
		t.Fatalf("a dead letter was typed at a root that was not idle: %d", typed)
	}
	state = session.StateIdle
	now = now.Add(time.Minute)
	b.PumpNotices(ctx)
	if typed != 1 {
		t.Fatalf("the root turned idle and the dead letter was typed %d times", typed)
	}
}

// A dead letter for any other reason — here, eight deliveries nobody
// acknowledged — is typed once more the next time its root reads idle, and
// that one attempt is on the record. Not twice: a root that saw it and still
// did not answer is not helped by a tenth copy.
func TestADeadLetterIsTypedOnceMoreWhenItsRootTurnsIdle(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	typed := 0
	b.Type = func(context.Context, string, string) error { typed++; return nil }
	state := session.StateWorking
	b.Live = rootAt(&state)
	r := finished(t, b, ctx, "c6f30000-0000-4000-8000-000000000013")

	for i := 0; i < AttemptLimit+2; i++ {
		b.PumpNotices(ctx)
		now = now.Add(maxAckWait + time.Second)
	}
	dead, _, _ := b.Record(ctx, r.ID)
	if dead.Notice.State != NoticeDeadLetter || typed != AttemptLimit {
		t.Fatalf("the ladder did not end: typed %d, %+v", typed, dead.Notice)
	}

	b.PumpNotices(ctx)
	if typed != AttemptLimit {
		t.Fatalf("a dead letter was typed at a working root")
	}
	state = session.StateIdle
	now = now.Add(time.Minute)
	b.PumpNotices(ctx)
	if typed != AttemptLimit+1 {
		t.Fatalf("the root read idle and the dead letter was typed %d extra times", typed-AttemptLimit)
	}
	again, _, _ := b.Record(ctx, r.ID)
	if again.Notice.State != NoticeDeadLetter || again.Notice.Attempts != AttemptLimit+1 ||
		!again.Notice.LastAttemptAt.Equal(now) {
		t.Fatalf("the extra attempt is not on the record: %+v", again.Notice)
	}
	if retyped, err := b.Store.EventCount(ctx, "task.completion.retyped"); err != nil || retyped != 1 {
		t.Fatalf("%d retyped events: %v", retyped, err)
	}
	for i := 0; i < 5; i++ {
		now = now.Add(time.Hour)
		b.PumpNotices(ctx)
	}
	if typed != AttemptLimit+1 {
		t.Fatalf("the dead letter was retyped %d times", typed-AttemptLimit)
	}
	// Acknowledging it still works, and ends it.
	if changed, err := b.Acknowledge(ctx, r.ID, again.Notice.ID); err != nil || !changed {
		t.Fatalf("ack after the retype: %v %v", changed, err)
	}
}

// The pull path. A root that never saw the typed line reads its own list at
// every turn boundary, and a completion nobody acknowledged is on it — pending
// or dead — until it is acknowledged.
func TestUnacknowledgedCompletionsAreListedForTheirRoot(t *testing.T) {
	b, ctx := newTestBroker(t)
	r := finished(t, b, ctx, "c6f30000-0000-4000-8000-000000000014")
	other := finished(t, b, ctx, "c6f30000-0000-4000-8000-000000000015")
	list, err := b.RootCompletions(ctx, rootConversation)
	if err != nil || len(list) != 2 {
		t.Fatalf("a root's unacknowledged completions: %v %+v", err, list)
	}
	if none, err := b.RootCompletions(ctx, "00000000-0000-4000-8000-00000000dead"); err != nil || len(none) != 0 {
		t.Fatalf("another root sees %d: %v", len(none), err)
	}
	if _, err := b.Acknowledge(ctx, r.ID, r.Notice.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = b.RootCompletions(ctx, rootConversation)
	if len(list) != 1 || list[0].Record.ID != other.ID {
		t.Fatalf("after one ack: %+v", list)
	}
}
