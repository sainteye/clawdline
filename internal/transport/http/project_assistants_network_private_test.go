package http

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	cloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/peerstore"
	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	appnet "github.com/sainteye/clawdline/internal/app/botnetwork"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	botwire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Root supplies only the task-owned synthetic fixture produced by the retained
// real Mongo/Fastify harness. This test never reads installed production keys.
// Its RelayURL must name the real local workerd service, not spineCloud above.
func TestBotCloudPrivateSourceSpine(t *testing.T) {
	path := os.Getenv("CLAWDLINE_BOT_NETWORK_PRIVATE_FIXTURE")
	if path == "" {
		t.Skip("Root-owned real private API/Mongo/workerd fixture required")
	}
	var fixture struct {
		Synthetic  bool   `json:"synthetic"`
		AccountID  string `json:"account_id"`
		APIBase    string `json:"api_base"`
		RelayURL   string `json:"relay_url"`
		ControlURL string `json:"control_url"`
		Sources    []struct {
			Identity cloud.Identity `json:"identity"`
			Seed     string         `json:"seed"`
			Pair     peerstore.Pair `json:"pair"`
		} `json:"sources"`
	}
	raw, e := os.ReadFile(path)
	if e != nil || json.Unmarshal(raw, &fixture) != nil || !fixture.Synthetic || len(fixture.Sources) != 2 {
		t.Fatal("invalid task-owned synthetic fixture")
	}
	for _, origin := range []string{fixture.APIBase, fixture.RelayURL, fixture.ControlURL} {
		u, e := url.Parse(origin)
		if e != nil || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			t.Fatal("real-service fixture must remain on task-owned loopback")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var fixtures []*paServerFixture
	var endpoints []botwire.BotEndpoint
	wait := func(check func() bool) {
		t.Helper()
		for !check() {
			select {
			case <-ctx.Done():
				t.Fatal("real private source verification pending", ctx.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	for _, source := range fixture.Sources {
		f := newPAServerFixture(t, true)
		fixtures = append(fixtures, f)
		f.identity = source.Identity
		f.key.MachineID = f.identity.MachineID
		if !f.identity.Valid() || f.identity.AccountID != fixture.AccountID || f.identity.APIBase != fixture.APIBase {
			t.Fatal("fixture identity mismatch")
		}
		seed, e := base64.StdEncoding.Strict().DecodeString(source.Seed)
		if e != nil {
			t.Fatal(e)
		}
		key, e := domaincloud.DeviceKeyFromSeed(seed)
		if e != nil {
			t.Fatal(e)
		}
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
		if _, e = f.s.settingsFile().Set(map[string]any{cloud.KeyEnabled: true, cloud.KeyAPIBase: fixture.APIBase, cloud.KeyRelayURL: fixture.RelayURL}); e != nil {
			t.Fatal(e)
		}
		f.s.projectAssistants.provider = func() (app.Provider, error) {
			return acp.NewProvider(acp.ProviderOptions{Configurations: map[pa.Agent]acp.Configuration{pa.Codex: {Executable: exe, Arguments: []string{"-test.run=^TestBotCloudOwnedACP$"}, Environment: []string{"CLAWDLINE_BOT_CLOUD_ACP=1", "GORACE=atexit_sleep_ms=0"}}}, Limits: acp.Limits{RPCPending: 8, IncomingPermissions: 4, Notifications: 32, FrameBytes: 1 << 20, CaptureBytes: 1 << 20, StderrBytes: 1024, OwnedDescendants: 64, WriteTimeout: time.Second, CleanupTimeout: 2 * time.Second, RPCTimeout: 10 * time.Second, DescendantPoll: 10 * time.Millisecond}, Verify: func(_ context.Context, b pa.Bot, _ acp.Configuration) (pa.Capabilities, error) {
				c := paServerCaps(b)
				c.SendMessage = true
				return c, nil
			}})
		}
		if e = f.s.StartProjectAssistants(ctx); e != nil {
			t.Fatal(e)
		}
		f.enable(t)
		paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
		got := f.get(t, f.local)
		endpoints = append(endpoints, botwire.BotEndpoint{AccountID: fixture.AccountID, MachineID: f.key.MachineID, ProjectID: got.Bot.Project.ProjectID, BotID: f.key.BotID, ConversationID: got.Conversation.ID, Generation: got.Conversation.Generation})
		n := f.s.projectAssistants.network
		if n == nil {
			t.Fatal("ordinary Host omitted real source composition")
		}
		_, authority, e := n.active(ctx)
		if e != nil {
			t.Fatal(e)
		}
		fp := source.Pair.TargetFingerprint
		if f.key.MachineID == source.Pair.TargetMachineID {
			fp = source.Pair.SourceFingerprint
		}
		if e = authority.Pairs.PinPair(ctx, source.Pair, f.key.MachineID, fp); e != nil {
			t.Fatal(e)
		}
		socket, e := cloud.New(cloud.Options{RelayURL: fixture.RelayURL, Identity: f.identity, Signer: key, Token: &cloud.TokenSource{Client: cloud.NewAccountClient(fixture.APIBase), Credential: f.identity.MachineCredential}, Replay: domaincloud.NewReplayWindow(0)})
		if e != nil {
			t.Fatal(e)
		}
		done := f.s.StartBotNetwork(ctx, socket)
		transportDone := make(chan struct{})
		go func() { defer close(transportDone); _ = socket.Run(ctx) }()
		defer func() { cancel(); socket.Stop(); <-done; <-transportDone }()
		wait(func() bool {
			return f.s.botNetworkCapabilities(ctx, fixture.AccountID, f.key.MachineID).Peer.State == pa.NetworkReady
		})
		on := true
		_, e = featurestore.New(f.s.store.ProjectAssistantDatabase()).SaveNetworkSettings(ctx, pa.NetworkSettingsWrite{Scope: pa.NetworkScope{AccountID: fixture.AccountID, Key: f.key, Project: paDomainProjectScope(got.Bot.Project)}, Fence: pa.Fence{Key: f.key, Project: paDomainProjectScope(got.Bot.Project), ConversationID: got.Conversation.ID, BindingID: got.Conversation.BindingID, Generation: got.Conversation.Generation}, ExpectedBotVersion: got.Bot.Version, Patch: pa.NetworkSettingsPatch{OfferedToNetwork: &on}}, pa.NetworkCapabilities{Peer: pa.NetworkCapability{State: pa.NetworkReady}})
		if e != nil {
			t.Fatal(e)
		}
	}
	control := func(action string, body any) {
		t.Helper()
		b, _ := json.Marshal(body)
		req, e := http.NewRequestWithContext(ctx, "POST", fixture.ControlURL+action, bytes.NewReader(b))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode != 200 {
			t.Fatal("real fixture manager refused", action, resp.StatusCode)
		}
	}
	// Only the fixture's real human session creates consent. Neither source
	// machine bearer nor this local sync endpoint can establish it.
	for i, f := range fixtures {
		wait(func() bool {
			caps, e := f.s.projectAssistants.network.client.Capabilities(ctx, f.key.MachineID)
			return e == nil && len(caps.Endpoints) == 1 && caps.Endpoints[0] == endpoints[i]
		})
	}
	control("/consent", map[string]any{"source": endpoints[0], "target": endpoints[1]})
	for _, f := range fixtures {
		paServerOK(t, f.request(t, "POST", "/v1/bot-network/consents/consent/sync", f.local, nil))
	}
	a, b := fixtures[0], fixtures[1]
	transport, e := a.s.botNetworkTransport(ctx, b.key.MachineID)
	if e != nil {
		t.Fatal(e)
	}
	var id [16]byte
	_, _ = rand.Read(id[:])
	request := appnet.Call{RequestID: base64.RawURLEncoding.EncodeToString(id[:]), Kind: "message", AuthorityID: "consent", Source: endpoints[0], Target: endpoints[1], Text: "Synthetic native source request through real private services"}
	result, e := a.s.projectAssistants.network.network.Call(ctx, request, transport)
	if e != nil || result.Receipt == nil {
		t.Fatalf("real source request: %+v %v", result, e)
	}
	wait(func() bool {
		x, e := featurestore.New(b.s.store.ProjectAssistantDatabase()).Export(ctx, b.key)
		if e == nil && len(x.Turns) == 1 && (x.Turns[0].State == pa.TurnUnknown || x.Turns[0].State == pa.TurnFailed) {
			t.Fatalf("actual native result remains unknown: %+v", x.Turns[0])
		}
		return e == nil && len(x.Turns) == 1 && x.Turns[0].State == pa.TurnSucceeded
	})
	read := request
	read.Kind = "read"
	read.RequestID = "read-" + request.RequestID
	read.CorrelationID = request.RequestID
	read.Text = ""
	result, e = a.s.projectAssistants.network.network.Call(ctx, read, transport)
	if e != nil || result.Receipt == nil || result.Receipt.State != "reply_stored" || result.Receipt.Reply != "Synthetic Cloud reply" {
		t.Fatalf("real correlated completion: %+v %v", result, e)
	}
	control("/revoke", map[string]any{})
	request.RequestID = "revoked-" + request.RequestID
	if _, e = a.s.projectAssistants.network.network.Call(ctx, request, transport); pa.Code(e) != pa.Revoked {
		t.Fatal("real private revocation did not stop source", e)
	}
	t.Log("two normal sources; real machine API and workerd rail; one durable native ACP effect and original-capture stored reply; real human revoke; external model requests=0")
}
