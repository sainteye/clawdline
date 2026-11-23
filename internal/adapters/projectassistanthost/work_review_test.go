package projectassistanthost

import (
	"context"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// dispatchUnrequested dispatches without continue_on_completion and lets the
// originating turn finish, so only the Bot's work review setting can wake it.
func dispatchUnrequested(t *testing.T, n *nativeFixture, id string) pa.WorkLink {
	t.Helper()
	ctx := context.Background()
	session := fixtureReceive(t, n.f.p.sessions)
	link, err := n.tools.Dispatch(ctx, id, "Review the result", "Change only a.go in the isolated worktree.", []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	session.reply <- app.PromptResult{FinalText: "Dispatched."}
	fixtureEventually(t, func() bool {
		turn, err := n.f.h.store.Turn(ctx, n.bot.Key, n.prompt.Turn.ID)
		return err == nil && turn.State == pa.TurnSucceeded
	})
	return link
}

func setWorkReview(t *testing.T, n *nativeFixture, value string) {
	t.Helper()
	ctx := context.Background()
	service, err := n.f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	x, err := n.f.h.store.Export(ctx, n.bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	b := x.Bot
	edited, _, err := service.Edit(ctx, pa.Command{Key: b.Key, Operation: pa.EditOperation, IdempotencyKey: "work-review-" + value},
		pa.EditBot{ExpectedVersion: b.Version, Name: b.Name, Appearance: b.Appearance, Instructions: b.Instructions.Text, WorkReview: value})
	if err != nil || edited.WorkReview != value {
		t.Fatalf("work review edit: %+v %v", edited.WorkReview, err)
	}
}

func workTurns(t *testing.T, n *nativeFixture) (wakes, results int) {
	t.Helper()
	x, err := n.f.h.store.Export(context.Background(), n.bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	for _, turn := range x.Turns {
		if pa.WorkCompletionTask(turn) != "" {
			wakes++
		}
	}
	for _, m := range x.Messages {
		if m.Kind == pa.WorkMessage {
			results++
		}
	}
	return wakes, results
}

// With the default setting, every ending of unrequested work queues one
// review turn, and its prompt says it is a report rather than the user.
func TestWorkReviewWakesUnrequestedWorkForEveryEnding(t *testing.T) {
	for _, state := range []orchestrator.State{orchestrator.StateSuccess, orchestrator.StateFailure, orchestrator.StateTimeout, orchestrator.StateCancelled, orchestrator.StateSpawnFailed} {
		t.Run(string(state), func(t *testing.T) {
			n := startNative(t, nil)
			ctx := context.Background()
			link := dispatchUnrequested(t, n, "work-"+string(state))
			if _, err := n.f.o.Broker.Settle(ctx, link.TaskID, state, "", &taskdir.Result{Status: "failure", Summary: "Worker summary."}); err != nil {
				t.Fatal(err)
			}
			wake := fixtureReceive(t, n.f.p.sent)
			text := wake.Text
			if pa.WorkCompletionTask(wake.Turn) != link.TaskID || wake.Quote == nil || wake.Quote.Text != "Worker summary." ||
				!strings.Contains(text, "not a new instruction from the user") || !strings.Contains(text, "不是使用者的新指示") ||
				!strings.Contains(text, link.TaskID) || !strings.Contains(text, "Review the result") {
				t.Fatalf("review turn did not carry the report: %+v", wake.Turn)
			}
			// Replaying the delivery finds the receipt and queues nothing.
			r, _, _ := n.f.o.Broker.Record(ctx, link.TaskID)
			if err := n.f.h.completeWork(ctx, orchestrator.AssistantWorkSnapshotOf(r)); err != nil {
				t.Fatal(err)
			}
			if wakes, results := workTurns(t, n); wakes != 1 || results != 1 {
				t.Fatalf("wakes=%d results=%d, want one each", wakes, results)
			}
		})
	}
}

// Turning the setting off publishes the result without a turn; an explicit
// continue_on_completion still wakes the Bot.
func TestWorkReviewOffPublishesOnlyUnlessRequested(t *testing.T) {
	n := startNative(t, nil)
	ctx := context.Background()
	setWorkReview(t, n, "off")
	session := fixtureReceive(t, n.f.p.sessions)
	quiet, err := n.tools.Dispatch(ctx, "quiet", "Quiet work", "Change only a.go in the isolated worktree.", []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	requested, err := n.tools.DispatchWithCompletion(ctx, "requested", "Requested review", "Inspect without editing files.", []string{}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	session.reply <- app.PromptResult{FinalText: "Dispatched."}
	fixtureEventually(t, func() bool {
		turn, err := n.f.h.store.Turn(ctx, n.bot.Key, n.prompt.Turn.ID)
		return err == nil && turn.State == pa.TurnSucceeded
	})
	if _, err := n.f.o.Broker.Settle(ctx, quiet.TaskID, orchestrator.StateFailure, "", &taskdir.Result{Status: "failure", Summary: "Quiet result."}); err != nil {
		t.Fatal(err)
	}
	fixtureEventually(t, func() bool { _, results := workTurns(t, n); return results == 1 })
	if wakes, _ := workTurns(t, n); wakes != 0 || n.f.p.opens.Load() != 1 {
		t.Fatalf("an unrequested task woke a Bot whose review is off: wakes=%d opens=%d", wakes, n.f.p.opens.Load())
	}
	if _, err := n.f.o.Broker.Settle(ctx, requested.TaskID, orchestrator.StateSuccess, "", &taskdir.Result{Status: "success", Summary: "Requested result."}); err != nil {
		t.Fatal(err)
	}
	wake := fixtureReceive(t, n.f.p.sent)
	if pa.WorkCompletionTask(wake.Turn) != requested.TaskID {
		t.Fatalf("an explicit request did not wake the Bot: %+v", wake.Turn)
	}
	if wakes, results := workTurns(t, n); wakes != 1 || results != 2 {
		t.Fatalf("wakes=%d results=%d, want one wake and two results", wakes, results)
	}
}

// The first wait for a person is the task's one review; its later settlement
// publishes the result and queues nothing more.
func TestWorkWaitingForAPersonIsTheOnlyReview(t *testing.T) {
	n := startNative(t, nil)
	ctx := context.Background()
	link := dispatchUnrequested(t, n, "waiting")
	r, _, err := n.f.o.Broker.Record(ctx, link.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	waiting := orchestrator.AssistantWorkSnapshotOf(r)
	waiting.Link.State = pa.WorkWaitingForPerson
	if err := n.f.h.completeWork(ctx, waiting); err != nil {
		t.Fatal(err)
	}
	wake := fixtureReceive(t, n.f.p.sent)
	if pa.WorkCompletionTask(wake.Turn) != link.TaskID || wake.Quote != nil || !strings.Contains(wake.Text, "waiting for the user") {
		t.Fatalf("waiting review did not describe the wait: %+v", wake.Turn)
	}
	wakeSession := fixtureReceive(t, n.f.p.sessions)
	wakeSession.reply <- app.PromptResult{FinalText: "The worker needs you."}
	fixtureEventually(t, func() bool {
		turn, err := n.f.h.store.Turn(ctx, n.bot.Key, wake.Turn.ID)
		return err == nil && turn.State == pa.TurnSucceeded
	})
	if _, err := n.f.o.Broker.Settle(ctx, link.TaskID, orchestrator.StateSuccess, "", &taskdir.Result{Status: "success", Summary: "Done after the answer."}); err != nil {
		t.Fatal(err)
	}
	fixtureEventually(t, func() bool { _, results := workTurns(t, n); return results == 1 })
	if wakes, _ := workTurns(t, n); wakes != 1 || n.f.p.opens.Load() != 2 {
		t.Fatalf("a task reviewed while waiting was reviewed again: wakes=%d opens=%d", wakes, n.f.p.opens.Load())
	}
}
