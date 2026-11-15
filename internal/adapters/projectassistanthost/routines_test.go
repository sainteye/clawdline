package projectassistanthost

import (
	"context"
	"fmt"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestRoutineTickStopRetainsItsOwnerUntilObserved(t *testing.T) {
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	// Replace only this fixture's tick with a controlled outstanding owner.
	f.h.routineStop()
	<-f.h.routineDone
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	done := make(chan struct{})
	defer close(done)
	f.h.routineStop = stop
	f.h.routineDone = done
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if e := f.h.Disable(cancelled); pa.Code(e) != pa.ExecutionEffectUnknown {
		t.Fatal(e)
	}
	if ctx.Err() == nil {
		t.Fatal("stop did not cancel its tick")
	}
	if e := f.h.Enable(context.Background()); pa.Code(e) != pa.ExecutionEffectUnknown {
		t.Fatalf("reenable replaced an outstanding tick: %v", e)
	}
	if f.factories.Load() != 0 {
		t.Fatal("tick cleanup constructed a provider")
	}
}

func TestRoutineCompletionRetryKeepsOneResultWithoutAnotherPrompt(t *testing.T) {
	f := newHostFixture(t)
	f.o.Limits.CleanupTimeout = 2 * time.Second
	f.start(t)
	f.enable(t)
	b := f.create(t, "bot")
	if _, e := f.send(t, b, "main"); e != nil {
		t.Fatal(e)
	}
	main := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	tools, ok := main.q.Tools.(app.RoutineTools)
	if !ok {
		t.Fatal("missing bound routine tools")
	}
	paused := false
	saved, e := tools.Routines(context.Background(), app.RoutineRequest{Action: "save", IdempotencyKey: "save", Name: "Fixture", Instruction: "Return one result", IntervalSeconds: 3600, Enabled: &paused})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tools.Routines(context.Background(), app.RoutineRequest{Action: "test", IdempotencyKey: "test", ID: saved.ID}); e != nil {
		t.Fatal(e)
	}
	// A reused key cannot turn an accepted test run into a deletion.
	_, e = tools.Routines(context.Background(), app.RoutineRequest{Action: "delete", IdempotencyKey: "test", ID: saved.ID})
	if pa.Code(e) != pa.IdempotencyConflict {
		t.Fatalf("action was absent from the durable command identity: %v", e)
	}
	if e = f.h.engine.TickRoutines(context.Background(), b.Key); e != nil {
		t.Fatal(e)
	}
	main.reply <- app.PromptResult{}
	routine := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	if routine.q.Binding.SessionSlot == "" || routine.q.Resume {
		t.Fatal("routine used main history")
	}
	x, e := f.h.store.Export(context.Background(), b.Key)
	if e != nil || len(x.RoutineRuns) != 1 {
		t.Fatal(x.RoutineRuns, e)
	}
	run := x.RoutineRuns[0]
	if _, e = f.send(t, b, "queued-human"); e != nil {
		t.Fatal(e)
	}
	sql := fmt.Sprintf(`CREATE TRIGGER fail_routine_terminal BEFORE INSERT ON project_assistant_receipts WHEN NEW.command_key='runtime-finish-%s' BEGIN SELECT RAISE(ABORT,'fixture terminal failure'); END`, run.TurnID)
	if _, e = f.o.Database.Exec(sql); e != nil {
		t.Fatal(e)
	}
	routine.reply <- app.PromptResult{FinalText: "One durable routine result."}
	// Usage can be unknown while a normal read holds the actor lock. The
	// public recovery refusal proves that the failed terminal commit actually
	// retained its owner before the fixture removes the injected SQL failure.
	fixtureEventually(t, func() bool { return pa.Code(f.h.Recover(context.Background(), b.Key)) == pa.ExecutionEffectUnknown })
	if f.p.opens.Load() != 2 {
		t.Fatal("failed terminal save replayed a prompt")
	}
	if _, e = f.o.Database.Exec(`DROP TRIGGER fail_routine_terminal`); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Disable(context.Background()); e != nil {
		t.Fatalf("verified completion could not be retried: %v", e)
	}
	x, e = f.h.store.Export(context.Background(), b.Key)
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, m := range x.Messages {
		if m.Text == "One durable routine result." {
			count++
			if m.TurnID != run.TurnID {
				t.Fatal("result changed turn")
			}
		}
	}
	if count != 1 || len(x.RoutineRuns) != 1 || x.RoutineRuns[0].State != pa.TurnSucceeded {
		t.Fatal("terminal retry lost or duplicated outcome", count, x.RoutineRuns)
	}
	f.enable(t)
	if f.p.opens.Load() != 2 {
		t.Fatal("reenable replayed the completed routine")
	}
}

type routineNoResumeProvider struct{ app.Provider }

func (p routineNoResumeProvider) Capabilities(ctx context.Context, b pa.Bot) (pa.Capabilities, error) {
	c, e := p.Provider.Capabilities(ctx, b)
	c.Resume = false
	return c, e
}

func TestRoutineUnsupportedResumeRefusesSavingWithoutBreakingChat(t *testing.T) {
	f := newHostFixture(t)
	original := f.o.Provider
	f.o.Provider = func() (app.Provider, error) { p, e := original(); return routineNoResumeProvider{p}, e }
	f.start(t)
	f.enable(t)
	b := f.create(t, "bot")
	if _, e := f.send(t, b, "main"); e != nil {
		t.Fatal(e)
	}
	main := fixtureReceive(t, f.p.sessions)
	fixtureReceive(t, f.p.sent)
	tools := main.q.Tools.(app.RoutineTools)
	_, e := tools.Routines(context.Background(), app.RoutineRequest{Action: "save", IdempotencyKey: "unsupported-save", Name: "Fixture", Instruction: "No replacement", IntervalSeconds: 3600})
	if pa.Code(e) != pa.ResumeUnsupported {
		t.Fatal(e)
	}
	x, e := f.h.store.Export(context.Background(), b.Key)
	if e != nil || len(x.Routines) != 0 || len(x.RoutineRuns) != 0 {
		t.Fatal("unsupported schedule was stored", x.Routines, e)
	}
	main.reply <- app.PromptResult{FinalText: "Ordinary chat remains available."}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	if f.p.opens.Load() != 1 {
		t.Fatal("unsupported schedule opened another session")
	}
}
