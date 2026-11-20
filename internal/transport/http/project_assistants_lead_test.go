package http

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestPersonalBotWorkspaceIsSourceOwnedAndRetryStable(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	request := f.create()
	request.Settings.Target.ProjectID = ""
	w := f.request(t, http.MethodPost, "/v1/project-assistants", f.lan, request)
	paServerOK(t, w)
	bot := f.get(t, f.lan).Bot
	scope := paDomainProjectScope(bot.Project)
	if scope.Kind != pa.PersonalWorkspace || !pa.IsPersonalWorkspaceID(scope.ProjectID) {
		t.Fatalf("workspace: %+v", scope)
	}
	p := projectAssistantProjects{f.s}
	resolved, err := p.Resolve(t.Context(), pa.ProjectSelection{MachineID: scope.MachineID, ProjectID: scope.ProjectID})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(resolved.Directory, "fixture.txt")
	if err = os.WriteFile(file, []byte("persistent"), 0600); err != nil {
		t.Fatal(err)
	}
	paServerOK(t, f.request(t, http.MethodPost, "/v1/project-assistants", f.lan, request))
	if again := f.get(t, f.lan).Bot; again.Project != bot.Project || again.ConversationID != bot.ConversationID {
		t.Fatal("a retry moved the personal Bot")
	}
	if bytes, err := os.ReadFile(file); err != nil || string(bytes) != "persistent" {
		t.Fatalf("workspace content: %q %v", bytes, err)
	}
	tampered := request
	tampered.Command.Key.BotID = "other"
	tampered.Settings.Target.ProjectID = scope.ProjectID
	paAssertHTTPFailure(t, f.request(t, http.MethodPost, "/v1/project-assistants", f.lan, tampered), 403, pa.ScopeDenied)
	edit := contract.ProjectAssistantEditRequest{
		Command: f.command(pa.EditOperation, "rename-personal"),
		Settings: contract.ProjectAssistantEditSettings{
			ExpectedVersion: bot.Version, Name: "Renamed workspace Bot", Appearance: bot.Appearance,
			Instructions: bot.Instructions.Text,
		},
	}
	paServerOK(t, f.request(t, http.MethodPatch, "/v1/project-assistants/"+f.key.BotID, f.lan, edit))
	if err = f.s.StopProjectAssistants(t.Context()); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(f.s.cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.StopProjectAssistants(context.Background()); _ = restarted.store.Close() })
	f.s, f.h = restarted, restarted.Handler()
	if after := f.get(t, f.lan).Bot; after.Name != edit.Settings.Name || after.Project != bot.Project || after.ConversationID != bot.ConversationID {
		t.Fatal("rename or host restart changed the personal Bot's workspace or conversation")
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "persistent" {
		t.Fatalf("workspace content after restart: %q %v", data, err)
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("workspace setup, rename or restart initialized a model")
	}
}

func TestLeadSetupCreatesOnceAndKeepsDeletedLeadGone(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.s.projectAssistants.defaultLeadAgent = func(time.Time) pa.Agent { return pa.Codex }
	f.enable(t)
	key := pa.BotKey{MachineID: f.key.MachineID, BotID: "default-lead"}
	db := f.s.store.ProjectAssistantDatabase()
	var raw []byte
	if err := db.QueryRow(`SELECT data FROM project_assistant_routines WHERE machine=? AND bot=?`, key.MachineID, key.BotID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var routine pa.Routine
	if err := json.Unmarshal(raw, &routine); err != nil {
		t.Fatal(err)
	}
	if routine.ID != pa.LeadCheckInID || routine.Project.Kind != pa.PersonalWorkspace ||
		len(routine.Triggers) != 1 || routine.Triggers[0].Expression != "0 9,18 * * *" || routine.Enabled {
		t.Fatalf("default check-in: %+v", routine)
	}
	var row contract.ProjectAssistantGetResponse
	path := "/v1/project-assistants/default-lead?machine_id=" + key.MachineID
	get := f.request(t, http.MethodGet, path, f.lan, nil)
	paServerOK(t, get)
	if err := json.Unmarshal(get.Body.Bytes(), &row); err != nil || !row.Bot.Lead {
		t.Fatalf("default lead: %+v %v", row.Bot, err)
	}
	cmd := contract.ProjectAssistantCommand{Key: paWireBotKey(key), Operation: "delete", IdempotencyKey: "remove-default"}
	paServerOK(t, f.request(t, http.MethodPost, "/v1/project-assistants/default-lead/delete", f.lan, contract.ProjectAssistantDeleteRequest{Command: cmd}))
	paServerOK(t, f.request(t, http.MethodPost, "/v1/settings/project-assistants", f.local, `{"enabled":false}`))
	f.enable(t)
	paAssertHTTPFailure(t, f.request(t, http.MethodGet, path, f.lan, nil), 404, pa.NotFound)
	if f.providerCalls.Load() != 0 {
		t.Fatal("setup or enabling a paused routine initialized a provider")
	}
}

func TestLeadVoiceGreetingIsIdempotentAndTextSurvivesRebinding(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.s.projectAssistants.defaultLeadAgent = func(time.Time) pa.Agent { return pa.Codex }
	f.s.projectAssistants.provider = func() (app.Provider, error) { return &alignmentProvider{}, nil }
	f.enable(t)
	key := pa.BotKey{MachineID: f.key.MachineID, BotID: "default-lead"}
	bots := store.New(f.s.store.ProjectAssistantDatabase())
	bot, err := bots.Bot(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	initial := paWireFence(pa.Fence{Key: key, Project: bot.Project, ConversationID: bot.ConversationID})
	path := "/v1/project-assistants/default-lead/messages"
	cmd := contract.ProjectAssistantCommand{Key: paWireBotKey(key), Operation: "send", IdempotencyKey: "lead-call-11111111-1111-4111-8111-111111111111"}
	start := contract.ProjectAssistantSendRequest{Command: cmd, Fence: initial, CallStarted: true}
	first := f.request(t, http.MethodPost, path, f.lan, start)
	paServerOK(t, first)
	replay := f.request(t, http.MethodPost, path, f.lan, start)
	paServerOK(t, replay)
	if first.Body.String() != replay.Body.String() {
		t.Fatal("voice greeting replay returned a different receipt")
	}
	cmd.IdempotencyKey = "person-intent"
	second := f.request(t, http.MethodPost, path, f.lan, contract.ProjectAssistantSendRequest{Command: cmd, Fence: initial, Text: "Synthetic next message"})
	paServerOK(t, second)
	var greeting, person contract.ProjectAssistantReceiptResponse
	if err := json.Unmarshal(first.Body.Bytes(), &greeting); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &person); err != nil {
		t.Fatal(err)
	}
	x, err := bots.Export(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	var greetingTurn, personTurn *pa.Turn
	for i := range x.Turns {
		turn := &x.Turns[i]
		switch turn.ID {
		case greeting.Receipt.ResourceID:
			greetingTurn = turn
		case person.Receipt.ResourceID:
			personTurn = turn
		}
	}
	if len(x.Turns) != 2 || greetingTurn == nil || !pa.IsLeadCallTurn(*greetingTurn) || personTurn == nil || personTurn.Origin != pa.UserOrigin {
		t.Fatal("greeting and person did not each own one correctly attributed turn")
	}
	for _, message := range x.Messages {
		if message.TurnID == greeting.Receipt.ResourceID && message.Kind == pa.UserMessage {
			t.Fatal("the source greeting manufactured a message from the person")
		}
	}
}
