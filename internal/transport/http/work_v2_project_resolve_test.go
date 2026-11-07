package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

// A Gemini Session refused three times for --project (the `project-…`
// identity, the directory, the folder name) read the daemon's SQLite file to
// reverse the catalog id (2026-11-07). Each form an agent can see now names
// the same Project, and leaving it out uses the Session's own.
func TestAnItemNamesItsProjectByIDDirectoryIdentityOrNothing(t *testing.T) {
	s, p, project := sessionItemServer(t)
	run := issueTestRun(t, s, p, "make items")
	canonical, _ := projects.CanonicalProjectKey(p.s.CWD)
	for i, value := range []string{project, p.s.CWD, canonical + "/", projects.ProjectID(canonical), ""} {
		rec := httptest.NewRecorder()
		s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items",
			sessionItemBody(t, p.s.ConversationID, run, value, "issue"), "form-"+string(rune('a'+i))))
		if rec.Code != http.StatusCreated {
			t.Fatalf("--project %q: %d %s", value, rec.Code, rec.Body)
		}
	}
	items, _, err := s.workV2().List(context.Background(), "", "", "all", "")
	if err != nil || len(items) != 5 {
		t.Fatalf("created %d items (%v), want 5", len(items), err)
	}
	for _, it := range items {
		if it.Item.ProjectID != project {
			t.Fatalf("item %s landed in Project %q, want %q", it.Item.ID, it.Item.ProjectID, project)
		}
	}
}

func TestAnUnknownProjectIsRefusedWithWhatIsAccepted(t *testing.T) {
	s, p, _ := sessionItemServer(t)
	run := issueTestRun(t, s, p, "make an item")
	for i, value := range []string{"notes", "/nowhere/at/all", projects.ProjectID("/nowhere/at/all")} {
		rec := httptest.NewRecorder()
		s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items",
			sessionItemBody(t, p.s.ConversationID, run, value, "issue"), "unknown-"+string(rune('a'+i))))
		var refusal struct {
			Error  string `json:"error"`
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &refusal)
		if rec.Code != http.StatusUnprocessableEntity || refusal.Error != "project_not_found" ||
			!strings.Contains(refusal.Detail, "clawdline project list") {
			t.Fatalf("--project %q: %d %s", value, rec.Code, rec.Body)
		}
	}
	items, _, err := s.workV2().List(context.Background(), "", "", "all", "")
	if err != nil || len(items) != 0 {
		t.Fatalf("refusals wrote %d items (%v)", len(items), err)
	}
}
