package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/skillmenu"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// The slash menu is read per session, by the id on its row: Claude's from its
// working directory, Codex's from its own rollout, and a session this daemon
// cannot place is refused rather than answered with an empty menu.
func TestTheSlashMenuIsReadPerSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	work := t.TempDir()
	skillFile := filepath.Join(work, ".claude", "skills", "ship", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillFile, []byte("---\ndescription: Ship it\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commandFile := filepath.Join(work, ".claude", "commands", "inspect.md")
	if err := os.MkdirAll(filepath.Dir(commandFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(commandFile, []byte("---\ndescription: Inspect it\n---\nPRIVATE COMMAND BODY"), 0o644); err != nil {
		t.Fatal(err)
	}
	promptFile := filepath.Join(home, ".codex", "prompts", "audit.md")
	if err := os.MkdirAll(filepath.Dir(promptFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(promptFile, []byte("---\ndescription: Audit it\n---\nPRIVATE PROMPT BODY"), 0o644); err != nil {
		t.Fatal(err)
	}
	geminiWork := t.TempDir()
	geminiSkillFile := filepath.Join(geminiWork, ".gemini", "skills", "audit", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(geminiSkillFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(geminiSkillFile, []byte("---\ndescription: Audit it\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reading := session.Inventory{Provenance: "ps", Complete: true, Sessions: []session.Session{
		{ID: "%81", TTY: "ttys081", Assistant: session.AssistantClaude, CWD: work},
		{ID: "%82", TTY: "ttys082", Assistant: session.AssistantClaude},
		{ID: "%83", TTY: "ttys083", Assistant: session.AssistantCodex},
		{ID: "%84", TTY: "ttys084", Assistant: session.AssistantAntigravity, CWD: geminiWork},
		{ID: "%85", TTY: "ttys085", Assistant: session.AssistantAntigravity},
	}}
	s := &Server{inventory: app.Inventory{Process: todoProcesses{reading}}, skillMenu: skillmenu.NewCache()}
	get := func(target, method string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		rec := httptest.NewRecorder()
		id, ok := skillsPath(req)
		if !ok {
			rec.Code = -1
			return rec
		}
		s.sessionSkillsRoute(rec, req, id)
		return rec
	}

	rec := get("/v1/sessions/%2581/skills", http.MethodGet)
	var reply contract.SkillsReply
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("a Claude session: %d %s", rec.Code, rec.Body)
	}
	if len(reply.Skills) != 2 || reply.Skills[0].Name != "inspect" || reply.Skills[0].Description != "Inspect it" || reply.Skills[0].Invocation != "/inspect" || reply.Skills[1].Name != "ship" ||
		reply.Skills[0].Source != contract.SkillSourceProject || reply.ObservedAt == 0 {
		t.Fatalf("answer %s", rec.Body)
	}
	// No path or body leaves; commands additionally carry their exact invocation.
	if strings.Contains(rec.Body.String(), work) || strings.Contains(rec.Body.String(), "body") || strings.Contains(rec.Body.String(), "PRIVATE") {
		t.Fatalf("the answer carries more than metadata: %s", rec.Body)
	}
	if r := s.skillsReading(); !r.Known || r.Used != 1 {
		t.Errorf("the cache row after one read: %+v", r)
	}

	if rec := get("/v1/sessions/%2582/skills", http.MethodGet); rec.Code != http.StatusNotFound ||
		!strings.Contains(rec.Body.String(), `"not_found"`) {
		t.Fatalf("a Claude session with no directory: %d %s", rec.Code, rec.Body)
	}
	if rec := get("/v1/sessions/%2583/skills", http.MethodGet); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"invocation":"/prompts:audit"`) || strings.Contains(rec.Body.String(), "PRIVATE") {
		t.Fatalf("a Codex session started fresh: %d %s", rec.Code, rec.Body)
	}
	recGemini := get("/v1/sessions/%2584/skills", http.MethodGet)
	var geminiReply contract.SkillsReply
	if err := json.Unmarshal(recGemini.Body.Bytes(), &geminiReply); err != nil || recGemini.Code != http.StatusOK {
		t.Fatalf("a Gemini session: %d %s", recGemini.Code, recGemini.Body)
	}
	if len(geminiReply.Skills) != 1 || geminiReply.Skills[0].Name != "audit" || geminiReply.Skills[0].Description != "Audit it" {
		t.Fatalf("gemini answer: %s", recGemini.Body)
	}
	if rec := get("/v1/sessions/%2585/skills", http.MethodGet); rec.Code != http.StatusNotFound ||
		!strings.Contains(rec.Body.String(), `"not_found"`) {
		t.Fatalf("a Gemini session with no directory: %d %s", rec.Code, rec.Body)
	}
	if rec := get("/v1/sessions/%2599/skills", http.MethodGet); rec.Code != http.StatusNotFound {
		t.Fatalf("a session a complete reading does not hold: %d %s", rec.Code, rec.Body)
	}
	if rec := get("/v1/sessions/%2581/skills", http.MethodPost); rec.Code != -1 {
		t.Fatalf("a POST was taken as the read: %d", rec.Code)
	}

	// A server with no cache still answers, and its row is a known zero.
	bare := &Server{inventory: s.inventory}
	if r := bare.skillsReading(); !r.Known || r.Used != 0 {
		t.Errorf("no cache: %+v", r)
	}
	if capacity.Default(capacity.CacheSessionSkills) <= 0 {
		t.Error("the skills cache has no registered limit")
	}
}

// The server list is a read, and nothing else is: a POST is refused by name.
func TestTheServerListRunsNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{inventory: app.Inventory{Process: todoProcesses{session.Inventory{Provenance: "ps", Complete: true}}}}
	rec := httptest.NewRecorder()
	s.devStacksRoute(rec, httptest.NewRequest(http.MethodPost, "/v1/devstacks", strings.NewReader(`{}`)))
	if rec.Code != http.StatusMethodNotAllowed || !strings.Contains(rec.Body.String(), `"method_not_allowed"`) {
		t.Fatalf("a POST: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	s.devStacksRoute(rec, httptest.NewRequest(http.MethodGet, "/v1/devstacks", nil))
	var reply contract.DevStacksReply
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("a GET on an empty machine: %d %s", rec.Code, rec.Body)
	}
	if reply.Stacks == nil || len(reply.Stacks) != 0 || reply.ObservedAt == 0 {
		t.Fatalf("answer %s", rec.Body)
	}
}
