package http

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Optional browser driver uses the installed Clawdline CLI, never a model/native adapter.
func TestProjectAssistantFallbackSettingsBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_FALLBACK_SETTINGS_E2E_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_FALLBACK_SETTINGS_E2E_DIR for the owned browser acceptance")
	}
	f := newPAServerFixture(t, true)
	var opens, sends, closes atomic.Int32
	var cleaned atomic.Bool
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		return paServerProvider{open: func(_ context.Context, q app.OpenRequest) (app.ProviderSession, error) {
			opens.Add(1)
			s := &fallbackHTTPSession{send: func(p app.PromptRequest) (app.PendingPrompt, error) {
				sends.Add(1)
				if q.Bot.Agent != pa.Codex || p.Handoff == nil || p.Handoff.Agent != pa.Codex {
					t.Error("alternate lost effective identity")
				}
				return fallbackHTTPPending{}, nil
			}, close: func() error {
				closes.Add(1)
				if q.Bot.Agent == pa.ClaudeCode {
					cleaned.Store(true)
				}
				return nil
			}}
			if q.Bot.Agent == pa.ClaudeCode {
				return s, fallbackUsageRefusal()
			}
			if !cleaned.Load() {
				t.Error("alternate preceded cleanup")
			}
			return s, nil
		}}, nil
	}
	h := &paSoakHTTP{fixture: f, server: httptest.NewServer(f.h)}
	h.reconnect()
	defer h.server.Close()
	defer h.client.CloseIdleConnections()
	if h.request(t, "POST", "/v1/settings/project-assistants", map[string]bool{"enabled": true}, nil) != 200 {
		t.Fatal("enable failed")
	}
	create := f.create()
	create.Settings.Agent = contract.ProjectAssistantAgentClaude
	if h.request(t, "POST", "/v1/project-assistants", create, nil) != 200 {
		t.Fatal("create failed")
	}
	cmd := exec.Command("node", "../../../web/console/tools/project-assistants-fallback-settings.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+h.server.URL, "BOTS_TOKEN="+f.local, "BOTS_READONLY_TOKEN="+f.readonly, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
	log, err := cmd.CombinedOutput()
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), log, 0600)
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, log)
	}
	saved := fallbackHTTPWait(t, f)
	counts := map[pa.MessageKind]int{}
	for _, m := range saved.Messages {
		counts[m.Kind]++
	}
	if saved.Bot.Agent != pa.ClaudeCode || len(saved.Bot.FallbackAgents.Agents()) != 1 || saved.Bot.FallbackAgents.Agents()[0] != pa.Codex || len(saved.Turns) != 1 || len(saved.Turns[0].Attempts) != 6 || counts[pa.UserMessage] != 1 || counts[pa.AssistantMessage] != 1 || counts[pa.NoticeMessage] != 1 || opens.Load() != 2 || sends.Load() != 1 || closes.Load() != 2 {
		t.Fatalf("primary=%s turns=%d messages=%v open/send/close=%d/%d/%d", saved.Bot.Agent, len(saved.Turns), counts, opens.Load(), sends.Load(), closes.Load())
	}
	t.Log("real browser + TCP + Host + SQLite: primary unchanged; one user/turn/alternate answer/notice; Open/Send/Close=2/1/2; installed-native/vendor/model/API requests=0")
}
