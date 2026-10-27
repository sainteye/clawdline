package http

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sainteye/clawdline/internal/adapters/ownrecords"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app/ports"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
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

func TestPersonEditsDirectTodoAndMustSendTheRevision(t *testing.T) {
	s, p, id := directTodoServer(t)
	first, err := s.workV2().MarkDirectTodoSent(context.Background(), id, p.s.ConversationID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.workV2().DirectTodos(context.Background(), p.s.ConversationID, false, true); err != nil {
		t.Fatal(err)
	}
	path := "/v1/work/v2/session-todos/pane-4/" + id + "/edit"
	body := `{"text":"  handle the revised task  ","expected_version":` + strconv.FormatInt(first.Version+1, 10) + `}`
	rec := httptest.NewRecorder()
	s.workV2Route(rec, personWorkV2Request(http.MethodPost, path, body, "todo-edit"))
	if rec.Code != http.StatusOK {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body)
	}
	var answer struct {
		Todo directTodoV2Wire `json:"todo"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Todo.Text != "handle the revised task" || answer.Todo.SentAt != nil || answer.Todo.ReadAt != nil ||
		answer.Todo.Version != first.Version+2 {
		t.Fatalf("revised todo: %+v", answer.Todo)
	}
	stale := httptest.NewRecorder()
	s.workV2Route(stale, personWorkV2Request(http.MethodPost, path, body, "todo-edit-stale"))
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), "version_conflict") {
		t.Fatalf("stale edit: %d %s", stale.Code, stale.Body)
	}
	replay := httptest.NewRecorder()
	s.workV2Route(replay, personWorkV2Request(http.MethodPost, path, body, "todo-edit"))
	if replay.Code != http.StatusOK || replay.Body.String() != rec.Body.String() {
		t.Fatalf("idempotent edit: %d %s", replay.Code, replay.Body)
	}
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
	if run, ok, err := s.runs().Latest(context.Background(), p.s.ConversationID); err != nil || !ok || run.Excerpt != "handle this" {
		t.Fatalf("sent to-do has no new-work run: %+v, %t, %v", run, ok, err)
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

// The provider has not written a transcript yet. This is the actual failing
// cell: the Workflow must itself be the first message, not a warm-up line.
func TestWorkflowCanBeTheFirstInstruction(t *testing.T) {
	s, p, _ := directTodoServer(t)
	p.s.ConversationID = ""
	p.s.Binding = session.BindingNoRecord
	p.s.PID = os.Getpid()
	if ownrecords.ProcessStart(p.s.PID).IsZero() {
		t.Skip("kernel process identity is unavailable")
	}
	body := `{"text":"Review this project.\n\n[Clawdline-Playbook 00000000-0000-4000-8000-000000000041 Review]"}`
	path := "/v1/work/v2/session-todos/pane-4"
	post := func(path, body, key string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.workV2Route(rec, personWorkV2Request(http.MethodPost, path, body, key))
		return rec
	}
	created := post(path, body, "first-workflow-create")
	if created.Code != http.StatusCreated {
		t.Fatalf("first Workflow: %d %s", created.Code, created.Body)
	}
	var answer struct {
		Todo directTodoV2Wire `json:"todo"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	replay := post(path, body, "first-workflow-create")
	if replay.Code != created.Code || replay.Body.String() != created.Body.String() {
		t.Fatalf("create replay: %d %s", replay.Code, replay.Body)
	}
	sent := post(path+"/"+answer.Todo.ID+"/send", `{}`, "first-workflow-send")
	if sent.Code != http.StatusOK {
		t.Fatalf("first send: %d %s", sent.Code, sent.Body)
	}
	replay = post(path+"/"+answer.Todo.ID+"/send", `{}`, "first-workflow-send")
	if replay.Code != sent.Code || replay.Body.String() != sent.Body.String() {
		t.Fatalf("send replay: %d %s", replay.Code, replay.Body)
	}
	second := post(path, body, "first-workflow-again")
	if second.Code != http.StatusConflict || !strings.Contains(second.Body.String(), "playbook_running") {
		t.Fatalf("second Workflow: %d %s", second.Code, second.Body)
	}
	if len(p.acts) != 1 || !strings.Contains(p.acts[0], "Review this project.") {
		t.Fatalf("terminal writes: %v", p.acts)
	}
	p.s.ConversationID = "20000000-0000-4000-8000-000000000004"
	p.s.Binding = session.BindingOpenFile
	queue := httptest.NewRecorder()
	s.workV2Route(queue, agentWorkV2Request(http.MethodGet, "/v1/work/v2/agent/session-todos/"+p.s.ConversationID, "", ""))
	if queue.Code != http.StatusOK || !strings.Contains(queue.Body.String(), answer.Todo.ID) {
		t.Fatalf("bound queue: %d %s", queue.Code, queue.Body)
	}
	rows, _, err := s.workV2().DirectTodos(context.Background(), p.s.ConversationID, true, false)
	if err != nil || len(rows) != 1 || rows[0].ID != answer.Todo.ID || rows[0].ReadAt.IsZero() || rows[0].SentAt.IsZero() {
		t.Fatalf("bound row: %+v %v", rows, err)
	}
	done := httptest.NewRecorder()
	s.workV2Route(done, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/session-todos/"+p.s.ConversationID+"/"+answer.Todo.ID+"/complete", `{}`, "first-workflow-done"))
	if done.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", done.Code, done.Body)
	}
}

// Unknown/unreadable is deliberately not the fresh-Session case.
func TestFirstWorkflowRefusesUnprovenProcessBindings(t *testing.T) {
	for _, binding := range []session.Binding{session.BindingUnreadable, session.BindingAmbiguous, ""} {
		t.Run(string(binding), func(t *testing.T) {
			s, p, _ := directTodoServer(t)
			p.s.ConversationID, p.s.Binding, p.s.PID = "", binding, os.Getpid()
			rec := httptest.NewRecorder()
			s.workV2Route(rec, personWorkV2Request(http.MethodPost, "/v1/work/v2/session-todos/pane-4", `{"text":"Review"}`, "unproven-create"))
			if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "session_unavailable") || len(p.acts) != 0 {
				t.Fatalf("unproven binding: %d %s acts=%v", rec.Code, rec.Body, p.acts)
			}
		})
	}
}

