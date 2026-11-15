package http

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestProjectAssistantFallbackHTTPConfiguration(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	raw, _ := json.Marshal(f.create())
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	body["settings"].(map[string]any)["fallback_agents"] = []string{"claude"}
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, body))
}

type fallbackHTTPSession struct {
	send  func(app.PromptRequest) (app.PendingPrompt, error)
	close func() error
}

func (*fallbackHTTPSession) SessionID() string { return "scripted-session" }
func (s *fallbackHTTPSession) Send(_ context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	return s.send(q)
}
func (*fallbackHTTPSession) Answer(context.Context, string, string) error { return nil }
func (*fallbackHTTPSession) Cancel(context.Context) error                 { return nil }
func (s *fallbackHTTPSession) Close(context.Context) error {
	if s.close != nil {
		return s.close()
	}
	return nil
}

type fallbackHTTPPending struct{ err error }

func (p fallbackHTTPPending) Wait(context.Context) (app.PromptResult, error) {
	return app.PromptResult{FinalText: "Scripted final answer"}, p.err
}

func fallbackUsageRefusal() error {
	return &app.PreSendRefusal{Failure: &app.ProviderFailure{Reason: app.ProviderUsageExhausted, Cause: &pa.Failure{Code: pa.Unsupported, Field: "scripted_usage"}}}
}

