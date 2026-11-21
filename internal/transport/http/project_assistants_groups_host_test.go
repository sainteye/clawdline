package http

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// This scripted provider receives the real Host-owned NativeWork. It blocks
// its real running turn while the person answers through the real HTTP gate.
type groupsManagerProvider struct {
	action  string
	args    json.RawMessage
	outcome chan error
	trace   chan func() error
}

func (p *groupsManagerProvider) Capabilities(_ context.Context, b pa.Bot) (pa.Capabilities, error) {
	return paServerCaps(b), nil
}
func (p *groupsManagerProvider) Open(_ context.Context, q app.OpenRequest, emit func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	return &groupsManagerSession{p: p, q: q, emit: emit}, nil
}

type groupsManagerSession struct {
	p    *groupsManagerProvider
	q    app.OpenRequest
	emit func(context.Context, app.ProtocolEvent) error
}

func (s *groupsManagerSession) SessionID() string { return "scripted-manager" }
func (s *groupsManagerSession) Send(ctx context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	if s.p.trace != nil {
		s.p.trace <- func() error {
			trace := pa.Trace{ID: "manager-progress", TurnID: q.Turn.ID, Category: "tool_call", Text: "Waiting for the synthetic decision"}
			return s.emit(ctx, app.ProtocolEvent{ID: trace.ID, Fence: q.Turn.Fence, TurnID: q.Turn.ID, Trace: &trace})
		}
	}
	return s, nil
}
func (s *groupsManagerSession) Wait(ctx context.Context) (app.PromptResult, error) {
	tools, ok := s.q.Tools.(app.TeamTools)
	if !ok {
		err := fmt.Errorf("host did not bind team tools")
		s.p.outcome <- err
		return app.PromptResult{}, err
	}
	_, err := tools.Team(ctx, s.p.action, s.p.args)
	s.p.outcome <- err
	return app.PromptResult{FinalText: "Synthetic manager finished"}, nil
}
func (*groupsManagerSession) Answer(context.Context, string, string) error { return nil }
func (*groupsManagerSession) Cancel(context.Context) error                 { return nil }
func (*groupsManagerSession) Close(context.Context) error                  { return nil }