type firstTodoFailureHost struct {
	*pane
	failure   error
	afterSend func()
}

func (h *firstTodoFailureHost) Send(ctx context.Context, s session.Session, text string) error {
	h.pane.Send(ctx, s, text)
	if h.afterSend != nil {
		h.afterSend()
	}
	return h.failure
}

func TestFirstWorkflowDeliveryFailuresNeverDuplicateAnUncertainWrite(t *testing.T) {
	for _, tc := range []struct {
		name      string
		failure   error
		safeRetry bool
	}{
		{"definitely unsent", terminal.Unsent{Why: "injected before first byte"}, true},
		{"answer lost", errors.New("injected after a possible terminal write"), false},
		{"typed but unsubmitted", terminal.Unsubmitted{Why: "injected Enter withheld"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, p, _ := directTodoServer(t)
			p.s.ConversationID, p.s.Binding, p.s.PID = "", session.BindingNoRecord, os.Getpid()
			owner := app.PendingTodoOwner(p.s)
			if owner == "" {
				t.Skip("kernel process identity is unavailable")
			}
			td, err := s.workV2().CreateDirectTodo(context.Background(), app.NewDirectTodoV2{SessionID: owner, Text: "First instruction", Actor: "device:phone"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			h := &firstTodoFailureHost{pane: p, failure: tc.failure}
			s.terminals = []ports.TerminalHost{h}
			s.inventory.Terminals = s.terminals
			path := "/v1/work/v2/session-todos/pane-4/" + td.ID + "/send"
			rec := httptest.NewRecorder()
			s.workV2Route(rec, personWorkV2Request(http.MethodPost, path, `{}`, "first-failure"))
			if rec.Code != http.StatusBadGateway {
				t.Fatalf("injected send: %d %s", rec.Code, rec.Body)
			}
			// A new conversation id must not erase the unresolved terminal attempt.
			p.s.ConversationID = "20000000-0000-4000-8000-000000000004"
			p.s.Binding = session.BindingOpenFile
			h.failure = nil
			retry := httptest.NewRecorder()
			s.workV2Route(retry, personWorkV2Request(http.MethodPost, path, `{}`, "retry-failure"))
			if tc.safeRetry {
				if retry.Code != http.StatusOK || len(p.acts) != 2 {
					t.Fatalf("safe retry: %d %s writes=%v", retry.Code, retry.Body, p.acts)
				}
			} else if retry.Code != http.StatusConflict || !strings.Contains(retry.Body.String(), "todo_delivery_unknown") || len(p.acts) != 1 {
				t.Fatalf("uncertain retry: %d %s writes=%v", retry.Code, retry.Body, p.acts)
			}
		})
	}
}

func TestFirstWorkflowQueueCanBindWhileTheTerminalSendIsReturning(t *testing.T) {
	s, p, _ := directTodoServer(t)
	p.s.ConversationID, p.s.Binding, p.s.PID = "", session.BindingNoRecord, os.Getpid()
	owner := app.PendingTodoOwner(p.s)
	if owner == "" {
		t.Skip("kernel process identity is unavailable")
	}
	td, err := s.workV2().CreateDirectTodo(context.Background(), app.NewDirectTodoV2{SessionID: owner, Text: "First instruction", Actor: "device:phone"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := &firstTodoFailureHost{pane: p, afterSend: func() {
		p.s.ConversationID = "20000000-0000-4000-8000-000000000004"
		p.s.Binding = session.BindingOpenFile
		queue := httptest.NewRecorder()
		s.workV2Route(queue, agentWorkV2Request(http.MethodGet, "/v1/work/v2/agent/session-todos/"+p.s.ConversationID, "", ""))
		if queue.Code != http.StatusOK || !strings.Contains(queue.Body.String(), td.ID) {
			t.Fatalf("queue during send: %d %s", queue.Code, queue.Body)
		}
	}}
	s.terminals = []ports.TerminalHost{h}
	s.inventory.Terminals = s.terminals
	sent := httptest.NewRecorder()
	s.workV2Route(sent, personWorkV2Request(http.MethodPost, "/v1/work/v2/session-todos/pane-4/"+td.ID+"/send", `{}`, "racing-send"))
	if sent.Code != http.StatusOK {
		t.Fatalf("send: %d %s", sent.Code, sent.Body)
	}
	rows, _, err := s.workV2().DirectTodos(context.Background(), p.s.ConversationID, true, false)
	if err != nil || len(rows) != 1 || rows[0].ID != td.ID || rows[0].SentAt.IsZero() || rows[0].ReadAt.IsZero() {
		t.Fatalf("racing bind: %+v %v", rows, err)
	}
}
