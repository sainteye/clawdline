package projectassistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// This behavioral repository double enforces CAS, atomic idempotency and event
// shape. PA-03 integration must repeat these cases against its SQLite adapter.
type runtimeStore struct {
	Repository
	mu          sync.Mutex
	bot         pa.Bot
	c           pa.Conversation
	turns       map[string]pa.Turn
	order       []string
	bindings    map[string]pa.ExecutionBinding
	messages    []pa.Message
	permissions map[string]pa.PermissionOffer
	receipts    map[string]pa.Receipt
	events      []pa.Event
	work        []pa.WorkLink
	mutations   []Mutation
	failCommit  bool
}

func clone[T any](v T) T             { b, _ := json.Marshal(v); var x T; _ = json.Unmarshal(b, &x); return x }
func receiptKey(c pa.Command) string { return string(c.Operation) + ":" + c.IdempotencyKey }
func (s *runtimeStore) Bot(context.Context, pa.BotKey) (pa.Bot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.bot), nil
}
func (s *runtimeStore) Conversation(context.Context, pa.BotKey, string) (pa.Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.c, nil
}
func (s *runtimeStore) Turn(_ context.Context, _ pa.BotKey, id string) (pa.Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, ok := s.turns[id]
	if !ok {
		return x, runtimeFailure(pa.NotFound, "turn")
	}
	return clone(x), nil
}
func (s *runtimeStore) Binding(_ context.Context, _ pa.BotKey, id string) (pa.ExecutionBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, ok := s.bindings[id]
	if !ok {
		return x, runtimeFailure(pa.NotFound, "binding")
	}
	return x, nil
}
func (s *runtimeStore) Permission(_ context.Context, _ pa.BotKey, id string) (pa.PermissionOffer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, ok := s.permissions[id]
	if !ok {
		return x, runtimeFailure(pa.NotFound, "permission")
	}
	return clone(x), nil
}
func (s *runtimeStore) PendingTurns(context.Context, pa.BotKey) ([]pa.Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []pa.Turn
	for _, id := range s.order {
		x := s.turns[id]
		switch x.State {
		case pa.TurnQueued, pa.TurnAccepted, pa.TurnRunning, pa.TurnWaitingPermission, pa.TurnUnknown:
			out = append(out, clone(x))
		}
	}
	return out, nil
}
func (s *runtimeStore) Receipt(_ context.Context, c pa.Command) (*pa.Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if x, ok := s.receipts[receiptKey(c)]; ok {
		return &x, nil
	}
	return nil, nil
}
func (s *runtimeStore) Commit(_ context.Context, m Mutation) (pa.Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCommit {
		return pa.Receipt{}, runtimeFailure(pa.OutcomeUnknown, "injected_store_failure")
	}
	if x, ok := s.receipts[receiptKey(m.Command)]; ok {
		if x.Command != m.Command {
			return x, runtimeFailure(pa.IdempotencyConflict, "receipt")
		}
		return x, nil
	}
	if m.ExpectedVersion != s.bot.Version || m.Bot == nil || m.Bot.Version != s.bot.Version+1 {
		return pa.Receipt{}, runtimeFailure(pa.StaleVersion, "bot")
	}
	for i := range m.Events {
		m.Events[i].Seq = int64(len(s.events) + i + 1)
		e := m.Events[i]
		if err := pa.ValidateEvent(e); err != nil {
			return pa.Receipt{}, err
		}
	}
	s.bot = clone(*m.Bot)
	if m.Conversation != nil {
		s.c = *m.Conversation
	}
	if m.Turn != nil {
		if _, ok := s.turns[m.Turn.ID]; !ok {
			s.order = append(s.order, m.Turn.ID)
		}
		s.turns[m.Turn.ID] = clone(*m.Turn)
	}
	if m.Binding != nil {
		s.bindings[m.Binding.Fence.BindingID] = *m.Binding
	}
	for _, msg := range m.Messages {
		duplicate := false
		for _, old := range s.messages {
			if old.ID == msg.ID {
				duplicate = true
			}
		}
		if !duplicate {
			msg.Seq = int64(len(s.messages) + 1)
			s.messages = append(s.messages, msg)
		}
	}
	for _, p := range m.Permissions {
		s.permissions[p.ID] = clone(p)
	}
	s.work = append(s.work, clone(m.WorkLinks)...)
	s.events = append(s.events, clone(m.Events)...)
	s.receipts[receiptKey(m.Command)] = m.Receipt
	s.mutations = append(s.mutations, clone(m))
	return m.Receipt, nil
}
func (s *runtimeStore) Export(context.Context, pa.BotKey) (Export, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var turns []pa.Turn
	for _, id := range s.order {
		turns = append(turns, clone(s.turns[id]))
	}
	var permissions []pa.PermissionOffer
	for _, offer := range s.permissions {
		permissions = append(permissions, clone(offer))
	}
	return Export{Bot: clone(s.bot), Conversation: s.c, Messages: clone(s.messages), WorkLinks: clone(s.work), Turns: turns, Permissions: permissions}, nil
}

type runtimeMemory struct {
	Memory
	mu       sync.Mutex
	current  pa.MemoryRevision
	snapshot pa.ContextSnapshot
	inside   atomic.Bool
}

