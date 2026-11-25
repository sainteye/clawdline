package projectassistant

import (
	"context"
	"strings"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A suggested-reply press is the person's own message, so it releases a queue
// paused by a restart exactly as a typed follow-up does.
func TestDecisionAnswerReleasesARestartPausedQueue(t *testing.T) {
	fence := pa.Fence{Key: pa.BotKey{MachineID: "m", BotID: "b"}, ConversationID: "c", Generation: 3}
	check := func(context.Context) error { return nil }
	typed := Submission{Message: pa.SendMessage{Fence: fence}, AuthorityCheck: check}
	if !personMessage(typed) {
		t.Fatal("a typed follow-up is not the person's message")
	}
	pressed := typed
	pressed.Decision = &pa.PermissionOffer{ID: "offer"}
	if !personMessage(pressed) {
		t.Fatal("a suggested-reply press is not the person's message")
	}
	unchecked := pressed
	unchecked.AuthorityCheck = nil
	if personMessage(unchecked) {
		t.Fatal("a press without a fresh authority check counts as the person's message")
	}
	notice := typed
	notice.Notice = true
	if personMessage(notice) {
		t.Fatal("a host notice counts as the person's message")
	}
}

func (f *runtimeFixture) submitPerson(t *testing.T, text, id string) (pa.Receipt, error) {
	t.Helper()
	f.store.mu.Lock()
	fence := currentFence(f.store.bot, f.store.c)
	f.store.mu.Unlock()
	return f.r.Submit(context.Background(), Submission{Command: pa.Command{Key: f.key, Operation: pa.SendOperation, IdempotencyKey: id, ContentHash: strings.Repeat("a", 64)},
		Message: pa.SendMessage{Fence: fence, Text: text}, Feature: pa.FeatureState{Enabled: true}, Source: f.source,
		AuthorityCheck: func(context.Context) error { return nil }})
}

// seedHeldQueue puts the chat in the state the daemon log showed: earlier
// turns with an unknown outcome and a queued message behind them, with no
// worker running.
func seedHeldQueue(f *runtimeFixture, unknown ...pa.Turn) pa.Fence {
	fence := pa.Fence{Key: f.key, Project: f.scope, ConversationID: "chat", BindingID: "old", Generation: 1}
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	f.store.c.BindingID, f.store.c.Generation = "old", 1
	f.store.bindings["old"] = pa.ExecutionBinding{Fence: fence, Agent: pa.Codex, NativeSessionID: "opaque", MemoryRevision: 1, InstructionRevision: 1}
	for _, u := range unknown {
		u.Fence = fence
		if u.State == "" {
			u.State = pa.TurnUnknown
		}
		f.store.turns[u.ID] = u
		f.store.order = append(f.store.order, u.ID)
	}
	queued := pa.Turn{ID: "queued", Fence: fence, MessageID: "queued-message", State: pa.TurnQueued, Origin: pa.UserOrigin, IdempotencyKey: "queued"}
	f.store.turns[queued.ID] = queued
	f.store.order = append(f.store.order, queued.ID)
	f.store.messages = append(f.store.messages, pa.Message{ID: "queued-message", Seq: int64(len(f.store.messages) + 1), Fence: fence, TurnID: queued.ID, Kind: pa.UserMessage, Text: "queued earlier", At: time.Now()})
	return fence
}

func unknownTurn(id, field string) pa.Turn {
	return pa.Turn{ID: id, MessageID: id + "-message", Origin: pa.UserOrigin, IdempotencyKey: id,
		Failure: &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: field}}
}

