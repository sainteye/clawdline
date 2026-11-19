package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func rosterEdit(bot pa.Bot, token string, changes contract.ProjectAssistantRosterEdit) contract.ProjectAssistantEditRequest {
	return contract.ProjectAssistantEditRequest{
		Command: contract.ProjectAssistantCommand{Key: paWireBotKey(bot.Key), Operation: "edit", IdempotencyKey: token},
		Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: bot.Version, Name: bot.Name,
			Appearance: paWireAppearance(bot.Appearance), Instructions: bot.Instructions.Text, Roster: &changes},
	}
}

func TestBotSidebarLeadReplacesLeadWithoutMovingProject(t *testing.T) {
	f := newGroupHTTPFixture(t)
	alice, bob := f.bots[0], f.bots[1]
	edit := func(bot pa.Bot, token string, changes contract.ProjectAssistantRosterEdit) pa.Bot {
		t.Helper()
		status, raw := f.request(t, http.MethodPatch, "/v1/project-assistants/"+bot.Key.BotID, "person", rosterEdit(bot, token, changes))
		if status != http.StatusOK {
			t.Fatalf("edit: %d %s", status, raw)
		}
		next, err := f.repo.Bot(t.Context(), bot.Key)
		if err != nil {
			t.Fatal(err)
		}
		return next
	}
	alice = edit(alice, "pin-and-section", contract.ProjectAssistantRosterEdit{
		Pinned: "pinned", Section: &contract.ProjectAssistantSidebarSection{Name: "  Research  "},
	})
	alice = edit(alice, "lead-alice", contract.ProjectAssistantRosterEdit{DesignateLead: true})
	if !alice.Lead || alice.Hidden || alice.SidebarSection != "Research" || alice.SidebarPinned == nil || !*alice.SidebarPinned {
		t.Fatal(alice)
	}
	status, _ := f.request(t, http.MethodPost, "/v1/project-assistants/alice/visibility", "person", contract.ProjectAssistantVisibilityRequest{
		Command: contract.ProjectAssistantCommand{Key: paWireBotKey(alice.Key), Operation: "visibility", IdempotencyKey: "hide-lead"}, Hidden: true,
	})
	if status != http.StatusForbidden {
		t.Fatalf("hide lead: %d", status)
	}
	bob = edit(bob, "lead-bob", contract.ProjectAssistantRosterEdit{DesignateLead: true})
	previous, err := f.repo.Bot(t.Context(), alice.Key)
	if err != nil || previous.Lead || !bob.Lead || previous.Project != alice.Project || previous.ConversationID != alice.ConversationID || previous.SidebarSection != "Research" {
		t.Fatalf("lead replacement: %v %v", previous, err)
	}
}

func TestBotNewSessionUsesFreshProviderContextAndKeepsHistory(t *testing.T) {
	provider := &routineHumanProvider{}
	f := newGroupHTTPFixture(t, provider)
	bot := f.bots[0]
	base := "/v1/project-assistants/" + bot.Key.BotID
	send := func(id, text string) {
		t.Helper()
		chat, err := f.repo.Conversation(t.Context(), bot.Key, bot.ConversationID)
		if err != nil {
			t.Fatal(err)
		}
		q := contract.ProjectAssistantSendRequest{Command: contract.ProjectAssistantCommand{Key: paWireBotKey(bot.Key), Operation: "send", IdempotencyKey: id}, Text: text,
			Fence: paWireFence(pa.Fence{Key: bot.Key, Project: bot.Project, ConversationID: chat.ID, BindingID: chat.BindingID, Generation: chat.Generation})}
		status, raw := f.request(t, http.MethodPost, base+"/messages", "person", q)
		if status != http.StatusOK {
			t.Fatalf("send: %d %s", status, raw)
		}
		var receipt contract.ProjectAssistantReceiptResponse
		if err := json.Unmarshal(raw, &receipt); err != nil {
			t.Fatal(err)
		}
		until := time.Now().Add(3 * time.Second)
		for time.Now().Before(until) {
			turn, err := f.repo.Turn(t.Context(), bot.Key, receipt.Receipt.ResourceID)
			if err == nil && turn.State == pa.TurnSucceeded {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("synthetic turn did not finish")
	}
	send("before-boundary", "Synthetic previous turn")
	bot, err := f.repo.Bot(t.Context(), bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	status, raw := f.request(t, http.MethodPatch, base, "person", rosterEdit(bot, "reset-context", contract.ProjectAssistantRosterEdit{FreshContext: true}))
	if status != http.StatusOK {
		t.Fatalf("reset: %d %s", status, raw)
	}
	send("after-boundary", "Synthetic fresh turn")
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.opens) != 2 || provider.opens[1].Resume || len(provider.sends) != 2 || strings.Contains(provider.sends[1], "Synthetic previous turn") {
		t.Fatal("new native session carried old conversation context")
	}
	x, err := f.repo.Export(t.Context(), bot.Key)
	if err != nil || x.Conversation.ID != bot.ConversationID || len(x.Messages) != 4 || x.Messages[0].Text != "Synthetic previous turn" {
		t.Fatalf("conversation was not retained: %v", err)
	}
}
