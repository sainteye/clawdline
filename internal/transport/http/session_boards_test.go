package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/cloudops"

	sb "github.com/sainteye/clawdline/internal/domain/sessionboard"
)

func TestSessionBoardRefusesTheNextConversationOnTheSameTerminal(t *testing.T) {
	s, pane, _ := sessionItemServer(t)
	project := projects.ProjectID(pane.s.CWD)
	pane.s.PID = os.Getpid()
	old := pane.s.ConversationID
	if _, _, ok := s.sessionBoardActor(t.Context(), old, false); !ok {
		t.Fatal("test Session identity did not resolve")
	}
	call := func(conversation, action, board string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := agentWorkV2Request(http.MethodPost, sessionBoardRoute+"?conversation_id="+url.QueryEscape(conversation), action, "test-key")
		if action == "" {
			req = agentWorkV2Request(http.MethodGet, sessionBoardRoute+"?conversation_id="+url.QueryEscape(conversation)+"&board="+board, "", "")
		}
		rec := httptest.NewRecorder()
		s.sessionBoardsRoute(rec, req)
		if rec.Code != want {
			t.Fatalf("%s: got %d, want %d, response %s", action, rec.Code, want, rec.Body)
		}
		return rec
	}

	rec := call(old, `{"action":"create","name":"Acceptance","text":"Compare one build"}`, "", http.StatusOK)
	var created sb.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("create receipt: %s (%v)", rec.Body, err)
	}
	rec = call(old, "", created.ID, http.StatusOK)
	var page sb.Page
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || page.Board.Project != project || len(page.Board.Members) != 1 {
		t.Fatalf("board page: %s (%v)", rec.Body, err)
	}
	pane.s.ConversationID = "20000000-0000-4000-8000-000000000004"
	call(old, "", created.ID, http.StatusForbidden)
	call(pane.s.ConversationID, "", created.ID, http.StatusForbidden)
	call(pane.s.ConversationID, `{"action":"join","board":"`+created.ID+`"}`, "", http.StatusOK)
	call(pane.s.ConversationID, "", created.ID, http.StatusOK)
	call(pane.s.ConversationID, `{"action":"archive","board":"`+created.ID+`"}`, "", http.StatusForbidden)

	req := personWorkV2Request(http.MethodGet, sessionBoardRoute+"?conversation_id="+pane.s.ConversationID, "", "")
	rec = httptest.NewRecorder()
	s.sessionBoardsRoute(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("paired person selected a Session actor: %d %s", rec.Code, rec.Body)
	}
}

func TestSessionBoardCloudOperationsCrossTheRealDeviceGate(t *testing.T) {
	f := projectCloudStandIn(t)
	now := time.Now()
	lead := sb.Actor{ID: "codex:lead", Label: "Lead"}
	result, err := f.server.store.ChangeSessionBoard(t.Context(), f.place, lead, sb.Actor{},
		sb.Command{Action: "create", Name: "Cloud acceptance", Text: "See one board"}, "create", now)
	if err != nil {
		t.Fatal(err)
	}
	post := map[string]any{
		"type": "session-boards.change", "session": cloudops.MachineReplySession, "request": "stable-board-post",
		"command": map[string]any{"action": "post", "board": result.ID, "text": "Only once"},
	}
	for _, seq := range []uint64{10, 11} {
		answer, p := f.ask(t, seq, post)
		if !answer.OK() || bodyOf(t, p)["seq"] != float64(1) {
			t.Fatalf("saved post: %d %s", answer.Status, answer.Payload)
		}
	}
	answer, p := f.ask(t, 12, map[string]any{
		"type": "session-boards.read", "session": cloudops.MachineReplySession,
		"request": "board-after-post", "board": result.ID,
	})
	if !answer.OK() {
		t.Fatalf("read: %d %s", answer.Status, answer.Payload)
	}
	posts, ok := bodyOf(t, p)["posts"].([]any)
	if !ok || len(posts) != 1 {
		t.Fatalf("duplicated or missing comments: %v", p)
	}
	f.bridge.AllowCommands = func() bool { return false }
	post["request"] = "write-gate-off"
	answer, _ = f.ask(t, 13, post)
	if answer.OK() {
		t.Fatal("disabled Cloud write admitted a post")
	}
}
