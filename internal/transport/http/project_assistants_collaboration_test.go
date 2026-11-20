package http

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	host "github.com/sainteye/clawdline/internal/adapters/projectassistanthost"
	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// This subprocess has only the owned ACP MCP descriptor, not a daemon token
// or direct Go callback. Its HTTP tool calls exercise the actual admission.
func TestCollaborationACPSubprocess(t *testing.T) {
	role := os.Getenv("CLAWDLINE_REVIEW_PEER")
	if role == "" {
		return
	}
	fail := func(code int) {
		f, err := os.OpenFile(os.Getenv("CLAWDLINE_REVIEW_LOG"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err == nil {
			fmt.Fprintf(f, "%s peer failure=%d\n", role, code)
			_ = f.Close()
		}
		os.Exit(code)
	}
	write := func(id json.RawMessage, result any) {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	var endpoint, token string
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}}
	rpc := func(method string, id any, params any) []byte {
		frame := map[string]any{"jsonrpc": "2.0", "method": method}
		if id != nil {
			frame["id"] = id
		}
		if params != nil {
			frame["params"] = params
		}
		body, _ := json.Marshal(frame)
		r, e := http.NewRequest("POST", endpoint, strings.NewReader(string(body)))
		if e != nil {
			fail(21)
		}
		r.Header.Set("Authorization", token)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("MCP-Protocol-Version", "2025-06-18")
		resp, e := client.Do(r)
		if e != nil {
			fail(22)
		}
		b, e := io.ReadAll(resp.Body)
		resp.Body.Close()
		if e != nil || resp.StatusCode >= 400 {
			fail(23)
		}
		return b
	}
	call := func(name string, args any, wantError bool) json.RawMessage {
		b := rpc("tools/call", "tool", map[string]any{"name": name, "arguments": args})
		var v struct {
			Result struct {
				IsError bool `json:"isError"`
				Content []struct{ Text string }
			}
			Error json.RawMessage
		}
		if json.Unmarshal(b, &v) != nil || len(v.Error) > 0 || v.Result.IsError != wantError || len(v.Result.Content) != 1 {
			fail(24)
		}
		return json.RawMessage(v.Result.Content[0].Text)
	}
	log := func(v string) {
		f, e := os.OpenFile(os.Getenv("CLAWDLINE_REVIEW_LOG"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if e != nil {
			fail(25)
		}
		_, _ = fmt.Fprintln(f, v)
		_ = f.Close()
	}
	var pending json.RawMessage
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var q struct {
			ID     json.RawMessage
			Method string
			Params json.RawMessage
			Result json.RawMessage
		}
		if json.Unmarshal(scan.Bytes(), &q) != nil {
			fail(26)
		}
		if q.Method == "" && string(q.ID) == `"review-permission"` {
			if !strings.Contains(string(q.Result), "allow-review") {
				fail(27)
			}
			log("B permission answered")
			write(pending, map[string]string{"stopReason": "end_turn", "text": "Review is safe."})
			continue
		}
		switch q.Method {
		case "initialize":
			name := "@agentclientprotocol/codex-acp"
			if role == "B" {
				name = "@agentclientprotocol/claude-agent-acp"
			}
			write(q.ID, map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": name, "version": "fixture"}, "agentCapabilities": map[string]any{"loadSession": true, "_meta": map[string]any{"claudeCode": map[string]any{}}, "mcpCapabilities": map[string]bool{"http": true}, "sessionCapabilities": map[string]any{"systemPrompt": map[string]any{}}}})
		case "session/new", "session/load":
			var p struct {
				MCPServers []struct {
					URL     string
					Headers []struct{ Name, Value string }
				} `json:"mcpServers"`
			}
			if json.Unmarshal(q.Params, &p) != nil || len(p.MCPServers) != 1 || len(p.MCPServers[0].Headers) != 1 {
				fail(28)
			}
			endpoint, token = p.MCPServers[0].URL, p.MCPServers[0].Headers[0].Value
			write(q.ID, map[string]string{"sessionId": "review-peer-" + role})
		case "session/prompt":
			rpc("initialize", 1, map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "review-peer", "version": "1"}})
			rpc("notifications/initialized", nil, nil)
			if !strings.Contains(string(rpc("tools/list", 2, map[string]any{})), `"message_bot"`) {
				fail(29)
			}
			if role == "A" && strings.Contains(string(q.Params), "This is a correlated Bot reply") {
				log("A reply")
				call("edit_memory", map[string]any{"idempotency_key": "reply-memory", "append": "Review is a user fact"}, true)
				write(q.ID, map[string]string{"stopReason": "end_turn", "text": "The peer review is complete: safe."})
			} else if role == "A" {
				log("A input")
				var page pa.BotDirectoryPage
				if json.Unmarshal(call("list_bots", map[string]any{}, false), &page) != nil || len(page.Bots) != 1 || page.Bots[0].ID != "review-b" {
					fail(30)
				}
				for _, id := range []string{"review-c", "review-d", "review-a"} {
					call("message_bot", map[string]any{"idempotency_key": id, "target_bot_id": id, "text": "Review."}, true)
				}
				call("message_bot", map[string]any{"idempotency_key": "forged", "target_bot_id": "review-b", "text": "Review.", "sender_bot_id": "review-c"}, true)
				args := map[string]any{"idempotency_key": "one-review", "target_bot_id": "review-b", "text": "Review this change; return a safety assessment."}
				var x, y pa.Exchange
				if json.Unmarshal(call("message_bot", args, false), &x) != nil || x.Outcome != pa.ExchangeQueued {
					fail(31)
				}
				if json.Unmarshal(call("message_bot", args, false), &y) != nil || y.ID != x.ID {
					fail(32)
				}
				args["text"] = "Different request"
				call("message_bot", args, true)
				write(q.ID, map[string]string{"stopReason": "end_turn"})
			} else {
				log("B request")
				call("send_message", map[string]any{"idempotency_key": "forbidden-message", "text": "Hidden recipient answer"}, true)
				call("message_bot", map[string]any{"idempotency_key": "forbidden-recursion", "target_bot_id": "review-a", "text": "Recurse"}, true)
				call("send_file", map[string]any{"idempotency_key": "forbidden-file", "path": "missing"}, true)
				call("publish_artifact", map[string]any{"idempotency_key": "forbidden-artifact", "path": "missing", "title": "Review"}, true)
				call("save_routine", map[string]any{"idempotency_key": "forbidden-followup", "name": "Follow up", "instruction": "Review later", "interval_seconds": 60}, true)
				pending = q.ID
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": "review-permission", "method": "session/request_permission", "params": map[string]any{"sessionId": "review-peer-B", "options": []any{map[string]string{"optionId": "allow-review", "name": "Allow once", "kind": "allow_once"}}}})
			}
		}
	}
	os.Exit(0)
}

type collaborationProvider struct {
	app.Provider
	t *testing.T
}

func (p collaborationProvider) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	s, err := p.Provider.Open(ctx, q, sink)
	if err != nil {
		p.t.Logf("scripted ACP Open %s: %v", q.Bot.Key.BotID, err)
	}
	if s != nil {
		return collaborationSession{ProviderSession: s, t: p.t, bot: q.Bot.Key.BotID}, err
	}
	return s, err
}

