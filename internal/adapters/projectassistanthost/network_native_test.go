package projectassistanthost

import (
	"bufio"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	transport "github.com/sainteye/clawdline/internal/adapters/botnetwork"
	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	network "github.com/sainteye/clawdline/internal/app/botnetwork"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	cloud "github.com/sainteye/clawdline/internal/domain/cloud"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The fixture is a real native ACP process. A alone learns its MCP bearer from
// session/new and reaches B through the execution-owned network tool.
func TestNetworkNativePeer(t *testing.T) {
	role := os.Getenv("CLAWDLINE_NETWORK_FIXTURE")
	if role == "" {
		return
	}
	var endpoint, authorization string
	write := func(id json.RawMessage, result any) {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	rpc := func(method string, id any, params any) json.RawMessage {
		message := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
		if id != nil {
			message["id"] = id
		}
		b, _ := json.Marshal(message)
		r, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(b)))
		r.Header.Set("Authorization", authorization)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("MCP-Protocol-Version", "2025-06-18")
		r.Header.Set("Accept", "application/json, text/event-stream")
		response, e := http.DefaultClient.Do(r)
		if e != nil {
			os.Exit(21)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		if response.StatusCode >= 400 {
			os.Exit(22)
		}
		return raw
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var f struct {
			ID     json.RawMessage
			Method string
			Params json.RawMessage
		}
		if json.Unmarshal(scan.Bytes(), &f) != nil {
			os.Exit(23)
		}
		_ = os.WriteFile(os.Getenv("CLAWDLINE_NETWORK_RECEIPT")+"."+role+".phase", append([]byte(f.Method+"\n"), f.Params...), 0600)
		switch f.Method {
		case "initialize":
			write(f.ID, map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": "@agentclientprotocol/codex-acp", "version": "synthetic"}, "agentCapabilities": map[string]any{"loadSession": true, "mcpCapabilities": map[string]bool{"http": true}, "sessionCapabilities": map[string]any{"systemPrompt": map[string]any{}}}})
		case "session/new":
			var p struct {
				MCPServers []struct {
					URL     string
					Headers []struct{ Name, Value string }
				} `json:"mcpServers"`
			}
			if json.Unmarshal(f.Params, &p) != nil || len(p.MCPServers) != 1 {
				os.Exit(24)
			}
			endpoint = p.MCPServers[0].URL
			authorization = p.MCPServers[0].Headers[0].Value
			write(f.ID, map[string]string{"sessionId": "network-native-" + role})
		case "session/prompt":
			if role == "a" {
				rpc("initialize", 1, map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "network-fixture", "version": "1"}})
				rpc("notifications/initialized", nil, map[string]any{})
				var target wire.BotEndpoint

				call := func(kind, id, correlation, text string) network.Response {
					raw := rpc("tools/call", id, map[string]any{"name": "network_bot", "arguments": app.NetworkToolRequest{Kind: kind, RequestID: id, CorrelationID: correlation, AuthorityID: "synthetic-bot-grant", Target: target, Text: text}})
					var out struct {
						Result struct {
							IsError bool `json:"isError"`
							Content []struct{ Text string }
						}
					}
					if json.Unmarshal(raw, &out) != nil || out.Result.IsError || len(out.Result.Content) != 1 {
						_ = os.WriteFile(os.Getenv("CLAWDLINE_NETWORK_RECEIPT"), raw, 0600)
						os.Exit(26)
					}
					var value network.Response
					if json.Unmarshal([]byte(out.Result.Content[0].Text), &value) != nil {
						os.Exit(27)
					}
					return value
				}
				routes := call("routes", "routes-1", "", "")
				if len(routes.Routes) != 1 || routes.Routes[0].AuthorityID != "synthetic-bot-grant" {
					os.Exit(32)
				}
				target = routes.Routes[0].Target
				directory := call("directory", "directory-1", "", "")
				if len(directory.Bots) != 1 || directory.Bots[0].Endpoint.BotID != target.BotID {
					os.Exit(28)
				}
				accepted := call("message", "native-request-1", "", "Synthetic peer review")
				replay := call("message", "native-request-1", "", "Synthetic peer review")
				if accepted.Receipt == nil || replay.Receipt == nil || accepted.Receipt.TurnID != replay.Receipt.TurnID {
					os.Exit(29)
				}
				var final network.Response
				deadline := time.Now().Add(12 * time.Second)
				for i := 0; time.Now().Before(deadline); i++ {
					final = call("read", fmt.Sprintf("native-read-%d", i), "native-request-1", "")
					if final.Receipt != nil && final.Receipt.State == "reply_stored" {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				b, _ := json.Marshal(final)
				_ = os.WriteFile(os.Getenv("CLAWDLINE_NETWORK_RECEIPT"), b, 0600)
				if final.Receipt == nil || final.Receipt.Reply != "Synthetic network reply" || !final.Receipt.Read {
					os.Exit(30)
				}
			} else {
				// The recipient can see the untrusted boundary, never a person-origin turn.
				if !strings.Contains(string(f.Params), "untrusted work from another Bot") {
					os.Exit(31)
				}
				rpc("initialize", 1, map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "recipient-scope-fixture", "version": "1"}})
				rpc("notifications/initialized", nil, map[string]any{})
				for _, forbidden := range []struct {
					name string
					args any
				}{
					{"edit_memory", map[string]any{"idempotency_key": "peer-memory-denied", "append": "Synthetic peer text must not persist"}},
					{"network_bot", app.NetworkToolRequest{Kind: "routes", RequestID: "peer-network-denied"}},
					{"dispatch_work", map[string]any{"idempotency_key": "peer-dispatch-denied", "title": "Inspect the result", "brief": "Read only.", "claims": []string{}}},
				} {
					raw := rpc("tools/call", "deny-"+forbidden.name, map[string]any{"name": forbidden.name, "arguments": forbidden.args})
					var refused struct {
						Result struct {
							IsError bool `json:"isError"`
						}
					}
					missingDispatchReason := forbidden.name == "dispatch_work" && (!strings.Contains(string(raw), "peer_dispatch") || !strings.Contains(string(raw), `\"reason\":`))
					if json.Unmarshal(raw, &refused) != nil || !refused.Result.IsError || !strings.Contains(string(raw), "pa_scope_denied") || missingDispatchReason {
						_ = os.WriteFile(os.Getenv("CLAWDLINE_NETWORK_RECEIPT")+".forbidden", raw, 0600)
						os.Exit(32)
					}
				}
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "network-native-b", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "Synthetic network reply"}}}})
			}
			write(f.ID, map[string]string{"stopReason": "end_turn"})
		}
	}
	os.Exit(0)
}