// On 2026-11-25 a Bot chat held a queue behind interrupted turns, and every
// message the person sent was refused as pa_outcome_unknown /
// inspect_interrupted_turn. A new message never repeats an old turn, so it is
// admitted: the unknown turns are acknowledged, the queue runs in order, and
// no earlier prompt is sent again.
func TestThePersonsMessageIsAdmittedBehindUnknownTurns(t *testing.T) {
	peer := unknownTurn("u1", "execution")
	peer.Origin = pa.BotRequestOrigin
	routine := unknownTurn("u1", "execution")
	routine.RoutineRunID = "run"
	for _, tc := range []struct {
		name    string
		unknown []pa.Turn
		active  string
	}{
		{"execution", []pa.Turn{unknownTurn("u1", "execution")}, ""},
		{"steer unknown", []pa.Turn{unknownTurn("u1", pa.SteerUnknownField)}, ""},
		{"peer request", []pa.Turn{peer}, ""},
		{"routine", []pa.Turn{routine}, ""},
		{"two unknown turns", []pa.Turn{unknownTurn("u1", RestartConfirmedField), unknownTurn("u2", "execution")}, ""},
		{"stale active turn", []pa.Turn{unknownTurn("u1", "execution")}, "u1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRuntimeFixture(t)
			seedHeldQueue(f, tc.unknown...)
			f.store.mu.Lock()
			f.store.c.ActiveTurnID = tc.active
			f.store.mu.Unlock()
			admitted, err := f.submitPerson(t, "carry on", "carry-on")
			if err != nil {
				t.Fatalf("the person's message was refused: %v", err)
			}
			first := receivePrompt(t, f.provider)
			if first.Turn.ID != "queued" {
				t.Fatalf("the earlier queue did not run first: %s", first.Turn.ID)
			}
			f.provider.session().reply <- PromptResult{FinalText: "done"}
			second := receivePrompt(t, f.provider)
			if second.Turn.ID != admitted.ResourceID {
				t.Fatalf("the person's message did not run next: %s", second.Turn.ID)
			}
			f.provider.session().reply <- PromptResult{FinalText: "done"}
			awaitTurn(t, f, admitted.ResourceID, pa.TurnSucceeded)
			for _, u := range tc.unknown {
				got, _ := f.store.Turn(context.Background(), f.key, u.ID)
				if got.State != pa.TurnFailed || got.Failure == nil || got.Failure.Code != pa.ExecutionEffectUnknown || got.Failure.Field != "acknowledged" {
					t.Fatalf("unknown turn %s was not acknowledged: %s %+v", u.ID, got.State, got.Failure)
				}
			}
			if f.provider.count() != 2 || len(f.provider.sent) != 0 {
				t.Fatalf("prompts sent: %d", f.provider.count())
			}
		})
	}
}

// A running row with no worker is an owner lost on an error (or a start
// that has not recovered yet). It is recorded unknown, never resent, and the
// person's message is admitted.
func TestAnOrphanedRunningTurnDoesNotRefuseThePerson(t *testing.T) {
	f := newRuntimeFixture(t)
	orphan := unknownTurn("orphan", "")
	orphan.State, orphan.Failure = pa.TurnRunning, nil
	seedHeldQueue(f, orphan)
	f.store.mu.Lock()
	f.store.c.ActiveTurnID = "orphan"
	f.store.mu.Unlock()
	admitted, err := f.submitPerson(t, "are you there", "are-you-there")
	if err != nil {
		t.Fatalf("an orphaned running row refused the person: %v", err)
	}
	if q := receivePrompt(t, f.provider); q.Turn.ID != "queued" {
		t.Fatalf("the earlier queue did not run first: %s", q.Turn.ID)
	}
	f.provider.session().reply <- PromptResult{FinalText: "done"}
	if q := receivePrompt(t, f.provider); q.Turn.ID != admitted.ResourceID {
		t.Fatalf("the person's message did not run: %s", q.Turn.ID)
	}
	f.provider.session().reply <- PromptResult{FinalText: "done"}
	awaitTurn(t, f, admitted.ResourceID, pa.TurnSucceeded)
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	lost := false
	for _, e := range f.store.events {
		if e.Turn != nil && e.Turn.ID == "orphan" && e.Turn.State == pa.TurnUnknown && e.Turn.Failure != nil && e.Turn.Failure.Field == WorkerLostField {
			lost = true
		}
	}
	if !lost || f.store.turns["orphan"].State != pa.TurnFailed {
		t.Fatalf("orphan not recorded as a lost worker: lost=%v state=%s", lost, f.store.turns["orphan"].State)
	}
	if f.provider.count() != 2 {
		t.Fatalf("the orphaned prompt was resent: %d prompts", f.provider.count())
	}
}