func fallbackHTTPCreate(t *testing.T, f *paServerFixture) {
	t.Helper()
	q := f.create()
	q.Settings.Agent = contract.ProjectAssistantAgentClaude
	q.Settings.FallbackAgents = []contract.ProjectAssistantAgent{contract.ProjectAssistantAgentCodex}
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, q))
}
func fallbackHTTPWait(t *testing.T, f *paServerFixture) app.Export {
	t.Helper()
	paResilienceAwait(t, func() bool { return f.s.projectAssistants.host.Usage().ActiveActors == 0 })
	x, err := store.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestProjectAssistantFallbackHTTPKeepsLogicalTurnAndDurableRest(t *testing.T) {
	f := newPAServerFixture(t, true)
	var now atomic.Int64
	now.Store(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).UnixNano())
	f.s.projectAssistants.clock = func() time.Time { return time.Unix(0, now.Load()).UTC() }
	var mu sync.Mutex
	var opens []app.OpenRequest
	var deadlines []time.Time
	var prompts []app.PromptRequest
	var oldTools app.NativeTools
	closed := false
	var factories atomic.Int32
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		factories.Add(1)
		return paServerProvider{open: func(ctx context.Context, q app.OpenRequest) (app.ProviderSession, error) {
			mu.Lock()
			defer mu.Unlock()
			opens = append(opens, q)
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Error("missing logical-turn deadline")
			}
			deadlines = append(deadlines, deadline)
			if len(opens) == 2 && !deadlines[0].Equal(deadlines[1]) {
				t.Error("fallback restarted the prompt deadline")
			}
			if len(opens) == 2 && f.s.projectAssistants.host.Usage().Processes != 1 {
				t.Error("fallback acquired a second process slot")
			}
			s := &fallbackHTTPSession{send: func(p app.PromptRequest) (app.PendingPrompt, error) {
				mu.Lock()
				defer mu.Unlock()
				prompts = append(prompts, p)
				if len(prompts) == 2 {
					now.Add(int64(time.Duration(pa.MaxFallbackUsageRestSeconds+1) * time.Second))
				}
				return fallbackHTTPPending{}, nil
			}}
			if q.Bot.Agent == pa.ClaudeCode && now.Load() == time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).UnixNano() {
				oldTools = q.Tools
				s.close = func() error { mu.Lock(); defer mu.Unlock(); closed = true; return nil }
				return s, fallbackUsageRefusal()
			}
			if q.Bot.Agent == pa.Codex && !closed {
				t.Error("alternate opened before confirmed primary Close")
			}
			return s, nil
		}}, nil
	}
	f.enable(t)
	fallbackHTTPCreate(t, f)
	initial := f.get(t, f.local)
	paServerOK(t, f.send(t, f.local, "first"))
	x := fallbackHTTPWait(t, f)
	if len(x.Turns) != 1 || x.Turns[0].State != pa.TurnSucceeded || x.Bot.Agent != pa.ClaudeCode || x.Conversation.ID != initial.Conversation.ID {
		t.Fatalf("identity/result: %+v", x.Turns)
	}
	first := x.Turns[0]
	if len(first.Attempts) != 6 || !pa.SafeAttemptClosed(pa.Turn{Attempts: first.Attempts[:3]}) || first.Attempts[3].Agent != pa.Codex || first.Attempts[3].Fence.Generation <= first.Attempts[0].Fence.Generation {
		t.Fatalf("attempt ledger: %+v", first.Attempts)
	}
	counts := map[pa.MessageKind]int{}
	for _, m := range x.Messages {
		counts[m.Kind]++
	}
	if counts[pa.UserMessage] != 1 || counts[pa.AssistantMessage] != 1 || counts[pa.NoticeMessage] != 1 {
		t.Fatalf("messages: %+v", counts)
	}
	if len(prompts) != 1 || prompts[0].Turn.ID != first.ID || prompts[0].Turn.MessageID != first.MessageID || prompts[0].Handoff.Agent != pa.Codex || prompts[0].Turn.Fence.Key != x.Bot.Key || prompts[0].Turn.Fence.Project != x.Bot.Project {
		t.Fatal("alternate lost logical input, scope or effective handoff")
	}
	if err := oldTools.SendMessage(context.Background(), "late", "must be refused"); pa.Code(err) != pa.Revoked {
		t.Fatalf("old tools remain live: %v", err)
	}
	repeat := contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, "first"), Text: "Synthetic first input", Fence: contract.ProjectAssistantFence{Key: initial.Bot.Key, Project: initial.Bot.Project, ConversationID: initial.Conversation.ID, BindingID: initial.Conversation.BindingID, Generation: initial.Conversation.Generation}}
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/messages", f.local, repeat))
	if len(fallbackHTTPWait(t, f).Turns) != 1 || len(prompts) != 1 {
		t.Fatal("receipt replay sent the accepted input again")
	}
	if factories.Load() != 1 {
		t.Fatalf("provider factories: %d", factories.Load())
	}
	archiveDB, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "restore.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = archiveDB.Close() })
	if err = (store.Installer{}).Install(context.Background(), archiveDB); err != nil {
		t.Fatal(err)
	}
	archiveStore := store.New(archiveDB)
	newer := x
	newer.SchemaVersion++
	if err = archiveStore.Restore(context.Background(), newer); pa.Code(err) != pa.Unsupported {
		t.Fatalf("newer schema accepted: %v", err)
	}
	historical := x
	historical.Bot.FallbackAgents = pa.FallbackConfig{}
	if err = archiveStore.Restore(context.Background(), historical); err != nil {
		t.Fatalf("legitimate historical multi-Agent snapshot refused: %v", err)
	}
	rest, err := store.New(f.s.store.ProjectAssistantDatabase()).AgentRest(context.Background(), f.key.MachineID, pa.ClaudeCode)
	if err != nil || rest == nil || rest.Until.Sub(time.Unix(0, now.Load())) != time.Duration(pa.MaxFallbackUsageRestSeconds)*time.Second {
		t.Fatalf("rest: %+v %v", rest, err)
	}
	reading := f.s.projectAssistantCapacityMeasures()[capacity.BotsAgentRestSeconds]()
	if !reading.Known || reading.Used != pa.MaxFallbackUsageRestSeconds {
		t.Fatalf("actual rest measurement: %+v", reading)
	}
	// The real HTTP read exposes the ledger; there is no private config map.
	w := f.request(t, "GET", "/v1/project-assistants/"+f.key.BotID+"/export?machine_id="+f.key.MachineID, f.local, nil)
	paServerOK(t, w)
	var exported map[string]any
	if err = json.Unmarshal(w.Body.Bytes(), &exported); err != nil {
		t.Fatal(err)
	}
	// Reopen the actual host and SQLite database before the next input. The
	// rest belongs to the machine store, not the old engine's actor map.
	providerFactory, clock := f.s.projectAssistants.provider, f.s.projectAssistants.clock
	if err = f.s.StopProjectAssistants(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(f.s.cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.StopProjectAssistants(context.Background()); _ = restarted.store.Close() })
	restarted.projectAssistants.provider, restarted.projectAssistants.clock = providerFactory, clock
	f.s, f.h = restarted, restarted.Handler()
	paServerOK(t, f.send(t, f.local, "second"))
	x = fallbackHTTPWait(t, f)
	notices := 0
	for _, m := range x.Messages {
		if m.Kind == pa.NoticeMessage {
			notices++
		}
	}
	if notices != 1 {
		t.Fatal("unchanged effective Agent repeated the switch notice")
	}
	if len(opens) != 3 || opens[2].Bot.Agent != pa.Codex || len(prompts) != 2 {
		t.Fatalf("persisted rest ignored: opens=%d prompts=%d", len(opens), len(prompts))
	}
	// The fake rest clock expired during the second Codex Send, not between turns.
	paServerOK(t, f.send(t, f.local, "third"))
	x = fallbackHTTPWait(t, f)
	if len(opens) != 4 || opens[3].Bot.Agent != pa.ClaudeCode || len(prompts) != 3 || len(x.Turns) != 3 {
		t.Fatalf("expiry did not return primary on next message: %+v", opens)
	}
	if got := f.get(t, f.local); got.Bot.Agent != contract.ProjectAssistantAgentClaude || len(got.Bot.FallbackAgents) != 1 {
		t.Fatal("primary configuration mutated")
	}
	t.Logf("HTTP create/send/get/export: primary Opens=2 Sends=1; Codex Opens=2 Sends=2; first logical turn=%s attempts=%d; 3 accepted user inputs, 3 final answers", first.ID, len(first.Attempts))
}

