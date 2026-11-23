package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestLeadManagesBotsThroughTheConsoleOperations(t *testing.T) {
	f, provider := groupsManagerFixture(t, true)
	groupsManagerDesignateLead(t, f, f.get(t, f.local).Bot)
	rows, err := projects.OpenPlaceRegistry(f.s.cfg.Dir).Add([]string{t.TempDir()}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	q := f.create()
	q.Command.Key.BotID, q.Command.IdempotencyKey = "foreign", "foreign"
	q.Settings.Target.ProjectID = projects.PlaceID(rows[0].Path)
	q.Settings.Name, q.Settings.Instructions = "Original", "Preserve these instructions"
	paServerOK(t, f.request(t, http.MethodPost, "/v1/project-assistants", f.local, q))

	live := make(chan app.LeadBotTools, 1)
	finish := make(chan struct{})
	provider.run = func(ctx context.Context, native app.NativeTools) error {
		tools, ok := native.(app.LeadBotTools)
		if !ok {
			return fmt.Errorf("missing Lead controls")
		}
		if err := tools.LeadBotAvailable(ctx); err != nil {
			return err
		}
		live <- tools
		select {
		case <-finish:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer close(finish)
	paServerOK(t, f.send(t, f.local, "Manage this Bot in the other Project"))
	var tools app.LeadBotTools
	select {
	case tools = <-live:
	case err := <-provider.outcome:
		t.Fatalf("Lead could not open the owner controls: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Lead's live turn did not start")
	}
	call := func(method, path, id, body string) (contract.ProjectAssistantBot, error) {
		t.Helper()
		raw, err := tools.ManageBots(t.Context(), app.LeadBotRequest{Method: method, Path: path, IdempotencyKey: id, Body: json.RawMessage(body)})
		var result contract.ProjectAssistantBotResponse
		if err == nil {
			err = json.Unmarshal(raw, &result)
		}
		return result.Bot, err
	}
	before, err := call("GET", "/foreign", "", "")
	if err != nil || before.Name != "Original" || before.Project.ProjectID != q.Settings.Target.ProjectID {
		t.Fatalf("wrong Bot read: %+v, %v", before, err)
	}
	edit := fmt.Sprintf(`{"settings":{"expected_version":%d,"name":"Renamed","notify":"off","permission_policy":"auto_read"}}`, before.Version)
	after, err := call("PATCH", "/foreign", "edit", edit)
	if err != nil || after.Name != "Renamed" || after.Instructions.Text != before.Instructions.Text || after.Project != before.Project || after.Notify != "off" || string(after.PermissionPolicy) != "auto_read" {
		t.Fatalf("the Lead's direct edit did not apply: %+v, %v", after, err)
	}
	replayed, err := call("PATCH", "/foreign", "edit", edit)
	if err != nil || replayed.Version != after.Version {
		t.Fatalf("retry changed the Bot twice: %+v, %v", replayed, err)
	}
	_, err = call("PATCH", "/foreign", "stale", edit)
	if pa.Code(err) != pa.StaleVersion {
		t.Fatalf("expected version was ignored: %v", err)
	}
	hidden, err := call("POST", "/foreign/visibility", "hide", `{"hidden":true}`)
	if err != nil || !hidden.Hidden {
		t.Fatalf("visibility did not apply: %+v, %v", hidden, err)
	}
	_, err = call("POST", "/foreign/permissions/example/answer", "approve", `{}`)
	if pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("the Lead could impersonate a permission answer: %v", err)
	}
	_, err = call("GET", "/../../settings", "", "")
	if pa.Code(err) != pa.Invalid {
		t.Fatalf("the tool could escape the Bot routes: %v", err)
	}
	_, err = call("POST", "/foreign/visibility", "forge", `{"hidden":false,"command":{"key":{"machine_id":"other","bot_id":"foreign"}}}`)
	if pa.Code(err) != pa.Invalid {
		t.Fatalf("the tool accepted caller-supplied authority: %v", err)
	}
	shown, err := call("POST", "/foreign/visibility", "show", `{"hidden":false}`)
	if err != nil || shown.Hidden {
		t.Fatalf("could not bring back the hidden Bot: %+v, %v", shown, err)
	}
	query := map[string]string{"project_id": shown.Project.ProjectID, "canonical_id": shown.Project.CanonicalID,
		"kind": string(shown.Project.Kind), "conversation_id": shown.ConversationID, "cursor": "", "page_size": "1"}
	readRoutines := func() contract.ProjectAssistantRoutineListResponse {
		t.Helper()
		raw, err := tools.ManageBots(t.Context(), app.LeadBotRequest{Method: "GET", Path: "/foreign/routines", Query: query})
		if err != nil {
			t.Fatal(err)
		}
		var page contract.ProjectAssistantRoutineListResponse
		if err = json.Unmarshal(raw, &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	createSettings := q.Settings
	createSettings.Target = contract.ProjectAssistantProjectSelection{}
	encoded, err := json.Marshal(createSettings)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "target")
	creation, err := json.Marshal(map[string]any{"settings": fields, "folder": rows[0].Path})
	if err != nil {
		t.Fatal(err)
	}
	created, err := call("POST", "", "create", string(creation))
	if err != nil || created.Key.BotID == "" || created.Project != shown.Project {
		t.Fatalf("the Lead could not create a Bot in the named Project: %+v %v", created, err)
	}
	if _, err = call("POST", "/"+created.Key.BotID+"/delete", "delete", ""); err != nil {
		t.Fatalf("the Lead could not delete the synthetic Bot: %v", err)
	}
	if _, err = call("GET", "/"+created.Key.BotID, "", ""); pa.Code(err) != pa.NotFound {
		t.Fatalf("deleted Bot remains readable: %v", err)
	}
	page := readRoutines()
	body, err := json.Marshal(map[string]any{
		"fence": page.Fence, "id": "", "expected_revision": 0, "name": "Synthetic routine", "instruction": "Check the fixture",
		"triggers": []contract.ProjectAssistantRoutineTrigger{{Type: "interval", Seconds: 3600}}, "timeout_seconds": 3600, "enabled": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tools.ManageBots(t.Context(), app.LeadBotRequest{Method: "POST", Path: "/foreign/routines", IdempotencyKey: "save-routine", Body: body}); err != nil {
		t.Fatalf("could not manage another Bot's routine: %v", err)
	}
	if page = readRoutines(); len(page.Definitions) != 1 || page.Definitions[0].Enabled || page.Definitions[0].Name != "Synthetic routine" {
		t.Fatalf("the other Bot's saved routine did not round trip: %+v", page)
	}
	latest, err := call("GET", "/foreign", "", "")
	if err != nil {
		t.Fatal(err)
	}
	transfer := fmt.Sprintf(`{"settings":{"expected_version":%d,"roster":{"designate_lead":true}}}`, latest.Version)
	newLead, err := call("PATCH", "/foreign", "transfer-role", transfer)
	if err != nil || !newLead.Lead {
		t.Fatalf("role transfer did not return its committed result: %+v %v", newLead, err)
	}
	_, err = call("POST", "/foreign/visibility", "after-transfer", `{"hidden":true}`)
	if pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("the former Lead kept direct control: %v", err)
	}
}
