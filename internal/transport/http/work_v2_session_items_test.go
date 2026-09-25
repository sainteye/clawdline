package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// sessionItemServer is a daemon whose live registry holds one assistant
// Session working in a Project of its own, and the Project's catalog id.
func sessionItemServer(t *testing.T) (*Server, *pane, string) {
	t.Helper()
	home := placesHome(t)
	dir := filepath.Join(home, "code", "notes")
	recordPlace(t, home, dir)
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	canonical, ok := projects.CanonicalProjectKey(dir)
	if !ok {
		t.Fatalf("no canonical Project for %s", dir)
	}
	p := &pane{s: session.Session{ID: "pane-4", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		CWD: canonical, ConversationID: "10000000-0000-4000-8000-000000000004", State: session.StateIdle}}
	s := paneServer(t, p)
	s.icons = icon.NewRegistry()
	s.broker.Live = func(context.Context) []session.Session { return []session.Session{p.s} }
	for _, place := range s.projectReaders().places.List(s.liveDirectories(context.Background()), 40) {
		if got, _ := projects.CanonicalProjectKey(place.Path); got == canonical {
			return s, p, place.ID
		}
	}
	t.Fatalf("the Session's Project is not in the catalog: %s", canonical)
	return nil, nil, ""
}

func sessionItemBody(t *testing.T, session, run, project, kind string, steps ...string) string {
	t.Helper()
	body := map[string]any{"session_id": session, "via": map[string]string{"run": run}, "project_id": project,
		"kind": kind, "title": "Tidy the release notes", "description": "The person asked for this.\n- draft\n- review"}
	if len(steps) > 0 {
		body["steps"] = steps
	}
	b, _ := json.Marshal(body)
	return string(b)
}

func issueTestRun(t *testing.T, s *Server, p *pane, text string) string {
	t.Helper()
	run, err := s.runs().Issue(context.Background(), p.s, "local", text)
	if err != nil {
		t.Fatal(err)
	}
	return run.ID
}

