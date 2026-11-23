package projectassistant

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type lifecycleProvider struct {
	*runtimeProvider
	openStarted  chan struct{}
	openRelease  chan struct{}
	capsStarted  chan struct{}
	capsRelease  chan struct{}
	closeRelease chan struct{}
	closeStarted chan struct{}
	closeUnknown atomic.Bool
	// closeFailures fails this many Close calls before delegating.
	closeFailures atomic.Int64
	closes        atomic.Int64
	once          sync.Once
}

func (p *lifecycleProvider) Capabilities(ctx context.Context, b pa.Bot) (pa.Capabilities, error) {
	if p.capsStarted != nil {
		p.once.Do(func() { close(p.capsStarted) })
		<-p.capsRelease
	}
	return p.runtimeProvider.Capabilities(ctx, b)
}
func (p *lifecycleProvider) Open(ctx context.Context, q OpenRequest, sink func(context.Context, ProtocolEvent) error) (ProviderSession, error) {
	if p.openStarted != nil {
		close(p.openStarted)
		<-p.openRelease
	}
	s, e := p.runtimeProvider.Open(ctx, q, sink)
	return &lifecycleSession{ProviderSession: s, p: p}, e
}

type lifecycleSession struct {
	ProviderSession
	p *lifecycleProvider
}

func (s *lifecycleSession) Close(ctx context.Context) error {
	s.p.closes.Add(1)
	if s.p.closeStarted != nil {
		s.p.once.Do(func() { close(s.p.closeStarted) })
	}
	if s.p.closeRelease != nil {
		<-s.p.closeRelease
	}
	if s.p.closeUnknown.Load() || s.p.closeFailures.Add(-1) >= 0 {
		return runtimeFailure(pa.ExecutionEffectUnknown, "fixture_cleanup")
	}
	return s.ProviderSession.Close(ctx)
}
func lifecycleFixture(t *testing.T) (*runtimeFixture, *lifecycleProvider) {
	f := newRuntimeFixture(t)
	p := &lifecycleProvider{runtimeProvider: f.provider}
	f.r.o.Provider = p
	f.r.o.Limits.CleanupTimeout = 30 * time.Millisecond
	return f, p
}
func TestRuntimeCloseRetainsLateOpenAndCancelsQueuedTurns(t *testing.T) {
	f, p := lifecycleFixture(t)
	p.openStarted = make(chan struct{})
	p.openRelease = make(chan struct{})
	first, err := f.submit(t, "one", "one")
	if err != nil {
		t.Fatal(err)
	}
	<-p.openStarted
	second, err := f.submit(t, "two", "two")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.r.Close(context.Background()); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal(err)
	}
	u := f.r.Usage()
	if u.Processes != 1 || u.ActiveActors != 1 || u.Known {
		t.Fatalf("late Open lost owner %+v", u)
	}
	if _, err = f.submit(t, "three", "three"); pa.Code(err) != pa.Disabled {
		t.Fatal(err)
	}
	close(p.openRelease)
	if err = f.r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitTurn(t, f, first.ResourceID, pa.TurnUnknown)
	awaitTurn(t, f, second.ResourceID, pa.TurnCancelled)
	if p.closes.Load() != 1 || f.provider.count() != 1 || len(f.provider.sent) != 0 {
		t.Fatal("late Open was sent or not closed once")
	}
	if u = f.r.Usage(); !u.Known || u.Processes != 0 || u.ActiveActors != 0 {
		t.Fatal(u)
	}
}
func TestRuntimeCloseRetainsUnknownCleanupUntilVerifiedRetry(t *testing.T) {
	f, p := lifecycleFixture(t)
	p.closeUnknown.Store(true)
	_, err := f.submit(t, "one", "one")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	if err = f.r.Close(context.Background()); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal(err)
	}
	u := f.r.Usage()
	if u.Known || u.Processes != 1 || u.ActiveActors != 1 {
		t.Fatal(u)
	}
	p.closeUnknown.Store(false)
	if err = f.r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if u = f.r.Usage(); !u.Known || u.Processes != 0 || u.ActiveActors != 0 {
		t.Fatal(u)
	}
}
func TestRuntimeCloseAdmissionAndBlockingCleanupStayBounded(t *testing.T) {
	t.Run("admission", func(t *testing.T) {
		f, p := lifecycleFixture(t)
		p.capsStarted = make(chan struct{})
		p.capsRelease = make(chan struct{})
		done := make(chan error, 1)
		go func() { _, e := f.submit(t, "one", "one"); done <- e }()
		<-p.capsStarted
		if e := f.r.Close(context.Background()); pa.Code(e) != pa.ExecutionEffectUnknown {
			t.Fatal(e)
		}
		if f.r.Usage().ActiveActors != 1 {
			t.Fatal("admission owner discarded")
		}
		close(p.capsRelease)
		if e := <-done; pa.Code(e) != pa.Disabled {
			t.Fatal(e)
		}
		if e := f.r.Close(context.Background()); e != nil {
			t.Fatal(e)
		}
		if len(f.store.messages) != 0 || f.provider.count() != 0 {
			t.Fatal("closed admission accepted")
		}
	})
	t.Run("cleanup", func(t *testing.T) {
		f, p := lifecycleFixture(t)
		p.closeStarted = make(chan struct{})
		p.closeRelease = make(chan struct{})
		_, err := f.submit(t, "one", "one")
		if err != nil {
			t.Fatal(err)
		}
		receivePrompt(t, f.provider)
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if e := f.r.Close(context.Background()); pa.Code(e) != pa.ExecutionEffectUnknown {
					t.Error(e)
				}
			}()
		}
		<-p.closeStarted
		wg.Wait()
		if p.closes.Load() != 1 || f.r.Usage().Processes != 1 {
			t.Fatal("duplicate or detached cleanup")
		}
		close(p.closeRelease)
		if e := f.r.Close(context.Background()); e != nil {
			t.Fatal(e)
		}
	})
}

