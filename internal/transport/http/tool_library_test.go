package http

import (
	"encoding/json"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/toollibrary"
	"strings"
	"testing"
)

func TestToolLibraryAuthorizationPersistenceAndReplay(t *testing.T) {
	f := newSquadFixture(t)
	_, reader, err := f.s.gate().auth.AddDeviceWithPermission("tools-readonly", true)
	if err != nil {
		t.Fatal(err)
	}
	f.reader = reader
	f.h = f.s.Handler()
	if status, _ := f.ask("GET", "/v1/tool-library", "", "", ""); status != 401 {
		t.Fatalf("unauthed %d", status)
	}
	status, body := f.ask("GET", "/v1/tool-library", f.reader, "", "")
	if status != 200 || !strings.Contains(body, "mattpocock--skills") {
		t.Fatalf("read %d %s", status, body)
	}
	p := toollibrary.Recommendations().Projects[0]
	request, _ := json.Marshal(map[string]any{"expected_version": 0, "project": p})
	if status, _ = f.ask("POST", "/v1/tool-library", f.reader, string(request), "save"); status != 403 {
		t.Fatalf("reader write %d", status)
	}
	if status, _ = f.ask("POST", "/v1/tool-library", f.sender, string(request), ""); status != 400 {
		t.Fatalf("keyless %d", status)
	}
	status, first := f.ask("POST", "/v1/tool-library", f.sender, string(request), "save")
	if status != 200 {
		t.Fatalf("save %d %s", status, first)
	}
	status, again := f.ask("POST", "/v1/tool-library", f.sender, string(request), "save")
	if status != 200 || again != first {
		t.Fatalf("replay %d %s", status, again)
	}
	if status, _ = f.ask("POST", "/v1/tool-library", f.sender, string(request), "new-save"); status != 409 {
		t.Fatalf("lost update %d", status)
	}
	status, body = f.ask("GET", "/v1/tool-library", f.reader, "", "")
	if status != 200 {
		t.Fatal(status, body)
	}
	var result struct {
		Version int64                 `json:"version"`
		Saved   []toollibrary.Project `json:"saved"`
	}
	if err := json.Unmarshal([]byte(body), &result); err != nil || result.Version != 1 || len(result.Saved) != 1 {
		t.Fatalf("saved state: %s %v", body, err)
	}
	if status, body = f.ask("POST", "/v1/tool-library/prepare", f.sender, `{"session":"missing","conversation":"missing","task":"Review","mode":"task"}`, "prepare"); status == 200 {
		t.Fatal("prepared nonexistent session", body)
	}
}

func TestToolPreparationPinsConversationAndDoesNotSend(t *testing.T) {
	f := newSquadFixture(t)
	p := &pane{s: session.Session{ID: "%tools", Backend: session.BackendTmux, Assistant: session.AssistantClaude, ConversationID: "00000000-0000-4000-8000-000000000777", CWD: t.TempDir(), State: session.StateIdle}}
	hosts := []ports.TerminalHost{p}
	f.s.terminals = hosts
	f.s.inventory = app.Inventory{Terminals: hosts, Screen: p}
	f.h = f.s.Handler()
	project := toollibrary.Recommendations().Projects[0]
	input := map[string]any{"project_id": project.ID, "skill_id": project.Skills[0].ID, "task": "Review architecture", "mode": "task", "session": p.s.ID, "conversation": p.s.ConversationID}
	raw, _ := json.Marshal(input)
	status, body := f.ask("POST", "/v1/tool-library/prepare", f.sender, string(raw), "prepare-valid")
	if status != 200 {
		t.Fatalf("prepare %d %s", status, body)
	}
	var prepared toollibrary.Prepared
	if err := json.Unmarshal([]byte(body), &prepared); err != nil || !strings.Contains(prepared.Prompt, project.Commit) || prepared.Task != "Review architecture" {
		t.Fatalf("prepared: %s %v", body, err)
	}
	if len(p.done()) != 0 {
		t.Fatal("preparation sent or executed a message", p.done())
	}
	input["project_id"] = "google-research--harness2"
	input["skill_id"] = ""
	input["locale"] = "zh-Hant-TW"
	raw, _ = json.Marshal(input)
	status, body = f.ask("POST", "/v1/tool-library/prepare", f.sender, string(raw), "prepare-framework")
	if status != 200 {
		t.Fatalf("framework prepare %d %s", status, body)
	}
	if err := json.Unmarshal([]byte(body), &prepared); err != nil || prepared.Skill != nil || prepared.Project.IntegrationKind != "framework" || !strings.Contains(prepared.Prompt, "獨立驗收") || !strings.Contains(prepared.Prompt, prepared.Project.Commit) {
		t.Fatalf("framework preparation: %s %v", body, err)
	}
	if len(p.done()) != 0 {
		t.Fatal("framework preparation sent or executed a message", p.done())
	}
	input["conversation"] = "another-conversation"
	raw, _ = json.Marshal(input)
	if status, body = f.ask("POST", "/v1/tool-library/prepare", f.sender, string(raw), "prepare-stale"); status != 409 {
		t.Fatalf("stale %d %s", status, body)
	}
}
