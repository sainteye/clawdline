package projectassistant_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type routineRetryProvider struct {
	base  *routineIsolationProvider
	mode  string
	opens atomic.Int64
}

func (p *routineRetryProvider) Capabilities(ctx context.Context, b pa.Bot) (pa.Capabilities, error) {
	return p.base.Capabilities(ctx, b)
}
func (p *routineRetryProvider) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	if q.Binding.SessionSlot != "" && (p.opens.Add(1) == 1 || p.mode == "qualified-always") {
		cause := &app.ProviderFailure{Reason: app.ProviderRateLimited, Cause: &pa.Failure{Code: pa.CapacityFull, Field: "synthetic_refusal"}}
		switch p.mode {
		case "qualified", "qualified-always":
			session, _ := p.base.Open(ctx, q, sink)
			return session, &app.PreSendRefusal{Failure: cause}
		case "timeout":
			session, _ := p.base.Open(ctx, q, sink)
			return session, context.DeadlineExceeded
		case "open-only":
			return nil, &app.OpenNotStarted{Cause: cause}
		default:
			session, _ := p.base.Open(ctx, q, sink)
			return session, cause
		}
	}
	return p.base.Open(ctx, q, sink)
}

func TestRoutineRetryUsesAllThreeDelaysAndThenStops(t *testing.T) {
	f := newRoutineIsolationFixture(t)
	f.main(t)
	p := installRoutineRetryProvider(t, f, "qualified-always")
	f.schedule(t, "bounded-retry", pa.RoutineTrigger{Type: "once", At: f.now.UnixMilli()})
	f.tick(t)
	f.idle(t)
	for i, delay := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute} {
		x := f.export(t)
		if len(x.RoutineRuns) != 1 {
			t.Fatal(x.RoutineRuns)
		}
		run := x.RoutineRuns[0]
		if run.State != pa.TurnQueued || run.RetryCount != i+1 || !run.NotBefore.Equal(f.now.Add(delay)) {
			t.Fatal("wrong retry cadence", run, delay)
		}
		f.now = run.NotBefore
		f.tick(t)
		f.idle(t)
	}
	x := f.export(t)
	if len(x.RoutineRuns) != 1 || x.RoutineRuns[0].State != pa.TurnFailed || x.RoutineRuns[0].RetryCount != pa.MaxRoutineRetryAttempts || p.opens.Load() != 4 {
		t.Fatal("safe retry is unbounded", x.RoutineRuns, p.opens.Load())
	}
	f.now = f.now.Add(48 * time.Hour)
	f.tick(t)
	if p.opens.Load() != 4 {
		t.Fatal("terminal refusal retried again")
	}
}

func TestRoutineNeverStartedQueueSurvivesPlannedRestart(t *testing.T) {
	f := newRoutineIsolationFixture(t)
	f.main(t)
	d := f.schedule(t, "restart-pending", pa.RoutineTrigger{Type: "webhook"})
	x := f.export(t)
	fence := pa.Fence{Key: f.b.Key, Project: f.b.Project, ConversationID: f.b.ConversationID, BindingID: x.Conversation.BindingID, Generation: x.Conversation.Generation}
	run := pa.RoutineRun{ID: "before-native-start", RoutineID: d.ID, Source: "webhook", MemoryRevision: x.Memory.Revision, Event: pa.RoutineEvent{}, Key: f.b.Key, Project: f.b.Project, ConversationID: f.b.ConversationID, State: pa.TurnQueued, TurnID: "routine-turn-before-native-start", TimeoutSeconds: d.TimeoutSeconds, CreatedAt: f.now}
	turn := pa.Turn{ID: run.TurnID, RoutineRunID: run.ID, Fence: fence, MessageID: "restart-notice", State: pa.TurnQueued, IdempotencyKey: "same-logical-send"}
	message := pa.Message{ID: turn.MessageID, Fence: fence, TurnID: turn.ID, Kind: pa.NoticeMessage, Text: d.Name, At: f.now}
	f.commit(t, "pending-before-restart", app.Mutation{RoutineRun: &run, Turn: &turn, Messages: []pa.Message{message}})
	if err := app.QuiesceHistoryFor(context.Background(), f.s, func() time.Time { return f.now }, func() string { return fmt.Sprintf("shutdown-%d", f.seq.Add(1)) }, f.b.Key, app.RestartField); err != nil {
		t.Fatal(err)
	}
	retained, err := f.s.Turn(context.Background(), f.b.Key, turn.ID)
	if err != nil || retained.State != pa.TurnQueued || retained.IdempotencyKey != turn.IdempotencyKey {
		t.Fatal("never-started queue was replaced", retained, err)
	}
	f.tick(t)
	s := f.receive(t)
	if s.open.Binding.SessionSlot != "routine-"+run.ID {
		t.Fatal("restart changed logical run", s.open)
	}
	s.reply <- app.PromptResult{FinalText: "one result"}
	f.idle(t)
	x = f.export(t)
	if len(x.RoutineRuns) != 1 || x.RoutineRuns[0].ID != run.ID || x.RoutineRuns[0].State != pa.TurnSucceeded {
		t.Fatal(x.RoutineRuns)
	}
}

