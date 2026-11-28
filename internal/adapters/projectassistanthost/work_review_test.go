package projectassistanthost

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/session"
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

func setWorkReview(t *testing.T, f *hostFixture, key pa.BotKey, value string) {
	t.Helper()
	ctx := context.Background()
	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.h.store.Bot(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
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
	setWorkReview(t, n.f, n.bot.Key, "off")
	session := fixtureReceive(t, n.f.p.sessions)
	quiet, err := n.tools.Dispatch(ctx, "quiet", "Inspect the result", "Inspect without editing files.", []string{})
	if err != nil {
		t.Fatal(err)
	}
	if quiet.Fence.Key != n.bot.Key || quiet.TurnID != n.prompt.Turn.ID {
		t.Fatalf("read-only work did not attach to the direct user turn: %+v", quiet)
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

func TestWorkReviewTurnDispatchesReadOnlyWorkInItsOwnProject(t *testing.T) {
	n := startNative(t, func(f *hostFixture) { f.o.Projects = peerFolders{fixtureResolver{f}} })
	ctx := context.Background()
	initial := dispatchUnrequested(t, n, "initial")
	if _, err := n.f.o.Broker.Settle(ctx, initial.TaskID, orchestrator.StateSuccess, "", &taskdir.Result{Status: "success", Summary: "Ready to review."}); err != nil {
		t.Fatal(err)
	}
	review := fixtureReceive(t, n.f.p.sessions)
	wake := fixtureReceive(t, n.f.p.sent)
	if pa.WorkCompletionTask(wake.Turn) != initial.TaskID {
		t.Fatalf("not a review turn for the initial task: %+v", wake.Turn)
	}
	tools := review.q.Tools.(NativeWork)
	var refused *pa.Failure
	if _, err := tools.DispatchToProject(ctx, "other", "Inspect elsewhere", "Read only.", []string{}, "other"); !errors.As(err, &refused) || refused.Code != pa.ScopeDenied || refused.Field != "dispatch_project_direct" {
		t.Fatalf("review turn cross-Project dispatch: %v", err)
	}
	if _, err := tools.DispatchWithCompletion(ctx, "callback", "Inspect the result", "Read only.", []string{}, "", "", ""); !errors.As(err, &refused) || refused.Code != pa.ScopeDenied || refused.Field != "work_completion_origin" {
		t.Fatalf("review turn completion callback: %v", err)
	}
	if _, err := tools.DispatchWithBoard(ctx, "board", "Inspect the result", "Read only.", []string{}, "", "", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"); !errors.As(err, &refused) || refused.Code != pa.ScopeDenied || refused.Field != "board_item_id" {
		t.Fatalf("review turn Board dispatch: %v", err)
	}
	for _, project := range []string{"own", ""} {
		link, err := tools.DispatchToProject(ctx, "review-read", "Inspect the result", "Read only.", []string{}, project)
		if err != nil || link.TaskID == "" || link.Fence.Key != n.bot.Key || link.TurnID != wake.Turn.ID {
			t.Fatalf("review dispatch with project %q: %+v, %v", project, link, err)
		}
	}
	review.reply <- app.PromptResult{FinalText: "Dispatched the review."}
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

// A full Bot is a transient refusal: the outbox retries it, so one full
// moment neither writes a notice nor locks the task. When capacity returns,
// the same delivery wakes the Bot. Only the attempt at the end of the retry
// window tells the person in the chat.
func TestAFullBotStillTakesTheWorkOverWhenCapacityReturns(t *testing.T) {
	n := startNative(t, nil)
	ctx := context.Background()
	link := dispatchUnrequested(t, n, "capacity")
	n.f.capacityFull.Store(true)
	if _, err := n.f.o.Broker.Settle(ctx, link.TaskID, orchestrator.StateSuccess, "", &taskdir.Result{Status: "success", Summary: "Capacity summary."}); err != nil {
		t.Fatal(err)
	}
	record, _, _ := n.f.o.Broker.Record(ctx, link.TaskID)
	snapshot := orchestrator.AssistantWorkSnapshotOf(record)
	if err := n.f.h.completeWork(ctx, snapshot); pa.Code(err) != pa.CapacityFull {
		t.Fatalf("a full Bot answered %v, want capacity_full", err)
	}
	x, err := n.f.h.store.Export(ctx, n.bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range x.Messages {
		if m.Kind == pa.NoticeMessage && strings.Contains(m.Text, link.TaskID) {
			t.Fatalf("a transient refusal wrote a final notice: %+v", m)
		}
	}
	n.f.capacityFull.Store(false)
	if err := n.f.h.completeWork(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if wakes, _ := workTurns(t, n); wakes != 1 {
		t.Fatalf("the task was not taken over after capacity returned: wakes=%d", wakes)
	}
}

// When the retry window has ended, a full Bot is told once in the chat.
func TestAFullBotAtTheEndOfTheRetryWindowTellsThePerson(t *testing.T) {
	n := startNative(t, nil)
	ctx := context.Background()
	link := dispatchUnrequested(t, n, "capacity-last")
	n.f.capacityFull.Store(true)
	if _, err := n.f.o.Broker.Settle(ctx, link.TaskID, orchestrator.StateSuccess, "", &taskdir.Result{Status: "success", Summary: "Last summary."}); err != nil {
		t.Fatal(err)
	}
	record, _, _ := n.f.o.Broker.Record(ctx, link.TaskID)
	snapshot := orchestrator.AssistantWorkSnapshotOf(record)
	snapshot.LastAttempt = true
	if err := n.f.h.completeWork(ctx, snapshot); pa.Code(err) != pa.CapacityFull {
		t.Fatalf("a full Bot answered %v, want capacity_full", err)
	}
	x, err := n.f.h.store.Export(ctx, n.bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	notices := 0
	for _, m := range x.Messages {
		if m.Kind == pa.NoticeMessage && strings.Contains(m.Text, link.TaskID) && strings.Contains(m.Text, string(pa.CapacityFull)) {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("last-attempt notices=%d, want 1", notices)
	}
}

func TestHostRepairsPreviouslySettledWorkWithoutReview(t *testing.T) {
	n := startNative(t, nil)
	f := n.f
	ctx := t.Context()
	setWorkReview(t, f, n.bot.Key, "off")
	link := dispatchUnrequested(t, n, "settled-before-restart")
	r, _, err := f.o.Broker.Record(ctx, link.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, r.Worktree.Path, "commit", "--allow-empty", "-qm", "Synthetic delivered change")
	if _, err = f.o.Broker.Settle(ctx, r.ID, orchestrator.StateSuccess, "", &taskdir.Result{Status: "success", Summary: "One retained result."}); err != nil {
		t.Fatal(err)
	}
	count := func() int64 {
		page, err := f.h.store.List(ctx, pa.ListBots{MachineID: n.bot.Key.MachineID, PageSize: pa.MaxPageRows})
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("Bot list: %d, %v", len(page.Items), err)
		}
		if page.Items[0].DispatchedWork == nil {
			return 0
		}
		return page.Items[0].DispatchedWork.Delivered
	}
	if got := count(); got != 1 {
		t.Fatalf("before settlement: %d pending deliveries", got)
	}
	if err = f.h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	f.o.Broker.AssistantWorkChanged = nil
	if _, err = f.o.Broker.Land(ctx, r.ID, orchestrator.LandingRequest{State: "abandoned", Note: "Superseded fixture", Machine: true}); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 1 {
		t.Fatalf("missed projection: %d pending deliveries", got)
	}

	f.o.Broker = f.broker(t, f.o.Broker.Dir)
	f.start(t)
	f.enable(t)
	fixtureEventually(t, func() bool { return count() == 0 })
	if wakes, results := workTurns(t, n); wakes != 0 || results != 1 {
		t.Fatalf("landing replayed work or its review: wakes=%d, results=%d", wakes, results)
	}
}

func TestHostRepairsClosedBotRootAndSyncsLiveClosureWithoutReview(t *testing.T) {
	n := startNative(t, nil)
	f := n.f
	ctx := t.Context()
	f.o.Broker.Type = func(context.Context, string, string) error { return nil }
	s := fixtureReceive(t, f.p.sessions)
	dispatchRoot := func(key, title string) pa.WorkLink {
		t.Helper()
		rootCtx := pa.WithDispatchChoice(ctx, pa.DispatchChoice{Mode: pa.DispatchModeRoot})
		link, err := n.tools.Dispatch(rootCtx, key, title, "Install dev and verify.", []string{})
		if err != nil {
			t.Fatalf("dispatch root %s: %v", key, err)
		}
		return link
	}
	liveRoot := dispatchRoot("root-live-close", "Live closed root")
	missedRoot := dispatchRoot("root-missed-close", "Closed before restart")
	s.reply <- app.PromptResult{FinalText: "Opened both roots."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, n.bot.Key, n.prompt.Turn.ID)
		return err == nil && turn.State == pa.TurnSucceeded
	})
	runningCount := func() int64 {
		page, err := f.h.store.List(ctx, pa.ListBots{MachineID: n.bot.Key.MachineID, PageSize: pa.MaxPageRows})
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("Bot list: %d, %v", len(page.Items), err)
		}
		if page.Items[0].DispatchedWork == nil {
			return 0
		}
		return page.Items[0].DispatchedWork.Running
	}
	if got := runningCount(); got != 2 {
		t.Fatalf("initial running count = %d, want 2", got)
	}
	// 1) A live close without delivery settles the WorkLink via the outbox without waking a review turn.
	a1, err := f.o.Broker.RootAssignmentByID(ctx, liveRoot.TaskID)
	if err != nil || a1.Executor == nil {
		t.Fatalf("live root assignment: %+v %v", a1, err)
	}
	if err = f.o.Broker.RecordRootClosure(ctx, a1.ID, session.Session{
		ID: a1.Executor.TerminalID, Backend: session.Backend(a1.Executor.Backend),
		Assistant: session.Assistant(a1.Assistant), ConversationID: "conv-live",
	}, "close", false); err != nil {
		t.Fatal(err)
	}
	f.o.Broker.RecoverEffects(ctx)
	if got := runningCount(); got != 1 {
		t.Fatalf("after live close running count = %d, want 1", got)
	}

	// 2) A Root closed while the Bot host had no closure callback is repaired on restart.
	if err = f.h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	f.o.Broker.AssistantRootClosed = nil
	a2, err := f.o.Broker.RootAssignmentByID(ctx, missedRoot.TaskID)
	if err != nil || a2.Executor == nil {
		t.Fatalf("missed root assignment: %+v %v", a2, err)
	}
	if err = f.o.Broker.RecordRootClosure(ctx, a2.ID, session.Session{
		ID: a2.Executor.TerminalID, Backend: session.Backend(a2.Executor.Backend),
		Assistant: session.Assistant(a2.Assistant), ConversationID: "conv-missed",
	}, "close", false); err != nil {
		t.Fatal(err)
	}
	if got := runningCount(); got != 1 {
		t.Fatalf("before restart reconciliation running count = %d, want 1", got)
	}
	f.o.Broker = f.broker(t, f.o.Broker.Dir)
	f.start(t)
	f.enable(t)
	fixtureEventually(t, func() bool { return runningCount() == 0 })

	x, err := f.h.store.Export(ctx, n.bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]pa.WorkState{}
	for _, wl := range x.WorkLinks {
		states[wl.ID] = wl.State
	}
	if states[liveRoot.ID] != pa.WorkUnknown || states[missedRoot.ID] != pa.WorkUnknown {
		t.Fatalf("reconciled root work link states = %+v, want both unknown", states)
	}
	if wakes, results := workTurns(t, n); wakes != 0 || results != 0 {
		t.Fatalf("closed roots created result or review: wakes=%d, results=%d", wakes, results)
	}
}

func TestBotRootDispatchReplayInterleavingAndDeliveredRepair(t *testing.T) {
	n := startNative(t, nil)
	f := n.f
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)
	f.o.Broker.Clock = func() time.Time { return now }
	f.o.Broker.Type = func(context.Context, string, string) error { return nil }
	s := fixtureReceive(t, f.p.sessions)
	rootCtx := pa.WithDispatchChoice(ctx, pa.DispatchChoice{Mode: pa.DispatchModeRoot})
	dispatchRoot := func(key, title string) pa.WorkLink {
		t.Helper()
		link, err := n.tools.Dispatch(rootCtx, key, title, "Verify review findings.", []string{})
		if err != nil {
			t.Fatalf("dispatch root %s: %v", key, err)
		}
		return link
	}

	// 1) Closed Root replayed with the same idempotency key keeps unknown instead of rewinding to executing.
	closedLink := dispatchRoot("root-replay-closed", "Closed before replay")
	aClosed, err := f.o.Broker.RootAssignmentByID(ctx, closedLink.TaskID)
	if err != nil || aClosed.Executor == nil {
		t.Fatalf("closed assignment: %+v %v", aClosed, err)
	}
	if err = f.o.Broker.RecordRootClosure(ctx, aClosed.ID, session.Session{
		ID: aClosed.Executor.TerminalID, Backend: session.Backend(aClosed.Executor.Backend),
		Assistant: session.Assistant(aClosed.Assistant), ConversationID: "conv-closed",
	}, "close", false); err != nil {
		t.Fatal(err)
	}
	replayedClosed := dispatchRoot("root-replay-closed", "Closed before replay")
	if replayedClosed.State != pa.WorkUnknown {
		t.Fatalf("replayed closed root state = %s, want unknown", replayedClosed.State)
	}

	// 2) Delivered -> closed -> same-key replay preserves completed, ResultMessageID, and delivery timestamp.
	deliveredLink := dispatchRoot("root-replay-delivered", "Delivered then closed")
	aDel, err := f.o.Broker.RootAssignmentByID(ctx, deliveredLink.TaskID)
	if err != nil || aDel.Executor == nil {
		t.Fatalf("delivered assignment: %+v %v", aDel, err)
	}
	delRow := session.Session{
		ID: aDel.Executor.TerminalID, Backend: session.Backend(aDel.Executor.Backend),
		Assistant: session.Assistant(aDel.Assistant), ConversationID: "conv-del",
		State: session.StateIdle, Evidence: session.EvidenceRegistry,
		Observation: session.Observation{ObservedAt: now, Freshness: session.FreshnessCurrent},
	}
	f.o.Broker.Live = func(context.Context) []session.Session { return []session.Session{delRow} }
	deliveredAt := now.Add(time.Minute)
	now = deliveredAt
	if _, err = f.o.Broker.ReportSessionDelivery(ctx, aDel.Executor.TerminalID, "Delivered cleanly."); err != nil {
		t.Fatal(err)
	}
	now = deliveredAt.Add(10 * time.Minute)
	f.o.Broker.RecoverEffects(ctx)
	now = deliveredAt.Add(15 * time.Minute)
	if err = f.o.Broker.RecordRootClosure(ctx, aDel.ID, delRow, "close", false); err != nil {
		t.Fatal(err)
	}
	replayedDelivered := dispatchRoot("root-replay-delivered", "Delivered then closed")
	if replayedDelivered.State != pa.WorkCompleted || replayedDelivered.ResultMessageID != "work-result-"+aDel.ID || !replayedDelivered.FinishedAt.Equal(deliveredAt) {
		t.Fatalf("replayed delivered root = %+v, want completed at %v", replayedDelivered, deliveredAt)
	}

	// 3) Interleaving: session report queued (pending) -> close runs first (unknown) -> delivery effect runs -> completed.
	interLink := dispatchRoot("root-interleaved", "Close before delivery effect")
	aInter, err := f.o.Broker.RootAssignmentByID(ctx, interLink.TaskID)
	if err != nil || aInter.Executor == nil {
		t.Fatalf("interleaved assignment: %+v %v", aInter, err)
	}
	interRow := session.Session{
		ID: aInter.Executor.TerminalID, Backend: session.Backend(aInter.Executor.Backend),
		Assistant: session.Assistant(aInter.Assistant), ConversationID: "conv-inter",
		State: session.StateIdle, Evidence: session.EvidenceRegistry,
		Observation: session.Observation{ObservedAt: now, Freshness: session.FreshnessCurrent},
	}
	f.o.Broker.Live = func(context.Context) []session.Session { return []session.Session{interRow} }
	interDelAt := now.Add(time.Minute)
	now = interDelAt
	if _, err = f.o.Broker.ReportSessionDelivery(ctx, aInter.Executor.TerminalID, "Interleaved summary."); err != nil {
		t.Fatal(err)
	}
	now = interDelAt.Add(5 * time.Second)
	if err = f.o.Broker.RecordRootClosure(ctx, aInter.ID, interRow, "close", false); err != nil {
		t.Fatal(err)
	}
	// Closure ran immediately before the pending delivery effect.
	x, err := f.h.store.Export(ctx, n.bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	findLink := func(links []pa.WorkLink, id string) pa.WorkLink {
		for _, wl := range links {
			if wl.ID == id {
				return wl
			}
		}
		t.Fatalf("missing link %s", id)
		return pa.WorkLink{}
	}
	if got := findLink(x.WorkLinks, interLink.ID); got.State != pa.WorkUnknown {
		t.Fatalf("after close before delivery effect: state = %s, want unknown", got.State)
	}
	now = interDelAt.Add(10 * time.Minute)
	f.o.Broker.RecoverEffects(ctx)
	x, err = f.h.store.Export(ctx, n.bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got := findLink(x.WorkLinks, interLink.ID); got.State != pa.WorkCompleted || got.ResultMessageID != "work-result-"+aInter.ID || !got.FinishedAt.Equal(interDelAt) {
		t.Fatalf("after pending delivery effect settled: %+v, want completed at %v", got, interDelAt)
	}

	// 4) Startup reconciliation repairs a Root with DeliveredAt > 0 whose WorkLink stayed executing.
	staleDelLink := dispatchRoot("root-stale-delivered", "Delivered but link stuck executing")
	s.reply <- app.PromptResult{FinalText: "Done dispatching."}
	fixtureEventually(t, func() bool {
		turn, err := f.h.store.Turn(ctx, n.bot.Key, n.prompt.Turn.ID)
		return err == nil && turn.State == pa.TurnSucceeded
	})
	aStale, err := f.o.Broker.RootAssignmentByID(ctx, staleDelLink.TaskID)
	if err != nil || aStale.Executor == nil {
		t.Fatalf("stale assignment: %+v %v", aStale, err)
	}
	if _, err = f.o.Database.Exec(`UPDATE broker_root_assignments SET record=json_set(CAST(record AS TEXT),'$.bot_owner.delivered_at',?,'$.closure',json(?)) WHERE id=?`,
		now.Unix(), `{"terminal_id":"`+aStale.Executor.TerminalID+`","method":"close","forced":false,"safe":true,"closed_at":`+strconv.FormatInt(now.Unix(), 10)+`}`, aStale.ID); err != nil {
		t.Fatal(err)
	}
	ids, err := f.h.store.ClosedRootWorkRepairs(ctx, n.bot.Key.MachineID, "")
	if err != nil || len(ids) != 1 || ids[0] != aStale.ID {
		t.Fatalf("ClosedRootWorkRepairs = %v %v, want [%s]", ids, err, aStale.ID)
	}
	f.h.reconcileSettledWork(ctx)
	x, err = f.h.store.Export(ctx, n.bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got := findLink(x.WorkLinks, staleDelLink.ID); got.State != pa.WorkCompleted || got.FinishedAt.IsZero() {
		t.Fatalf("reconciled delivered root = %+v, want completed with finished_at", got)
	}
	if ids, err = f.h.store.ClosedRootWorkRepairs(ctx, n.bot.Key.MachineID, ""); err != nil || len(ids) != 0 {
		t.Fatalf("ClosedRootWorkRepairs after repair = %v %v, want empty", ids, err)
	}
	page, err := f.h.store.List(ctx, pa.ListBots{MachineID: n.bot.Key.MachineID, PageSize: pa.MaxPageRows})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("Bot list: %v", err)
	}
	if page.Items[0].DispatchedWork != nil && page.Items[0].DispatchedWork.Running != 0 {
		t.Fatalf("running count after all repairs = %+v, want 0", page.Items[0].DispatchedWork)
	}
}
