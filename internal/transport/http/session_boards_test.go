package http

import (
	"context"
	"encoding/json"
	"fmt"
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
	inboxQuery := sessionBoardInboxRoute + "?board=" + created.ID + "&conversation_id=" + url.QueryEscape(old)
	readInbox := func(path string, status int) {
		t.Helper()
		got := httptest.NewRecorder()
		s.sessionBoardInbox(got, agentWorkV2Request(http.MethodGet, path, "", ""))
		if got.Code != status {
			t.Fatalf("native inbox status = %d, want %d: %s", got.Code, status, got.Body)
		}
	}
	readInbox(inboxQuery, http.StatusOK)
	readInbox(inboxQuery+"&member=codex:other", http.StatusForbidden)
	readInbox(inboxQuery+"&after=1&before=2", http.StatusBadRequest)
	pane.s.ConversationID = "20000000-0000-4000-8000-000000000004"
	readInbox(inboxQuery, http.StatusForbidden)
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
	for seq := 2; seq <= sb.MaxPosts+1; seq++ {
		if _, err := f.server.store.ChangeSessionBoard(t.Context(), f.place, lead, sb.Actor{},
			sb.Command{Action: "post", Board: result.ID, Text: "Earlier discussion"}, fmt.Sprintf("earlier-%d", seq), now); err != nil {
			t.Fatalf("seed post %d: %v", seq, err)
		}
	}
	original, err := f.server.store.ChangeSessionBoard(t.Context(), f.place, lead, sb.Actor{},
		sb.Command{Action: "post", Board: result.ID, Text: "From the Session"}, "lead-post", now)
	if err != nil {
		t.Fatal(err)
	}
	post["request"] = "board-reply"
	post["command"] = map[string]any{"action": "post", "board": result.ID, "text": "Reply from the browser", "reply_to": original.Seq}
	answer, p = f.ask(t, 13, post)
	if !answer.OK() || bodyOf(t, p)["seq"] != float64(original.Seq+1) {
		t.Fatalf("reply: %d %s", answer.Status, answer.Payload)
	}
	f.bridge.AllowCommands = func() bool { return false }
	answer, p = f.ask(t, 14, map[string]any{
		"type": "session-boards.inbox", "session": cloudops.MachineReplySession, "request": "inbox",
		"board": result.ID, "member": lead.ID, "before": original.Seq + 2,
	})
	if !answer.OK() {
		t.Fatalf("read-only inbox: %d %s", answer.Status, answer.Payload)
	}
	replies, ok := bodyOf(t, p)["posts"].([]any)
	if !ok || len(replies) != 1 || replies[0].(map[string]any)["reply_to"] != float64(original.Seq) {
		t.Fatalf("wrong inbox: %v", p)
	}
	post["request"] = "write-gate-off"
	answer, _ = f.ask(t, 15, post)
	if answer.OK() {
		t.Fatal("disabled Cloud write admitted a post")
	}
	answer, p = f.ask(t, 16, map[string]any{
		"type": "session-boards.read", "session": cloudops.MachineReplySession,
		"request": "board-tail", "board": result.ID, "after": original.Seq,
	})
	if !answer.OK() {
		t.Fatalf("read after #%d: %d %s", original.Seq, answer.Status, answer.Payload)
	}
	posts, ok = bodyOf(t, p)["posts"].([]any)
	if !ok || len(posts) != 1 || posts[0].(map[string]any)["seq"] != float64(original.Seq+1) {
		t.Fatalf("wrong board tail: %v", p)
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

func TestSessionBoardArchivesOnlyAfterTwoCompleteReadingsWithoutListedMembers(t *testing.T) {
	s, pane, _ := sessionItemServer(t)
	ctx := t.Context()
	project := projects.ProjectID(pane.s.CWD)
	lead := sb.Actor{ID: "claude:lead", Label: "Lead"}
	now := time.Now()
	created, err := s.store.ChangeSessionBoard(ctx, project, lead, sb.Actor{},
		sb.Command{Action: "create", Name: "Review", Text: "Historical work"}, "setup", now)
	if err != nil {
		t.Fatal(err)
	}
	var previous map[string]sb.Board
	check := func(wire sessionsSnapshotWire, wantArchived bool) {
		t.Helper()
		boards, err := s.store.SessionBoards(ctx, "", "")
		if err != nil {
			t.Fatal(err)
		}
		previous, err = s.archiveInactiveSessionBoards(ctx, previous, boards, wire, now)
		if err != nil {
			t.Fatal(err)
		}
		page, err := s.store.SessionBoardPage(ctx, "", "", created.ID, 0, 0)
		if err != nil || (page.Board.ArchivedAt != 0) != wantArchived {
			t.Fatalf("archived_at = %d, want archived %t, error %v", page.Board.ArchivedAt, wantArchived, err)
		}
	}
	empty := sessionsSnapshotWire{Scan: contract.Scan{EmptyAuthoritative: true, Complete: true}}
	present := sessionsSnapshotWire{Scan: empty.Scan, Sessions: []sessionRowWire{{SessionRow: contract.SessionRow{
		SessionID: "lead", Assistant: "claude", State: "idle",
		Source: &contract.BearingsSource{Freshness: contract.SourceFreshnessCurrent},
	}}}}
	check(present, false)
	check(present, false)
	check(empty, false)
	check(sessionsSnapshotWire{}, false)
	check(empty, false)
	check(sessionsSnapshotWire{Scan: contract.Scan{Sources: []contract.ScanSource{
		{Source: "tmux", Complete: true}, {Source: "iterm", Disabled: "setting"},
	}}}, false)
	check(empty, false)
	unknown := present
	unknown.Sessions[0].SessionID = ""
	unknown.Sessions[0].Identity = contract.IdentityBindingUnreadable
	check(unknown, false)
	check(empty, false)
	check(empty, true)
}

func TestSessionBoardNamesReuseThePublishedListForEveryPost(t *testing.T) {
	s := &Server{}
	const id = "known-conversation"
	rows := []sessionRowWire{{SessionRow: contract.SessionRow{
		ID: "first", Assistant: "codex", SessionID: id, Label: "Current title",
		Source: &contract.BearingsSource{Freshness: contract.SourceFreshnessCurrent},
	}}}
	lists := s.lists()
	lists.sessions.build = func(context.Context) (sessionsSnapshotWire, error) {
		return sessionsSnapshotWire{Sessions: rows}, nil
	}
	if _, err := lists.sessions.refresh(); err != nil {
		t.Fatal(err)
	}
	actor := sb.Actor{ID: "codex:" + id, Label: "Saved title"}
	boards := []sb.Board{{Members: []sb.Actor{actor}}}
	posts := make([]sb.Post, sb.MaxPosts)
	for i := range posts {
		posts[i].Author = actor
	}
	s.projectSessionBoardNames(false, boards, posts)
	if got := boards[0].Members[0].Label; got != "Current title" {
		t.Fatalf("roster title: %q", got)
	}
	for _, post := range posts {
		if post.Author.Label != "Current title" {
			t.Fatalf("author title: %q", post.Author.Label)
		}
	}
	if builds := lists.sessions.builds.Load(); builds != 1 {
		t.Fatalf("projecting %d posts rebuilt the Session list: %d builds", len(posts), builds)
	}
	second := rows[0]
	second.ID, second.Label = "second", "Duplicate title"
	rows = append(rows, second)
	if _, err := lists.sessions.refresh(); err != nil {
		t.Fatal(err)
	}
	boards[0].Members[0], posts[0].Author = actor, actor
	s.projectSessionBoardNames(false, boards, posts[:1])
	if boards[0].Members[0] != actor || posts[0].Author != actor {
		t.Fatal("ambiguous identity replaced a saved title")
	}
}
