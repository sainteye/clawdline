package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/domain/auth"
)

// The board's routes: a person writes as themselves, a session only relays a
// person under a run, every write carries a key and is answered once, and a
// body that tries to set what only facts decide is refused by name.
func TestTheBoardRoutesAnswerOnceAndOnlyToWhoMayWrite(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{store: st}
	person := access{verdict: auth.Verdict{Allowed: true, Local: true}}
	machine := access{machine: true}
	do := func(a access, method, target, key, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, a))
		rec := httptest.NewRecorder()
		s.workRoute(rec, req)
		return rec
	}
	code := func(rec *httptest.ResponseRecorder) string {
		var body struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return body.Error
	}

	create := `{"title":"ship it","project":"/p","place":"backlog"}`
	if rec := do(person, http.MethodPost, "/v1/work/items", "", create); rec.Code != 400 || code(rec) != "idempotency_key_required" {
		t.Fatalf("no key: %d %s", rec.Code, rec.Body)
	}
	if rec := do(machine, http.MethodPost, "/v1/work/items", "m1", create); rec.Code != 403 || code(rec) != "session_cannot_decide" {
		t.Fatalf("a session on its own: %d %s", rec.Code, rec.Body)
	}
	rec := do(person, http.MethodPost, "/v1/work/items", "c1", create)
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var made struct {
		Item struct {
			ID      string `json:"id"`
			Place   string `json:"place"`
			Version int64  `json:"version"`
		} `json:"item"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &made)
	if made.Item.Place != "backlog" || len(made.Item.ID) != 36 {
		t.Fatalf("made %s", rec.Body)
	}
	// The same request again is the same answer, and one item.
	again := do(person, http.MethodPost, "/v1/work/items", "c1", create)
	if again.Code != 201 || again.Body.String() != rec.Body.String() || again.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay: %d %s", again.Code, again.Body)
	}
	if n, _ := st.WorkOpenCount(context.Background()); n != 1 {
		t.Fatalf("open %d after a replay", n)
	}
	if rec := do(person, http.MethodPost, "/v1/work/items", "c1", `{"title":"other","project":"/p","place":"backlog"}`); rec.Code != 409 || code(rec) != "idempotency_key_reused" {
		t.Fatalf("a reused key: %d %s", rec.Code, rec.Body)
	}

	item := "/v1/work/items/" + made.Item.ID
	// D31: the body cannot set a state, and no command lands anything.
	if rec := do(person, http.MethodPost, item, "x1", `{"op":"accept","state":"done","closed_reason":"landed"}`); rec.Code != 400 || code(rec) != "invalid_command" {
		t.Fatalf("a state in the body: %d %s", rec.Code, rec.Body)
	}
	if rec := do(person, http.MethodPost, item, "x2", `{"op":"mark_landed"}`); rec.Code != 422 || code(rec) != "landing_is_broker_fact" {
		t.Fatalf("mark_landed: %d %s", rec.Code, rec.Body)
	}
	// A session relays a person's words under the run that carried them.
	rec = do(machine, http.MethodPost, item, "r1", `{"op":"start","owner":"root-conv","via":{"run":"run-42"}}`)
	if rec.Code != 200 {
		t.Fatalf("relayed start: %d %s", rec.Code, rec.Body)
	}
	moves := do(person, http.MethodGet, item+"/moves", "", "")
	var list struct {
		Moves []struct {
			Actor    string          `json:"actor"`
			Trigger  string          `json:"trigger"`
			Evidence json.RawMessage `json:"evidence"`
		} `json:"moves"`
	}
	_ = json.Unmarshal(moves.Body.Bytes(), &list)
	if len(list.Moves) != 2 || list.Moves[1].Actor != "user_via_session:run-42" || list.Moves[1].Trigger != "start" ||
		!strings.Contains(string(list.Moves[1].Evidence), `"principal":"machine"`) {
		t.Fatalf("moves %s", moves.Body)
	}
	// A refused command gives its key back: the same key is decided afresh.
	if rec := do(person, http.MethodPost, item, "a1", `{"op":"accept"}`); rec.Code != 409 || code(rec) != "not_awaiting_closure" {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body)
	}
	if rec := do(person, http.MethodPost, item, "a1", `{"op":"accept"}`); code(rec) != "not_awaiting_closure" {
		t.Fatalf("the refused key was kept: %d %s", rec.Code, rec.Body)
	}
	board := do(person, http.MethodGet, "/v1/work/board?project=/p", "", "")
	if board.Code != 200 || !strings.Contains(board.Body.String(), `"active":1`) || !strings.Contains(board.Body.String(), `"sweep"`) {
		t.Fatalf("board: %d %s", board.Code, board.Body)
	}
	if rec := do(person, http.MethodGet, "/v1/work/board?limit=5", "", ""); rec.Code != 400 {
		t.Fatalf("a selector the route does not take: %d", rec.Code)
	}
	if rec := do(person, http.MethodGet, "/v1/work/items/not-an-id", "", ""); rec.Code != 404 {
		t.Fatalf("a malformed id: %d", rec.Code)
	}
}
