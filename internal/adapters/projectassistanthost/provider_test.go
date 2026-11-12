package projectassistanthost

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type nativeFixture struct {
	f            *hostFixture
	tools        NativeWork
	bot          pa.Bot
	prompt       app.PromptRequest
	unregistered *atomic.Int64
}

func startNative(t *testing.T, configure func(*hostFixture)) *nativeFixture {
	t.Helper()
	f := newHostFixture(t)
	tools := make(chan NativeWork, 1)
	unregistered := &atomic.Int64{}
	f.o.NativeWork = func(_ context.Context, _ app.OpenRequest, w NativeWork) (func(), error) {
		tools <- w
		return func() { unregistered.Add(1) }, nil
	}
	if configure != nil {
		configure(f)
	}
	f.start(t)
	f.enable(t)
	bot := f.create(t, "bot")
	if _, e := f.send(t, bot, "one"); e != nil {
		t.Fatal(e)
	}
	return &nativeFixture{f: f, tools: fixtureReceive(t, tools), bot: bot, prompt: fixtureReceive(t, f.p.sent), unregistered: unregistered}
}

func TestNativeWorkScopesMessageIdentityAndClosesExactlyOnce(t *testing.T) {
	n := startNative(t, nil)
	ctx := context.Background()
	if e := n.tools.SendMessage(ctx, "visible", "One visible reply."); e != nil {
		t.Fatal(e)
	}
	if e := n.tools.SendMessage(ctx, "visible", "One visible reply."); e != nil {
		t.Fatal(e)
	}
	if e := n.tools.SendMessage(ctx, "visible", "Changed reply."); pa.Code(e) != pa.IdempotencyConflict {
		t.Fatal(e)
	}
	x, e := n.f.h.store.Export(ctx, n.bot.Key)
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, m := range x.Messages {
		if m.Kind == pa.AssistantMessage {
			count++
		}
	}
	if count != 1 {
		t.Fatal("repeated explicit message", count)
	}
	raw, e := json.Marshal(n.tools)
	if e != nil || string(raw) != "{}" {
		t.Fatal("private credential escaped", string(raw), e)
	}
	if e = n.f.h.Disable(ctx); e != nil {
		t.Fatal(e)
	}
	if e = n.tools.SendMessage(ctx, "late", "late"); e == nil {
		t.Fatal("closed tool accepted")
	}
	if _, e = n.tools.Dispatch(ctx, "late", "Change a.go", []string{"a.go"}); e == nil {
		t.Fatal("closed tool dispatched")
	}
	if _, e = n.tools.Status(ctx, "a1111111-1111-4111-8111-111111111111"); e == nil {
		t.Fatal("closed tool read")
	}
	_ = n.f.h.Close(ctx)
	_ = n.f.h.Disable(ctx)
	if n.unregistered.Load() != 1 {
		t.Fatal("registration not reclaimed exactly once", n.unregistered.Load())
	}
}

