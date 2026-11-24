package http

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func settingsRebaseBase(b contract.ProjectAssistantBot) *contract.ProjectAssistantEditBase {
	return &contract.ProjectAssistantEditBase{Name: b.Name, Appearance: b.Appearance, Instructions: b.Instructions.Text, Agent: b.Agent, PermissionPolicy: b.PermissionPolicy, Mode: b.Mode}
}

func settingsRebaseSave(t *testing.T, f *paServerFixture, route, id string, settings contract.ProjectAssistantEditSettings) contract.ProjectAssistantBotResponse {
	t.Helper()
	w := f.request(t, "PATCH", route, f.local, contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, id), Settings: settings})
	paServerOK(t, w)
	return paHTTPDecode[contract.ProjectAssistantBotResponse](t, w)
}

// The person's report: the Bot replied while its standing instructions were
// being edited, and Save answered "changed elsewhere" although no setting had.
func TestSettingsSaveSucceedsAfterTheBotRepliesHTTP(t *testing.T) {
	f, _ := memorySettingsFixture(t)
	route := "/v1/project-assistants/" + f.key.BotID
	opened := f.get(t, f.local).Bot
	paServerOK(t, f.send(t, f.local, "reply-while-editing"))
	fallbackHTTPWait(t, f)
	replied := f.get(t, f.local).Bot
	if replied.Version <= opened.Version || replied.Instructions.Text != opened.Instructions.Text {
		t.Fatalf("the turn did not advance only the version: %d -> %d", opened.Version, replied.Version)
	}
	draft := contract.ProjectAssistantEditSettings{ExpectedVersion: opened.Version, Name: opened.Name, Appearance: opened.Appearance, Instructions: "Answer in short sentences."}
	// An older console sends no base; it keeps the whole-Bot version check.
	paAssertHTTPFailure(t, f.request(t, "PATCH", route, f.local, contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, "older-console"), Settings: draft}), 409, pa.StaleVersion)
	draft.Base = settingsRebaseBase(opened)
	saved := settingsRebaseSave(t, f, route, "save-after-reply", draft)
	if saved.Bot.Instructions.Text != "Answer in short sentences." || saved.Bot.Version != replied.Version+1 {
		t.Fatalf("saved %+v", saved.Bot)
	}
	// Another editor still holding the opened settings changes the same field.
	stale := contract.ProjectAssistantEditSettings{ExpectedVersion: opened.Version, Name: "Renamed", Appearance: opened.Appearance, Instructions: "A different draft", Base: settingsRebaseBase(opened)}
	refused := paAssertHTTPFailure(t, f.request(t, "PATCH", route, f.local, contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, "stale-draft"), Settings: stale}), 409, pa.StaleVersion)
	if refused.Field != pa.SettingsConflictPrefix+pa.ConflictInstructions {
		t.Fatalf("conflict field = %q", refused.Field)
	}
	stale.OverwriteConflicts = true
	kept := settingsRebaseSave(t, f, route, "keep-mine", stale)
	if kept.Bot.Instructions.Text != "A different draft" || kept.Bot.Name != "Renamed" {
		t.Fatalf("overwrite saved %+v", kept.Bot)
	}
}

// TestSettingsRebaseBrowser drives the production settings editor against
// this real handler, store and synthetic runtime, and keeps screenshots.
func TestSettingsRebaseBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_SETTINGS_REBASE_BROWSER_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_SETTINGS_REBASE_BROWSER_DIR for the real synthetic browser")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f, _ := memorySettingsFixture(t)
	server := httptest.NewServer(f.h)
	defer server.Close()
	cmd := exec.Command("node", "../../../web/console/tools/project-assistants-settings-rebase.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
	log, err := cmd.CombinedOutput()
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), log, 0600)
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, log)
	}
}