func TestASessionCreatesAnItemOnThePersonsMessageAndThePersonSeesTheirWords(t *testing.T) {
	s, p, project := sessionItemServer(t)
	run := issueTestRun(t, s, p, "Make a Board item: draft, check and publish the notes")
	body := sessionItemBody(t, p.s.ConversationID, run, project, "feature", "draft", "check", "publish")

	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items", body, "session-item-1"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var created struct {
		OK   bool           `json:"ok"`
		Item workV2ItemWire `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	it := created.Item
	if it.Phase != "assigned" || it.OwnerSession == nil || *it.OwnerSession != p.s.ConversationID ||
		it.CreatedBy != work.ActorViaSession+run || len(it.Assignments) != 1 ||
		it.Assignments[0].State != "active" || it.Assignments[0].TerminalID != p.s.ID {
		t.Fatalf("item: %s", rec.Body)
	}
	var titles []string
	for _, st := range it.Steps {
		titles = append(titles, st.Title)
	}
	if strings.Join(titles, "|") != "draft|check|publish" {
		t.Fatalf("steps: %v", titles)
	}
	if it.CreatedVia == nil || it.CreatedVia.Run != run || it.CreatedVia.At == 0 ||
		it.CreatedVia.Excerpt != "Make a Board item: draft, check and publish the notes" {
		t.Fatalf("created_via: %+v", it.CreatedVia)
	}
	// The Session asked for it: nothing is typed into its terminal, and none of
	// the steps became a Session to-do.
	if effects := p.done(); len(effects) != 0 {
		t.Fatalf("the route typed into the Session: %v", effects)
	}
	if rows := ownTodoRows(t, s, p.s.ConversationID); len(rows) != 0 {
		t.Fatalf("steps became %d Session to-dos", len(rows))
	}

	// The same key and body answer what was stored; another body is refused.
	replay := httptest.NewRecorder()
	s.workV2Route(replay, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items", body, "session-item-1"))
	if replay.Code != http.StatusCreated || replay.Body.String() != rec.Body.String() ||
		replay.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body)
	}
	reused := httptest.NewRecorder()
	s.workV2Route(reused, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items",
		sessionItemBody(t, p.s.ConversationID, run, project, "issue"), "session-item-1"))
	if reused.Code == http.StatusCreated {
		t.Fatalf("a reused key wrote: %d %s", reused.Code, reused.Body)
	}

	// A person's read of the Board carries the provenance; a person's own item
	// carries none.
	person := httptest.NewRecorder()
	s.workV2Route(person, personWorkV2Request(http.MethodPost, "/v1/work/v2/items",
		`{"project_id":"`+project+`","kind":"issue","title":"Mine","description":"Mine"}`, "person-item"))
	if person.Code != http.StatusCreated || strings.Contains(person.Body.String(), "created_via") {
		t.Fatalf("person item: %d %s", person.Code, person.Body)
	}
	list := httptest.NewRecorder()
	s.workV2Route(list, httptest.NewRequest(http.MethodGet, "/v1/work/v2/items?status=open", nil))
	if list.Code != http.StatusOK || strings.Count(list.Body.String(), `"created_via":{"run":"`+run+`"`) != 1 {
		t.Fatalf("list: %d %s", list.Code, list.Body)
	}
}

func TestASessionCreatedItemIsRefusedByNameAndWritesNothing(t *testing.T) {
	s, p, project := sessionItemServer(t)
	mine := issueTestRun(t, s, p, "make an item")
	other, err := s.runs().Issue(context.Background(), session.Session{ID: "pane-9",
		ConversationID: "10000000-0000-4000-8000-000000000009"}, "local", "not to you")
	if err != nil {
		t.Fatal(err)
	}
	tooMany := make([]string, store.WorkV2StepLimit+1)
	for i := range tooMany {
		tooMany[i] = "step"
	}
	for _, c := range []struct {
		name, body string
		status     int
		code       string
	}{
		{"no run", sessionItemBody(t, p.s.ConversationID, "", project, "feature"), http.StatusForbidden, "run_unknown"},
		{"unknown run", sessionItemBody(t, p.s.ConversationID, "0f0f0f0f-0000-4000-8000-000000000001", project, "feature"),
			http.StatusForbidden, "run_unknown"},
		{"another Session's run", sessionItemBody(t, p.s.ConversationID, other.ID, project, "feature"),
			http.StatusForbidden, "run_other_session"},
		{"no live Session", sessionItemBody(t, "10000000-0000-4000-8000-000000000999", mine, project, "feature"),
			http.StatusNotFound, "session_not_found"},
		{"unknown Project", sessionItemBody(t, p.s.ConversationID, mine, "nope", "feature"),
			http.StatusUnprocessableEntity, "project_not_found"},
		{"bad kind", sessionItemBody(t, p.s.ConversationID, mine, project, "chore"), http.StatusConflict, "invalid_kind"},
		{"too many steps", sessionItemBody(t, p.s.ConversationID, mine, project, "feature", tooMany...),
			http.StatusRequestEntityTooLarge, "too_many_steps"},
	} {
		rec := httptest.NewRecorder()
		s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items", c.body, "refused-"+c.name))
		if rec.Code != c.status || codeOf(t, rec) != c.code {
			t.Fatalf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
	// A person's device is not a Session.
	rec := httptest.NewRecorder()
	s.workV2Route(rec, personWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items",
		sessionItemBody(t, p.s.ConversationID, mine, project, "feature"), "person-on-agent"))
	if rec.Code != http.StatusUnauthorized || codeOf(t, rec) != "machine_required" {
		t.Fatalf("person: %d %s", rec.Code, rec.Body)
	}
	// Without a key nothing is written.
	rec = httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items",
		sessionItemBody(t, p.s.ConversationID, mine, project, "feature"), ""))
	if rec.Code == http.StatusCreated {
		t.Fatalf("no key: %d %s", rec.Code, rec.Body)
	}
	items, _, err := s.workV2().List(context.Background(), "", "", "all", "")
	if err != nil || len(items) != 0 {
		t.Fatalf("refusals wrote %d items (%v)", len(items), err)
	}

	// Five items on one message, and the sixth is refused.
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items",
			sessionItemBody(t, p.s.ConversationID, mine, project, "issue"), "five-"+string(rune('a'+i))))
		if rec.Code != http.StatusCreated {
			t.Fatalf("item %d: %d %s", i+1, rec.Code, rec.Body)
		}
	}
	rec = httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items",
		sessionItemBody(t, p.s.ConversationID, mine, project, "issue"), "sixth"))
	if rec.Code != http.StatusTooManyRequests || codeOf(t, rec) != "run_items_exhausted" {
		t.Fatalf("sixth: %d %s", rec.Code, rec.Body)
	}
}

func TestAnExpiredRunOrAChildCreatesNoItem(t *testing.T) {
	s, p, project := sessionItemServer(t)
	at := time.Now().Add(-work.RelayWindow - time.Minute)
	old := s.runs()
	old.Now = func() time.Time { return at }
	expired := issueTestRun(t, s, p, "yesterday")
	old.Now = nil
	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items",
		sessionItemBody(t, p.s.ConversationID, expired, project, "feature"), "expired"))
	if rec.Code != http.StatusForbidden || codeOf(t, rec) != "run_expired" {
		t.Fatalf("expired: %d %s", rec.Code, rec.Body)
	}

	r := orchestrator.Record{ID: "7a5c0000-0000-4000-8000-000000000002", Kind: "custom", Title: "child",
		Assistant: "claude", State: orchestrator.StateBriefed, CreatedAt: time.Now(),
		ChildTerminalID: p.s.ID, ChildBackend: "tmux", SpawnedAt: time.Now()}
	body, _ := json.Marshal(r)
	if _, err := s.store.CreateBrokerTask(context.Background(), store.BrokerRow{ID: r.ID, Project: "/p",
		Assistant: "claude", State: string(r.State), CreatedAt: r.CreatedAt, SecretHash: "h", Record: body}, nil); err != nil {
		t.Fatal(err)
	}
	fresh := issueTestRun(t, s, p, "today")
	rec = httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items",
		sessionItemBody(t, p.s.ConversationID, fresh, project, "feature"), "child"))
	if rec.Code != http.StatusConflict || codeOf(t, rec) != "child_session" {
		t.Fatalf("child: %d %s", rec.Code, rec.Body)
	}
	items, _, _ := s.workV2().List(context.Background(), "", "", "all", "")
	if len(items) != 0 {
		t.Fatalf("refusals wrote %d items", len(items))
	}
}
