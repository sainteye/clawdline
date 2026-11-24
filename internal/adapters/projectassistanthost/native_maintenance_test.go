package projectassistanthost

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	journalstore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type maintenancePeer struct {
	*fixtureProvider
	open            func(context.Context, app.OpenRequest) (app.ProviderSession, error)
	opens, commands atomic.Int64
}

func (p *maintenancePeer) Open(ctx context.Context, request app.OpenRequest, _ func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	p.opens.Add(1)
	return p.open(ctx, request)
}

type maintenanceSession struct {
	app.ProviderSession
	peer   *maintenancePeer
	fence  pa.Fence
	onRead func()
	wait   func(context.Context) (app.NativeCompactionResult, error)
}

func (s *maintenanceSession) NativeContext() app.NativeContextState {
	if s.onRead != nil {
		s.onRead()
	}
	return app.NativeContextState{Fence: s.fence, NativeSessionID: s.SessionID(), CommandsKnown: true, CompactAdvertised: true}
}
func (s *maintenanceSession) BeginCompaction(_ context.Context, fence pa.Fence) (app.PendingNativeCompaction, error) {
	if s.fence != fence {
		return nil, failure(pa.StaleGeneration, "maintenance_peer")
	}
	s.peer.commands.Add(1)
	return s, nil
}
func (s *maintenanceSession) Wait(ctx context.Context) (app.NativeCompactionResult, error) {
	if s.wait != nil {
		return s.wait(ctx)
	}
	return s.terminal(app.NativeCompactionCompleted), nil
}
func (s *maintenanceSession) terminal(state app.NativeCompactionState) app.NativeCompactionResult {
	return app.NativeCompactionResult{Fence: s.fence, NativeSessionID: s.SessionID(), ID: "native-terminal", State: state}
}

func qualificationJournal(t *testing.T, path string) (*sql.DB, *journalstore.NativeMaintenanceJournal) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	j, err := journalstore.NewNativeMaintenanceJournal(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return db, j
}

type maintenanceCase struct {
	f       *hostFixture
	bot     pa.Bot
	intent  app.NativeMaintenanceIntent
	path    string
	journal *journalstore.NativeMaintenanceJournal
	peer    *maintenancePeer
	s       *maintenanceSession
	seed    *fixtureSession
}

func newMaintenanceCase(t *testing.T) *maintenanceCase {
	t.Helper()
	f, _, bot, q, seed := liveContextFixture(t)
	binding, err := f.h.store.Binding(context.Background(), bot.Key, q.Turn.Fence.BindingID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "maintenance.sqlite3")
	_, journal := qualificationJournal(t, path)
	p := &maintenancePeer{fixtureProvider: f.p}
	s := &maintenanceSession{ProviderSession: &fixtureSession{p: f.p}, peer: p, fence: binding.Fence}
	p.open = func(ctx context.Context, q app.OpenRequest) (app.ProviderSession, error) {
		if !q.Resume || q.Binding != binding || q.Bot.Key != bot.Key || q.Directory != f.repo ||
			q.Tools != nil || !reflect.DeepEqual(q.Extensions, app.NativeExtensions{}) {
			t.Errorf("purpose or binding crossed the native boundary: %+v", q)
		}
		saved, found, e := journal.Lookup(ctx, app.NativeMaintenanceIntent{
			ID: "maintenance-id", Binding: binding, PrincipalID: "server-reader", ReaderID: "server-reader",
			AuthorizationRevision: "current", CLIVersion: "fake", AdapterVersion: "fake", Evidence: "fixture-only",
		})
		if e != nil || !found || saved.State != "" {
			t.Errorf("Open entered without durable intent: %+v %v %v", saved, found, e)
		}
		return s, nil
	}
	intent := app.NativeMaintenanceIntent{
		ID: "maintenance-id", Binding: binding, PrincipalID: "server-reader", ReaderID: "server-reader",
		AuthorizationRevision: "current", CLIVersion: "fake", AdapterVersion: "fake", Evidence: "fixture-only",
	}
	return &maintenanceCase{f: f, bot: bot, intent: intent, path: path, journal: journal, peer: p, s: s, seed: seed}
}

