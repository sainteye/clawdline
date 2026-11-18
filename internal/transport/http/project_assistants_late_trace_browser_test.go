package http

import (
	"context"
	"encoding/json"
	"fmt"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type lateTraceBrowserProvider struct{}

func (lateTraceBrowserProvider) Capabilities(_ context.Context, b pa.Bot) (pa.Capabilities, error) {
	return paServerCaps(b), nil
}
func (lateTraceBrowserProvider) Open(_ context.Context, _ app.OpenRequest, emit func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	return &fallbackHTTPSession{send: func(q app.PromptRequest) (app.PendingPrompt, error) {
		return lateTraceBrowserPending{q: q, emit: emit}, nil
	}}, nil
}

type lateTraceBrowserPending struct {
	q    app.PromptRequest
	emit func(context.Context, app.ProtocolEvent) error
}

func (p lateTraceBrowserPending) Wait(ctx context.Context) (app.PromptResult, error) {
	short := p.q.Text == "Return the short synthetic reply."
	count := 6
	if short {
		count = 2
	}
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("synthetic-%s-%d", p.q.Turn.ID, i)
		category, text := "tool_call", "Synthetic tool status\ncompleted fixture read"
		if short {
			category = "agent_message_chunk"
			text = "Synthetic short reply ready."
			if i == 0 {
				category = "agent_thought_chunk"
				text = "Synthetic completed short-answer observation"
			}
		}
		trace := pa.Trace{ID: id, TurnID: p.q.Turn.ID, Category: category, Text: text, Complete: true}
		if err := p.emit(ctx, app.ProtocolEvent{ID: id, Fence: p.q.Turn.Fence, TurnID: p.q.Turn.ID, Trace: &trace}); err != nil {
			return app.PromptResult{}, err
		}
	}
	if short {
		return app.PromptResult{FinalText: "Synthetic short reply ready."}, nil
	}
	return app.PromptResult{FinalText: "The synthetic fixture completed successfully. Its stable response provides enough conversation history to exercise a narrow viewport without invoking a native provider."}, nil
}
func TestProjectAssistantLateTraceBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_LATE_TRACE_BROWSER_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_LATE_TRACE_BROWSER_DIR for the isolated browser regression")
	}
	f := newPAServerFixture(t, true)
	f.s.projectAssistants.provider = func() (app.Provider, error) { f.providerCalls.Add(1); return lateTraceBrowserProvider{}, nil }
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	for i, text := range []string{"Read the synthetic fixture and report its completed status.", "Return the short synthetic reply."} {
		got := f.get(t, f.local)
		fence := pa.Fence{Key: f.key, Project: paDomainProjectScope(got.Bot.Project), ConversationID: got.Conversation.ID, BindingID: got.Conversation.BindingID, Generation: got.Conversation.Generation}
		q := contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, fmt.Sprintf("synthetic-seed-%d", i)), Fence: paWireFence(fence), Text: text}
		paServerOK(t, f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/messages", f.local, q))
		fallbackHTTPWait(t, f)
	}
	before := fallbackHTTPWait(t, f)
	if len(before.Messages) != 4 || len(before.Turns) != 2 {
		t.Fatal("incorrect synthetic history")
	}
	calls := f.providerCalls.Load()
	seed, _ := json.MarshalIndent(map[string]any{"messages": before.Messages, "turns": before.Turns, "events": before.Events}, "", "  ")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "synthetic-source.json"), seed, 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(f.h)
	defer server.Close()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "node", "../../../web/console/tools/project-assistants-late-trace.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
	out, err := cmd.CombinedOutput()
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), out, 0600)
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	after := fallbackHTTPWait(t, f)
	if f.providerCalls.Load() != calls || len(after.Messages) != 4 || len(after.Turns) != 2 {
		t.Fatal("probe changed history or invoked provider")
	}
	t.Log(string(out))
}
