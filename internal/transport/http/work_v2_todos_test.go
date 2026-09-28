package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func personWorkV2Request(method, path, body, key string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	return req.WithContext(context.WithValue(req.Context(), accessKey{}, access{verdict: auth.Verdict{
		Allowed: true, Device: "phone", Caps: auth.NewCaps(auth.Read, auth.Send),
	}}))
}

func directTodoServer(t *testing.T) (*Server, *pane, string) {
	t.Helper()
	p := &pane{s: session.Session{ID: "pane-4", Backend: session.BackendTmux, Assistant: session.AssistantCodex,
		ConversationID: "10000000-0000-4000-8000-000000000004", State: session.StateIdle}}
	s := paneServer(t, p)
	todo, err := s.workV2().CreateDirectTodo(context.Background(), app.NewDirectTodoV2{
		SessionID: p.s.ConversationID, Text: "handle this", Actor: "device:phone",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, p, todo.ID
}

func TestPersonTodoReadRetainsACompletedDirectTodo(t *testing.T) {
	s, p, id := directTodoServer(t)
	if _, err := s.workV2().CompleteDirectTodo(context.Background(), id, p.s.ConversationID,
		p.s.ConversationID, false, nil); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.workV2Route(rec, personWorkV2Request(http.MethodGet, "/v1/work/v2/session-todos/pane-4", "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}
	var answer struct {
		Direct []directTodoV2Wire `json:"direct_todos"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if len(answer.Direct) != 1 || answer.Direct[0].ID != id || answer.Direct[0].CompletedAt == nil {
		t.Fatalf("completed direct todos: %+v", answer.Direct)
	}
}

func TestPersonCanReopenACompletedDirectTodo(t *testing.T) {
	s, p, id := directTodoServer(t)
	if _, err := s.workV2().MarkDirectTodoSent(context.Background(), id, p.s.ConversationID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.workV2().DirectTodos(context.Background(), p.s.ConversationID, false, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.workV2().CompleteDirectTodo(context.Background(), id, p.s.ConversationID,
		p.s.ConversationID, false, nil); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.workV2Route(rec, personWorkV2Request(http.MethodPost,
		"/v1/work/v2/session-todos/pane-4/"+id+"/reopen", `{}`, "todo-reopen"))
	if rec.Code != http.StatusOK {
		t.Fatalf("reopen: %d %s", rec.Code, rec.Body)
	}
	var answer struct {
		Todo directTodoV2Wire `json:"todo"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Todo.ID != id || answer.Todo.CompletedAt != nil || answer.Todo.CompletedBy != "" ||
		answer.Todo.SentAt == nil || answer.Todo.ReadAt == nil {
		t.Fatalf("reopened todo: %+v", answer.Todo)
	}
	again := httptest.NewRecorder()
	s.workV2Route(again, personWorkV2Request(http.MethodPost,
		"/v1/work/v2/session-todos/pane-4/"+id+"/reopen", `{}`, "todo-reopen-again"))
	if again.Code != http.StatusOK {
		t.Fatalf("reopen an open todo: %d %s", again.Code, again.Body)
	}
}

// A Session row carried across a temporarily unreadable terminal inventory still
// has its durable conversation id. Reading work keyed by that id must not ask
// the terminal layer to prove that the tab is visible right now.
func TestPersonReadsSessionTodosByConversationWhenTheTerminalIsUnavailable(t *testing.T) {
	s, p, id := directTodoServer(t)
	rec := httptest.NewRecorder()
	path := "/v1/work/v2/session-todos/conversation:" + p.s.ConversationID
	s.workV2Route(rec, personWorkV2Request(http.MethodGet, path, "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("read durable Session work without a terminal lookup: %d %s", rec.Code, rec.Body)
	}
	var answer struct {
		Direct []directTodoV2Wire `json:"direct_todos"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if len(answer.Direct) != 1 || answer.Direct[0].ID != id {
		t.Fatalf("direct todos: %+v", answer.Direct)
	}

	// The selector is read-only. A write still has to resolve the live terminal
	// that owns the side effect instead of treating a known conversation id as
	// proof that the tab is actionable.
	write := httptest.NewRecorder()
	s.workV2Route(write, personWorkV2Request(http.MethodPost, path, `{"text":"must not be added"}`, "conversation-write"))
	if write.Code != http.StatusConflict || codeOf(t, write) != "session_unavailable" {
		t.Fatalf("write through a conversation selector: %d %s", write.Code, write.Body)
	}
	if rows, _, err := s.workV2().DirectTodos(context.Background(), p.s.ConversationID, true, false); err != nil || len(rows) != 1 {
		t.Fatalf("refused write changed durable todos: %+v %v", rows, err)
	}

	bad := httptest.NewRecorder()
	s.workV2Route(bad, personWorkV2Request(http.MethodGet,
		"/v1/work/v2/session-todos/conversation:not-a-session", "", ""))
	if bad.Code != http.StatusBadRequest || codeOf(t, bad) != "conversation_id_malformed" {
		t.Fatalf("malformed conversation selector: %d %s", bad.Code, bad.Body)
	}
}

func TestPersonCanRemindOnlyAfterThePreviousDeliveryWasRead(t *testing.T) {
	s, p, id := directTodoServer(t)
	// Sent just now: inside app.DirectTodoResendAfter an unread delivery is
	// still protected from being typed twice.
	if _, err := s.workV2().MarkDirectTodoSent(context.Background(), id, p.s.ConversationID,
		time.Now()); err != nil {
		t.Fatal(err)
	}

	path := "/v1/work/v2/session-todos/pane-4/" + id + "/send"
	rec := httptest.NewRecorder()
	s.workV2Route(rec, personWorkV2Request(http.MethodPost, path, `{}`, "todo-unread-reminder"))
	if rec.Code != http.StatusConflict || codeOf(t, rec) != "todo_awaiting_read" {
		t.Fatalf("unread reminder: %d %s", rec.Code, rec.Body)
	}
	if effects := p.done(); len(effects) != 0 {
		t.Fatalf("unread reminder typed: %v", effects)
	}

	rows, _, err := s.workV2().DirectTodos(context.Background(), p.s.ConversationID, false, true)
	if err != nil || len(rows) != 1 || rows[0].ReadAt.IsZero() {
		t.Fatalf("Agent read: %+v %v", rows, err)
	}
	rec = httptest.NewRecorder()
	s.workV2Route(rec, personWorkV2Request(http.MethodPost, path, `{}`, "todo-read-reminder"))
	if rec.Code != http.StatusOK {
		t.Fatalf("read reminder: %d %s", rec.Code, rec.Body)
	}
	if effects := p.done(); len(effects) != 1 || effects[0] != "send:"+app.DirectTodoSendText(id, "handle this") {
		t.Fatalf("read reminder effects: %v", effects)
	}
	var answer struct {
		Todo directTodoV2Wire `json:"todo"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Todo.SentAt == nil || answer.Todo.ReadAt != nil {
		t.Fatalf("reminder receipt: %+v", answer.Todo)
	}
}

// The first Send types the person's words and then the line that names the
// row: without it the Session that does the work cannot tell which to-do it
// finished (2026-09-25).
func TestFirstSendTypesTheWordsAndTheRowThatChecksThemOff(t *testing.T) {
	s, p, id := directTodoServer(t)
	rec := httptest.NewRecorder()
	s.workV2Route(rec, personWorkV2Request(http.MethodPost, "/v1/work/v2/session-todos/pane-4/"+id+"/send", `{}`, "todo-first-send"))
	if rec.Code != http.StatusOK {
		t.Fatalf("send: %d %s", rec.Code, rec.Body)
	}
	want := "send:handle this\n\n(Clawdline to-do " + id + ". When it is done: clawdline todo done " + id + ")"
	if effects := p.done(); len(effects) != 1 || effects[0] != want {
		t.Fatalf("typed %q, want %q", effects, want)
	}
}

func TestSessionTodosDecodeTerminalNamesOnce(t *testing.T) {
	for _, terminal := range []string{"%4", "%254", "pane-4"} {
		t.Run(terminal, func(t *testing.T) {
			s, p, v := workV2AssignmentServer(t, session.StateIdle)
			p.s.ID = terminal
			if _, err := s.workV2().Assign(context.Background(), v.Item.ID, app.AssignWorkV2{
				ExpectedVersion: v.Item.Version, Mode: "existing_session", SessionID: p.s.ConversationID,
				TerminalID: terminal, Assistant: "codex", Model: "default", Actor: "local",
			}, false, nil); err != nil {
				t.Fatal(err)
			}
			path := "/v1/work/v2/session-todos/" + url.PathEscape(terminal)
			rec := httptest.NewRecorder()
			s.workV2Route(rec, personWorkV2Request(http.MethodGet, path, "", ""))
			if rec.Code != http.StatusOK {
				t.Fatalf("read: %d %s", rec.Code, rec.Body)
			}
			var page struct {
				Assigned []workV2ItemWire `json:"assigned_items"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if len(page.Assigned) != 1 || page.Assigned[0].ID != v.Item.ID {
				t.Fatalf("assigned: %+v", page.Assigned)
			}
			rec = httptest.NewRecorder()
			s.workV2Route(rec, personWorkV2Request(http.MethodPost, path, `{"text":"Check the result"}`, "create-encoded-terminal"))
			if rec.Code != http.StatusCreated {
				t.Fatalf("create: %d %s", rec.Code, rec.Body)
			}
			var created struct {
				Todo directTodoV2Wire `json:"todo"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
				t.Fatal(err)
			}
			rec = httptest.NewRecorder()
			s.workV2Route(rec, personWorkV2Request(http.MethodPost, path+"/"+created.Todo.ID+"/send", `{}`, "send-encoded-terminal"))
			if rec.Code != http.StatusOK {
				t.Fatalf("send: %d %s", rec.Code, rec.Body)
			}
			if effects := p.done(); len(effects) != 1 || effects[0] != "send:"+app.DirectTodoSendText(created.Todo.ID, "Check the result") {
				t.Fatalf("effects: %v", effects)
			}
		})
	}
}
