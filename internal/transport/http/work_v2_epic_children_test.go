package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

const epicOwnerConversation = "20000000-0000-4000-8000-000000000001"

// epicChildrenServer is a server whose Board holds an Epic, in the pane's
// Project, owned by another conversation and already past its plan gate
// (the gate has tests of its own), and the pane's idle Session to hand
// children to.
func epicChildrenServer(t *testing.T, phase work.Phase) (*Server, *pane, app.WorkV2View) {
	t.Helper()
	s, p, feature := workV2AssignmentServer(t, session.StateIdle)
	ctx := context.Background()
	epic, err := s.workV2().Create(ctx, app.NewWorkV2{ProjectID: "project-1", ProjectPath: feature.Item.ProjectPath,
		Kind: work.KindEpic, Title: "Big rework", Description: "All of it.",
		AcceptanceCriteria: "Every part of the rework is complete.", Actor: "local"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := s.workV2().Assign(ctx, epic.Item.ID, app.AssignWorkV2{ExpectedVersion: epic.Item.Version,
		Mode: "existing_session", SessionID: epicOwnerConversation, Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if phase != work.PhaseAssigned {
		if err := s.store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
			prev, err := tx.Item(epic.Item.ID)
			if err != nil {
				return err
			}
			next := prev
			next.Phase = phase
			return tx.PutItem(prev, next, "item.phase_changed", epicOwnerConversation, `{}`)
		}); err != nil {
			t.Fatal(err)
		}
	}
	owned, err = s.workV2().Item(ctx, owned.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	return s, p, owned
}

func agentPost(t *testing.T, s *Server, path, key string, body map[string]any, capability ...string) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	if len(capability) > 0 {
		req.Header.Set(squadSessionCapabilityHeader, capability[0])
	}
	req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{machine: true, verdict: auth.Verdict{Allowed: true}}))
	rec := httptest.NewRecorder()
	s.workV2Route(rec, req)
	return rec
}

