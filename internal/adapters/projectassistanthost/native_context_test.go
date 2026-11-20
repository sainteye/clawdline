package projectassistanthost

import (
	"context"
	"sync"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type hostNativeContextReader interface {
	NativeContext(context.Context, pa.Fence) (app.NativeContextState, error)
}

func readHostNativeContext(t *testing.T, h *Host, fence pa.Fence) (app.NativeContextState, error) {
	t.Helper()
	r, ok := any(h).(hostNativeContextReader)
	if !ok {
		t.Fatal("Host cannot read its live native session")
	}
	return r.NativeContext(context.Background(), fence)
}

type contextFixtureProvider struct {
	*fixtureProvider
	mu                         sync.Mutex
	change                     func(*app.NativeContextState)
	closeStarted, closeRelease chan struct{}
}

func (p *contextFixtureProvider) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	s, err := p.fixtureProvider.Open(ctx, q, sink)
	return &contextFixtureSession{ProviderSession: s, p: p, fence: q.Binding.Fence}, err
}

type contextFixtureSession struct {
	app.ProviderSession
	p     *contextFixtureProvider
	fence pa.Fence
}

func (s *contextFixtureSession) Close(ctx context.Context) error {
	if s.p.closeStarted != nil {
		close(s.p.closeStarted)
		select {
		case <-s.p.closeRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.ProviderSession.Close(ctx)
}

func (s *contextFixtureSession) NativeContext() app.NativeContextState {
	s.p.mu.Lock()
	defer s.p.mu.Unlock()
	state := app.NativeContextState{Fence: s.fence, NativeSessionID: s.SessionID(), CommandsKnown: true, CompactAdvertised: true, UsageKnown: true, UsedTokens: 0, WindowTokens: 1000}
	if s.p.change != nil {
		s.p.change(&state)
	}
	return state
}

func liveContextFixture(t *testing.T) (*hostFixture, *contextFixtureProvider, pa.Bot, app.PromptRequest, *fixtureSession) {
	t.Helper()
	f := newHostFixture(t)
	p := &contextFixtureProvider{fixtureProvider: f.p}
	f.o.Provider = func() (app.Provider, error) { f.factories.Add(1); return p, nil }
	f.start(t)
	f.enable(t)
	b := f.create(t, "context")
	if _, err := f.send(t, b, "context-send"); err != nil {
		t.Fatal(err)
	}
	session := fixtureReceive(t, f.p.sessions)
	q := fixtureReceive(t, f.p.sent)
	return f, p, b, q, session
}

func assertUnknownNativeContext(t *testing.T, state app.NativeContextState) {
	t.Helper()
	if state.NativeSessionID != "" || state.CommandsKnown || state.CompactAdvertised || state.UsageKnown || state.UsedTokens != 0 || state.WindowTokens != 0 {
		t.Fatalf("unqualified live reading escaped: %+v", state)
	}
}

func TestHostNativeContextDoesNotStartRuntime(t *testing.T) {
	f := newHostFixture(t)
	f.start(t)
	state, _ := readHostNativeContext(t, f.h, pa.Fence{Key: pa.BotKey{MachineID: "source", BotID: "missing"}})
	assertUnknownNativeContext(t, state)
	f.enable(t)
	b := f.create(t, "idle")
	s, _ := f.h.Service()
	_, c, _, err := s.Get(context.Background(), b.Key)
	if err != nil {
		t.Fatal(err)
	}
	state, err = readHostNativeContext(t, f.h, pa.Fence{Key: b.Key, Project: b.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation})
	if err != nil {
		t.Fatal(err)
	}
	assertUnknownNativeContext(t, state)
	if f.h.engine != nil || f.factories.Load() != 0 || f.p.opens.Load() != 0 || f.p.caps.Load() != 0 {
		t.Fatal("a read allocated an execution owner or provider")
	}
}

func TestHostNativeContextTraversesOwnedSessionAndCloses(t *testing.T) {
	f, _, b, q, session := liveContextFixture(t)
	state, err := readHostNativeContext(t, f.h, q.Turn.Fence)
	if err != nil || state.TurnID != q.Turn.ID || state.Fence != q.Turn.Fence || state.NativeSessionID != session.SessionID() || !state.CommandsKnown || !state.CompactAdvertised || !state.UsageKnown || state.UsedTokens != 0 || state.WindowTokens != 1000 {
		t.Fatalf("live zero/command reading lost through Host composition: %+v %v", state, err)
	}
	session.reply <- app.PromptResult{FinalText: "finished"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	state, err = readHostNativeContext(t, f.h, q.Turn.Fence)
	if err != nil {
		t.Fatal(err)
	}
	assertUnknownNativeContext(t, state)
	if f.p.opens.Load() != 1 {
		t.Fatal("closed session was reopened for a read")
	}
	s, _ := f.h.Service()
	x, err := s.Export(context.Background(), b.Key)
	if err != nil || len(x.Turns) != 1 || len(x.Messages) != 2 || x.Conversation.ActiveTurnID != "" {
		t.Fatalf("read changed chat facts: %+v %v", x, err)
	}
	sample := x.Conversation.ContextUsage
	if sample.TurnID != q.Turn.ID || sample.Generation != q.Turn.Fence.Generation ||
		sample.BindingID != q.Turn.Fence.BindingID || sample.UsedTokens != 0 || sample.WindowTokens != 1000 {
		t.Fatalf("successful close did not retain the last known reading: %+v", sample)
	}
}

func TestHostNativeContextRefusesChangedBoundary(t *testing.T) {
	for _, name := range []string{"fence", "native-id", "native-fence", "revoked-before", "revoked-during", "project-during"} {
		t.Run(name, func(t *testing.T) {
			f, p, _, q, _ := liveContextFixture(t)
			fence := q.Turn.Fence
			switch name {
			case "fence":
				fence.Generation++
			case "native-id":
				p.change = func(s *app.NativeContextState) { s.NativeSessionID = "foreign" }
			case "native-fence":
				p.change = func(s *app.NativeContextState) { s.Fence.BindingID = "foreign" }
			case "revoked-before":
				f.revoked.Store(true)
			case "revoked-during":
				p.change = func(*app.NativeContextState) { f.revoked.Store(true) }
			case "project-during":
				p.change = func(*app.NativeContextState) { f.projectChanged.Store(true) }
			}
			state, err := readHostNativeContext(t, f.h, fence)
			assertUnknownNativeContext(t, state)
			if err == nil {
				t.Fatal("changed execution boundary was accepted")
			}
			if f.p.opens.Load() != 1 {
				t.Fatal("boundary refusal reopened a provider")
			}
		})
	}
}

func TestOwnedSessionNativeContextIsSealedBeforeCleanup(t *testing.T) {
	p := &contextFixtureProvider{}
	inner := &contextFixtureSession{ProviderSession: &fixtureSession{}, p: p}
	life := &workLifetime{}
	wrapped := wrapSession(inner, life)
	reader, ok := wrapped.(app.NativeContextReader)
	if !ok {
		t.Fatal("owned wrapper hides live context capability")
	}
	if !reader.NativeContext().UsageKnown {
		t.Fatal("live reading lost")
	}
	life.invalidate()
	assertUnknownNativeContext(t, reader.NativeContext())
}

func TestHostNativeContextUnsupportedAndUnknownCleanup(t *testing.T) {
	for _, cleanupUnknown := range []bool{false, true} {
		f := newHostFixture(t)
		f.start(t)
		f.enable(t)
		b := f.create(t, "unsupported")
		if _, err := f.send(t, b, "unsupported-send"); err != nil {
			t.Fatal(err)
		}
		session := fixtureReceive(t, f.p.sessions)
		q := fixtureReceive(t, f.p.sent)
		state, err := readHostNativeContext(t, f.h, q.Turn.Fence)
		if err != nil {
			t.Fatal(err)
		}
		assertUnknownNativeContext(t, state)
		f.p.unknown.Store(cleanupUnknown)
		session.reply <- app.PromptResult{FinalText: "finished"}
		fixtureEventually(t, func() bool { return f.p.closes.Load() > 0 })
		state, _ = readHostNativeContext(t, f.h, q.Turn.Fence)
		assertUnknownNativeContext(t, state)
	}
}

func TestHostNativeContextUnknownDuringOpenAndClose(t *testing.T) {
	f := newHostFixture(t)
	p := &contextFixtureProvider{fixtureProvider: f.p, closeStarted: make(chan struct{}), closeRelease: make(chan struct{})}
	f.p.openStarted, f.p.openRelease = make(chan struct{}), make(chan struct{})
	f.o.Provider = func() (app.Provider, error) { f.factories.Add(1); return p, nil }
	f.start(t)
	f.enable(t)
	b := f.create(t, "opening")
	if _, err := f.send(t, b, "opening-send"); err != nil {
		t.Fatal(err)
	}
	fixtureReceive(t, f.p.openStarted)
	s, _ := f.h.Service()
	_, c, _, err := s.Get(context.Background(), b.Key)
	if err != nil {
		t.Fatal(err)
	}
	fence := pa.Fence{Key: b.Key, Project: b.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}
	state, err := readHostNativeContext(t, f.h, fence)
	if err != nil {
		t.Fatal(err)
	}
	assertUnknownNativeContext(t, state)
	close(f.p.openRelease)
	session := fixtureReceive(t, f.p.sessions)
	q := fixtureReceive(t, f.p.sent)
	state, err = readHostNativeContext(t, f.h, q.Turn.Fence)
	if err != nil || !state.UsageKnown {
		t.Fatalf("live reading: %+v %v", state, err)
	}
	session.reply <- app.PromptResult{FinalText: "finished"}
	fixtureReceive(t, p.closeStarted)
	state, err = readHostNativeContext(t, f.h, q.Turn.Fence)
	if err != nil {
		t.Fatal(err)
	}
	assertUnknownNativeContext(t, state)
	close(p.closeRelease)
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
}

func TestHostNativeContextForgetDuringRead(t *testing.T) {
	f, p, b, q, _ := liveContextFixture(t)
	p.change = func(*app.NativeContextState) {
		current, err := f.h.store.Current(context.Background(), b.Key)
		if err != nil {
			t.Fatal(err)
		}
		s, _ := f.h.Service()
		_, err = s.Forget(context.Background(), app.ForgetRequest{Command: pa.Command{Key: b.Key, Operation: pa.ForgetOperation, IdempotencyKey: "forget-during-read"}, Project: b.Project, ExpectedRevision: current.Revision, SourceIDs: []string{"manual:old"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	state, err := readHostNativeContext(t, f.h, q.Turn.Fence)
	assertUnknownNativeContext(t, state)
	if pa.Code(err) != pa.ContextStale {
		t.Fatalf("Forget did not invalidate the old reading: %v", err)
	}
}

func TestOwnedSessionNativeContextSealedDuringRead(t *testing.T) {
	life := &workLifetime{}
	p := &contextFixtureProvider{change: func(*app.NativeContextState) { life.invalidate() }}
	inner := &contextFixtureSession{ProviderSession: &fixtureSession{}, p: p}
	reader, ok := wrapSession(inner, life).(app.NativeContextReader)
	if !ok {
		t.Fatal("owned wrapper hides live context capability")
	}
	assertUnknownNativeContext(t, reader.NativeContext())
}

func TestHostNativeContextBusyReadDoesNotWait(t *testing.T) {
	f, p, _, q, _ := liveContextFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	p.change = func(*app.NativeContextState) { close(entered); <-release }
	type result struct {
		state app.NativeContextState
		err   error
	}
	done := make(chan result, 1)
	reader := any(f.h).(hostNativeContextReader)
	go func() {
		state, err := reader.NativeContext(context.Background(), q.Turn.Fence)
		done <- result{state, err}
	}()
	fixtureReceive(t, entered)
	state, err := readHostNativeContext(t, f.h, q.Turn.Fence)
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	assertUnknownNativeContext(t, state)
	first := fixtureReceive(t, done)
	if first.err != nil || !first.state.UsageKnown {
		t.Fatalf("first read: %+v %v", first.state, first.err)
	}
}

func TestHostNativeContextKeepsEmptyAndUnknownDistinct(t *testing.T) {
	f, p, _, q, _ := liveContextFixture(t)
	p.change = func(s *app.NativeContextState) {
		s.CompactAdvertised = false // an observed empty command list
		s.UsageKnown = false
		s.UsedTokens, s.WindowTokens = 99, 100 // unqualified counters must not escape
	}
	state, err := readHostNativeContext(t, f.h, q.Turn.Fence)
	if err != nil || !state.CommandsKnown || state.CompactAdvertised || state.UsageKnown || state.UsedTokens != 0 || state.WindowTokens != 0 {
		t.Fatalf("empty commands/unknown usage: %+v %v", state, err)
	}
	if err = f.h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err = readHostNativeContext(t, f.h, q.Turn.Fence)
	assertUnknownNativeContext(t, state)
	if pa.Code(err) != pa.Disabled {
		t.Fatalf("closed Host was readable: %v", err)
	}
}
