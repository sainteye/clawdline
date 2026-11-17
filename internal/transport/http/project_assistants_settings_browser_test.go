package http

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
)

// The native peer is a local scripted ACP process. No installed CLI or model is used.
func TestProjectAssistantSettingsBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_SETTINGS_BROWSER_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_SETTINGS_BROWSER_DIR for the owned settings browser")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f, record := memorySettingsFixture(t)
	t.Cleanup(func() {
		if data, err := os.ReadFile(record); err == nil {
			_ = os.WriteFile(filepath.Join(dir, "protocol.jsonl"), data, 0600)
		}
	})
	server := httptest.NewServer(f.h)
	defer server.Close()
	cmd := exec.Command("node", "../../../web/console/tools/project-assistants-settings.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_READ_TOKEN="+f.readonly, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
	output, err := cmd.CombinedOutput()
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), output, 0600)
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, output)
	}
	got := f.get(t, f.local)
	if got.Bot.Instructions.Text != "Synthetic saved instructions zh-Hant" {
		t.Fatalf("saved instructions: %+v", got.Bot.Instructions)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "protocol.jsonl"), data, 0600)
	if !strings.Contains(string(data), "Synthetic saved instructions zh-Hant") {
		t.Fatal("saved standing instructions did not reach the next ACP session")
	}
	if !strings.Contains(string(data), `"value":true`) {
		t.Fatal("selected runtime did not reach native configuration")
	}
	if _, err := store.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key); err != nil {
		t.Fatal(err)
	}
	t.Log("Production Chat settings -> authenticated HTTP/Host/SQLite -> scripted ACP; desktop/mobile; external model requests=0")
}
