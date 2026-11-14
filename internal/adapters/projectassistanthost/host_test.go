package projectassistanthost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gitadapter "github.com/sainteye/clawdline/internal/adapters/git"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	hoststore "github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/app/ports"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/session"
)

type fixtureResolver struct{ f *hostFixture }

func (r fixtureResolver) Resolve(_ context.Context, q pa.ProjectSelection) (pa.ProjectResolution, error) {
	s := r.f.scope
	if r.f.projectChanged.Load() {
		s.CanonicalID = "replaced"
	}
	return pa.ProjectResolution{Scope: s, Directory: r.f.repo, Current: true, Available: true}, nil
}

type fixtureLauncher struct {
	ports.Launcher
	calls atomic.Int64
}

func (*fixtureLauncher) ITermRunning(context.Context) (bool, error) { return false, nil }
func (*fixtureLauncher) TmuxReach(context.Context) int              { return 2 }
func (l *fixtureLauncher) NewTmuxSession(context.Context, string, string, string) (string, error) {
	l.calls.Add(1)
	return "%80", nil
}
func (*fixtureLauncher) CloseTmuxSession(context.Context, string, string) (bool, error) {
	return true, nil
}

type fixtureProvider struct {
	openErr                  error
	caps, opens, closes      atomic.Int64
	unknown                  atomic.Bool
	openStarted, openRelease chan struct{}
	sent                     chan app.PromptRequest
	sessions                 chan *fixtureSession
	steerOutcome             app.SteerOutcome
	steered                  chan string
}

func (p *fixtureProvider) Capabilities(_ context.Context, b pa.Bot) (pa.Capabilities, error) {
	p.caps.Add(1)
	return pa.Capabilities{Key: b.Key, Project: b.Project, Agent: b.Agent, CLIVersion: "fake", AdapterVersion: "fake", Evidence: "fixture-only", ReadOnlyEnforced: true, NativeMemory: pa.NativeMemoryDisabled, NativeMemoryVerified: true, Resume: true}, nil
}
func (p *fixtureProvider) Open(_ context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	p.opens.Add(1)
	if p.openErr != nil {
		return nil, p.openErr
	}
	if p.openStarted != nil {
		close(p.openStarted)
		<-p.openRelease
	}
	s := &fixtureSession{p: p, q: q, sink: sink, reply: make(chan app.PromptResult, 1)}
	p.sessions <- s
	return s, nil
}

type fixtureSession struct {
	p     *fixtureProvider
	q     app.OpenRequest
	sink  func(context.Context, app.ProtocolEvent) error
	reply chan app.PromptResult
}

func (s *fixtureSession) SessionID() string { return "fake-native" }
func (s *fixtureSession) Send(_ context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	s.p.sent <- q
	return s, nil
}
func (s *fixtureSession) Wait(ctx context.Context) (app.PromptResult, error) {
	select {
	case q := <-s.reply:
		return q, nil
	case <-ctx.Done():
		return app.PromptResult{}, ctx.Err()
	}
}
func (s *fixtureSession) Answer(context.Context, string, string) error { return nil }
func (s *fixtureSession) Steer(_ context.Context, text string) (app.SteerOutcome, error) {
	if s.p.steered == nil {
		return "", failure(pa.Unsupported, "steering")
	}
	s.p.steered <- text
	return s.p.steerOutcome, nil
}
func (s *fixtureSession) Cancel(context.Context) error { return nil }
func (s *fixtureSession) Close(context.Context) error {
	s.p.closes.Add(1)
	if s.p.unknown.Load() {
		return failure(pa.ExecutionEffectUnknown, "fixture_cleanup")
	}
	return nil
}
func (s *fixtureSession) Usage() app.SessionUsage { return app.SessionUsage{} }

type readerContextKey struct{}
type hostFixture struct {
	h                       *Host
	st                      *hoststore.Store
	o                       Options
	scope                   pa.ProjectScope
	repo                    string
	p                       *fixtureProvider
	launcher                *fixtureLauncher
	seq, factories          atomic.Int64
	revoked, projectChanged atomic.Bool
}

func fixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v %s", args, e, b)
	}
	return strings.TrimSpace(string(b))
}
func newHostFixture(t *testing.T) *hostFixture {
	t.Helper()
	f := &hostFixture{scope: pa.ProjectScope{MachineID: "source", ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}, p: &fixtureProvider{sent: make(chan app.PromptRequest, pa.MaxPendingTurns), sessions: make(chan *fixtureSession, pa.MaxPendingTurns)}}
	var e error
	f.st, e = hoststore.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		f.p.unknown.Store(false)
		if f.h != nil {
			_ = f.h.Close(context.Background())
		}
		_ = f.st.Close()
	})
	f.repo, e = filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	fixtureGit(t, f.repo, "init", "-q", "-b", "main")
	if e = os.WriteFile(filepath.Join(f.repo, "a.go"), []byte("package a\n"), 0600); e != nil {
		t.Fatal(e)
	}
	fixtureGit(t, f.repo, "add", "a.go")
	fixtureGit(t, f.repo, "commit", "-q", "-m", "Initial fixture")
	dir := t.TempDir()
	f.launcher = &fixtureLauncher{}
	broker := f.broker(t, dir)
	f.o = Options{Database: f.st.ProjectAssistantDatabase(), MachineID: "source", Projects: fixtureResolver{f}, Broker: broker, Clock: time.Now, ID: func() string { return fmt.Sprintf("host-%d", f.seq.Add(1)) }, Limits: app.RuntimeLimits{MaxBots: 4, MaxProcesses: 1, PromptTimeout: 5 * time.Second, CleanupTimeout: 100 * time.Millisecond},
		Source: func(context.Context, pa.BotKey, bool) (pa.SourceStatus, error) {
			s := pa.SourceStatus{MachineID: "source", State: pa.SourceOnline, Readable: true, Writable: true, Fresh: true, Access: pa.Interactive}
			if f.revoked.Load() {
				s.State = pa.SourceRevoked
			}
			return s, nil
		},
		DispatchPolicy: func(context.Context, pa.BotKey) (DispatchPolicy, error) {
			return DispatchPolicy{Allowed: true, AllowedClaims: []string{"a.go"}, Assistant: "codex", PermissionMode: "edits", TimeoutMinutes: 30}, nil
		},
		Provider: func() (app.Provider, error) { f.factories.Add(1); return f.p, nil },
	}
	f.o.Authority = func(ctx context.Context, k pa.BotKey, write bool) (app.Authority, error) {
		s, e := f.o.Source(ctx, k, write)
		reader := "server-reader"
		if v, ok := ctx.Value(readerContextKey{}).(string); ok {
			reader = v
		}
		return app.Authority{PrincipalID: reader, ReaderID: reader, AuthorizationRevision: "current", Feature: pa.FeatureState{Enabled: true}, Source: s}, e
	}
	return f
}