// Lifecycle diagnostics distinguish a completed reply from native cleanup.
// The optional fault is test-owned and runs only after the real child is closed.
type networkNativeLifecycle struct {
	mu    sync.Mutex
	steps []string
}

func (d *networkNativeLifecycle) record(role, stage string, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.steps = append(d.steps, fmt.Sprintf("%s/%s=%v", role, stage, err))
}
func (d *networkNativeLifecycle) snapshot() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.steps...)
}

type networkLifecycleProvider struct {
	app.Provider
	role       string
	diagnostic *networkNativeLifecycle
	failClose  bool
}

func (p *networkLifecycleProvider) Open(ctx context.Context, q app.OpenRequest, emit func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	session, err := p.Provider.Open(ctx, q, emit)
	p.diagnostic.record(p.role, "open", err)
	if session == nil {
		return nil, err
	}
	return &networkLifecycleSession{ProviderSession: session, provider: p}, err
}

type networkLifecycleSession struct {
	app.ProviderSession
	provider *networkLifecycleProvider
}

func (s *networkLifecycleSession) Send(ctx context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	pending, err := s.ProviderSession.Send(ctx, q)
	s.provider.diagnostic.record(s.provider.role, "send", err)
	if pending == nil {
		return nil, err
	}
	return &networkLifecyclePending{PendingPrompt: pending, provider: s.provider}, err
}
func (s *networkLifecycleSession) Close(ctx context.Context) error {
	err := s.ProviderSession.Close(ctx)
	if err == nil && s.provider.failClose {
		err = context.DeadlineExceeded
	}
	s.provider.diagnostic.record(s.provider.role, "close", err)
	return err
}

