package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestAPersonCompletesAnItemByHandOverTheRoute(t *testing.T) {
	s, p, v := workV2AssignmentServer(t, session.StateWorking)
	owned, err := s.workV2().Assign(context.Background(), v.Item.ID, app.AssignWorkV2{ExpectedVersion: v.Item.Version,
		Mode: "existing_session", SessionID: p.s.ConversationID, TerminalID: p.s.ID, Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := "/v1/work/v2/items/" + v.Item.ID + "/complete"

	stale := httptest.NewRecorder()
	s.workV2Route(stale, personWorkV2Request(http.MethodPost, path,
		fmt.Sprintf(`{"expected_version":%d}`, v.Item.Version), "complete-stale"))
	if stale.Code != http.StatusConflict || codeOf(t, stale) != "version_conflict" {
		t.Fatalf("stale completion: %d %s", stale.Code, stale.Body)
	}

	body := fmt.Sprintf(`{"expected_version":%d,"note":"Done outside the Session."}`, owned.Item.Version)
	rec := httptest.NewRecorder()
	s.workV2Route(rec, personWorkV2Request(http.MethodPost, path, body, "complete-by-hand"))
	if rec.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", rec.Code, rec.Body)
	}
	var answer struct {
		Item workV2ItemWire `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Item.Phase != "done" || answer.Item.OwnerSession != nil || answer.Item.ClosedAt == nil {
		t.Fatalf("completed item = %+v", answer.Item)
	}

	replay := httptest.NewRecorder()
	s.workV2Route(replay, personWorkV2Request(http.MethodPost, path, body, "complete-by-hand"))
	if replay.Code != http.StatusOK || replay.Header().Get("Idempotent-Replayed") != "true" ||
		replay.Body.String() != rec.Body.String() {
		t.Fatalf("replay: %d %s, headers=%v", replay.Code, replay.Body, replay.Header())
	}

	again := httptest.NewRecorder()
	s.workV2Route(again, personWorkV2Request(http.MethodPost, path,
		fmt.Sprintf(`{"expected_version":%d}`, answer.Item.Version), "complete-twice"))
	if again.Code != http.StatusConflict || codeOf(t, again) != "item_terminal" {
		t.Fatalf("second completion: %d %s", again.Code, again.Body)
	}

	current, err := s.workV2().Item(context.Background(), v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	completed := 0
	for _, event := range current.Events {
		if event.Kind == "item.completed" {
			completed++
			if event.Actor != "device:phone" {
				t.Fatalf("completion actor = %q", event.Actor)
			}
		}
	}
	if completed != 1 {
		t.Fatalf("completion events = %+v", current.Events)
	}
	if got := p.done(); len(got) != 0 {
		t.Fatalf("a completion typed into the Session: %v", got)
	}
}