func (m *runtimeMemory) Current(context.Context, pa.BotKey) (pa.MemoryRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return clone(m.current), nil
}
func (m *runtimeMemory) Snapshot(context.Context, pa.BotKey) (pa.ContextSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return clone(m.snapshot), nil
}
func (m *runtimeMemory) WithCurrent(_ context.Context, _ pa.BotKey, revision int64, f func(pa.ContextSnapshot) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if revision != m.current.Revision {
		return runtimeFailure(pa.ContextStale, "revision")
	}
	m.inside.Store(true)
	defer m.inside.Store(false)
	return f(clone(m.snapshot))
}

type runtimeResolver struct{ scope pa.ProjectScope }

func (r runtimeResolver) Resolve(context.Context, pa.ProjectSelection) (pa.ProjectResolution, error) {
	return pa.ProjectResolution{Scope: r.scope, Directory: "/scripted-project", Available: true, Current: true}, nil
}

type runtimeBridge struct {
	store *runtimeStore
	WorkBridge
	calls   atomic.Int64
	failure error
}

func (b *runtimeBridge) Dispatch(_ context.Context, q pa.WorkRequest) (pa.WorkLink, error) {
	b.calls.Add(1)
	if b.failure != nil {
		return pa.WorkLink{}, b.failure
	}
	link := pa.WorkLink{ID: "broker-link", TaskID: "owned-task", Fence: q.Actor.Fence, TurnID: q.TurnID, State: pa.WorkAccepted}
	if b.store != nil {
		b.store.mu.Lock()
		b.store.work = append(b.store.work, link)
		b.store.mu.Unlock()
	}
	return link, nil
}

type runtimeProvider struct {
	mu       sync.Mutex
	opens    []OpenRequest
	sessions []*runtimeSession
	sent     chan PromptRequest
	memory   *runtimeMemory
	sendErr  error
	capsErr  error
}

func (p *runtimeProvider) Capabilities(_ context.Context, b pa.Bot) (pa.Capabilities, error) {
	return pa.Capabilities{Key: b.Key, Project: b.Project, Agent: b.Agent, CLIVersion: "scripted", AdapterVersion: "scripted", Evidence: "scripted-only", ReadOnlyEnforced: true, NativeMemory: pa.NativeMemoryDisabled, NativeMemoryVerified: true, Resume: true}, p.capsErr
}
func (p *runtimeProvider) Open(_ context.Context, q OpenRequest, sink func(context.Context, ProtocolEvent) error) (ProviderSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := &runtimeSession{p: p, sink: sink, q: q, reply: make(chan PromptResult, 1), cancel: make(chan struct{}), answered: make(chan string, 1)}
	p.opens = append(p.opens, q)
	p.sessions = append(p.sessions, s)
	return s, nil
}
func (p *runtimeProvider) session() *runtimeSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessions[len(p.sessions)-1]
}
func (p *runtimeProvider) count() int { p.mu.Lock(); defer p.mu.Unlock(); return len(p.opens) }

type runtimeSession struct {
	p          *runtimeProvider
	sink       func(context.Context, ProtocolEvent) error
	q          OpenRequest
	reply      chan PromptResult
	cancel     chan struct{}
	once       sync.Once
	answered   chan string
	sendInside bool
	waitInside bool
}

func (s *runtimeSession) SessionID() string { return "native-session" }
func (s *runtimeSession) Send(_ context.Context, q PromptRequest) (PendingPrompt, error) {
	s.sendInside = s.p.memory.inside.Load()
	s.p.sent <- q
	if s.p.sendErr != nil {
		return nil, s.p.sendErr
	}
	return s, nil
}
func (s *runtimeSession) Wait(ctx context.Context) (PromptResult, error) {
	s.waitInside = s.p.memory.inside.Load()
	select {
	case q := <-s.reply:
		return q, nil
	case <-ctx.Done():
		return PromptResult{}, ctx.Err()
	case <-s.cancel:
		return PromptResult{}, runtimeFailure(pa.ExecutionEffectUnknown, "cancelled")
	}
}
func (s *runtimeSession) Answer(_ context.Context, _ string, option string) error {
	s.answered <- option
	return nil
}
func (s *runtimeSession) Cancel(context.Context) error {
	s.once.Do(func() { close(s.cancel) })
	return nil
}
func (s *runtimeSession) Close(context.Context) error { return nil }

type runtimeFixture struct {
	r        *Engine
	store    *runtimeStore
	memory   *runtimeMemory
	provider *runtimeProvider
	source   pa.SourceStatus
	key      pa.BotKey
	scope    pa.ProjectScope
	seq      atomic.Int64
	revoked  atomic.Bool
}

