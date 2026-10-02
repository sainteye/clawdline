package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// Needs independent review is the person's switch, like the deployment
// policy: the person's create and edit routes set it, a Session's create
// route does not take the field at all, and a Session's edit route refuses it
// by name. Nothing is written by a refused request.
func TestOnlyThePersonSetsAFeaturesReviewSwitch(t *testing.T) {
	s, p, project := sessionItemServer(t)
	run := issueTestRun(t, s, p, "Put the label fix on the Board")
	body := strings.Replace(sessionItemBody(t, p.s.ConversationID, run, project, "feature"),
		`"kind":"feature"`, `"kind":"feature","review_required":true`, 1)
	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items", body, "agent-review-switch"))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `does not take the field \"review_required\"`) {
		t.Fatalf("a Session set the switch on create: %d %s", rec.Code, rec.Body)
	}

	person := httptest.NewRecorder()
	s.workV2Route(person, personWorkV2Request(http.MethodPost, "/v1/work/v2/items",
		`{"project_id":"`+project+`","kind":"feature","title":"Mine","description":"Mine","review_required":true}`, "person-review-switch"))
	var created struct {
		Item workV2ItemWire `json:"item"`
	}
	if person.Code != http.StatusCreated || json.Unmarshal(person.Body.Bytes(), &created) != nil || !created.Item.ReviewRequired {
		t.Fatalf("person create: %d %s", person.Code, person.Body)
	}
	issue := httptest.NewRecorder()
	s.workV2Route(issue, personWorkV2Request(http.MethodPost, "/v1/work/v2/items",
		`{"project_id":"`+project+`","kind":"issue","title":"Bug","description":"Bug","review_required":true}`, "person-review-issue"))
	if issue.Code == http.StatusCreated || !strings.Contains(issue.Body.String(), "review_required_not_applicable") {
		t.Fatalf("an Issue took the switch: %d %s", issue.Code, issue.Body)
	}

	edit := httptest.NewRecorder()
	s.workV2Route(edit, personWorkV2Request(http.MethodPatch, "/v1/work/v2/items/"+created.Item.ID,
		`{"expected_version":1,"review_required":false}`, "person-review-off"))
	var edited struct {
		Item workV2ItemWire `json:"item"`
	}
	if edit.Code != http.StatusOK || json.Unmarshal(edit.Body.Bytes(), &edited) != nil || edited.Item.ReviewRequired {
		t.Fatalf("person edit: %d %s", edit.Code, edit.Body)
	}
}

func TestTheOwningSessionCannotFlipTheReviewSwitch(t *testing.T) {
	s, p, v := workV2AssignmentServer(t, session.StateWorking)
	assigned, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
		"existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"expected_version": assigned.Item.Version,
		"session_id": p.s.ConversationID, "review_required": true})
	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPatch, "/v1/work/v2/agent/items/"+v.Item.ID+"/edit",
		string(body), "agent-review-on"))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "review_required_person_only") {
		t.Fatalf("the owner flipped the switch: %d %s", rec.Code, rec.Body)
	}
	after, err := s.workV2().Item(context.Background(), v.Item.ID)
	if err != nil || after.Item.ReviewRequired || after.Item.Version != assigned.Item.Version {
		t.Fatalf("a refused edit wrote: %+v %v", after.Item, err)
	}
}
