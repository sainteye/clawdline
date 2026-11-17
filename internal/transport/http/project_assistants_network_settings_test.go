package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func networkSettingsFixture(t *testing.T) *paServerFixture {
	t.Helper()
	f := newPAServerFixture(t, true)
	f.enable(t)
	paServerOK(t, f.request(t, http.MethodPost, "/v1/project-assistants", f.local, f.create()))
	return f
}

func TestNetworkSettingsCrossAuthenticatedHTTPHostAndSQLite(t *testing.T) {
	f := networkSettingsFixture(t)
	g := f.get(t, f.local)
	path := "/v1/project-assistants/" + f.key.BotID + "/network-settings"
	read := f.request(t, http.MethodGet, path+"?machine_id="+f.key.MachineID+"&conversation_id="+g.Conversation.ID, f.local, nil)
	paServerOK(t, read)
	var got contract.ProjectAssistantNetworkSettingsResponse
	if err := json.Unmarshal(read.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Network.Settings.Revision != 0 || got.Network.Settings.OfferedToNetwork || got.Network.Capabilities.Peer.State != "unavailable" {
		t.Fatal(read.Body.String())
	}
	fence := got.Network.Fence
	request := func(revision int64, patch map[string]bool) contract.ProjectAssistantNetworkSettingsRequest {
		converted := contract.ProjectAssistantNetworkSettingsPatch{}
		if value, ok := patch["offered_to_network"]; ok {
			converted.OfferedToNetwork = &contract.ProjectAssistantNetworkPreferenceValue{Enabled: value}
		}
		if value, ok := patch["reachable_by_assistants"]; ok {
			converted.ReachableByAssistants = &contract.ProjectAssistantNetworkPreferenceValue{Enabled: value}
		}
		return contract.ProjectAssistantNetworkSettingsRequest{Command: f.command(pa.NetworkSettingsOperation, "network-preference"), Fence: fence, ExpectedVersion: got.Network.BotVersion, ExpectedRevision: revision, Patch: converted}
	}
	for _, token := range []string{"", f.readonly} {
		r := f.request(t, http.MethodPost, path, token, request(0, map[string]bool{"offered_to_network": false}))
		if r.Code < 400 {
			t.Fatal("unauthorized preference write", r.Code)
		}
	}
	saved := f.request(t, http.MethodPost, path, f.local, request(0, map[string]bool{"offered_to_network": false}))
	paServerOK(t, saved)
	if err := json.Unmarshal(saved.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Network.Settings.Revision != 1 {
		t.Fatal(saved.Body.String())
	}
	refused := f.request(t, http.MethodPost, path, f.local, request(1, map[string]bool{"offered_to_network": true}))
	if refused.Code != http.StatusNotImplemented {
		t.Fatal("unconfigured network must refuse enable", refused.Code, refused.Body.String())
	}
	empty := f.request(t, http.MethodPost, path, f.local, request(1, map[string]bool{}))
	paServerOK(t, empty)
	var after contract.ProjectAssistantNetworkSettingsResponse
	if err := json.Unmarshal(empty.Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	if after.Network.Settings.Revision != 1 || after.Network.Settings.OfferedToNetwork {
		t.Fatal("omission changed preference", empty.Body.String())
	}
	stale := f.request(t, http.MethodPost, path, f.local, request(0, map[string]bool{"offered_to_network": false}))
	if stale.Code < 400 {
		t.Fatal("stale setting revision accepted")
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("management started native provider")
	}
}

func TestNetworkSettingsBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_NETWORK_BROWSER_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_NETWORK_BROWSER_DIR for owned production network settings browser")
	}
	f := networkSettingsFixture(t)
	server := httptest.NewServer(f.h)
	defer server.Close()
	cmd := exec.Command("node", "../../../web/console/tools/project-assistants-network-settings.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_READ_TOKEN="+f.readonly, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
	output, err := cmd.CombinedOutput()
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), output, 0600)
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, output)
	}
}
