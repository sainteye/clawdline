package http

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// What a group works on crosses HTTP, the service and SQLite: create stores
// it, an edit without the key (a console from before API 101) keeps it, an
// empty string clears it, and an over-long one is refused before any write.
func TestGroupDescriptionCrossesHTTPAndSQLite(t *testing.T) {
	f := newGroupHTTPFixture(t)
	members := []contract.ProjectAssistantBotKey{paWireBotKey(f.bots[0].Key), paWireBotKey(f.bots[1].Key)}
	create := contract.ProjectAssistantGroupCreateRequest{Command: f.command(pa.CreateOperation, "create-described"), Settings: contract.ProjectAssistantGroupCreateSettings{Target: contract.ProjectAssistantProjectSelection{MachineID: "machine", ProjectID: "project"}, Name: "Review room", Description: "  Reviews pull requests  ", Members: members}}
	status, body := f.request(t, "POST", "/v1/project-assistants/rooms/groups", "person", create)
	if status != 200 {
		t.Fatalf("create %d %s", status, body)
	}
	var created contract.ProjectAssistantGroupResponse
	if err := json.Unmarshal(body, &created); err != nil || created.Group.Description != "Reviews pull requests" {
		t.Fatalf("created description %q: %v", created.Group.Description, err)
	}
	f.groupID = created.Group.Key.BotID
	read := func() contract.ProjectAssistantGroup {
		t.Helper()
		status, body := f.request(t, "GET", "/v1/project-assistants/rooms/groups?machine_id=machine", "reader", nil)
		var list contract.ProjectAssistantGroupListResponse
		if status != 200 || json.Unmarshal(body, &list) != nil || len(list.Groups) != 1 {
			t.Fatalf("list %d %s", status, body)
		}
		return list.Groups[0]
	}
	if got := read(); got.Description != "Reviews pull requests" {
		t.Fatalf("stored description %q", got.Description)
	}
	// The exact bytes an older console sends: no description key at all.
	old := map[string]any{"command": f.command(pa.EditOperation, "rename-only"), "settings": map[string]any{"expected_version": created.Group.Version, "name": "Renamed room", "members": members}}
	if status, body = f.request(t, "PATCH", "/v1/project-assistants/rooms/groups/room", "person", old); status != 200 {
		t.Fatalf("edit without description %d %s", status, body)
	}
	kept := read()
	if kept.Name != "Renamed room" || kept.Description != "Reviews pull requests" {
		t.Fatalf("an edit without the key changed the description: %+v", kept)
	}
	long := strings.Repeat("說", pa.MaxGroupDescriptionRunes+1)
	refused := contract.ProjectAssistantGroupEditRequest{Command: f.command(pa.EditOperation, "too-long"), Settings: contract.ProjectAssistantGroupEditSettings{ExpectedVersion: kept.Version, Name: kept.Name, Description: &contract.ProjectAssistantGroupDescriptionEdit{Text: long}, Members: members}}
	if status, body = f.request(t, "PATCH", "/v1/project-assistants/rooms/groups/room", "person", refused); status != 400 || !strings.Contains(string(body), "group_description") {
		t.Fatalf("over-long description %d %s", status, body)
	}
	if got := read(); got.Version != kept.Version || got.Description != "Reviews pull requests" {
		t.Fatalf("a refused edit wrote: %+v", got)
	}
	const empty = ""
	clear := contract.ProjectAssistantGroupEditRequest{Command: f.command(pa.EditOperation, "clear"), Settings: contract.ProjectAssistantGroupEditSettings{ExpectedVersion: kept.Version, Name: kept.Name, Description: &contract.ProjectAssistantGroupDescriptionEdit{Text: empty}, Members: members}}
	if status, body = f.request(t, "PATCH", "/v1/project-assistants/rooms/groups/room", "person", clear); status != 200 {
		t.Fatalf("clear %d %s", status, body)
	}
	if got := read(); got.Description != "" {
		t.Fatalf("cleared description %q", got.Description)
	}
	if status, body = f.request(t, "GET", "/v1/project-assistants/rooms/groups?machine_id=machine", "reader", nil); strings.Contains(string(body), `"description"`) {
		t.Fatalf("an empty description is still on the wire: %s", body)
	}
}
