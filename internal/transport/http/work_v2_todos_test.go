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

func TestPersonCanRemindOnlyAfterThePreviousDeliveryWasRead(t *testing.T) {
	s, p, id := directTodoServer(t)
	if _, err := s.workV2().MarkDirectTodoSent(context.Background(), id, p.s.ConversationID,
		time.Unix(1_790_000_001, 0)); err != nil {
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
	if effects := p.done(); len(effects) != 1 || effects[0] != "send:handle this" {
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
			if effects := p.done(); len(effects) != 1 || effects[0] != "send:Check the result" {
				t.Fatalf("effects: %v", effects)
			}
		})
	}
}