func projectionCommand(key pa.BotKey, id string) pa.Command {
	return pa.Command{Key: key, Operation: pa.SendOperation, IdempotencyKey: id, ContentHash: fmt.Sprintf("%x", sha256.Sum256([]byte(id)))}
}
func replaceNativeTurn(t *testing.T, n *nativeFixture, newGeneration bool) {
	t.Helper()
	ctx := context.Background()
	x, e := n.f.h.store.Export(ctx, n.bot.Key)
	if e != nil {
		t.Fatal(e)
	}
	c := x.Conversation
	var old pa.Turn
	for _, turn := range x.Turns {
		if turn.ID == c.ActiveTurnID {
			old = turn
		}
	}
	old.State = pa.TurnUnknown
	c.ActiveTurnID = ""
	if _, e = n.f.h.store.Commit(ctx, app.Mutation{Command: projectionCommand(n.bot.Key, "end-old"), ExpectedVersion: x.Bot.Version, Conversation: &c, Turn: &old, Receipt: pa.Receipt{ID: "end-old", State: pa.EffectUnknown}}); e != nil {
		t.Fatal(e)
	}
	x, e = n.f.h.store.Export(ctx, n.bot.Key)
	if e != nil {
		t.Fatal(e)
	}
	c = x.Conversation
	fence := old.Fence
	var binding *pa.ExecutionBinding
	if newGeneration {
		c.Generation++
		c.BindingID = "replacement-binding"
		fence.Generation = c.Generation
		fence.BindingID = c.BindingID
		b := pa.ExecutionBinding{Fence: fence, Agent: n.bot.Agent, MemoryRevision: 1, InstructionRevision: 1}
		binding = &b
	}
	c.ActiveTurnID = "replacement"
	c.LastMessageSeq++
	turn := pa.Turn{ID: "replacement", Fence: fence, MessageID: "replacement-input", State: pa.TurnRunning, Instructions: n.bot.Instructions, MemoryRevision: 1, IdempotencyKey: "replacement"}
	msg := pa.Message{ID: turn.MessageID, Fence: fence, TurnID: turn.ID, Seq: c.LastMessageSeq, Kind: pa.UserMessage, Text: "replacement", At: time.Now()}
	if _, e = n.f.h.store.Commit(ctx, app.Mutation{Command: projectionCommand(n.bot.Key, "replace"), ExpectedVersion: x.Bot.Version, Conversation: &c, Binding: binding, Turn: &turn, Messages: []pa.Message{msg}, Receipt: pa.Receipt{ID: "replace", State: pa.EffectAccepted}}); e != nil {
		t.Fatal(e)
	}
}

func TestNativeWorkRejectsCurrentRevocationAndCapturedTurnChanges(t *testing.T) {
	for _, mode := range []string{"source", "project", "same-binding-turn", "generation", "binding-revoked", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			n := startNative(t, func(f *hostFixture) {
				if mode == "deadline" {
					f.o.Limits.PromptTimeout = 100 * time.Millisecond
				}
			})
			switch mode {
			case "source":
				n.f.revoked.Store(true)
			case "project":
				n.f.projectChanged.Store(true)
			case "same-binding-turn":
				replaceNativeTurn(t, n, false)
			case "generation":
				replaceNativeTurn(t, n, true)
			case "binding-revoked":
				x, e := n.f.h.store.Export(context.Background(), n.bot.Key)
				if e != nil {
					t.Fatal(e)
				}
				binding := x.Bindings[len(x.Bindings)-1]
				for _, b := range x.Bindings {
					if b.Fence == n.prompt.Turn.Fence {
						binding = b
					}
				}
				binding.Revoked = true
				if _, e = n.f.h.store.Commit(context.Background(), app.Mutation{Command: projectionCommand(n.bot.Key, "revoke"), ExpectedVersion: x.Bot.Version, Binding: &binding, Receipt: pa.Receipt{ID: "revoke", State: pa.EffectDelivered}}); e != nil {
					t.Fatal(e)
				}
			case "deadline":
				fixtureEventually(t, func() bool { return n.unregistered.Load() == 1 })
			}
			before, e := n.f.h.store.Export(context.Background(), n.bot.Key)
			if e != nil {
				t.Fatal(e)
			}
			if e = n.tools.SendMessage(context.Background(), "stale", "must not publish"); e == nil {
				t.Fatal("stale closure succeeded")
			}
			if _, e = n.tools.Dispatch(context.Background(), "stale", "Change a.go", []string{"a.go"}); e == nil {
				t.Fatal("stale closure dispatched")
			}
			after, e := n.f.h.store.Export(context.Background(), n.bot.Key)
			if e != nil {
				t.Fatal(e)
			}
			if len(before.Messages) != len(after.Messages) || n.f.launcher.calls.Load() != 0 {
				t.Fatal("stale closure changed messages/work")
			}
			if n.unregistered.Load() != 1 {
				t.Fatal("stale registration retained")
			}
		})
	}
}

