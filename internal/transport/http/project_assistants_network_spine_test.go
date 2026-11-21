package http

import (
	"bufio"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adapter "github.com/sainteye/clawdline/internal/adapters/botnetwork"
	cloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/peerstore"
	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	appnet "github.com/sainteye/clawdline/internal/app/botnetwork"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	botwire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"golang.org/x/net/websocket"
)

// Exercise JSON persistence and repeated consent synchronization in a fresh
// UTC process even when the developer's local timezone has a nonzero offset.
// The private API, Relay and native provider remain the existing local doubles.
func TestBotCloudConsentReplayInUTC(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestBotCloudNormalSourceSpineAndRevocation$", "-test.v")
	cmd.Env = append(os.Environ(), "TZ=UTC", "CLAWDLINE_BOT_NETWORK_NATIVE_PROOF=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("UTC consent synchronization: %v\n%s", err, output)
	}
	t.Log(string(output))
}

// The subprocess uses the real ACP adapter and performs zero model requests.
func TestBotCloudOwnedACP(t *testing.T) {
	if os.Getenv("CLAWDLINE_BOT_CLOUD_ACP") == "" {
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var q struct {
			ID     json.RawMessage
			Method string
		}
		if json.Unmarshal(scan.Bytes(), &q) != nil {
			os.Exit(20)
		}
		var result any
		switch q.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": "@agentclientprotocol/codex-acp", "version": "fixture"}, "agentCapabilities": map[string]any{"loadSession": true, "mcpCapabilities": map[string]bool{"http": true}, "sessionCapabilities": map[string]any{"systemPrompt": map[string]any{}}}}
		case "session/new":
			result = map[string]string{"sessionId": "owned-bot-cloud"}
		case "session/prompt":
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "owned-bot-cloud", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "Synthetic Cloud reply"}}}})
			result = map[string]string{"stopReason": "end_turn"}
		default:
			continue
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
	}
	os.Exit(0)
}

type spineSocket struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (s *spineSocket) send(v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return websocket.JSON.Send(s.conn, v)
}

// This is deliberately a simulated private API/Relay, not cross-service
// acceptance. It verifies normal Server/Host construction with actual TCP,
// negotiated sockets, SQL, signed transport ACKs, and the owned ACP process.
type spineCloud struct {
	t              *testing.T
	mu             sync.Mutex
	keys           map[string]domaincloud.DeviceKey
	sockets        map[string]*spineSocket
	caps           map[string]cloud.BotCapabilities
	consent        cloud.BotConsent
	pair           cloud.PeerPair
	revoked        bool
	authorizations atomic.Int32
	acks           atomic.Int32
}

