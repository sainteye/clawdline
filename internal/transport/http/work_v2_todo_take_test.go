package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// With session_todo_auto_take off, which is how every machine starts, a
// Session's to-do read says so and a take is refused with nothing changed:
// the behaviour before the setting existed. Turned on, the Session's take is
// recorded on the row and shown on its next read; nothing is typed.
func TestASessionTakesAPersonsTodoOnlyWhenThePersonTurnedItOn(t *testing.T) {
	s, p := ownTodoServer(t)
	s.cfg.Dir = filepath.Join(t.TempDir(), "state")
	conversation := p.s.ConversationID
	rec := httptest.NewRecorder()
	s.workV2Route(rec, personWorkV2Request(http.MethodPost, "/v1/work/v2/session-todos/pane-4", `{"text":"Tidy the README"}`, "take-create"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var created struct {
		Todo directTodoV2Wire `json:"todo"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	read := func() map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		s.workV2Route(rec, agentWorkV2Request(http.MethodGet, "/v1/work/v2/agent/session-todos/"+conversation, "", ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("read: %d %s", rec.Code, rec.Body)
		}
		var answer map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
			t.Fatal(err)
		}
		return answer
	}
	take := func(key string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.workV2Route(rec, agentWorkV2Request(http.MethodPost,
			"/v1/work/v2/agent/session-todos/"+conversation+"/"+created.Todo.ID+"/take", `{}`, key))
		return rec
	}

	if answer := read(); answer["auto_take"] != false || answer["auto_take_unknown"] != nil {
		t.Fatalf("auto_take with the setting never written = %v", answer)
	}
	if off := take("take-off"); off.Code != http.StatusConflict || !strings.Contains(off.Body.String(), "todo_auto_take_off") {
		t.Fatalf("take with the setting off: %d %s", off.Code, off.Body)
	}
	if strings.Contains(rec.Body.String(), "taken_at") {
		t.Fatalf("an untaken row carries taken_at: %s", rec.Body)
	}

	if _, err := s.settingsFile().Set(map[string]any{"session_todo_auto_take": true}); err != nil {
		t.Fatal(err)
	}
	if got := read()["auto_take"]; got != true {
		t.Fatalf("auto_take with the setting on = %v", got)
	}
	on := take("take-on")
	if on.Code != http.StatusOK || !strings.Contains(on.Body.String(), `"taken_at"`) {
		t.Fatalf("take with the setting on: %d %s", on.Code, on.Body)
	}
	if replay := take("take-on"); replay.Code != on.Code || replay.Body.String() != on.Body.String() {
		t.Fatalf("take replay: %d %s", replay.Code, replay.Body)
	}
	todos, _ := read()["direct_todos"].([]any)
	if len(todos) != 1 || todos[0].(map[string]any)["taken_at"] == nil {
		t.Fatalf("taken row on the next read: %v", todos)
	}
	if len(p.acts) != 0 {
		t.Fatalf("a take typed into the terminal: %v", p.acts)
	}
}
