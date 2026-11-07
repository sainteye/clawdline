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

// The refusal promises that an empty project_id means the Session's own
// Project; the proposal route passed no directory and refused it every time
// (review of 66b86a989). It now resolves the Project and reaches the source
// check instead.
func TestAProposalWithoutAProjectUsesTheSessionsOwn(t *testing.T) {
	s, p, _ := sessionItemServer(t)
	body, _ := json.Marshal(map[string]any{"session_id": p.s.ConversationID, "project_id": "", "kind": "issue",
		"title": "Tidy", "description": "d", "reason": "r", "suggested_acceptance": "a"})
	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/proposals", string(body), "propose-no-project"))
	if code := codeOf(t, rec); code == "project_not_found" || code != "proposal_source_required" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

// Two catalog places in one repository (its root and a folder inside it)
// share the canonical path. The root answers for the repository; without it
// the value is ambiguous and refused, never the lowest-hashing folder.
func TestAProjectIdentitySharedByTwoPlacesPicksTheRootOrRefuses(t *testing.T) {
	root := workV2ProjectWire{ID: projects.PlaceID("/repo"), Label: "repo", Path: "/repo"}
	web := workV2ProjectWire{ID: projects.PlaceID("/repo/web"), Label: "web", Path: "/repo"}
	docs := workV2ProjectWire{ID: projects.PlaceID("/repo/docs"), Label: "docs", Path: "/repo"}
	identity := projects.ProjectID("/repo")
	both := workV2ProjectCatalog{root.ID: root, web.ID: web}
	if got, ok := both.resolve(identity, ""); !ok || got.ID != root.ID {
		t.Fatalf("with the root listed: %+v %v", got, ok)
	}
	if got, ok := both.resolve("/repo", ""); !ok || got.ID != root.ID {
		t.Fatalf("the root directory: %+v %v", got, ok)
	}
	folders := workV2ProjectCatalog{web.ID: web, docs.ID: docs}
	if got, ok := folders.resolve(identity, ""); ok {
		t.Fatalf("two folders and no root answered %+v", got)
	}
	single := workV2ProjectCatalog{web.ID: web}
	if got, ok := single.resolve(identity, ""); !ok || got.ID != web.ID {
		t.Fatalf("one folder: %+v %v", got, ok)
	}
}

// The candidates a refused `item add` prints come from the same catalog the
// route resolves against, which includes the Session's own directory.
func TestTheAgentProjectListIsTheCatalogItemsResolveAgainst(t *testing.T) {
	s, _, project := sessionItemServer(t)
	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodGet, "/v1/work/v2/agent/projects", "", ""))
	var got struct {
		Projects []struct{ ID, Label, Path string } `json:"projects"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	found := false
	for _, p := range got.Projects {
		found = found || p.ID == project
	}
	if !found {
		t.Fatalf("the Session's Project %s is not listed: %s", project, rec.Body)
	}
}
