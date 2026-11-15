package projectassistanthost

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// This is intentionally outside the ordinary test contract. Authorization for
// four native prompts is required; never set this flag in CI or check.sh.
// A failed/unknown outcome stops the entire run, including the other agent.
func TestInstalledAgentsNativeQualification(t *testing.T) {
	if os.Getenv("CLAWDLINE_NATIVE_QUALIFICATION_07DDAF71") != "four-prompts-once" {
		t.Skip("task-specific authorization for installed native agents is absent")
	}
	root := os.Getenv("CLAWDLINE_NATIVE_QUALIFICATION_ROOT")
	if !filepath.IsAbs(root) {
		t.Fatal("qualification root must be an absolute, empty task-owned directory")
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal("qualification root must not already exist:", err)
	}
	p := &qualificationProvider{Provider: acp.InstalledProvider(root), opened: map[string]bool{}, evidence: filepath.Join(root, "observations.json")}
	defer func() { qualificationJSON(t, filepath.Join(root, "observations.json"), p.snapshot()) }()
	// Check both installed tools and native-memory admission before spending a
	// prompt on either. This does not test account/model access.
	for _, agent := range []pa.Agent{pa.ClaudeCode, pa.Codex} {
		b := pa.Bot{Key: pa.BotKey{MachineID: "source", BotID: "qualification-" + string(agent)}, Project: pa.ProjectScope{MachineID: "source", ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}, Agent: agent}
		caps, err := p.Capabilities(context.Background(), b)
		qualificationJSON(t, filepath.Join(root, string(agent)+"-setup.json"), caps)
		if err != nil {
			t.Fatal("installed preflight:", err)
		}
	}
	for _, agent := range []pa.Agent{pa.ClaudeCode, pa.Codex} {
		if !t.Run(string(agent), func(t *testing.T) {
			f := newHostFixture(t)
			f.o.Limits = app.RuntimeLimits{MaxBots: capacity.MaximumBotActors, MaxProcesses: capacity.MaximumBotProcesses,
				PromptTimeout:  time.Duration(capacity.MaximumBotPromptMilliseconds) * time.Millisecond,
				CleanupTimeout: time.Duration(capacity.MaximumBotCleanupMilliseconds) * time.Millisecond}
			f.o.Provider = func() (app.Provider, error) { return p, nil }
			f.start(t)
			f.enable(t)
			s, err := f.h.Service()
			if err != nil {
				t.Fatal(err)
			}
			key := pa.BotKey{MachineID: f.scope.MachineID, BotID: "qualification-" + string(agent)}
			b, _, err := s.Create(context.Background(), pa.Command{Key: key, Operation: pa.CreateOperation, IdempotencyKey: "create"}, pa.CreateBot{
				Target: pa.ProjectSelection{MachineID: f.scope.MachineID, ProjectID: f.scope.ProjectID}, Name: "Qualification Bot", Agent: agent,
				Appearance: pa.Appearance{Shape: pa.EntryShape, Color: pa.EntryColor}, PermissionPolicy: pa.Ask,
				Instructions: "Use only Clawdline read_memory and send_message for this qualification. Do not inspect files, run commands, change settings, create rules, or use other tools. Final replies must be exactly Done. Never repeat a tool message in the final reply."})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				qualificationJSON(t, filepath.Join(root, string(agent)+"-final.json"), qualificationReopen(t, f, key))
			})
			if b.Mode != pa.AutoMode && b.Mode != "" {
				t.Fatal("default mode changed", b.Mode)
			}
			caps, err := p.Capabilities(context.Background(), b)
			qualificationJSON(t, filepath.Join(root, string(agent)+"-capabilities.json"), caps)
			if err != nil {
				t.Fatal("installed admission:", err)
			}
			if caps.NativeMemory != pa.NativeMemoryDisabled || !caps.NativeMemoryVerified || caps.Resume != (agent == pa.ClaudeCode) {
				t.Fatal("unexpected native memory/resume contract", caps)
			}
			before := qualificationReopen(t, f, b.Key)
			if len(before.Messages) != 0 || len(before.Turns) != 0 || len(before.MemoryEntries) != 0 {
				t.Fatal("fresh Bot was not empty")
			}
			stem := "qualification-" + string(agent)
			nonce1, nonce2 := stem+"-first", stem+"-second"
			contextToken, memoryToken := "violet-bridge-"+string(agent), "amber-orbit-"+string(agent)
			first := fmt.Sprintf("Read your own Bot memory with read_memory and verify it is empty. Retain this conversation-only token for the next turn: %s. Then call send_message exactly once with idempotency_key first and text exactly %s. Do not send anything else through send_message. Final reply: Done.", contextToken, nonce1)
			x := qualificationTurn(t, f, b, "first", first, nonce1)
			qualificationJSON(t, filepath.Join(root, string(agent)+"-first.json"), x)
			if _, err = s.SaveMemory(context.Background(), app.MemoryWrite{Command: pa.Command{Key: key, Operation: pa.MemorySaveOperation, IdempotencyKey: "approved-fact"},
				ExpectedRevision: x.Memory.Revision, Entry: pa.MemoryEntry{ID: "approved-fact", Key: key, Project: b.Project, Text: "The approved qualification memory token is " + memoryToken}}); err != nil {
				t.Fatal(err)
			}
			second := "Read your own Bot memory with read_memory. Call send_message exactly once with idempotency_key second and text consisting of " + nonce2 + ", then one space, then the approved qualification memory token, then one space, then the conversation-only token from the previous turn. Do not guess missing tokens or use other tools. Final reply: Done."
			x = qualificationTurn(t, f, b, "second", second, nonce2+" "+memoryToken+" "+contextToken)
			qualificationJSON(t, filepath.Join(root, string(agent)+"-second.json"), x)
			if len(x.MemoryEntries) != 1 || x.MemoryEntries[0].Key != key || x.MemoryEntries[0].Project != b.Project {
				t.Fatal("approved memory scope changed")
			}
			p.mu.Lock()
			var opens []qualificationOpen
			var reads []qualificationRead
			for _, o := range p.opens {
				if o.Request.Bot.Key == key {
					opens = append(opens, o)
				}
			}
			for _, r := range p.reads {
				if r.Fence.Key == key {
					reads = append(reads, r)
				}
			}
			p.mu.Unlock()
			if len(opens) != 2 || opens[0].Request.Resume || opens[1].Request.Resume != caps.Resume {
				t.Fatal("unexpected fresh/resume path", opens)
			}
			if caps.Resume && opens[0].NativeID != opens[1].NativeID || !caps.Resume && opens[0].NativeID == opens[1].NativeID {
				t.Fatal("native session identity did not follow resume contract")
			}
			if len(reads) != 2 || len(reads[0].Entries) != 0 || len(reads[1].Entries) != 1 || reads[1].Entries[0].Text != "The approved qualification memory token is "+memoryToken {
				t.Fatal("native read_memory did not observe empty then approved memory", reads)
			}
			if err = f.h.Disable(context.Background()); err != nil {
				t.Fatal("native cleanup:", err)
			}
			t.Logf("%s: two native opens, two prompt writes, two MCP reads, two durable nonce messages; resume=%t", agent, caps.Resume)
		}) {
			t.Fatal("qualification stopped; no automatic retry or further native prompt")
		}
	}
}

