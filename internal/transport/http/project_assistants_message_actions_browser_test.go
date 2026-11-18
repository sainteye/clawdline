package http

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Source-owned history and reactions use real HTTP/Host/SQLite; the provider is scripted.
func TestProjectAssistantMessageActionsBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_MESSAGE_ACTIONS_BROWSER_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_MESSAGE_ACTIONS_BROWSER_DIR and PLAYWRIGHT_MODULE")
	}
	f := reactionHTTPFixture(t, true)
	for i := 0; i < 3; i++ {
		got := f.get(t, f.local)
		fence := pa.Fence{Key: f.key, Project: paDomainProjectScope(got.Bot.Project), ConversationID: got.Conversation.ID, BindingID: got.Conversation.BindingID, Generation: got.Conversation.Generation}
		q := contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, "synthetic-long-"+string(rune('a'+i))), Fence: paWireFence(fence), Text: strings.Repeat("Synthetic message for stable geometry and readable actions. ", 8)}
		paServerOK(t, f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/messages", f.local, q))
		fallbackHTTPWait(t, f)
	}
	calls := f.providerCalls.Load()
	server := httptest.NewServer(f.h)
	defer server.Close()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "../../../web/console/tools/project-assistants-message-actions.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
	out, err := cmd.CombinedOutput()
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), out, 0600)
	if err != nil {
		t.Fatalf("message actions: %v\n%s", err, out)
	}
	if f.providerCalls.Load() != calls {
		t.Fatal("message controls triggered provider work")
	}
	t.Log(string(out))
}
