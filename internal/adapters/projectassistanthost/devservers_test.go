package projectassistanthost

import (
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type hostServerProcess struct {
	exit     chan struct{}
	once     sync.Once
	closed   atomic.Bool
	closeErr error
	observe  func(context.Context) ([]servers.Listener, error)
}

func (p *hostServerProcess) TCPListeners(ctx context.Context) ([]servers.Listener, error) {
	if p.observe == nil {
		return nil, failure(pa.Unsupported, "synthetic_observer")
	}
	return p.observe(ctx)
}

func TestHostServerRevocationKeepsUnknownCleanupOwnedAndBlocksReplacement(t *testing.T) {
	f, count, started := newServerHostFixture(t)
	bot := f.create(t, "server-unknown")
	tools, session := beginServerNative(t, f, bot, "one")
	_ = startReviewedServer(t, f, bot, tools, "start")
	process := fixtureReceive(t, started)
	process.closeErr = errors.New("synthetic process-tree cleanup uncertainty")
	f.revoked.Store(true)
	if err := f.h.serverRegistry.Reconcile(context.Background(), f.h.serverCheck); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal(err)
	}
	if u := f.h.serverRegistry.Usage(); u.Known || u.Processes != 1 || !process.closed.Load() {
		t.Fatal("uncertain cleanup released its owner", u)
	}
	if u, err := f.h.ServerUsage(context.Background()); err != nil || u.Known || u.Processes != 1 {
		t.Fatal("source usage concealed unknown cleanup", u, err)
	}
	session.reply <- app.PromptResult{FinalText: "Revoked"}
	if err := f.h.Disable(context.Background()); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal(err)
	}
	f.revoked.Store(false)
	if err := f.h.Enable(context.Background()); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal("unknown owner was replaced", err)
	}
	if count.Load() != 1 {
		t.Fatal("uncertain command was replayed")
	}
}

func (p *hostServerProcess) Output() io.Reader {
	return io.NopCloser(strings.NewReader("first source line\nlast source line\n"))
}
func (p *hostServerProcess) WaitExit(ctx context.Context) error {
	select {
	case <-p.exit:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (p *hostServerProcess) Close(context.Context) error {
	p.closed.Store(true)
	p.once.Do(func() { close(p.exit) })
	return p.closeErr
}

func newServerHostFixture(t *testing.T, observers ...func(context.Context) ([]servers.Listener, error)) (*hostFixture, *atomic.Int32, chan *hostServerProcess) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("source launcher is unavailable on Windows")
	}
	f := newHostFixture(t)
	count := &atomic.Int32{}
	started := make(chan *hostServerProcess, 10)
	f.o.ServerLaunch = func(ctx context.Context, spec servers.Spec, project pa.ProjectResolution) (servers.Process, error) {
		if spec.Key.MachineID != "source" || spec.Project != project.Scope || spec.Directory != f.repo {
			t.Error("source identity was not resolved", spec, project)
		}
		count.Add(1)
		p := &hostServerProcess{exit: make(chan struct{})}
		if len(observers) > 0 {
			p.observe = observers[0]
		}
		started <- p
		return p, nil
	}
	f.start(t)
	f.enable(t)
	return f, count, started
}

func beginServerNative(t *testing.T, f *hostFixture, bot pa.Bot, key string) (NativeWork, *fixtureSession) {
	t.Helper()
	if _, err := f.send(t, bot, key); err != nil {
		t.Fatal(err)
	}
	_ = fixtureReceive(t, f.p.sent)
	session := fixtureReceive(t, f.p.sessions)
	return session.q.Tools.(NativeWork), session
}

func startReviewedServer(t *testing.T, f *hostFixture, bot pa.Bot, tools NativeWork, key string, choices ...pa.PermissionOptionKind) app.DevServerInfo {
	t.Helper()
	done := make(chan struct {
		r app.DevServerResult
		e error
	}, 1)
	go func() {
		r, e := tools.DevServers(context.Background(), app.DevServerRequest{Action: "start", Key: key, Name: "Synthetic source command", Command: "synthetic-server --foreground"})
		done <- struct {
			r app.DevServerResult
			e error
		}{r, e}
	}()
	offer := hostSourceOffer(t, f, bot.Key)
	service, _ := f.h.Service()
	choice := pa.AllowAlways
	if len(choices) > 0 {
		choice = choices[0]
	}
	if _, err := service.Answer(context.Background(), pa.Command{Key: bot.Key, Operation: pa.AnswerOperation, IdempotencyKey: "allow-" + key}, pa.PermissionAnswer{Fence: offer.Fence, TurnID: offer.TurnID, OfferID: offer.ID, OptionID: string(choice)}); err != nil {
		t.Fatal(err)
	}
	got := fixtureReceive(t, done)
	if got.e != nil || got.r.Server == nil {
		t.Fatal(got)
	}
	return *got.r.Server
}

