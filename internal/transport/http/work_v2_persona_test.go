package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/work"
)

// The person's assign route takes a persona with a new Session: the
// assignment records it, the Board wire carries it, and the Root Assignment
// that opens the Session names it. A persona with an existing Session, and a
// name the catalog lacks, are refused by name before anything is written.
func TestThePersonsNewSessionAssignmentCarriesAPersona(t *testing.T) {
	s, p, project := sessionItemServer(t)
	assign := func(item workV2ItemWire, key, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.workV2Route(rec, personWorkV2Request(http.MethodPost, "/v1/work/v2/items/"+item.ID+"/assign",
			fmt.Sprintf(`{"expected_version":%d,%s}`, item.Version, body), key))
		return rec
	}

	existing := personItem(t, s, project, "feature", "persona-existing")
	rec := assign(existing, "persona-existing-assign",
		fmt.Sprintf(`"mode":"existing_session","terminal_id":"%s","persona":"architect"`, p.s.ID))
	if rec.Code != http.StatusUnprocessableEntity || codeOf(t, rec) != "persona_not_applicable" {
		t.Fatalf("persona on an existing Session: %d %s", rec.Code, rec.Body)
	}
	if got := readItem(t, s, existing.ID); got.OwnerSession != nil || len(got.Assignments) != 0 || len(p.done()) != 0 {
		t.Fatalf("the refusal assigned: %+v", got)
	}

	rec = assign(existing, "persona-unknown-assign", `"mode":"new_session","assistant":"claude","persona":"wizard"`)
	if rec.Code != http.StatusBadRequest || codeOf(t, rec) != "unknown_persona" ||
		!strings.Contains(rec.Body.String(), "architect") {
		t.Fatalf("unknown persona: %d %s", rec.Code, rec.Body)
	}
	if got := readItem(t, s, existing.ID); len(got.Assignments) != 0 || got.Version != existing.Version {
		t.Fatalf("the refusal wrote: %+v", got)
	}

	// This broker has no terminal to open a Session in, so the assignment
	// fails after its record, and the Root Assignment's, name the persona.
	_ = assign(existing, "persona-new-assign", `"mode":"new_session","assistant":"claude","persona":"architect"`)
	after, err := s.workV2().Item(context.Background(), existing.ID)
	if err != nil || len(after.Assignments) != 1 || after.Assignments[0].Persona != "architect" {
		t.Fatalf("recorded assignment: %+v %v", after.Assignments, err)
	}
	wire := readItem(t, s, existing.ID)
	if len(wire.Assignments) != 1 || wire.Assignments[0].Persona != "architect" {
		t.Fatalf("assignment wire: %+v", wire.Assignments)
	}
	ra := after.Assignments[0].RootAssignment
	if opened, err := s.broker.RootAssignmentByID(context.Background(), ra); err != nil || opened.Persona != "architect" {
		t.Fatalf("root assignment %q: %+v %v", ra, opened, err)
	}

	// None is none: an existing Session with an empty persona is assigned,
	// and the wire leaves the field out.
	plain := personItem(t, s, project, "issue", "persona-plain")
	rec = assign(plain, "persona-plain-assign",
		fmt.Sprintf(`"mode":"existing_session","terminal_id":"%s","persona":""`, p.s.ID))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"persona"`) {
		t.Fatalf("no persona: %d %s", rec.Code, rec.Body)
	}
}

// The Epic owner's two routes take the same field with the same refusals;
// on the children route a refused persona creates no child.
func TestTheEpicOwnersAssignmentCarriesAPersona(t *testing.T) {
	s, p, epic := epicChildrenServer(t, work.PhaseImplementing)
	children := "/v1/work/v2/agent/items/" + epic.Item.ID + "/children"
	child := func(key string, version int64, assign map[string]any) *httptest.ResponseRecorder {
		return agentPost(t, s, children, key, map[string]any{"expected_version": version,
			"session_id": epicOwnerConversation, "kind": "feature", "title": "Part " + key, "description": "Build it.",
			"acceptance_criteria": "The part is complete.",
			"assign":              assign})
	}
	countChildren := func() int {
		items, _, err := s.store.WorkV2Items(context.Background(), "", "", "all", "", 100)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, i := range items {
			if i.ParentID == epic.Item.ID {
				n++
			}
		}
		return n
	}

	rec := child("c-existing", epic.Item.Version,
		map[string]any{"mode": "existing_session", "terminal_id": p.s.ID, "persona": "backend"})
	if rec.Code != http.StatusUnprocessableEntity || codeOf(t, rec) != "persona_not_applicable" || countChildren() != 0 {
		t.Fatalf("children, existing Session: %d %s", rec.Code, rec.Body)
	}
	rec = child("c-unknown", epic.Item.Version, map[string]any{"mode": "new_session", "persona": "wizard"})
	if rec.Code != http.StatusBadRequest || codeOf(t, rec) != "unknown_persona" || countChildren() != 0 {
		t.Fatalf("children, unknown persona: %d %s", rec.Code, rec.Body)
	}
	rec = child("c-new", epic.Item.Version, map[string]any{"mode": "new_session", "assistant": "claude", "persona": "backend"})
	var got epicChildAnswer
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("children, new Session: %d %s", rec.Code, rec.Body)
	}
	recorded, err := s.workV2().Item(context.Background(), got.Item.ID)
	if err != nil || len(recorded.Assignments) != 1 || recorded.Assignments[0].Persona != "backend" ||
		len(got.Item.Assignments) != 1 || got.Item.Assignments[0].Persona != "backend" {
		t.Fatalf("children, recorded: %+v / wire %+v %v", recorded.Assignments, got.Item.Assignments, err)
	}

	fresh, _ := s.workV2().Item(context.Background(), got.Item.ID)
	assign := func(key string, body map[string]any) *httptest.ResponseRecorder {
		body["expected_version"], body["session_id"] = fresh.Item.Version, epicOwnerConversation
		return agentPost(t, s, "/v1/work/v2/agent/items/"+got.Item.ID+"/assign", key, body)
	}
	rec = assign("a-existing", map[string]any{"mode": "existing_session", "terminal_id": p.s.ID, "persona": "security"})
	if rec.Code != http.StatusUnprocessableEntity || codeOf(t, rec) != "persona_not_applicable" {
		t.Fatalf("assign, existing Session: %d %s", rec.Code, rec.Body)
	}
	rec = assign("a-unknown", map[string]any{"mode": "new_session", "persona": "Architect"})
	if rec.Code != http.StatusBadRequest || codeOf(t, rec) != "unknown_persona" {
		t.Fatalf("assign, unknown persona: %d %s", rec.Code, rec.Body)
	}
	if again, _ := s.workV2().Item(context.Background(), got.Item.ID); len(again.Assignments) != 1 ||
		again.Item.Version != fresh.Item.Version {
		t.Fatalf("a refused assign wrote: %+v", again)
	}
	_ = assign("a-new", map[string]any{"mode": "new_session", "assistant": "codex", "persona": "security"})
	moved, err := s.workV2().Item(context.Background(), got.Item.ID)
	personas := []string{}
	for _, a := range moved.Assignments {
		personas = append(personas, a.Persona)
	}
	if err != nil || len(moved.Assignments) != 2 || !strings.Contains(strings.Join(personas, ","), "security") {
		t.Fatalf("assign, recorded: %+v %v", moved.Assignments, err)
	}
}
