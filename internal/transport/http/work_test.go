package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/app"
	"github.com/sainteye/clawdline-go/internal/domain/auth"
	"github.com/sainteye/clawdline-go/internal/domain/session"
	"github.com/sainteye/clawdline-go/internal/domain/work"
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
	// A session relays a person's words only under a run this daemon issued
	// when the person sent their message: an invented one is refused by name,
	// as it was accepted before runs had an issuer, and nothing is written.
	if rec := do(machine, http.MethodPost, item, "r0", `{"op":"start","owner":"root-conv","via":{"run":"run-42"}}`); rec.Code != 403 || code(rec) != "run_unknown" {
		t.Fatalf("an invented run: %d %s", rec.Code, rec.Body)
	}
	run, err := s.runs().Issue(context.Background(), session.Session{ID: "%4", ConversationID: "root-conv"}, "local")
	if err != nil {
		t.Fatal(err)
	}
	rec = do(machine, http.MethodPost, item, "r1", `{"op":"start","owner":"root-conv","via":{"run":"`+run.ID+`"}}`)
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
	if len(list.Moves) != 2 || list.Moves[1].Actor != "user_via_session:"+run.ID || list.Moves[1].Trigger != "start" ||
		!strings.Contains(string(list.Moves[1].Evidence), `"principal":"machine"`) ||
		!strings.Contains(string(list.Moves[1].Evidence), `"session":"root-conv"`) {
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

// A relay's run is checked inside the write, after its receipt is claimed: a
// retry of a relay that was answered is its stored answer even once the run
// is too old to carry a new one, and a new request under that run is refused.
func TestARelayReplaysAfterItsRunExpires(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{store: st}
	at := time.Now()
	runsByServer.Store(s, &app.Runs{Store: st, Now: func() time.Time { return at }})
	machine := access{machine: true}
	do := func(key, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/work/items", strings.NewReader(body))
		req.Header.Set("Idempotency-Key", key)
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, machine))
		rec := httptest.NewRecorder()
		s.workRoute(rec, req)
		return rec
	}
	run, err := s.runs().Issue(context.Background(), session.Session{ID: "%4", ConversationID: "root-conv"}, "local")
	if err != nil {
		t.Fatal(err)
	}
	body := `{"title":"ship it","project":"/p","place":"backlog","via":{"run":"` + run.ID + `"}}`
	first := do("k1", body)
	if first.Code != 201 {
		t.Fatalf("relayed create: %d %s", first.Code, first.Body)
	}
	at = at.Add(work.RelayWindow + time.Minute)
	again := do("k1", body)
	if again.Code != 201 || again.Body.String() != first.Body.String() || again.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("the retry: %d %s", again.Code, again.Body)
	}
	if rec := do("k2", body); rec.Code != 403 || !strings.Contains(rec.Body.String(), `"run_expired"`) {
		t.Fatalf("a new request under an old run: %d %s", rec.Code, rec.Body)
	}
}

// BD-17 on the route: the command a person gives work whose delivery named
// no item. It carries their words, it is refused without them, and a session
// may give it only as a relay of what a person said — this is the one
// closure with no fact behind it but a person's word, so whose word it was
// has to be on the record.
func TestDoneElsewhereCarriesThePersonsWords(t *testing.T) {
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
	rec := do(person, http.MethodPost, "/v1/work/items", "c1",
		`{"title":"the URL carries no session","project":"/p","place":"backlog"}`)
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var made struct {
		Item struct {
			ID string `json:"id"`
		} `json:"item"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &made)
	item := "/v1/work/items/" + made.Item.ID

	if rec := do(person, http.MethodPost, item, "d0", `{"op":"done_elsewhere"}`); rec.Code != 400 ||
		code(rec) != "reason_required" {
		t.Fatalf("with no words: %d %s", rec.Code, rec.Body)
	}
	if rec := do(machine, http.MethodPost, item, "d1",
		`{"op":"done_elsewhere","reason":"shipped in task 3f21"}`); rec.Code != 403 ||
		code(rec) != "session_cannot_decide" {
		t.Fatalf("a session on its own: %d %s", rec.Code, rec.Body)
	}
	rec = do(person, http.MethodPost, item, "d2",
		`{"op":"done_elsewhere","reason":"shipped in task 3f21, which named no work_id"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"closed_reason":"done_elsewhere"`) {
		t.Fatalf("done elsewhere: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"place":"board"`) {
		t.Fatalf("it did not come out of the Backlog onto the board: %s", rec.Body)
	}
	moves := do(person, http.MethodGet, item+"/moves", "", "")
	if !strings.Contains(moves.Body.String(), `"trigger":"done_elsewhere"`) ||
		!strings.Contains(moves.Body.String(), `"actor":"user"`) ||
		!strings.Contains(moves.Body.String(), "named no work_id") {
		t.Fatalf("the moves do not say who said it, or why: %s", moves.Body)
	}
	// Control: `drop` is still there and still says a person dropped it.
	rec = do(person, http.MethodPost, "/v1/work/items", "c2",
		`{"title":"the other one","project":"/p","place":"backlog"}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &made)
	if rec := do(person, http.MethodPost, "/v1/work/items/"+made.Item.ID, "d3", `{"op":"drop"}`); rec.Code != 200 ||
		!strings.Contains(rec.Body.String(), `"state":"dropped"`) {
		t.Fatalf("drop: %d %s", rec.Code, rec.Body)
	}
}
