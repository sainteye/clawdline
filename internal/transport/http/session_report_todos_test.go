package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// reportServer is a Server whose broker sees one live assistant Session.
func reportServer(t *testing.T) (*Server, session.Session) {
	t.Helper()
	p := &pane{s: session.Session{ID: "pane-9", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		ConversationID: "10000000-0000-4000-8000-000000000009", State: session.StateIdle}}
	s := paneServer(t, p)
	s.broker.Live = func(context.Context) []session.Session { return []session.Session{p.s} }
	return s, p.s
}

func reportTurn(t *testing.T, s *Server, terminal string) contract.BrokerSessionDelivery {
	t.Helper()
	rec := httptest.NewRecorder()
	s.brokerSessionRoute(rec, w8Request(http.MethodPost, "/v1/orchestrator/sessions/"+terminal+"/complete",
		`{"summary":"Fixed and deployed."}`, true, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", rec.Code, rec.Body)
	}
	var answer contract.BrokerSessionDelivery
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if !answer.OK || !answer.Created {
		t.Fatalf("the receipt was not recorded: %s", rec.Body)
	}
	return answer
}

// The turn receipt lists the to-dos the person sent that the Session has not
// checked off — sent or read, never one still unsent or already done — and
// reading them for the answer does not count as the Session reading them.
func TestTurnReceiptListsTheSentToDosStillOpen(t *testing.T) {
	s, sess := reportServer(t)
	ctx := context.Background()
	w := s.workV2()
	mk := func(text string) string {
		td, err := w.CreateDirectTodo(ctx, app.NewDirectTodoV2{SessionID: sess.ConversationID, Text: text, Actor: "device:phone"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return td.ID
	}
	sentFirst := mk("Fix the login page\nand deploy it " + strings.Repeat("x", 200))
	unsent := mk("Not sent yet")
	done := mk("Sent and finished")
	sentSecond := mk("Rename the button")
	at := time.Now().Add(-time.Hour)
	for _, id := range []string{sentFirst, done, sentSecond} {
		if _, err := w.MarkDirectTodoSent(ctx, id, sess.ConversationID, at); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.CompleteDirectTodo(ctx, done, sess.ConversationID, sess.ConversationID, false, nil); err != nil {
		t.Fatal(err)
	}

	answer := reportTurn(t, s, sess.ID)
	if answer.OpenTodosUnknown || answer.OpenTodosTruncated {
		t.Fatalf("flags: %+v", answer)
	}
	if len(answer.OpenTodos) != 2 || answer.OpenTodos[0].ID != sentFirst || answer.OpenTodos[1].ID != sentSecond {
		t.Fatalf("open to-dos (want %s then %s, not %s or %s): %+v", sentFirst, sentSecond, unsent, done, answer.OpenTodos)
	}
	first := answer.OpenTodos[0]
	if first.SentAt == nil || *first.SentAt != at.Unix() || first.ReadAt != nil {
		t.Fatalf("times: %+v", first)
	}
	if strings.Contains(first.Text, "\n") || !strings.HasPrefix(first.Text, "Fix the login page and deploy it") ||
		len([]rune(first.Text)) != 120 {
		t.Fatalf("preview: %q (%d runes)", first.Text, len([]rune(first.Text)))
	}
	rows, _, err := w.DirectTodos(ctx, sess.ConversationID, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, td := range rows {
		if !td.ReadAt.IsZero() {
			t.Fatalf("the report marked %s read", td.ID)
		}
	}
}

// With nothing sent the list is empty, not absent.
func TestTurnReceiptWithNothingSentListsAnEmptyArray(t *testing.T) {
	s, sess := reportServer(t)
	rec := httptest.NewRecorder()
	s.brokerSessionRoute(rec, w8Request(http.MethodPost, "/v1/orchestrator/sessions/"+sess.ID+"/complete",
		`{"summary":"Nothing to check off."}`, true, nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"open_todos":[]`) ||
		!strings.Contains(rec.Body.String(), `"open_todos_unknown":false`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

// When the to-dos cannot be read the receipt still stands, and the answer says
// unknown rather than an empty list that would read as none.
func TestTurnReceiptSaysUnknownWhenTheToDosCannotBeRead(t *testing.T) {
	s, sess := reportServer(t)
	broken, err := store.Open(filepath.Join(t.TempDir(), "broken"))
	if err != nil {
		t.Fatal(err)
	}
	_ = broken.Close()
	s.store = broken // the broker keeps its own, open store for the receipt
	answer := reportTurn(t, s, sess.ID)
	if !answer.OpenTodosUnknown || len(answer.OpenTodos) != 0 {
		t.Fatalf("unknown: %+v", answer)
	}
}
