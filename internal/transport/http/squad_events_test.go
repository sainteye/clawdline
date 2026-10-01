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