func TestBoundEpicOwnerMustProveItsOwnSquadCapability(t *testing.T) {
	s, _, epic := epicChildrenServer(t, work.PhaseImplementing)
	ctx := context.Background()
	launch, err := s.store.PrepareSquadLaunch(ctx, json.RawMessage(`{"definition_id":"clawdline.persona.product-manager","scope_id":"project-test"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.RecordSquadTerminal(ctx, launch.ID, "owner-terminal"); err != nil {
		t.Fatal(err)
	}
	if err := s.store.BindSquadConversation(ctx, launch.ID, epicOwnerConversation); err != nil {
		t.Fatal(err)
	}
	path := "/v1/work/v2/agent/items/" + epic.Item.ID + "/children"
	body := map[string]any{"expected_version": epic.Item.Version, "session_id": epicOwnerConversation,
		"kind": "issue", "title": "A child", "description": "One part."}
	if got := agentPost(t, s, path, "bound-missing", body); got.Code != http.StatusForbidden || !strings.Contains(got.Body.String(), "session_actor_required") {
		t.Fatalf("missing capability = %d %s", got.Code, got.Body)
	}
	if got := agentPost(t, s, path, "bound-wrong", body, "wrong"); got.Code != http.StatusForbidden || !strings.Contains(got.Body.String(), "session_actor_required") {
		t.Fatalf("wrong capability = %d %s", got.Code, got.Body)
	}
	if got := agentPost(t, s, path, "bound-valid", body, launch.ActorCapability); got.Code != http.StatusCreated {
		t.Fatalf("bound owner = %d %s", got.Code, got.Body)
	}
}

type epicChildAnswer struct {
	Item            workV2ItemWire `json:"item"`
	Assigned        bool           `json:"assigned"`
	AssignmentError *struct {
		Code string `json:"code"`
	} `json:"assignment_error"`
}

// The Epic's owner creates a child assigned to an existing Session: it is
// created with its parent on the wire and the Epic's provenance, handed to
// the pane's Session, which is told the item belongs to the Epic, and a
// replay of the same key answers the same without a second item.
func TestTheEpicsOwnerCreatesAChildAssignedToAnotherSession(t *testing.T) {
	s, p, epic := epicChildrenServer(t, work.PhaseImplementing)
	body := map[string]any{"expected_version": epic.Item.Version, "session_id": epicOwnerConversation,
		"kind": "feature", "title": "The storage part", "description": "Store it.",
		"acceptance_criteria": "The data survives a restart.", "steps": []string{"schema", "reads"},
		"assign": map[string]any{"mode": "existing_session", "terminal_id": p.s.ID}}
	path := "/v1/work/v2/agent/items/" + epic.Item.ID + "/children"
	rec := agentPost(t, s, path, "epic-child-1", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var got epicChildAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	it := got.Item
	if !got.Assigned || got.AssignmentError != nil || it.ParentID != epic.Item.ID || it.OwnerSession == nil ||
		*it.OwnerSession != p.s.ConversationID || it.Phase != "assigned" || len(it.Steps) != 2 ||
		it.CreatedBy != work.EpicOwnerActor(epicOwnerConversation) || it.CreatedVia == nil ||
		it.CreatedVia.EpicID != epic.Item.ID || it.CreatedVia.Run != "" ||
		it.AcceptanceCriteria != "The data survives a restart." || it.AcceptanceVersion != 1 ||
		it.AcceptanceDigest != work.AcceptanceDigest("The data survives a restart.") {
		t.Fatalf("child answer: %s", rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"parent_id":"`+epic.Item.ID+`"`) {
		t.Fatalf("parent_id is not on the wire: %s", rec.Body)
	}
	sent := p.done()
	if len(sent) != 1 || !strings.Contains(sent[0], "part of Epic "+epic.Item.ID) ||
		!strings.Contains(sent[0], "Do not create Board items.") {
		t.Fatalf("the child's owner was told: %v", sent)
	}
	again := agentPost(t, s, path, "epic-child-1", body)
	if again.Code != http.StatusCreated || again.Body.String() != rec.Body.String() {
		t.Fatalf("replay: %d %s", again.Code, again.Body)
	}
	items, _, err := s.store.WorkV2Items(context.Background(), "", "", "all", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	children := 0
	for _, i := range items {
		if i.ParentID == epic.Item.ID {
			children++
		}
	}
	if children != 1 {
		t.Fatalf("children after a replay: %d", children)
	}
}

// Without an assignment the child waits unassigned for the person; when the
// assignment fails — here a new Session this machine cannot open — the child
// still exists, unassigned, and the answer names the assignment's code.
func TestAnEpicChildWhoseAssignmentFailsStaysAndSaysSo(t *testing.T) {
	s, _, epic := epicChildrenServer(t, work.PhaseImplementing)
	path := "/v1/work/v2/agent/items/" + epic.Item.ID + "/children"
	plain := agentPost(t, s, path, "epic-child-plain", map[string]any{"expected_version": epic.Item.Version,
		"session_id": epicOwnerConversation, "kind": "issue", "title": "A bug", "description": "Fix it."})
	var got epicChildAnswer
	if plain.Code != http.StatusCreated || json.Unmarshal(plain.Body.Bytes(), &got) != nil || got.Assigned ||
		got.AssignmentError != nil || got.Item.OwnerSession != nil || got.Item.Area != "unassigned" {
		t.Fatalf("unassigned child: %d %s", plain.Code, plain.Body)
	}
	fresh, _ := s.workV2().Item(context.Background(), epic.Item.ID)
	failed := agentPost(t, s, path, "epic-child-new", map[string]any{"expected_version": fresh.Item.Version,
		"session_id": epicOwnerConversation, "kind": "feature", "title": "Another part", "description": "Build it.",
		"acceptance_criteria": "The part is built.",
		"assign":              map[string]any{"mode": "new_session", "assistant": "claude"}})
	got = epicChildAnswer{}
	if failed.Code != http.StatusCreated || json.Unmarshal(failed.Body.Bytes(), &got) != nil || got.Assigned ||
		got.AssignmentError == nil || got.AssignmentError.Code != "assignment_failed" || got.Item.OwnerSession != nil ||
		got.Item.ParentID != epic.Item.ID {
		t.Fatalf("failed assignment: %d %s", failed.Code, failed.Body)
	}
	child, err := s.workV2().Item(context.Background(), got.Item.ID)
	if err != nil || len(child.Assignments) != 1 || child.Assignments[0].State != "failed" ||
		child.Assignments[0].HumanActor != work.EpicOwnerActor(epicOwnerConversation) {
		t.Fatalf("the failed assignment's record: %+v %v", child.Assignments, err)
	}
	bad := agentPost(t, s, path, "epic-child-bad", map[string]any{"expected_version": fresh.Item.Version + 1,
		"session_id": epicOwnerConversation, "kind": "feature", "title": "x", "description": "y",
		"assign": map[string]any{"mode": "existing_session"}})
	if bad.Code != http.StatusUnprocessableEntity || !strings.Contains(bad.Body.String(), "invalid_assignment") {
		t.Fatalf("malformed assign: %d %s", bad.Code, bad.Body)
	}
}

// The route refuses everyone but the Epic's owner, an Epic still before its
// plan gate, a parent that is not an Epic, and a child kind that is not a
// Feature or an Issue — each by name, writing nothing.
func TestTheChildrenRouteRefusesByName(t *testing.T) {
	s, _, epic := epicChildrenServer(t, work.PhaseImplementing)
	create := func(key, id string, version int64, sessionID, kind string) *httptest.ResponseRecorder {
		return agentPost(t, s, "/v1/work/v2/agent/items/"+id+"/children", key, map[string]any{
			"expected_version": version, "session_id": sessionID, "kind": kind, "title": "t", "description": "d"})
	}
	for _, c := range []struct {
		name, sessionID, kind, code string
		status                      int
	}{
		{"another Session", "30000000-0000-4000-8000-000000000003", "feature", "not_epic_owner", http.StatusConflict},
		{"an Epic", epicOwnerConversation, "epic", "child_kind_not_allowed", http.StatusUnprocessableEntity},
		{"a Plan", epicOwnerConversation, "plan", "child_kind_not_allowed", http.StatusUnprocessableEntity},
	} {
		rec := create("refuse-"+c.name, epic.Item.ID, epic.Item.Version, c.sessionID, c.kind)
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), `"error":"`+c.code+`"`) {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
	s2, _, early := epicChildrenServer(t, work.PhaseAssigned)
	rec := agentPost(t, s2, "/v1/work/v2/agent/items/"+early.Item.ID+"/children", "early", map[string]any{
		"expected_version": early.Item.Version, "session_id": epicOwnerConversation, "kind": "feature", "title": "t", "description": "d"})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"error":"epic_not_planned"`) {
		t.Errorf("before the plan gate: %d %s", rec.Code, rec.Body)
	}
	feature, err := s.workV2().Create(context.Background(), app.NewWorkV2{ProjectID: "project-1",
		ProjectPath: epic.Item.ProjectPath, Kind: work.KindFeature, Title: "Loose", Description: "Not an Epic.",
		AcceptanceCriteria: "The loose feature is complete.", Actor: "local"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := s.workV2().Assign(context.Background(), feature.Item.ID, app.AssignWorkV2{ExpectedVersion: feature.Item.Version,
		Mode: "existing_session", SessionID: epicOwnerConversation, Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec = create("not-an-epic", feature.Item.ID, owned.Item.Version, epicOwnerConversation, "feature")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"error":"parent_not_epic"`) {
		t.Errorf("a Feature as the parent: %d %s", rec.Code, rec.Body)
	}
	items, _, err := s.store.WorkV2Items(context.Background(), "", "", "all", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range items {
		if i.ParentID != "" {
			t.Fatalf("a refused create wrote %+v", i)
		}
	}
}

// The Epic's owner moves an existing child to a Session; another Session
// cannot, and an item that is no Epic's child is refused not_epic_child.
func TestTheEpicsOwnerAssignsAnExistingChild(t *testing.T) {
	s, p, epic := epicChildrenServer(t, work.PhaseImplementing)
	created := agentPost(t, s, "/v1/work/v2/agent/items/"+epic.Item.ID+"/children", "child-to-assign", map[string]any{
		"expected_version": epic.Item.Version, "session_id": epicOwnerConversation, "kind": "issue", "title": "t", "description": "d"})
	var got epicChildAnswer
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &got) != nil {
		t.Fatalf("create: %d %s", created.Code, created.Body)
	}
	assign := func(key, id, sessionID string, version int64) *httptest.ResponseRecorder {
		return agentPost(t, s, "/v1/work/v2/agent/items/"+id+"/assign", key, map[string]any{"expected_version": version,
			"session_id": sessionID, "mode": "existing_session", "terminal_id": p.s.ID})
	}
	rec := assign("assign-other", got.Item.ID, "30000000-0000-4000-8000-000000000003", got.Item.Version)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"error":"not_epic_owner"`) {
		t.Fatalf("another Session: %d %s", rec.Code, rec.Body)
	}
	rec = assign("assign-owner", got.Item.ID, epicOwnerConversation, got.Item.Version)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"owner_session":"`+p.s.ConversationID+`"`) {
		t.Fatalf("the owner: %d %s", rec.Code, rec.Body)
	}
	feature, err := s.workV2().Create(context.Background(), app.NewWorkV2{ProjectID: "project-1",
		ProjectPath: epic.Item.ProjectPath, Kind: work.KindFeature, Title: "Loose", Description: "Nobody's child.",
		AcceptanceCriteria: "The loose feature is complete.", Actor: "local"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec = assign("assign-loose", feature.Item.ID, epicOwnerConversation, feature.Item.Version)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"error":"not_epic_child"`) {
		t.Fatalf("an item with no parent: %d %s", rec.Code, rec.Body)
	}
}

// An Epic's owner is told, in every brief, that it may break the Epic into
// children after the reviewed plan and stays responsible for them; every
// other owner is still told to create no Board items.
func TestAnEpicsOwnerIsToldItMayCreateAndAssignChildren(t *testing.T) {
	const id = "0e0e0e0e-0000-4000-8000-000000000001"
	for name, brief := range map[string]string{
		"Root Assignment constraints": workV2RootConstraints(id, work.KindEpic),
		"assignment":                  workV2AssignmentBrief(id, "Big", work.KindEpic),
		"reassignment":                workV2ReassignmentBrief(id, "Big", work.KindEpic, work.PhaseImplementing),
	} {
		for _, want := range []string{"clawdline item child " + id, "clawdline item assign", "--assign-terminal",
			"clawdline guide send", "remain responsible", "all its children are closed"} {
			if !strings.Contains(brief, want) {
				t.Errorf("%s lacks %q:\n%s", name, want, brief)
			}
		}
		if strings.Contains(brief, "Do not create Board items.") {
			t.Errorf("%s still forbids creating the Epic's children:\n%s", name, brief)
		}
	}
	for name, brief := range map[string]string{
		"Root Assignment constraints": workV2RootConstraints(id, work.KindFeature),
		"assignment":                  workV2AssignmentBrief(id, "Small", work.KindFeature),
		"reassignment":                workV2ReassignmentBrief(id, "Small", work.KindIssue, work.PhaseImplementing),
	} {
		if !strings.Contains(brief, "Do not create Board items.") || strings.Contains(brief, "item child") {
			t.Errorf("%s of a Feature/Issue:\n%s", name, brief)
		}
	}
}
