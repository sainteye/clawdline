package http

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Exercise the actual authenticated TCP entry point with a scripted provider.
// No installed vendor executable or model request is reachable from this fixture.
func TestProjectAssistantFallbackNetworkKeepsOneAcceptedInput(t *testing.T) {
	f := newPAServerFixture(t, true)
	var opens, sends, closes atomic.Int32
	var primaryClosed atomic.Bool
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		return paServerProvider{open: func(_ context.Context, q app.OpenRequest) (app.ProviderSession, error) {
			opens.Add(1)
			s := &fallbackHTTPSession{send: func(p app.PromptRequest) (app.PendingPrompt, error) {
				sends.Add(1)
				if q.Bot.Agent != pa.Codex || p.Handoff == nil || p.Handoff.Agent != pa.Codex {
					t.Error("alternate lost its effective identity or filtered handoff")
				}
				return fallbackHTTPPending{}, nil
			}, close: func() error {
				closes.Add(1)
				if q.Bot.Agent == pa.ClaudeCode {
					primaryClosed.Store(true)
				}
				return nil
			}}
			if q.Bot.Agent == pa.ClaudeCode {
				return s, fallbackUsageRefusal()
			}
			if !primaryClosed.Load() {
				t.Error("alternate opened before primary cleanup")
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
	create.Settings.FallbackAgents = []contract.ProjectAssistantAgent{contract.ProjectAssistantAgentCodex}
	if h.request(t, "POST", "/v1/project-assistants", create, nil) != 200 {
		t.Fatal("create failed")
	}
	command, admission := h.send(t, "network-first")
	saved := fallbackHTTPWait(t, f)
	var exported contract.ProjectAssistantExport
	if h.request(t, "GET", "/v1/project-assistants/"+f.key.BotID+"/export?machine_id="+f.key.MachineID, nil, &exported) != 200 {
		t.Fatal("export failed")
	}
	if len(saved.Turns) != 1 || saved.Turns[0].ID != admission.ResourceID || saved.Turns[0].State != pa.TurnSucceeded || fallbackAttemptStages(saved.Turns[0].Attempts) != fallbackRefusedThenAnswered {
		t.Fatalf("logical turn changed: %+v", saved.Turns)
	}
	for _, attempt := range saved.Turns[0].Attempts {
		if attempt.Fence.Key != saved.Bot.Key || attempt.Fence.Project != saved.Bot.Project || attempt.Fence.ConversationID != saved.Conversation.ID {
			t.Fatal("attempt escaped the accepted scope")
		}
	}
	counts := map[pa.MessageKind]int{}
	for _, m := range saved.Messages {
		counts[m.Kind]++
	}
	if counts[pa.UserMessage] != 1 || counts[pa.AssistantMessage] != 1 || counts[pa.NoticeMessage] != 1 {
		t.Fatalf("messages: %+v", counts)
	}
	var replay contract.ProjectAssistantReceiptResponse
	if h.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/messages", command, &replay) != 200 || replay.Receipt != admission {
		t.Fatal("accepted command replay changed its receipt")
	}
	if exported.SchemaVersion != int64(pa.SchemaVersion) || opens.Load() != 2 || sends.Load() != 1 || closes.Load() != 2 {
		t.Fatalf("schema=%d open/send/close=%d/%d/%d", exported.SchemaVersion, opens.Load(), sends.Load(), closes.Load())
	}
	t.Logf("real TCP endpoint: one admitted input, one alternate answer, one switch notice; schema=%d open/send/close=%d/%d/%d; native/model requests=0", exported.SchemaVersion, opens.Load(), sends.Load(), closes.Load())
}
