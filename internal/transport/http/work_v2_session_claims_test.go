package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// personItem is an unassigned item the person created on the Board.
func personItem(t *testing.T, s *Server, project, kind, key string) workV2ItemWire {
	t.Helper()
	rec := httptest.NewRecorder()
	s.workV2Route(rec, personWorkV2Request(http.MethodPost, "/v1/work/v2/items",
		`{"project_id":"`+project+`","kind":"`+kind+`","title":"Tidy the notes","description":"Tidy them."}`, key))
	if rec.Code != http.StatusCreated {
		t.Fatalf("person item: %d %s", rec.Code, rec.Body)
	}
	return itemAnswer(t, rec)
}

func itemAnswer(t *testing.T, rec *httptest.ResponseRecorder) workV2ItemWire {
	t.Helper()
	var got struct {
		Item workV2ItemWire `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("%v: %s", err, rec.Body)
	}
	return got.Item
}

func claimBody(session, run string, version int64) string {
	b, _ := json.Marshal(map[string]any{"expected_version": version, "session_id": session,
		"via": map[string]string{"run": run}})
	return string(b)
}

func claim(s *Server, id, body, key string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items/"+id+"/claim", body, key))
	return rec
}

func readItem(t *testing.T, s *Server, id string) workV2ItemWire {
	t.Helper()
	rec := httptest.NewRecorder()
	s.workV2Route(rec, httptest.NewRequest(http.MethodGet, "/v1/work/v2/items/"+id, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}
	return itemAnswer(t, rec)
}

func TestASessionClaimsAnItemOnThePersonsMessageAsIfThePersonAssignedIt(t *testing.T) {
	s, p, project := sessionItemServer(t)
	item := personItem(t, s, project, "feature", "person-item")
	run := issueTestRun(t, s, p, "Please take the notes item on the Board")
	body := claimBody(p.s.ConversationID, run, item.Version)

	rec := claim(s, item.ID, body, "claim-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", rec.Code, rec.Body)
	}
	got := readItem(t, s, item.ID)
	if got.Phase != "assigned" || got.OwnerSession == nil || *got.OwnerSession != p.s.ConversationID ||
		got.Version != item.Version+1 || len(got.Assignments) != 1 {
		t.Fatalf("item: %+v", got)
	}
	a := got.Assignments[0]
	if a.Mode != "existing_session" || a.State != "active" || a.SessionID != p.s.ConversationID ||
		a.TerminalID != p.s.ID || a.Assistant != string(p.s.Assistant) {
		t.Fatalf("assignment: %+v", a)
	}
	if a.ClaimedVia == nil || a.ClaimedVia.Run != run || a.ClaimedVia.SessionID != p.s.ConversationID ||
		a.ClaimedVia.At == 0 || a.ClaimedVia.Excerpt != "Please take the notes item on the Board" {
		t.Fatalf("claimed_via: %+v", a.ClaimedVia)
	}
	var assigned *workV2EventWire
	for n := range got.Events {
		if got.Events[n].Kind == "item.assigned" {
			assigned = &got.Events[n]
		}
	}
	if assigned == nil || assigned.Actor != work.ActorViaSession+run {
		t.Fatalf("history: %+v", got.Events)
	}
	var fields map[string]any
	if err := json.Unmarshal(assigned.Payload, &fields); err != nil || fields["via_run"] != run ||
		fields["claimed"] != true || fields["session_id"] != p.s.ConversationID ||
		fields["excerpt"] != "Please take the notes item on the Board" {
		t.Fatalf("history payload: %s", assigned.Payload)
	}
	if got.ClaimedVia == nil || got.ClaimedVia.Run != run || got.ClaimedVia.Excerpt != a.ClaimedVia.Excerpt {
		t.Fatalf("item claimed_via: %+v", got.ClaimedVia)
	}
	// The Board's page of cards, which carries no assignments, says it too.
	list := httptest.NewRecorder()
	s.workV2Route(list, httptest.NewRequest(http.MethodGet, "/v1/work/v2/items?status=open", nil))
	if list.Code != http.StatusOK || strings.Count(list.Body.String(), `"claimed_via":{"run":"`+run+`"`) != 1 {
		t.Fatalf("list: %d %s", list.Code, list.Body)
	}
	// The Session asked for it: nothing is typed into its terminal.
	if effects := p.done(); len(effects) != 0 {
		t.Fatalf("the route typed into the Session: %v", effects)
	}

	// The same key and body answer what was stored; another body is refused.
	replay := claim(s, item.ID, body, "claim-1")
	if replay.Code != http.StatusOK || replay.Body.String() != rec.Body.String() ||
		replay.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body)
	}
	if reused := claim(s, item.ID, claimBody(p.s.ConversationID, run, 99), "claim-1"); reused.Code == http.StatusOK {
		t.Fatalf("a reused key wrote: %d %s", reused.Code, reused.Body)
	}
	// A second claim of an item already held is refused by name.
	again := claim(s, item.ID, claimBody(p.s.ConversationID, run, got.Version), "claim-2")
	if again.Code != http.StatusConflict || codeOf(t, again) != "item_assigned" {
		t.Fatalf("again: %d %s", again.Code, again.Body)
	}

	// A person's own assignment carries no claim.
	mine := personItem(t, s, project, "issue", "person-item-2")
	assign := httptest.NewRecorder()
	s.workV2Route(assign, personWorkV2Request(http.MethodPost, "/v1/work/v2/items/"+mine.ID+"/assign",
		fmt.Sprintf(`{"expected_version":%d,"mode":"existing_session","terminal_id":"%s"}`, mine.Version, p.s.ID), "person-assign"))
	if assign.Code != http.StatusOK || strings.Contains(assign.Body.String(), "claimed_via") {
		t.Fatalf("person assign: %d %s", assign.Code, assign.Body)
	}
	if list := readItem(t, s, mine.ID); list.ClaimedVia != nil {
		t.Fatalf("a person's assignment reads as a claim: %+v", list.ClaimedVia)
	}
}

// The person's route stays the person's: a Session calling it is refused
// whether or not it holds a run.
func TestThePersonsAssignRouteStillRefusesASession(t *testing.T) {
	s, p, project := sessionItemServer(t)
	item := personItem(t, s, project, "feature", "person-item")
	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/items/"+item.ID+"/assign",
		fmt.Sprintf(`{"expected_version":%d,"mode":"existing_session","terminal_id":"%s"}`, item.Version, p.s.ID), "session-assign"))
	if rec.Code != http.StatusForbidden || codeOf(t, rec) != "session_cannot_create_item" {
		t.Fatalf("session on the person route: %d %s", rec.Code, rec.Body)
	}
	if got := readItem(t, s, item.ID); got.OwnerSession != nil || len(got.Assignments) != 0 {
		t.Fatalf("the refusal assigned: %+v", got)
	}
}

func TestASessionsClaimIsRefusedByNameAndWritesNothing(t *testing.T) {
	s, p, project := sessionItemServer(t)
	item := personItem(t, s, project, "feature", "person-item")
	epic := personItem(t, s, project, "epic", "person-epic")
	cancelled := personItem(t, s, project, "issue", "person-cancelled")
	if _, err := s.workV2().Cancel(context.Background(), cancelled.ID, cancelled.Version, "person", "not needed", nil); err != nil {
		t.Fatal(err)
	}
	cancelled = readItem(t, s, cancelled.ID)
	var elsewhere work.ItemV2
	if err := s.store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		now := time.Now()
		elsewhere = work.ItemV2{ID: "20000000-0000-4000-8000-00000000e15e", ProjectID: "other", ProjectPath: "/elsewhere",
			Kind: work.KindFeature, Title: "Elsewhere", Description: "Elsewhere.", Phase: work.PhaseCreated,
			DeploymentPolicy: work.DeployAgentDecides, CreatedBy: "person", CreatedAt: now, UpdatedAt: now, Cycle: 1, Version: 1}
		return tx.CreateItem(elsewhere, "person", "{}")
	}); err != nil {
		t.Fatal(err)
	}
	mine := issueTestRun(t, s, p, "take that item")
	other, err := s.runs().Issue(context.Background(), session.Session{ID: "pane-9",
		ConversationID: "10000000-0000-4000-8000-000000000009"}, "local", "not to you")
	if err != nil {
		t.Fatal(err)
	}
	me := p.s.ConversationID
	for _, c := range []struct {
		name, id, body string
		status         int
		code           string
	}{
		{"no run", item.ID, claimBody(me, "", item.Version), http.StatusForbidden, "run_unknown"},
		{"unknown run", item.ID, claimBody(me, "0f0f0f0f-0000-4000-8000-000000000001", item.Version),
			http.StatusForbidden, "run_unknown"},
		{"another Session's run", item.ID, claimBody(me, other.ID, item.Version), http.StatusForbidden, "run_other_session"},
		{"no live Session", item.ID, claimBody("10000000-0000-4000-8000-000000000999", mine, item.Version),
			http.StatusNotFound, "session_not_found"},
		{"another Project", elsewhere.ID, claimBody(me, mine, 1), http.StatusConflict, "project_mismatch"},
		{"planning kind", epic.ID, claimBody(me, mine, epic.Version), http.StatusConflict, "planning_not_assignable"},
		{"terminal item", cancelled.ID, claimBody(me, mine, cancelled.Version), http.StatusConflict, "item_terminal"},
		{"stale version", item.ID, claimBody(me, mine, item.Version+7), http.StatusConflict, "version_conflict"},
		{"no such item", "20000000-0000-4000-8000-0000000000ff", claimBody(me, mine, 1), http.StatusNotFound, "work_not_found"},
	} {
		rec := claim(s, c.id, c.body, "refused-"+c.name)
		if rec.Code != c.status || codeOf(t, rec) != c.code {
			t.Fatalf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
	// A person's device is not a Session.
	rec := httptest.NewRecorder()
	s.workV2Route(rec, personWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items/"+item.ID+"/claim",
		claimBody(me, mine, item.Version), "person-on-agent"))
	if rec.Code != http.StatusUnauthorized || codeOf(t, rec) != "machine_required" {
		t.Fatalf("person: %d %s", rec.Code, rec.Body)
	}
	// Without a key nothing is written.
	if rec := claim(s, item.ID, claimBody(me, mine, item.Version), ""); rec.Code == http.StatusOK {
		t.Fatalf("no key: %d %s", rec.Code, rec.Body)
	}
	for _, id := range []string{item.ID, epic.ID, cancelled.ID} {
		if got := readItem(t, s, id); got.OwnerSession != nil || len(got.Assignments) != 0 {
			t.Fatalf("a refusal assigned %s: %+v", id, got)
		}
	}
	// A refused claim released its key: the same key claims once the cause is gone.
	if rec := claim(s, item.ID, claimBody(me, mine, item.Version), "refused-stale version"); rec.Code != http.StatusOK {
		t.Fatalf("retry after refusal: %d %s", rec.Code, rec.Body)
	}
}

func TestAnExpiredRunOrAChildClaimsNothing(t *testing.T) {
	s, p, project := sessionItemServer(t)
	item := personItem(t, s, project, "feature", "person-item")
	at := time.Now().Add(-work.RelayWindow - time.Minute)
	old := s.runs()
	old.Now = func() time.Time { return at }
	expired := issueTestRun(t, s, p, "yesterday")
	old.Now = nil
	if rec := claim(s, item.ID, claimBody(p.s.ConversationID, expired, item.Version), "expired"); rec.Code != http.StatusForbidden ||
		codeOf(t, rec) != "run_expired" {
		t.Fatalf("expired: %d %s", rec.Code, rec.Body)
	}

	r := orchestrator.Record{ID: "7a5c0000-0000-4000-8000-000000000003", Kind: "custom", Title: "child",
		Assistant: "claude", State: orchestrator.StateBriefed, CreatedAt: time.Now(),
		ChildTerminalID: p.s.ID, ChildBackend: "tmux", SpawnedAt: time.Now()}
	body, _ := json.Marshal(r)
	if _, err := s.store.CreateBrokerTask(context.Background(), store.BrokerRow{ID: r.ID, Project: "/p",
		Assistant: "claude", State: string(r.State), CreatedAt: r.CreatedAt, SecretHash: "h", Record: body}, nil); err != nil {
		t.Fatal(err)
	}
	fresh := issueTestRun(t, s, p, "today")
	if rec := claim(s, item.ID, claimBody(p.s.ConversationID, fresh, item.Version), "child"); rec.Code != http.StatusConflict ||
		codeOf(t, rec) != "child_session" {
		t.Fatalf("child: %d %s", rec.Code, rec.Body)
	}
	if got := readItem(t, s, item.ID); got.OwnerSession != nil || len(got.Assignments) != 0 {
		t.Fatalf("a refusal assigned: %+v", got)
	}
}

// One message backs five claims, and the sixth is refused with nothing written.
func TestOneMessageBacksAtMostFiveClaims(t *testing.T) {
	s, p, project := sessionItemServer(t)
	run := issueTestRun(t, s, p, "take these")
	for i := 0; i < 5; i++ {
		item := personItem(t, s, project, "issue", fmt.Sprintf("item-%d", i))
		if rec := claim(s, item.ID, claimBody(p.s.ConversationID, run, item.Version), fmt.Sprintf("claim-%d", i)); rec.Code != http.StatusOK {
			t.Fatalf("claim %d: %d %s", i+1, rec.Code, rec.Body)
		}
	}
	sixth := personItem(t, s, project, "issue", "item-6")
	rec := claim(s, sixth.ID, claimBody(p.s.ConversationID, run, sixth.Version), "claim-6")
	if rec.Code != http.StatusTooManyRequests || codeOf(t, rec) != "run_claims_exhausted" {
		t.Fatalf("sixth: %d %s", rec.Code, rec.Body)
	}
	if got := readItem(t, s, sixth.ID); got.OwnerSession != nil || len(got.Assignments) != 0 {
		t.Fatalf("the sixth claim assigned: %+v", got)
	}
	// Another message has its own budget.
	next := issueTestRun(t, s, p, "and this one")
	if rec := claim(s, sixth.ID, claimBody(p.s.ConversationID, next, sixth.Version), "claim-7"); rec.Code != http.StatusOK {
		t.Fatalf("next message: %d %s", rec.Code, rec.Body)
	}
}
