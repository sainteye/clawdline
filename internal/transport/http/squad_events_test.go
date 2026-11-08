package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestSquadEventHTTPRequiresBoundLocalActorAndReadsCursor(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	launch, err := st.PrepareSquadLaunch(ctx, json.RawMessage(`{"definition_id":"clawdline.persona.architect","scope_id":"project-test","skills":[{"id":"skill.a","version":"1","enabled":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSquadTerminal(ctx, launch.ID, "terminal-a"); err != nil {
		t.Fatal(err)
	}
	if err := st.BindSquadConversation(ctx, launch.ID, "conversation-a"); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st}
	snapshot := httptest.NewRecorder()
	s.squadSessionSnapshotRead(snapshot, httptest.NewRequest(http.MethodGet, "/v1/squad/session-snapshots/conversation-a", nil))
	if snapshot.Code != http.StatusOK || !strings.Contains(snapshot.Body.String(), `"snapshot_id":"`+launch.SnapshotID+`"`) ||
		!strings.Contains(snapshot.Body.String(), `"id":"skill.a"`) {
		t.Fatalf("bound snapshot status/body = %d %s", snapshot.Code, snapshot.Body.String())
	}
	missing := httptest.NewRecorder()
	s.squadSessionSnapshotRead(missing, httptest.NewRequest(http.MethodGet, "/v1/squad/session-snapshots/legacy-conversation", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("legacy snapshot status = %d", missing.Code)
	}
	request := func(local bool, capability, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/squad/session-events", strings.NewReader(body))
		r.Header.Set(squadSessionCapabilityHeader, capability)
		r = r.WithContext(context.WithValue(r.Context(), accessKey{}, access{
			verdict: auth.Verdict{Allowed: true, Local: local},
		}))
		w := httptest.NewRecorder()
		s.squadSessionEvents(w, r)
		return w
	}
	body := `{"skill_id":"skill.a","skill_version":"1","client_event_id":"event-1","status":"applied"}`
	if got := request(false, launch.ActorCapability, body).Code; got != http.StatusForbidden {
		t.Fatalf("remote actor status = %d", got)
	}
	if got := request(true, "", body).Code; got != http.StatusForbidden {
		t.Fatalf("missing actor status = %d", got)
	}
	first := request(true, launch.ActorCapability, body)
	if first.Code != http.StatusOK {
		t.Fatalf("bound actor status = %d: %s", first.Code, first.Body.String())
	}
	var receipt store.SquadSkillReceipt
	if err := json.Unmarshal(first.Body.Bytes(), &receipt); err != nil || receipt.Seq != 1 {
		t.Fatalf("receipt = %+v, %v", receipt, err)
	}
	if got := request(true, launch.ActorCapability, body).Code; got != http.StatusOK {
		t.Fatalf("same event retry status = %d", got)
	}
	changed := strings.Replace(body, `"applied"`, `"read"`, 1)
	if got := request(true, launch.ActorCapability, changed).Code; got != http.StatusConflict {
		t.Fatalf("changed event retry status = %d", got)
	}
	head := httptest.NewRecorder()
	s.squadEventHead(head, httptest.NewRequest(http.MethodGet, "/v1/squad/events/head", nil))
	if head.Code != http.StatusOK || !strings.Contains(head.Body.String(), `"seq":1`) {
		t.Fatalf("head status/body = %d %s", head.Code, head.Body.String())
	}
	page := httptest.NewRecorder()
	s.squadEvents(page, httptest.NewRequest(http.MethodGet, "/v1/squad/events?after=0&limit=1", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `"next_after":1`) ||
		!strings.Contains(page.Body.String(), `"status":"applied"`) {
		t.Fatalf("page status/body = %d %s", page.Code, page.Body.String())
	}
	bad := httptest.NewRecorder()
	s.squadEvents(bad, httptest.NewRequest(http.MethodGet, "/v1/squad/events?after=-1", nil))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor status = %d", bad.Code)
	}
	s.readings = app.NewInventoryReading(func(context.Context) session.Inventory {
		return session.Inventory{Complete: true, Sessions: []session.Session{
			{ID: "terminal-a", Assistant: session.AssistantCodex, ConversationID: "conversation-a"},
			{ID: "terminal-old", Assistant: session.AssistantClaude, ConversationID: "conversation-old"},
			{ID: "terminal-unknown", Assistant: session.AssistantCodex},
		}}
	}, 0)
	bindings := httptest.NewRecorder()
	s.squadSessionBindings(bindings, httptest.NewRequest(http.MethodGet, "/v1/squad/session-bindings", nil))
	if bindings.Code != http.StatusOK || !strings.Contains(bindings.Body.String(), `"state":"bound"`) ||
		!strings.Contains(bindings.Body.String(), `"definition_id":"clawdline.persona.architect"`) ||
		!strings.Contains(bindings.Body.String(), `"state":"legacy_unsnapshotted"`) ||
		!strings.Contains(bindings.Body.String(), `"state":"identity_unavailable"`) {
		t.Fatalf("session bindings status/body = %d %s", bindings.Code, bindings.Body.String())
	}
}

func TestSquadEventsHTTPFiltersByConversationWithinTheBound(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	capabilities := map[string]string{}
	for _, name := range []string{"a", "b", "noise"} {
		launch, err := st.PrepareSquadLaunch(ctx, json.RawMessage(`{"definition_id":"clawdline.persona.architect","scope_id":"project-test","skills":[{"id":"skill.a","version":"1","enabled":true}]}`))
		if err != nil {
			t.Fatal(err)
		}
		if err := st.RecordSquadTerminal(ctx, launch.ID, "terminal-"+name); err != nil {
			t.Fatal(err)
		}
		if err := st.BindSquadConversation(ctx, launch.ID, "conversation-"+name); err != nil {
			t.Fatal(err)
		}
		capabilities[name] = launch.ActorCapability
	}
	for i, name := range []string{"noise", "a", "noise", "b", "noise", "a"} {
		event := store.SquadSkillEvent{SkillID: "skill.a", SkillVersion: "1", ClientEventID: "event-" + string(rune('0'+i)), Status: "read"}
		if _, err := st.RecordSquadSkillEvent(ctx, capabilities[name], event); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{store: st}
	read := func(query string) (int, string, []store.SquadSkillReceipt) {
		w := httptest.NewRecorder()
		s.squadEvents(w, httptest.NewRequest(http.MethodGet, "/v1/squad/events?"+query, nil))
		var page struct {
			Events []store.SquadSkillReceipt `json:"events"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &page)
		return w.Code, w.Body.String(), page.Events
	}
	conversations := func(events []store.SquadSkillReceipt) string {
		var out []string
		for _, e := range events {
			out = append(out, strings.TrimPrefix(e.ConversationID, "conversation-"))
		}
		return strings.Join(out, ",")
	}
	for _, query := range []string{"conversation=conversation-a,conversation-b", "conversation=conversation-a&conversation=conversation-b", "conversation=conversation-b,conversation-a,conversation-a"} {
		code, body, events := read(query + "&after=0&limit=50")
		if code != http.StatusOK || conversations(events) != "a,b,a" {
			t.Fatalf("%s = %d %s", query, code, body)
		}
	}
	if code, body, events := read("conversation=conversation-a&after=2"); code != http.StatusOK || conversations(events) != "a" || !strings.Contains(body, `"next_after":6`) {
		t.Fatalf("filtered after cursor = %d %s", code, body)
	}
	if code, body, events := read("after=0"); code != http.StatusOK || len(events) != 6 {
		t.Fatalf("unfiltered = %d %s", code, body)
	}
	many := make([]string, store.MaxSquadEventConversations+1)
	for i := range many {
		many[i] = "c" + string(rune('A'+i%26)) + string(rune('A'+i/26))
	}
	if code, body, _ := read("conversation=" + strings.Join(many[:store.MaxSquadEventConversations], ",")); code != http.StatusOK {
		t.Fatalf("at the bound = %d %s", code, body)
	}
	for _, bad := range []string{
		"conversation=" + strings.Join(many, ","),
		"conversation=a,,b",
		"conversation=",
		"conversation=" + strings.Repeat("x", store.MaxSquadEventIDBytes+1),
		"conversation=a%20b",
	} {
		if code, body, _ := read(bad); code != http.StatusBadRequest || !strings.Contains(body, `"bad_request"`) {
			t.Fatalf("%s = %d %s", bad, code, body)
		}
	}
}