func (c *spineCloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	machine := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if r.URL.Path == "/v1/connect" {
		websocket.Server{Handshake: func(*websocket.Config, *http.Request) error { return nil }, Handler: func(ws *websocket.Conn) { c.socket(machine, ws) }}.ServeHTTP(w, r)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	send := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	if _, ok := c.keys[machine]; !ok {
		w.WriteHeader(401)
		return
	}
	switch {
	case r.URL.Path == "/v1/tokens/device":
		send(cloud.DeviceToken{Token: machine, TokenType: "Bearer", ExpiresIn: 300, ExpiresAt: time.Now().Add(5 * time.Minute), Kid: "synthetic"})
	case r.URL.Path == "/v1/bot-network/capabilities" && r.Method == "PUT":
		var q struct {
			ExpectedRevision int64                 `json:"expected_revision"`
			WireVersion      int                   `json:"wire_version"`
			State            string                `json:"state"`
			Endpoints        []botwire.BotEndpoint `json:"endpoints"`
			ExpiresAt        int64                 `json:"expires_at"`
		}
		_ = json.NewDecoder(r.Body).Decode(&q)
		if c.caps[machine].Revision != q.ExpectedRevision {
			w.WriteHeader(409)
			send(map[string]any{"authorized": false, "reason": "consent_stale"})
			return
		}
		c.caps[machine] = cloud.BotCapabilities{MachineID: machine, Revision: q.ExpectedRevision + 1, WireVersion: 1, State: q.State, Endpoints: q.Endpoints, ExpiresAt: q.ExpiresAt}
		send(map[string]any{"revision": q.ExpectedRevision + 1, "expires_at": q.ExpiresAt})
	case strings.HasPrefix(r.URL.Path, "/v1/bot-network/capabilities/"):
		cap := c.caps[strings.TrimPrefix(r.URL.Path, "/v1/bot-network/capabilities/")]
		if cap.Revision == 0 {
			w.WriteHeader(403)
			send(map[string]any{"authorized": false, "reason": "capability_unavailable"})
			return
		}
		send(cap)
	case r.URL.Path == "/v1/bot-network/source-consents/consent":
		send(c.consent)
	case strings.HasPrefix(r.URL.Path, "/v1/peer/pairs/"):
		send(c.pair)
	case r.URL.Path == "/v1/bot-network/authorize":
		c.authorizations.Add(1)
		var q struct {
			Header botwire.BotHeader `json:"header"`
		}
		_ = json.NewDecoder(r.Body).Decode(&q)
		if q.Header.Version != 1 || q.Header.RequestID == "" || len(q.Header.BodyDigest) != 64 {
			c.t.Error("live authorizer received an incomplete Bot header")
			w.WriteHeader(400)
			send(map[string]any{"authorized": false, "reason": "invalid_shape"})
			return
		}
		if c.revoked {
			w.WriteHeader(403)
			send(map[string]any{"authorized": false, "reason": "consent_revoked"})
			return
		}
		send(map[string]any{"authorized": true, "account_id": q.Header.Source.AccountID, "pair_id": q.Header.PairID, "authority_id": q.Header.AuthorityID, "authority_epoch": q.Header.AuthorityEpoch, "expires_at": time.Now().Unix() + 60})
	default:
		w.WriteHeader(404)
	}
}
func (c *spineCloud) socket(machine string, ws *websocket.Conn) {
	defer ws.Close()
	c.mu.Lock()
	key, ok := c.keys[machine]
	c.mu.Unlock()
	if !ok {
		return
	}
	s := &spineSocket{conn: ws}
	nonce := make([]byte, 32)
	_, _ = rand.Read(nonce)
	challenge := base64.StdEncoding.EncodeToString(nonce)
	_ = s.send(cloud.ChallengeFrame{Type: "challenge", V: 1, Context: cloud.ChallengeContext, Account: "synthetic-account", Device: machine, Challenge: challenge, ExpiresInMs: 15000})
	var hello cloud.HelloFrame
	if websocket.JSON.Receive(ws, &hello) != nil {
		return
	}
	sig, _ := base64.StdEncoding.DecodeString(hello.Sig)
	if !ed25519.Verify(key.PublicKey(), []byte(cloud.ChallengeString("synthetic-account", machine, challenge)), sig) {
		return
	}
	_ = s.send(cloud.ReadyFrame{Type: "ready", V: 1, Account: "synthetic-account", Device: machine, Role: "machine", ConnectedAt: time.Now().UnixMilli(), TokenExpiresAt: time.Now().Add(time.Minute).UnixMilli()})
	for {
		var raw []byte
		if websocket.Message.Receive(ws, &raw) != nil {
			return
		}
		var f struct {
			Type    string `json:"type"`
			Version int    `json:"version"`
			botwire.BotEnvelope
		}
		if json.Unmarshal(raw, &f) != nil {
			return
		}
		switch f.Type {
		case "ping":
			_ = s.send(map[string]string{"type": "pong"})
		case "bot_network_ready":
			if f.Version != 1 {
				return
			}
			c.mu.Lock()
			c.sockets[machine] = s
			c.mu.Unlock()
			_ = s.send(map[string]any{"type": "bot_network_ready", "version": 1})
		case "bot_network_publish":
			c.mu.Lock()
			target := c.sockets[f.Header.Target.MachineID]
			c.mu.Unlock()
			if target == nil {
				return
			}
			hd, fd := adapter.CloudDigests(f.BotEnvelope)
			delivery := "delivery-" + f.Header.RequestID
			_ = target.send(map[string]string{"type": "bot_network_delivery", "request_id": f.Header.RequestID, "header_digest": hd, "frame_digest": fd, "delivery_id": delivery})
			_ = target.send(struct {
				Type string `json:"type"`
				botwire.BotEnvelope
			}{"bot_network_envelope", f.BotEnvelope})
			_ = s.send(map[string]any{"type": "bot_network_ack", "request_id": f.Header.RequestID, "header_digest": hd, "frame_digest": fd, "sender": machine, "target": f.Header.Target.MachineID, "status": "relay_accepted", "accepted_at": time.Now().UnixMilli(), "delivered_at": nil})
		case "bot_network_delivery_ack":
			var ack map[string]any
			_ = json.Unmarshal(raw, &ack)
			signature, _ := base64.StdEncoding.DecodeString(ack["signature"].(string))
			body := map[string]any{"account_id": "synthetic-account", "request_id": ack["request_id"], "header_digest": ack["header_digest"], "frame_digest": ack["frame_digest"], "delivery_id": ack["delivery_id"]}
			b, _ := json.Marshal(body)
			v, _ := domaincloud.Parse(b)
			if !ed25519.Verify(key.PublicKey(), append([]byte("clawdline-bot-network-delivery-v1\n"), domaincloud.CanonicalBytes(v)...), signature) {
				c.t.Error("forged target ACK")
				return
			}
			c.acks.Add(1)
		}
	}
}
func TestBotCloudNormalSourceSpineAndRevocation(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "original-pair", true: "reverse-pair"}[reverse], func(t *testing.T) { testBotCloudNormalSourceSpine(t, reverse) })
	}
}
func testBotCloudNormalSourceSpine(t *testing.T, reverse bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	c := &spineCloud{t: t, keys: map[string]domaincloud.DeviceKey{}, sockets: map[string]*spineSocket{}, caps: map[string]cloud.BotCapabilities{}}
	backend := httptest.NewServer(c)
	defer backend.Close()
	fixtures := []*paServerFixture{newPAServerFixture(t, true), newPAServerFixture(t, true)}
	var endpoints []botwire.BotEndpoint
	var pairs []peerstore.Pair
	signers := []ed25519.PrivateKey{}
	privates := []*ecdh.PrivateKey{}
	for i, f := range fixtures {
		machine := []string{"source", "target"}[i]
		f.identity.MachineID = machine
		f.identity.MachineCredential = machine
		f.identity.APIBase = backend.URL
		f.key.MachineID = machine
		key, _ := domaincloud.NewDeviceKey(rand.Reader)
		c.keys[machine] = key
		signers = append(signers, ed25519.NewKeyFromSeed(key.Seed()))
		x, _ := ecdh.X25519().GenerateKey(rand.Reader)
		privates = append(privates, x)
		keys, e := cloudkeys.Open(f.s.cfg.Dir)
		if e != nil {
			t.Fatal(e)
		}
		if e = keys.SaveDeviceKey(key); e != nil {
			t.Fatal(e)
		}
		if e = cloud.NewIdentityStore(keys.Dir()).Save(f.identity); e != nil {
			t.Fatal(e)
		}
		_, e = f.s.settingsFile().Set(map[string]any{cloud.KeyEnabled: true, cloud.KeyAPIBase: backend.URL, cloud.KeyRelayURL: "ws" + strings.TrimPrefix(backend.URL, "http"), "project_assistants_enabled": false})
		if e != nil {
			t.Fatal(e)
		}
		exe, e := os.Executable()
		if e != nil {
			t.Fatal(e)
		}
		f.s.projectAssistants.provider = func() (app.Provider, error) {
			if os.Getenv("CLAWDLINE_BOT_NETWORK_NATIVE_PROOF") != "1" {
				return paServerProvider{capability: func(b pa.Bot) (pa.Capabilities, error) { c := paServerCaps(b); c.SendMessage = true; return c, nil }, open: func(context.Context, app.OpenRequest) (app.ProviderSession, error) {
					return &fallbackHTTPSession{send: func(app.PromptRequest) (app.PendingPrompt, error) { return fallbackHTTPPending{}, nil }}, nil
				}}, nil
			}
			return acp.NewProvider(acp.ProviderOptions{Configurations: map[pa.Agent]acp.Configuration{pa.Codex: {Executable: exe, Arguments: []string{"-test.run=^TestBotCloudOwnedACP$"}, Environment: []string{"CLAWDLINE_BOT_CLOUD_ACP=1", "GORACE=atexit_sleep_ms=0"}}}, Limits: acp.Limits{RPCPending: 8, Notifications: 32, FrameBytes: 1 << 20, CaptureBytes: 1 << 20, StderrBytes: 1024, OwnedDescendants: 64, WriteTimeout: time.Second, CleanupTimeout: 2 * time.Second, RPCTimeout: 10 * time.Second, DescendantPoll: 10 * time.Millisecond}, Verify: func(_ context.Context, b pa.Bot, _ acp.Configuration) (pa.Capabilities, error) {
				caps := paServerCaps(b)
				caps.SendMessage = true
				return caps, nil
			}})
		}
		if e = f.s.StartProjectAssistants(ctx); e != nil {
			t.Fatal(e)
		}
		var tables int
		_ = f.s.store.ProjectAssistantDatabase().QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name LIKE 'project_assistant_network_%'`).Scan(&tables)
		if tables != 0 || f.providerCalls.Load() != 0 {
			t.Fatal("explicitly disabled startup opened Bot network state or provider", tables)
		}
		f.enable(t)
		paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
		got := f.get(t, f.local)
		endpoints = append(endpoints, botwire.BotEndpoint{AccountID: f.identity.AccountID, MachineID: machine, ProjectID: got.Bot.Project.ProjectID, BotID: f.key.BotID, ConversationID: got.Conversation.ID, Generation: got.Conversation.Generation})
	}
	enc := base64.StdEncoding.EncodeToString
	a, b := 0, 1
	if reverse {
		a, b = b, a
	}
	p := botwire.PairTranscript{PairID: "pair", SourceMachineID: endpoints[a].MachineID, TargetMachineID: endpoints[b].MachineID, SourcePublicKey: enc(c.keys[endpoints[a].MachineID].PublicKey()), TargetPublicKey: enc(c.keys[endpoints[b].MachineID].PublicKey()), SourceEncryptionKey: enc(privates[a].PublicKey().Bytes()), TargetEncryptionKey: enc(privates[b].PublicKey().Bytes()), SourceFingerprint: c.keys[endpoints[a].MachineID].Fingerprint(), TargetFingerprint: c.keys[endpoints[b].MachineID].Fingerprint()}
	p.SourceOfferSignature = botwire.SignOffer(p, signers[a])
	p.TargetAcceptSignature = botwire.SignAcceptance(p, signers[b])
	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	accepted := time.Now().UTC().Format(time.RFC3339)
	c.pair = cloud.PeerPair{PairID: p.PairID, SourceMachineID: p.SourceMachineID, TargetMachineID: p.TargetMachineID, SourcePublicKey: p.SourcePublicKey, TargetPublicKey: p.TargetPublicKey, SourceKeyFingerprint: p.SourceFingerprint, TargetKeyFingerprint: p.TargetFingerprint, SourceEncryptionKey: p.SourceEncryptionKey, TargetEncryptionKey: &p.TargetEncryptionKey, SourceOfferSignature: p.SourceOfferSignature, TargetAcceptSignature: &p.TargetAcceptSignature, AcceptedAt: &accepted, ExpiresAt: expiry.Format(time.RFC3339)}
	c.consent = cloud.BotConsent{ID: "consent", AccountID: "synthetic-account", PairID: "pair", Epoch: 1, Source: endpoints[0], Target: endpoints[1], Kinds: []string{"directory", "message", "read"}, CreatedAt: time.Now().Unix() - 1, ExpiresAt: expiry.Unix()}
	for i, f := range fixtures {
		n := f.s.projectAssistants.network
		if n == nil {
			t.Fatal("normal host omitted source composition")
		}
		_, authority, e := n.active(ctx)
		if e != nil {
			t.Fatal(e)
		}
		pp := peerstore.Pair{ID: p.PairID, AccountID: f.identity.AccountID, SourceMachineID: p.SourceMachineID, TargetMachineID: p.TargetMachineID, SourcePublicKey: p.SourcePublicKey, TargetPublicKey: p.TargetPublicKey, SourceFingerprint: p.SourceFingerprint, TargetFingerprint: p.TargetFingerprint, SourceEncryptionKey: p.SourceEncryptionKey, TargetEncryptionKey: p.TargetEncryptionKey, SourceSignature: p.SourceOfferSignature, TargetSignature: p.TargetAcceptSignature, LocalPrivateKey: privates[i].Bytes(), ExpiresAt: expiry}
		pairs = append(pairs, pp)
		fp := p.TargetFingerprint
		if f.identity.MachineID == p.TargetMachineID {
			fp = p.SourceFingerprint
		}
		if e = authority.Pairs.PinPair(ctx, pp, f.identity.MachineID, fp); e != nil {
			t.Fatal(e)
		}
		socket, e := cloud.New(cloud.Options{RelayURL: "ws" + strings.TrimPrefix(backend.URL, "http"), Identity: f.identity, Signer: c.keys[f.identity.MachineID], Token: &cloud.TokenSource{Client: cloud.NewAccountClient(backend.URL), Credential: f.identity.MachineCredential}, Replay: domaincloud.NewReplayWindow(0)})
		if e != nil {
			t.Fatal(e)
		}
		done := f.s.StartBotNetwork(ctx, socket)
		transportDone := make(chan struct{})
		go func() { defer close(transportDone); _ = socket.Run(ctx) }()
		defer func() { cancel(); socket.Stop(); <-done; <-transportDone }()
	}
	wait := func(condition func() bool) {
		t.Helper()
		for !condition() {
			select {
			case <-ctx.Done():
				t.Fatal("source spine timed out", ctx.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	for _, f := range fixtures {
		n := f.s.projectAssistants.network
		wait(func() bool {
			return f.s.botNetworkCapabilities(ctx, n.identity.AccountID, n.identity.MachineID).Peer.State == pa.NetworkReady
		})
		got := f.get(t, f.local)
		on := true
		_, e := featurestore.New(f.s.store.ProjectAssistantDatabase()).SaveNetworkSettings(ctx, pa.NetworkSettingsWrite{Scope: pa.NetworkScope{AccountID: n.identity.AccountID, Key: f.key, Project: paDomainProjectScope(got.Bot.Project)}, Fence: paDomainFence(contract.ProjectAssistantFence{Key: got.Bot.Key, Project: got.Bot.Project, ConversationID: got.Conversation.ID, BindingID: got.Conversation.BindingID, Generation: got.Conversation.Generation}), ExpectedBotVersion: got.Bot.Version, Patch: pa.NetworkSettingsPatch{OfferedToNetwork: &on}}, pa.NetworkCapabilities{Peer: pa.NetworkCapability{State: pa.NetworkReady}})
		if e != nil {
			t.Fatal(e)
		}
		for _, token := range []string{f.machine, f.lan, f.readonly} {
			w := f.request(t, "POST", "/v1/bot-network/consents/consent/sync", token, nil)
			if w.Code == 200 {
				t.Fatal("nonlocal manager installed consent")
			}
		}
		paServerOK(t, f.request(t, "POST", "/v1/bot-network/consents/consent/sync", f.local, nil))
		paServerOK(t, f.request(t, "POST", "/v1/bot-network/consents/consent/sync", f.local, nil))
		if bad := f.request(t, "POST", "/v1/bot-network/consents/consent/sync", f.local, map[string]any{"grant": "arbitrary"}); bad.Code == 200 {
			t.Fatal("manager installed a supplied arbitrary grant")
		}
	}
	source, target := fixtures[0], fixtures[1]
	n := source.s.projectAssistants.network
	// Actual capability leases bootstrap complete directory captures. Repeated
	// observations have distinct identities and never reuse message effects.
	wait(func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.caps["source"].Endpoints) == 1 && len(c.caps["target"].Endpoints) == 1
	})
	for i := 0; i < 2; i++ {
		routes, err := n.network.Routes(ctx, endpoints[0], source.s.botNetworkTransport)
		if err != nil || len(routes.Routes) != 1 || routes.Routes[0].Target != endpoints[1] {
			t.Fatalf("normal source directory: %+v %v", routes, err)
		}
	}
	call := appnet.Call{RequestID: "native-message", Kind: "message", AuthorityID: "consent", Source: endpoints[0], Target: endpoints[1], Text: "Synthetic Bot message over dedicated Cloud socket"}
	rail, e := source.s.botNetworkTransport(ctx, "target")
	if e != nil {
		t.Fatal(e)
	}
	result, e := n.network.Call(ctx, call, rail)
	if e != nil || result.Receipt == nil {
		t.Fatalf("normal source message: %+v %v", result, e)
	}
	if result.Receipt.State != "accepted" {
		t.Fatal("not accepted", result)
	}
	var stored app.Export
	wait(func() bool {
		var err error
		stored, err = featurestore.New(target.s.store.ProjectAssistantDatabase()).Export(ctx, target.key)
		if err == nil && len(stored.Turns) == 1 && (stored.Turns[0].State == pa.TurnFailed || stored.Turns[0].State == pa.TurnUnknown) {
			t.Fatalf("native source retained terminal failure (no replay): %+v", stored.Turns[0])
		}
		return err == nil && len(stored.Turns) == 1 && stored.Turns[0].State == pa.TurnSucceeded
	})
	if stored.Conversation.Generation <= endpoints[1].Generation {
		t.Fatal("native generation did not advance")
	}
	read := call
	read.RequestID = "native-read"
	read.CorrelationID = call.RequestID
	read.Kind = "read"
	read.Text = ""
	result, e = n.network.Call(ctx, read, rail)
	wantReply := "Scripted final answer"
	if os.Getenv("CLAWDLINE_BOT_NETWORK_NATIVE_PROOF") == "1" {
		wantReply = "Synthetic Cloud reply"
	}
	if e != nil || result.Receipt == nil || result.Receipt.State != "reply_stored" || result.Receipt.Reply != wantReply || !result.Receipt.Read {
		t.Fatalf("correlated native reply after generation advance: %+v %v", result, e)
	}
	if c.acks.Load() < 4 {
		t.Fatal("target signed transport ACKs missing", c.acks.Load())
	}
	if _, e = n.network.Call(ctx, call, rail); pa.Code(e) != pa.IdempotencyConflict && pa.Code(e) != pa.ExecutionEffectUnknown {
		t.Fatal("ambiguous effect replayed", e)
	}
	source.s.StartCapacity(ctx)
	wait(func() bool { b := source.s.capacity(); b.mu.Lock(); defer b.mu.Unlock(); return b.passes > 0 })
	panelResponse := source.request(t, "GET", "/v1/capacity", source.local, nil)
	paServerOK(t, panelResponse)
	var panel contract.CapacityPanel
	if e = json.Unmarshal(panelResponse.Body.Bytes(), &panel); e != nil {
		t.Fatal(e)
	}
	occupancy := entry(t, panel.Capacity, "bots.cloud_receipts")
	if occupancy.Used == nil || *occupancy.Used != 8 {
		t.Fatal("HTTP capacity lost durable transport facts", occupancy)
	}
	stale := call
	stale.RequestID = "stale-target-new-message"
	if refused, err := n.network.Call(ctx, stale, rail); err != nil || refused.Receipt == nil || refused.Receipt.State != "refused" || refused.Receipt.TurnID != "" || refused.Receipt.Failure == nil || refused.Receipt.Failure.Code != pa.StaleGeneration {
		t.Fatalf("new work reused a prior target generation: %+v %v", refused.Receipt, err)
	}
	// Local preference revocation is independent of the API's current allow.
	targetStore := featurestore.New(target.s.store.ProjectAssistantDatabase())
	scope := pa.NetworkScope{AccountID: target.identity.AccountID, Key: target.key, Project: stored.Bot.Project}
	preferences, e := targetStore.NetworkSettings(ctx, scope)
	if e != nil {
		t.Fatal(e)
	}
	off := false
	_, e = targetStore.SaveNetworkSettings(ctx, pa.NetworkSettingsWrite{Scope: scope, Fence: pa.Fence{Key: target.key, Project: stored.Bot.Project, ConversationID: stored.Conversation.ID, BindingID: stored.Conversation.BindingID, Generation: stored.Conversation.Generation}, ExpectedBotVersion: stored.Bot.Version, ExpectedRevision: preferences.Revision, Patch: pa.NetworkSettingsPatch{OfferedToNetwork: &off}}, pa.NetworkCapabilities{})
	if e != nil {
		t.Fatal(e)
	}
	if e = target.s.projectAssistants.network.network.CheckRequest(ctx, result.Receipt.Header); pa.Code(e) != pa.Revoked {
		t.Fatal("local preference revoke bypassed effect boundary", e)
	}
	c.mu.Lock()
	c.revoked = true
	c.mu.Unlock()
	denied := call
	denied.RequestID = "revoked-message"
	if _, e = n.network.Call(ctx, denied, rail); pa.Code(e) != pa.Revoked {
		t.Fatal("live API revoke was bypassed", e)
	}
	c.mu.Lock()
	revokedAt := time.Now().Unix()
	c.consent.RevokedAt = &revokedAt
	c.consent.Epoch = 3
	c.mu.Unlock()
	paServerOK(t, source.request(t, "POST", "/v1/bot-network/consents/consent/sync", source.local, nil))
	g, e := n.Grant(ctx, "consent")
	if e != nil || !g.Revoked || g.Epoch != 3 {
		t.Fatal("missed revocation epoch was not retained", g, e)
	}
	c.mu.Lock()
	c.consent.RevokedAt = nil
	c.consent.Epoch = 4
	c.mu.Unlock()
	if resurrect := source.request(t, "POST", "/v1/bot-network/consents/consent/sync", source.local, nil); resurrect.Code == 200 {
		t.Fatal("revoked consent resurrected")
	}
	var effects int
	final, e := featurestore.New(target.s.store.ProjectAssistantDatabase()).Export(ctx, target.key)
	if e != nil {
		t.Fatal(e)
	}
	effects = len(final.Turns)
	if effects != 1 {
		t.Fatal("more than one durable effect", effects)
	}
	paServerOK(t, source.request(t, "POST", "/v1/settings/project-assistants", source.local, map[string]bool{"enabled": false}))
	wait(func() bool { n.mu.Lock(); defer n.mu.Unlock(); return n.rail == nil })
	source.enable(t)
	wait(func() bool {
		return source.s.botNetworkCapabilities(ctx, n.identity.AccountID, n.identity.MachineID).Peer.State == pa.NetworkReady
	})
	t.Logf("normal Server/Host x2; real TCP API adapter and dedicated WebSocket; effects=%d signed target ACKs=%d live authorization requests=%d model requests=0 native ACP=%t; private API/Relay are simulated", effects, c.acks.Load(), c.authorizations.Load(), os.Getenv("CLAWDLINE_BOT_NETWORK_NATIVE_PROOF") == "1")
}