func TestProjectAssistantFallbackHTTPConfigAuthorityAndLegacyEdits(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	fallbackHTTPCreate(t, f)
	edit := func(id string) contract.ProjectAssistantEditRequest {
		bot := f.get(t, f.local).Bot
		return contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, id), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: bot.Version, Name: bot.Name, Appearance: bot.Appearance, Instructions: bot.Instructions.Text}}
	}
	route := "/v1/project-assistants/" + f.key.BotID
	legacy := edit("legacy")
	paServerOK(t, f.request(t, "PATCH", route, f.local, legacy))
	if got := f.get(t, f.local); got.Bot.FallbackAgents == nil || len(got.Bot.FallbackAgents) != 1 {
		t.Fatal("old client cleared saved fallback")
	}
	readonly := edit("readonly")
	readonly.Settings.FallbackAgents = []contract.ProjectAssistantAgent{}
	if w := f.request(t, "PATCH", route, f.readonly, readonly); w.Code != 403 {
		t.Fatalf("read-only configuration accepted: %d %s", w.Code, w.Body)
	}
	stale := edit("stale")
	stale.Settings.ExpectedVersion--
	paAssertHTTPFailure(t, f.request(t, "PATCH", route, f.local, stale), 409, pa.StaleVersion)
	foreign := edit("foreign")
	foreign.Command.Key.MachineID = "foreign"
	paAssertHTTPFailure(t, f.request(t, "PATCH", route, f.local, foreign), 403, pa.ScopeDenied)
	duplicate := edit("duplicate")
	duplicate.Settings.FallbackAgents = []contract.ProjectAssistantAgent{contract.ProjectAssistantAgentClaude}
	paAssertHTTPFailure(t, f.request(t, "PATCH", route, f.local, duplicate), 400, pa.Invalid)
	for _, field := range []string{`"fallback_agents":null,`, `"fallback_agents":"codex",`, `"FALLBACK_AGENTS":[],`, `"fallback_agents":[],"fallback_agents":[],`} {
		raw, _ := json.Marshal(edit("malformed-" + field))
		body := strings.Replace(string(raw), `"name":`, field+`"name":`, 1)
		paAssertHTTPFailure(t, f.request(t, "PATCH", route, f.local, body), 400, pa.Invalid)
	}
	// A raw empty array clears; omitting the key above preserved it.
	clear := edit("clear")
	raw, _ := json.Marshal(clear)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	body["settings"].(map[string]any)["fallback_agents"] = []string{}
	paServerOK(t, f.request(t, "PATCH", route, f.local, body))
	if got := f.get(t, f.local); len(got.Bot.FallbackAgents) != 0 {
		t.Fatal("explicit empty array did not clear")
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("configuration constructed a provider")
	}
}