func groupsManagerFixture(t *testing.T, enabled bool) (*paServerFixture, *groupsManagerProvider) {
	t.Helper()
	f := newPAServerFixture(t, true)
	p := &groupsManagerProvider{action: "create_bot", args: json.RawMessage(`{"idempotency_key":"helper","name":"Helper","instructions":"Review the synthetic change"}`), outcome: make(chan error, 1)}
	f.s.projectAssistants.provider = func() (app.Provider, error) { return p, nil }
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	if enabled {
		groupsManagerToggle(t, f, true)
	}
	return f, p
}
func groupsManagerToggle(t *testing.T, f *paServerFixture, enabled bool) {
	t.Helper()
	bot := f.get(t, f.local).Bot
	r := contract.ProjectAssistantTeamSettingsRequest{Command: f.command(pa.EditOperation, fmt.Sprintf("toggle-%v-%d", enabled, bot.Version)), Settings: contract.ProjectAssistantTeamEditSettings{ExpectedVersion: bot.Version, Name: bot.Name, Appearance: bot.Appearance, Instructions: bot.Instructions.Text, ManageTeam: enabled}}
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/team-settings", f.local, r))
	got := f.get(t, f.local)
	if got.Bot.ManageTeam != enabled {
		t.Fatalf("switch did not round trip: %+v", got.Bot)
	}
}
func groupsManagerPlan(t *testing.T, f *paServerFixture) contract.ProjectAssistantTeamPlan {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r := f.request(t, "GET", "/v1/project-assistants/"+f.key.BotID+"/team-plans?machine_id="+f.key.MachineID, f.local, nil)
		paServerOK(t, r)
		var page contract.ProjectAssistantTeamPlansResponse
		if err := json.Unmarshal(r.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Plans) == 1 {
			return page.Plans[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("real Host never published the manager approval card")
	return contract.ProjectAssistantTeamPlan{}
}
func groupsManagerBots(t *testing.T, f *paServerFixture) []contract.ProjectAssistantBot {
	t.Helper()
	r := f.request(t, "GET", "/v1/project-assistants?machine_id="+f.key.MachineID+"&cursor=&query=&page_size=100", f.local, nil)
	paServerOK(t, r)
	var page contract.ProjectAssistantListResponse
	if err := json.Unmarshal(r.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	out := []contract.ProjectAssistantBot{}
	for _, v := range page.Items {
		out = append(out, v.Bot)
	}
	return out
}

func TestGroupsRootWiringManagerApprovalRechecksTheRealGrant(t *testing.T) {
	for _, scenario := range []string{"accept", "decline", "off", "regranted"} {
		t.Run(scenario, func(t *testing.T) {
			f, p := groupsManagerFixture(t, true)
			paServerOK(t, f.send(t, f.local, "start"))
			plan := groupsManagerPlan(t, f)
			answer := contract.ProjectAssistantTeamAnswerRequest{Command: f.command(pa.AnswerOperation, "decide"), PlanID: plan.ID, Allow: scenario != "decline"}
			read := f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/team-answer", f.readonly, answer)
			if read.Code < 400 {
				t.Fatal("read-only device answered a team plan")
			}
			if scenario == "off" || scenario == "regranted" {
				groupsManagerToggle(t, f, false)
				if scenario == "regranted" {
					groupsManagerToggle(t, f, true)
				}
			}
			r := f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/team-answer", f.local, answer)
			if scenario == "off" || scenario == "regranted" {
				if r.Code < 400 {
					t.Fatal("revoked card applied", r.Body.String())
				}
			} else {
				paServerOK(t, r)
			}
			select {
			case err := <-p.outcome:
				if (scenario == "accept") != (err == nil) {
					t.Fatalf("manager outcome %s: %v", scenario, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("manager tool never settled")
			}
			routineHumanIdle(t, f)
			bots := groupsManagerBots(t, f)
			if scenario != "accept" {
				if len(bots) != 1 {
					t.Fatal("refused plan changed team", len(bots))
				}
				return
			}
			if len(bots) != 2 {
				t.Fatal("approved helper missing", len(bots))
			}
			for _, b := range bots {
				if b.Key != paWireBotKey(f.key) {
					if b.CreatedBy == nil || *b.CreatedBy != paWireBotKey(f.key) || !b.ManageTeam || b.Project != bots[0].Project || b.Agent != contract.ProjectAssistantAgentCodex || b.PermissionPolicy != contract.ProjectAssistantPermissionPolicyAsk {
						t.Fatalf("bad inheritance: %+v", b)
					}
				}
			}
		})
	}
}
func TestGroupsRootWiringLeadManagesTeamByRole(t *testing.T) {
	for _, transfer := range []bool{false, true} {
		t.Run(fmt.Sprintf("transfer=%v", transfer), func(t *testing.T) {
			f, p := groupsManagerFixture(t, false)
			designate := func(bot contract.ProjectAssistantBot) {
				t.Helper()
				command := f.command(pa.EditOperation, "lead-"+bot.Key.BotID)
				command.Key = bot.Key
				settings := contract.ProjectAssistantEditSettings{
					ExpectedVersion: bot.Version, Name: bot.Name, Appearance: bot.Appearance,
					Instructions: bot.Instructions.Text, Roster: &contract.ProjectAssistantRosterEdit{DesignateLead: true},
				}
				paServerOK(t, f.request(t, "PATCH", "/v1/project-assistants/"+bot.Key.BotID, f.local,
					contract.ProjectAssistantEditRequest{Command: command, Settings: settings}))
			}
			designate(f.get(t, f.local).Bot)
			if bot := f.get(t, f.local).Bot; !bot.Lead || !bot.ManageTeam {
				t.Fatal("the lead's default grant was not visible to the approval UI")
			}

			paServerOK(t, f.send(t, f.local, "start"))
			plan := groupsManagerPlan(t, f)
			if len(groupsManagerBots(t, f)) != 1 {
				t.Fatal("a pending proposal created a Bot before approval")
			}
			if transfer {
				q := f.create()
				q.Command.Key.BotID = "other-lead"
				q.Command.IdempotencyKey = "create-other-lead"
				paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, q))
				for _, bot := range groupsManagerBots(t, f) {
					if bot.Key == q.Command.Key {
						designate(bot)
					}
				}
				if bot := f.get(t, f.local).Bot; bot.Lead || bot.ManageTeam {
					t.Fatal("the former lead kept the role's grant")
				}
			}
			answer := contract.ProjectAssistantTeamAnswerRequest{Command: f.command(pa.AnswerOperation, "decide"), PlanID: plan.ID, Allow: true}
			r := f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/team-answer", f.local, answer)
			if transfer {
				if r.Code < 400 {
					t.Fatal("the former lead's pending card applied")
				}
			} else {
				paServerOK(t, r)
			}
			select {
			case err := <-p.outcome:
				if (err != nil) != transfer {
					t.Fatalf("manager outcome: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("manager tool never settled")
			}
			routineHumanIdle(t, f)
			bots := groupsManagerBots(t, f)
			if len(bots) != 2 {
				t.Fatal("unexpected Bot count after decision", len(bots))
			}
			if !transfer {
				for _, bot := range bots {
					if bot.Key != paWireBotKey(f.key) && (bot.ManageTeam || bot.CreatedBy == nil || *bot.CreatedBy != paWireBotKey(f.key)) {
						t.Fatal("the helper inherited the role's grant or lost its creator")
					}
				}
			}
		})
	}
}

func TestGroupsRootWiringManagerOffAndCrossProjectRefuseWithoutACard(t *testing.T) {
	for _, scenario := range []string{"off", "other-project", "authority-field"} {
		t.Run(scenario, func(t *testing.T) {
			f, p := groupsManagerFixture(t, scenario != "off")
			if scenario == "other-project" {
				q := f.create()
				q.Command.Key.BotID = "foreign"
				q.Command.IdempotencyKey = "foreign"
				otherDir := t.TempDir()
				registered, err := projects.OpenPlaceRegistry(f.s.cfg.Dir).Add([]string{otherDir}, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				q.Settings.Target.ProjectID = projects.PlaceID(registered[len(registered)-1].Path)
				// A real Bot in another registered Project remains outside this manager's grant.
				p.action = "update_bot"
				p.args = json.RawMessage(`{"idempotency_key":"foreign","botId":"foreign","name":"Changed"}`)
				paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, q))
			}
			if scenario == "authority-field" {
				p.action = "update_bot"
				p.args = json.RawMessage(`{"idempotency_key":"grant","botId":"` + f.key.BotID + `","manageTeam":true}`)
			}
			paServerOK(t, f.send(t, f.local, "start"))
			select {
			case err := <-p.outcome:
				if err == nil {
					t.Fatal("manager refusal was lost")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("invalid manager call waited for approval", f.request(t, "GET", "/v1/project-assistants/"+f.key.BotID+"/team-plans?machine_id="+f.key.MachineID, f.local, nil).Body.String(), groupsManagerBots(t, f))
			}
			routineHumanIdle(t, f)
			if bots := groupsManagerBots(t, f); len(bots) != map[bool]int{true: 2, false: 1}[scenario == "other-project"] {
				t.Fatal(len(bots))
			}
		})
	}
}
func TestGroupsRootWiringStopsRoomsAndKeepsMembers(t *testing.T) {
	f, p, server := newRoutineHumanFixture(t)
	_ = p
	second := f.create()
	second.Command.Key.BotID = "second"
	second.Command.IdempotencyKey = "second"
	second.Settings.Name = "Second"
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, second))
	key := contract.ProjectAssistantBotKey{MachineID: f.key.MachineID, BotID: "real-room"}
	cmd := f.command(pa.CreateOperation, "real-room")
	cmd.Key = key
	create := contract.ProjectAssistantGroupCreateRequest{Command: cmd, Settings: contract.ProjectAssistantGroupCreateSettings{Target: second.Settings.Target, Name: "Actual Host room", Members: []contract.ProjectAssistantBotKey{paWireBotKey(f.key), second.Command.Key}}}
	status, body := routineHumanTCP(t, f, server, "POST", "/v1/project-assistants/rooms/groups", f.local, create)
	if status != 200 {
		t.Fatal(status, string(body))
	}
	var created contract.ProjectAssistantGroupResponse
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatal(err)
	}
	cmd.Key = created.Group.Key
	cmd.Operation = "send"
	cmd.IdempotencyKey = "room-say"
	status, body = routineHumanTCP(t, f, server, "POST", "/v1/project-assistants/rooms/groups/"+created.Group.Key.BotID+"/messages", f.local, contract.ProjectAssistantGroupSendRequest{Command: cmd, Text: "Review this"})
	if status != 200 {
		t.Fatal(status, string(body))
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, body = routineHumanTCP(t, f, server, "GET", "/v1/project-assistants/rooms/groups/"+created.Group.Key.BotID+"?machine_id="+f.key.MachineID+"&before_seq=0&page_size=100", f.local, nil)
		if status != 200 {
			t.Fatal(status, string(body))
		}
		if strings.Count(string(body), `"kind":"member"`) == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if strings.Count(string(body), `"kind":"member"`) != 2 {
		t.Fatal(string(body))
	}
	cmd.Operation = "stop"
	cmd.IdempotencyKey = "room-stop"
	status, body = routineHumanTCP(t, f, server, "POST", "/v1/project-assistants/rooms/groups/"+created.Group.Key.BotID+"/stop", f.local, contract.ProjectAssistantGroupStopRequest{Command: cmd})
	if status != 200 {
		t.Fatal(status, string(body))
	}
	if err := f.s.StopProjectAssistants(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestGroupsRootWiringTeamChatCardSurvivesProgressAndReload(t *testing.T) {
	for _, action := range []string{"create_bot", "update_bot"} {
		t.Run(action, func(t *testing.T) {
			f, p := groupsManagerFixture(t, true)
			f.pin(t, "reader", adaptercloud.ScopeReadOnly, false)
			p.trace = make(chan func() error, 1)
			if action == "update_bot" {
				p.action = action
				p.args, _ = json.Marshal(map[string]any{"idempotency_key": "rename", "botId": f.key.BotID, "name": "Renamed manager"})
			}
			paServerOK(t, f.send(t, f.local, "Manage the team"))
			plan := groupsManagerPlan(t, f)
			get := f.get(t, f.local)
			query := contract.ProjectAssistantReadViewRequest{Key: paWireBotKey(f.key), ConversationID: get.Conversation.ID, Mode: contract.ProjectAssistantReadViewModeSnapshot, PageSize: 100}
			read := func() contract.ProjectAssistantReadViewResponse {
				t.Helper()
				response := paViewServerBridge(t, f, "reader", query)
				if response.Status != 200 {
					t.Fatalf("Cloud snapshot returned %d: %s", response.Status, response.Payload)
				}
				var page struct {
					Body contract.ProjectAssistantReadViewResponse `json:"body"`
				}
				if err := json.Unmarshal(response.Payload, &page); err != nil {
					t.Fatal(err)
				}
				return page.Body
			}
			old := read()
			for _, row := range old.Items {
				if row.Permission != nil {
					t.Fatal("old clients received an unrequested timeline card")
				}
			}
			query.TeamCards = true
			before := read()
			find := func(page contract.ProjectAssistantReadViewResponse) contract.ProjectAssistantPermissionOffer {
				t.Helper()
				for i, row := range page.Items {
					if row.Permission != nil && row.Permission.ID == plan.ID {
						if row.AnchorMessageSeq < 1 || i+1 >= len(page.Items) || page.Items[i+1].Message == nil || page.Items[i+1].Message.Seq != row.AnchorMessageSeq {
							t.Fatalf("card isn't ordered immediately after its anchor: %+v", row)
						}
						return *row.Permission
					}
				}
				t.Fatal("missing team card in the conversation")
				return contract.ProjectAssistantPermissionOffer{}
			}
			card := find(before)
			if card.State != "pending" || before.Head.PendingPermissionCount != 1 || card.Action == nil || len(card.Options) != 2 {
				t.Fatalf("wrong pending card: %+v", card)
			}
			select {
			case emit := <-p.trace:
				if err := emit(); err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("missing the running manager's progress callback")
			}
			roster := f.request(t, "GET", "/v1/project-assistants?machine_id="+f.key.MachineID+"&cursor=&query=&page_size=100", f.local, nil)
			paServerOK(t, roster)
			var rows contract.ProjectAssistantListResponse
			if err := json.Unmarshal(roster.Body.Bytes(), &rows); err != nil || len(rows.Items) != 1 || rows.Items[0].PendingPermissions != 1 || rows.Items[0].Status != "needsInput" {
				t.Fatalf("pending approval wasn't visible in the roster: %s", roster.Body)
			}
			answer := contract.ProjectAssistantPermissionAnswerRequest{Command: f.command(pa.AnswerOperation, "answer-team-in-chat"), Fence: card.Fence, OfferID: card.ID, TurnID: card.TurnID, OptionID: card.Options[0].ID}
			path := "/v1/project-assistants/" + f.key.BotID + "/permissions/" + card.ID + "/answer"
			paServerOK(t, f.request(t, "POST", path, f.local, answer))
			select {
			case err := <-p.outcome:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("the manager didn't learn the outcome")
			}
			routineHumanIdle(t, f)
			after := find(read())
			if after.State != "answered" || after.SelectedOptionID != answer.OptionID {
				t.Fatalf("card didn't retain the approved outcome: %+v", after)
			}
			paServerOK(t, f.request(t, "POST", path, f.local, answer))
			bots := groupsManagerBots(t, f)
			if action == "create_bot" && len(bots) != 2 || action == "update_bot" && (len(bots) != 1 || bots[0].Name != "Renamed manager") {
				t.Fatalf("unexpected mutation or duplicate: %+v", bots)
			}
		})
	}
}