// A dispatched task's review waits behind an unknown turn instead of being
// refused, and does not settle that turn on the person's behalf.
func TestAWorkReviewQueuesBehindAnUnknownTurn(t *testing.T) {
	f := newRuntimeFixture(t)
	fence := seedHeldQueue(f, unknownTurn("u1", "execution"))
	_, err := f.r.Submit(context.Background(), Submission{Command: pa.Command{Key: f.key, Operation: pa.SendOperation, IdempotencyKey: pa.WorkCompletionPrefix + "task", ContentHash: strings.Repeat("a", 64)},
		Notice: true, Message: pa.SendMessage{Fence: fence, Text: "review"}, Feature: pa.FeatureState{Enabled: true}, Source: f.source,
		AuthorityCheck: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatalf("review refused behind an unknown turn: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if got, _ := f.store.Turn(context.Background(), f.key, "u1"); got.State != pa.TurnUnknown || f.provider.count() != 0 {
		t.Fatalf("a host review settled the unknown turn or ran: %s, %d", got.State, f.provider.count())
	}
}

func TestAWorkReviewAcceptsAnInterruptedOrigin(t *testing.T) {
	key := pa.BotKey{MachineID: "m", BotID: "b"}
	x := Export{Bot: pa.Bot{Key: key, ConversationID: "c"}, Conversation: pa.Conversation{Key: key, ID: "c", BindingID: "b1", Generation: 1}}
	fence := currentFence(x.Bot, x.Conversation)
	x.WorkLinks = []pa.WorkLink{{TaskID: "task", TurnID: "origin", Fence: fence}}
	for _, tc := range []struct {
		name     string
		origin   pa.Turn
		finished bool
		ok       bool
	}{
		{"unknown at wake", pa.Turn{State: pa.TurnUnknown}, false, true},
		{"unknown at start", pa.Turn{State: pa.TurnUnknown}, true, false},
		{"acknowledged", pa.Turn{State: pa.TurnUnknown}.AcknowledgeUnknown(), true, true},
		{"not repeated", pa.Turn{State: pa.TurnFailed, Failure: &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: pa.RestartNotRepeatedField}}, true, true},
		{"resumed", pa.Turn{State: pa.TurnFailed, Failure: &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: pa.RestartResumedField}}, true, true},
		{"failed", pa.Turn{State: pa.TurnFailed, Failure: &pa.Failure{Code: pa.AuthRejected}}, false, false},
		{"cancelled", pa.Turn{State: pa.TurnCancelled}, false, false},
	} {
		origin := tc.origin
		origin.ID, origin.Fence = "origin", fence
		x.Turns = []pa.Turn{origin}
		if err := checkWorkCompletion(x, "task", "", tc.finished); (err == nil) != tc.ok {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}

// The live case of 2026-11-25: after a verified restart the interrupted turn
// was too old to continue, so no continuation ran and the queue behind it
// waited until the person typed. A declined continuation now settles the
// interrupted turn unrepeated, says so in the chat, and lets the queue drain;
// the next prompt first inspects what finished.
func TestADeclinedRestartContinuationReleasesTheQueue(t *testing.T) {
	f := newRuntimeFixture(t)
	original, err := f.submit(t, "first request", "first")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	next, err := f.submit(t, "next request", "next")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.r.CloseForRestart(t.Context()); err != nil {
		t.Fatal(err)
	}
	awaitTurn(t, f, original.ResourceID, pa.TurnUnknown)
	if got, _ := f.store.Turn(context.Background(), f.key, original.ResourceID); got.Failure.Field != RestartConfirmedField {
		t.Fatalf("shutdown field = %s", got.Failure.Field)
	}
	options := f.r.o
	options.Clock = func() time.Time { return time.Now().Add(2 * time.Hour) }
	r, err := NewRuntime(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	if err = r.Recover(t.Context(), f.key); err != nil {
		t.Fatal(err)
	}
	continued := receivePrompt(t, f.provider)
	if continued.Turn.ID != next.ResourceID || !strings.HasPrefix(continued.Text, restartNotRepeatedPrompt) || !strings.Contains(continued.Text, "next request") {
		t.Fatalf("queued message = %s %q", continued.Turn.ID, continued.Text)
	}
	f.provider.session().reply <- PromptResult{FinalText: "next request resolved"}
	awaitTurn(t, f, next.ResourceID, pa.TurnSucceeded)
	old, _ := f.store.Turn(context.Background(), f.key, original.ResourceID)
	if old.State != pa.TurnFailed || old.Failure.Code != pa.ExecutionEffectUnknown || old.Failure.Field != pa.RestartNotRepeatedField {
		t.Fatalf("interrupted turn = %s %+v", old.State, old.Failure)
	}
	f.store.mu.Lock()
	notice, resumed := false, false
	for _, m := range f.store.messages {
		notice = notice || m.Notice != nil && m.Notice.Key == pa.NoticeRestartNotRepeated && m.Notice.Cause == original.ResourceID
		resumed = resumed || m.Text == resumePrompt
	}
	f.store.mu.Unlock()
	if !notice || resumed {
		t.Fatalf("notice=%v continuation=%v", notice, resumed)
	}
	if f.provider.count() != 2 {
		t.Fatalf("sent %d prompts; the interrupted turn must not be repeated", f.provider.count())
	}
	// A later start finds nothing to settle and sends nothing.
	if err = r.Recover(t.Context(), f.key); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if f.provider.count() != 2 {
		t.Fatalf("a second start sent %d prompts", f.provider.count())
	}
}

// With nothing queued, a declined continuation leaves the interrupted turn
// for the person to see; only a waiting queue is released.
func TestADeclinedRestartWithNothingQueuedStaysUnknown(t *testing.T) {
	f := newRuntimeFixture(t)
	original, err := f.submit(t, "first request", "first")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	if err = f.r.CloseForRestart(t.Context()); err != nil {
		t.Fatal(err)
	}
	options := f.r.o
	options.Clock = func() time.Time { return time.Now().Add(2 * time.Hour) }
	r, err := NewRuntime(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	if err = r.Recover(t.Context(), f.key); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if old, _ := f.store.Turn(context.Background(), f.key, original.ResourceID); old.State != pa.TurnUnknown || f.provider.count() != 1 {
		t.Fatalf("interrupted turn = %s, prompts %d", old.State, f.provider.count())
	}
}