func (c *maintenanceCase) idle(t *testing.T) {
	t.Helper()
	c.seed.reply <- app.PromptResult{FinalText: "seeded"}
	fixtureEventually(t, func() bool { return c.f.h.Usage().ActiveActors == 0 })
}

func (c *maintenanceCase) run(ctx context.Context) (app.NativeMaintenanceRecord, error) {
	return c.f.h.qualifyNativeMaintenance(ctx, c.intent, c.journal, c.peer)
}

func (c *maintenanceCase) export(t *testing.T) app.Export {
	t.Helper()
	x, err := c.f.h.store.Export(context.Background(), c.bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func (c *maintenanceCase) forget(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	service, _ := c.f.h.Service()
	revision, err := c.f.h.store.Current(ctx, c.bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Forget(ctx, app.ForgetRequest{
		Command: pa.Command{Key: c.bot.Key, Operation: pa.ForgetOperation, IdempotencyKey: "forget-maintenance"},
		Project: c.bot.Project, ExpectedRevision: revision.Revision, SourceIDs: []string{"synthetic-source"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestHostNativeMaintenanceIdleTypedTerminalAndReplay(t *testing.T) {
	for _, state := range []app.NativeCompactionState{app.NativeCompactionCompleted, app.NativeCompactionFailed, app.NativeCompactionCancelled} {
		t.Run(string(state), func(t *testing.T) {
			ctx := context.Background()
			c := newMaintenanceCase(t)
			if _, err := c.run(ctx); pa.Code(err) != pa.CapacityFull || c.peer.opens.Load() != 0 {
				t.Fatal("busy actor admitted maintenance", err)
			}
			if _, found, err := c.journal.Lookup(ctx, c.intent); err != nil || found {
				t.Fatal("busy actor left a durable effect", found, err)
			}
			c.idle(t)
			before := c.export(t)
			c.s.wait = func(context.Context) (app.NativeCompactionResult, error) { return c.s.terminal(state), nil }
			record, err := c.run(ctx)
			if err != nil || record.State != state || record.NativeID != "native-terminal" {
				t.Fatal("qualified typed terminal was lost", record, err)
			}
			_, c.journal = qualificationJournal(t, c.path)
			if replay, e := c.run(ctx); e != nil || replay != record || c.peer.opens.Load() != 1 || c.peer.commands.Load() != 1 {
				t.Fatal("retry after reopening the journal repeated native work", replay, e)
			}
			if after := c.export(t); !reflect.DeepEqual(before, after) {
				t.Fatal("maintenance wrote chat/turn, memory, binding or completion facts")
			}
		})
	}
}

func TestHostNativeMaintenanceInvalidatesBeforeSend(t *testing.T) {
	for _, change := range []string{"forget", "source-revoked", "principal-changed", "project-changed"} {
		t.Run(change, func(t *testing.T) {
			c := newMaintenanceCase(t)
			c.idle(t)
			ctx := context.Background()
			if change == "principal-changed" {
				c.intent.ReaderID = "another-principal"
				if _, err := c.run(ctx); pa.Code(err) != pa.ScopeDenied || c.peer.opens.Load() != 0 {
					t.Fatal("foreign principal opened a native session", err)
				}
				return
			}
			c.s.onRead = func() {
				switch change {
				case "forget":
					c.forget(t)
				case "source-revoked":
					c.f.revoked.Store(true)
				case "project-changed":
					c.f.projectChanged.Store(true)
				}
			}
			record, err := c.run(ctx)
			if pa.Code(err) != pa.ExecutionEffectUnknown || record.State != app.NativeCompactionUnknown || c.peer.commands.Load() != 0 {
				t.Fatal("stale source crossed native send", record, err)
			}
		})
	}
}

func TestHostNativeMaintenanceUnknownCannotBeReplayed(t *testing.T) {
	for _, name := range []string{"lost-result", "cancelled-caller", "late-terminal-after-recovery", "forget-after-send", "revoke-after-send", "foreign-native-id"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := newMaintenanceCase(t)
			c.idle(t)
			_, restart := qualificationJournal(t, c.path)
			c.s.wait = func(context.Context) (app.NativeCompactionResult, error) {
				got := c.s.terminal(app.NativeCompactionCompleted)
				switch name {
				case "lost-result":
					return got, errors.New("response disconnected despite terminal notification")
				case "cancelled-caller":
					cancel()
				case "late-terminal-after-recovery":
					if n, err := restart.RecoverInterrupted(context.Background()); err != nil || n != 1 {
						t.Fatalf("restart did not freeze the interrupted intent: %d %v", n, err)
					}
				case "forget-after-send":
					c.forget(t)
				case "revoke-after-send":
					c.f.revoked.Store(true)
				case "foreign-native-id":
					got.NativeSessionID = "wrong-session"
				}
				return got, nil
			}
			record, err := c.run(ctx)
			if pa.Code(err) != pa.ExecutionEffectUnknown || record.State != app.NativeCompactionUnknown || record.NativeID != "" || c.peer.commands.Load() != 1 {
				t.Fatal("unconfirmed result was accepted", record, err)
			}
			c.f.revoked.Store(false)
			c.journal = restart
			if replay, e := c.run(context.Background()); pa.Code(e) != pa.ExecutionEffectUnknown || replay != record || c.peer.opens.Load() != 1 || c.peer.commands.Load() != 1 {
				t.Fatal("same key repeated an unknown native write", replay, e)
			}
		})
	}
}

func TestHostNativeMaintenanceRejectsChatWhileMaintenanceOwnsActor(t *testing.T) {
	c := newMaintenanceCase(t)
	c.idle(t)
	entered, reply := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(reply) })
	c.s.wait = func(ctx context.Context) (app.NativeCompactionResult, error) {
		close(entered)
		select {
		case <-ctx.Done():
			return app.NativeCompactionResult{}, ctx.Err()
		case <-reply:
			return c.s.terminal(app.NativeCompactionCompleted), nil
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.run(ctx); done <- err }()
	fixtureReceive(t, entered)
	if _, err := c.f.send(t, c.bot, "rival-chat"); pa.Code(err) != pa.CapacityFull {
		t.Fatal("chat bypassed maintenance ownership", err)
	}
	if _, err := c.run(ctx); pa.Code(err) != pa.CapacityFull {
		t.Fatal("same key did not report an in-flight intent", err)
	}
	cancel()
	if err := fixtureReceive(t, done); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal("caller cancellation lost the durable unknown effect", err)
	}
	if c.peer.opens.Load() != 1 || c.peer.commands.Load() != 1 {
		t.Fatal("second maintenance attempt crossed the transport")
	}
}

type lostTerminalAcknowledgement struct {
	app.NativeMaintenanceJournal
}

func (j lostTerminalAcknowledgement) Resolve(ctx context.Context, record app.NativeMaintenanceRecord) (app.NativeMaintenanceRecord, error) {
	saved, err := j.NativeMaintenanceJournal.Resolve(ctx, record)
	if err == nil && record.State == app.NativeCompactionCompleted {
		return app.NativeMaintenanceRecord{}, errors.New("lost acknowledgement after durable terminal commit")
	}
	return saved, err
}

func TestHostNativeMaintenanceLostAckReadsCommittedTerminalWithoutResend(t *testing.T) {
	ctx := context.Background()
	c := newMaintenanceCase(t)
	c.idle(t)
	record, err := c.f.h.qualifyNativeMaintenance(ctx, c.intent, lostTerminalAcknowledgement{c.journal}, c.peer)
	if pa.Code(err) != pa.ExecutionEffectUnknown || record.State != app.NativeCompactionCompleted {
		t.Fatal("lost acknowledgement overwrote committed terminal", record, err)
	}
	if err := c.f.h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	c.f.o.Broker = c.f.broker(t, c.f.o.Broker.Dir)
	c.f.start(t)
	c.f.enable(t)
	_, c.journal = qualificationJournal(t, c.path)
	replay, err := c.run(ctx)
	if err != nil || replay != record || c.peer.opens.Load() != 1 || c.peer.commands.Load() != 1 {
		t.Fatal("replacement Host failed to read committed terminal without native work", replay, err)
	}
}

func TestHostNativeMaintenanceUsesScriptedACP(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native process supervisor is unavailable")
	}
	if _, err := exec.Command("/bin/ps", "-axo", "pid=,ppid=,lstart=").Output(); err != nil {
		t.Skipf("process inventory unavailable: %v", err)
	}
	c := newMaintenanceCase(t)
	c.idle(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	frames := filepath.Join(t.TempDir(), "frames.jsonl")
	provider, err := acp.NewProvider(acp.ProviderOptions{
		Configurations: map[pa.Agent]acp.Configuration{c.bot.Agent: {
			Executable: exe, Arguments: []string{"-test.run=^TestMaintenanceACPPeer$"},
			Environment: []string{"CLAWDLINE_SYNTHETIC_MAINTENANCE_PEER=" + frames},
		}},
		Limits: acp.Limits{
			RPCPending: capacity.MaximumBotRPCPending,
			FrameBytes: capacity.MaximumBotFrameBytes, CaptureBytes: capacity.MaximumBotProcessCensusBytes,
			StderrBytes: capacity.MaximumBotStderrBytes, OwnedDescendants: capacity.MaximumBotOwnedDescendants,
			WriteTimeout: time.Second, CleanupTimeout: time.Second, RPCTimeout: time.Second,
			DescendantPoll: time.Duration(capacity.MaximumBotSurveyMilliseconds) * time.Millisecond,
		},
		Verify: func(ctx context.Context, bot pa.Bot, _ acp.Configuration) (pa.Capabilities, error) {
			return c.f.p.Capabilities(ctx, bot)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	before := c.export(t)
	record, err := c.f.h.qualifyNativeMaintenance(ctx, c.intent, c.journal, provider)
	if err != nil || record.State != app.NativeCompactionCompleted || record.NativeID != "scripted-terminal" {
		t.Fatal("Host/ACP/SQLite failed to resolve a matching terminal", record, err)
	}
	if _, err := c.f.h.qualifyNativeMaintenance(ctx, c.intent, c.journal, provider); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(frames)
	if err != nil {
		t.Fatal(err)
	}
	var operations []string
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		var req struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			t.Fatal(err)
		}
		operations = append(operations, req.Method)
		if req.Method == "session/prompt" {
			var got map[string]any
			if err := json.Unmarshal(req.Params, &got); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{"sessionId": c.intent.Binding.NativeSessionID, "prompt": []any{map[string]any{"type": "text", "text": "/compact"}}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("non-maintenance payload crossed ACP: %v", got)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(operations, []string{"initialize", "session/load", "session/prompt"}) {
		t.Fatal("unexpected ACP operations", operations)
	}
	if after := c.export(t); !reflect.DeepEqual(before, after) {
		t.Fatal("ACP maintenance leaked into Bot archive facts")
	}
}

// The helper is the test binary itself and only speaks to its owned stdin/stdout.
func TestMaintenanceACPPeer(t *testing.T) {
	path := os.Getenv("CLAWDLINE_SYNTHETIC_MAINTENANCE_PEER")
	if path == "" {
		t.Skip("scripted ACP child only")
	}
	output := json.NewEncoder(os.Stdout)
	send := func(frame map[string]any) {
		frame["jsonrpc"] = "2.0"
		if err := output.Encode(frame); err != nil {
			os.Exit(6)
		}
	}
	notice := func(update any) {
		send(map[string]any{"method": "session/update", "params": map[string]any{"sessionId": "fake-native", "update": update}})
	}
	log, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(7)
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		raw := scanner.Bytes()
		if _, err := log.Write(append(append([]byte(nil), raw...), '\n')); err != nil {
			os.Exit(8)
		}
		var req struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
		}
		if json.Unmarshal(raw, &req) != nil {
			os.Exit(9)
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion":   1,
				"agentInfo":         map[string]string{"name": "@agentclientprotocol/codex-acp", "version": "fake"},
				"agentCapabilities": map[string]any{"loadSession": true, "sessionCapabilities": map[string]any{"systemPrompt": map[string]any{}}},
			}
		case "session/load":
			notice(map[string]any{"sessionUpdate": "available_commands_update", "availableCommands": []any{map[string]string{"name": "compact"}}})
			result = map[string]string{"sessionId": "fake-native"}
		case "session/prompt":
			for _, state := range []string{"in_progress", "completed"} {
				notice(map[string]string{"sessionUpdate": "compaction_update", "compactionId": "scripted-terminal", "status": state})
			}
			result = map[string]string{"stopReason": "end_turn"}
		default:
			os.Exit(10)
		}
		send(map[string]any{"id": req.ID, "result": result})
	}
	if scanner.Err() != nil || log.Close() != nil {
		os.Exit(11)
	}
	os.Exit(0)
}

func establishHostHistoryFixture(t *testing.T, state string) *journalstore.NativeMaintenanceJournal {
	t.Helper()
	dir := filepath.Join(state, "project-assistant-native-maintenance")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	_, journal := qualificationJournal(t, filepath.Join(dir, "journal.sqlite3"))
	if err := os.WriteFile(filepath.Join(state, "project-assistant-native-maintenance.initialized"), []byte("native-maintenance-v1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return journal
}

func TestHostNativeMaintenanceHistoryBlocksNativeLoadAcrossRestart(t *testing.T) {
	for _, recover := range []bool{false, true} {
		name := "pending"
		if recover {
			name = "unknown"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f := newHostFixture(t)
			state := t.TempDir()
			f.o.NativeHistory = journalstore.NewNativeMaintenanceHistoryReader(state)
			f.start(t)
			f.enable(t)
			bot := f.create(t, "protected-history")
			if _, err := f.send(t, bot, "seed"); err != nil {
				t.Fatal(err)
			}
			seed := fixtureReceive(t, f.p.sent)
			fixtureReceive(t, f.p.sessions).reply <- app.PromptResult{FinalText: "seeded"}
			fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
			binding, err := f.h.store.Binding(ctx, bot.Key, seed.Turn.Fence.BindingID)
			if err != nil {
				t.Fatal(err)
			}
			journal := establishHostHistoryFixture(t, state)
			intent := app.NativeMaintenanceIntent{
				ID: "interrupted", Binding: binding, PrincipalID: "server-reader", ReaderID: "server-reader",
				AuthorizationRevision: "current", CLIVersion: "fake", AdapterVersion: "fake", Evidence: "fixture-only",
			}
			if _, reserved, err := journal.Reserve(ctx, intent); !reserved || err != nil {
				t.Fatal("could not stage the independent maintenance journal", reserved, err)
			}
			if err := f.h.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if recover {
				n, err := journal.RecoverInterrupted(ctx)
				if n != 1 || err != nil {
					t.Fatal("could not simulate sealing the interrupted writer", n, err)
				}
			}

			f.o.Broker = f.broker(t, f.o.Broker.Dir)
			f.o.NativeHistory = journalstore.NewNativeMaintenanceHistoryReader(state)
			f.start(t)
			f.enable(t)
			receipt, err := f.send(t, bot, "blocked-resume")
			if err != nil {
				t.Fatal(err)
			}
			fixtureEventually(t, func() bool {
				turn, err := f.h.store.Turn(ctx, bot.Key, receipt.ResourceID)
				return err == nil && turn.State == pa.TurnFailed
			})
			failed, err := f.h.store.Turn(ctx, bot.Key, receipt.ResourceID)
			if err != nil || failed.Failure == nil || failed.Failure.Code != pa.ExecutionEffectUnknown || f.p.opens.Load() != 1 {
				t.Fatal("uncertain history reached the native provider after restart", failed.Failure, f.p.opens.Load(), err)
			}

			// Forget creates a different physical history epoch. On an ordinary
			// authorized input it must get a fresh environment, not load the
			// blocked session or require resolving the old writer.
			service, err := f.h.Service()
			if err != nil {
				t.Fatal(err)
			}
			current, err := f.h.store.Current(ctx, bot.Key)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Forget(ctx, app.ForgetRequest{
				Command: pa.Command{Key: bot.Key, Operation: pa.ForgetOperation, IdempotencyKey: "forget-held"},
				Project: bot.Project, ExpectedRevision: current.Revision, SourceIDs: []string{"synthetic-source"},
			}); err != nil {
				t.Fatal(err)
			}
			f.revoked.Store(true)
			if _, err := f.send(t, bot, "revoked"); pa.Code(err) != pa.Revoked || f.p.opens.Load() != 1 {
				t.Fatal("revocation accidentally opened native history", err)
			}
			f.revoked.Store(false)
			if _, err := f.send(t, bot, "clean-privacy-epoch"); err != nil {
				t.Fatal(err)
			}
			fresh := fixtureReceive(t, f.p.sessions)
			if fresh.q.Resume || fresh.q.Binding.NativeSessionID != "" || fresh.q.Binding.ForgottenThrough <= binding.ForgottenThrough {
				t.Fatal("Forget did not yield a separate filtered session", fresh.q)
			}
			fixtureReceive(t, f.p.sent)
			fresh.reply <- app.PromptResult{FinalText: "new history only"}
			fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
			if old, found, err := journal.Lookup(ctx, intent); err != nil || !found || (recover && old.State != app.NativeCompactionUnknown) || (!recover && old.State != "") {
				t.Fatal("normal chat or Host startup rewrote the isolated journal", old.State, found, err)
			}
		})
	}
}

func TestHostNativeMaintenanceHistoryRefusesStartupWithMissingEstablishedJournal(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(state, "project-assistant-native-maintenance.initialized"), []byte("native-maintenance-v1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f.o.NativeHistory = journalstore.NewNativeMaintenanceHistoryReader(state)
	f.start(t)
	if err := f.h.Enable(ctx); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal("unreadable maintenance state enabled native recovery", err)
	}
	if f.h.engine != nil || f.h.routineDone != nil || f.factories.Load() != 0 || f.p.opens.Load() != 0 {
		t.Fatal("failed readiness started native recovery or scheduled work")
	}
}

type historyReaderObservation struct {
	app.NativeMaintenanceHistory
	checked chan error
}

func (r historyReaderObservation) CheckNativeHistory(ctx context.Context, binding pa.ExecutionBinding) error {
	err := r.NativeMaintenanceHistory.CheckNativeHistory(ctx, binding)
	r.checked <- err
	return err
}

func TestHostNativeMaintenanceHistoryKeepsInterruptedChatUnacknowledged(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	state := t.TempDir()
	f.o.NativeHistory = journalstore.NewNativeMaintenanceHistoryReader(state)
	f.start(t)
	f.enable(t)
	bot := f.create(t, "interrupted")
	receipt, err := f.send(t, bot, "original")
	if err != nil {
		t.Fatal(err)
	}
	seed := fixtureReceive(t, f.p.sent)
	binding, err := f.h.store.Binding(ctx, bot.Key, seed.Turn.Fence.BindingID)
	if err != nil {
		t.Fatal(err)
	}
	journal := establishHostHistoryFixture(t, state)
	intent := app.NativeMaintenanceIntent{
		ID: "prior-owner", Binding: binding, PrincipalID: "server-reader", ReaderID: "server-reader",
		AuthorizationRevision: "current", CLIVersion: "fake", AdapterVersion: "fake", Evidence: "fixture-only",
	}
	if _, reserved, err := journal.Reserve(ctx, intent); !reserved || err != nil {
		t.Fatal("could not stage a pending maintenance intent", reserved, err)
	}
	if err := f.h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := f.h.store.Export(ctx, bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	checked := make(chan error, 1)
	f.o.NativeHistory = historyReaderObservation{journalstore.NewNativeMaintenanceHistoryReader(state), checked}
	f.o.Broker = f.broker(t, f.o.Broker.Dir)
	f.start(t)
	f.enable(t)
	if err := fixtureReceive(t, checked); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal("restart checked the wrong native history", err)
	}
	after, err := f.h.store.Export(ctx, bot.Key)
	if err != nil || !reflect.DeepEqual(before.Turns, after.Turns) || !reflect.DeepEqual(before.Messages, after.Messages) || f.p.opens.Load() != 1 {
		t.Fatal("restricted restart created a replacement notice or opened a provider", err)
	}
	turn, err := f.h.store.Turn(ctx, bot.Key, receipt.ResourceID)
	if err != nil || turn.State != pa.TurnUnknown || turn.Failure == nil || turn.Failure.Field != app.RestartConfirmedField {
		t.Fatal("original interrupted chat was acknowledged by the blocked resume", turn, err)
	}
}

func TestHostNativeMaintenanceHistoryKeepsIsolatedRoutineAvailable(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	state := t.TempDir()
	f.o.NativeHistory = journalstore.NewNativeMaintenanceHistoryReader(state)
	f.start(t)
	f.enable(t)
	bot := f.create(t, "protected-main")
	if _, err := f.send(t, bot, "seed-main"); err != nil {
		t.Fatal(err)
	}
	seed := fixtureReceive(t, f.p.sent)
	main := fixtureReceive(t, f.p.sessions)
	binding, err := f.h.store.Binding(ctx, bot.Key, seed.Turn.Fence.BindingID)
	if err != nil {
		t.Fatal(err)
	}
	tools := main.q.Tools.(app.RoutineTools)
	disabled := false
	routine, err := tools.Routines(ctx, app.RoutineRequest{Action: "save", IdempotencyKey: "save", Name: "Fixture", Instruction: "Independent routine", IntervalSeconds: 3600, Enabled: &disabled})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tools.Routines(ctx, app.RoutineRequest{Action: "test", IdempotencyKey: "run", ID: routine.ID}); err != nil {
		t.Fatal(err)
	}
	journal := establishHostHistoryFixture(t, state)
	intent := app.NativeMaintenanceIntent{
		ID: "prior-main", Binding: binding, PrincipalID: "server-reader", ReaderID: "server-reader",
		AuthorizationRevision: "current", CLIVersion: "fake", AdapterVersion: "fake", Evidence: "fixture-only",
	}
	if _, reserved, err := journal.Reserve(ctx, intent); !reserved || err != nil {
		t.Fatal("could not stage a pending main history intent", reserved, err)
	}
	if err := f.h.engine.TickRoutines(ctx, bot.Key); err != nil {
		t.Fatal(err)
	}
	main.reply <- app.PromptResult{FinalText: "main done"}
	isolated := fixtureReceive(t, f.p.sessions)
	if isolated.q.Resume || isolated.q.Binding.SessionSlot == "" || isolated.q.Binding.NativeSessionID != "" {
		t.Fatal("independent routine reused protected main history", isolated.q)
	}
	fixtureReceive(t, f.p.sent)
	isolated.reply <- app.PromptResult{FinalText: "isolated routine"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
}
