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
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// ownTodoServer is a daemon whose live registry holds one assistant Session,
// the one a Session writing its own to-dos resolves to.
func ownTodoServer(t *testing.T) (*Server, *pane) {
	t.Helper()
	p := &pane{s: session.Session{ID: "pane-4", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		ConversationID: "10000000-0000-4000-8000-000000000004", State: session.StateWorking}}
	s := paneServer(t, p)
	s.broker.Live = func(context.Context) []session.Session { return []session.Session{p.s} }
	return s, p
}

func agentWorkV2Request(method, path, body, key string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	return req.WithContext(context.WithValue(req.Context(), accessKey{}, access{
		machine: true, verdict: auth.Verdict{Allowed: true},
	}))
}

func todoBatch(texts ...string) string {
	rows := make([]map[string]string, 0, len(texts))
	for _, t := range texts {
		rows = append(rows, map[string]string{"text": t})
	}
	b, _ := json.Marshal(map[string]any{"todos": rows})
	return string(b)
}

func ownTodoRows(t *testing.T, s *Server, conversation string) []directTodoV2Wire {
	t.Helper()
	rec := httptest.NewRecorder()
	s.workV2Route(rec, personWorkV2Request(http.MethodGet, "/v1/work/v2/session-todos/pane-4", "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("person read: %d %s", rec.Code, rec.Body)
	}
	var page struct {
		Direct []directTodoV2Wire `json:"direct_todos"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	return page.Direct
}

func TestASessionAddsItsOwnTodosAndThePersonSeesWhoWroteThem(t *testing.T) {
	s, p := ownTodoServer(t)
	path := "/v1/work/v2/agent/session-todos/" + p.s.ConversationID
	body := todoBatch("Delete the stale bucket", "Rotate the old key", "Close the unused tunnel")

	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, path, body, "own-todos-1"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var created struct {
		OK    bool               `json:"ok"`
		Todos []directTodoV2Wire `json:"todos"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !created.OK || len(created.Todos) != 3 || created.Todos[1].Text != "Rotate the old key" ||
		created.Todos[0].CreatedBy != p.s.ConversationID || created.Todos[0].ReadAt == nil {
		t.Fatalf("created: %s", rec.Body)
	}

	// The same key and body answer what was stored and add nothing.
	replay := httptest.NewRecorder()
	s.workV2Route(replay, agentWorkV2Request(http.MethodPost, path, body, "own-todos-1"))
	if replay.Code != http.StatusCreated || replay.Body.String() != rec.Body.String() {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body)
	}
	// The same key with another body is refused.
	reused := httptest.NewRecorder()
	s.workV2Route(reused, agentWorkV2Request(http.MethodPost, path, todoBatch("something else"), "own-todos-1"))
	if reused.Code == http.StatusCreated {
		t.Fatalf("a reused key wrote: %d %s", reused.Code, reused.Body)
	}

	rows := ownTodoRows(t, s, p.s.ConversationID)
	if len(rows) != 3 {
		t.Fatalf("person sees %d rows", len(rows))
	}
	for i, want := range []string{"Delete the stale bucket", "Rotate the old key", "Close the unused tunnel"} {
		if rows[i].Text != want || rows[i].CreatedBy != p.s.ConversationID {
			t.Fatalf("row %d = %+v", i, rows[i])
		}
	}
	// A person's row names the person, not the Session.
	person := httptest.NewRecorder()
	s.workV2Route(person, personWorkV2Request(http.MethodPost, "/v1/work/v2/session-todos/pane-4",
		`{"text":"Check the phone"}`, "person-todo"))
	if person.Code != http.StatusCreated {
		t.Fatalf("person create: %d %s", person.Code, person.Body)
	}
	rows = ownTodoRows(t, s, p.s.ConversationID)
	if len(rows) != 4 || rows[3].CreatedBy == p.s.ConversationID || rows[3].CreatedBy == "" {
		t.Fatalf("person row provenance: %+v", rows)
	}
	var raw map[string]any
	personRow, _ := json.Marshal(rows[3])
	_ = json.Unmarshal(personRow, &raw)
	if _, ok := raw["created_by"]; !ok {
		t.Fatalf("created_by is not on the wire: %s", personRow)
	}

	// The Agent may complete its own row.
	done := httptest.NewRecorder()
	s.workV2Route(done, agentWorkV2Request(http.MethodPost, path+"/"+created.Todos[0].ID+"/complete", `{}`, "own-done"))
	if done.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", done.Code, done.Body)
	}

	// Send and delete stay the person's: the Agent surface has no such route.
	for _, c := range []struct{ method, suffix string }{
		{http.MethodPost, "/" + created.Todos[1].ID + "/send"},
		{http.MethodDelete, "/" + created.Todos[1].ID},
		{http.MethodPost, "/" + created.Todos[1].ID + "/delete"},
	} {
		got := httptest.NewRecorder()
		s.workV2Route(got, agentWorkV2Request(c.method, path+c.suffix, `{}`, "agent-"+c.method+c.suffix))
		if got.Code != http.StatusNotFound {
			t.Fatalf("%s %s: %d %s", c.method, c.suffix, got.Code, got.Body)
		}
	}
	if n := len(ownTodoRows(t, s, p.s.ConversationID)); n != 4 {
		t.Fatalf("an Agent send or delete changed the list: %d rows", n)
	}
	if effects := p.done(); len(effects) != 0 {
		t.Fatalf("the Agent route typed into the Session: %v", effects)
	}
}