// broker builds the fixture's broker; a new daemon process builds its own.
func (f *hostFixture) broker(t *testing.T, dir string) *orchestrator.Broker {
	t.Helper()
	broker := &orchestrator.Broker{Store: f.st, Tasks: taskdir.New(dir), Git: gitadapter.New(), Dir: dir, Launcher: f.launcher, ScreenReadEvery: time.Millisecond, Terminal: func() projects.TerminalChoice { return projects.TerminalTmux }, Live: func(context.Context) []session.Session {
		return []session.Session{{ID: "%80", Backend: session.BackendTmux, Assistant: session.AssistantCodex}}
	}, Type: func(context.Context, string, string) error { return nil }}
	screen, e := os.ReadFile("../../app/orchestrator/testdata/codex-composer.txt")
	if e != nil {
		t.Fatal(e)
	}
	broker.Screen = func(context.Context, string) (string, bool) { return string(screen), true }
	return broker
}
func (f *hostFixture) start(t *testing.T) {
	t.Helper()
	var e error
	f.h, e = New(context.Background(), f.o)
	if e != nil {
		t.Fatal(e)
	}
}
func (f *hostFixture) enable(t *testing.T) {
	t.Helper()
	if e := f.h.Enable(context.Background()); e != nil {
		t.Fatal(e)
	}
}
func (f *hostFixture) create(t *testing.T, id string) pa.Bot {
	t.Helper()
	s, e := f.h.Service()
	if e != nil {
		t.Fatal(e)
	}
	b, _, e := s.Create(context.Background(), pa.Command{Key: pa.BotKey{MachineID: "source", BotID: id}, Operation: pa.CreateOperation, IdempotencyKey: "create-" + id}, pa.CreateBot{Target: pa.ProjectSelection{MachineID: "source", ProjectID: "project"}, Name: "Bot " + id, Agent: pa.Codex, Appearance: pa.Appearance{Shape: pa.Blob, Color: "#e39567"}, PermissionPolicy: pa.Ask, Instructions: "Read only"})
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func (f *hostFixture) send(t *testing.T, b pa.Bot, id string) (pa.Receipt, error) {
	t.Helper()
	s, e := f.h.Service()
	if e != nil {
		return pa.Receipt{}, e
	}
	_, c, _, e := s.Get(context.Background(), b.Key)
	if e != nil {
		return pa.Receipt{}, e
	}
	return s.Send(context.Background(), pa.Command{Key: b.Key, Operation: pa.SendOperation, IdempotencyKey: id}, pa.SendMessage{Fence: pa.Fence{Key: b.Key, Project: b.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}, Text: "Hello " + id})
}
func fixtureReceive[T any](t *testing.T, c <-chan T) T {
	t.Helper()
	select {
	case x := <-c:
		return x
	case <-time.After(3 * time.Second):
		t.Fatal("fixture barrier timed out")
		var z T
		return z
	}
}
func fixtureEventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("fixture condition timed out")
}