func installRoutineRetryProvider(t *testing.T, f *routineIsolationFixture, mode string) *routineRetryProvider {
	t.Helper()
	// Establish main history with the standard fixture first. This runtime's
	// short prompt budget must not govern unrelated main-chat preparation.
	if err := f.r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	p := &routineRetryProvider{base: f.p, mode: mode}
	var err error
	f.r, err = app.NewRuntime(app.RuntimeOptions{Repository: f.s, Memory: f.s, WorkBridge: routineIsolationBridge{}, ProjectResolver: routineIsolationResolver{f.b.Project}, Provider: p, Clock: func() time.Time { return f.now }, ID: func() string { return fmt.Sprintf("retry-generated-%d", f.seq.Add(1)) }, Authorize: func(context.Context, pa.BotKey) (pa.FeatureState, pa.SourceStatus, error) {
		return pa.FeatureState{Enabled: true}, f.source(), nil
	}, Limits: app.RuntimeLimits{MaxBots: 1, MaxProcesses: 1, PromptTimeout: time.Second, CleanupTimeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRoutineRetryRequiresQualifiedNoEffectAndRetainsIdentity(t *testing.T) {
	f := newRoutineIsolationFixture(t)
	f.main(t)
	p := installRoutineRetryProvider(t, f, "qualified")
	f.schedule(t, "safe", pa.RoutineTrigger{Type: "once", At: f.now.UnixMilli()})
	f.tick(t)
	f.idle(t)
	x := f.export(t)
	if len(x.RoutineRuns) != 1 {
		t.Fatal(x.RoutineRuns)
	}
	run := x.RoutineRuns[0]
	if run.State != pa.TurnQueued || !run.NotStartedProved || run.RetryCount != 1 || !run.NotBefore.Equal(f.now.Add(time.Minute)) {
		t.Fatal("qualified refusal lost safe wait", run)
	}
	turnID, runID := run.TurnID, run.ID
	turn, err := f.s.Turn(context.Background(), f.b.Key, turnID)
	if err != nil {
		t.Fatal(err)
	}
	key := turn.IdempotencyKey
	// Restart retains an explicitly proved, not-yet-retried queue entry.
	if err = app.QuiesceHistoryFor(context.Background(), f.s, func() time.Time { return f.now }, func() string { return fmt.Sprintf("shutdown-%d", f.seq.Add(1)) }, f.b.Key, app.RestartField); err != nil {
		t.Fatal(err)
	}
	retained, err := f.s.Turn(context.Background(), f.b.Key, turnID)
	if err != nil || retained.State != pa.TurnQueued {
		t.Fatal("restart discarded proved unstarted work", retained, err)
	}
	f.tick(t)
	if p.opens.Load() != 1 {
		t.Fatal("retry deadline ignored")
	}
	// Sleeping past the wake does not exhaust a retry deadline.
	f.now = f.now.Add(2 * time.Hour)
	f.tick(t)
	s := f.receive(t)
	if s.open.Binding.Agent != f.b.Agent || s.open.Binding.SessionSlot != "routine-"+runID {
		t.Fatal("retry changed owner", s.open)
	}
	s.reply <- app.PromptResult{FinalText: "one result"}
	f.idle(t)
	x = f.export(t)
	if len(x.RoutineRuns) != 1 || x.RoutineRuns[0].ID != runID || x.RoutineRuns[0].TurnID != turnID || x.RoutineRuns[0].State != pa.TurnSucceeded {
		t.Fatal("safe retry duplicated work", x.RoutineRuns)
	}
	turn, err = f.s.Turn(context.Background(), f.b.Key, turnID)
	if err != nil || turn.IdempotencyKey != key {
		t.Fatal("retry changed logical key", turn, err)
	}
}

func TestRoutineUnknownOpenAndTimeoutNeverReplay(t *testing.T) {
	for _, mode := range []string{"unknown", "open-only", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			f := newRoutineIsolationFixture(t)
			f.main(t)
			p := installRoutineRetryProvider(t, f, mode)
			f.schedule(t, "unsafe", pa.RoutineTrigger{Type: "once", At: f.now.UnixMilli()})
			f.tick(t)
			f.idle(t)
			x := f.export(t)
			if len(x.RoutineRuns) != 1 || x.RoutineRuns[0].State != pa.TurnUnknown || x.RoutineRuns[0].RetryCount != 0 {
				t.Fatal("unknown effect gained retry authority", x.RoutineRuns)
			}
			f.now = f.now.Add(48 * time.Hour)
			f.tick(t)
			if p.opens.Load() != 1 {
				t.Fatal("unknown run replayed", p.opens.Load())
			}
		})
	}
}

func TestRoutineFollowUpResumesMainAndBoundsItsChain(t *testing.T) {
	f := newRoutineIsolationFixture(t)
	f.submit(t, "person")
	main := f.receive(t)
	saved, err := f.r.Routine(context.Background(), main.open.Binding.Fence, f.export(t).Conversation.ActiveTurnID, app.RoutineRequest{Action: "follow_up", Minutes: 1, Content: "Synthetic build is still running; inspect its result", IdempotencyKey: "follow-up"})
	if err != nil {
		t.Fatal(err)
	}
	main.reply <- app.PromptResult{FinalText: "Reminder set"}
	f.idle(t)
	f.now = f.now.Add(2 * time.Minute)
	f.tick(t)
	follow := f.receive(t)
	if follow.open.Binding.SessionSlot != "" || !follow.open.Resume || follow.SessionID() != "main-A" || !strings.Contains(follow.prompt.Text, "scheduled follow-up") {
		t.Fatal("follow-up left its main chat", follow.open, follow.prompt)
	}
	if deadline, ok := follow.promptCtx.Deadline(); ok {
		t.Fatalf("follow-up in the main chat inherited a routine deadline: %v", deadline)
	}
	x := f.export(t)
	var d pa.Routine
	for _, v := range x.Routines {
		if v.ID == saved.ID {
			d = v
		}
	}
	if !d.FollowUp || d.FollowUpChain != 0 {
		t.Fatal(d)
	}
	next, err := f.r.Routine(context.Background(), follow.open.Binding.Fence, f.export(t).Conversation.ActiveTurnID, app.RoutineRequest{Action: "follow_up", Minutes: 1, Content: "Synthetic build result is pending", IdempotencyKey: "continue"})
	if err != nil {
		t.Fatal(err)
	}
	x = f.export(t)
	for _, v := range x.Routines {
		if v.ID == next.ID && v.FollowUpChain != 1 {
			t.Fatal("chain reset itself", v)
		}
	}
	follow.reply <- app.PromptResult{FinalText: "Follow-up checked"}
	f.idle(t)
	x = f.export(t)
	for _, v := range x.Routines {
		if v.ID == saved.ID && !v.Deleted {
			t.Fatal("consumed follow-up remains pending", v)
		}
	}
	if turn, err := f.s.Turn(context.Background(), f.b.Key, follow.prompt.Turn.ID); err != nil || turn.State != pa.TurnSucceeded {
		t.Fatalf("follow-up did not finish: %v, %v", turn.State, err)
	}

	f.now = f.now.Add(2 * time.Minute)
	f.tick(t)
	chained := f.receive(t)
	if _, err := f.r.Stop(context.Background(), app.StopRequest{
		Command: routineIsolationCommand(f.b.Key, pa.StopOperation, "stop-running-follow-up"),
		Fence:   chained.prompt.Turn.Fence,
		TurnID:  chained.prompt.Turn.ID,
	}); err != nil {
		t.Fatal(err)
	}
	f.idle(t)
	if err := chained.promptCtx.Err(); err != context.Canceled {
		t.Fatalf("Stop did not cancel the running follow-up: %v", err)
	}
}

func TestRoutineFollowUpIsRefusedInsideIsolatedRun(t *testing.T) {
	f := newRoutineIsolationFixture(t)
	f.main(t)
	f.schedule(t, "isolated", pa.RoutineTrigger{Type: "once", At: f.now.UnixMilli()})
	f.tick(t)
	s := f.receive(t)
	_, err := f.r.Routine(context.Background(), s.open.Binding.Fence, f.export(t).Conversation.ActiveTurnID, app.RoutineRequest{Action: "follow_up", Minutes: 1, Content: "Synthetic status pending", IdempotencyKey: "forbidden"})
	if pa.Code(err) != pa.ScopeDenied {
		t.Fatal("isolated run set a main follow-up", err)
	}
	s.reply <- app.PromptResult{FinalText: "done"}
	f.idle(t)
}

func TestRoutineFollowUpLimitsCancelAndStopKeepFuture(t *testing.T) {
	f := newRoutineIsolationFixture(t)
	f.submit(t, "person")
	main := f.receive(t)
	call := func(q app.RoutineRequest) (app.RoutineResult, error) {
		return f.r.Routine(context.Background(), main.open.Binding.Fence, f.export(t).Conversation.ActiveTurnID, q)
	}
	for _, q := range []app.RoutineRequest{
		{Minutes: 0, Content: "Synthetic pending"}, {Minutes: pa.MaxRoutineFollowUpSeconds/60 + 1, Content: "Synthetic pending"}, {Minutes: 1, Content: strings.Repeat("界", pa.MaxRoutineFollowUpTokens+1)},
	} {
		q.Action, q.IdempotencyKey = "follow_up", fmt.Sprintf("invalid-%d-%d", q.Minutes, len(q.Content))
		if _, err := call(q); err == nil {
			t.Fatal("invalid follow-up accepted", q)
		}
	}
	ids := []string{}
	for i := 0; i < pa.MaxRoutineFollowUps; i++ {
		q := app.RoutineRequest{Action: "follow_up", Minutes: pa.MaxRoutineFollowUpSeconds / 60, Content: "Synthetic pending", IdempotencyKey: fmt.Sprintf("follow-%d", i)}
		if i == 0 {
			q.Minutes, q.Content = 1, strings.Repeat("界", pa.MaxRoutineFollowUpTokens)
		}
		out, err := call(q)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, out.ID)
	}
	if _, err := call(app.RoutineRequest{Action: "follow_up", Minutes: 1, Content: "Synthetic pending", IdempotencyKey: "overflow"}); pa.Code(err) != pa.CapacityFull {
		t.Fatal("follow-up queue is unbounded", err)
	}
	if _, err := call(app.RoutineRequest{Action: "cancel_follow_up", ID: ids[1], IdempotencyKey: "cancel"}); err != nil {
		t.Fatal(err)
	}
	if _, err := call(app.RoutineRequest{Action: "follow_up", Minutes: 60, Content: "Synthetic pending", IdempotencyKey: "replacement"}); err != nil {
		t.Fatal(err)
	}
	main.reply <- app.PromptResult{FinalText: "done"}
	f.idle(t)
	f.now = f.now.Add(2 * time.Minute)
	x := f.export(t)
	fence := pa.Fence{Key: f.b.Key, Project: f.b.Project, ConversationID: f.b.ConversationID, BindingID: x.Conversation.BindingID, Generation: x.Conversation.Generation}
	if _, err := f.r.Stop(context.Background(), app.StopRequest{Command: routineIsolationCommand(f.b.Key, pa.StopOperation, "stop-due"), Fence: fence}); err != nil {
		t.Fatal(err)
	}
	x = f.export(t)
	for _, d := range x.Routines {
		if d.ID == ids[0] && !d.Deleted {
			t.Fatal("stop retained due follow-up", d)
		}
		if d.ID == ids[2] && d.Deleted {
			t.Fatal("stop removed future follow-up", d)
		}
	}
}

func TestRoutineFollowUpChainStopsAndForgettingRefusesStaleNote(t *testing.T) {
	f := newRoutineIsolationFixture(t)
	f.main(t)
	d := pa.Routine{ID: "long-watch", Key: f.b.Key, Project: f.b.Project, ConversationID: f.b.ConversationID, Name: "Synthetic watch", Instruction: "Synthetic build pending", TimeoutSeconds: 3600, Enabled: true, Version: 1, FollowUp: true, FollowUpChain: pa.MaxRoutineFollowUpChain, MemoryRevision: 1}
	var err error
	d, err = pa.SetRoutineSchedule(d, pa.Routine{}, []pa.RoutineTrigger{{Type: "once", At: f.now.UnixMilli()}}, f.now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	f.commit(t, "watch", app.Mutation{Routine: &d})
	f.tick(t)
	s := f.receive(t)
	q := app.RoutineRequest{Action: "follow_up", Minutes: 1, Content: "Synthetic build pending", IdempotencyKey: "chain-overflow"}
	_, err = f.r.Routine(context.Background(), s.open.Binding.Fence, f.export(t).Conversation.ActiveTurnID, q)
	if pa.Code(err) != pa.CapacityFull {
		t.Fatal("follow-up chain kept running", err)
	}
	if _, err = f.s.Forget(context.Background(), app.ForgetRequest{Command: routineIsolationCommand(f.b.Key, pa.ForgetOperation, "forget-follow-up"), Project: f.b.Project, ExpectedRevision: 1, SourceIDs: []string{"synthetic-source"}}); err != nil {
		t.Fatal(err)
	}
	q.IdempotencyKey = "stale-after-forget"
	_, err = f.r.Routine(context.Background(), s.open.Binding.Fence, f.export(t).Conversation.ActiveTurnID, q)
	if pa.Code(err) != pa.ContextStale {
		t.Fatal("stale execution restored forgotten reminder", err)
	}
	s.reply <- app.PromptResult{FinalText: "done"}
	f.idle(t)
}