func newRuntimeFixture(t *testing.T) *runtimeFixture {
	t.Helper()
	f := &runtimeFixture{key: pa.BotKey{MachineID: "machine", BotID: "bot"}, scope: pa.ProjectScope{MachineID: "machine", ProjectID: "project", CanonicalID: "repo", Kind: pa.RepositoryProject}}
	f.store = &runtimeStore{bot: pa.Bot{Key: f.key, Project: f.scope, ConversationID: "chat", Version: 1, Agent: pa.Codex, Instructions: pa.InstructionSnapshot{Revision: 1, Text: "fixed"}}, c: pa.Conversation{Key: f.key, ID: "chat"}, turns: map[string]pa.Turn{}, bindings: map[string]pa.ExecutionBinding{}, permissions: map[string]pa.PermissionOffer{}, receipts: map[string]pa.Receipt{}}
	m := pa.MemoryRevision{Key: f.key, Project: f.scope, Revision: 1}
	f.memory = &runtimeMemory{current: m, snapshot: pa.ContextSnapshot{Memory: m, ProvenanceComplete: true}}
	f.provider = &runtimeProvider{sent: make(chan PromptRequest, pa.MaxPendingTurns), memory: f.memory}
	f.source = pa.SourceStatus{MachineID: "machine", State: pa.SourceOnline, Readable: true, Writable: true, Fresh: true, Access: pa.Interactive}
	var err error
	f.r, err = NewRuntime(RuntimeOptions{Repository: f.store, Memory: f.memory, WorkBridge: &runtimeBridge{store: f.store}, ProjectResolver: runtimeResolver{f.scope}, Provider: f.provider, Clock: time.Now, ID: func() string { return fmt.Sprint(f.seq.Add(1)) }, Authorize: func(context.Context, pa.BotKey) (pa.FeatureState, pa.SourceStatus, error) {
		s := f.source
		if f.revoked.Load() {
			s.State = pa.SourceRevoked
		}
		return pa.FeatureState{Enabled: true}, s, nil
	}, Limits: RuntimeLimits{MaxBots: 2, MaxProcesses: 1, PromptTimeout: time.Second * 5, CleanupTimeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = f.r.Disable(context.Background(), pa.ProjectSelection{MachineID: "machine", ProjectID: "project"})
	})
	return f
}
func (f *runtimeFixture) submit(t *testing.T, text, id string) (pa.Receipt, error) {
	t.Helper()
	f.store.mu.Lock()
	fence := currentFence(f.store.bot, f.store.c)
	f.store.mu.Unlock()
	return f.r.Submit(context.Background(), Submission{Command: pa.Command{Key: f.key, Operation: pa.SendOperation, IdempotencyKey: id, ContentHash: strings.Repeat("a", 64)}, Message: pa.SendMessage{Fence: fence, Text: text}, Feature: pa.FeatureState{Enabled: true}, Source: f.source})
}
func receivePrompt(t *testing.T, p *runtimeProvider) PromptRequest {
	t.Helper()
	select {
	case q := <-p.sent:
		return q
	case <-time.After(time.Second * 3):
		t.Fatal("prompt not sent")
		return PromptRequest{}
	}
}
func eventually(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second * 3)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
func awaitTurn(t *testing.T, f *runtimeFixture, id string, state pa.TurnState) {
	t.Helper()
	eventually(t, func() bool { x, _ := f.store.Turn(context.Background(), f.key, id); return x.State == state })
}

func TestRuntimeLazySerialDurableAdmission(t *testing.T) {
	f := newRuntimeFixture(t)
	if f.provider.count() != 0 {
		t.Fatal("constructor launched process")
	}
	a, err := f.submit(t, "one", "one")
	if err != nil {
		t.Fatal(err)
	}
	q := receivePrompt(t, f.provider)
	if q.Turn.ID != a.ResourceID {
		t.Fatal("receipt is not accepted turn")
	}
	b, err := f.submit(t, "two", "two")
	if err != nil {
		t.Fatal(err)
	}
	if f.provider.count() != 1 {
		t.Fatal("queued turn launched concurrent process")
	}
	f.provider.session().reply <- PromptResult{FinalText: "first"}
	awaitTurn(t, f, a.ResourceID, pa.TurnSucceeded)
	q2 := receivePrompt(t, f.provider)
	if q2.Text != "two" || q2.Turn.Fence.Generation <= q.Turn.Fence.Generation {
		t.Fatalf("bad queued continuation: %+v", q2)
	}
	f.provider.session().reply <- PromptResult{FinalText: "second"}
	awaitTurn(t, f, b.ResourceID, pa.TurnSucceeded)
	f.provider.mu.Lock()
	for _, s := range f.provider.sessions {
		if !s.sendInside || s.waitInside {
			t.Fatal("memory lock covers wrong phase")
		}
	}
	f.provider.mu.Unlock()
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	if len(f.store.messages) != 4 {
		t.Fatalf("messages=%d", len(f.store.messages))
	}
}

func TestRuntimePermissionAndExplicitReplyWhilePromptPending(t *testing.T) {
	f := newRuntimeFixture(t)
	rec, err := f.submit(t, "one", "one")
	if err != nil {
		t.Fatal(err)
	}
	q := receivePrompt(t, f.provider)
	s := f.provider.session()
	offer := pa.PermissionOffer{ID: "offer", Fence: q.Turn.Fence, TurnID: q.Turn.ID, NativeRequestID: "7", ExpiresAt: time.Now().Add(time.Minute), Options: []pa.PermissionOption{{ID: "once", Kind: pa.AllowOnce, Allow: true, ReadOnly: true}, {ID: "always", Kind: pa.AllowAlways, Allow: true, ReadOnly: true}}}
	if err = s.sink(context.Background(), ProtocolEvent{ID: "offer", Fence: q.Turn.Fence, TurnID: q.Turn.ID, Permission: &offer}); err != nil {
		t.Fatal(err)
	}
	answer := AnswerRequest{Command: pa.Command{Key: f.key, Operation: pa.AnswerOperation, IdempotencyKey: "answer", ContentHash: strings.Repeat("b", 64)}, Answer: pa.PermissionAnswer{Fence: q.Turn.Fence, TurnID: q.Turn.ID, OfferID: "offer", OptionID: "always"}}
	if _, err = f.r.Answer(context.Background(), answer); pa.Code(err) != pa.ScopeExpansionDenied {
		t.Fatalf("persistent allow=%v", err)
	}
	answer.Answer.OptionID = "once"
	if _, err = f.r.Answer(context.Background(), answer); err != nil {
		t.Fatal(err)
	}
	select {
	case option := <-s.answered:
		if option != "once" {
			t.Fatal(option)
		}
	default:
		t.Fatal("permission blocked by prompt")
	}
	e := ProtocolEvent{ID: "visible", Fence: q.Turn.Fence, TurnID: q.Turn.ID, Message: "explicit"}
	if err = s.sink(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if err = s.sink(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	s.reply <- PromptResult{FinalText: "duplicate fallback"}
	awaitTurn(t, f, rec.ResourceID, pa.TurnSucceeded)
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	if len(f.store.messages) != 2 || f.store.messages[1].Text != "explicit" {
		t.Fatalf("reply=%+v", f.store.messages)
	}
}

func TestRuntimeStopCancelsQueueAndRejectsLateEvents(t *testing.T) {
	f := newRuntimeFixture(t)
	a, err := f.submit(t, "one", "one")
	if err != nil {
		t.Fatal(err)
	}
	q := receivePrompt(t, f.provider)
	s := f.provider.session()
	b, err := f.submit(t, "two", "two")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.r.Stop(context.Background(), StopRequest{Command: pa.Command{Key: f.key, Operation: pa.StopOperation, IdempotencyKey: "stop", ContentHash: strings.Repeat("c", 64)}, Fence: q.Turn.Fence, TurnID: q.Turn.ID})
	if err != nil {
		t.Fatal(err)
	}
	awaitTurn(t, f, a.ResourceID, pa.TurnUnknown)
	awaitTurn(t, f, b.ResourceID, pa.TurnCancelled)
	if err = s.sink(context.Background(), ProtocolEvent{ID: "late", Fence: q.Turn.Fence, TurnID: q.Turn.ID, Message: "late"}); err == nil {
		t.Fatal("late event accepted")
	}
}

func TestRuntimeForgetStartsFilteredBindingAndNextInstructionSnapshot(t *testing.T) {
	f := newRuntimeFixture(t)
	rec, err := f.submit(t, "one", "one")
	if err != nil {
		t.Fatal(err)
	}
	first := receivePrompt(t, f.provider)
	s := f.provider.session()
	f.store.mu.Lock()
	f.store.bot.Instructions = pa.InstructionSnapshot{Revision: 2, Text: "new fixed"}
	f.store.bot.Version++
	f.store.mu.Unlock()
	f.memory.mu.Lock()
	f.memory.current.Revision = 2
	f.memory.current.ForgottenThrough = 2
	f.memory.current.ForgottenSourceIDs = []string{"old"}
	f.memory.snapshot = pa.ContextSnapshot{Memory: f.memory.current, ProvenanceComplete: true}
	f.memory.mu.Unlock()
	s.reply <- PromptResult{FinalText: "done"}
	awaitTurn(t, f, rec.ResourceID, pa.TurnSucceeded)
	rec, err = f.submit(t, "two", "two")
	if err != nil {
		t.Fatal(err)
	}
	second := receivePrompt(t, f.provider)
	if second.Turn.Instructions.Revision != 2 || second.Turn.MemoryRevision != 2 || second.Context.Text != "" {
		t.Fatalf("snapshot=%+v", second)
	}
	f.provider.mu.Lock()
	open := f.provider.opens[len(f.provider.opens)-1]
	f.provider.mu.Unlock()
	if open.Resume || open.Binding.Fence.BindingID == first.Turn.Fence.BindingID {
		t.Fatal("forgotten opaque session resumed")
	}
	f.provider.session().reply <- PromptResult{}
	awaitTurn(t, f, rec.ResourceID, pa.TurnSucceeded)
}

func TestRuntimeUnknownWritesAndRestartNeverReplay(t *testing.T) {
	f := newRuntimeFixture(t)
	f.provider.sendErr = runtimeFailure(pa.ExecutionEffectUnknown, "partial_stdin")
	rec, err := f.submit(t, "one", "one")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	awaitTurn(t, f, rec.ResourceID, pa.TurnUnknown)
	replayed, err := f.submit(t, "one", "one")
	if err != nil || replayed.ID != rec.ID {
		t.Fatalf("replay=%+v %v", replayed, err)
	}
	if err = f.r.Recover(context.Background(), f.key); err != nil {
		t.Fatal(err)
	}
	if f.provider.count() != 1 {
		t.Fatal("unknown prompt replayed")
	}
}

func TestRuntimeAdmissionRefusesCapabilityAndPersistenceFailure(t *testing.T) {
	f := newRuntimeFixture(t)
	f.provider.capsErr = runtimeFailure(pa.MemoryIsolationUnverified, "native_memory")
	if _, err := f.submit(t, "one", "one"); pa.Code(err) != pa.MemoryIsolationUnverified {
		t.Fatal(err)
	}
	f.provider.capsErr = nil
	f.store.mu.Lock()
	f.store.failCommit = true
	f.store.mu.Unlock()
	if _, err := f.submit(t, "one", "one"); err == nil {
		t.Fatal("durable failure acknowledged")
	}
	if f.provider.count() != 0 {
		t.Fatal("failed accept launched process")
	}
}

func TestRuntimeFreshRevocationPreventsIncomingEffects(t *testing.T) {
	f := newRuntimeFixture(t)
	_, err := f.submit(t, "one", "one")
	if err != nil {
		t.Fatal(err)
	}
	q := receivePrompt(t, f.provider)
	f.revoked.Store(true)
	if err = f.provider.session().sink(context.Background(), ProtocolEvent{ID: "revoked", Fence: q.Turn.Fence, TurnID: q.Turn.ID, Message: "forbidden"}); pa.Code(err) != pa.Revoked {
		t.Fatal(err)
	}
}

func TestRuntimeQueueFullAndIdleActorReclaimed(t *testing.T) {
	f := newRuntimeFixture(t)
	f.r.slots <- struct{}{} // Hold the process slot; admission still persists.
	for i := 0; i < pa.MaxPendingTurns; i++ {
		if _, err := f.submit(t, "queued", fmt.Sprint(i)); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, err := f.submit(t, "overflow", "overflow"); pa.Code(err) != pa.CapacityFull {
		t.Fatal(err)
	}
	f.store.mu.Lock()
	fence := currentFence(f.store.bot, f.store.c)
	f.store.mu.Unlock()
	if _, err := f.r.Stop(context.Background(), StopRequest{Command: pa.Command{Key: f.key, Operation: pa.StopOperation, IdempotencyKey: "stop-all", ContentHash: strings.Repeat("d", 64)}, Fence: fence}); err != nil {
		t.Fatal(err)
	}
	<-f.r.slots
	eventually(t, func() bool { f.r.mu.Lock(); defer f.r.mu.Unlock(); return len(f.r.actors) == 0 })
	if f.provider.count() != 0 {
		t.Fatal("stopped queue launched a process")
	}
}

func TestRuntimeRecoverRunningTurnBecomesUnknownWithoutProcess(t *testing.T) {
	f := newRuntimeFixture(t)
	fence := pa.Fence{Key: f.key, Project: f.scope, ConversationID: "chat", BindingID: "old", Generation: 1}
	f.store.mu.Lock()
	f.store.c.BindingID = "old"
	f.store.c.Generation = 1
	f.store.c.ActiveTurnID = "interrupted"
	f.store.turns["interrupted"] = pa.Turn{ID: "interrupted", Fence: fence, State: pa.TurnRunning}
	f.store.order = []string{"interrupted"}
	f.store.bindings["old"] = pa.ExecutionBinding{Fence: fence, Agent: pa.Codex, NativeSessionID: "opaque", MemoryRevision: 1, InstructionRevision: 1}
	f.store.mu.Unlock()
	if err := f.r.Recover(context.Background(), f.key); err != nil {
		t.Fatal(err)
	}
	awaitTurn(t, f, "interrupted", pa.TurnUnknown)
	if f.provider.count() != 0 {
		t.Fatal("interrupted native prompt replayed")
	}
}

func TestRuntimeActiveBotSlotsRefuseBeforePersistence(t *testing.T) {
	f := newRuntimeFixture(t)
	f.r.o.Limits.MaxBots = 1
	foreign, err := f.r.acquire(pa.BotKey{MachineID: "machine", BotID: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.submit(t, "full", "one"); pa.Code(err) != pa.CapacityFull {
		t.Fatal(err)
	}
	f.store.mu.Lock()
	n := len(f.store.turns)
	f.store.mu.Unlock()
	if n != 0 {
		t.Fatal("capacity refusal persisted orphan work")
	}
	f.r.release(foreign)
}

func TestRuntimeWorkBridgeUnknownNeverDispatchesTwice(t *testing.T) {
	f := newRuntimeFixture(t)
	bridge := f.r.o.WorkBridge.(*runtimeBridge)
	bridge.failure = runtimeFailure(pa.OutcomeUnknown, "broker_after_accept")
	_, err := f.submit(t, "change it", "one")
	if err != nil {
		t.Fatal(err)
	}
	prompt := receivePrompt(t, f.provider)
	q := pa.WorkRequest{Actor: pa.AssistantExecutionActor{CapabilityID: "host-issued", Fence: prompt.Turn.Fence, ExpiresAt: time.Now().Add(time.Minute), Operations: []pa.ActorOperation{pa.DispatchWork}}, Command: pa.Command{Key: f.key, Operation: pa.DispatchOperation, IdempotencyKey: "dispatch", ContentHash: strings.Repeat("e", 64)}, TurnID: prompt.Turn.ID, Brief: "Change in isolated checkout", Claims: []string{"owned.go"}}
	if _, err = f.r.DispatchWork(context.Background(), q); pa.Code(err) != pa.OutcomeUnknown {
		t.Fatal(err)
	}
	if _, err = f.r.DispatchWork(context.Background(), q); pa.Code(err) != pa.OutcomeUnknown {
		t.Fatal(err)
	}
	if bridge.calls.Load() != 1 {
		t.Fatal("unknown broker effect repeated")
	}
}

func TestRuntimeWorkBridgeKeepsPersistedLink(t *testing.T) {
	f := newRuntimeFixture(t)
	_, err := f.submit(t, "change it", "one")
	if err != nil {
		t.Fatal(err)
	}
	prompt := receivePrompt(t, f.provider)
	q := pa.WorkRequest{Actor: pa.AssistantExecutionActor{CapabilityID: "host-issued", Fence: prompt.Turn.Fence, ExpiresAt: time.Now().Add(time.Minute), Operations: []pa.ActorOperation{pa.DispatchWork}}, Command: pa.Command{Key: f.key, Operation: pa.DispatchOperation, IdempotencyKey: "dispatch", ContentHash: strings.Repeat("f", 64)}, TurnID: prompt.Turn.ID, Brief: "Change in isolated checkout", Claims: []string{"owned.go"}}
	link, err := f.r.DispatchWork(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	if len(f.store.work) != 1 || f.store.work[0].ID != link.ID || link.TaskID != "owned-task" {
		t.Fatal("missing broker link")
	}
}

type projectedRuntimeBridge struct {
	WorkBridge
	store      *runtimeStore
	projection func(pa.WorkLink) *pa.WorkLink
}

func (b projectedRuntimeBridge) Dispatch(_ context.Context, q pa.WorkRequest) (pa.WorkLink, error) {
	link := pa.WorkLink{ID: "persisted", TaskID: "task", Fence: q.Actor.Fence, TurnID: q.TurnID, State: pa.WorkAccepted}
	if projected := b.projection(link); projected != nil {
		b.store.mu.Lock()
		b.store.work = append(b.store.work, *projected)
		b.store.mu.Unlock()
	}
	return link, nil
}
func TestRuntimeWorkBridgeChecksPersistedOwnerAndReturnsAdvancedState(t *testing.T) {
	for _, mode := range []string{"missing", "foreign task", "advanced"} {
		t.Run(mode, func(t *testing.T) {
			f := newRuntimeFixture(t)
			f.r.o.WorkBridge = projectedRuntimeBridge{store: f.store, projection: func(link pa.WorkLink) *pa.WorkLink {
				switch mode {
				case "missing":
					return nil
				case "foreign task":
					link.TaskID = "foreign"
				case "advanced":
					link.State = pa.WorkCompleted
				}
				return &link
			}}
			if _, e := f.submit(t, "work", "one"); e != nil {
				t.Fatal(e)
			}
			prompt := receivePrompt(t, f.provider)
			q := pa.WorkRequest{Actor: pa.AssistantExecutionActor{CapabilityID: "host-issued", Fence: prompt.Turn.Fence, ExpiresAt: time.Now().Add(time.Minute), Operations: []pa.ActorOperation{pa.DispatchWork}}, Command: pa.Command{Key: f.key, Operation: pa.DispatchOperation, IdempotencyKey: "work", ContentHash: strings.Repeat("f", 64)}, TurnID: prompt.Turn.ID, Brief: "Change owned.go", Claims: []string{"owned.go"}}
			link, e := f.r.DispatchWork(context.Background(), q)
			switch mode {
			case "missing":
				if pa.Code(e) != pa.OutcomeUnknown {
					t.Fatal(e)
				}
			case "foreign task":
				if pa.Code(e) != pa.ScopeDenied {
					t.Fatal(e)
				}
			case "advanced":
				if e != nil || link.State != pa.WorkCompleted {
					t.Fatal(link, e)
				}
			}
			f.store.mu.Lock()
			defer f.store.mu.Unlock()
			for _, event := range f.store.events {
				if event.Kind == pa.WorkUpdated {
					t.Fatal("Engine projected a second bridge-owned work event")
				}
			}
		})
	}
}

func TestRuntimeAutoReadAnswersOnlyVerifiedOnceOption(t *testing.T) {
	f := newRuntimeFixture(t)
	f.store.mu.Lock()
	f.store.bot.PermissionPolicy = pa.AutoRead
	f.store.mu.Unlock()
	_, err := f.submit(t, "read", "one")
	if err != nil {
		t.Fatal(err)
	}
	q := receivePrompt(t, f.provider)
	s := f.provider.session()
	offer := pa.PermissionOffer{ID: "read", Fence: q.Turn.Fence, TurnID: q.Turn.ID, NativeRequestID: "read", ExpiresAt: time.Now().Add(time.Minute), Options: []pa.PermissionOption{{ID: "always", Kind: pa.AllowAlways, Allow: true, ReadOnly: true}, {ID: "write", Kind: pa.AllowOnce, Allow: true}, {ID: "read-once", Kind: pa.AllowOnce, Allow: true, ReadOnly: true}}}
	if err = s.sink(context.Background(), ProtocolEvent{ID: "read", Fence: q.Turn.Fence, TurnID: q.Turn.ID, Permission: &offer}); err != nil {
		t.Fatal(err)
	}
	select {
	case option := <-s.answered:
		if option != "read-once" {
			t.Fatal(option)
		}
	default:
		t.Fatal("read policy did not answer")
	}
}

func (f *runtimeFixture) submitQuote(t *testing.T, text, id, quote string) (pa.Receipt, error) {
	t.Helper()
	f.store.mu.Lock()
	fence := currentFence(f.store.bot, f.store.c)
	f.store.mu.Unlock()
	return f.r.Submit(context.Background(), Submission{Command: pa.Command{Key: f.key, Operation: pa.SendOperation, IdempotencyKey: id, ContentHash: strings.Repeat("a", 64)}, Message: pa.SendMessage{Fence: fence, Text: text, QuoteMessageID: quote}, Feature: pa.FeatureState{Enabled: true}, Source: f.source})
}

func TestRuntimeQuoteReachesProviderWithoutChangingStoredUserText(t *testing.T) {
	f := newRuntimeFixture(t)
	first, err := f.submit(t, "first", "one")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	f.provider.session().reply <- PromptResult{FinalText: "precise quoted body"}
	awaitTurn(t, f, first.ResourceID, pa.TurnSucceeded)
	second, err := f.submitQuote(t, "explain this", "two", "reply-"+first.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	prompt := receivePrompt(t, f.provider)
	if prompt.Quote == nil || prompt.Quote.Text != "precise quoted body" || prompt.Text != "explain this" {
		t.Fatalf("quote not provided separately: %+v", prompt)
	}
	f.store.mu.Lock()
	for _, m := range f.store.messages {
		if m.TurnID == second.ResourceID && m.Kind == pa.UserMessage && m.Text != "explain this" {
			t.Fatal("user text changed")
		}
	}
	f.store.mu.Unlock()
	f.provider.session().reply <- PromptResult{FinalText: "done"}
	awaitTurn(t, f, second.ResourceID, pa.TurnSucceeded)
}

func TestRuntimeForgetRejectsQuoteBeforeAnotherProcess(t *testing.T) {
	f := newRuntimeFixture(t)
	first, err := f.submit(t, "first", "one")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	f.provider.session().reply <- PromptResult{FinalText: "old body"}
	awaitTurn(t, f, first.ResourceID, pa.TurnSucceeded)
	f.memory.mu.Lock()
	f.memory.current.Revision = 2
	f.memory.current.ForgottenThrough = 2
	f.memory.snapshot = pa.ContextSnapshot{Memory: f.memory.current, ProvenanceComplete: true}
	f.memory.mu.Unlock()
	if _, err = f.submitQuote(t, "quote", "two", "reply-"+first.ResourceID); pa.Code(err) != pa.ContextStale {
		t.Fatal(err)
	}
	if f.provider.count() != 1 {
		t.Fatal("forgotten quote launched provider")
	}
}

func TestRuntimeQuoteWithUnknownWriteNeverReplays(t *testing.T) {
	f := newRuntimeFixture(t)
	first, err := f.submit(t, "first", "one")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	f.provider.session().reply <- PromptResult{FinalText: "reference"}
	awaitTurn(t, f, first.ResourceID, pa.TurnSucceeded)
	f.provider.sendErr = runtimeFailure(pa.ExecutionEffectUnknown, "partial_stdin")
	second, err := f.submitQuote(t, "quote", "two", "reply-"+first.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	awaitTurn(t, f, second.ResourceID, pa.TurnUnknown)
	if _, err = f.submitQuote(t, "quote", "two", "reply-"+first.ResourceID); err != nil {
		t.Fatal(err)
	}
	if f.provider.count() != 2 {
		t.Fatal("quote unknown effect replayed")
	}
}

func TestRuntimeStopTerminatesPendingPermissionInSameMutation(t *testing.T) {
	f := newRuntimeFixture(t)
	_, err := f.submit(t, "read", "one")
	if err != nil {
		t.Fatal(err)
	}
	prompt := receivePrompt(t, f.provider)
	s := f.provider.session()
	offer := pa.PermissionOffer{ID: "pending-stop", Fence: prompt.Turn.Fence, TurnID: prompt.Turn.ID, NativeRequestID: "native-stop", ExpiresAt: time.Now().Add(time.Minute), Options: []pa.PermissionOption{{ID: "reject", Kind: pa.RejectOnce}}}
	if err = s.sink(context.Background(), ProtocolEvent{ID: offer.ID, Fence: offer.Fence, TurnID: offer.TurnID, Permission: &offer}); err != nil {
		t.Fatal(err)
	}
	stop := StopRequest{Command: pa.Command{Key: f.key, Operation: pa.StopOperation, IdempotencyKey: "stop-permission", ContentHash: strings.Repeat("b", 64)}, Fence: prompt.Turn.Fence, TurnID: prompt.Turn.ID}
	first, err := f.r.Stop(context.Background(), stop)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.r.Stop(context.Background(), stop)
	if err != nil || first.ID != second.ID {
		t.Fatalf("repeated stop=%v %v", second, err)
	}
	f.store.mu.Lock()
	stored := f.store.permissions[offer.ID]
	active := f.store.c.ActiveTurnID
	atomic := false
	for _, m := range f.store.mutations {
		if m.Turn != nil && m.Turn.ID == prompt.Turn.ID && m.Turn.State == pa.TurnUnknown && m.Conversation != nil && m.Conversation.ActiveTurnID == "" {
			for _, p := range m.Permissions {
				if p.ID == offer.ID && p.State == pa.PermissionUnknown {
					for _, e := range m.Events {
						if e.Kind == pa.PermissionAnswered && e.Permission != nil && e.Permission.ID == offer.ID {
							atomic = true
						}
					}
				}
			}
		}
	}
	pending := 0
	for _, p := range f.store.permissions {
		if p.State == pa.PermissionPending {
			pending++
		}
	}
	f.store.mu.Unlock()
	if stored.State != pa.PermissionUnknown || active != "" || !atomic || pending != 0 {
		t.Fatalf("terminal aggregate not atomic: state=%s active=%s atomic=%v pending=%d", stored.State, active, atomic, pending)
	}
	answer := AnswerRequest{Command: pa.Command{Key: f.key, Operation: pa.AnswerOperation, IdempotencyKey: "late-answer", ContentHash: strings.Repeat("c", 64)}, Answer: pa.PermissionAnswer{Fence: offer.Fence, OfferID: offer.ID, TurnID: offer.TurnID, OptionID: "reject"}}
	if _, err = f.r.Answer(context.Background(), answer); err == nil {
		t.Fatal("late answer reactivated permission")
	}
	if err = s.sink(context.Background(), ProtocolEvent{ID: "late-native", Fence: offer.Fence, TurnID: offer.TurnID, Permission: &offer}); err == nil {
		t.Fatal("late event reactivated permission")
	}
	select {
	case <-s.answered:
		t.Fatal("stop produced an extra permission answer")
	default:
	}
}

func TestRuntimeKnownCompletionCancelsOnlyStillPendingPermissions(t *testing.T) {
	f := newRuntimeFixture(t)
	rec, err := f.submit(t, "read", "one")
	if err != nil {
		t.Fatal(err)
	}
	prompt := receivePrompt(t, f.provider)
	s := f.provider.session()
	makeOffer := func(id string, expiry time.Time) pa.PermissionOffer {
		return pa.PermissionOffer{ID: id, Fence: prompt.Turn.Fence, TurnID: prompt.Turn.ID, NativeRequestID: id, ExpiresAt: expiry, Options: []pa.PermissionOption{{ID: "reject", Kind: pa.RejectOnce}}}
	}
	for _, offer := range []pa.PermissionOffer{makeOffer("pending", time.Now().Add(time.Minute)), makeOffer("expired", time.Now().Add(-time.Minute)), makeOffer("resolved", time.Now().Add(time.Minute))} {
		p := offer
		if err = s.sink(context.Background(), ProtocolEvent{ID: p.ID, Fence: p.Fence, TurnID: p.TurnID, Permission: &p}); err != nil {
			t.Fatal(err)
		}
	}
	answer := AnswerRequest{Command: pa.Command{Key: f.key, Operation: pa.AnswerOperation, IdempotencyKey: "resolve-one", ContentHash: strings.Repeat("d", 64)}, Answer: pa.PermissionAnswer{Fence: prompt.Turn.Fence, OfferID: "resolved", TurnID: prompt.Turn.ID, OptionID: "reject"}}
	if _, err = f.r.Answer(context.Background(), answer); err != nil {
		t.Fatal(err)
	}
	<-s.answered
	f.store.mu.Lock()
	prior := makeOffer("already-unknown", time.Now().Add(time.Minute))
	prior.State = pa.PermissionUnknown
	prior.SelectedOptionID = "reject"
	f.store.permissions[prior.ID] = prior
	f.store.mu.Unlock()
	s.reply <- PromptResult{FinalText: "done"}
	awaitTurn(t, f, rec.ResourceID, pa.TurnSucceeded)
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	for id, want := range map[string]pa.PermissionState{"pending": pa.PermissionCancelled, "expired": pa.PermissionExpired, "resolved": pa.PermissionResolved, "already-unknown": pa.PermissionUnknown} {
		if f.store.permissions[id].State != want {
			t.Fatalf("%s state=%s want=%s", id, f.store.permissions[id].State, want)
		}
	}
	if f.store.permissions["already-unknown"].SelectedOptionID != "reject" {
		t.Fatal("prior unknown answer overwritten")
	}
	select {
	case <-s.answered:
		t.Fatal("completion sent extra native permission answer")
	default:
	}
}