func TestHostServerToolsKeepOwnershipAcrossTurnsAndMakeOldControlsHarmless(t *testing.T) {
	f, count, started := newServerHostFixture(t)
	bot := f.create(t, "server-bot")
	tools, session := beginServerNative(t, f, bot, "one")
	first := startReviewedServer(t, f, bot, tools, "start-one")
	process := fixtureReceive(t, started)
	if u, err := f.h.ServerUsage(context.Background()); err != nil || !u.Known || u.Definitions != 1 || u.Processes != 1 {
		t.Fatal("actual source occupancy", u, err)
	}
	fixtureEventually(t, func() bool {
		r, e := tools.DevServers(context.Background(), app.DevServerRequest{Action: "list"})
		return e == nil && len(r.Servers) == 1 && r.Servers[0].State == "running"
	})
	request := app.DevServerRequest{Action: "start", Key: "start-one", Name: "Synthetic source command", Command: "synthetic-server --foreground"}
	if again, err := tools.DevServers(context.Background(), request); err != nil || again.Server == nil || again.Server.ID != first.ID || count.Load() != 1 {
		t.Fatal("native retry launched again", again, err)
	}
	request.Command += " --changed"
	if _, err := tools.DevServers(context.Background(), request); pa.Code(err) != pa.IdempotencyConflict {
		t.Fatal(err)
	}
	fixtureEventually(t, func() bool {
		r, e := tools.DevServers(context.Background(), app.DevServerRequest{Action: "logs", ID: first.ID, Lines: 1})
		return e == nil && r.Log != nil && r.Log.Text == "last source line\n" && r.Log.Truncated
	})
	session.reply <- app.PromptResult{FinalText: "Source server started"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	if process.closed.Load() {
		t.Fatal("turn end stopped the Host's process")
	}
	if _, err := tools.DevServers(context.Background(), app.DevServerRequest{Action: "list"}); err == nil {
		t.Fatal("old native execution kept content authority")
	}
	current, next := beginServerNative(t, f, bot, "two")
	stopped, err := current.DevServers(context.Background(), app.DevServerRequest{Action: "stop", Key: "stop-old", ID: first.ID})
	if err != nil || stopped.Server == nil || stopped.Server.State != "stopped" || !process.closed.Load() {
		t.Fatal(stopped, err)
	}
	restarted, err := current.DevServers(context.Background(), app.DevServerRequest{Action: "start", Key: "restart", Name: "Synthetic source command", Command: "synthetic-server --foreground"})
	if err != nil || restarted.Server == nil || restarted.Server.Attempt == first.Attempt {
		t.Fatal(restarted, err)
	}
	second := fixtureReceive(t, started)
	if _, err := current.DevServers(context.Background(), app.DevServerRequest{Action: "stop", Key: "stop-old", ID: first.ID}); err != nil || second.closed.Load() {
		t.Fatal("cached old stop reached new attempt", err)
	}
	next.reply <- app.PromptResult{FinalText: "Source restarted"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	if err := f.h.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !second.closed.Load() || f.h.serverRegistry.Usage().Processes != 0 {
		t.Fatal("disable lost process ownership")
	}
	f.enable(t)
	if count.Load() != 2 {
		t.Fatal("enable replayed a command")
	}
	service, _ := f.h.Service()
	if err := service.Delete(context.Background(), pa.Command{Key: bot.Key, Operation: pa.DeleteOperation, IdempotencyKey: "delete-server-bot"}); err != nil {
		t.Fatal(err)
	}
	if got := f.h.serverRegistry.Usage().Definitions; got != 0 {
		t.Fatal("deleted Bot retained executable definitions", got)
	}
}

func TestHostServerForgetHidesOldDefinitionsAndReconcilesTheirProcesses(t *testing.T) {
	f, _, started := newServerHostFixture(t)
	bot := f.create(t, "server-forget")
	service, _ := f.h.Service()
	ctx := context.Background()
	if _, err := service.SaveMemory(ctx, app.MemoryWrite{Command: pa.Command{Key: bot.Key, Operation: pa.MemorySaveOperation, IdempotencyKey: "topic"}, ExpectedRevision: 1, Entry: pa.MemoryEntry{ID: "topic", Key: bot.Key, Project: bot.Project, Kind: pa.TopicMemory, Title: "Synthetic", Summary: "Synthetic", Text: "Synthetic"}}); err != nil {
		t.Fatal(err)
	}
	tools, session := beginServerNative(t, f, bot, "one")
	first := startReviewedServer(t, f, bot, tools, "start")
	process := fixtureReceive(t, started)
	if _, err := service.Forget(ctx, app.ForgetRequest{Command: pa.Command{Key: bot.Key, Operation: pa.ForgetOperation, IdempotencyKey: "forget"}, Project: bot.Project, ExpectedRevision: 2, SourceIDs: []string{"topic"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := tools.DevServers(ctx, app.DevServerRequest{Action: "logs", ID: first.ID}); err == nil {
		t.Fatal("old binding read forgotten output")
	}
	published := false
	if err := tools.PublishDevServers(ctx, func() error { published = true; return nil }); err == nil || published {
		t.Fatal("old binding published after Forget", err)
	}
	if err := f.h.serverRegistry.Reconcile(ctx, f.h.serverCheck); err != nil {
		t.Fatal(err)
	}
	if !process.closed.Load() {
		t.Fatal("Forget left the old source process running")
	}
	session.reply <- app.PromptResult{FinalText: "Old context ended"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	current, next := beginServerNative(t, f, bot, "two")
	if listed, err := current.DevServers(ctx, app.DevServerRequest{Action: "list"}); err != nil || len(listed.Servers) != 0 {
		t.Fatal("fresh binding disclosed old definitions", listed, err)
	}
	if _, err := current.DevServers(ctx, app.DevServerRequest{Action: "logs", ID: first.ID}); pa.Code(err) != pa.NotFound {
		t.Fatal(err)
	}
	next.reply <- app.PromptResult{FinalText: "Fresh context only"}
}