type collaborationSession struct {
	app.ProviderSession
	t   *testing.T
	bot string
}

func (s collaborationSession) Send(ctx context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	p, err := s.ProviderSession.Send(ctx, q)
	if err != nil {
		s.t.Logf("scripted ACP Send %s: %v", s.bot, err)
	}
	if p != nil {
		return collaborationPending{PendingPrompt: p, t: s.t, bot: s.bot}, err
	}
	return p, err
}

type collaborationPending struct {
	app.PendingPrompt
	t   *testing.T
	bot string
}

func (p collaborationPending) Wait(ctx context.Context) (app.PromptResult, error) {
	r, err := p.PendingPrompt.Wait(ctx)
	if err != nil {
		p.t.Logf("scripted ACP Wait %s: %v", p.bot, err)
	}
	return r, err
}

type collaborationProjects struct {
	directory string
	scopes    map[string]pa.ProjectScope
}

func (p collaborationProjects) Resolve(_ context.Context, q pa.ProjectSelection) (pa.ProjectResolution, error) {
	s, ok := p.scopes[q.ProjectID]
	if !ok || s.MachineID != q.MachineID {
		return pa.ProjectResolution{}, &pa.Failure{Code: pa.ScopeDenied, Field: "fixture_project"}
	}
	return pa.ProjectResolution{Scope: s, Directory: p.directory, Current: true, Available: true}, nil
}

