package projectassistanthost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	hoststore "github.com/sainteye/clawdline/internal/adapters/store"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// These are test-batch inputs, not new product limits. Never enable this test
// from a gate, CI, or another test. Authorization is for this exact batch only.
const commonFlowAuthorization = "e675bf5b-common-flow-once-24-prompts"

type commonFlowLimits struct{ Opens, Prompts, Tools int }

func commonFlowFixedLimits() commonFlowLimits { return commonFlowLimits{24, 24, 96} }

type commonFlowStage struct {
	Agent    pa.Agent  `json:"agent"`
	Boundary string    `json:"boundary"`
	Status   string    `json:"status"`
	Cause    string    `json:"cause,omitempty"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitempty"`
}

type commonFlowUnsupported string

func (e commonFlowUnsupported) Error() string { return string(e) }

type commonFlowCall struct {
	Kind    string   `json:"kind"`
	Fence   pa.Fence `json:"fence"`
	Request any      `json:"request,omitempty"`
	Result  any      `json:"result,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type commonFlowEvidence struct {
	Kind      string            `json:"kind"`
	Limits    commonFlowLimits  `json:"limits"`
	Opens     int               `json:"opens"`
	Prompts   int               `json:"prompts"`
	Tools     int               `json:"tools"`
	Halt      string            `json:"halt,omitempty"`
	Stages    []commonFlowStage `json:"stages"`
	Calls     []commonFlowCall  `json:"calls"`
	Remaining []commonFlowStage `json:"remaining"`
}

// All reservations and stop decisions share a lock, including concurrent
// routine/peer execution. Reserve and journal BEFORE crossing the boundary.
type commonFlowBatch struct {
	mu     sync.Mutex
	path   string
	e      commonFlowEvidence
	opened map[string]bool
}

func newCommonFlowBatch(path, kind string, limits commonFlowLimits) *commonFlowBatch {
	b := &commonFlowBatch{path: path, e: commonFlowEvidence{Kind: kind, Limits: limits}, opened: map[string]bool{}}
	for _, agent := range []pa.Agent{pa.ClaudeCode, pa.Codex} {
		for _, boundary := range []string{"capability-admission", "main-chat", "approved-memory-continuation", "output-artifact-publication-list", "isolated-routine-output", "peer-roundtrip", "group-roundtrip", "forget-and-late-event-barrier", "fallback-refusal-unknown-safe-switch", "team-mutation", "source-revocation", "native-permission-request-denial"} {
			b.e.Remaining = append(b.e.Remaining, commonFlowStage{Agent: agent, Boundary: boundary, Status: "not-executed"})
		}
	}
	return b
}

func (b *commonFlowBatch) beginLocked(agent pa.Agent, name string) {
	for i, v := range b.e.Remaining {
		if v.Agent == agent && v.Boundary == name {
			b.e.Remaining = append(b.e.Remaining[:i], b.e.Remaining[i+1:]...)
			return
		}
	}
}

func (b *commonFlowBatch) persistLocked() error {
	data, err := json.MarshalIndent(b.e, "", "  ")
	if err == nil {
		err = os.WriteFile(b.path+".tmp", append(data, '\n'), 0600)
	}
	if err == nil {
		err = os.Rename(b.path+".tmp", b.path)
	}
	if err != nil {
		b.e.Halt = "evidence_write: " + err.Error()
	}
	return err
}

func (b *commonFlowBatch) persist() error { b.mu.Lock(); defer b.mu.Unlock(); return b.persistLocked() }
func (b *commonFlowBatch) stop(cause string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.e.Halt == "" {
		b.e.Halt = cause
	}
	_ = b.persistLocked()
}

func (b *commonFlowBatch) reserve(kind string, fence pa.Fence, request any) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.e.Halt != "" {
		return -1, failure(pa.ExecutionEffectUnknown, "common_flow_halted")
	}
	var count *int
	var limit int
	switch kind {
	case "open":
		count, limit = &b.e.Opens, b.e.Limits.Opens
	case "prompt":
		count, limit = &b.e.Prompts, b.e.Limits.Prompts
	default:
		count, limit = &b.e.Tools, b.e.Limits.Tools
	}
	if *count >= limit {
		b.e.Halt = "cap_exhausted:" + kind
		_ = b.persistLocked()
		return -1, failure(pa.ExecutionEffectUnknown, "common_flow_cap")
	}
	*count++
	i := len(b.e.Calls)
	b.e.Calls = append(b.e.Calls, commonFlowCall{Kind: kind, Fence: fence, Request: request})
	return i, b.persistLocked()
}

func (b *commonFlowBatch) complete(i int, result any, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.e.Calls[i].Result = result
	if err != nil {
		b.e.Calls[i].Error = err.Error()
		if b.e.Halt == "" {
			b.e.Halt = "boundary_error:" + err.Error()
		}
	}
	_ = b.persistLocked()
}

func (b *commonFlowBatch) stage(agent pa.Agent, name string, run func() error) error {
	b.mu.Lock()
	if b.e.Halt != "" {
		b.mu.Unlock()
		return failure(pa.ExecutionEffectUnknown, "common_flow_halted")
	}
	i := len(b.e.Stages)
	b.beginLocked(agent, name)
	b.e.Stages = append(b.e.Stages, commonFlowStage{Agent: agent, Boundary: name, Status: "running", Started: time.Now().UTC()})
	err := b.persistLocked()
	b.mu.Unlock()
	if err != nil {
		return err
	}
	// The defer preserves a running-stage failure even if a fixture uses Fatal.
	finished := false
	defer func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		s := &b.e.Stages[i]
		s.Finished = time.Now().UTC()
		s.Status = "pass"
		if !finished {
			s.Status = "failed"
			s.Cause = "stage did not return"
			b.e.Halt = s.Cause
		}
		if err != nil {
			s.Status = "failed"
			s.Cause = err.Error()
			var unsupported commonFlowUnsupported
			if errors.As(err, &unsupported) {
				s.Status = "unsupported"
			}
			if b.e.Halt == "real_person_permission_wait" {
				s.Status = "waiting-for-user"
			}
			if b.e.Halt == "" {
				b.e.Halt = s.Cause
			}
		}
		_ = b.persistLocked()
	}()
	err = run()
	if err == nil {
		b.mu.Lock()
		if b.e.Halt != "" {
			err = errors.New(b.e.Halt)
		}
		b.mu.Unlock()
	}
	finished = true
	return err
}

func (b *commonFlowBatch) unsupported(agent pa.Agent, name, cause string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.e.Halt != "" {
		return failure(pa.ExecutionEffectUnknown, "common_flow_halted")
	}
	b.beginLocked(agent, name)
	b.e.Stages = append(b.e.Stages, commonFlowStage{Agent: agent, Boundary: name, Status: "unsupported", Cause: cause, Started: time.Now().UTC(), Finished: time.Now().UTC()})
	return b.persistLocked()
}

func commonFlowRoot(token, root string) error {
	if token != commonFlowAuthorization {
		return errors.New("wrong task-specific native authorization")
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || root == string(filepath.Separator) {
		return errors.New("new absolute task-owned state root required")
	}
	// Refuse existing paths, including dangling symlinks. Mkdir is the atomic
	// final check; never attach to existing user state or silently reuse a run.
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		return errors.New("state root already exists or cannot be checked")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(root))
	if err != nil {
		return err
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return errors.New("state parent unavailable")
	}
	return os.Mkdir(root, 0700)
}

type commonFlowProvider struct {
	app.Provider
	batch *commonFlowBatch
}
type commonFlowSession struct {
	app.ProviderSession
	batch *commonFlowBatch
	fence pa.Fence
}

func (p *commonFlowProvider) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	b := p.batch
	b.mu.Lock()
	identity := fmt.Sprintf("%s/%s/%d", q.Bot.Key.BotID, q.Binding.Fence.BindingID, q.Binding.Fence.Generation)
	duplicate := b.opened[identity]
	b.opened[identity] = true
	b.mu.Unlock()
	if duplicate {
		b.stop("duplicate_open")
		return nil, failure(pa.ExecutionEffectUnknown, "common_flow_no_retry")
	}
	i, err := b.reserve("open", q.Binding.Fence, q)
	if err != nil {
		return nil, err
	}
	q.Tools = &commonFlowTools{NativeTools: q.Tools, batch: b, fence: q.Binding.Fence}
	wrappedSink := func(ctx context.Context, e app.ProtocolEvent) error {
		// A real offer is evidence, never a grant to choose an answer. Stop
		// globally before the Engine can observe a subsequent queued action.
		if e.Permission != nil {
			b.stop("real_person_permission_wait")
		}
		err := sink(ctx, e)
		b.mu.Lock()
		b.e.Calls = append(b.e.Calls, commonFlowCall{Kind: "event", Fence: e.Fence, Request: e})
		_ = b.persistLocked()
		b.mu.Unlock()
		if err != nil {
			b.stop("event_error:" + err.Error())
		}
		return err
	}
	s, err := p.Provider.Open(ctx, q, wrappedSink)
	var nativeID string
	if s != nil {
		nativeID = s.SessionID()
	}
	b.complete(i, nativeID, err)
	if s == nil {
		return nil, err
	}
	return &commonFlowSession{ProviderSession: s, batch: b, fence: q.Binding.Fence}, err
}

func (s *commonFlowSession) Send(ctx context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	i, err := s.batch.reserve("prompt", q.Turn.Fence, q)
	if err != nil {
		return nil, err
	}
	p, err := s.ProviderSession.Send(ctx, q)
	s.batch.complete(i, map[string]bool{"pending_handle": p != nil}, err)
	if p == nil {
		return nil, err
	}
	return commonFlowPending{PendingPrompt: p, batch: s.batch}, err
}

// Answer is deliberately unavailable even if an unexpected caller tries it.
func (s *commonFlowSession) Answer(context.Context, string, string) error {
	s.batch.stop("unexpected_permission_answer")
	return failure(pa.ScopeDenied, "common_flow_no_permission_answer")
}

type commonFlowPending struct {
	app.PendingPrompt
	batch *commonFlowBatch
}

func (p commonFlowPending) Wait(ctx context.Context) (app.PromptResult, error) {
	r, e := p.PendingPrompt.Wait(ctx)
	if e != nil {
		p.batch.stop("wait_error:" + e.Error())
	}
	return r, e
}

type commonFlowTools struct {
	app.NativeTools
	batch *commonFlowBatch
	fence pa.Fence
}

func (w *commonFlowTools) call(kind string, q any, run func() (any, error)) (any, error) {
	i, e := w.batch.reserve(kind, w.fence, q)
	if e != nil {
		return nil, e
	}
	r, e := run()
	w.batch.complete(i, r, e)
	return r, e
}
func (w *commonFlowTools) Memory(ctx context.Context) ([]pa.MemoryEntry, error) {
	r, e := w.call("read_memory", nil, func() (any, error) { return w.NativeTools.Memory(ctx) })
	if r == nil {
		return nil, e
	}
	return r.([]pa.MemoryEntry), e
}
func (w *commonFlowTools) SendMessage(ctx context.Context, id, text string) error {
	_, e := w.call("send_message", map[string]string{"id": id, "text": text}, func() (any, error) { return nil, w.NativeTools.SendMessage(ctx, id, text) })
	return e
}
func (w *commonFlowTools) EditMemory(context.Context, string, app.MemoryEdit) error {
	w.batch.stop("unapproved_memory_edit")
	return failure(pa.ScopeDenied, "common_flow_memory_edit")
}
func (w *commonFlowTools) Dispatch(context.Context, string, string, string, []string) (pa.WorkLink, error) {
	w.batch.stop("dispatch_forbidden")
	return pa.WorkLink{}, failure(pa.ScopeDenied, "common_flow_dispatch")
}
func (w *commonFlowTools) Status(context.Context, string) (pa.WorkLink, error) {
	w.batch.stop("work_status_forbidden")
	return pa.WorkLink{}, failure(pa.ScopeDenied, "common_flow_work_status")
}
func (w *commonFlowTools) Routines(ctx context.Context, q app.RoutineRequest) (app.RoutineResult, error) {
	r, e := w.call("routine", q, func() (any, error) {
		p, ok := w.NativeTools.(app.RoutineTools)
		if !ok {
			return nil, failure(pa.Unsupported, "routine_tools")
		}
		return p.Routines(ctx, q)
	})
	if r == nil {
		return app.RoutineResult{}, e
	}
	return r.(app.RoutineResult), e
}
func (w *commonFlowTools) PublishOutput(ctx context.Context, id string, q app.OutputRequest) (pa.Output, error) {
	r, e := w.call("publish_output", struct {
		ID      string
		Request app.OutputRequest
	}{id, q}, func() (any, error) {
		p, ok := w.NativeTools.(app.OutputTools)
		if !ok {
			return nil, failure(pa.Unsupported, "output_tools")
		}
		return p.PublishOutput(ctx, id, q)
	})
	if r == nil {
		return pa.Output{}, e
	}
	return r.(pa.Output), e
}
func (w *commonFlowTools) ListArtifacts(ctx context.Context) ([]app.ArtifactInfo, error) {
	r, e := w.call("list_artifacts", nil, func() (any, error) {
		p, ok := w.NativeTools.(app.OutputTools)
		if !ok {
			return nil, failure(pa.Unsupported, "output_tools")
		}
		return p.ListArtifacts(ctx)
	})
	if r == nil {
		return nil, e
	}
	return r.([]app.ArtifactInfo), e
}
func (w *commonFlowTools) ListBots(ctx context.Context, q pa.BotDirectoryQuery) (pa.BotDirectoryPage, error) {
	r, e := w.call("list_bots", q, func() (any, error) {
		p, ok := w.NativeTools.(app.CollaborationTools)
		if !ok {
			return nil, failure(pa.Unsupported, "collaboration_tools")
		}
		return p.ListBots(ctx, q)
	})
	if r == nil {
		return pa.BotDirectoryPage{}, e
	}
	return r.(pa.BotDirectoryPage), e
}
func (w *commonFlowTools) MessageBot(ctx context.Context, q pa.BotMessageRequest) (pa.Exchange, error) {
	r, e := w.call("message_bot", q, func() (any, error) {
		p, ok := w.NativeTools.(app.CollaborationTools)
		if !ok {
			return nil, failure(pa.Unsupported, "collaboration_tools")
		}
		return p.MessageBot(ctx, q)
	})
	if r == nil {
		return pa.Exchange{}, e
	}
	return r.(pa.Exchange), e
}

// This fixture reuses the real composition but moves every retained byte under
// the authorized batch root. No existing Bot, Project or database is opened.
func commonFlowFixture(t *testing.T, root string, p app.Provider) *hostFixture {
	t.Helper()
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	f := newHostFixture(t)
	if err := f.st.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	f.st, err = hoststore.Open(filepath.Join(root, "database"))
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, "project")
	if err = os.Rename(f.repo, project); err != nil {
		t.Fatal(err)
	}
	f.repo = project
	f.o.Database = f.st.ProjectAssistantDatabase()
	f.o.Broker = f.broker(t, filepath.Join(root, "broker"))
	f.o.Outputs = featurestore.Outputs{Dir: filepath.Join(root, "outputs")}
	f.o.Provider = func() (app.Provider, error) { return p, nil }
	f.o.Limits = app.RuntimeLimits{MaxBots: capacity.MaximumBotActors, PromptTimeout: time.Duration(capacity.MaximumBotControlWindowMilliseconds) * time.Millisecond, CleanupTimeout: time.Duration(capacity.MaximumBotCleanupMilliseconds) * time.Millisecond}
	f.start(t)
	f.enable(t)
	return f
}

func commonFlowCreate(t *testing.T, f *hostFixture, agent pa.Agent, id string) pa.Bot {
	t.Helper()
	s, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.Create(context.Background(), pa.Command{Key: pa.BotKey{MachineID: f.scope.MachineID, BotID: id}, Operation: pa.CreateOperation, IdempotencyKey: "create-" + id}, pa.CreateBot{Target: pa.ProjectSelection{MachineID: f.scope.MachineID, ProjectID: f.scope.ProjectID}, Name: id, Agent: agent, Appearance: pa.Appearance{Shape: pa.EntryShape, Color: pa.EntryColor}, PermissionPolicy: pa.Ask, Instructions: "Follow only the current harness request. Use only named Clawdline tools. Never dispatch work, edit memory, access networks or run commands. A peer review request returns only its requested final text. A correlated peer reply sends its exact review text once using send_message with idempotency_key peer-summary. Routine runs return only their requested final text. In group rooms answer only when directly addressed. Otherwise final text is exactly Done."})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func commonFlowSend(ctx context.Context, f *hostFixture, b pa.Bot, id, text string) (pa.Receipt, error) {
	s, err := f.h.Service()
	if err != nil {
		return pa.Receipt{}, err
	}
	_, c, _, err := s.Get(ctx, b.Key)
	if err != nil {
		return pa.Receipt{}, err
	}
	return s.Send(ctx, pa.Command{Key: b.Key, Operation: pa.SendOperation, IdempotencyKey: id}, pa.SendMessage{Fence: pa.Fence{Key: b.Key, Project: b.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}, Text: text})
}

func commonFlowWait(t *testing.T, f *hostFixture, b pa.Bot, batch *commonFlowBatch, check func(app.Export) (bool, error)) (app.Export, error) {
	t.Helper()
	deadline := time.Now().Add(f.o.Limits.PromptTimeout + f.o.Limits.CleanupTimeout)
	for {
		x := qualificationReopen(t, f, b.Key)
		for _, turn := range x.Turns {
			switch turn.State {
			case pa.TurnFailed, pa.TurnCancelled, pa.TurnUnknown, pa.TurnWaitingPermission:
				return x, fmt.Errorf("turn %s is %s (%v)", turn.ID, turn.State, turn.Failure)
			}
		}
		batch.mu.Lock()
		halt := batch.e.Halt
		batch.mu.Unlock()
		if halt != "" {
			return x, errors.New(halt)
		}
		ok, err := check(x)
		if err != nil {
			return x, err
		}
		if ok {
			return x, nil
		}
		if time.Now().After(deadline) {
			return x, errors.New("deadline; no retry or unknown acknowledgement allowed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func commonFlowReceipt(x app.Export, b pa.Bot, r pa.Receipt, text, expected string) error {
	var turn *pa.Turn
	for i := range x.Turns {
		if x.Turns[i].ID == r.ResourceID {
			turn = &x.Turns[i]
		}
	}
	if turn == nil || turn.State != pa.TurnSucceeded || turn.Fence.Key != b.Key || turn.Fence.Project != b.Project {
		return errors.New("missing successful scoped turn")
	}
	users, answers, receipts := 0, 0, 0
	for _, m := range x.Messages {
		if m.TurnID != turn.ID {
			continue
		}
		if m.Fence.Key != b.Key || m.Fence.Project != b.Project || m.Fence.ConversationID != turn.Fence.ConversationID {
			return errors.New("message scope escaped")
		}
		if m.Kind == pa.UserMessage && m.Text == text {
			users++
			events := 0
			for _, e := range x.Events {
				if e.Kind == pa.MessageCreated && e.Message != nil && e.Message.ID == m.ID && e.Fence == m.Fence {
					events++
				}
			}
			if events != 1 {
				return errors.New("accepted input lacks one durable event")
			}
		}
		if m.Kind == pa.AssistantMessage && m.Text == expected {
			answers++
			if m.Fence != turn.Fence {
				return errors.New("answer execution fence escaped")
			}
		}
	}
	for _, v := range x.Receipts {
		if v.ID == r.ID && v.ResourceID == turn.ID && v.Command == r.Command {
			receipts++
		}
	}
	if users != 1 || answers != 1 || receipts != 1 {
		return fmt.Errorf("durable counts input=%d answer=%d receipt=%d", users, answers, receipts)
	}
	return nil
}

func commonFlowTurn(t *testing.T, f *hostFixture, b pa.Bot, batch *commonFlowBatch, id, text, expected string) (app.Export, error) {
	r, err := commonFlowSend(context.Background(), f, b, id, text)
	if err != nil {
		return app.Export{}, err
	}
	x, err := commonFlowWait(t, f, b, batch, func(x app.Export) (bool, error) {
		for _, v := range x.Turns {
			if v.ID == r.ResourceID {
				return v.State == pa.TurnSucceeded && x.Conversation.ActiveTurnID == "", nil
			}
		}
		return false, nil
	})
	if err == nil {
		err = commonFlowReceipt(x, b, r, text, expected)
	}
	return x, err
}

func TestInstalledAgentsNativeCommonFlow(t *testing.T) {
	token := os.Getenv("CLAWDLINE_NATIVE_COMMON_FLOW_E675BF5B")
	if token == "" {
		t.Skip("installed common flow NOT EXECUTED; separate capped authorization required")
	}
	root := os.Getenv("CLAWDLINE_NATIVE_COMMON_FLOW_ROOT")
	if err := commonFlowRoot(token, root); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAWDLINE_NEXT_DIR", root)
	batch := newCommonFlowBatch(filepath.Join(root, "evidence.json"), "installed-native", commonFlowFixedLimits())
	defer func() {
		if err := batch.persist(); err != nil {
			t.Error(err)
		}
	}()
	p := &commonFlowProvider{Provider: acp.InstalledProvider(root), batch: batch}
	for _, agent := range []pa.Agent{pa.ClaudeCode, pa.Codex} {
		if !t.Run(string(agent), func(t *testing.T) { commonFlowAgent(t, root, agent, p, batch, true) }) {
			t.Fatal("entire batch stopped; no next stage, agent, resend or retry")
		}
	}
}

func commonFlowAgent(t *testing.T, root string, agent pa.Agent, p *commonFlowProvider, batch *commonFlowBatch, probePermission bool) {
	t.Helper()
	ctx := context.Background()
	f := commonFlowFixture(t, filepath.Join(root, string(agent)), p)
	b := commonFlowCreate(t, f, agent, "common-"+string(agent))
	peer := commonFlowCreate(t, f, agent, "peer-"+string(agent))
	// Registered after fixture cleanup, so export precedes closure on Fatal.
	t.Cleanup(func() {
		qualificationJSON(t, filepath.Join(root, string(agent)+"-final.json"), qualificationReopen(t, f, b.Key))
		qualificationJSON(t, filepath.Join(root, string(agent)+"-peer-final.json"), qualificationReopen(t, f, peer.Key))
		if err := f.h.Disable(ctx); err != nil {
			batch.stop("cleanup:" + err.Error())
			t.Error(err)
		}
		qualificationJSON(t, filepath.Join(root, string(agent)+"-usage.json"), f.h.Usage())
		if err := batch.persist(); err != nil {
			t.Error(err)
		}
	})
	stage := func(name string, fn func() error) {
		t.Helper()
		t.Logf("common-flow %s: %s", agent, name)
		if err := batch.stage(agent, name, fn); err != nil {
			t.Fatal(name, err)
		}
	}
	stage("capability-admission", func() error {
		caps, err := p.Capabilities(ctx, b)
		qualificationJSON(t, filepath.Join(root, string(agent)+"-capabilities.json"), caps)
		if err != nil {
			return err
		}
		if !caps.Resume || !caps.NativeMemoryVerified || caps.NativeMemory != pa.NativeMemoryDisabled {
			return errors.New("native resume or memory suppression is unsupported")
		}
		return nil
	})
	stem := "common-" + string(agent)
	chatToken := stem + "-chat-token"
	memoryToken := stem + "-approved-token"
	stage("main-chat", func() error {
		_, err := commonFlowTurn(t, f, b, batch, "first", "Read your own memory using read_memory; it must be empty. Retain the conversation-only token "+chatToken+". Call send_message exactly once with idempotency_key first and text "+stem+"-first. Final: Done.", stem+"-first")
		return err
	})
	stage("approved-memory-continuation", func() error {
		s, _ := f.h.Service()
		before := qualificationReopen(t, f, b.Key)
		if _, err := s.SaveMemory(ctx, app.MemoryWrite{Command: pa.Command{Key: b.Key, Operation: pa.MemorySaveOperation, IdempotencyKey: "approved"}, ExpectedRevision: before.Memory.Revision, Entry: pa.MemoryEntry{ID: "approved", Key: b.Key, Project: b.Project, Text: "The approved common-flow fact is " + memoryToken}}); err != nil {
			return err
		}
		x, err := commonFlowTurn(t, f, b, batch, "second", "Read your own memory with read_memory. Call send_message once with idempotency_key second and text composed of the approved common-flow token, one space, then the conversation-only token retained earlier. Do not guess. Final: Done.", memoryToken+" "+chatToken)
		if err == nil && (len(x.MemoryEntries) != 1 || x.MemoryEntries[0].Key != b.Key || x.MemoryEntries[0].Project != b.Project) {
			return errors.New("approved memory scope escaped")
		}
		if err == nil {
			batch.mu.Lock()
			defer batch.mu.Unlock()
			var opens []commonFlowCall
			var reads [][]pa.MemoryEntry
			for _, call := range batch.e.Calls {
				if call.Fence.Key != b.Key {
					continue
				}
				if call.Kind == "open" {
					opens = append(opens, call)
				}
				if call.Kind == "read_memory" && call.Error == "" {
					reads = append(reads, call.Result.([]pa.MemoryEntry))
				}
			}
			if len(opens) != 2 || opens[0].Request.(app.OpenRequest).Resume || !opens[1].Request.(app.OpenRequest).Resume || opens[0].Result == "" || opens[0].Result != opens[1].Result {
				return errors.New("native fresh/resume identity mismatch")
			}
			if len(reads) != 2 || len(reads[0]) != 0 || len(reads[1]) != 1 || reads[1][0].Text != "The approved common-flow fact is "+memoryToken {
				return errors.New("actual MCP reads did not observe empty then approved memory")
			}
		}
		return err
	})
	stage("output-artifact-publication-list", func() error {
		// Input bytes are fixtures. Only actual model-owned publication creates
		// output cards; the harness never seeds a successful output receipt.
		path := filepath.Join(f.repo, "common.html")
		if err := os.WriteFile(path, []byte("<!doctype html><title>Common flow</title><p>"+stem+"</p>"), 0600); err != nil {
			return err
		}
		text := "Use send_file once with idempotency_key file, path " + path + ", name common.html. Use publish_artifact once with idempotency_key html, path " + path + ", title Common flow. Then list_artifacts; verify the single published artifact. Call send_message once with idempotency_key outputs and text " + stem + "-outputs. Final: Done."
		x, err := commonFlowTurn(t, f, b, batch, "outputs", text, stem+"-outputs")
		if err != nil {
			return err
		}
		outputs := 0
		html := 0
		for _, m := range x.Messages {
			if m.Output != nil {
				outputs++
				if m.Output.ArtifactID != "" {
					html++
				}
				raw, e := os.ReadFile(mustCommonFlowOutputPath(t, f, b.Key, *m.Output))
				if e != nil || !strings.Contains(string(raw), stem) {
					return errors.New("published output bytes mismatch")
				}
			}
		}
		if outputs != 2 || html != 1 {
			return errors.New("missing actual file/artifact publication")
		}
		return commonFlowRequireCalls(batch, b.Key, "publish_output", 2, "list_artifacts", 1)
	})
	stage("isolated-routine-output", func() error {
		text := "Call save_routine once with idempotency_key schedule, name Common flow, instruction Return exactly " + stem + "-routine as final text without tools, interval_seconds 3600, enabled false. Call test_run_routine once with idempotency_key test and its returned routine id. Send one message via send_message, idempotency_key routine-ready, text " + stem + "-routine-ready. Final: Done."
		if _, err := commonFlowTurn(t, f, b, batch, "routine", text, stem+"-routine-ready"); err != nil {
			return err
		}
		if err := f.h.engine.TickRoutines(ctx, b.Key); err != nil {
			return err
		}
		x, err := commonFlowWait(t, f, b, batch, func(x app.Export) (bool, error) {
			return len(x.RoutineRuns) == 1 && x.RoutineRuns[0].State == pa.TurnSucceeded && x.Conversation.ActiveTurnID == "", nil
		})
		if err != nil {
			return err
		}
		if len(x.Routines) != 1 || x.Routines[0].Enabled {
			return errors.New("routine was not paused")
		}
		run := x.RoutineRuns[0]
		found := 0
		for _, m := range x.Messages {
			if m.TurnID == run.TurnID && m.Text == stem+"-routine" {
				found++
			}
		}
		if found != 1 {
			return errors.New("routine output absent")
		}
		batch.mu.Lock()
		defer batch.mu.Unlock()
		mainID := ""
		for _, c := range batch.e.Calls {
			if c.Kind == "open" {
				q := c.Request.(app.OpenRequest)
				if q.Bot.Key == b.Key && q.Binding.SessionSlot == "" && mainID == "" {
					mainID, _ = c.Result.(string)
				}
				if q.Binding.SessionSlot != "" && q.Bot.Key == b.Key {
					if q.Resume || mainID == "" || c.Result == mainID {
						return errors.New("routine resumed main native session")
					}
					return nil
				}
			}
		}
		return errors.New("isolated routine native open absent")
	})
	stage("peer-roundtrip", func() error {
		text := "Call list_bots, then message_bot exactly once with idempotency_key peer, target_bot_id " + peer.Key.BotID + ", text Return exactly " + stem + "-peer-reviewed as your final text. Do not poll or wait. Send one message with send_message idempotency_key peer-start, text " + stem + "-peer-start. Final: Done."
		// Recipient and correlated source reply are runtime-owned extra turns.
		if _, err := commonFlowTurn(t, f, b, batch, "peer", text, stem+"-peer-start"); err != nil {
			return err
		}
		x, err := commonFlowWait(t, f, b, batch, func(x app.Export) (bool, error) {
			return len(x.Exchanges) == 1 && x.Exchanges[0].Outcome == pa.ExchangeCompleted && x.Exchanges[0].Delivery == "delivered" && x.Conversation.ActiveTurnID == "", nil
		})
		if err != nil {
			return err
		}
		e := x.Exchanges[0]
		if e.Source.Key != b.Key || e.Target.Key != peer.Key || e.Project != b.Project || e.Reply != stem+"-peer-reviewed" || e.ReplyTurnID == "" {
			return errors.New("peer correlation or scope mismatch")
		}
		summaries := 0
		for _, m := range x.Messages {
			if m.TurnID == e.ReplyTurnID && m.Kind == pa.AssistantMessage {
				if m.Text != e.Reply || m.Fence.Key != b.Key || m.Fence.Project != b.Project {
					return errors.New("correlated source summary mismatch")
				}
				summaries++
			}
		}
		if summaries != 1 {
			return errors.New("correlated source summary missing or duplicated")
		}
		return commonFlowRequireCalls(batch, b.Key, "message_bot", 1, "list_bots", 1)
	})
	stage("group-roundtrip", func() error {
		s, _ := f.h.Service()
		g, _, err := s.CreateGroup(ctx, pa.Command{Key: pa.BotKey{MachineID: b.Key.MachineID, BotID: "room-" + string(agent)}, Operation: pa.CreateOperation, IdempotencyKey: "room"}, app.CreateGroup{Target: pa.ProjectSelection{MachineID: b.Key.MachineID, ProjectID: b.Project.ProjectID}, Name: "Common flow", Members: []pa.BotKey{b.Key, peer.Key}})
		if err != nil {
			return err
		}
		if _, _, err = s.SendGroup(ctx, pa.Command{Key: g.Key, Operation: pa.SendOperation, IdempotencyKey: "room-message"}, "@"+b.Name+" reply exactly "+stem+"-room as final text, without tools; only the addressed Bot replies.", pa.BotKey{}); err != nil {
			return err
		}
		_, err = commonFlowWait(t, f, b, batch, func(x app.Export) (bool, error) {
			h, e := s.GroupHistory(ctx, app.GroupHistoryQuery{Key: g.Key, PageSize: 10})
			if e != nil {
				return false, e
			}
			for _, line := range h.Lines {
				if line.Author == b.Key && line.Text == stem+"-room" {
					return h.Room.Running == 0 && h.Room.Pending == 0 && f.h.Usage().ActiveActors == 0, nil
				}
			}
			return false, nil
		})
		return err
	})
	stage("forget-and-late-event-barrier", func() error {
		s, _ := f.h.Service()
		before := qualificationReopen(t, f, b.Key)
		old := before.Turns[len(before.Turns)-1]
		if _, err := s.Forget(ctx, app.ForgetRequest{Command: pa.Command{Key: b.Key, Operation: pa.ForgetOperation, IdempotencyKey: "forget"}, Project: b.Project, ExpectedRevision: before.Memory.Revision, SourceIDs: []string{"manual:approved"}}); err != nil {
			return err
		}
		if err := f.h.engine.Publish(ctx, app.ProtocolEvent{ID: "late-before-forget", Fence: old.Fence, TurnID: old.ID, Message: "must-never-appear"}); err == nil {
			return errors.New("old native event accepted after forget")
		}
		x, err := commonFlowTurn(t, f, b, batch, "after-forget", "Read memory using read_memory. It must be empty. Call send_message once with idempotency_key after-forget and text "+stem+"-forgotten. Final: Done.", stem+"-forgotten")
		if err != nil {
			return err
		}
		for _, entry := range x.MemoryEntries {
			if !entry.Forgotten {
				return errors.New("forgotten memory reappeared")
			}
		}
		if x.Context.Text != "" {
			return errors.New("forgotten context reappeared")
		}
		batch.mu.Lock()
		defer batch.mu.Unlock()
		var first, last *commonFlowCall
		var prompt *app.PromptRequest
		var reads [][]pa.MemoryEntry
		for i := range batch.e.Calls {
			call := &batch.e.Calls[i]
			if call.Fence.Key != b.Key {
				continue
			}
			if call.Kind == "open" {
				if first == nil {
					first = call
				}
				last = call
			}
			if call.Kind == "prompt" {
				v := call.Request.(app.PromptRequest)
				prompt = &v
			}
			if call.Kind == "read_memory" && call.Error == "" {
				reads = append(reads, call.Result.([]pa.MemoryEntry))
			}
		}
		if len(reads) != 3 || len(reads[2]) != 0 {
			return errors.New("actual native read_memory did not observe forgotten memory")
		}
		if first == nil || last == nil || last.Request.(app.OpenRequest).Resume || last.Result == first.Result || prompt == nil || prompt.Context.Text != "" {
			return errors.New("forget did not create a fresh native owner with empty memory context")
		}
		return nil
	})
	// Installed adapters expose no safe, deterministic no-send refusal injector.
	// Do not sign out, exhaust credits, or forge a provider guarantee to get green.
	if err := batch.unsupported(agent, "fallback-refusal-unknown-safe-switch", "InstalledProvider has no no-send fault injection port; natural refusal/unknown cannot be forced safely. Deterministic controls are separate; no native fallback pass is claimed."); err != nil {
		t.Fatal(err)
	}
	if err := batch.unsupported(agent, "team-mutation", "create_bot/update_bot require a real person's approval card; this batch never approves cards. Peer and group are exercised separately."); err != nil {
		t.Fatal(err)
	}
	stage("source-revocation", func() error {
		f.revoked.Store(true)
		before := qualificationReopen(t, f, b.Key)
		_, err := commonFlowSend(ctx, f, b, "revoked", "must not execute")
		if pa.Code(err) != pa.Revoked {
			return fmt.Errorf("revoked source was not refused: %v", err)
		}
		after := qualificationReopen(t, f, b.Key)
		if len(after.Turns) != len(before.Turns) || len(after.Messages) != len(before.Messages) {
			return errors.New("revoked admission changed durable state")
		}
		f.revoked.Store(false)
		return nil
	})
	if !probePermission {
		return
	}
	// Last because a legitimate permission wait MUST end the entire batch.
	// This read is confined to a task-owned fixture. No permission is answered.
	stage("native-permission-request-denial", func() error {
		_, err := commonFlowSend(ctx, f, b, "permission", "For this safe permission probe only, request your native read-file tool to read a.go in this Project. Do not use shell, writes, network or Clawdline tools. If permission is denied, return exactly Denied; never retry.")
		if err != nil {
			return err
		}
		_, err = commonFlowWait(t, f, b, batch, func(x app.Export) (bool, error) {
			for _, o := range x.Permissions {
				if o.State == pa.PermissionPending {
					return false, errors.New("real person permission wait; no denial/allow answer sent")
				}
			}
			if x.Conversation.ActiveTurnID == "" {
				return false, commonFlowUnsupported("native read produced no permission offer; denial unsupported on this adapter/tool path")
			}
			return false, nil
		})
		return err
	})
}

func mustCommonFlowOutputPath(t *testing.T, f *hostFixture, key pa.BotKey, o pa.Output) string {
	t.Helper()
	p, e := f.o.Outputs.Path(key, o)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func commonFlowRequireCalls(b *commonFlowBatch, key pa.BotKey, one string, n int, two string, m int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	a, c := 0, 0
	for _, call := range b.e.Calls {
		if call.Fence.Key == key && call.Error == "" {
			if call.Kind == one {
				a++
			}
			if call.Kind == two {
				c++
			}
		}
	}
	if a != n || c != m {
		return fmt.Errorf("tool calls %s=%d/%d %s=%d/%d", one, a, n, two, c, m)
	}
	return nil
}
