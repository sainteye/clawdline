package projectassistant_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	_ "modernc.org/sqlite"
)

// Exercise the real Engine and SQLite transaction boundary; only the native
// provider is scripted. No installed agent, timer owner or live Bot is used.
type routineIsolationFixture struct {
	db  *sql.DB
	s   *store.Store
	r   *app.Engine
	b   pa.Bot
	now time.Time
	seq atomic.Int64
	p   *routineIsolationProvider
}

type routineIsolationResolver struct{ scope pa.ProjectScope }

func (r routineIsolationResolver) Resolve(context.Context, pa.ProjectSelection) (pa.ProjectResolution, error) {
	return pa.ProjectResolution{Scope: r.scope, Directory: "fixture", Current: true, Available: true}, nil
}

type routineIsolationBridge struct{ app.WorkBridge }
type routineIsolationProvider struct{ sent chan *routineIsolationSession }

func (*routineIsolationProvider) Capabilities(_ context.Context, b pa.Bot) (pa.Capabilities, error) {
	return pa.Capabilities{Key: b.Key, Project: b.Project, Agent: b.Agent, CLIVersion: "fixture", AdapterVersion: "fixture", Evidence: "scripted-only", ReadOnlyEnforced: true, NativeMemory: pa.NativeMemoryDisabled, NativeMemoryVerified: true, Resume: true}, nil
}
func (p *routineIsolationProvider) Open(_ context.Context, q app.OpenRequest, _ func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	return &routineIsolationSession{p: p, open: q, reply: make(chan app.PromptResult, 1)}, nil
}

type routineIsolationSession struct {
	p      *routineIsolationProvider
	open   app.OpenRequest
	prompt app.PromptRequest
	reply  chan app.PromptResult
}

func (s *routineIsolationSession) SessionID() string {
	if s.open.Binding.SessionSlot == "" {
		return "main-A"
	}
	return "routine-B"
}
func (s *routineIsolationSession) Send(_ context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	s.prompt = q
	s.p.sent <- s
	return s, nil
}
func (s *routineIsolationSession) Wait(ctx context.Context) (app.PromptResult, error) {
	select {
	case v := <-s.reply:
		return v, nil
	case <-ctx.Done():
		return app.PromptResult{}, ctx.Err()
	}
}
func (*routineIsolationSession) Answer(context.Context, string, string) error { return nil }
func (*routineIsolationSession) Cancel(context.Context) error                 { return nil }
func (*routineIsolationSession) Close(context.Context) error                  { return nil }

