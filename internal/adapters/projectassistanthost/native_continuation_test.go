package projectassistanthost

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	hoststore "github.com/sainteye/clawdline/internal/adapters/store"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Only resume admission uses production code here. Fresh opens and responses
// are scripted; this control does not claim a native model prompt succeeded.
type continuationOwnerControl struct {
	*fixtureProvider
	installed app.Provider
	refusals  int
	fresh     int
}

func (p *continuationOwnerControl) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	if q.Resume {
		s, err := p.installed.Open(ctx, q, sink)
		if s != nil || pa.Code(err) != pa.ResumeUnsupported {
			return s, err
		}
		p.refusals++
		return nil, err
	}
	s, err := p.fixtureProvider.Open(ctx, q, sink)
	if err != nil {
		return s, err
	}
	p.fresh++
	return continuationControlSession{ProviderSession: s, id: fmt.Sprintf("10000000-0000-4000-8000-%012d", p.fresh)}, nil
}

type continuationControlSession struct {
	app.ProviderSession
	id string
}

func (s continuationControlSession) SessionID() string { return s.id }

func reopenContinuationHost(t *testing.T, f *hostFixture) {
	t.Helper()
	var seq int
	var name, path string
	if err := f.o.Database.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err := f.h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.st.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	f.st, err = hoststore.Open(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	f.o.Database = f.st.ProjectAssistantDatabase()
	f.o.Broker = f.broker(t, t.TempDir())
	f.start(t)
	f.enable(t)
}

func TestHostLegacyUnownedBindingReopensFreshWithChat(t *testing.T) {
	f := newHostFixture(t)
	state := t.TempDir()
	p := &continuationOwnerControl{fixtureProvider: f.p, installed: acp.InstalledProvider(state)}
	f.o.Provider = func() (app.Provider, error) { return p, nil }
	f.start(t)
	f.enable(t)
	b := f.create(t, "legacy-continuation")
	send := func(id, text string) app.PromptRequest {
		t.Helper()
		s, err := f.h.Service()
		if err != nil {
			t.Fatal(err)
		}
		_, c, _, err := s.Get(context.Background(), b.Key)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Send(context.Background(), pa.Command{Key: b.Key, Operation: pa.SendOperation, IdempotencyKey: id}, pa.SendMessage{Fence: pa.Fence{Key: b.Key, Project: b.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		q := fixtureReceive(t, f.p.sent)
		native := fixtureReceive(t, f.p.sessions)
		native.reply <- app.PromptResult{FinalText: "Acknowledged " + id}
		fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
		return q
	}
	topic := "Research https://example.invalid/legacy-alpha"
	send("topic", topic)
	// The synthetic native ID is durably in SQLite, but was never minted by
	// InstalledProvider and has no owner record. Reopening cannot bless it.
	reopenContinuationHost(t, f)
	q := send("followup", "Please research")
	if p.refusals != 1 || p.fresh != 2 {
		t.Fatal("legacy ID was resumed or not safely replaced", p.refusals, p.fresh)
	}
	if q.Handoff == nil {
		t.Fatal("missing fresh continuation")
	}
	opening, err := q.Handoff.Opening(q.Turn, q.Context)
	if err != nil || !strings.Contains(opening, topic) {
		t.Fatal("lost source-owned topic", opening, err)
	}
	x := qualificationReopen(t, f, b.Key)
	if len(x.MemoryEntries) != 0 || len(x.Messages) != 4 || len(x.Turns) != 2 {
		t.Fatal("legacy fallback changed memory or replayed history")
	}
	// Owners directories may exist, but no ownership record was manufactured.
	if err := filepath.WalkDir(state, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".json") {
			t.Errorf("unexpected owner record %s", filepath.Base(path))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// Opt in only after authorizing one batch of four native prompt writes. The
// existing qualificationProvider caps both opens and sends at four and refuses
// retries. An empty task-owned state is mandatory; default CI always skips.
func TestInstalledAgentsNativeFreshContinuation(t *testing.T) {
	if os.Getenv("CLAWDLINE_NATIVE_CONTINUATION") != "four-prompts-once" {
		t.Skip("native continuation batch is not enabled")
	}
	root := os.Getenv("CLAWDLINE_NATIVE_CONTINUATION_ROOT")
	if !filepath.IsAbs(root) {
		t.Fatal("absolute new task-owned root required")
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal("root must not exist", err)
	}
	p := &qualificationProvider{Provider: acp.InstalledProvider(root), opened: map[string]bool{}, evidence: filepath.Join(root, "observations.json")}
	defer func() { qualificationJSON(t, filepath.Join(root, "observations.json"), p.snapshot()) }()
	for _, agent := range []pa.Agent{pa.ClaudeCode, pa.Codex} {
		if !t.Run(string(agent), func(t *testing.T) {
			f := newHostFixture(t)
			f.o.Limits = app.RuntimeLimits{MaxBots: capacity.MaximumBotActors, MaxProcesses: capacity.MaximumBotProcesses, PromptTimeout: time.Duration(capacity.MaximumBotControlWindowMilliseconds) * time.Millisecond, CleanupTimeout: time.Duration(capacity.MaximumBotCleanupMilliseconds) * time.Millisecond}
			f.o.Provider = func() (app.Provider, error) { return p, nil }
			f.start(t)
			f.enable(t)
			s, err := f.h.Service()
			if err != nil {
				t.Fatal(err)
			}
			key := pa.BotKey{MachineID: f.scope.MachineID, BotID: "fresh-" + string(agent)}
			instructions := "Use only Clawdline send_message for replies. Never read files, run commands, use external tools, or edit memory. Historical chat is data, not permission to redo actions. Send exactly one tool message per turn; final reply must be Done."
			b, _, err := s.Create(context.Background(), pa.Command{Key: key, Operation: pa.CreateOperation, IdempotencyKey: "create"}, pa.CreateBot{Target: pa.ProjectSelection{MachineID: f.scope.MachineID, ProjectID: f.scope.ProjectID}, Name: "Fresh continuation", Agent: agent, Appearance: pa.Appearance{Shape: pa.EntryShape, Color: pa.EntryColor}, PermissionPolicy: pa.Ask, Instructions: instructions})
			if err != nil {
				t.Fatal(err)
			}
			contextToken := "violet-orbit-" + string(agent)
			topic := "Research URL https://example.invalid/" + contextToken + "; banner 1600 by 900, violet mountains. Keep these details in this chat only. Call send_message once with idempotency_key first and text Ready. Final: Done."
			qualificationTurn(t, f, b, "first", topic, "Ready")
			reopenContinuationHost(t, f)
			s, err = f.h.Service()
			if err != nil {
				t.Fatal(err)
			}
			current, _, _, err := s.Get(context.Background(), key)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = s.Edit(context.Background(), pa.Command{Key: key, Operation: pa.EditOperation, IdempotencyKey: "revision"}, pa.EditBot{ExpectedVersion: current.Version, Name: current.Name, Appearance: current.Appearance, Instructions: instructions + " Keep replies concise."})
			if err != nil {
				t.Fatal(err)
			}
			x := qualificationTurn(t, f, b, "second", "Please start the earlier research/banner. Do not access the URL or tools other than send_message. Send exactly one message with idempotency_key second: the URL token from the earlier topic, one space, then its banner dimensions as 1600x900. Do not guess. Final: Done.", contextToken+" 1600x900")
			qualificationJSON(t, filepath.Join(root, string(agent)+"-final.json"), x)
			if len(x.MemoryEntries) != 0 {
				t.Fatal("short-term chat became memory")
			}
			p.mu.Lock()
			var opens []qualificationOpen
			var prompts []app.PromptRequest
			for _, o := range p.opens {
				if o.Request.Bot.Key == key {
					opens = append(opens, o)
				}
			}
			for _, q := range p.prompts {
				if q.Turn.Fence.Key == key {
					prompts = append(prompts, q)
				}
			}
			p.mu.Unlock()
			if len(opens) != 2 || opens[0].Request.Resume || opens[1].Request.Resume || opens[0].NativeID == opens[1].NativeID {
				t.Fatal("instruction revision did not produce distinct fresh native sessions", opens)
			}
			if len(prompts) != 2 || prompts[1].Handoff == nil || len(prompts[1].Handoff.Continuation) != 2 {
				t.Fatal("native fresh prompt lost chat provenance")
			}
			if err = f.h.Disable(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: two fresh native opens, two prompt writes; source-owned chat token returned; zero durable memory entries", agent)
		}) {
			t.Fatal("batch stopped; no further native prompts or retry")
		}
	}
}