func TestCollaborationRealTCPMCPRequestAndReply(t *testing.T) {
	for _, actors := range []int{1, 16} {
		t.Run(fmt.Sprint("actors-", actors), func(t *testing.T) { collaborationRealTCPMCPRequestAndReply(t, actors) })
	}
}

func collaborationRealTCPMCPRequestAndReply(t *testing.T, maxActors int) {
	if runtime.GOOS == "windows" {
		t.Skip("native supervisor unavailable")
	}
	f := newPAServerFixture(t, true)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "peer.log")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	project := pa.ProjectScope{MachineID: f.key.MachineID, ProjectID: "review-project", CanonicalID: "shared-canonical", Kind: pa.RepositoryProject}
	other := project
	other.ProjectID = "different-project"
	resolver := collaborationProjects{directory: f.project, scopes: map[string]pa.ProjectScope{project.ProjectID: project, other.ProjectID: other}}
	provider := func() (app.Provider, error) {
		configs := map[pa.Agent]acp.Configuration{}
		for agent, role := range map[pa.Agent]string{pa.Codex: "A", pa.ClaudeCode: "B"} {
			configs[agent] = acp.Configuration{Executable: exe, Arguments: []string{"-test.run=^TestCollaborationACPSubprocess$"}, Environment: []string{"CLAWDLINE_REVIEW_PEER=" + role, "CLAWDLINE_REVIEW_LOG=" + logPath, "GORACE=atexit_sleep_ms=0"}}
		}
		p, e := acp.NewProvider(acp.ProviderOptions{Configurations: configs, Limits: acp.Limits{RPCPending: 8, Notifications: 32, FrameBytes: 1 << 20, CaptureBytes: 1 << 20, StderrBytes: 1024, OwnedDescendants: 64, WriteTimeout: time.Second, CleanupTimeout: 2 * time.Second, RPCTimeout: 15 * time.Second, DescendantPoll: 10 * time.Millisecond}, Verify: func(_ context.Context, b pa.Bot, _ acp.Configuration) (pa.Capabilities, error) {
			c := paServerCaps(b)
			c.SendMessage = true
			return c, nil
		}})
		return collaborationProvider{Provider: p, t: t}, e
	}
	makeHost := func(s *Server) (*host.Host, error) {
		return host.New(context.Background(), host.Options{Database: s.store.ProjectAssistantDatabase(), Reader: s.store.ProjectAssistantReader(), MachineID: f.key.MachineID, Projects: resolver, Broker: s.broker, Clock: time.Now, ID: projectAssistantID, Limits: app.RuntimeLimits{MaxBots: maxActors, MaxProcesses: 4, PromptTimeout: 20 * time.Second, CleanupTimeout: 2 * time.Second}, Authority: s.projectAssistantAuthority, Source: s.projectAssistantSource, Provider: provider, ReadViewByteLimit: 1 << 20})
	}
	hosted, err := makeHost(f.s)
	if err != nil {
		t.Fatal(err)
	}
	f.s.projectAssistants.host = hosted
	f.s.projectAssistants.machine = f.identity.MachineID
	f.s.projectAssistants.account = f.identity.AccountID
	h := &paSoakHTTP{fixture: f, server: httptest.NewServer(f.h)}
	h.reconnect()
	defer h.server.Close()
	defer h.client.CloseIdleConnections()
	if h.request(t, "POST", "/v1/settings/project-assistants", map[string]bool{"enabled": true}, nil) != 200 {
		t.Fatal("enable")
	}
	for _, id := range []string{"review-a", "review-b", "review-c"} {
		f.key.BotID = id
		q := f.create()
		q.Settings.Target = paWireProjectSelection(pa.ProjectSelection{MachineID: project.MachineID, ProjectID: project.ProjectID})
		q.Settings.Name = "Synthetic reviewer"
		if id == "review-b" {
			q.Settings.Agent = contract.ProjectAssistantAgentClaude
		}
		if id == "review-c" {
			q.Settings.Target.ProjectID = other.ProjectID
		}
		if h.request(t, "POST", "/v1/project-assistants", q, nil) != 200 {
			t.Fatal("create", id)
		}
	}
	f.key.BotID = "review-b"
	groupsManagerToggle(t, f, true)
	// The foreign source is actual stored data, but cannot be selected by an
	// owned tool whose sender machine is host bound.
	st := store.New(f.s.store.ProjectAssistantDatabase())
	dKey := pa.BotKey{MachineID: "other-source", BotID: "review-d"}
	foreign := project
	foreign.MachineID = dKey.MachineID
	b := pa.Bot{Key: dKey, Project: foreign, ConversationID: "foreign-chat", Name: "Synthetic reviewer", Appearance: pa.Appearance{Shape: pa.Blob, Color: pa.EntryColor}, Agent: pa.Codex, PermissionPolicy: pa.Ask, Instructions: pa.InstructionSnapshot{Revision: 1}, Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	c := pa.Conversation{Key: dKey, ID: b.ConversationID, MemoryRevision: 1, Version: 1}
	cmd := pa.Command{Key: dKey, Operation: pa.CreateOperation, IdempotencyKey: "foreign-create", ContentHash: strings.Repeat("a", 64)}
	if _, err = st.Commit(context.Background(), app.Mutation{Command: cmd, Bot: &b, Conversation: &c, Receipt: pa.Receipt{ID: "foreign-created", State: pa.EffectDelivered}}); err != nil {
		t.Fatal(err)
	}
	f.key.BotID = "review-a"
	_, admission := h.send(t, "request-review")
	deadline := time.Now().Add(15 * time.Second)
	var bx app.Export
	for time.Now().Before(deadline) {
		bx, err = st.Export(context.Background(), pa.BotKey{MachineID: f.key.MachineID, BotID: "review-b"})
		if err != nil {
			t.Fatal(err)
		}
		if len(bx.Permissions) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(bx.Permissions) != 1 {
		a, _ := st.Export(context.Background(), f.key)
		log, _ := os.ReadFile(logPath)
		t.Fatalf("permission not offered: B=%+v A=%+v events=%+v usage=%+v log=%s", bx.Turns, a.Turns, a.Events, hosted.Usage(), log)
	}
	// A's tool response and original native turn must finish before B replies.
	ax := paSoakAwait(t, f, admission.ResourceID, pa.TurnSucceeded)
	if len(ax.Exchanges) != 1 || ax.Exchanges[0].Outcome != pa.ExchangeSent {
		t.Fatalf("queued exchange: %+v", ax.Exchanges)
	}
	p := bx.Permissions[0]
	f.key.BotID = "review-b"
	answer := contract.ProjectAssistantPermissionAnswerRequest{Command: f.command(pa.AnswerOperation, "answer-review"), Fence: paWireFence(p.Fence), OfferID: p.ID, OptionID: "allow-review", TurnID: p.TurnID}
	if h.request(t, "POST", "/v1/project-assistants/review-b/permissions/"+p.ID+"/answer", answer, nil) != 200 {
		t.Fatal("permission answer")
	}
	f.key.BotID = "review-a"
	for time.Now().Before(deadline) {
		ax, err = st.Export(context.Background(), f.key)
		if err != nil {
			t.Fatal(err)
		}
		if len(ax.Turns) == 2 && len(ax.Exchanges) == 1 && ax.Exchanges[0].Delivery == "delivered" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(ax.Turns) != 2 || ax.Exchanges[0].Outcome != pa.ExchangeCompleted || ax.Exchanges[0].Delivery != "delivered" {
		t.Fatalf("reply missing: turns=%+v exchange=%+v", ax.Turns, ax.Exchanges)
	}
	counts := map[pa.MessageKind]int{}
	for _, m := range ax.Messages {
		counts[m.Kind]++
	}
	if counts[pa.UserMessage] != 1 || counts[pa.AssistantMessage] != 1 {
		t.Fatal("source bubbles", counts)
	}
	bx, err = st.Export(context.Background(), pa.BotKey{MachineID: f.key.MachineID, BotID: "review-b"})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range bx.Messages {
		if m.Kind == pa.UserMessage || m.Kind == pa.AssistantMessage {
			t.Fatal("recipient leaked normal chat", m)
		}
	}
	if len(bx.Turns) != 1 || bx.Turns[0].Origin != pa.BotRequestOrigin || bx.Turns[0].State != pa.TurnSucceeded {
		t.Fatal("recipient request", bx.Turns)
	}
	h.reconnect()
	var exported contract.ProjectAssistantExport
	if h.request(t, "GET", "/v1/project-assistants/review-a/export?machine_id="+f.key.MachineID, nil, &exported) != 200 || len(exported.Exchanges) != 1 || exported.Exchanges[0].Delivery != "delivered" {
		t.Fatal("HTTP durable export")
	}
	var view contract.ProjectAssistantReadViewResponse
	if h.request(t, "GET", paViewURL(f.key, ax.Conversation.ID, pa.ReadViewSnapshot, "", "", 0, 100), nil, &view) != 200 {
		t.Fatal("HTTP view")
	}
	records, bad, err := f.s.broker.Records(context.Background())
	if err != nil || len(records) != 0 || len(bad) != 0 {
		t.Fatal("unexpected broker launch", records, bad, err)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"A input", "A reply", "B request", "B permission answered"} {
		if strings.Count(string(logBytes), line+"\n") != 1 {
			t.Fatalf("invocations: %s", logBytes)
		}
	}
	// Close the actual Host, TCP server and SQLite owner. New production
	// Server/Host instances must read the same persisted result without ACP IO.
	if err = hosted.Close(context.Background()); err != nil {
		t.Fatal("close host", err)
	}
	h.server.Close()
	h.client.CloseIdleConnections()
	if err = f.s.store.Close(); err != nil {
		t.Fatal("close SQLite", err)
	}
	reopened, err := New(f.s.cfg)
	if err != nil {
		t.Fatal("reopen server", err)
	}
	t.Cleanup(func() {
		if err := reopened.StopProjectAssistants(context.Background()); err != nil {
			t.Error(err)
		}
		if err := reopened.store.Close(); err != nil {
			t.Error(err)
		}
	})
	reopenedHost, err := makeHost(reopened)
	if err != nil {
		t.Fatal("reopen host", err)
	}
	reopened.projectAssistants.host = reopenedHost
	reopened.projectAssistants.machine = f.identity.MachineID
	reopened.projectAssistants.account = f.identity.AccountID
	f.s = reopened
	f.h = reopened.Handler()
	h.server = httptest.NewServer(f.h)
	defer h.server.Close()
	h.reconnect()
	var retained contract.ProjectAssistantExport
	if h.request(t, "GET", "/v1/project-assistants/review-a/export?machine_id="+f.key.MachineID, nil, &retained) != 200 || len(retained.Exchanges) != 1 || retained.Exchanges[0].Delivery != "delivered" || len(retained.Turns) != 2 {
		t.Fatal("reopen lost exchange/turn", retained.Exchanges)
	}
	if h.request(t, "GET", paViewURL(f.key, ax.Conversation.ID, pa.ReadViewSnapshot, "", "", 0, 100), nil, &view) != 200 {
		t.Fatal("reopened HTTP view")
	}
	afterLog, err := os.ReadFile(logPath)
	if err != nil || string(afterLog) != string(logBytes) {
		t.Fatal("reopen repeated native work", err, string(afterLog))
	}
	t.Logf("authenticated TCP create/send/permission/read-view/export + owned MCP + actual SQLite/Host reopen: A input=1 A reply=1 B request=1; exchanges=1 completed/delivered; A user=1 summary=1 B user/answer=0 Broker=0 vendor/model=0")
}