func routineIsolationCommand(key pa.BotKey, op pa.Operation, id string) pa.Command {
	return pa.Command{Key: key, Operation: op, IdempotencyKey: id, ContentHash: fmt.Sprintf("%x", sha256.Sum256([]byte(id)))}
}
func newRoutineIsolationFixture(t *testing.T) *routineIsolationFixture {
	t.Helper()
	f := &routineIsolationFixture{now: time.Date(2096, 2, 29, 0, 0, 0, 0, time.UTC), p: &routineIsolationProvider{sent: make(chan *routineIsolationSession, pa.MaxPendingTurns)}}
	var err error
	f.db, err = sql.Open("sqlite", filepath.Join(t.TempDir(), "routines.db"))
	if err != nil {
		t.Fatal(err)
	}
	f.db.SetMaxOpenConns(1)
	t.Cleanup(func() { f.db.Close() })
	if err = (store.Installer{}).Install(context.Background(), f.db); err != nil {
		t.Fatal(err)
	}
	f.s = store.New(f.db)
	f.b = pa.Bot{Key: pa.BotKey{MachineID: "machine", BotID: "bot"}, Project: pa.ProjectScope{MachineID: "machine", ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}, ConversationID: "chat", Name: "Fixture", Appearance: pa.Appearance{Shape: pa.Blob, Color: "#e39567"}, Agent: pa.Codex, PermissionPolicy: pa.Ask, Instructions: pa.InstructionSnapshot{Revision: 1, Text: "Fixed instructions"}, Version: 1, CreatedAt: f.now, UpdatedAt: f.now}
	c := pa.Conversation{Key: f.b.Key, ID: f.b.ConversationID, Version: 1, MemoryRevision: 1}
	if _, err = f.s.Commit(context.Background(), app.Mutation{Command: routineIsolationCommand(f.b.Key, pa.CreateOperation, "create"), Bot: &f.b, Conversation: &c, Receipt: pa.Receipt{ID: "create", State: pa.EffectDelivered}}); err != nil {
		t.Fatal(err)
	}
	f.r, err = app.NewRuntime(app.RuntimeOptions{Repository: f.s, Memory: f.s, WorkBridge: routineIsolationBridge{}, ProjectResolver: routineIsolationResolver{f.b.Project}, Provider: f.p, Clock: func() time.Time { return f.now }, ID: func() string { return fmt.Sprintf("generated-%d", f.seq.Add(1)) }, Authorize: func(context.Context, pa.BotKey) (pa.FeatureState, pa.SourceStatus, error) {
		return pa.FeatureState{Enabled: true}, f.source(), nil
	}, Limits: app.RuntimeLimits{MaxBots: 1, MaxProcesses: 1, PromptTimeout: 5 * time.Second, CleanupTimeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.r.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return f
}
func (f *routineIsolationFixture) source() pa.SourceStatus {
	return pa.SourceStatus{MachineID: f.b.Key.MachineID, State: pa.SourceOnline, Readable: true, Writable: true, Fresh: true, Access: pa.Interactive}
}
func (f *routineIsolationFixture) commit(t *testing.T, id string, m app.Mutation) {
	t.Helper()
	b, err := f.s.Bot(context.Background(), f.b.Key)
	if err != nil {
		t.Fatal(err)
	}
	m.Command, m.ExpectedVersion, m.Receipt = routineIsolationCommand(b.Key, pa.EditOperation, id), b.Version, pa.Receipt{ID: id, State: pa.EffectDelivered}
	if _, err = f.s.Commit(context.Background(), m); err != nil {
		t.Fatal(err)
	}
}
func (f *routineIsolationFixture) export(t *testing.T) app.Export {
	t.Helper()
	x, err := f.s.Export(context.Background(), f.b.Key)
	if err != nil {
		t.Fatal(err)
	}
	return x
}
func (f *routineIsolationFixture) schedule(t *testing.T, id string, triggers ...pa.RoutineTrigger) pa.Routine {
	t.Helper()
	d := pa.Routine{ID: id, Key: f.b.Key, Project: f.b.Project, ConversationID: f.b.ConversationID, Name: id, Instruction: "Run " + id, TimeoutSeconds: 3600, Enabled: true, Version: 1}
	d, err := pa.SetRoutineSchedule(d, pa.Routine{}, triggers, f.now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	f.commit(t, "save-"+id, app.Mutation{Routine: &d})
	return d
}
func (f *routineIsolationFixture) tick(t *testing.T) {
	t.Helper()
	if err := f.r.TickRoutines(context.Background(), f.b.Key); err != nil {
		t.Fatalf("one exhausted definition blocked the Bot tick: %v", err)
	}
}
func (f *routineIsolationFixture) submit(t *testing.T, id string) {
	t.Helper()
	x := f.export(t)
	fence := pa.Fence{Key: f.b.Key, Project: f.b.Project, ConversationID: f.b.ConversationID, BindingID: x.Conversation.BindingID, Generation: x.Conversation.Generation}
	_, err := f.r.Submit(context.Background(), app.Submission{Command: routineIsolationCommand(f.b.Key, pa.SendOperation, id), Message: pa.SendMessage{Fence: fence, Text: id}, Feature: pa.FeatureState{Enabled: true}, Source: f.source()})
	if err != nil {
		t.Fatal(err)
	}
}
func (f *routineIsolationFixture) receive(t *testing.T) *routineIsolationSession {
	t.Helper()
	select {
	case s := <-f.p.sent:
		return s
	case <-time.After(3 * time.Second):
		t.Fatal("healthy queued routine did not reach the provider")
		return nil
	}
}
func (f *routineIsolationFixture) idle(t *testing.T) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		if f.r.Usage().ActiveActors == 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("actor did not finish")
			return
		case <-time.After(time.Millisecond):
		}
	}
}
func (f *routineIsolationFixture) main(t *testing.T) {
	t.Helper()
	f.submit(t, "main-before")
	s := f.receive(t)
	if s.open.Binding.SessionSlot != "" || s.SessionID() != "main-A" {
		t.Fatal("main history changed")
	}
	s.reply <- app.PromptResult{FinalText: "Main before"}
	f.idle(t)
}
func (f *routineIsolationFixture) exhausted(t *testing.T) pa.Routine {
	t.Helper()
	// This was valid at save time. After leap day 2096, the next leap day is
	// in 2104 (2100 is not a leap year), beyond robfig's five-year search.
	// Earlier OR entries would be advanced before the cron error. None may
	// leak out of the failed advancement or be silently consumed.
	d := f.schedule(t, "a-exhausted", pa.RoutineTrigger{Type: "once", At: f.now.UnixMilli()}, pa.RoutineTrigger{Type: "interval", Seconds: 60}, pa.RoutineTrigger{Type: "cron", Expression: "0 0 29 2 *", TimeZone: "UTC"})
	if _, err := pa.AdvanceRoutineSchedule(d, f.now); pa.Code(err) != pa.Invalid {
		t.Fatalf("fixture does not exhaust the real parser: %v", err)
	}
	return d
}

func TestRoutineScheduleIsolationKeepsHealthyQueuedWorkAndMainHistory(t *testing.T) {
	f := newRoutineIsolationFixture(t)
	f.main(t)
	bad := f.exhausted(t)
	healthy := f.schedule(t, "b-healthy", pa.RoutineTrigger{Type: "once", At: f.now.UnixMilli()}, pa.RoutineTrigger{Type: "interval", Seconds: 60})
	run := pa.RoutineRun{ID: "queued-healthy", RoutineID: healthy.ID, Source: "schedule", Key: f.b.Key, Project: f.b.Project, ConversationID: f.b.ConversationID, State: pa.TurnQueued, CreatedAt: f.now.Add(-time.Minute), TimeoutSeconds: 3600}
	f.commit(t, "queued-wake", app.Mutation{RoutineRun: &run})
	f.tick(t)
	s := f.receive(t)
	if s.open.Binding.SessionSlot != "routine-queued-healthy" || s.open.Resume || s.prompt.Text != healthy.Instruction {
		t.Fatal("queued work lost its isolated history or instruction")
	}
	s.reply <- app.PromptResult{FinalText: "Healthy routine completed"}
	f.idle(t)
	f.tick(t)
	x := f.export(t)
	if len(x.Routines) != 2 || x.Routines[0].Enabled || x.Routines[0].Deleted || !reflect.DeepEqual(x.Routines[0].Triggers, bad.Triggers) || !reflect.DeepEqual(x.Routines[0].NextRuns, bad.NextRuns) || !x.Routines[0].NextDue.Equal(bad.NextDue) {
		t.Fatal("failed definition lost its original due intent", x.Routines)
	}
	if !x.Routines[1].Enabled || !x.Routines[1].NextRuns[0].IsZero() || !x.Routines[1].NextDue.Equal(f.now.Add(time.Minute)) {
		t.Fatal("once/OR or interval cadence changed", x.Routines[1])
	}
	if len(x.RoutineRuns) != 2 {
		t.Fatal("coalesced wake or failure was repeated", x.RoutineRuns)
	}
	failedRunID := ""
	for _, run := range x.RoutineRuns {
		if run.RoutineID == bad.ID {
			failedRunID = run.ID
			if run.State != pa.TurnFailed || run.FinishedAt == nil || !run.Published || run.BindingID != "" {
				t.Fatal("schedule failure claims a native run or lacks a terminal outcome", run)
			}
		}
	}
	failures := 0
	for _, turn := range x.Turns {
		if turn.Failure != nil && turn.Failure.Field == "routine_cron_future" {
			failures++
			if turn.State != pa.TurnFailed || turn.Failure.Code != pa.Invalid || turn.RoutineRunID != failedRunID || turn.RoutineRunID == "" || turn.StartedAt != nil {
				t.Fatal("schedule failure lost its type or claims execution", turn)
			}
		}
	}
	if failures != 1 {
		t.Fatal("missing durable per-definition failure", x.Turns)
	}
	failedReceipts := 0
	for _, receipt := range x.Receipts {
		if receipt.ResourceID == bad.ID && receipt.State == pa.EffectFailed {
			failedReceipts++
		}
	}
	if failedReceipts != 1 {
		t.Fatal("failure receipt missing or repeated", failedReceipts)
	}
	events, err := f.s.Read(context.Background(), pa.EventQuery{Key: f.b.Key, PageSize: pa.MaxEventPageRows})
	if err != nil {
		t.Fatal(err)
	}
	failureEvents := 0
	for _, e := range events.Items {
		if e.Turn != nil && e.Turn.Failure != nil && e.Turn.Failure.Field == "routine_cron_future" {
			failureEvents++
		}
	}
	if failureEvents != 1 {
		t.Fatal("failure event missing or repeated", failureEvents)
	}
	f.submit(t, "main-after")
	s = f.receive(t)
	if !s.open.Resume || s.open.Binding.NativeSessionID != "main-A" || s.open.Binding.SessionSlot != "" {
		t.Fatal("routine replaced main A history", s.open)
	}
	s.reply <- app.PromptResult{FinalText: "Main after"}
	f.idle(t)
}

func TestRoutineScheduleIsolationRetainsUnknownBarrier(t *testing.T) {
	f := newRoutineIsolationFixture(t)
	f.main(t)
	f.exhausted(t)
	f.schedule(t, "b-once", pa.RoutineTrigger{Type: "once", At: f.now.UnixMilli()})
	x := f.export(t)
	fence := pa.Fence{Key: f.b.Key, Project: f.b.Project, ConversationID: f.b.ConversationID, BindingID: x.Conversation.BindingID, Generation: x.Conversation.Generation}
	turn := pa.Turn{ID: "unknown", Fence: fence, MessageID: "unknown-message", State: pa.TurnUnknown, IdempotencyKey: "unknown", MemoryRevision: 1, Instructions: f.b.Instructions, Failure: &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "execution"}}
	msg := pa.Message{ID: turn.MessageID, Fence: fence, TurnID: turn.ID, Kind: pa.UserMessage, Text: "Uncertain prior execution", At: f.now}
	f.commit(t, "unknown", app.Mutation{Turn: &turn, Messages: []pa.Message{msg}})
	f.tick(t)
	f.tick(t)
	x = f.export(t)
	queued := 0
	for _, r := range x.RoutineRuns {
		if r.State == pa.TurnQueued {
			queued++
			if r.TurnID != "" {
				t.Fatal("unknown barrier admitted another prompt", r)
			}
		}
	}
	if queued != 1 || len(x.RoutineRuns) != 2 || !x.Routines[1].NextDue.IsZero() {
		t.Fatal("unknown barrier lost or repeated the once wake", x.RoutineRuns)
	}
	select {
	case <-f.p.sent:
		t.Fatal("unknown execution permitted another prompt")
	default:
	}
}