// The role rows on screen read their launch snapshots in one request: only
// what a row draws, enabled Skills only, never their content, and an older
// Session with no snapshot is simply absent.
func TestSquadSessionSnapshotsAnswerSeveralRowsInOneRead(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for i, conversation := range []string{"conversation-a", "conversation-b"} {
		launch, err := st.PrepareSquadLaunch(ctx, json.RawMessage(`{"definition_id":"clawdline.persona.architect","scope_id":"project-test","skills":[`+
			`{"id":"skill.a","version":"1","enabled":true,"name":{"en":"Review","zh-Hant":"審查"},"content":"Use this skill when reviewing."},`+
			`{"id":"skill.off","version":"1","enabled":false}],"handbook":{"text":"`+conversation+`"}}`))
		if err != nil {
			t.Fatal(err)
		}
		if err := st.RecordSquadTerminal(ctx, launch.ID, "terminal-"+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
		if err := st.BindSquadConversation(ctx, launch.ID, conversation); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{store: st}
	read := func(query string) (int, string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/v1/squad/session-snapshots?"+query, nil)
		s.squadRoute(w, r)
		return w.Code, w.Body.String()
	}
	code, body := read("conversation=conversation-a,legacy-conversation&conversation=conversation-b")
	var reply struct {
		Snapshots []squadRowSnapshot `json:"snapshots"`
	}
	if err := json.Unmarshal([]byte(body), &reply); code != http.StatusOK || err != nil {
		t.Fatalf("batch = %d %s", code, body)
	}
	if len(reply.Snapshots) != 2 || reply.Snapshots[0].ConversationID != "conversation-a" || reply.Snapshots[1].ConversationID != "conversation-b" {
		t.Fatalf("batch rows = %s", body)
	}
	for _, row := range reply.Snapshots {
		if row.SnapshotID == "" || row.DefinitionID != "clawdline.persona.architect" || row.ScopeID != "project-test" ||
			len(row.Skills) != 1 || row.Skills[0].ID != "skill.a" || row.Skills[0].Name.ZhHant != "審查" {
			t.Fatalf("row = %+v", row)
		}
	}
	if strings.Contains(body, "Use this skill") || strings.Contains(body, "skill.off") {
		t.Fatalf("a row read carries content or a disabled Skill: %s", body)
	}
	many := make([]string, store.MaxSquadSnapshotConversations+1)
	for i := range many {
		many[i] = "c" + string(rune('A'+i%26)) + string(rune('A'+i/26))
	}
	if code, body := read("conversation=" + strings.Join(many[:store.MaxSquadSnapshotConversations], ",")); code != http.StatusOK {
		t.Fatalf("at the bound = %d %s", code, body)
	}
	for _, bad := range []string{"", "conversation=", "conversation=a,,b", "conversation=a%20b", "conversation=" + strings.Join(many, ",")} {
		if code, body := read(bad); code != http.StatusBadRequest || !strings.Contains(body, `"bad_request"`) {
			t.Fatalf("%q = %d %s", bad, code, body)
		}
	}
}
