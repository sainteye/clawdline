package projectassistanthost

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The actual public NativeWork surface, including reads and discovery. A real
// SQLite trigger rejects the journal, rather than mocking the operation itself.
func TestEveryNativeWorkToolStopsBeforeEffectWhenJournalFails(t *testing.T) {
	calls := map[string]func(context.Context, NativeWork, string) error{
		"teamAvailable": func(c context.Context, w NativeWork, _ string) error { return w.TeamAvailable(c) },
		"team": func(c context.Context, w NativeWork, _ string) error {
			_, e := w.Team(c, "list", json.RawMessage(`{}`))
			return e
		},
		"listBots": func(c context.Context, w NativeWork, _ string) error {
			_, e := w.ListBots(c, pa.BotDirectoryQuery{})
			return e
		},
		"messageBot": func(c context.Context, w NativeWork, _ string) error {
			_, e := w.MessageBot(c, pa.BotMessageRequest{})
			return e
		},
		"routines": func(c context.Context, w NativeWork, _ string) error {
			_, e := w.Routines(c, app.RoutineRequest{Action: "list"})
			return e
		},
		"output": func(c context.Context, w NativeWork, p string) error {
			_, e := w.PublishOutput(c, "publish", app.OutputRequest{Path: p})
			return e
		},
		"artifacts": func(c context.Context, w NativeWork, _ string) error { _, e := w.ListArtifacts(c); return e },
		"memory":    func(c context.Context, w NativeWork, _ string) error { _, e := w.Memory(c); return e },
		"edit": func(c context.Context, w NativeWork, _ string) error {
			return w.EditMemory(c, "edit", app.MemoryEdit{})
		},
		"browserStatus": func(c context.Context, w NativeWork, _ string) error { _, e := w.BrowserStatus(c); return e },
		"browserCommand": func(c context.Context, w NativeWork, _ string) error {
			_, e := w.BrowserCommand(c, app.BrowserCommand{Op: "open", Args: json.RawMessage(`{"url":"https://example.invalid"}`)})
			return e
		},
		"send": func(c context.Context, w NativeWork, _ string) error {
			return w.SendMessage(c, "message", "Must not be published")
		},
		"dispatch": func(c context.Context, w NativeWork, _ string) error {
			_, e := w.Dispatch(c, "dispatch", "", "Change a.go", []string{"a.go"})
			return e
		},
		"status": func(c context.Context, w NativeWork, _ string) error { _, e := w.Status(c, "task"); return e },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			var browserCalls atomic.Int32
			n := startNative(t, func(f *hostFixture) {
				f.o.Browser = &BrowserAccess{
					Status: func(context.Context, string) (app.BrowserStatus, error) {
						browserCalls.Add(1)
						return app.BrowserStatus{}, nil
					},
					Command: func(context.Context, string, app.BrowserCommand) (app.BrowserResult, error) {
						browserCalls.Add(1)
						return app.BrowserResult{}, nil
					},
				}
			})
			ctx := context.Background()
			path := filepath.Join(n.f.repo, "report.txt")
			if e := os.WriteFile(path, []byte("fixture output"), 0600); e != nil {
				t.Fatal(e)
			}
			before, e := n.f.h.store.Export(ctx, n.bot.Key)
			if e != nil {
				t.Fatal(e)
			}
			// RAISE is observable in the returned error, so a disabled/malformed tool
			// cannot accidentally satisfy this assertion without reaching the journal.
			_, e = n.f.o.Database.Exec(`CREATE TRIGGER reject_tool_journal BEFORE UPDATE OF data ON project_assistant_events WHEN json_extract(NEW.data,'$.trace.category')='tool_activity' BEGIN SELECT RAISE(ABORT,'injected tool journal failure'); END`)
			if e != nil {
				t.Fatal(e)
			}
			e = call(ctx, n.tools, path)
			if e == nil || !strings.Contains(e.Error(), "injected tool journal failure") {
				t.Fatalf("journal failure was not propagated: %v", e)
			}
			after, e := n.f.h.store.Export(ctx, n.bot.Key)
			if e != nil {
				t.Fatal(e)
			}
			if after.Bot.Version != before.Bot.Version || len(after.Messages) != len(before.Messages) || after.Memory.Revision != before.Memory.Revision || len(after.WorkLinks) != len(before.WorkLinks) || len(after.Events) != len(before.Events) || len(after.Receipts) != len(before.Receipts) || n.f.launcher.calls.Load() != 0 || browserCalls.Load() != 0 {
				t.Fatal("journal failure advanced durable state or reached an external effect")
			}
		})
	}
}

