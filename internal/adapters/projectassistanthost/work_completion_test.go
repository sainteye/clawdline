package projectassistanthost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestRestartKeepsACompletedDispatchUntilItsBotCanReview(t *testing.T) {
	n := startNative(t, nil)
	f, ctx := n.f, t.Context()
	fixtureReceive(t, f.p.sessions)
	link, err := n.tools.DispatchWithCompletion(ctx, "finish-during-restart", "Review the result", "Check the synthetic result.", []string{}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	f.o.Broker = f.broker(t, f.o.Broker.Dir)
	f.o.NativeWork = nil // The first boot’s registration capture has no reader after restart.
	f.start(t)
	f.enable(t)
	recovery := fixtureReceive(t, f.p.sent)
	recoverySession := fixtureReceive(t, f.p.sessions)
	if pa.RestartSourceTurn(recovery.Turn) != n.prompt.Turn.ID || !recoverySession.q.Resume || !recoverySession.q.RestartReadOnly {
		t.Fatalf("the source turn did not resume in its original session: %+v", recovery)
	}
	if _, err = recoverySession.q.Tools.Dispatch(ctx, "new-dispatch", "Should stay read-only", "Try dispatching.", nil); pa.Code(err) != pa.ReadOnly {
		t.Fatalf("the recovery turn dispatched work: %v", err)
	}

	broker := f.o.Broker
	settled, err := broker.Settle(ctx, link.TaskID, orchestrator.StateSuccess, "", &taskdir.Result{Status: "success", Summary: "Synthetic result retained."})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := orchestrator.AssistantWorkSnapshotOf(settled)
	// The review is queued behind the recovering source turn and starts
	// only after that turn is resumed.
	if err = f.h.completeWork(ctx, snapshot); err != nil {
		t.Fatalf("result review was not queued behind its source turn: %v", err)
	}
	select {
	case early := <-f.p.sent:
		t.Fatalf("the review started before its source recovered: %+v", early.Turn)
	case <-time.After(100 * time.Millisecond):
	}
	recoverySession.reply <- app.PromptResult{FinalText: "Recovered previous progress."}
	fixtureEventually(t, func() bool {
		old, err := f.h.store.Turn(ctx, n.bot.Key, n.prompt.Turn.ID)
		return err == nil && old.Failure != nil && old.Failure.Field == pa.RestartResumedField
	})
	if err = f.h.completeWork(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	review := fixtureReceive(t, f.p.sent)
	if pa.WorkCompletionTask(review.Turn) != link.TaskID || review.Quote == nil || review.Quote.Text != "Synthetic result retained." {
		t.Fatalf("review did not quote the existing result: %+v", review)
	}
	reviewSession := fixtureReceive(t, f.p.sessions)
	if err = f.h.completeWork(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	reviewSession.reply <- app.PromptResult{FinalText: "Reviewed."}
	fixtureEventually(t, func() bool {
		wakes, results := workTurns(t, n)
		return wakes == 1 && results == 1 && f.h.Usage().ActiveActors == 0
	})
	if f.launcher.calls.Load() != 1 || f.p.opens.Load() != 3 {
		t.Fatalf("recovered dispatch duplicated work: workers=%d model turns=%d", f.launcher.calls.Load(), f.p.opens.Load())
	}
}

func TestDispatchedWorkCompletionWakesBotOnce(t *testing.T) {
	n := startNative(t, nil)
	f, ctx := n.f, context.Background()
	session := fixtureReceive(t, f.p.sessions)
	title, brief, claims := "Check the result", "Inspect the isolated checkout without changing files.", []string{}
	link, err := n.tools.DispatchWithCompletion(ctx, "waiting-work", title, brief, claims, "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	task, _, err := f.o.Broker.Record(ctx, link.TaskID)
	if err != nil || task.AssistantOwner == nil || !task.AssistantOwner.ContinueOnCompletion || len(task.Claims) != 0 {
		t.Fatalf("read-only callback was not recorded: %+v %v", task.AssistantOwner, err)
	}
	briefing, err := os.ReadFile(filepath.Join(task.Dir, "CHILD.md"))
	if err != nil || !strings.Contains(string(briefing), "use the Clawdline command callback") {
		t.Fatalf("the worker did not receive the command callback instructions: %v", err)
	}
	if _, err := n.tools.Status(ctx, link.TaskID); err != nil {
		t.Fatal(err)
	}
	var failure *pa.Failure
	if _, err := n.tools.Status(ctx, link.TaskID); !errors.As(err, &failure) || failure.Code != pa.ReadOnly || failure.Field != "work_completion_pending" {
		t.Fatalf("repeated status query was not deferred to callback: %v", err)
	}
	session.reply <- app.PromptResult{FinalText: "Handed off."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, n.bot.Key, n.prompt.Turn.ID)
		return err == nil && turn.State == pa.TurnSucceeded
	})
	if f.p.opens.Load() != 1 {
		t.Fatal("opened another model before worker completion")
	}

	broker := f.o.Broker
	complete := broker.AssistantWorkCompleted
	broker.AssistantWorkCompleted = func(context.Context, orchestrator.AssistantWorkSnapshot) error {
		return errors.New("injected interruption before callback delivery")
	}
	settled, err := broker.Settle(ctx, link.TaskID, orchestrator.StateFailure, "", &taskdir.Result{Status: "failure", Summary: "Worker could not complete."})
	if err != nil {
		t.Fatal(err)
	}
	if f.p.opens.Load() != 1 {
		t.Fatal("failed callback opened a model")
	}
	broker.AssistantWorkCompleted = complete
	// Move the fixture's failed delivery past the broker's unattended age.
	if _, err := f.o.Database.Exec("UPDATE outbox SET started_at = ? WHERE kind = ? AND subject = ?", time.Now().Add(-time.Hour).Unix(), orchestrator.EffectAssistantWorkCompleted, link.TaskID); err != nil {
		t.Fatal(err)
	}
	broker.RecoverEffects(ctx)
	wake := fixtureReceive(t, f.p.sent)
	wakeSession := fixtureReceive(t, f.p.sessions)
	if pa.WorkCompletionTask(wake.Turn) != link.TaskID || wake.Quote == nil || wake.Quote.Text != "Worker could not complete." || !wakeSession.q.Resume {
		t.Fatalf("callback did not resume with the durable result: %+v", wake)
	}
	// Replaying the same durable completion while the continuation runs
	// returns the same admission. A later broker update is not a new wake.
	snapshot := orchestrator.AssistantWorkSnapshotOf(settled)
	if err := complete(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := broker.BindWork(ctx, link.TaskID, "a1111111-1111-4111-8111-111111111111", "fixture"); err != nil {
		t.Fatal(err)
	}
	wakeSession.reply <- app.PromptResult{FinalText: "The worker failed."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, n.bot.Key, wake.Turn.ID)
		return err == nil && turn.State == pa.TurnSucceeded
	})
	x, err := f.h.store.Export(ctx, n.bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	var wakes, results int
	for _, turn := range x.Turns {
		if pa.WorkCompletionTask(turn) != "" {
			wakes++
		}
	}
	for _, message := range x.Messages {
		if message.Kind == pa.WorkMessage {
			results++
		}
	}
	if wakes != 1 || results != 1 || f.p.opens.Load() != 2 || f.launcher.calls.Load() != 1 {
		t.Fatalf("callback duplicated work, model turns or results: wake=%d result=%d model=%d worker=%d", wakes, results, f.p.opens.Load(), f.launcher.calls.Load())
	}
}

func TestCompletedWorkRespectsForgottenOrigin(t *testing.T) {
	n := startNative(t, nil)
	f, ctx := n.f, context.Background()
	session := fixtureReceive(t, f.p.sessions)
	if err := n.tools.EditMemory(ctx, "memo", app.MemoryEdit{Append: "A fixture detail to forget."}); err != nil {
		t.Fatal(err)
	}
	link, err := n.tools.DispatchWithCompletion(ctx, "work", "Inspect the repository", "Return one finding without editing files.", []string{}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	session.reply <- app.PromptResult{FinalText: "Waiting for the worker."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, n.bot.Key, n.prompt.Turn.ID)
		return err == nil && turn.State == pa.TurnSucceeded
	})
	memory, err := f.h.store.Current(ctx, n.bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := f.h.store.MemoryEntries(ctx, n.bot.Key)
	if err != nil || len(entries) != 1 || len(entries[0].Sources) == 0 {
		t.Fatalf("fixture memory: %v %v", entries, err)
	}
	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Forget(ctx, app.ForgetRequest{
		Command: pa.Command{Key: n.bot.Key, Operation: pa.ForgetOperation, IdempotencyKey: "forget-work"},
		Project: n.bot.Project, ExpectedRevision: memory.Revision, SourceIDs: []string{entries[0].Sources[0].ID},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.o.Broker.Settle(ctx, link.TaskID, orchestrator.StateSuccess, "", &taskdir.Result{Status: "success", Summary: "Worker result belongs to earlier context."}); err != nil {
		t.Fatal(err)
	}
	x, err := f.h.store.Export(ctx, n.bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(x.Turns) != 1 || f.p.opens.Load() != 1 || len(x.WorkLinks) != 1 || x.WorkLinks[0].State != pa.WorkCompleted {
		t.Fatalf("the forgotten turn was resumed or the result lost: turns=%d opens=%d links=%+v", len(x.Turns), f.p.opens.Load(), x.WorkLinks)
	}
}
