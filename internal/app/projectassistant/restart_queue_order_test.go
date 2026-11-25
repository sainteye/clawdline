package projectassistant

import (
	"context"
	"strings"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// seedHeldReview replaces the queued message seedHeldQueue leaves behind the
// unknown turns with an automatic host notice, as a report waiting there.
func seedHeldReview(f *runtimeFixture, unknown ...pa.Turn) {
	seedHeldQueue(f, unknown...)
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	review := f.store.turns["queued"]
	review.Origin, review.IdempotencyKey = pa.HostNoticeOrigin, "held-report"
	f.store.turns["queued"] = review
	for i := range f.store.messages {
		if f.store.messages[i].ID == review.MessageID {
			f.store.messages[i].Kind = pa.NoticeMessage
			f.store.messages[i].Text = "held work report"
		}
	}
}

// On 2026-11-25 the person's message released a chat paused by a restart,
// and an automatic work report that had waited in the queue ran first while
// the person's request to inspect the interrupted turn waited behind it.
// The releasing message now runs first, inspects what finished, and is told
// how much automatic work is still to be delivered; nothing is dropped.
func TestThePersonsReleasingMessageRunsBeforeHeldAutomaticWork(t *testing.T) {
	f := newRuntimeFixture(t)
	seedHeldReview(f, unknownTurn("u1", RestartConfirmedField))
	admitted, err := f.submitPerson(t, "check the paused turn", "check")
	if err != nil {
		t.Fatal(err)
	}
	first := receivePrompt(t, f.provider)
	if first.Turn.ID != admitted.ResourceID {
		t.Fatalf("the held report ran before the person's message: %s", first.Turn.ID)
	}
	if !strings.Contains(first.Text, restartReleasedPrompt(1)+"\n\ncheck the paused turn") {
		t.Fatalf("the person's prompt = %q", first.Text)
	}
	f.provider.session().reply <- PromptResult{FinalText: "inspected"}
	second := receivePrompt(t, f.provider)
	if second.Turn.ID != "queued" || strings.Contains(second.Text, "Clawdline restarted") {
		t.Fatalf("the held report = %s %q", second.Turn.ID, second.Text)
	}
	f.provider.session().reply <- PromptResult{FinalText: "reviewed"}
	awaitTurn(t, f, "queued", pa.TurnSucceeded)
	if f.provider.count() != 2 {
		t.Fatalf("prompts sent: %d", f.provider.count())
	}
}

// An unknown turn that no restart interrupted keeps the existing order: the
// person acknowledged it, but did not release a restart hold.
func TestAnAcknowledgedNonRestartTurnKeepsTheQueueOrder(t *testing.T) {
	f := newRuntimeFixture(t)
	seedHeldReview(f, unknownTurn("u1", "execution"))
	admitted, err := f.submitPerson(t, "carry on", "carry-on")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := f.store.Turn(context.Background(), f.key, admitted.ResourceID); got.ReleasedRestart {
		t.Fatal("a non-restart acknowledgement was marked as releasing a restart hold")
	}
	if first := receivePrompt(t, f.provider); first.Turn.ID != "queued" {
		t.Fatalf("the queue order changed: %s", first.Turn.ID)
	}
	f.provider.session().reply <- PromptResult{FinalText: "done"}
	if second := receivePrompt(t, f.provider); second.Turn.ID != admitted.ResourceID || strings.Contains(second.Text, "Clawdline restarted") {
		t.Fatalf("the person's message = %s %q", second.Turn.ID, second.Text)
	}
	f.provider.session().reply <- PromptResult{FinalText: "done"}
	awaitTurn(t, f, admitted.ResourceID, pa.TurnSucceeded)
}

// The afternoon case of 2026-11-25: the restart was verified but the
// continuation was declined while nothing was queued, so nothing was
// released. A work report that arrived half an hour later then waited until
// the person typed. A review admitted after the decision is now released the
// same way as work queued before it, and it first inspects what finished.
func TestAWorkReviewAfterADeclinedRestartIsReleased(t *testing.T) {
	f := newRuntimeFixture(t)
	original, err := f.submit(t, "publish what is done", "first")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	if err = f.r.CloseForRestart(t.Context()); err != nil {
		t.Fatal(err)
	}
	awaitTurn(t, f, original.ResourceID, pa.TurnUnknown)
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
		t.Fatalf("before the review: %s, prompts %d", old.State, f.provider.count())
	}
	origin, _ := f.store.Turn(context.Background(), f.key, original.ResourceID)
	link := pa.WorkLink{TaskID: "task", TurnID: origin.ID, Fence: origin.Fence, ResultMessageID: "result", Title: "cleanup", State: pa.WorkCompleted}
	f.store.mu.Lock()
	f.store.work = append(f.store.work, link)
	f.store.messages = append(f.store.messages, pa.Message{ID: "result", Seq: int64(len(f.store.messages) + 1), Fence: origin.Fence, TurnID: origin.ID, Kind: pa.WorkMessage, Text: "result", At: time.Now()})
	f.store.mu.Unlock()
	if err = r.WakeWorkCompletion(t.Context(), link, pa.FeatureState{Enabled: true}, f.source); err != nil {
		t.Fatal(err)
	}
	review := receivePrompt(t, f.provider)
	if !strings.HasPrefix(review.Text, restartNotRepeatedPrompt) || !strings.Contains(review.Text, "cleanup") {
		t.Fatalf("the late review = %q", review.Text)
	}
	f.provider.session().reply <- PromptResult{FinalText: "reviewed"}
	awaitTurn(t, f, review.Turn.ID, pa.TurnSucceeded)
	if old, _ := f.store.Turn(context.Background(), f.key, original.ResourceID); old.State != pa.TurnFailed || old.Failure.Field != pa.RestartNotRepeatedField {
		t.Fatalf("interrupted turn = %s %+v", old.State, old.Failure)
	}
	if f.provider.count() != 2 {
		t.Fatalf("prompts sent: %d; the interrupted turn must not be repeated", f.provider.count())
	}
}