type networkLifecyclePending struct {
	app.PendingPrompt
	provider *networkLifecycleProvider
}

func (p *networkLifecyclePending) Wait(ctx context.Context) (app.PromptResult, error) {
	result, err := p.PendingPrompt.Wait(ctx)
	p.provider.diagnostic.record(p.provider.role, "wait", err)
	return result, err
}

func TestNetworkOwnedACPRealHTTPStoresAndCorrelatedReply(t *testing.T) {
	testNetworkOwnedACPRealHTTPStoresAndCorrelatedReply(t, false, false)
}

func TestNetworkOwnedACPReversePairRealHTTPStoresAndCorrelatedReply(t *testing.T) {
	testNetworkOwnedACPRealHTTPStoresAndCorrelatedReply(t, true, false)
}

func TestNetworkOwnedACPReplyKeepsUnknownAfterCloseFailure(t *testing.T) {
	testNetworkOwnedACPRealHTTPStoresAndCorrelatedReply(t, false, true)
}

func testNetworkOwnedACPRealHTTPStoresAndCorrelatedReply(t *testing.T, reversePair, closeFault bool) {
	if runtime.GOOS == "windows" {
		t.Skip("native fixture requires owned process supervisor")
	}
	ctx := context.Background()
	a, b := newHostFixture(t), newHostFixture(t)
	b.o.MachineID = "target"
	b.scope.MachineID = "target"
	b.o.Source = func(context.Context, pa.BotKey, bool) (pa.SourceStatus, error) {
		return pa.SourceStatus{MachineID: "target", State: pa.SourceOnline, Readable: true, Writable: true, Fresh: true, Access: pa.Interactive}, nil
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	receiptPath := filepath.Join(t.TempDir(), "source-read.json")
	var target wire.BotEndpoint
	diagnostic := &networkNativeLifecycle{}
	configure := func(f *hostFixture, role string) {
		// Functional cleanup uses the same registered budget as the Host.
		// The separate closeFault case exercises uncertainty without a timing race.
		f.o.Limits.PromptTimeout = 20 * time.Second
		f.o.Provider = func() (app.Provider, error) {
			provider, err := acp.NewProvider(acp.ProviderOptions{Configurations: map[pa.Agent]acp.Configuration{pa.Codex: {Executable: exe, Arguments: []string{"-test.run=^TestNetworkNativePeer$"}, Environment: []string{"CLAWDLINE_NETWORK_FIXTURE=" + role, "CLAWDLINE_NETWORK_RECEIPT=" + receiptPath, "GORACE=atexit_sleep_ms=0"}}}, Limits: acp.Limits{RPCPending: 8, Notifications: 32, FrameBytes: 1 << 20, CaptureBytes: 1 << 20, StderrBytes: 1024, OwnedDescendants: 64, WriteTimeout: time.Second, CleanupTimeout: time.Duration(capacity.MaximumBotCleanupMilliseconds) * time.Millisecond, RPCTimeout: 15 * time.Second, DescendantPoll: 10 * time.Millisecond}, Verify: func(_ context.Context, bot pa.Bot, _ acp.Configuration) (pa.Capabilities, error) {
				return pa.Capabilities{Key: bot.Key, Project: bot.Project, Agent: bot.Agent, CLIVersion: "synthetic", AdapterVersion: "synthetic", Evidence: "local ACP only", ReadOnlyEnforced: true, NativeMemory: pa.NativeMemoryDisabled, NativeMemoryVerified: true, SendMessage: true}, nil
			}})
			if err != nil {
				return nil, err
			}
			return &networkLifecycleProvider{Provider: provider, role: role, diagnostic: diagnostic, failClose: closeFault && role == "a"}, nil
		}
	}
	configure(a, "a")
	configure(b, "b")
	a.start(t)
	a.enable(t)
	b.start(t)
	b.enable(t)
	create := func(f *hostFixture, id string) pa.Bot {
		s, _ := f.h.Service()
		bot, _, err := s.Create(ctx, pa.Command{Key: pa.BotKey{MachineID: f.o.MachineID, BotID: id}, Operation: pa.CreateOperation, IdempotencyKey: "create-" + id}, pa.CreateBot{Target: pa.ProjectSelection{MachineID: f.o.MachineID, ProjectID: "project"}, Name: "Synthetic " + id, Agent: pa.Codex, Appearance: pa.Appearance{Shape: pa.Blob, Color: "#e39567"}, PermissionPolicy: pa.Ask, Instructions: "Read only"})
		if err != nil {
			t.Fatal(err)
		}
		return bot
	}
	ba, bb := create(a, "bot-a"), create(b, "bot-b")
	endpoint := func(bot pa.Bot) wire.BotEndpoint {
		return wire.BotEndpoint{AccountID: "synthetic-account", MachineID: bot.Key.MachineID, ProjectID: bot.Project.ProjectID, BotID: bot.Key.BotID, ConversationID: bot.ConversationID}
	}
	source := endpoint(ba)
	source.Generation = 1
	target = endpoint(bb)
	for _, f := range []*hostFixture{a, b} {
		bot := ba
		if f == b {
			bot = bb
		}
		c, _ := f.h.store.Conversation(ctx, bot.Key, bot.ConversationID)
		on := true
		_, err := f.h.store.SaveNetworkSettings(ctx, pa.NetworkSettingsWrite{Scope: pa.NetworkScope{AccountID: "synthetic-account", Key: bot.Key, Project: bot.Project}, Fence: pa.Fence{Key: bot.Key, Project: bot.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}, ExpectedBotVersion: bot.Version, Patch: pa.NetworkSettingsPatch{OfferedToNetwork: &on}}, pa.NetworkCapabilities{Peer: pa.NetworkCapability{State: pa.NetworkReady}})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, as, _ := ed25519.GenerateKey(rand.Reader)
	_, bs, _ := ed25519.GenerateKey(rand.Reader)
	ax, _ := ecdh.X25519().GenerateKey(rand.Reader)
	bx, _ := ecdh.X25519().GenerateKey(rand.Reader)
	enc := base64.StdEncoding.EncodeToString
	pair := wire.PairTranscript{PairID: "synthetic-pair", SourceMachineID: "source", TargetMachineID: "target", SourcePublicKey: enc(as.Public().(ed25519.PublicKey)), TargetPublicKey: enc(bs.Public().(ed25519.PublicKey)), SourceEncryptionKey: enc(ax.PublicKey().Bytes()), TargetEncryptionKey: enc(bx.PublicKey().Bytes()), SourceFingerprint: cloud.Fingerprint(as.Public().(ed25519.PublicKey)), TargetFingerprint: cloud.Fingerprint(bs.Public().(ed25519.PublicKey))}
	offerSigner, acceptSigner := as, bs
	if reversePair {
		pair.SourceMachineID, pair.TargetMachineID = pair.TargetMachineID, pair.SourceMachineID
		pair.SourcePublicKey, pair.TargetPublicKey = pair.TargetPublicKey, pair.SourcePublicKey
		pair.SourceEncryptionKey, pair.TargetEncryptionKey = pair.TargetEncryptionKey, pair.SourceEncryptionKey
		pair.SourceFingerprint, pair.TargetFingerprint = pair.TargetFingerprint, pair.SourceFingerprint
		offerSigner, acceptSigner = bs, as
	}
	pair.SourceOfferSignature = wire.SignOffer(pair, offerSigner)
	pair.TargetAcceptSignature = wire.SignAcceptance(pair, acceptSigner)
	grants := make([]*transport.Store, 2)
	for i, f := range []*hostFixture{a, b} {
		st, err := transport.NewStore(ctx, f.o.Database, f.o.MachineID)
		if err != nil {
			t.Fatal(err)
		}
		grants[i] = st
		fingerprint := pair.TargetFingerprint
		if f.o.MachineID == pair.TargetMachineID {
			fingerprint = pair.SourceFingerprint
		}
		private, signer := ax, as
		if f == b {
			private, signer = bx, bs
		}
		g := network.Grant{ID: "synthetic-bot-grant", AccountID: "synthetic-account", Epoch: 1, Source: source, Target: target, Pair: pair, ComparedFingerprint: fingerprint, ExpiresAt: time.Now().Add(time.Hour)}
		if err = st.PutGrant(ctx, g); err != nil {
			t.Fatal(err)
		}
		service, err := network.New(network.Options{Authority: st, AccountID: g.AccountID, MachineID: f.o.MachineID, Private: private, Signer: signer, Repository: st, Host: NetworkHost{Host: f.h, AccountID: g.AccountID}, Clock: time.Now})
		if err != nil {
			t.Fatal(err)
		}
		f.h.o.Network = service
	}
	server := httptest.NewServer(transport.Handler{Service: b.h.o.Network})
	defer server.Close()
	a.h.o.NetworkTransport = func(context.Context, string) (network.Transport, error) {
		return transport.HTTPTransport{URL: server.URL, Client: server.Client()}, nil
	}
	if _, e = a.send(t, ba, "network-start"); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		xa, _ := a.h.store.Export(ctx, ba.Key)
		if len(xa.Turns) > 0 && xa.Conversation.ActiveTurnID == "" && xa.Turns[0].State != pa.TurnQueued && xa.Turns[0].State != pa.TurnAccepted {
			want := pa.TurnSucceeded
			if closeFault {
				want = pa.TurnUnknown
			}
			if xa.Turns[0].State != want {
				raw, _ := os.ReadFile(receiptPath)
				phase, _ := os.ReadFile(receiptPath + ".a.phase")
				t.Fatalf("requester failed: %+v diagnostic=%s phase=%s lifecycle=%v prompt_budget=%s", xa.Turns, raw, phase, diagnostic.snapshot(), a.o.Limits.PromptTimeout)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("requester timeout %+v", xa.Turns)
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw, e := os.ReadFile(receiptPath)
	if e != nil {
		t.Fatal(e)
	}
	var result network.Response
	if json.Unmarshal(raw, &result) != nil || result.Receipt == nil || result.Receipt.Reply != "Synthetic network reply" {
		t.Fatalf("reply %s", raw)
	}
	xb, e := b.h.store.Export(ctx, bb.Key)
	if e != nil || len(xb.Turns) != 1 || xb.Turns[0].Origin != pa.NetworkRequestOrigin {
		t.Fatalf("target ran more than once or wrong origin %+v %v", xb.Turns, e)
	}
	if n, e := grants[1].Usage(ctx); e != nil || n != 1 {
		t.Fatalf("receipt occupancy %d %v", n, e)
	}
	reopened, e := transport.NewStore(ctx, b.o.Database, "target")
	if e != nil {
		t.Fatal(e)
	}
	r, e := reopened.Receipt(ctx, result.Receipt.Header, result.Receipt.Header.RequestID)
	if e != nil || r.State != "reply_stored" || !r.Read {
		t.Fatalf("durable reply %+v %v", r, e)
	}
	if closeFault {
		requester, err := a.h.store.Export(ctx, ba.Key)
		if err != nil || len(requester.Turns) != 1 || requester.Turns[0].Failure == nil || requester.Turns[0].Failure.Code != pa.ExecutionEffectUnknown || requester.Conversation.ActiveTurnID != "" {
			t.Fatalf("cleanup uncertainty lost: %+v %v", requester.Turns, err)
		}
		phases := diagnostic.snapshot()
		joined := strings.Join(phases, "\n")
		if !strings.Contains(joined, "a/wait=<nil>") || !strings.Contains(joined, "a/close=context deadline exceeded") {
			t.Fatalf("fault did not follow completed prompt: %v", phases)
		}
		for _, role := range []string{"a", "b"} {
			if strings.Count(joined, role+"/open=") != 1 {
				t.Fatalf("native retry after cleanup failure: %v", phases)
			}
		}
		if xb.Turns[0].State != pa.TurnSucceeded {
			t.Fatalf("recipient reply was not completed: %+v", xb.Turns)
		}
		t.Logf("reply stayed durable/read; requester unknown; recipient succeeded; native attempts=1+1; lifecycle=%v", phases)
		return
	}
	client := a.h.o.Network
	httpTransport := transport.HTTPTransport{URL: server.URL, Client: server.Client()}
	call := network.Call{RequestID: r.Header.RequestID, Kind: "message", AuthorityID: r.Header.AuthorityID, Source: r.Header.Source, Target: r.Header.Target, Text: "Altered payload"}
	if _, err := client.Call(ctx, call, httpTransport); pa.Code(err) != pa.IdempotencyConflict {
		t.Fatalf("changed payload accepted %v", err)
	}
	stale := call
	stale.RequestID = "stale-source"
	stale.Source.Generation++
	if _, err := client.Call(ctx, stale, httpTransport); pa.Code(err) != pa.StaleGeneration {
		t.Fatalf("stale source accepted %v", err)
	}

	// Turning off the actual stored receiver preference withholds its directory
	// and any new message before another native execution can be admitted.
	targetState, err := b.h.store.Export(ctx, bb.Key)
	if err != nil {
		t.Fatal(err)
	}
	scope := pa.NetworkScope{AccountID: "synthetic-account", Key: bb.Key, Project: bb.Project}
	preferences, err := b.h.store.NetworkSettings(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	off, on := false, true
	settingsWrite := pa.NetworkSettingsWrite{Scope: scope, Fence: pa.Fence{Key: bb.Key, Project: bb.Project, ConversationID: targetState.Conversation.ID, BindingID: targetState.Conversation.BindingID, Generation: targetState.Conversation.Generation}, ExpectedBotVersion: targetState.Bot.Version, ExpectedRevision: preferences.Revision, Patch: pa.NetworkSettingsPatch{OfferedToNetwork: &off}}
	if _, err = b.h.store.SaveNetworkSettings(ctx, settingsWrite, pa.NetworkCapabilities{}); err != nil {
		t.Fatal(err)
	}
	routes, err := client.Routes(ctx, r.Header.Source, a.h.o.NetworkTransport)
	if err != nil || len(routes.Routes) != 0 {
		t.Fatalf("disabled recipient advertised %+v %v", routes, err)
	}
	disabled := call
	disabled.RequestID = "disabled-message"
	disabled.Text = "Do not execute"
	if _, err = client.Call(ctx, disabled, httpTransport); pa.Code(err) != pa.Revoked {
		t.Fatalf("disabled recipient admitted %v", err)
	}
	settingsWrite.ExpectedRevision++
	settingsWrite.Patch.OfferedToNetwork = &on
	if _, err = b.h.store.SaveNetworkSettings(ctx, settingsWrite, pa.NetworkCapabilities{Peer: pa.NetworkCapability{State: pa.NetworkReady}}); err != nil {
		t.Fatal(err)
	}
	grant, err := grants[1].Grant(ctx, r.Header.AuthorityID)
	if err != nil {
		t.Fatal(err)
	}
	grant.Epoch++
	grant.Revoked = true
	if err = grants[1].PutGrant(ctx, grant); err != nil {
		t.Fatal(err)
	}
	call.RequestID = "revoked-read"
	call.Kind = "read"
	call.CorrelationID = r.Header.RequestID
	call.Text = ""
	if _, err = client.Call(ctx, call, httpTransport); pa.Code(err) != pa.Revoked {
		t.Fatalf("revoked grant accepted %v", err)
	}
	again, _ := b.h.store.Export(ctx, bb.Key)
	if len(again.Turns) != 1 {
		t.Fatal("refusals ran target")
	}
	t.Logf("request=%s target_turn=%s reply_state=%s read=%t target_executions=%d receipt_rows=1", r.Header.RequestID, r.TurnID, r.State, r.Read, len(xb.Turns))
}
