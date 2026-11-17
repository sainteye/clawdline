package http

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type templateFixtureProvider struct{ paServerProvider }

func (templateFixtureProvider) Configuration(_ context.Context, b pa.Bot, _, _ string) (pa.ConfigurationCatalog, error) {
	return pa.ConfigurationCatalog{Agent: b.Agent, Model: "synthetic-model", Options: []pa.ConfigOption{
		{ID: "effort", Category: "thought_level", Choices: []pa.ConfigChoice{{Value: "high", Name: "High"}}},
		{ID: "fast", Choices: []pa.ConfigChoice{{Value: "false", Name: "Off"}}},
	}}, nil
}

func TestProjectAssistantTemplateBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_TEMPLATE_BROWSER_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_TEMPLATE_BROWSER_DIR for the owned template browser")
	}
	f := newPAServerFixture(t, true)
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		f.providerCalls.Add(1)
		return templateFixtureProvider{paServerProvider{open: func(context.Context, app.OpenRequest) (app.ProviderSession, error) {
			t.Error("template fixture must never open an execution session")
			return nil, paHTTPFailure(pa.Unsupported, "template")
		}}}, nil
	}
	f.enable(t)
	create := f.create()
	create.Settings.Name = "Synthetic template Bot"
	create.Settings.Instructions = "Synthetic configured instructions.\n</textarea><script>window.templateExecuted=true</script>"
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, create))
	g := f.get(t, f.local)
	edit := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, "template-fixture-settings"), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, Notify: "off", RuntimeSettings: &contract.ProjectAssistantRuntimeSettings{Model: "synthetic-model", Options: map[string]string{"effort": "high", "fast": "false"}}}}
	paServerOK(t, f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, edit))
	before := fallbackHTTPWait(t, f)
	providerCalls := f.providerCalls.Load()
	server := httptest.NewServer(f.h)
	defer server.Close()
	cmd := exec.Command("node", "../../../web/console/tools/project-assistants-template.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_READ_TOKEN="+f.readonly, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
	output, err := cmd.CombinedOutput()
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), output, 0600)
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, output)
	}
	after := fallbackHTTPWait(t, f)
	if !reflect.DeepEqual(before, after) || f.providerCalls.Load() != providerCalls {
		t.Fatal("template read/copy mutated durable Bot state or opened a provider")
	}
	t.Log("Real HTTP/Host/SQLite + production Chat menu + native browser clipboard; durable state unchanged; provider/model/session mutations=0")
}
