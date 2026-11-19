package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/contract"

	sb "github.com/sainteye/clawdline/internal/domain/sessionboard"
)

func TestSessionBoardRefusesTheNextConversationOnTheSameTerminal(t *testing.T) {
	s, pane, _ := sessionItemServer(t)
	project := projects.ProjectID(pane.s.CWD)
	pane.s.PID = os.Getpid()
	old := pane.s.ConversationID
	if _, _, ok := s.sessionBoardActor(t.Context(), s.freshReading(t.Context()), old, false); !ok {
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

func TestPersonCanAddLiveSessionsToBoardWithoutActingAsThem(t *testing.T) {
	s, pane, _ := sessionItemServer(t)
	pane.s.PID = os.Getpid()
	lead := string(pane.s.Assistant) + ":" + pane.s.ConversationID
	request := func(req *http.Request, status int) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		s.sessionBoardsRoute(rec, req)
		if rec.Code != status {
			t.Fatalf("got %d, want %d: %s", rec.Code, status, rec.Body)
		}
		return rec
	}
	command := func(value sb.Command) string {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	create := sb.Command{Action: "create", Name: "Acceptance", Text: "Compare one build", Target: lead, Members: []string{lead}}
	rec := request(personWorkV2Request(http.MethodPost, sessionBoardRoute, command(create), "create-voice-board"), http.StatusOK)
	var result sb.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || result.ID == "" {
		t.Fatalf("create receipt: %s (%v)", rec.Body, err)
	}
	pane.s.ConversationID = "20000000-0000-4000-8000-000000000005"
	member := string(pane.s.Assistant) + ":" + pane.s.ConversationID
	add := sb.Command{Action: "add-members", Board: result.ID, Members: []string{member}}
	request(agentWorkV2Request(http.MethodPost, sessionBoardRoute+"?conversation_id="+url.QueryEscape(pane.s.ConversationID),
		command(add), ""), http.StatusForbidden)
	add.Members = append(add.Members, "codex:unavailable")
	request(personWorkV2Request(http.MethodPost, sessionBoardRoute, command(add), ""), http.StatusForbidden)
	request(agentWorkV2Request(http.MethodGet, sessionBoardRoute+"?conversation_id="+url.QueryEscape(pane.s.ConversationID)+"&board="+result.ID,
		"", ""), http.StatusForbidden)
	add.Members = add.Members[:1]
	for range 2 {
		request(personWorkV2Request(http.MethodPost, sessionBoardRoute, command(add), ""), http.StatusOK)
	}
	rec = request(agentWorkV2Request(http.MethodGet, sessionBoardRoute+"?conversation_id="+url.QueryEscape(pane.s.ConversationID)+"&board="+result.ID,
		"", ""), http.StatusOK)
	var page sb.Page
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || len(page.Board.Members) != 2 || page.Board.Lead != lead {
		t.Fatalf("member page: %s (%v)", rec.Body, err)
	}
}

func TestSessionBoardRosterDistinguishesMembershipFromTheSessionList(t *testing.T) {
	s, pane, _ := sessionItemServer(t)
	ctx := t.Context()
	project := projects.ProjectID(pane.s.CWD)
	lead := sb.Actor{ID: "claude:present", Label: "Lead"}
	away := sb.Actor{ID: "claude:away", Label: "Away"}
	stale := sb.Actor{ID: "claude:stale", Label: "Stale"}
	left := sb.Actor{ID: "claude:left", Label: "Left"}
	change := func(actor sb.Actor, cmd sb.Command) sb.Result {
		t.Helper()
		result, err := s.store.ChangeSessionBoard(ctx, project, actor, sb.Actor{}, cmd, "setup", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	board := change(lead, sb.Command{Action: "create", Name: "Review", Text: "Coordinate review"}).ID
	for _, member := range []sb.Actor{away, stale, left} {
		change(member, sb.Command{Action: "join", Board: board})
	}
	change(left, sb.Command{Action: "leave", Board: board})

	var snapshot = sessionsSnapshotWire{Sessions: []sessionRowWire{
		{SessionRow: contract.SessionRow{Assistant: "claude", SessionID: "present", Source: &contract.BearingsSource{Freshness: contract.SourceFreshnessCurrent}}},
		{SessionRow: contract.SessionRow{Assistant: "claude", SessionID: "stale", Source: &contract.BearingsSource{Freshness: contract.SourceFreshnessStale}}},
		{SessionRow: contract.SessionRow{Assistant: "claude", SessionID: "left", Source: &contract.BearingsSource{Freshness: contract.SourceFreshnessCurrent}}},
	}}
	list := &s.lists().sessions
	list.build = func(context.Context) (sessionsSnapshotWire, error) { return snapshot, nil }
	type memberWire struct {
		ID       string `json:"id"`
		Presence string `json:"presence"`
	}
	type boardWire struct {
		Members []memberWire `json:"members"`
	}
	check := func(query string, wantAway string) {
		t.Helper()
		rec := httptest.NewRecorder()
		s.sessionBoardsRoute(rec, personWorkV2Request(http.MethodGet, sessionBoardRoute+query, "", ""))
		var result struct {
			Board  boardWire   `json:"board"`
			Boards []boardWire `json:"boards"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &result); rec.Code != http.StatusOK || err != nil {
			t.Fatalf("roster status %d: %s (%v)", rec.Code, rec.Body, err)
		}
		members := result.Board.Members
		if len(result.Boards) == 1 {
			members = result.Boards[0].Members
		}
		got := map[string]string{}
		for _, member := range members {
			got[member.ID] = member.Presence
		}
		if len(got) != 3 || got[lead.ID] != "listed" || got[away.ID] != wantAway || got[stale.ID] != "" {
			t.Fatalf("roster from %s: %+v", query, got)
		}
		if _, found := got[left.ID]; found {
			t.Fatal("a Session that left the board is still enrolled")
		}
	}
	check("", "")
	snapshot.Scan.Sources = []contract.ScanSource{{Source: "tmux", Complete: true}, {Source: "iterm", Disabled: "setting"}}
	list.mu.Lock()
	list.at = time.Time{}
	list.mu.Unlock()
	check("?board="+board, "not_listed")
	snapshot.Sessions = append(snapshot.Sessions, sessionRowWire{SessionRow: contract.SessionRow{
		Assistant: "claude", Identity: contract.IdentityBindingNoRecord,
		Source: &contract.BearingsSource{Freshness: contract.SourceFreshnessCurrent},
	}})
	list.mu.Lock()
	list.at = time.Time{}
	list.mu.Unlock()
	check("?board="+board, "not_listed")
	snapshot.Sessions[len(snapshot.Sessions)-1].Identity = contract.IdentityBindingUnreadable
	list.mu.Lock()
	list.at = time.Time{}
	list.mu.Unlock()
	check("?board="+board, "")
}
