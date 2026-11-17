package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The provider is local protocol data, never an installed CLI or a model call.
type alignmentProvider struct {
	mu      sync.Mutex
	answers []string
}

func (p *alignmentProvider) Capabilities(_ context.Context, bot pa.Bot) (pa.Capabilities, error) {
	return paServerCaps(bot), nil
}
func (p *alignmentProvider) Open(_ context.Context, _ app.OpenRequest, emit func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	return &alignmentSession{provider: p, emit: emit, answered: make(chan string, 1)}, nil
}

type alignmentSession struct {
	provider *alignmentProvider
	emit     func(context.Context, app.ProtocolEvent) error
	answered chan string
}

func (*alignmentSession) SessionID() string { return "synthetic-alignment-session" }
func (s *alignmentSession) Send(_ context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	return alignmentPending{s, q}, nil
}
func (s *alignmentSession) Answer(_ context.Context, _ string, option string) error {
	s.provider.mu.Lock()
	s.provider.answers = append(s.provider.answers, option)
	s.provider.mu.Unlock()
	s.answered <- option
	return nil
}
func (*alignmentSession) Cancel(context.Context) error { return nil }
func (*alignmentSession) Close(context.Context) error  { return nil }

type alignmentPending struct {
	session *alignmentSession
	prompt  app.PromptRequest
}

func (p alignmentPending) Wait(ctx context.Context) (app.PromptResult, error) {
	if !strings.HasPrefix(p.prompt.Text, "approval ") {
		return app.PromptResult{FinalText: "Synthetic answer for " + p.prompt.Text}, nil
	}
	turn := p.prompt.Turn
	offer := pa.PermissionOffer{ID: "approval-" + turn.ID, Fence: turn.Fence, TurnID: turn.ID, NativeRequestID: "native-" + turn.ID, ExpiresAt: time.Now().Add(time.Hour), Action: &pa.PermissionAction{Title: "Edit synthetic.ts", ToolKind: "edit", Detail: "Complete synthetic request: replace the old value and retain the final line.", Diffs: []pa.PermissionDiff{{Path: "synthetic.ts", OldText: "const value = 1", NewText: "const value = 2\n// final detail line"}}}, Options: []pa.PermissionOption{{ID: "native-deny", Kind: pa.RejectOnce, Label: "Deny native edit"}, {ID: "native-always", Kind: pa.AllowAlways, Allow: true, Label: "Allow edits in synthetic project"}, {ID: "native-once", Kind: pa.AllowOnce, Allow: true, Label: "Allow this native edit"}, {ID: "native-never", Kind: pa.RejectAlways, Label: "Never allow this native edit"}}}
	if err := p.session.emit(ctx, app.ProtocolEvent{ID: "permission-" + turn.ID, Fence: turn.Fence, TurnID: turn.ID, Permission: &offer}); err != nil {
		return app.PromptResult{}, err
	}
	select {
	case option := <-p.session.answered:
		return app.PromptResult{FinalText: "Synthetic native answer: " + option}, nil
	case <-ctx.Done():
		return app.PromptResult{}, ctx.Err()
	}
}

func TestProjectAssistantUIAlignmentBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_UI_ALIGNMENT_BROWSER_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_UI_ALIGNMENT_BROWSER_DIR to run the owned UI browser fixture")
	}
	_ = os.MkdirAll(dir, 0700)
	f := newPAServerFixture(t, true)
	provider := &alignmentProvider{}
	f.s.projectAssistants.provider = func() (app.Provider, error) { return provider, nil }
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	paServerOK(t, f.send(t, f.local, "seed-conversation"))
	fallbackHTTPWait(t, f)
	server := httptest.NewServer(f.h)
	defer server.Close()
	groups := newGroupHTTPFixture(t)
	group := groups.create(t)
	// More than one production history page, with actual member turns and authors.
	for index := 0; index < 35; index++ {
		status, body := groups.request(t, "POST", "/v1/project-assistants/rooms/groups/room/messages", "person", contract.ProjectAssistantGroupSendRequest{Command: groups.command(pa.SendOperation, fmt.Sprintf("seed-room-%d", index)), Text: fmt.Sprintf("Synthetic room question %d", index)})
		if status != 200 {
			t.Fatalf("seed room: %d %s", status, body)
		}
		groups.rooms.Idle()
	}
	var bots []contract.ProjectAssistantBot
	for _, bot := range groups.bots[:2] {
		wire, err := paWireBot(bot)
		if err != nil {
			t.Fatal(err)
		}
		bots = append(bots, wire)
	}
	groupJSON, _ := json.Marshal(group)
	botsJSON, _ := json.Marshal(bots)
	cmd := exec.Command("node", "../../../web/console/tools/project-assistants-ui-alignment.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_READ_TOKEN="+f.readonly, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir, "GROUP_ORIGIN="+groups.server.URL, "ALIGN_GROUP="+string(groupJSON), "ALIGN_BOTS="+string(botsJSON))
	output, err := cmd.CombinedOutput()
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), output, 0600)
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, output)
	}
	provider.mu.Lock()
	answers := append([]string{}, provider.answers...)
	provider.mu.Unlock()
	if strings.Join(answers, ",") != "native-once,native-always,native-deny" {
		t.Fatalf("native option identities: %v", answers)
	}
	got := f.get(t, f.local)
	if got.Bot.Name != "Synthetic saved name" {
		t.Fatalf("settings did not persist: %s", got.Bot.Name)
	}
	t.Log("Production Chat/GroupRoom/PermissionCard -> authenticated TCP -> Host -> SQLite; exact native choices and settings persisted; vendor/model/API requests=0")
}
