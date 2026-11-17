package projectassistanthost

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	transport "github.com/sainteye/clawdline/internal/adapters/botnetwork"
	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	network "github.com/sainteye/clawdline/internal/app/botnetwork"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestAssistantSignedHTTPActualNativeACPAndStoredReply(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native fixture requires owned process supervisor")
	}
	ctx := context.Background()
	f := newHostFixture(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := filepath.Join(t.TempDir(), "assistant-native")
	f.o.Limits.PromptTimeout = 20 * time.Second
	f.o.Provider = func() (app.Provider, error) {
		return acp.NewProvider(acp.ProviderOptions{
			Configurations: map[pa.Agent]acp.Configuration{pa.Codex: {Executable: exe, Arguments: []string{"-test.run=^TestNetworkNativePeer$"}, Environment: []string{"CLAWDLINE_NETWORK_FIXTURE=b", "CLAWDLINE_NETWORK_RECEIPT=" + diagnostics, "GORACE=atexit_sleep_ms=0"}}},
			Limits:         acp.Limits{RPCPending: 8, IncomingPermissions: 4, Notifications: 32, FrameBytes: 1 << 20, CaptureBytes: 1 << 20, StderrBytes: 1024, OwnedDescendants: 64, WriteTimeout: time.Second, CleanupTimeout: 2 * time.Second, RPCTimeout: 15 * time.Second, DescendantPoll: 10 * time.Millisecond},
			Verify: func(_ context.Context, bot pa.Bot, _ acp.Configuration) (pa.Capabilities, error) {
				return pa.Capabilities{Key: bot.Key, Project: bot.Project, Agent: bot.Agent, CLIVersion: "synthetic", AdapterVersion: "synthetic", Evidence: "local ACP only", ReadOnlyEnforced: true, NativeMemory: pa.NativeMemoryDisabled, NativeMemoryVerified: true, SendMessage: true}, nil
			},
		})
	}
	f.start(t)
	f.enable(t)
	service, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	bot, _, err := service.Create(ctx, pa.Command{Key: pa.BotKey{MachineID: f.o.MachineID, BotID: "assistant-target"}, Operation: pa.CreateOperation, IdempotencyKey: "create-assistant-target"}, pa.CreateBot{Target: pa.ProjectSelection{MachineID: f.o.MachineID, ProjectID: "project"}, Name: "Synthetic assistant target", Agent: pa.Codex, Appearance: pa.Appearance{Shape: pa.Blob, Color: "#e39567"}, PermissionPolicy: pa.Ask, Instructions: "Read only"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.h.store.Conversation(ctx, bot.Key, bot.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	on := true
	_, err = f.h.store.SaveNetworkSettings(ctx, pa.NetworkSettingsWrite{Scope: pa.NetworkScope{AccountID: "synthetic-account", Key: bot.Key, Project: bot.Project}, Fence: pa.Fence{Key: bot.Key, Project: bot.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}, ExpectedBotVersion: bot.Version, Patch: pa.NetworkSettingsPatch{ReachableByAssistants: &on}}, pa.NetworkCapabilities{Assistants: pa.NetworkCapability{State: pa.NetworkReady}})
	if err != nil {
		t.Fatal(err)
	}
	store, err := transport.NewAssistantStore(ctx, f.o.Database, "synthetic-account", f.o.MachineID)
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x66}, 32))
	now := time.Now().UTC().Truncate(time.Second)
	grant := wire.AssistantGrant{ID: "synthetic-assistant-consent", Epoch: 1, KeyID: "synthetic-gateway-key", PublicKey: key.Public().(ed25519.PublicKey), PrincipalID: "synthetic-external-assistant", AccountID: "synthetic-account", MachineID: f.o.MachineID, ProjectID: bot.Project.ProjectID, BotIDs: []string{bot.Key.BotID}, AllowList: true, AllowMessage: true, AllowRead: true, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	if err = store.PutAssistantPolicy(ctx, network.AssistantPolicy{AccountID: grant.AccountID, MachineID: grant.MachineID, Version: 1, GlobalOptIn: true, GatewayKeyID: grant.KeyID, GatewayPublicKey: grant.PublicKey}, 0); err != nil {
		t.Fatal(err)
	}
	if err = store.PutAssistantGrant(ctx, grant, 0); err != nil {
		t.Fatal(err)
	}
	assistant, err := network.NewAssistant(network.AssistantOptions{AccountID: grant.AccountID, MachineID: grant.MachineID, Repository: store, Host: NetworkHost{Host: f.h, AccountID: grant.AccountID}, Clock: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	f.h.o.Assistants = assistant
	server := httptest.NewServer(transport.AssistantHandler{Service: assistant})
	defer server.Close()
	// Whitespace is intentional: the captured native authority must retain exact
	// signed body bytes instead of reconstructing a different JSON encoding.
	body := []byte("{ \"text\" : \"Synthetic external assistant review\" }")
	call := wire.AssistantCall{Version: wire.BotWireVersion, RequestID: "assistant-native-request", Kind: "message", Target: wire.BotEndpoint{AccountID: grant.AccountID, MachineID: grant.MachineID, ProjectID: grant.ProjectID, BotID: bot.Key.BotID, ConversationID: bot.ConversationID, Generation: c.Generation}, GrantID: grant.ID, GrantEpoch: grant.Epoch, KeyID: grant.KeyID, BodyDigest: network.Digest(body), IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()}
	post := func(q wire.AssistantCall, b []byte) network.AssistantResponse {
		t.Helper()
		q.BodyDigest = network.Digest(b)
		signed, e := wire.SignAssistantCall(q, key)
		if e != nil {
			t.Fatal(e)
		}
		encoded, _ := json.Marshal(transport.AssistantRequest{Call: signed, Body: string(b)})
		r, e := server.Client().Post(server.URL+"/v1/bot-assistant/call", "application/json", bytes.NewReader(encoded))
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		raw, e := io.ReadAll(r.Body)
		if e != nil {
			t.Fatal(e)
		}
		if r.StatusCode != http.StatusOK {
			t.Fatalf("assistant HTTP status=%d body=%s", r.StatusCode, raw)
		}
		var out network.AssistantResponse
		if e = json.Unmarshal(raw, &out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	accepted := post(call, body)
	replay := post(call, body)
	if accepted.Receipt == nil || replay.Receipt == nil || accepted.Receipt.TurnID != replay.Receipt.TurnID {
		t.Fatalf("fixed-operation acceptance %+v %+v", accepted, replay)
	}
	var final network.AssistantResponse
	deadline := time.Now().Add(20 * time.Second)
	for i := 0; time.Now().Before(deadline); i++ {
		read := call
		read.Kind = "read"
		read.RequestID = fmt.Sprintf("assistant-native-read-%d", i)
		read.CorrelationID = call.RequestID
		final = post(read, []byte(`{}`))
		if final.Receipt != nil && (final.Receipt.State == "reply_stored" || final.Receipt.State == "failed" || final.Receipt.State == "unknown") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if final.Receipt == nil || final.Receipt.Reply != "Synthetic network reply" || !final.Receipt.Read {
		phase, _ := os.ReadFile(diagnostics + ".b.phase")
		x, _ := f.h.store.Export(ctx, bot.Key)
		t.Fatalf("native reply %+v turns=%+v phase=%s", final.Receipt, x.Turns, phase)
	}
	x, err := f.h.store.Export(ctx, bot.Key)
	if err != nil || len(x.Turns) != 1 || x.Turns[0].Origin != pa.NetworkRequestOrigin || x.Turns[0].Network == nil || x.Turns[0].Network.Assistant == nil || x.Turns[0].Network.Assistant.PrincipalID != grant.PrincipalID || x.Turns[0].Network.Assistant.Body != string(body) {
		t.Fatalf("native principal/capture %+v %v", x.Turns, err)
	}
	grants, receipts, err := store.AssistantUsage(ctx)
	if err != nil || grants != 1 || receipts != 1 {
		t.Fatalf("durable assistant usage %d %d %v", grants, receipts, err)
	}
	t.Logf("request=%s principal=%s native_turn=%s reply_state=%s read=%t native_executions=1 retained_receipts=%d external_requests=0", call.RequestID, grant.PrincipalID, final.Receipt.TurnID, final.Receipt.State, final.Receipt.Read, receipts)
}