func TestProjectAssistantFallbackHTTPTransitionFailureRemainsUnknown(t *testing.T) {
	for _, boundary := range []string{"refusal", "closed", "alternate-start", "send"} {
		t.Run(boundary, func(t *testing.T) {
			f := newPAServerFixture(t, true)
			var primaryOpens, alternateOpens, sends atomic.Int32
			f.s.projectAssistants.provider = func() (app.Provider, error) {
				return paServerProvider{open: func(_ context.Context, q app.OpenRequest) (app.ProviderSession, error) {
					session := &fallbackHTTPSession{send: func(app.PromptRequest) (app.PendingPrompt, error) { sends.Add(1); return fallbackHTTPPending{}, nil }}
					if q.Bot.Agent == pa.ClaudeCode {
						primaryOpens.Add(1)
						return session, fallbackUsageRefusal()
					}
					alternateOpens.Add(1)
					return session, nil
				}}, nil
			}
			f.enable(t)
			fallbackHTTPCreate(t, f)
			condition := map[string]string{"refusal": "NEW.command_key LIKE 'runtime-refused-%'", "closed": "NEW.command_key LIKE 'runtime-attempt-%-3'", "alternate-start": "NEW.command_key LIKE 'runtime-start-%' AND EXISTS(SELECT 1 FROM project_assistant_receipts WHERE command_key LIKE 'runtime-refused-%')", "send": "NEW.command_key LIKE 'runtime-attempt-%-5'"}[boundary]
			_, err := f.s.store.ProjectAssistantDatabase().Exec(`CREATE TRIGGER fallback_receipt_failure BEFORE INSERT ON project_assistant_receipts WHEN ` + condition + ` BEGIN SELECT RAISE(ABORT,'scripted receipt failure'); END`)
			if err != nil {
				t.Fatal(err)
			}
			paServerOK(t, f.send(t, f.local, "boundary"))
			x := fallbackHTTPWait(t, f)
			if len(x.Turns) != 1 || x.Turns[0].State != pa.TurnUnknown || sends.Load() != 0 || primaryOpens.Load() != 1 {
				t.Fatalf("boundary replayed: %+v sends=%d", x.Turns, sends.Load())
			}
			wantAlternate := int32(0)
			if boundary == "send" {
				wantAlternate = 1
			}
			if alternateOpens.Load() != wantAlternate {
				t.Fatalf("alternate Opens=%d want %d", alternateOpens.Load(), wantAlternate)
			}
			_, err = f.s.store.ProjectAssistantDatabase().Exec(`DROP TRIGGER fallback_receipt_failure`)
			if err != nil {
				t.Fatal(err)
			}
			// Recovery sees quota in the persisted ledger but must not advance it.
			factory := f.s.projectAssistants.provider
			if err = f.s.StopProjectAssistants(context.Background()); err != nil {
				t.Fatal(err)
			}
			restarted, err := New(f.s.cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = restarted.StopProjectAssistants(context.Background()); _ = restarted.store.Close() })
			restarted.projectAssistants.provider = factory
			f.s, f.h = restarted, restarted.Handler()
			paServerOK(t, f.request(t, "GET", routeForFallbackExport(f), f.local, nil))
			if err = restarted.projectAssistants.host.Recover(context.Background(), f.key); err != nil {
				t.Fatal(err)
			}
			if primaryOpens.Load() != 1 || alternateOpens.Load() != wantAlternate || sends.Load() != 0 {
				t.Fatal("recovery restarted a provider")
			}
		})
	}
}
func routeForFallbackExport(f *paServerFixture) string {
	return "/v1/project-assistants/" + f.key.BotID + "/export?machine_id=" + f.key.MachineID
}