func TestNativeWorkReadsHaveDistinctDurableInvocationIDs(t *testing.T) {
	n := startFallbackNative(t)
	ctx := context.Background()
	for range 2 {
		if _, e := n.tools.Memory(ctx); e != nil {
			t.Fatal(e)
		}
	}
	x, e := n.f.h.store.Export(ctx, n.bot.Key)
	if e != nil {
		t.Fatal(e)
	}
	ids := map[string]bool{}
	for _, event := range x.Events {
		if event.Trace != nil && event.Trace.Category == "tool_activity" {
			id := event.Trace.Text
			if ids[id] || !strings.Contains(id, "-memory-") {
				t.Fatal("unstable or duplicate Host invocation", id)
			}
			ids[id] = true
		}
	}
	if len(ids) != 2 {
		t.Fatalf("read invocation count=%d", len(ids))
	}
	turn, e := n.f.h.store.Turn(ctx, n.bot.Key, n.prompt.Turn.ID)
	if e != nil {
		t.Fatal(e)
	}
	fact := turn.Attempts[len(turn.Attempts)-1]
	if !fact.ToolActivity || !fact.NativeActivity || fact.Effect != "unknown" || pa.SafeAttemptClosed(turn) {
		t.Fatal("tool journal authorized replay", fact)
	}
}

type sendCallbackSession struct {
	app.ProviderSession
	tools NativeWork
	err   error
}

type independentSendSession struct {
	app.ProviderSession
	entered chan<- struct{}
	release <-chan struct{}
}

func (s *independentSendSession) Send(ctx context.Context, _ app.PromptRequest) (app.PendingPrompt, error) {
	s.entered <- struct{}{}
	select {
	case <-s.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestNativeWorkIndependentConsumerIsNotRefusedDuringSend(t *testing.T) {
	n := startNative(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), n.f.o.Limits.PromptTimeout)
	defer cancel()
	life, w, e := n.f.h.tools(ctx, app.OpenRequest{Bot: n.bot, Binding: pa.ExecutionBinding{Fence: n.prompt.Turn.Fence}})
	if e != nil {
		t.Fatal(e)
	}
	defer life.invalidate()
	entered, release := make(chan struct{}, 1), make(chan struct{})
	defer close(release)
	session := wrapSession(&independentSendSession{entered: entered, release: release}, life)
	done := make(chan error, 1)
	go func() { _, err := session.Send(ctx, n.prompt); done <- err }()
	fixtureReceive(t, entered)
	// A consumer has its own context, unlike a synchronous callback in Send.
	// Its journal may await the real engine memory lock without Send awaiting it.
	if _, e = w.Memory(context.Background()); e != nil {
		t.Fatalf("independent consumer was refused: %v", e)
	}
	cancel()
	fixtureReceive(t, done)
}

func (s *sendCallbackSession) Send(ctx context.Context, _ app.PromptRequest) (app.PendingPrompt, error) {
	_, s.err = s.tools.Memory(ctx)
	return nil, s.err
}

func TestNativeWorkRefusesSynchronousCallbackInsideSend(t *testing.T) {
	n := startNative(t, nil)
	// Use the same lifetime as these captured tools. The callback must return
	// before any store mutation, even when Send is invoked with a fresh context.
	// Its actual lifetime is captured from a fresh Host tools binding.
	ctx, cancel := context.WithTimeout(context.Background(), n.f.o.Limits.PromptTimeout)
	defer cancel()
	life, w, e := n.f.h.tools(ctx, app.OpenRequest{Bot: n.bot, Binding: pa.ExecutionBinding{Fence: n.prompt.Turn.Fence}})
	if e != nil {
		t.Fatal(e)
	}
	defer life.invalidate()
	underlying := &sendCallbackSession{tools: w}
	session := wrapSession(underlying, life)
	_, e = session.Send(ctx, n.prompt)
	if pa.Code(e) != pa.ExecutionEffectUnknown || !life.used.Load() {
		t.Fatalf("synchronous Send callback reached journal: %v", e)
	}
}

func startFallbackNative(t *testing.T) *nativeFixture {
	t.Helper()
	f := newHostFixture(t)
	tools := make(chan NativeWork, 1)
	unregister := &atomic.Int64{}
	f.o.NativeWork = func(_ context.Context, _ app.OpenRequest, w NativeWork) (func(), error) {
		tools <- w
		return func() { unregister.Add(1) }, nil
	}
	f.start(t)
	f.enable(t)
	service, e := f.h.Service()
	if e != nil {
		t.Fatal(e)
	}
	bot, _, e := service.Create(context.Background(), pa.Command{Key: pa.BotKey{MachineID: "source", BotID: "fallback"}, Operation: pa.CreateOperation, IdempotencyKey: "create-fallback"}, pa.CreateBot{Target: pa.ProjectSelection{MachineID: "source", ProjectID: "project"}, Name: "Fallback Bot", Agent: pa.Codex, FallbackAgents: []pa.Agent{pa.ClaudeCode}, Appearance: pa.Appearance{Shape: pa.Blob, Color: "#e39567"}, PermissionPolicy: pa.Ask, Instructions: "Read only"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.send(t, bot, "one"); e != nil {
		t.Fatal(e)
	}
	return &nativeFixture{f: f, tools: fixtureReceive(t, tools), bot: bot, prompt: fixtureReceive(t, f.p.sent), unregistered: unregister}
}