func TestRoutineScheduleIsolationStorageFailureHaltsAndRollsBack(t *testing.T) {
	f := newRoutineIsolationFixture(t)
	f.main(t)
	f.exhausted(t)
	f.schedule(t, "b-healthy", pa.RoutineTrigger{Type: "interval", Seconds: 60})
	before := f.export(t)
	if _, err := f.db.Exec(`CREATE TRIGGER reject_schedule_failure BEFORE INSERT ON project_assistant_receipts WHEN NEW.command_key LIKE 'routine-due-%' BEGIN SELECT RAISE(ABORT,'fixture schedule storage failure'); END`); err != nil {
		t.Fatal(err)
	}
	err := f.r.TickRoutines(context.Background(), f.b.Key)
	if err == nil || !strings.Contains(err.Error(), "fixture schedule storage failure") {
		t.Fatalf("storage failure did not halt safely: %v", err)
	}
	if after := f.export(t); !reflect.DeepEqual(before, after) {
		t.Fatal("failed failure receipt consumed intent or admitted work")
	}
	select {
	case <-f.p.sent:
		t.Fatal("storage failure permitted another prompt")
	default:
	}
	if _, err = f.db.Exec(`DROP TRIGGER reject_schedule_failure`); err != nil {
		t.Fatal(err)
	}
	f.tick(t)
	s := f.receive(t)
	s.reply <- app.PromptResult{}
	f.idle(t)
}