func TestProjectAssistantFallbackHTTPRechecksBeforeAlternate(t *testing.T) {
	for _, change := range []string{"configuration", "forget", "stop"} {
		t.Run(change, func(t *testing.T) {
			f := newPAServerFixture(t, true)
			closing, release := make(chan struct{}), make(chan struct{})
			var alternateOpens, sends atomic.Int32
			f.s.projectAssistants.provider = func() (app.Provider, error) {
				return paServerProvider{open: func(_ context.Context, q app.OpenRequest) (app.ProviderSession, error) {
					session := &fallbackHTTPSession{send: func(app.PromptRequest) (app.PendingPrompt, error) { sends.Add(1); return fallbackHTTPPending{}, nil }}
					if q.Bot.Agent == pa.Codex {
						alternateOpens.Add(1)
						return session, nil
					}
					session.close = func() error { close(closing); <-release; return nil }
					return session, fallbackUsageRefusal()
				}}, nil
			}
			f.enable(t)
			fallbackHTTPCreate(t, f)
			paServerOK(t, f.send(t, f.local, "recheck"))
			select {
			case <-closing:
			case <-time.After(time.Duration(capacity.MaximumBotCleanupMilliseconds) * time.Millisecond):
				t.Fatal("primary did not reach cleanup")
			}
			bot := f.get(t, f.local)
			route := "/v1/project-assistants/" + f.key.BotID
			switch change {
			case "configuration":
				q := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, "change"), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: bot.Bot.Version, Name: bot.Bot.Name, Appearance: bot.Bot.Appearance, Instructions: bot.Bot.Instructions.Text}}
				raw, _ := json.Marshal(q)
				var body map[string]any
				_ = json.Unmarshal(raw, &body)
				body["settings"].(map[string]any)["fallback_agents"] = []string{}
				paServerOK(t, f.request(t, "PATCH", route, f.local, body))
			case "forget":
				paServerOK(t, f.request(t, "POST", route+"/memory/forget", f.local, contract.ProjectAssistantForgetRequest{Command: f.command(pa.ForgetOperation, "forget"), Project: bot.Bot.Project, ExpectedRevision: bot.Conversation.MemoryRevision, SourceIds: []string{"scripted-forgotten-source"}}))
			case "stop":
				paServerOK(t, f.request(t, "POST", route+"/stop", f.local, contract.ProjectAssistantStopRequest{Command: f.command(pa.StopOperation, "stop"), Fence: contract.ProjectAssistantFence{Key: bot.Bot.Key, Project: bot.Bot.Project, ConversationID: bot.Conversation.ID, BindingID: bot.Conversation.BindingID, Generation: bot.Conversation.Generation}}))
			}
			close(release)
			x := fallbackHTTPWait(t, f)
			if alternateOpens.Load() != 0 || sends.Load() != 0 || len(x.Turns) != 1 || x.Turns[0].State == pa.TurnSucceeded {
				t.Fatalf("changed authority/context sent an alternate: %+v opens=%d sends=%d", x.Turns, alternateOpens.Load(), sends.Load())
			}
		})
	}
}

