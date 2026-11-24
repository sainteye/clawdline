package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// A person edits an item's title while its Session works on it. The Session's
// step check-off advances the item version without touching the title, so the
// person's save, which names the values it started from, succeeds. Only the
// same field changed elsewhere to a different value is a field_conflict that
// names the field, and overwrite_conflicts then keeps the person's value.
func TestPersonEditSurvivesAgentStepsAndNamesRealConflicts(t *testing.T) {
	s, p, v := workV2AssignmentServer(t, session.StateWorking)
	owned, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
		"existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	conversation := p.s.ConversationID
	agent := "/v1/work/v2/agent/items/" + v.Item.ID
	do := func(r *http.Request) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		s.workV2Route(rec, r)
		return rec
	}
	// The editor opens here.
	opened := owned.Item
	quote := func(v string) string { b, _ := json.Marshal(v); return string(b) }
	edit := func(title, key string, extra string) *httptest.ResponseRecorder {
		body := `{"expected_version":` + strconv.FormatInt(opened.Version, 10) + `,"title":` + quote(title) +
			`,"description":` + quote(opened.Description) +
			`,"base":{"title":` + quote(opened.Title) + `,"description":` + quote(opened.Description) + `}` + extra + `}`
		return do(personWorkV2Request(http.MethodPatch, "/v1/work/v2/items/"+v.Item.ID, body, key))
	}

	// The Session adds and checks off a step while the editor is open.
	if rec := do(agentWorkV2Request(http.MethodPost, agent+"/steps", `{"session_id":"`+conversation+`","title":"Wire it","position":0}`,
		"rebase-step-add")); rec.Code != http.StatusCreated {
		t.Fatalf("step add: %d %s", rec.Code, rec.Body)
	}
	read, err := s.workV2().Item(context.Background(), v.Item.ID)
	if err != nil || len(read.Steps) != 1 {
		t.Fatalf("steps = %+v %v", read.Steps, err)
	}
	if rec := do(agentWorkV2Request(http.MethodPost, agent+"/steps/"+read.Steps[0].ID+"/complete", `{"session_id":"`+conversation+`"}`,
		"rebase-step-done")); rec.Code != http.StatusOK {
		t.Fatalf("step done: %d %s", rec.Code, rec.Body)
	}

	if rec := edit("Renamed while the agent works", "rebase-edit-1", ""); rec.Code != http.StatusOK {
		t.Fatalf("person title edit after an agent step = %d %s", rec.Code, rec.Body)
	}
	after, err := s.workV2().Item(context.Background(), v.Item.ID)
	if err != nil || after.Item.Title != "Renamed while the agent works" || len(after.Steps) != 1 || !after.Steps[0].Done {
		t.Fatalf("after edit = %+v steps %+v %v", after.Item, after.Steps, err)
	}

	// A second editor, opened before the rename, changes the same title differently.
	if rec := edit("Another title", "rebase-edit-2", ""); rec.Code != http.StatusConflict {
		t.Fatalf("same-field edit = %d %s", rec.Code, rec.Body)
	} else {
		var refusal struct {
			Error  string   `json:"error"`
			Fields []string `json:"fields"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &refusal); err != nil || refusal.Error != "field_conflict" ||
			len(refusal.Fields) != 1 || refusal.Fields[0] != "title" {
			t.Fatalf("conflict body = %s (%v)", rec.Body, err)
		}
	}
	if rec := edit("Another title", "rebase-edit-3", `,"overwrite_conflicts":true`); rec.Code != http.StatusOK {
		t.Fatalf("overwrite = %d %s", rec.Code, rec.Body)
	}
	final, err := s.workV2().Item(context.Background(), v.Item.ID)
	if err != nil || final.Item.Title != "Another title" {
		t.Fatalf("after overwrite = %+v %v", final.Item, err)
	}

	// Without a base, an old client's stale version is still a version_conflict.
	old := `{"expected_version":` + strconv.FormatInt(opened.Version, 10) + `,"title":"Old client"}`
	if rec := do(personWorkV2Request(http.MethodPatch, "/v1/work/v2/items/"+v.Item.ID, old, "rebase-old")); rec.Code != http.StatusConflict {
		t.Fatalf("old client stale edit = %d %s", rec.Code, rec.Body)
	} else if !json.Valid(rec.Body.Bytes()) || !strings.Contains(rec.Body.String(), `"version_conflict"`) {
		t.Fatalf("old client refusal = %s", rec.Body)
	}

	// A Session's edit cannot use a base.
	agentEdit := `{"session_id":"` + conversation + `","title":"Agent","base":{"title":"x"}}`
	if rec := do(agentWorkV2Request(http.MethodPatch, agent+"/edit", agentEdit, "rebase-agent")); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "base_person_only") {
		t.Fatalf("agent base = %d %s", rec.Code, rec.Body)
	}
}