func qualificationJSON(t *testing.T, path string, value any) {
	t.Helper()
	b, err := json.MarshalIndent(value, "", "  ")
	if err == nil {
		err = os.WriteFile(path, append(b, '\n'), 0600)
	}
	if err != nil {
		t.Error("qualification evidence:", err)
	}
}

// This scripted control validates the qualification's durable assertions. It
// never constructs InstalledProvider and is not vendor qualification evidence.
func TestQualificationDurableReadbackKeepsBothFenceStages(t *testing.T) {
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	b := f.create(t, "readback-control")
	go func() {
		s := <-f.p.sessions
		q := <-f.p.sent
		if err := s.sink(context.Background(), app.ProtocolEvent{ID: "control-nonce", Fence: q.Turn.Fence, TurnID: q.Turn.ID, Message: "control-nonce"}); err != nil {
			t.Error(err)
		}
		s.reply <- app.PromptResult{FinalText: "Done."}
	}()
	qualificationTurn(t, f, b, "control", "One scripted control", "control-nonce")
}

// Reopen through a separate query-only SQLite connection, rather than trusting
// a provider reply or a previously returned in-memory aggregate.
func qualificationReopen(t *testing.T, f *hostFixture, key pa.BotKey) app.Export {
	t.Helper()
	var seq int
	var name, path string
	if err := f.o.Database.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	x, err := featurestore.New(db).Export(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func qualificationTurn(t *testing.T, f *hostFixture, b pa.Bot, id, text, expected string) app.Export {
	t.Helper()
	s, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	_, c, _, err := s.Get(context.Background(), b.Key)
	if err != nil {
		t.Fatal(err)
	}
	inputFence := pa.Fence{Key: b.Key, Project: b.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}
	receipt, err := s.Send(context.Background(), pa.Command{Key: b.Key, Operation: pa.SendOperation, IdempotencyKey: id}, pa.SendMessage{Fence: inputFence, Text: text})
	if err != nil {
		t.Fatal("send acceptance:", err)
	}
	deadline := time.Now().Add(f.o.Limits.PromptTimeout + f.o.Limits.CleanupTimeout)
	for {
		x, err := f.h.store.Export(context.Background(), b.Key)
		if err != nil {
			t.Fatal(err)
		}
		for _, turn := range x.Turns {
			if turn.ID != receipt.ResourceID {
				continue
			}
			if turn.State == pa.TurnWaitingPermission {
				t.Fatal("native permission requires a human; no answer was sent")
			}
			if turn.State == pa.TurnFailed || turn.State == pa.TurnCancelled || turn.State == pa.TurnUnknown {
				t.Fatalf("native outcome %s: %v", turn.State, turn.Failure)
			}
			if turn.State != pa.TurnSucceeded || x.Conversation.ActiveTurnID != "" {
				continue
			}
			x = qualificationReopen(t, f, b.Key)
			var users, nonces, mentions, accepted int
			for _, m := range x.Messages {
				if m.TurnID != turn.ID {
					continue
				}
				if m.Kind == pa.UserMessage {
					users++
					acceptedFence := inputFence
					// The first admission creates the initial binding, before
					// execution advances it again for the native process.
					if inputFence.BindingID == "" {
						acceptedFence.BindingID = m.Fence.BindingID
						acceptedFence.Generation = 1
					}
					if m.Text != text || m.Fence != acceptedFence || m.Fence.BindingID == "" {
						t.Fatal("accepted user input or fence changed")
					}
					created := 0
					for _, event := range x.Events {
						if event.Kind == pa.MessageCreated && event.Message != nil && event.Message.ID == m.ID && event.Fence == m.Fence {
							created++
						}
					}
					if created != 1 {
						t.Fatal("accepted user fence lacks its durable event")
					}
				}
				if m.Kind == pa.AssistantMessage && m.Fence != turn.Fence {
					t.Fatal("assistant execution fence mismatch", m.Fence, turn.Fence)
				}
				if m.Kind == pa.AssistantMessage && m.Text == expected {
					nonces++
				}
				if m.Kind == pa.AssistantMessage {
					mentions += strings.Count(m.Text, strings.Fields(expected)[0])
				}
			}
			for _, r := range x.Receipts {
				if r.ID == receipt.ID && r.Command.Key == b.Key && r.ResourceID == turn.ID {
					accepted++
				}
			}
			if turn.Fence.Key != b.Key || turn.Fence.Project != b.Project || users != 1 || nonces != 1 || mentions != 1 || accepted != 1 {
				t.Fatalf("durable scope/count mismatch: users=%d nonce=%d receipt=%d", users, nonces, accepted)
			}
			return x
		}
		if time.Now().After(deadline) {
			t.Fatal("native deadline; no retry permitted")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

type qualificationOpen struct {
	Request  app.OpenRequest
	NativeID string
	Error    string
}
type qualificationRead struct {
	Fence   pa.Fence
	Entries []pa.MemoryEntry
	Error   string
}
type qualificationSend struct {
	Fence           pa.Fence
	ID, Text, Error string
}
type qualificationProvider struct {
	app.Provider
	mu       sync.Mutex
	writeMu  sync.Mutex
	opened   map[string]bool
	opens    []qualificationOpen
	prompts  []app.PromptRequest
	reads    []qualificationRead
	sends    []qualificationSend
	evidence string
}

func (p *qualificationProvider) persist() {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	b, err := json.MarshalIndent(p.snapshot(), "", "  ")
	if err == nil {
		err = os.WriteFile(p.evidence+".tmp", append(b, '\n'), 0600)
	}
	if err == nil {
		_ = os.Rename(p.evidence+".tmp", p.evidence)
	}
}

func (p *qualificationProvider) snapshot() json.RawMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	b, _ := json.Marshal(struct {
		Opens   []qualificationOpen
		Prompts []app.PromptRequest
		Reads   []qualificationRead
		Sends   []qualificationSend
	}{p.opens, p.prompts, p.reads, p.sends})
	return b
}
func (p *qualificationProvider) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	p.mu.Lock()
	key := q.Bot.Key.BotID + "/" + q.Binding.Fence.BindingID
	if p.opened[key] || len(p.opens) >= 4 {
		p.mu.Unlock()
		return nil, failure(pa.ExecutionEffectUnknown, "qualification_no_retry")
	}
	p.opened[key] = true
	i := len(p.opens)
	p.opens = append(p.opens, qualificationOpen{Request: q})
	p.mu.Unlock()
	p.persist()
	q.Tools = &qualificationTools{NativeTools: q.Tools, p: p, fence: q.Binding.Fence}
	s, err := p.Provider.Open(ctx, q, sink)
	p.mu.Lock()
	if s != nil {
		p.opens[i].NativeID = s.SessionID()
	}
	if err != nil {
		p.opens[i].Error = err.Error()
	}
	p.mu.Unlock()
	p.persist()
	if s == nil {
		return nil, err
	}
	return &qualificationSession{ProviderSession: s, p: p}, err
}

type qualificationSession struct {
	app.ProviderSession
	p *qualificationProvider
}

func (s *qualificationSession) Send(ctx context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	s.p.mu.Lock()
	if len(s.p.prompts) >= 4 {
		s.p.mu.Unlock()
		return nil, failure(pa.ExecutionEffectUnknown, "qualification_prompt_cap")
	}
	s.p.prompts = append(s.p.prompts, q)
	s.p.mu.Unlock()
	s.p.persist()
	return s.ProviderSession.Send(ctx, q)
}

type qualificationTools struct {
	app.NativeTools
	p     *qualificationProvider
	fence pa.Fence
}

func (w *qualificationTools) Memory(ctx context.Context) ([]pa.MemoryEntry, error) {
	entries, err := w.NativeTools.Memory(ctx)
	r := qualificationRead{Fence: w.fence, Entries: entries}
	if err != nil {
		r.Error = err.Error()
	}
	w.p.mu.Lock()
	w.p.reads = append(w.p.reads, r)
	w.p.mu.Unlock()
	w.p.persist()
	return entries, err
}
func (w *qualificationTools) SendMessage(ctx context.Context, id, text string) error {
	err := w.NativeTools.SendMessage(ctx, id, text)
	r := qualificationSend{Fence: w.fence, ID: id, Text: text}
	if err != nil {
		r.Error = err.Error()
	}
	w.p.mu.Lock()
	w.p.sends = append(w.p.sends, r)
	w.p.mu.Unlock()
	w.p.persist()
	return err
}
func (w *qualificationTools) Routines(ctx context.Context, q app.RoutineRequest) (app.RoutineResult, error) {
	return w.NativeTools.(app.RoutineTools).Routines(ctx, q)
}
func (w *qualificationTools) PublishOutput(ctx context.Context, id string, q app.OutputRequest) (pa.Output, error) {
	return w.NativeTools.(app.OutputTools).PublishOutput(ctx, id, q)
}
func (w *qualificationTools) ListArtifacts(ctx context.Context) ([]app.ArtifactInfo, error) {
	return w.NativeTools.(app.OutputTools).ListArtifacts(ctx)
}