func TestProjectAssistantFallbackHTTPUnknownNeverOpensAlternate(t *testing.T) {
	for _, stage := range []string{"typed-only", "missing-handle", "send", "wait", "cleanup", "open-effect"} {
		t.Run(stage, func(t *testing.T) {
			f := newPAServerFixture(t, true)
			var primary, alternate, sends atomic.Int32
			var retained app.NativeTools
			f.s.projectAssistants.provider = func() (app.Provider, error) {
				return paServerProvider{open: func(ctx context.Context, q app.OpenRequest) (app.ProviderSession, error) {
					if q.Bot.Agent == pa.Codex {
						alternate.Add(1)
					} else {
						primary.Add(1)
					}
					retained = q.Tools
					s := &fallbackHTTPSession{send: func(app.PromptRequest) (app.PendingPrompt, error) {
						sends.Add(1)
						if stage == "send" {
							return nil, fallbackUsageRefusal()
						}
						return fallbackHTTPPending{err: fallbackUsageRefusal()}, nil
					}}
					switch stage {
					case "typed-only":
						return s, &app.ProviderFailure{Reason: app.ProviderUsageExhausted, Cause: &pa.Failure{Code: pa.Unsupported}}
					case "missing-handle":
						return nil, fallbackUsageRefusal()
					case "cleanup":
						s.close = func() error { return errors.New("scripted cleanup not confirmed") }
						return s, fallbackUsageRefusal()
					case "open-effect":
						_ = q.Tools.SendMessage(ctx, "open-effect", "Scripted early effect")
						return s, fallbackUsageRefusal()
					default:
						return s, nil
					}
				}}, nil
			}
			f.enable(t)
			fallbackHTTPCreate(t, f)
			paServerOK(t, f.send(t, f.local, "uncertain"))
			if stage == "missing-handle" || stage == "cleanup" {
				paResilienceAwait(t, func() bool {
					x, e := store.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
					return e == nil && len(x.Turns) == 1 && x.Turns[0].State == pa.TurnUnknown
				})
			} else {
				fallbackHTTPWait(t, f)
			}
			if primary.Load() != 1 || alternate.Load() != 0 {
				t.Fatalf("unknown was replayed: primary=%d alternate=%d", primary.Load(), alternate.Load())
			}
			expected := int32(0)
			if stage == "send" || stage == "wait" {
				expected = 1
			}
			if sends.Load() != expected {
				t.Fatalf("Send count %d want %d", sends.Load(), expected)
			}
			if retained != nil {
				if err := retained.SendMessage(context.Background(), "late", "late"); pa.Code(err) != pa.Revoked {
					t.Fatalf("old tools: %v", err)
				}
			}
			if stage == "missing-handle" || stage == "cleanup" {
				u := f.s.projectAssistants.host.Usage()
				x, e := store.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
				if e != nil {
					t.Fatal(e)
				}
				last := x.Turns[0].Attempts[len(x.Turns[0].Attempts)-1]
				if last.CleanupConfirmed {
					t.Fatal("failed/missing cleanup was marked confirmed")
				}
				if u.Processes != 1 || u.Known {
					t.Fatalf("unknown cleanup owner discarded: %+v", u)
				}
			}
		})
	}
}

// The helper is an owned OS process, not an installed vendor Agent. Both
// protocol writes and process reaping run under the real Host's named budgets.
func TestProjectAssistantFallbackProcessHelper(t *testing.T) {
	mode := os.Getenv("CLAWDLINE_FALLBACK_HELPER")
	if mode == "" {
		return
	}
	if mode == "refuse" {
		fmt.Println("usage_limit")
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	fmt.Println("ready")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		fmt.Println("Scripted OS answer")
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

type fallbackOSSession struct {
	cmd    *exec.Cmd
	input  io.WriteCloser
	output *bufio.Reader
	sends  *atomic.Int32
	closed *atomic.Bool
}

func (s *fallbackOSSession) SessionID() string {
	return fmt.Sprintf("scripted-os-%d", s.cmd.Process.Pid)
}
func (s *fallbackOSSession) Send(_ context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	s.sends.Add(1)
	raw, err := json.Marshal(q)
	if err != nil {
		return nil, err
	}
	_, err = s.input.Write(append(raw, '\n'))
	return fallbackOSPending{s.output}, err
}
func (*fallbackOSSession) Answer(context.Context, string, string) error { return nil }
func (*fallbackOSSession) Cancel(context.Context) error                 { return nil }
func (s *fallbackOSSession) Close(ctx context.Context) error {
	_ = s.input.Close()
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			s.closed.Store(true)
		}
		return err
	case <-ctx.Done():
		_ = s.cmd.Process.Kill()
		<-done
		return ctx.Err()
	}
}

