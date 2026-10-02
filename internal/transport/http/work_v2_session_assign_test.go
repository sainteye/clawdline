package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// A Session handing an ordinary item to a new Session on the person's
// message: POST /v1/work/v2/agent/items/<id>/assign with {"via":{"run"}}.

func runAssignBody(session, run string, version int64, extra map[string]any) map[string]any {
	body := map[string]any{"expected_version": version, "session_id": session, "mode": "new_session",
		"assistant": "claude", "persona": "security", "via": map[string]string{"run": run}}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

func runAssign(t *testing.T, s *Server, id, key string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return agentPost(t, s, "/v1/work/v2/agent/items/"+id+"/assign", key, body)
}

// The assignment is recorded on the person's message, with the assigning
// Session and the persona, the new Session's Root Assignment is opened as
// that persona, and once it is active the Board card says who asked for it.
func TestASessionAssignsAnOrdinaryItemToANewPersonaSessionOnThePersonsMessage(t *testing.T) {
	s, p, project := sessionItemServer(t)
	item := personItem(t, s, project, "feature", "person-item")
	words := "Open a security Session for the notes item"
	run := issueTestRun(t, s, p, words)

	// This broker has no terminal to open a Session in, so the opening fails
	// after the assignment and its Root Assignment are recorded.
	rec := runAssign(t, s, item.ID, "run-assign-1", runAssignBody(p.s.ConversationID, run, item.Version, nil))
	if rec.Code != http.StatusBadGateway || codeOf(t, rec) != "assignment_failed" {
		t.Fatalf("assign: %d %s", rec.Code, rec.Body)
	}
	after, err := s.workV2().Item(context.Background(), item.ID)
	if err != nil || len(after.Assignments) != 1 {
		t.Fatalf("recorded: %+v %v", after.Assignments, err)
	}
	a := after.Assignments[0]
	if a.Mode != "new_session" || a.Persona != "security" || a.HumanActor != work.ActorViaSession+run ||
		a.ClaimedVia == nil || !a.ClaimedVia.Assigned || a.ClaimedVia.Run != run ||
		a.ClaimedVia.Session != p.s.ConversationID || a.ClaimedVia.Persona != "security" || a.ClaimedVia.Excerpt != words {
		t.Fatalf("assignment: %+v via %+v", a, a.ClaimedVia)
	}
	if opened, err := s.broker.RootAssignmentByID(context.Background(), a.RootAssignment); err != nil || opened.Persona != "security" || opened.Label != item.Title {
		t.Fatalf("root assignment %q: %+v %v", a.RootAssignment, opened, err)
	}
	var assigned *work.EventV2
	for n := range after.Events {
		if after.Events[n].Kind == "item.assigned" {
			assigned = &after.Events[n]
		}
	}
	var fields map[string]any
	if assigned == nil || assigned.Actor != work.ActorViaSession+run || json.Unmarshal([]byte(assigned.Payload), &fields) != nil ||
		fields["via_run"] != run || fields["claimed"] != false || fields["assigned_by"] != p.s.ConversationID ||
		fields["persona"] != "security" || fields["excerpt"] != words {
		t.Fatalf("history: %+v", assigned)
	}
	// The Session asked; nothing is typed into its own terminal.
	if effects := p.done(); len(effects) != 0 {
		t.Fatalf("the route typed into the Session: %v", effects)
	}

	// Once a new Session is active, the card carries the message, the
	// assigning Session and the persona; the new Session owns the item.
	fresh := personItem(t, s, project, "issue", "person-item-2")
	claim, err := app.RunAssignment(mustRun(t, s, run), p.s.ConversationID, "security")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := s.workV2().Assign(context.Background(), fresh.ID, app.AssignWorkV2{ExpectedVersion: fresh.Version,
		Mode: "new_session", Assistant: "claude", Actor: work.ActorViaSession + run, Claim: claim, Persona: "security"}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	const opened = "30000000-0000-4000-8000-0000000000a1"
	if _, err := s.workV2().FinishAssignment(context.Background(), fresh.ID, app.FinishAssignmentV2{
		AssignmentID: pending.Assignments[0].ID, SessionID: opened, TerminalID: "pane-new", Actor: work.ActorViaSession + run}); err != nil {
		t.Fatal(err)
	}
	card := readItem(t, s, fresh.ID)
	if card.OwnerSession == nil || *card.OwnerSession != opened || card.ClaimedVia == nil || !card.ClaimedVia.Assigned ||
		card.ClaimedVia.SessionID != p.s.ConversationID || card.ClaimedVia.Persona != "security" || card.ClaimedVia.Excerpt != words {
		t.Fatalf("card: %+v via %+v", card, card.ClaimedVia)
	}
	list := httptest.NewRecorder()
	s.workV2Route(list, httptest.NewRequest(http.MethodGet, "/v1/work/v2/items?status=open", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"assigned":true,"persona":"security"`) {
		t.Fatalf("list: %d %s", list.Code, list.Body)
	}
}

func mustRun(t *testing.T, s *Server, id string) work.Run {
	t.Helper()
	run, err := s.runs().Relay(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

// Without the person's message, and for everything a message does not
// cover, the route refuses by name and writes nothing.
func TestASessionsAssignmentWithoutAMessageOrBeyondItIsRefusedByName(t *testing.T) {
	s, p, project := sessionItemServer(t)
	item := personItem(t, s, project, "feature", "person-item")
	epic := personItem(t, s, project, "epic", "person-epic")
	refactor := personItem(t, s, project, "refactor", "person-refactor")
	mine := issueTestRun(t, s, p, "open a new Session for it")
	other, err := s.runs().Issue(context.Background(), session.Session{ID: "pane-9",
		ConversationID: "10000000-0000-4000-8000-000000000009"}, "local", "not to you")
	if err != nil {
		t.Fatal(err)
	}
	me := p.s.ConversationID
	noVia := runAssignBody(me, "", item.Version, nil)
	delete(noVia, "via")
	for _, c := range []struct {
		name, id string
		body     map[string]any
		status   int
		code     string
	}{
		{"no message", item.ID, noVia, http.StatusConflict, "not_epic_child"},
		{"no run", item.ID, runAssignBody(me, "", item.Version, nil), http.StatusForbidden, "run_unknown"},
		{"unknown run", item.ID, runAssignBody(me, "0f0f0f0f-0000-4000-8000-000000000001", item.Version, nil),
			http.StatusForbidden, "run_unknown"},
		{"another Session's run", item.ID, runAssignBody(me, other.ID, item.Version, nil), http.StatusForbidden, "run_other_session"},
		{"an existing Session", item.ID, runAssignBody(me, mine, item.Version,
			map[string]any{"mode": "existing_session", "terminal_id": p.s.ID, "persona": ""}),
			http.StatusUnprocessableEntity, "new_session_only"},
		{"an Epic", epic.ID, runAssignBody(me, mine, epic.Version, nil), http.StatusUnprocessableEntity, "kind_person_assigns"},
		{"a Refactor", refactor.ID, runAssignBody(me, mine, refactor.Version, nil), http.StatusConflict, "planning_not_assignable"},
		{"an unknown persona", item.ID, runAssignBody(me, mine, item.Version, map[string]any{"persona": "wizard"}),
			http.StatusBadRequest, "unknown_persona"},
		{"a stale version", item.ID, runAssignBody(me, mine, item.Version+3, nil), http.StatusConflict, "version_conflict"},
	} {
		rec := runAssign(t, s, c.id, "refused-"+c.name, c.body)
		if rec.Code != c.status || codeOf(t, rec) != c.code {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
	for _, id := range []string{item.ID, epic.ID, refactor.ID} {
		if got := readItem(t, s, id); got.OwnerSession != nil || len(got.Assignments) != 0 {
			t.Fatalf("a refusal assigned %s: %+v", id, got)
		}
	}
	// An item someone already holds stays with them.
	held := personItem(t, s, project, "issue", "person-held")
	if rec := claim(s, held.ID, claimBody(me, mine, held.Version), "claim-held"); rec.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", rec.Code, rec.Body)
	}
	held = readItem(t, s, held.ID)
	if rec := runAssign(t, s, held.ID, "assign-held", runAssignBody(me, mine, held.Version, nil)); rec.Code != http.StatusConflict ||
		codeOf(t, rec) != "item_assigned" {
		t.Fatalf("held: %d %s", rec.Code, rec.Body)
	}
	if got := readItem(t, s, held.ID); len(got.Assignments) != 1 || got.OwnerSession == nil || *got.OwnerSession != me {
		t.Fatalf("the refusal moved a held item: %+v", got)
	}
}

// Claims and assignments share one message's budget of five: the sixth is
// refused run_claims_exhausted with nothing written, and another message has
// its own budget.
func TestOneMessageBacksAtMostFiveClaimsAndAssignmentsTogether(t *testing.T) {
	s, p, project := sessionItemServer(t)
	run := issueTestRun(t, s, p, "hand these out")
	first := personItem(t, s, project, "issue", "item-claimed")
	if rec := claim(s, first.ID, claimBody(p.s.ConversationID, run, first.Version), "claim-0"); rec.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", rec.Code, rec.Body)
	}
	for i := 1; i < 5; i++ {
		item := personItem(t, s, project, "feature", fmt.Sprintf("item-%d", i))
		rec := runAssign(t, s, item.ID, fmt.Sprintf("assign-%d", i), runAssignBody(p.s.ConversationID, run, item.Version, nil))
		// The opening fails on this broker; the attempt still spent the budget.
		if codeOf(t, rec) != "assignment_failed" {
			t.Fatalf("assign %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	sixth := personItem(t, s, project, "feature", "item-6")
	rec := runAssign(t, s, sixth.ID, "assign-6", runAssignBody(p.s.ConversationID, run, sixth.Version, nil))
	if rec.Code != http.StatusTooManyRequests || codeOf(t, rec) != "run_claims_exhausted" {
		t.Fatalf("sixth: %d %s", rec.Code, rec.Body)
	}
	if got := readItem(t, s, sixth.ID); got.OwnerSession != nil || len(got.Assignments) != 0 || got.Version != sixth.Version {
		t.Fatalf("the sixth assignment wrote: %+v", got)
	}
	next := issueTestRun(t, s, p, "and this one")
	if rec := runAssign(t, s, sixth.ID, "assign-7", runAssignBody(p.s.ConversationID, next, sixth.Version, nil)); codeOf(t, rec) != "assignment_failed" {
		t.Fatalf("next message: %d %s", rec.Code, rec.Body)
	}
}