func TestASessionTodoWriteNeedsMachineAuthAndAKey(t *testing.T) {
	s, p := ownTodoServer(t)
	path := "/v1/work/v2/agent/session-todos/" + p.s.ConversationID
	rec := httptest.NewRecorder()
	s.workV2Route(rec, personWorkV2Request(http.MethodPost, path, todoBatch("x"), "person-key"))
	if rec.Code != http.StatusUnauthorized || codeOf(t, rec) != "machine_required" {
		t.Fatalf("person on the Agent route: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, path, todoBatch("x"), ""))
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusPreconditionRequired {
		t.Fatalf("no key: %d %s", rec.Code, rec.Body)
	}
	if n := len(ownTodoRows(t, s, p.s.ConversationID)); n != 0 {
		t.Fatalf("an unauthenticated or keyless write added %d rows", n)
	}
}

func TestASessionTodoWriteIsRefusedForAnythingButALiveOwnSession(t *testing.T) {
	s, p := ownTodoServer(t)
	twentyOne := make([]string, 21)
	for i := range twentyOne {
		twentyOne[i] = fmt.Sprintf("row %d", i)
	}
	for _, c := range []struct {
		name, conversation, body string
		status                   int
		code                     string
	}{
		{"malformed", "10000000-0000-4000-8000-00000000000A", todoBatch("x"), http.StatusBadRequest, "conversation_id_malformed"},
		{"unknown", "10000000-0000-4000-8000-000000000999", todoBatch("x"), http.StatusNotFound, "session_not_found"},
		{"twenty-one rows", p.s.ConversationID, todoBatch(twentyOne...), http.StatusRequestEntityTooLarge, "too_many_todos"},
		{"blank row", p.s.ConversationID, todoBatch("ok", " "), http.StatusBadRequest, "todo_text_required"},
		{"unknown field", p.s.ConversationID, `{"todos":[{"text":"x","created_by":"someone"}]}`, http.StatusBadRequest, "invalid_request"},
	} {
		rec := httptest.NewRecorder()
		s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/session-todos/"+c.conversation,
			c.body, "refused-"+c.name))
		if rec.Code != c.status || codeOf(t, rec) != c.code {
			t.Fatalf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
	if n := len(ownTodoRows(t, s, p.s.ConversationID)); n != 0 {
		t.Fatalf("refused writes added %d rows", n)
	}
	// A refusal releases its key: the same key with a valid body then writes.
	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/session-todos/"+p.s.ConversationID,
		todoBatch("ok"), "refused-blank row"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("retry after refusal: %d %s", rec.Code, rec.Body)
	}

	// With no registry reading at all, nothing is written.
	s.broker.Live = nil
	rec = httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/session-todos/"+p.s.ConversationID,
		todoBatch("unknown registry"), "unknown-registry"))
	if rec.Code == http.StatusCreated {
		t.Fatalf("an unreadable registry wrote: %d %s", rec.Code, rec.Body)
	}
}

func TestAClawdlineChildCannotWriteSessionTodos(t *testing.T) {
	s, p := ownTodoServer(t)
	r := orchestrator.Record{ID: "7a5c0000-0000-4000-8000-000000000001", Kind: "custom", Title: "child",
		Assistant: "claude", State: orchestrator.StateBriefed, CreatedAt: time.Now(),
		ChildTerminalID: p.s.ID, ChildBackend: "tmux", SpawnedAt: time.Now()}
	body, _ := json.Marshal(r)
	if _, err := s.store.CreateBrokerTask(context.Background(), store.BrokerRow{ID: r.ID, Project: "/p",
		Assistant: "claude", State: string(r.State), CreatedAt: r.CreatedAt, SecretHash: "h", Record: body}, nil); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/session-todos/"+p.s.ConversationID,
		todoBatch("child work"), "child-todos"))
	if rec.Code != http.StatusConflict || codeOf(t, rec) != "child_session" {
		t.Fatalf("child: %d %s", rec.Code, rec.Body)
	}
	if n := len(ownTodoRows(t, s, p.s.ConversationID)); n != 0 {
		t.Fatalf("a child added %d rows", n)
	}
}