type fallbackOSPending struct{ output *bufio.Reader }

func (p fallbackOSPending) Wait(context.Context) (app.PromptResult, error) {
	line, err := p.output.ReadString('\n')
	return app.PromptResult{FinalText: line}, err
}

func TestProjectAssistantFallbackHTTPWithOwnedOSProcesses(t *testing.T) {
	f := newPAServerFixture(t, true)
	limits := app.RuntimeLimits{MaxBots: capacity.MaximumBotActors, MaxProcesses: capacity.MaximumBotProcesses, PromptTimeout: time.Duration(capacity.MaximumBotPromptMilliseconds) * time.Millisecond, CleanupTimeout: time.Duration(capacity.MaximumBotCleanupMilliseconds) * time.Millisecond}
	f.s.projectAssistants.limits = &limits
	var primarySends, alternateSends atomic.Int32
	var primaryClosed, alternateClosed atomic.Bool
	var processIDs []int
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		return paServerProvider{open: func(ctx context.Context, q app.OpenRequest) (app.ProviderSession, error) {
			executable, err := os.Executable()
			if err != nil {
				return nil, err
			}
			mode := "answer"
			sends, closed := &alternateSends, &alternateClosed
			if q.Bot.Agent == pa.ClaudeCode {
				mode = "refuse"
				sends, closed = &primarySends, &primaryClosed
			} else if !primaryClosed.Load() {
				return nil, errors.New("primary process not reaped")
			}
			command := exec.CommandContext(ctx, executable, "-test.run=^TestProjectAssistantFallbackProcessHelper$")
			command.Env = append(os.Environ(), "CLAWDLINE_FALLBACK_HELPER="+mode)
			input, err := command.StdinPipe()
			if err != nil {
				return nil, err
			}
			output, err := command.StdoutPipe()
			if err != nil {
				return nil, err
			}
			if err = command.Start(); err != nil {
				return nil, err
			}
			processIDs = append(processIDs, command.Process.Pid)
			session := &fallbackOSSession{cmd: command, input: input, output: bufio.NewReader(output), sends: sends, closed: closed}
			line, err := session.output.ReadString('\n')
			if err != nil {
				return session, err
			}
			if line == "usage_limit\n" {
				return session, fallbackUsageRefusal()
			}
			return session, nil
		}}, nil
	}
	f.enable(t)
	fallbackHTTPCreate(t, f)
	paServerOK(t, f.send(t, f.local, "os-scripted"))
	x := fallbackHTTPWait(t, f)
	if len(x.Turns) != 1 || x.Turns[0].State != pa.TurnSucceeded || primarySends.Load() != 0 || alternateSends.Load() != 1 || !primaryClosed.Load() || !alternateClosed.Load() {
		t.Fatalf("process/SQLite outcome: %+v primary=%d alternate=%d closed=%v/%v", x.Turns, primarySends.Load(), alternateSends.Load(), primaryClosed.Load(), alternateClosed.Load())
	}
	t.Logf("real HTTP handler → Service → Engine → Host → owned OS fixtures → fresh SQLite; reaped PIDs=%v; primary Sends=0 alternate Sends=1; no native Agent constructed", processIDs)
}