func TestHostDefaultOffAndMetadataNeverInitializeProvider(t *testing.T) {
	f := newHostFixture(t)
	f.start(t)
	state, e := f.h.Status(context.Background())
	if e != nil || state.Access() != pa.Hidden {
		t.Fatal(state, e)
	}
	var n int
	if e = f.o.Database.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name LIKE 'project_assistant_%'`).Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	if f.factories.Load() != 0 || f.h.engine != nil || f.o.Broker.AssistantAuthority != nil || f.o.Broker.AssistantWorkChanged != nil {
		t.Fatal("default off initialized feature")
	}
	f.enable(t)
	state, e = f.h.Status(context.Background())
	if e != nil || state.HasData {
		t.Fatal("empty tables are data", state, e)
	}
	b := f.create(t, "bot")
	s, _ := f.h.Service()
	if _, _, _, e = s.Get(context.Background(), b.Key); e != nil {
		t.Fatal(e)
	}
	page, e := s.List(context.Background(), pa.ListBots{MachineID: "source", PageSize: 10})
	if e != nil || len(page.Items) != 1 || page.Items[0].ReadCursor.ReaderID != "server-reader" {
		t.Fatal(page, e)
	}
	if f.factories.Load() != 0 || f.p.caps.Load() != 0 || f.p.opens.Load() != 0 || f.h.engine != nil {
		t.Fatal("metadata initialized execution")
	}
	if e = f.h.Disable(context.Background()); e != nil {
		t.Fatal(e)
	}
	state, e = f.h.Status(context.Background())
	if e != nil || state.Access() != pa.HistoryOnly {
		t.Fatal(state, e)
	}
	if _, e = s.Export(context.Background(), b.Key); e != nil {
		t.Fatal(e)
	}
	if _, e = f.send(t, b, "disabled"); pa.Code(e) != pa.Disabled {
		t.Fatal(e)
	}
	if e = f.h.Close(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e = f.o.Database.Ping(); e != nil {
		t.Fatal("feature closed shared DB", e)
	}
}

func TestHostLazySendDisablePreservesUnknownAndExistingHostWork(t *testing.T) {
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	b := f.create(t, "bot")
	now := time.Now()
	seen := []hoststore.ExecutionSeen{{ID: "existing-session", Source: "fixture", Fingerprint: "same-execution"}}
	beforeSession, err := f.st.ObserveExecutions(context.Background(), "source", seen, func(string, string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if e := f.st.CreateScheduleFile(context.Background(), "existing", []byte("schedule"), now, now); e != nil {
		t.Fatal(e)
	}
	first, e := f.send(t, b, "one")
	if e != nil {
		t.Fatal(e)
	}
	fixtureReceive(t, f.p.sent)
	second, e := f.send(t, b, "two")
	if e != nil {
		t.Fatal(e)
	}
	if f.factories.Load() != 1 || f.p.opens.Load() != 1 {
		t.Fatal("send not lazy")
	}
	if e = f.h.Disable(context.Background()); e != nil {
		t.Fatal(e)
	}
	x, e := f.h.store.Export(context.Background(), b.Key)
	if e != nil {
		t.Fatal(e)
	}
	states := map[string]pa.TurnState{}
	for _, turn := range x.Turns {
		states[turn.ID] = turn.State
	}
	if states[first.ResourceID] != pa.TurnUnknown || states[second.ResourceID] != pa.TurnCancelled {
		t.Fatal(states)
	}
	if e = f.st.CreateScheduleFile(context.Background(), "continued", []byte("schedule"), now, now); e != nil {
		t.Fatal(e)
	}
	afterSession, err := f.st.ObserveExecutions(context.Background(), "source", seen, func(string, string) bool { return false })
	if err != nil || afterSession["existing-session"] != beforeSession["existing-session"] {
		t.Fatal("existing Session generation changed", afterSession, err)
	}
	if err = f.st.AdmitExecution(context.Background(), "source", "existing-session", beforeSession["existing-session"]); err != nil {
		t.Fatal("existing Session stopped admitting", err)
	}
	schedules, e := f.st.ScheduleFiles(context.Background())
	if e != nil || len(schedules) != 2 {
		t.Fatal(schedules, e)
	}
	f.enable(t)
	if e = f.h.Recover(context.Background(), b.Key); e != nil {
		t.Fatal(e)
	}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	if f.factories.Load() != 1 || f.p.opens.Load() != 1 {
		t.Fatal("recovery initialized provider or replayed the unknown turn")
	}
	// As in CodyNC, the person may continue after an unknown turn. The new
	// message is a new turn; the unknown one stays unknown and is not resent.
	if _, e = f.send(t, b, "after-unknown"); e != nil {
		t.Fatalf("a new message after an unknown turn was refused: %v", e)
	}
	fixtureReceive(t, f.p.sent)
	if f.factories.Load() != 2 || f.p.opens.Load() != 2 {
		t.Fatalf("expected exactly one new native open on a fresh owner: factories=%d opens=%d", f.factories.Load(), f.p.opens.Load())
	}
	x, e = f.h.store.Export(context.Background(), b.Key)
	if e != nil {
		t.Fatal(e)
	}
	for _, turn := range x.Turns {
		if turn.ID == first.ResourceID && (turn.State != pa.TurnFailed || turn.Failure == nil || turn.Failure.Code != pa.ExecutionEffectUnknown || turn.Failure.Field != "acknowledged") {
			t.Fatalf("the unknown turn was not acknowledged as an unknown effect: %+v", turn)
		}
	}
	unknownReceipt := false
	for _, receipt := range x.Receipts {
		if receipt.ResourceID == first.ResourceID && receipt.State == pa.EffectUnknown && receipt.Command.IdempotencyKey == "runtime-acknowledge-"+first.ResourceID {
			unknownReceipt = true
		}
	}
	if !unknownReceipt {
		t.Fatal("acknowledgement did not keep the effect unknown in its receipt")
	}
}

func TestHostUnavailableProviderKeepsMetadataAndFutureSchemaUntouched(t *testing.T) {
	t.Run("unavailable", func(t *testing.T) {
		f := newHostFixture(t)
		f.o.Provider = nil
		f.start(t)
		f.enable(t)
		b := f.create(t, "bot")
		if _, e := f.send(t, b, "one"); pa.Code(e) != pa.AdapterNotFound {
			t.Fatal(e)
		}
		s, _ := f.h.Service()
		if _, e := s.Export(context.Background(), b.Key); e != nil {
			t.Fatal(e)
		}
		if f.p.opens.Load() != 0 {
			t.Fatal("unavailable provider opened")
		}
	})
	t.Run("future", func(t *testing.T) {
		f := newHostFixture(t)
		if _, e := f.o.Database.Exec(`PRAGMA user_version=83; CREATE TABLE project_assistant_schema(id INTEGER PRIMARY KEY,version INTEGER); INSERT INTO project_assistant_schema VALUES(1,99); CREATE TABLE future_data(value TEXT); INSERT INTO future_data VALUES('retained')`); e != nil {
			t.Fatal(e)
		}
		if _, e := New(context.Background(), f.o); pa.Code(e) != pa.Unsupported {
			t.Fatal(e)
		}
		var v int
		if e := f.o.Database.QueryRow(`PRAGMA user_version`).Scan(&v); e != nil || v != 83 {
			t.Fatal(v, e)
		}
		var data string
		if e := f.o.Database.QueryRow(`SELECT value FROM future_data`).Scan(&data); e != nil || data != "retained" {
			t.Fatal(data, e)
		}
	})
}

func TestHostRetainsLateOpenAndUnknownCleanupAcrossReenable(t *testing.T) {
	for _, late := range []bool{true, false} {
		t.Run(fmt.Sprint("late=", late), func(t *testing.T) {
			f := newHostFixture(t)
			if late {
				f.p.openStarted = make(chan struct{})
				f.p.openRelease = make(chan struct{})
			} else {
				f.p.unknown.Store(true)
			}
			f.start(t)
			f.enable(t)
			b := f.create(t, "bot")
			if _, e := f.send(t, b, "one"); e != nil {
				t.Fatal(e)
			}
			if late {
				fixtureReceive(t, f.p.openStarted)
			} else {
				fixtureReceive(t, f.p.sent)
			}
			if e := f.h.Disable(context.Background()); pa.Code(e) != pa.ExecutionEffectUnknown {
				t.Fatal(e)
			}
			u := f.h.Usage()
			if u.Known || u.Processes != 1 || u.ActiveActors != 1 {
				t.Fatal(u)
			}
			if e := f.h.Enable(context.Background()); pa.Code(e) != pa.ExecutionEffectUnknown {
				t.Fatal(e)
			}
			if late {
				close(f.p.openRelease)
			} else {
				f.p.unknown.Store(false)
			}
			if e := f.h.Disable(context.Background()); e != nil {
				t.Fatal(e)
			}
			f.enable(t)
			if u = f.h.Usage(); !u.Known || u.Processes != 0 {
				t.Fatal(u)
			}
			if late && len(f.p.sent) != 0 {
				t.Fatal("late Open sent a prompt after disable")
			}
		})
	}
}

func TestHostRefusesForeignBrokerOwner(t *testing.T) {
	f := newHostFixture(t)
	foreign := func(context.Context, orchestrator.AssistantWorkSnapshot) error { return nil }
	f.o.Broker.AssistantWorkChanged = foreign
	f.start(t)
	if e := f.h.Enable(context.Background()); pa.Code(e) != pa.ScopeDenied {
		t.Fatal(e)
	}
	if f.o.Broker.AssistantAuthority != nil || f.o.Broker.AssistantWorkChanged == nil {
		t.Fatal("foreign owner overwritten")
	}
}

func TestHostDisableCancelsProcessSlotWaitersAndPermissionOffers(t *testing.T) {
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	first, second := f.create(t, "first"), f.create(t, "second")
	active, err := f.send(t, first, "active")
	if err != nil {
		t.Fatal(err)
	}
	prompt := fixtureReceive(t, f.p.sent)
	native := fixtureReceive(t, f.p.sessions)
	offer := pa.PermissionOffer{ID: "offer", Fence: prompt.Turn.Fence, TurnID: prompt.Turn.ID, NativeRequestID: "native-request", ExpiresAt: time.Now().Add(time.Minute), Options: []pa.PermissionOption{{ID: "reject", Kind: pa.RejectOnce}}}
	if err = native.sink(context.Background(), app.ProtocolEvent{ID: "permission", Fence: prompt.Turn.Fence, TurnID: prompt.Turn.ID, Permission: &offer}); err != nil {
		t.Fatal(err)
	}
	queued, err := f.send(t, second, "slot-waiter")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.h.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	x, err := f.h.store.Export(context.Background(), first.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(x.Permissions) != 1 || x.Permissions[0].State != pa.PermissionUnknown {
		t.Fatal(x.Permissions)
	}
	turn, err := f.h.store.Turn(context.Background(), first.Key, active.ResourceID)
	if err != nil || turn.State != pa.TurnUnknown {
		t.Fatal(turn, err)
	}
	turn, err = f.h.store.Turn(context.Background(), second.Key, queued.ResourceID)
	if err != nil || turn.State != pa.TurnCancelled {
		t.Fatal(turn, err)
	}
	if f.p.opens.Load() != 1 {
		t.Fatal("slot waiter opened provider", f.p.opens.Load())
	}
}

func TestHostForgetUsesOneStoreAndFreshBindingWithoutForeignContext(t *testing.T) {
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	b, other := f.create(t, "one"), f.create(t, "other")
	s, _ := f.h.Service()
	memory, err := s.SaveMemory(context.Background(), app.MemoryWrite{Command: pa.Command{Key: b.Key, Operation: pa.MemorySaveOperation, IdempotencyKey: "memory"}, ExpectedRevision: 1, Entry: pa.MemoryEntry{ID: "note", Key: b.Key, Project: b.Project, Text: "Private note"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveMemory(context.Background(), app.MemoryWrite{Command: pa.Command{Key: other.Key, Operation: pa.MemorySaveOperation, IdempotencyKey: "foreign"}, ExpectedRevision: 1, Entry: pa.MemoryEntry{ID: "foreign-note", Key: other.Key, Project: other.Project, Text: "Other Bot note"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.send(t, b, "before"); err != nil {
		t.Fatal(err)
	}
	before := fixtureReceive(t, f.p.sent)
	native := fixtureReceive(t, f.p.sessions)
	if before.Context.Text != "Private note" {
		t.Fatal(before.Context)
	}
	if before.Handoff == nil || before.Handoff.Context.Text != "Private note" || before.Handoff.Bookmark.MessageSeq != 1 {
		t.Fatal("first session lost stored handoff", before.Handoff)
	}

	native.reply <- app.PromptResult{FinalText: "Done"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	// Reopen the actual SQLite store and composition owner before forgetting.
	// A cached opening from the original Host cannot satisfy this boundary.
	var dbSequence int
	var dbName, dbPath string
	if err = f.o.Database.QueryRow("PRAGMA database_list").Scan(&dbSequence, &dbName, &dbPath); err != nil || dbName != "main" || dbPath == "" {
		t.Fatal("fixture database path", err, dbName)
	}
	if err = f.h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = f.st.Close(); err != nil {
		t.Fatal(err)
	}
	f.st, err = hoststore.Open(filepath.Dir(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	oldBroker := f.o.Broker
	f.o.Database = f.st.ProjectAssistantDatabase()
	f.o.Broker = &orchestrator.Broker{Store: f.st, Tasks: oldBroker.Tasks, Git: oldBroker.Git, Dir: oldBroker.Dir, Launcher: oldBroker.Launcher, ScreenReadEvery: oldBroker.ScreenReadEvery, Terminal: oldBroker.Terminal, Live: oldBroker.Live, Type: oldBroker.Type, Screen: oldBroker.Screen}
	f.start(t)
	f.enable(t)
	s, err = f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Forget(context.Background(), app.ForgetRequest{Command: pa.Command{Key: b.Key, Operation: pa.ForgetOperation, IdempotencyKey: "forget"}, Project: b.Project, ExpectedRevision: memory.Revision, SourceIDs: []string{"manual:note"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.send(t, b, "after"); err != nil {
		t.Fatal(err)
	}
	after := fixtureReceive(t, f.p.sent)
	native = fixtureReceive(t, f.p.sessions)
	if after.Context.Text != "" || after.Turn.Fence.BindingID == before.Turn.Fence.BindingID || native.q.Resume {
		t.Fatal("forgotten or foreign context resumed", after, native.q)
	}
	if after.Handoff == nil || after.Handoff.Identity != after.Turn.Fence || after.Handoff.Bookmark.MessageSeq != 3 {
		t.Fatal("reopened source lost fresh handoff", after.Handoff)
	}
	opening, err := after.Handoff.Opening(after.Turn, after.Context)
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"Private note", "Other Bot note", "Hello before"} {
		if strings.Contains(opening, absent) {
			t.Fatal("new session copied forgotten, foreign or transcript text")
		}
	}

	otherMemory, err := f.h.store.Snapshot(context.Background(), other.Key)
	if err != nil || otherMemory.Text != "Other Bot note" {
		t.Fatal(otherMemory, err)
	}
}

func TestHostReaderMetadataUsesCurrentPrincipalInDisabledHistory(t *testing.T) {
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	b := f.create(t, "bot")
	if _, err := f.send(t, b, "one"); err != nil {
		t.Fatal(err)
	}
	fixtureReceive(t, f.p.sent)
	if err := f.h.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, _ := f.h.Service()
	ctx := context.WithValue(context.Background(), readerContextKey{}, "authenticated-reader")
	cursor, err := s.MarkRead(ctx, pa.Command{Key: b.Key, Operation: pa.ReadOperation, IdempotencyKey: "read"}, pa.ReadCursor{Key: b.Key, ConversationID: b.ConversationID, ReaderID: "forged-reader", MessageSeq: 1})
	if err != nil || cursor.ReaderID != "authenticated-reader" {
		t.Fatal(cursor, err)
	}
	page, err := s.List(ctx, pa.ListBots{MachineID: "source", PageSize: 10})
	if err != nil || page.Items[0].UnreadCount != 0 || page.Items[0].ReadCursor.ReaderID != "authenticated-reader" {
		t.Fatal(page, err)
	}
	other, err := s.List(context.Background(), pa.ListBots{MachineID: "source", PageSize: 10})
	if err != nil || other.Items[0].UnreadCount != 1 || other.Items[0].ReadCursor.ReaderID != "server-reader" {
		t.Fatal(other, err)
	}
}

func TestHostDisableCancelsDurableQueueWithoutLiveActor(t *testing.T) {
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	b := f.create(t, "bot")
	c := pa.Conversation{Key: b.Key, ID: b.ConversationID, MemoryRevision: 1, Version: 1, LastMessageSeq: 1}
	fence := pa.Fence{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID}
	turn := pa.Turn{ID: "persisted-queue", Fence: fence, MessageID: "persisted-input", State: pa.TurnQueued, IdempotencyKey: "persisted"}
	msg := pa.Message{ID: turn.MessageID, Fence: fence, TurnID: turn.ID, Seq: 1, Kind: pa.UserMessage, Text: "Accepted before restart", At: time.Now()}
	if _, e := f.h.store.Commit(context.Background(), app.Mutation{Command: projectionCommand(b.Key, "persist-queue"), ExpectedVersion: b.Version, Conversation: &c, Turn: &turn, Messages: []pa.Message{msg}, Receipt: pa.Receipt{ID: "persist-queue", State: pa.EffectAccepted}}); e != nil {
		t.Fatal(e)
	}
	if f.h.engine != nil {
		t.Fatal("fixture unexpectedly has an Engine")
	}
	if e := f.h.Disable(context.Background()); e != nil {
		t.Fatal(e)
	}
	stored, e := f.h.store.Turn(context.Background(), b.Key, turn.ID)
	if e != nil || stored.State != pa.TurnCancelled {
		t.Fatal("disable retained executable queue", stored, e)
	}
	if f.h.engine != nil || f.factories.Load() != 0 {
		t.Fatal("durable queue cleanup initialized execution")
	}
	f.enable(t)
	if e = f.h.Recover(context.Background(), b.Key); e != nil {
		t.Fatal(e)
	}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	if f.factories.Load() != 0 || f.p.opens.Load() != 0 {
		t.Fatal("disabled queue replayed after reenable")
	}
}

func persistFixtureQueue(t *testing.T, f *hostFixture, b pa.Bot) {
	t.Helper()
	c := pa.Conversation{Key: b.Key, ID: b.ConversationID, MemoryRevision: 1, Version: 1, LastMessageSeq: 1}
	fence := pa.Fence{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID}
	turn := pa.Turn{ID: "queued-" + b.Key.BotID, Fence: fence, MessageID: "input-" + b.Key.BotID, State: pa.TurnQueued, IdempotencyKey: "persisted"}
	msg := pa.Message{ID: turn.MessageID, Fence: fence, TurnID: turn.ID, Seq: 1, Kind: pa.UserMessage, Text: "Accepted before restart", At: time.Now()}
	if _, e := f.h.store.Commit(context.Background(), app.Mutation{Command: projectionCommand(b.Key, "persist-queue"), ExpectedVersion: b.Version, Conversation: &c, Turn: &turn, Messages: []pa.Message{msg}, Receipt: pa.Receipt{ID: "persist-queue", State: pa.EffectAccepted}}); e != nil {
		t.Fatal(e)
	}
}

func TestHostReopenedDisableQuiescesMoreThanOneHistoryPage(t *testing.T) {
	f := newHostFixture(t)
	f.o.Limits.CleanupTimeout = 3 * time.Second
	f.start(t)
	f.enable(t)
	for i := 0; i < pa.MaxPageRows+1; i++ {
		b := f.create(t, fmt.Sprintf("bot-%03d", i))
		persistFixtureQueue(t, f, b)
	}
	// Simulate a process restart before any queue recovery: a new Broker/Host,
	// the same real retained database, and no old live execution owner.
	f.o.Broker = &orchestrator.Broker{}
	f.start(t)
	if e := f.h.Disable(context.Background()); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < pa.MaxPageRows+1; i++ {
		key := pa.BotKey{MachineID: "source", BotID: fmt.Sprintf("bot-%03d", i)}
		x, e := f.h.store.Export(context.Background(), key)
		if e != nil || len(x.Turns) != 1 || x.Turns[0].State != pa.TurnCancelled {
			t.Fatal(key, x.Turns, e)
		}
	}
	if f.h.engine != nil || f.factories.Load() != 0 || !f.h.Usage().Known {
		t.Fatal("history scan constructed execution or lost completion")
	}
}

func TestHostDurableCleanupFailureAndTimeoutKeepReenableFenced(t *testing.T) {
	t.Run("partial SQL failure", func(t *testing.T) {
		f := newHostFixture(t)
		f.start(t)
		f.enable(t)
		a, b := f.create(t, "a"), f.create(t, "b")
		persistFixtureQueue(t, f, a)
		persistFixtureQueue(t, f, b)
		if _, e := f.o.Database.Exec(`CREATE TRIGGER deny_cleanup BEFORE UPDATE ON project_assistant_bots WHEN NEW.bot='b' BEGIN SELECT RAISE(ABORT,'fixture cleanup failure'); END`); e != nil {
			t.Fatal(e)
		}
		if e := f.h.Disable(context.Background()); pa.Code(e) != pa.ExecutionEffectUnknown {
			t.Fatal(e)
		}
		first, e := f.h.store.Export(context.Background(), a.Key)
		if e != nil || first.Turns[0].State != pa.TurnCancelled {
			t.Fatal(first.Turns, e)
		}
		second, e := f.h.store.Export(context.Background(), b.Key)
		if e != nil || second.Turns[0].State != pa.TurnQueued {
			t.Fatal(second.Turns, e)
		}
		if f.h.Usage().Known || f.h.engine != nil {
			t.Fatal("partial scan reported clean")
		}
		if e = f.h.Enable(context.Background()); pa.Code(e) != pa.ExecutionEffectUnknown {
			t.Fatal(e)
		}
		if _, e = f.o.Database.Exec(`DROP TRIGGER deny_cleanup`); e != nil {
			t.Fatal(e)
		}
		f.enable(t)
		repeated, e := f.h.store.Export(context.Background(), a.Key)
		if e != nil || len(first.Events) != len(repeated.Events) || len(first.Receipts) != len(repeated.Receipts) {
			t.Fatal("retry duplicated completed cleanup", e)
		}
		second, e = f.h.store.Export(context.Background(), b.Key)
		if e != nil || second.Turns[0].State != pa.TurnCancelled {
			t.Fatal(second.Turns, e)
		}
		if f.factories.Load() != 0 || f.h.engine != nil {
			t.Fatal("durable retry initialized execution")
		}
	})
	t.Run("shared writer deadline", func(t *testing.T) {
		f := newHostFixture(t)
		f.o.Limits.CleanupTimeout = 30 * time.Millisecond
		f.start(t)
		f.enable(t)
		b := f.create(t, "bot")
		persistFixtureQueue(t, f, b)
		tx, e := f.o.Database.BeginTx(context.Background(), nil)
		if e != nil {
			t.Fatal(e)
		}
		began := time.Now()
		e = f.h.Disable(context.Background())
		elapsed := time.Since(began)
		if pa.Code(e) != pa.ExecutionEffectUnknown || elapsed > time.Second || f.h.Usage().Known {
			_ = tx.Rollback()
			t.Fatal("cleanup deadline lost", e, elapsed)
		}
		if e = f.h.Enable(context.Background()); pa.Code(e) != pa.ExecutionEffectUnknown {
			_ = tx.Rollback()
			t.Fatal(e)
		}
		if e = tx.Rollback(); e != nil {
			t.Fatal(e)
		}
		f.enable(t)
		stored, e := f.h.store.Turn(context.Background(), b.Key, "queued-bot")
		if e != nil || stored.State != pa.TurnCancelled {
			t.Fatal(stored, e)
		}
		if f.factories.Load() != 0 || f.h.engine != nil {
			t.Fatal("timeout retry started execution")
		}
	})
}

// As in Codync, a turn the daemon's own restart cut off resumes on the next
// start through the real store: the interrupted turn is acknowledged and the
// resume prompt runs as a notice. A refused resume must not acknowledge it.
func TestHostRestartResumesTheTurnItCutOff(t *testing.T) {
	f := newHostFixture(t)
	f.o.Reader = f.st.ProjectAssistantReader()
	f.o.ReadViewByteLimit = cloudops.ReadPlaintextBytesLimit
	f.start(t)
	f.enable(t)
	b := f.create(t, "bot")
	first, e := f.send(t, b, "one")
	if e != nil {
		t.Fatal(e)
	}
	fixtureReceive(t, f.p.sent)
	if e = f.h.Close(context.Background()); e != nil {
		t.Fatal(e)
	}
	// A new process has a new broker, as the daemon builds one per start.
	f.o.Broker = f.broker(t, f.o.Broker.Dir)
	f.start(t)
	f.enable(t)
	resumed := fixtureReceive(t, f.p.sent)
	if !strings.Contains(resumed.Text, "Clawdline restarted") {
		t.Fatalf("resume prompt=%q", resumed.Text)
	}
	// While the resume runs, the chat still reads: its pending turn's input
	// is the notice, not a person's message.
	s, e := f.h.Service()
	if e != nil {
		t.Fatal(e)
	}
	_, c, _, e := s.Get(context.Background(), b.Key)
	if e != nil {
		t.Fatal(e)
	}
	view, e := s.View(context.Background(), pa.ReadViewRequest{Key: b.Key, ConversationID: c.ID, Mode: pa.ReadViewSnapshot, PageSize: 100})
	if e != nil {
		t.Fatalf("the chat did not read during a resume: %v", e)
	}
	if view.View.Snapshot == nil || len(view.View.Snapshot.PendingTurns) != 1 {
		t.Fatalf("resumed turn not pending in the view: %+v", view.View.Snapshot)
	}
	fixtureReceive(t, f.p.sessions) // the first process's session
	fixtureReceive(t, f.p.sessions).reply <- app.PromptResult{FinalText: "Carried on"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	// A finished resume stays in the history the chat reads.
	if view, e = s.View(context.Background(), pa.ReadViewRequest{Key: b.Key, ConversationID: c.ID, Mode: pa.ReadViewSnapshot, PageSize: 100}); e != nil {
		t.Fatalf("the chat did not read after a resume: %v", e)
	}
	x, e := f.h.store.Export(context.Background(), b.Key)
	if e != nil {
		t.Fatal(e)
	}
	for _, turn := range x.Turns {
		if turn.ID == first.ResourceID && turn.State != pa.TurnFailed {
			t.Fatalf("interrupted turn=%s", turn.State)
		}
	}
}

// As in Codync, a Bot keeps its own memory through its tools. Adding and
// changing entries keeps the native session, whose next prompt hears the
// whole memory; forgetting starts a new filtered session.
func TestHostBotMemoryToolsKeepTheSessionUntilForgetting(t *testing.T) {
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	b := f.create(t, "bot")
	ctx := context.Background()
	if _, e := f.send(t, b, "one"); e != nil {
		t.Fatal(e)
	}
	fixtureReceive(t, f.p.sent)
	first := fixtureReceive(t, f.p.sessions)
	tools := first.q.Tools
	if e := tools.EditMemory(ctx, "c1", app.MemoryEdit{Append: "Has two cats"}); e != nil {
		t.Fatal(e)
	}
	if e := tools.EditMemory(ctx, "c2", app.MemoryEdit{Old: "two cats", New: "three cats"}); e != nil {
		t.Fatal(e)
	}
	if e := tools.EditMemory(ctx, "c3", app.MemoryEdit{Old: "dogs", New: "x"}); pa.Code(e) != pa.NotFound {
		t.Fatalf("an edit of absent text: %v", e)
	}
	entries, e := tools.Memory(ctx)
	if e != nil || len(entries) != 1 || entries[0].Text != "Has three cats" {
		t.Fatalf("memory=%+v %v", entries, e)
	}
	first.reply <- app.PromptResult{FinalText: "Noted"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })

	if _, e = f.send(t, b, "two"); e != nil {
		t.Fatal(e)
	}
	prompt := fixtureReceive(t, f.p.sent)
	second := fixtureReceive(t, f.p.sessions)
	if !second.q.Resume || prompt.Handoff != nil || !prompt.MemoryChanged || !strings.Contains(prompt.Context.Text, "Has three cats") {
		t.Fatalf("after adding memory: resume=%v handoff=%v changed=%v memory=%q", second.q.Resume, prompt.Handoff != nil, prompt.MemoryChanged, prompt.Context.Text)
	}
	if e = second.q.Tools.EditMemory(ctx, "c4", app.MemoryEdit{Old: "three cats", Delete: true}); e != nil {
		t.Fatal(e)
	}
	second.reply <- app.PromptResult{FinalText: "Forgotten"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })

	if _, e = f.send(t, b, "three"); e != nil {
		t.Fatal(e)
	}
	prompt = fixtureReceive(t, f.p.sent)
	third := fixtureReceive(t, f.p.sessions)
	if third.q.Resume || prompt.Handoff == nil || strings.Contains(prompt.Context.Text, "cats") {
		t.Fatalf("after forgetting: resume=%v handoff=%v memory=%q", third.q.Resume, prompt.Handoff != nil, prompt.Context.Text)
	}
}

// As Codync's unqueue, stopping a queued turn takes back that one message: the
// running turn and the rest of the queue carry on, and it is never sent. A
// running turn records when it started, for the chat's elapsed time.
func TestHostStoppingAQueuedTurnRemovesOnlyThatMessage(t *testing.T) {
	f := newHostFixture(t)
	f.o.Reader = f.st.ProjectAssistantReader()
	f.o.ReadViewByteLimit = cloudops.ReadPlaintextBytesLimit
	f.start(t)
	f.enable(t)
	b := f.create(t, "bot")
	ctx := context.Background()
	first, e := f.send(t, b, "one")
	if e != nil {
		t.Fatal(e)
	}
	fixtureReceive(t, f.p.sent)
	running := fixtureReceive(t, f.p.sessions)
	second, e := f.send(t, b, "two")
	if e != nil {
		t.Fatal(e)
	}
	third, e := f.send(t, b, "three")
	if e != nil {
		t.Fatal(e)
	}
	s, e := f.h.Service()
	if e != nil {
		t.Fatal(e)
	}
	_, c, _, e := s.Get(ctx, b.Key)
	if e != nil {
		t.Fatal(e)
	}
	fence := pa.Fence{Key: b.Key, Project: b.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}
	if _, e = s.Stop(ctx, app.StopRequest{Command: pa.Command{Key: b.Key, Operation: pa.StopOperation, IdempotencyKey: "remove-two"}, Fence: fence, TurnID: second.ResourceID}); e != nil {
		t.Fatalf("removing a queued message: %v", e)
	}
	turns := func() map[string]pa.Turn {
		x, e := f.h.store.Export(ctx, b.Key)
		if e != nil {
			t.Fatal(e)
		}
		out := map[string]pa.Turn{}
		for _, turn := range x.Turns {
			out[turn.ID] = turn
		}
		return out
	}
	now := turns()
	if now[first.ResourceID].State != pa.TurnRunning || now[first.ResourceID].StartedAt == nil {
		t.Fatalf("running turn: %+v", now[first.ResourceID])
	}
	if now[second.ResourceID].State != pa.TurnCancelled || now[second.ResourceID].Failure != nil {
		t.Fatalf("removed turn: %+v", now[second.ResourceID])
	}
	if now[third.ResourceID].State != pa.TurnQueued {
		t.Fatalf("the rest of the queue: %+v", now[third.ResourceID])
	}
	view, e := s.View(ctx, pa.ReadViewRequest{Key: b.Key, ConversationID: c.ID, Mode: pa.ReadViewSnapshot, PageSize: 100})
	if e != nil || view.View.Snapshot == nil {
		t.Fatal(e)
	}
	started := ""
	for _, turn := range view.View.Snapshot.PendingTurns {
		if turn.ID == first.ResourceID {
			started = turn.StartedAt
		}
	}
	if _, e = time.Parse(time.RFC3339, started); e != nil {
		t.Fatalf("the chat cannot read when the running turn started: %q", started)
	}
	if _, e = s.Stop(ctx, app.StopRequest{Command: pa.Command{Key: b.Key, Operation: pa.StopOperation, IdempotencyKey: "remove-two-again"}, Fence: fence, TurnID: second.ResourceID}); pa.Code(e) != pa.StaleVersion {
		t.Fatalf("removing a removed message again: %v", e)
	}
	running.reply <- app.PromptResult{FinalText: "One done"}
	next := fixtureReceive(t, f.p.sent)
	if !strings.Contains(next.Text, "three") {
		t.Fatalf("after the running turn, the next prompt was %q", next.Text)
	}
}

// As Codync's steer, Send now hands a queued message to the running turn.
// Joined, it ends succeeded with no prompt of its own; when that turn had
// just ended, it stays queued and runs in its turn.
func TestHostSendNowJoinsTheRunningTurnOrWaits(t *testing.T) {
	for _, outcome := range []app.SteerOutcome{app.SteerJoined, app.SteerNext} {
		f := newHostFixture(t)
		f.p.steerOutcome, f.p.steered = outcome, make(chan string, 1)
		f.o.Reader = f.st.ProjectAssistantReader()
		f.o.ReadViewByteLimit = cloudops.ReadPlaintextBytesLimit
		f.start(t)
		f.enable(t)
		b := f.create(t, "bot")
		ctx := context.Background()
		first, e := f.send(t, b, "one")
		if e != nil {
			t.Fatal(e)
		}
		fixtureReceive(t, f.p.sent)
		running := fixtureReceive(t, f.p.sessions)
		second, e := f.send(t, b, "two")
		if e != nil {
			t.Fatal(e)
		}
		s, e := f.h.Service()
		if e != nil {
			t.Fatal(e)
		}
		_, c, _, e := s.Get(ctx, b.Key)
		if e != nil {
			t.Fatal(e)
		}
		fence := pa.Fence{Key: b.Key, Project: b.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}
		if _, e = s.SendNow(ctx, app.SendNowRequest{Command: pa.Command{Key: b.Key, Operation: pa.SendNowOperation, IdempotencyKey: "now-" + string(outcome)}, Fence: fence, TurnID: second.ResourceID}); e != nil {
			t.Fatalf("%s: send now: %v", outcome, e)
		}
		if text := fixtureReceive(t, f.p.steered); text != "Hello two" {
			t.Fatalf("steered %q", text)
		}
		got, e := f.h.store.Turn(ctx, b.Key, second.ResourceID)
		if e != nil {
			t.Fatal(e)
		}
		running.reply <- app.PromptResult{FinalText: "Both answered"}
		if outcome == app.SteerJoined {
			if got.State != pa.TurnSucceeded || got.JoinedTurnID != first.ResourceID {
				t.Fatalf("joined turn: %+v", got)
			}
			fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
			if _, e = s.View(ctx, pa.ReadViewRequest{Key: b.Key, ConversationID: c.ID, Mode: pa.ReadViewSnapshot, PageSize: 100}); e != nil {
				t.Fatalf("the chat did not read after a joined message: %v", e)
			}
			select {
			case q := <-f.p.sent:
				t.Fatalf("a joined message was sent again: %q", q.Text)
			default:
			}
			continue
		}
		if got.State != pa.TurnQueued {
			t.Fatalf("a message the agent left for later: %+v", got)
		}
		if next := fixtureReceive(t, f.p.sent); !strings.Contains(next.Text, "two") {
			t.Fatalf("next prompt %q", next.Text)
		}
	}
}

// As in Codync, a Bot's permission setting changes after creation, through
// the real store.
func TestHostBotPermissionSettingChangesAfterCreation(t *testing.T) {
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	b := f.create(t, "bot")
	s, e := f.h.Service()
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	next, _, e := s.Edit(ctx, pa.Command{Key: b.Key, Operation: pa.EditOperation, IdempotencyKey: "approve"}, pa.EditBot{ExpectedVersion: b.Version, Name: b.Name, Appearance: b.Appearance, Instructions: b.Instructions.Text, PermissionPolicy: pa.Approve})
	if e != nil {
		t.Fatalf("changing the permission setting: %v", e)
	}
	stored, e := f.h.store.Bot(ctx, b.Key)
	if e != nil || stored.PermissionPolicy != pa.Approve || next.PermissionPolicy != pa.Approve {
		t.Fatalf("stored=%+v %v", stored, e)
	}
	if _, _, e = s.Edit(ctx, pa.Command{Key: b.Key, Operation: pa.EditOperation, IdempotencyKey: "read-only"}, pa.EditBot{ExpectedVersion: stored.Version, Name: b.Name, Appearance: b.Appearance, Instructions: b.Instructions.Text, PermissionPolicy: pa.Ask, Mode: "read-only"}); e != nil {
		t.Fatalf("a Codex mode for a Codex Bot: %v", e)
	}
	if stored, _ = f.h.store.Bot(ctx, b.Key); stored.Mode != "read-only" || stored.PermissionPolicy != pa.Ask {
		t.Fatalf("mode=%+v", stored)
	}
}

// A Claude Code Bot's standing rules are read from and revoked in its own
// Project's local settings; a Codex Bot has none Clawdline can read.
func TestHostListsAndRevokesAClaudeBotsProjectRules(t *testing.T) {
	f := newHostFixture(t)
	f.o.Rules = ruleStoreForTest{}
	f.start(t)
	f.enable(t)
	s, e := f.h.Service()
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	claude, _, e := s.Create(ctx, pa.Command{Key: pa.BotKey{MachineID: "source", BotID: "claude"}, Operation: pa.CreateOperation, IdempotencyKey: "create-claude"}, pa.CreateBot{Target: pa.ProjectSelection{MachineID: "source", ProjectID: "project"}, Name: "Claude", Agent: pa.ClaudeCode, Appearance: pa.Appearance{Shape: pa.Blob, Color: "#e39567"}, PermissionPolicy: pa.Ask})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.MkdirAll(filepath.Join(f.repo, ".claude"), 0o755); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(f.repo, ".claude", "settings.local.json"), []byte(`{"permissions":{"allow":["Bash(ls:*)"]}}`), 0o644); e != nil {
		t.Fatal(e)
	}
	rules, e := s.Rules(ctx, claude.Key)
	if e != nil || len(rules) != 1 || rules[0].Rule != "Bash(ls:*)" {
		t.Fatalf("rules=%+v %v", rules, e)
	}
	if e = s.RemoveRule(ctx, pa.Command{Key: claude.Key, Operation: pa.RuleRemoveOperation, IdempotencyKey: "revoke"}, rules[0]); e != nil {
		t.Fatal(e)
	}
	if rules, e = s.Rules(ctx, claude.Key); e != nil || len(rules) != 0 {
		t.Fatalf("after revoking: %+v %v", rules, e)
	}
	codex := f.create(t, "codex")
	if _, e = s.Rules(ctx, codex.Key); pa.Code(e) != pa.Unsupported {
		t.Fatalf("a Codex Bot's rules: %v", e)
	}
}

// ruleStoreForTest is the production rule store, reached without importing
// the ACP adapter into this package's tests.
type ruleStoreForTest struct{}

func (ruleStoreForTest) List(dir string) ([]pa.PermissionRule, error) {
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if os.IsNotExist(err) {
		return []pa.PermissionRule{}, nil
	}
	var top struct {
		Permissions struct{ Allow, Deny []string } `json:"permissions"`
	}
	if err != nil || json.Unmarshal(data, &top) != nil {
		return nil, failure(pa.Invalid, "rules_file")
	}
	out := []pa.PermissionRule{}
	for _, r := range top.Permissions.Allow {
		out = append(out, pa.PermissionRule{Behavior: "allow", Rule: r})
	}
	return out, nil
}
func (ruleStoreForTest) Remove(dir string, rule pa.PermissionRule) error {
	return os.WriteFile(filepath.Join(dir, ".claude", "settings.local.json"), []byte(`{"permissions":{"allow":[]}}`), 0o644)
}
