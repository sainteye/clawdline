package projectassistanthost

import (
	"context"
	"strings"
	"sync/atomic"
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

// requestPerson stands for the gate verdict a child's task-secret request
// carries. The production source revalidates any such verdict as a person's
// and refuses it as pa_revoked; the review turn must not inherit it.
type requestPerson struct{}

// A child reports through its own request after the Bot has moved on by
// several turns. The review turn is still queued, in the Bot's current
// conversation, exactly once.
func TestAChildsReportingRequestStillWakesTheBotAfterLaterTurns(t *testing.T) {
	n := startNative(t, func(f *hostFixture) {
		// Later turns register their own tools; only the first is the
		// fixture's to receive.
		native, registered := f.o.NativeWork, atomic.Bool{}
		f.o.NativeWork = func(ctx context.Context, q app.OpenRequest, w NativeWork) (func(), error) {
			if registered.Swap(true) {
				return func() {}, nil
			}
			return native(ctx, q, w)
		}
		source := f.o.Source
		f.o.Source = func(ctx context.Context, key pa.BotKey, write bool) (pa.SourceStatus, error) {
			s, err := source(ctx, key, write)
			if ctx.Value(requestPerson{}) != nil {
				s.State = pa.SourceRevoked
			}
			return s, err
		}
	})
	ctx := context.Background()
	session := fixtureReceive(t, n.f.p.sessions)
	link, err := n.tools.Dispatch(ctx, "later-turns", "Review the result", "Change only a.go in the isolated worktree.", []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	session.reply <- app.PromptResult{FinalText: "Dispatched."}
	fixtureEventually(t, func() bool {
		turn, err := n.f.h.store.Turn(ctx, n.bot.Key, n.prompt.Turn.ID)
		return err == nil && turn.State == pa.TurnSucceeded
	})
	for _, text := range []string{"two", "three"} {
		if _, e := n.f.send(t, n.bot, text); e != nil {
			t.Fatal(e)
		}
		sent := fixtureReceive(t, n.f.p.sent)
		select {
		case reopened := <-n.f.p.sessions:
			session = reopened
		default:
		}
		session.reply <- app.PromptResult{FinalText: "Reply to " + text}
		fixtureEventually(t, func() bool {
			turn, err := n.f.h.store.Turn(ctx, n.bot.Key, sent.Turn.ID)
			return err == nil && turn.State == pa.TurnSucceeded
		})
	}
	x, err := n.f.h.store.Export(ctx, n.bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("dispatch fence generation=%d binding=%q; current generation=%d binding=%q", link.Fence.Generation, link.Fence.BindingID, x.Conversation.Generation, x.Conversation.BindingID)
	reporting := context.WithValue(ctx, requestPerson{}, true)
	if _, err := n.f.o.Broker.Settle(reporting, link.TaskID, orchestrator.StateSuccess, "", &taskdir.Result{Status: "success", Summary: "Child summary."}); err != nil {
		t.Fatal(err)
	}
	wake := fixtureReceive(t, n.f.p.sent)
	if pa.WorkCompletionTask(wake.Turn) != link.TaskID || wake.Turn.Fence.ConversationID != x.Conversation.ID {
		t.Fatalf("the review turn was not queued in the current conversation: %+v", wake.Turn)
	}
	r, _, _ := n.f.o.Broker.Record(ctx, link.TaskID)
	// A replayed delivery reaches the host the way the broker hands it over.
	if err := n.f.h.completeWork(ctx, orchestrator.AssistantWorkSnapshotOf(r)); err != nil {
		t.Fatal(err)
	}
	if wakes, results := workTurns(t, n); wakes != 1 || results != 1 {
		t.Fatalf("wakes=%d results=%d, want one each", wakes, results)
	}
}

// A wake the host refuses for good is not silent: the person reads the typed
// reason in the chat, once, and no review turn follows later.
func TestARefusedWakeTellsThePersonInTheChatOnce(t *testing.T) {
	n := startNative(t, nil)
	ctx := context.Background()
	link := dispatchUnrequested(t, n, "refused")
	n.f.revoked.Store(true)
	if _, err := n.f.o.Broker.Settle(ctx, link.TaskID, orchestrator.StateSuccess, "", &taskdir.Result{Status: "success", Summary: "Refused summary."}); err != nil {
		t.Fatal(err)
	}
	var notices []pa.Message
	fixtureEventually(t, func() bool {
		x, err := n.f.h.store.Export(ctx, n.bot.Key)
		if err != nil {
			return false
		}
		notices = notices[:0]
		for _, m := range x.Messages {
			if m.Kind == pa.NoticeMessage && strings.Contains(m.Text, link.TaskID) {
				notices = append(notices, m)
			}
		}
		return len(notices) > 0
	})
	if len(notices) != 1 || !strings.Contains(notices[0].Text, string(pa.Revoked)) || !strings.Contains(notices[0].Text, "沒能接手") {
		t.Fatalf("refusal notice: %+v", notices)
	}
	n.f.revoked.Store(false)
	record, _, _ := n.f.o.Broker.Record(ctx, link.TaskID)
	if err := n.f.h.completeWork(ctx, orchestrator.AssistantWorkSnapshotOf(record)); err != nil {
		t.Fatal(err)
	}
	if wakes, _ := workTurns(t, n); wakes != 0 {
		t.Fatalf("a refused task was woken later: wakes=%d", wakes)
	}
}