// A finished turn whose Agent process could not be confirmed stopped keeps
// its owner. A later message is refused before anything is written, so the
// refusal must say it was not accepted rather than that its effect is unknown
// (observed 2026-11-23: five sends answered execution_effect_unknown while
// their receipts were absent).
func TestRuntimeRetainedCleanupRefusesNewMessageAsNotAccepted(t *testing.T) {
	f, p := lifecycleFixture(t)
	p.closeUnknown.Store(true)
	first, err := f.submit(t, "one", "one")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	f.provider.session().reply <- PromptResult{FinalText: "done"}
	awaitTurn(t, f, first.ResourceID, pa.TurnUnknown)
	eventually(t, func() bool { return !f.r.Usage().Known })

	_, err = f.submit(t, "two", "two")
	var refusal *pa.Failure
	if !errors.As(err, &refusal) || refusal.Code != pa.CapacityFull || refusal.Field != "owned_cleanup" {
		t.Fatalf("a message refused before admission was reported as %v", err)
	}
	cmd := pa.Command{Key: f.key, Operation: pa.SendOperation, IdempotencyKey: "two", ContentHash: strings.Repeat("a", 64)}
	if receipt, e := f.r.replay(context.Background(), cmd, pa.SendOperation); e != nil || receipt != nil {
		t.Fatalf("a refused message left a receipt %+v %v", receipt, e)
	}
	if f.provider.count() != 1 {
		t.Fatal("a refused message launched another Agent process")
	}
}

// An unconfirmed Agent cleanup used to keep the Bot refusing every message
// until Clawdline restarted (Beacon 15:20, Lens 17:50 on 2026-11-23). The
// retained owner now retries on its own with a bounded backoff and releases
// once cleanup is confirmed. The earlier turn keeps its unknown outcome and
// its prompt is never sent again.
func TestRuntimeRetainedCleanupRetriesAndReleasesWithoutRestart(t *testing.T) {
	f, p := lifecycleFixture(t)
	f.r.o.Limits.CleanupRetries, f.r.o.Limits.CleanupRetryDelay = 4, 2*time.Millisecond
	p.closeFailures.Store(2)
	first, err := f.submit(t, "one", "one")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	f.provider.session().reply <- PromptResult{FinalText: "done"}
	awaitTurn(t, f, first.ResourceID, pa.TurnUnknown)
	eventually(t, func() bool { u := f.r.Usage(); return u.Known && u.Processes == 0 && u.ActiveActors == 0 })
	if got := p.closes.Load(); got != 3 {
		t.Fatalf("closes=%d, want the failed close, one failed retry and one confirmed retry", got)
	}
	// The retry never rewrites the earlier turn: it keeps its unknown outcome.
	if x, _ := f.store.Turn(context.Background(), f.key, first.ResourceID); x.State != pa.TurnUnknown {
		t.Fatalf("a cleanup retry rewrote the earlier turn to %v", x.State)
	}
	second, err := f.submit(t, "two", "two")
	if err != nil {
		t.Fatalf("a Bot whose cleanup was confirmed on retry still refused: %v", err)
	}
	receivePrompt(t, f.provider)
	f.provider.session().reply <- PromptResult{FinalText: "two done"}
	awaitTurn(t, f, second.ResourceID, pa.TurnSucceeded)
	// A new message acknowledges the earlier unknown outcome; it stays an
	// unknown effect and its prompt was not sent again.
	if x, _ := f.store.Turn(context.Background(), f.key, first.ResourceID); pa.Code(x.Failure) != pa.ExecutionEffectUnknown {
		t.Fatalf("the earlier turn lost its unknown effect: %+v", x.Failure)
	}
	if f.provider.count() != 2 {
		t.Fatalf("processes=%d, want one per accepted message", f.provider.count())
	}
}

func TestRuntimeRetainedCleanupRetriesStayBounded(t *testing.T) {
	f, p := lifecycleFixture(t)
	f.r.o.Limits.CleanupRetries, f.r.o.Limits.CleanupRetryDelay = 2, time.Millisecond
	p.closeUnknown.Store(true)
	first, err := f.submit(t, "one", "one")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	f.provider.session().reply <- PromptResult{FinalText: "done"}
	awaitTurn(t, f, first.ResourceID, pa.TurnUnknown)
	eventually(t, func() bool { return p.closes.Load() == 3 })
	time.Sleep(20 * time.Millisecond)
	if got := p.closes.Load(); got != 3 {
		t.Fatalf("closes=%d, want the failed close plus exactly two retries", got)
	}
	if u := f.r.Usage(); u.Known || u.Processes != 1 {
		t.Fatalf("an unconfirmed cleanup was released: %+v", u)
	}
	if _, err = f.submit(t, "two", "two"); pa.Code(err) != pa.CapacityFull {
		t.Fatalf("an unconfirmed cleanup admitted a message: %v", err)
	}
}
