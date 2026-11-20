package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	botwire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The real private API and workerd provide only a task-owned synthetic fixture.
func TestAssistantCloudPrivateSource(t *testing.T) {
	path := os.Getenv("CLAWDLINE_ASSISTANT_CLOUD_PRIVATE_FIXTURE")
	if path == "" {
		t.Skip("synthetic private API and workerd fixture required")
	}
	var input struct {
		Synthetic bool                  `json:"synthetic"`
		Identity  cloud.Identity        `json:"identity"`
		Seed      string                `json:"seed"`
		Policy    assistantSourcePolicy `json:"policy"`
		Relay     string                `json:"relay_url"`
		Control   string                `json:"control_url"`
	}
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &input) != nil || !input.Synthetic || !input.Identity.Valid() {
		t.Fatal("invalid synthetic source fixture")
	}
	for _, origin := range []string{input.Identity.APIBase, input.Relay, input.Control} {
		u, err := url.Parse(origin)
		if err != nil || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			t.Fatal("source and harness must remain on loopback")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	f := newPAServerFixture(t, true)
	f.identity = input.Identity
	f.key.MachineID = f.identity.MachineID
	seed, err := base64.StdEncoding.Strict().DecodeString(input.Seed)
	if err != nil {
		t.Fatal(err)
	}
	key, err := domaincloud.DeviceKeyFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := cloudkeys.Open(f.s.cfg.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = keys.SaveDeviceKey(key); err != nil {
		t.Fatal(err)
	}
	if err = cloud.NewIdentityStore(keys.Dir()).Save(f.identity); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.settingsFile().Set(map[string]any{cloud.KeyEnabled: true, cloud.KeyAPIBase: f.identity.APIBase, cloud.KeyRelayURL: input.Relay}); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(f.s.cfg.Dir, assistantSourcePolicyFile)
	policyBytes, err := json.Marshal(input.Policy)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(policyPath, policyBytes, 0600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		return acp.NewProvider(acp.ProviderOptions{Configurations: map[pa.Agent]acp.Configuration{pa.Codex: {
			Executable: exe, Arguments: []string{"-test.run=^TestBotCloudOwnedACP$"},
			Environment: []string{"CLAWDLINE_BOT_CLOUD_ACP=1", "GORACE=atexit_sleep_ms=0"},
		}}, Limits: acp.Limits{RPCPending: 8, Notifications: 32, FrameBytes: 1 << 20, CaptureBytes: 1 << 20, StderrBytes: 1024, OwnedDescendants: 64, WriteTimeout: time.Second, CleanupTimeout: 2 * time.Second, RPCTimeout: 10 * time.Second, DescendantPoll: 10 * time.Millisecond}, Verify: func(_ context.Context, b pa.Bot, _ acp.Configuration) (pa.Capabilities, error) {
			c := paServerCaps(b)
			c.SendMessage = true
			return c, nil
		}})
	}
	if err = f.s.StartProjectAssistants(ctx); err != nil {
		t.Fatal(err)
	}
	f.enable(t)
	paServerOK(t, f.request(t, http.MethodPost, "/v1/project-assistants", f.local, f.create()))
	export := f.get(t, f.local)
	target := botwire.BotEndpoint{AccountID: f.identity.AccountID, MachineID: f.key.MachineID, ProjectID: export.Bot.Project.ProjectID,
		BotID: f.key.BotID, ConversationID: export.Conversation.ID, Generation: export.Conversation.Generation}
	checkCloud := func(stage string) {
		t.Helper()
		targetBytes, _ := json.Marshal(target)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, input.Control+"/"+stage, bytes.NewReader(targetBytes))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("private OAuth/MCP %s matrix refused: %d", stage, resp.StatusCode)
		}
	}
	path = "/v1/project-assistants/" + f.key.BotID + "/network-settings"
	getSwitches := func() contract.ProjectAssistantNetworkSettingsResponse {
		t.Helper()
		r := f.request(t, http.MethodGet, path+"?machine_id="+f.key.MachineID+"&conversation_id="+export.Conversation.ID, f.local, nil)
		paServerOK(t, r)
		var settings contract.ProjectAssistantNetworkSettingsResponse
		if err := json.Unmarshal(r.Body.Bytes(), &settings); err != nil {
			t.Fatal(err)
		}
		return settings
	}
	if getSwitches().Network.Capabilities.Assistants.State != "unavailable" {
		t.Fatal("local opt-in alone must not mark the lane ready")
	}
	checkCloud("offline")
	socket, err := cloud.New(cloud.Options{RelayURL: input.Relay, Identity: f.identity, Signer: key,
		Token:  &cloud.TokenSource{Client: cloud.NewAccountClient(f.identity.APIBase), Credential: f.identity.MachineCredential},
		Replay: domaincloud.NewReplayWindow(0), Log: func(line string) { t.Log(line) }})
	if err != nil {
		t.Fatal(err)
	}
	done, transportDone := f.s.StartBotNetwork(ctx, socket), make(chan struct{})
	go func() { defer close(transportDone); _ = socket.Run(ctx) }()
	defer func() { cancel(); socket.Stop(); <-done; <-transportDone }()
	var switches contract.ProjectAssistantNetworkSettingsResponse
	for {
		switches = getSwitches()
		if switches.Network.Capabilities.Assistants.State == "ready" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("machine did not negotiate the assistant rail")
		case <-time.After(25 * time.Millisecond):
		}
	}
	r := f.request(t, http.MethodPost, path, f.local, contract.ProjectAssistantNetworkSettingsRequest{
		Command: f.command(pa.NetworkSettingsOperation, "assistant-network-preference"), Fence: switches.Network.Fence,
		ExpectedVersion: switches.Network.BotVersion, ExpectedRevision: switches.Network.Settings.Revision,
		Patch: contract.ProjectAssistantNetworkSettingsPatch{
			ReachableByAssistants: &contract.ProjectAssistantNetworkPreferenceValue{Enabled: true},
		},
	})
	paServerOK(t, r)
	checkCloud("online")
	x, err := featurestore.New(f.s.store.ProjectAssistantDatabase()).Export(ctx, f.key)
	if err != nil || len(x.Turns) != 1 || x.Turns[0].State != pa.TurnSucceeded || x.Turns[0].Origin != pa.NetworkRequestOrigin ||
		x.Turns[0].Network == nil || x.Turns[0].Network.Assistant == nil {
		t.Fatalf("expected exactly one assistant-origin native effect, persisted in SQLite: turns=%d err=%v", len(x.Turns), err)
	}
	if err = os.Remove(policyPath); err != nil {
		t.Fatal(err)
	}
	if getSwitches().Network.Capabilities.Assistants.State != "unavailable" {
		t.Fatal("source opt-out must immediately remove availability")
	}
	t.Log("real NetworkSwitches GET/POST; one assistant-origin native ACP turn; source opt-out; external model requests=0")
}
