package http

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/sainteye/clawdline/internal/adapters/botcapabilities"
	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/squad"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConnectorCredentialsBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_CONNECTOR_BROWSER_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_CONNECTOR_BROWSER_DIR for owned production settings browser")
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	f, _ := loginCapabilitiesFixture(t, func(original app.Provider) app.Provider {
		provider, e := acp.NewProvider(acp.ProviderOptions{Configurations: map[pa.Agent]acp.Configuration{pa.Codex: {Executable: exe, Arguments: []string{"-test.run=^TestExtensionsNativeProtocolPeer$", "--", "extensions-native"}}}, Limits: acp.Limits{RPCPending: 16, Notifications: 32, FrameBytes: 1 << 20, CaptureBytes: 4 << 20, StderrBytes: 64 << 10, OwnedDescendants: 32, WriteTimeout: time.Second, CleanupTimeout: 5 * time.Second, RPCTimeout: 10 * time.Second, DescendantPoll: 10 * time.Millisecond}, Verify: func(_ context.Context, b pa.Bot, _ acp.Configuration) (pa.Capabilities, error) {
			return paServerCaps(b), nil
		}})
		if e != nil {
			t.Fatal(e)
		}
		return provider
	})
	g := f.get(t, f.local)
	skills := []squad.Skill{{SkillID: "owned-folder", Folder: true, Content: "# Owned fixture\nRead assets/canary.bin.", Files: []squad.SkillFile{{Path: "assets/canary.bin", ContentBase64: base64.StdEncoding.EncodeToString(extensionNativeCanary)}}, Name: squad.Names{En: "Owned folder fixture"}}}
	raw, _ := json.Marshal(skills)
	if e = os.WriteFile(filepath.Join(f.s.cfg.Dir, "synthetic-installed-skill.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	definitions, _ := json.Marshal([]botcapabilities.Connector{{ID: "owned-stdio", Name: "fixture-connector", Executable: exe, Arguments: []string{"-test.run=^TestExtensionsNativeProtocolPeer$", "--", "extensions-connector"}, Project: paDomainProjectScope(g.Bot.Project), Agent: pa.Codex, Enabled: true, CredentialEnv: "MCP_CONNECTOR_TOKEN"}})
	if e = os.WriteFile(filepath.Join(f.s.cfg.Dir, "project-assistants", "connectors.json"), definitions, 0600); e != nil {
		t.Fatal(e)
	}

	server := httptest.NewServer(f.h)
	defer server.Close()
	cmd := exec.Command("node", "../../../web/console/tools/project-assistants-connector-credentials.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_READ_TOKEN="+f.readonly, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
	output, err := cmd.CombinedOutput()
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), output, 0600)
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, output)
	}
	x := fallbackHTTPWait(t, f)
	count := 0
	for _, m := range x.Messages {
		if strings.Contains(m.Text, "Owned folder binary and stdio connector read through native ACP.") {
			count++
		}
	}
	if count != 2 {
		t.Fatal("both locale actions did not reach owned native folder/stdio entry", count, x.Turns)
	}
}