func TestNativeWorkCancellationBoundsAnAlreadyRunningCall(t *testing.T) {
	type callKey struct{}
	started := make(chan struct{})
	n := startNative(t, func(f *hostFixture) {
		source := f.o.Source
		f.o.Source = func(ctx context.Context, k pa.BotKey, write bool) (pa.SourceStatus, error) {
			if ctx.Value(callKey{}) == true {
				close(started)
				<-ctx.Done()
				return pa.SourceStatus{}, ctx.Err()
			}
			return source(ctx, k, write)
		}
	})
	done := make(chan error, 1)
	go func() {
		done <- n.tools.SendMessage(context.WithValue(context.Background(), callKey{}, true), "blocked", "must not publish")
	}()
	fixtureReceive(t, started)
	if e := n.f.h.Disable(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := fixtureReceive(t, done); e == nil {
		t.Fatal("canceled in-flight call succeeded")
	}
	if n.unregistered.Load() != 1 {
		t.Fatal("concurrent cancellation leaked registration")
	}
	x, e := n.f.h.store.Export(context.Background(), n.bot.Key)
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range x.Messages {
		if m.Kind == pa.AssistantMessage {
			t.Fatal("late call published")
		}
	}
}

func TestNativeWorkReclaimsRegistrationOnFailedSetup(t *testing.T) {
	for _, mode := range []string{"registration", "open"} {
		t.Run(mode, func(t *testing.T) {
			f := newHostFixture(t)
			var clears atomic.Int64
			captured := make(chan NativeWork, 1)
			f.o.NativeWork = func(_ context.Context, _ app.OpenRequest, w NativeWork) (func(), error) {
				captured <- w
				var e error
				if mode == "registration" {
					e = failure(pa.ScopeDenied, "fixture_registration")
				}
				return func() { clears.Add(1) }, e
			}
			if mode == "open" {
				f.p.openErr = failure(pa.AdapterNotFound, "fixture_open")
			}
			f.start(t)
			f.enable(t)
			b := f.create(t, "bot")
			if _, e := f.send(t, b, "one"); e != nil {
				t.Fatal(e)
			}
			w := fixtureReceive(t, captured)
			fixtureEventually(t, func() bool { return clears.Load() == 1 })
			if e := w.SendMessage(context.Background(), "late", "late"); e == nil {
				t.Fatal("failed setup retained usable tools")
			}
			if mode == "registration" && f.p.opens.Load() != 0 {
				t.Fatal("registration failure opened provider")
			}
		})
	}
}

func TestNativeWorkRequiresRealDeadlineAndSeparateDispatchPolicy(t *testing.T) {
	n := startNative(t, func(f *hostFixture) { f.o.DispatchPolicy = nil })
	if e := n.tools.SendMessage(context.Background(), "visible", "Chat remains usable."); e != nil {
		t.Fatal(e)
	}
	if _, e := n.tools.Dispatch(context.Background(), "work", "Change a.go", []string{"a.go"}); pa.Code(e) != pa.ScopeDenied {
		t.Fatal(e)
	}
	if _, _, e := n.f.h.tools(context.Background(), app.OpenRequest{Bot: n.bot}); pa.Code(e) != pa.Invalid {
		t.Fatal(e)
	}
	if n.f.launcher.calls.Load() != 0 {
		t.Fatal("missing policy dispatched")
	}
}

func TestNativeWorkRegistrationCanceledBeforeOpenIsReclaimed(t *testing.T) {
	f := newHostFixture(t)
	started := make(chan NativeWork, 1)
	var clears atomic.Int64
	f.o.NativeWork = func(ctx context.Context, _ app.OpenRequest, w NativeWork) (func(), error) {
		started <- w
		<-ctx.Done()
		return func() { clears.Add(1) }, nil
	}
	f.start(t)
	f.enable(t)
	b := f.create(t, "bot")
	if _, e := f.send(t, b, "one"); e != nil {
		t.Fatal(e)
	}
	w := fixtureReceive(t, started)
	if e := f.h.Disable(context.Background()); e != nil {
		t.Fatal(e)
	}
	if clears.Load() != 1 || f.p.opens.Load() != 0 {
		t.Fatal("canceled registration leaked or opened provider", clears.Load(), f.p.opens.Load())
	}
	if e := w.SendMessage(context.Background(), "late", "late"); e == nil {
		t.Fatal("canceled registration retained authority")
	}
	if e := f.h.Close(context.Background()); e != nil {
		t.Fatal(e)
	}
	if clears.Load() != 1 {
		t.Fatal("registration removed twice")
	}
}

func TestHostUnknownOpenWithoutHandleKeepsResourceOwner(t *testing.T) {
	f := newHostFixture(t)
	f.p.openErr = failure(pa.ExecutionEffectUnknown, "fixture_open_without_handle")
	f.start(t)
	f.enable(t)
	b := f.create(t, "bot")
	if _, e := f.send(t, b, "one"); e != nil {
		t.Fatal(e)
	}
	fixtureEventually(t, func() bool { return f.p.opens.Load() == 1 && !f.h.Usage().Known })
	for range 2 {
		if e := f.h.Disable(context.Background()); pa.Code(e) != pa.ExecutionEffectUnknown {
			t.Fatal(e)
		}
		u := f.h.Usage()
		if u.Known || u.Processes != 1 || u.ActiveActors != 1 {
			t.Fatal("unknown owner discarded", u)
		}
		if e := f.h.Enable(context.Background()); pa.Code(e) != pa.ExecutionEffectUnknown {
			t.Fatal(e)
		}
	}
	if f.p.opens.Load() != 1 || f.p.closes.Load() != 0 {
		t.Fatal("unknown Open replayed or fabricated cleanup")
	}
}

func TestHostRealStoreBrokerProjectsOneWorkResultAndRecoversDisabledHistory(t *testing.T) {
	n := startNative(t, nil)
	f := n.f
	ctx := context.Background()
	link, e := n.tools.Dispatch(ctx, "work", "Change only a.go in the isolated worktree.", []string{"a.go"})
	if e != nil {
		t.Fatal(e)
	}
	x, e := f.h.store.Export(ctx, n.bot.Key)
	if e != nil {
		t.Fatal(e)
	}
	updates := 0
	for _, event := range x.Events {
		if event.Kind == pa.WorkUpdated {
			updates++
		}
	}
	if updates != 1 || len(x.WorkLinks) != 1 {
		t.Fatalf("duplicate acceptance: updates=%d links=%d", updates, len(x.WorkLinks))
	}
	if _, e = n.tools.Dispatch(ctx, "work", "Change only a.go in the isolated worktree.", []string{"a.go"}); pa.Code(e) != pa.OutcomeUnknown {
		t.Fatal(e)
	}
	if _, e = n.tools.Dispatch(ctx, "work", "Different intent.", []string{"a.go"}); pa.Code(e) != pa.IdempotencyConflict {
		t.Fatal(e)
	}
	var original pa.Receipt
	for _, receipt := range x.Receipts {
		if receipt.Command.IdempotencyKey == "work" {
			original = receipt
		}
	}
	if original.State != pa.EffectUnknown {
		t.Fatal("original dispatch receipt changed", original)
	}
	r, _, e := f.o.Broker.Record(ctx, link.TaskID)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(r.Worktree.Path, "a.go"), []byte("package a\n// fixture delivered\n"), 0600); e != nil {
		t.Fatal(e)
	}
	fixtureGit(t, r.Worktree.Path, "add", "a.go")
	fixtureGit(t, r.Worktree.Path, "commit", "-q", "-m", "Scoped fixture delivery")
	commit := fixtureGit(t, r.Worktree.Path, "rev-parse", "HEAD")
	if _, e = f.o.Broker.Settle(ctx, r.ID, orchestrator.StateSuccess, "", &taskdir.Result{Status: "success", Summary: "One visible work result."}); e != nil {
		t.Fatal(e)
	}
	if _, e = n.tools.Status(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	boardID := "a1111111-1111-4111-8111-111111111111"
	if e = f.o.Broker.BindWork(ctx, r.ID, boardID, "fixture"); e != nil {
		t.Fatal(e)
	}
	fixtureGit(t, r.Repository, "merge", "--ff-only", r.Worktree.Branch)
	if _, e = f.o.Broker.Land(ctx, r.ID, orchestrator.LandingRequest{State: "landed", Target: "main", Commit: commit, Machine: true}); e != nil {
		t.Fatal(e)
	}
	link, e = n.tools.Status(ctx, r.ID)
	if e != nil || link.State != pa.WorkLanded || link.BoardItemID != boardID || link.LandingCommit != commit {
		t.Fatal(link, e)
	}
	other := f.create(t, "other")
	if _, e = f.h.RecoverWork(ctx, other.Key, r.ID, time.Now().Add(time.Minute)); e == nil {
		t.Fatal("foreign Bot read task")
	}
	if e = f.h.Disable(ctx); e != nil {
		t.Fatal(e)
	}
	factories, opens := f.factories.Load(), f.p.opens.Load()
	link, e = f.h.RecoverWork(ctx, n.bot.Key, r.ID, time.Now().Add(time.Minute))
	if e != nil || link.State != pa.WorkLanded {
		t.Fatal(link, e)
	}
	if f.h.engine != nil || f.factories.Load() != factories || f.p.opens.Load() != opens {
		t.Fatal("history initialized execution")
	}
	x, e = f.h.store.Export(ctx, n.bot.Key)
	if e != nil {
		t.Fatal(e)
	}
	messages := 0
	for _, m := range x.Messages {
		if m.Kind == pa.WorkMessage {
			messages++
			if m.Text != "One visible work result." {
				t.Fatal(m)
			}
		}
	}
	if messages != 1 || f.launcher.calls.Load() != 1 {
		t.Fatal("result duplicated or dispatch replayed", messages, f.launcher.calls.Load())
	}
	stored, e := f.h.store.Receipt(ctx, original.Command)
	if e != nil || *stored != original {
		t.Fatal("original unknown receipt rewritten", stored, e)
	}
	if _, e = f.h.RecoverWork(ctx, n.bot.Key, r.ID, time.Now().Add(-time.Second)); pa.Code(e) != pa.Expired {
		t.Fatal(e)
	}
	f.revoked.Store(true)
	if _, e = f.h.RecoverWork(ctx, n.bot.Key, r.ID, time.Now().Add(time.Minute)); pa.Code(e) != pa.Revoked {
		t.Fatal(e)
	}
}

func TestNativeWorkRechecksActiveTurnAtPublicationCAS(t *testing.T) {
	type raceKey struct{}
	entered, release := make(chan struct{}), make(chan struct{})
	n := startNative(t, func(f *hostFixture) {
		policy := f.o.DispatchPolicy
		f.o.DispatchPolicy = func(ctx context.Context, key pa.BotKey) (DispatchPolicy, error) {
			if ctx.Value(raceKey{}) == true {
				select {
				case <-entered:
				default:
					close(entered)
					<-release
				}
			}
			return policy(ctx, key)
		}
	})
	done := make(chan error, 1)
	go func() {
		done <- n.tools.SendMessage(context.WithValue(context.Background(), raceKey{}, true), "old-turn-race", "must not publish for old turn")
	}()
	fixtureReceive(t, entered)
	x, e := n.f.h.store.Export(context.Background(), n.bot.Key)
	if e != nil {
		t.Fatal(e)
	}
	c := x.Conversation
	c.ActiveTurnID = "new-active"
	c.LastMessageSeq++
	turn := pa.Turn{ID: "new-active", Fence: n.prompt.Turn.Fence, MessageID: "new-input", State: pa.TurnRunning, Instructions: n.bot.Instructions, MemoryRevision: 1, IdempotencyKey: "new-active"}
	msg := pa.Message{ID: turn.MessageID, Fence: turn.Fence, TurnID: turn.ID, Seq: c.LastMessageSeq, Kind: pa.UserMessage, Text: "new input", At: time.Now()}
	if _, e = n.f.h.store.Commit(context.Background(), app.Mutation{Command: projectionCommand(n.bot.Key, "move-active"), ExpectedVersion: x.Bot.Version, Conversation: &c, Turn: &turn, Messages: []pa.Message{msg}, Receipt: pa.Receipt{ID: "move-active", State: pa.EffectAccepted}}); e != nil {
		t.Fatal(e)
	}
	close(release)
	if e = fixtureReceive(t, done); e == nil {
		t.Fatal("old captured turn published after active turn moved during preflight")
	}
	x, e = n.f.h.store.Export(context.Background(), n.bot.Key)
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range x.Messages {
		if m.Text == "must not publish for old turn" {
			t.Fatal("stale message persisted")
		}
	}
}
