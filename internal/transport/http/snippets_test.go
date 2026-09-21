package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// snippetSessions is a process table that answers one fixed reading.
type snippetSessions struct{ inv session.Inventory }

func (p snippetSessions) Scan(context.Context) (session.Inventory, error) { return p.inv, nil }

type snippetHarness struct {
	t     *testing.T
	s     *Server
	store *store.Store
	keys  int
}

func newSnippetHarness(t *testing.T, sessions ...session.Session) *snippetHarness {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	reading := session.Inventory{Provenance: "test", Complete: true, Sessions: sessions}
	return &snippetHarness{t: t, store: st, s: &Server{store: st,
		inventory: app.Inventory{Process: snippetSessions{reading}}}}
}

// person is a paired device that may read and send, which is the console on
// this Mac. writes says whether it may send.
func (h *snippetHarness) do(method, target, body string, writes bool, key string) *httptest.ResponseRecorder {
	h.t.Helper()
	caps := auth.NewCaps(auth.Read)
	if writes {
		caps = auth.NewCaps(auth.Read, auth.Send)
	}
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, reader)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	req = req.WithContext(context.WithValue(req.Context(), accessKey{},
		access{verdict: auth.Verdict{Allowed: true, Local: true, Caps: caps}}))
	rec := httptest.NewRecorder()
	if strings.HasPrefix(req.URL.Path, "/v1/snippets/") {
		h.s.snippetRoute(rec, req)
	} else {
		h.s.snippetsRoute(rec, req)
	}
	return rec
}

// write is one write under a fresh key, as the console mints one per decision.
func (h *snippetHarness) write(method, target, body string) *httptest.ResponseRecorder {
	h.keys++
	return h.do(method, target, body, true, "k"+string(rune('a'+h.keys)))
}

func (h *snippetHarness) list(target string) snippetListWire {
	h.t.Helper()
	rec := h.do(http.MethodGet, target, "", false, "")
	if rec.Code != http.StatusOK {
		h.t.Fatalf("GET %s answered %d: %s", target, rec.Code, rec.Body.String())
	}
	var out snippetListWire
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		h.t.Fatal(err)
	}
	return out
}

func snippetRefusalOf(t *testing.T, rec *httptest.ResponseRecorder) snippetRefusalWire {
	t.Helper()
	var out snippetRefusalWire
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%d %s: %v", rec.Code, rec.Body.String(), err)
	}
	return out
}

// A fresh machine holds no snippets, and says so as an empty list rather than
// as a refusal: nothing here is missing.
func TestAFreshMachineAnswersAnEmptySnippetList(t *testing.T) {
	h := newSnippetHarness(t)
	got := h.list("/v1/snippets")
	if len(got.Snippets) != 0 || got.Project != nil {
		t.Fatalf("%+v", got)
	}
}

// The whole round trip a person makes: write one, read it back, save it, and
// take it away.
func TestASnippetIsMadeSavedAndTakenAway(t *testing.T) {
	h := newSnippetHarness(t)
	rec := h.write(http.MethodPost, "/v1/snippets",
		`{"title":"回報進度","body":"回報你剛剛做了什麼。","scope":"global"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create answered %d: %s", rec.Code, rec.Body.String())
	}
	var made snippetWire
	if err := json.Unmarshal(rec.Body.Bytes(), &made); err != nil {
		t.Fatal(err)
	}
	if made.ID == "" || made.Scope != "global" || made.Position != 100 {
		t.Fatalf("%+v", made)
	}
	if made.Project != "" {
		t.Errorf("a global snippet carried a project: %q", made.Project)
	}
	// The wire leaves the key out rather than sending null, which is what lets
	// a client copy a record and send it back.
	if strings.Contains(rec.Body.String(), `"project"`) {
		t.Errorf("a global snippet's wire mentions project: %s", rec.Body.String())
	}

	if got := h.list("/v1/snippets"); len(got.Snippets) != 1 || got.Snippets[0].ID != made.ID {
		t.Fatalf("%+v", got)
	}

	rec = h.write(http.MethodPatch, "/v1/snippets/"+made.ID, `{"title":"回報"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("save answered %d: %s", rec.Code, rec.Body.String())
	}
	var saved snippetWire
	_ = json.Unmarshal(rec.Body.Bytes(), &saved)
	if saved.Title != "回報" || saved.Body != made.Body || saved.CreatedAt != made.CreatedAt {
		t.Fatalf("%+v", saved)
	}

	rec = h.write(http.MethodDelete, "/v1/snippets/"+made.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete answered %d: %s", rec.Code, rec.Body.String())
	}
	var gone snippetDeletedWire
	_ = json.Unmarshal(rec.Body.Bytes(), &gone)
	if !gone.OK || gone.Deleted != made.ID {
		t.Fatalf("%+v", gone)
	}
	if got := h.list("/v1/snippets"); len(got.Snippets) != 0 {
		t.Fatalf("%+v", got)
	}
}

// An id typed in capitals finds its snippet, as the Swift store canonicalises
// one, rather than being told it does not exist.
func TestAnIdInCapitalsFindsItsSnippet(t *testing.T) {
	h := newSnippetHarness(t)
	rec := h.write(http.MethodPost, "/v1/snippets", `{"title":"t","body":"b","scope":"global"}`)
	var made snippetWire
	_ = json.Unmarshal(rec.Body.Bytes(), &made)
	rec = h.write(http.MethodPatch, "/v1/snippets/"+strings.ToUpper(made.ID), `{"title":"T"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

// A device that may read and not send is told so, and nothing is written.
func TestAReadOnlyDeviceIsRefusedEveryWrite(t *testing.T) {
	h := newSnippetHarness(t)
	for _, c := range []struct{ method, target, body string }{
		{http.MethodPost, "/v1/snippets", `{"title":"t","body":"b","scope":"global"}`},
		{http.MethodPatch, "/v1/snippets/whatever", `{"title":"t"}`},
		{http.MethodDelete, "/v1/snippets/whatever", ""},
		{http.MethodPost, "/v1/snippets/order", `{"scope":"global","order":[]}`},
	} {
		rec := h.do(c.method, c.target, c.body, false, "key")
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s answered %d: %s", c.method, c.target, rec.Code, rec.Body.String())
			continue
		}
		if got := snippetRefusalOf(t, rec); got.Error != "forbidden" || got.Detail == "" {
			t.Errorf("%s %s: %+v", c.method, c.target, got)
		}
	}
	if rows, _ := h.store.Snippets(context.Background()); len(rows) != 0 {
		t.Fatalf("%d row(s) written by a refused device", len(rows))
	}
}

// Every write carries a key, and a retry under the same key is answered rather
// than carried out a second time.
func TestAWriteNeedsAKeyAndARetryUnderItIsAnsweredOnce(t *testing.T) {
	h := newSnippetHarness(t)
	rec := h.do(http.MethodPost, "/v1/snippets", `{"title":"t","body":"b","scope":"global"}`, true, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a write with no key answered %d", rec.Code)
	}
	if got := snippetRefusalOf(t, rec); got.Error != "idempotency_key_required" {
		t.Fatalf("%+v", got)
	}

	body := `{"title":"t","body":"b","scope":"global"}`
	first := h.do(http.MethodPost, "/v1/snippets", body, true, "one-key")
	again := h.do(http.MethodPost, "/v1/snippets", body, true, "one-key")
	if first.Code != http.StatusOK || again.Code != http.StatusOK {
		t.Fatalf("%d %d", first.Code, again.Code)
	}
	if first.Body.String() != again.Body.String() {
		t.Errorf("the retry was carried out again:\n%s\n%s", first.Body.String(), again.Body.String())
	}
	if again.Header().Get("Idempotent-Replayed") != "true" {
		t.Errorf("the retry was not marked a replay: %v", again.Header())
	}
	rows, _ := h.store.Snippets(context.Background())
	if len(rows) != 1 {
		t.Fatalf("%d snippet(s) from one decision sent twice", len(rows))
	}
}

// A refusal the domain decided reaches the browser with `error` and `detail` as
// two strings, which is what every client on this daemon branches on, and with
// the numbers a code cannot carry beside them.
func TestASnippetRefusalKeepsTheDaemonsEnvelope(t *testing.T) {
	h := newSnippetHarness(t)
	long := strings.Repeat("句", 70)
	rec := h.write(http.MethodPost, "/v1/snippets",
		`{"title":"`+long+`","body":"b","scope":"global"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	got := snippetRefusalOf(t, rec)
	if got.Error != "snippet_too_long" || got.Detail == "" {
		t.Fatalf("%+v", got)
	}
	if got.Counts["title_count"] != int64(len(long)) {
		t.Errorf("counts: %+v", got.Counts)
	}
	// And the two keys really are strings on the wire, not an object: a client
	// that reads `body.error` as a code must not be handed `{code, message}`.
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if _, ok := raw["error"].(string); !ok {
		t.Errorf("error is not a string: %v", raw["error"])
	}
	if _, ok := raw["detail"].(string); !ok {
		t.Errorf("detail is not a string: %v", raw["detail"])
	}
}

func TestAnUnknownFieldInAWriteIsRefusedByName(t *testing.T) {
	h := newSnippetHarness(t)
	rec := h.write(http.MethodPost, "/v1/snippets",
		`{"title":"t","body":"b","scope":"global","postition":"1"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if got := snippetRefusalOf(t, rec); got.Error != "malformed_snippet" {
		t.Fatalf("%+v", got)
	}
}

// One group's order is written whole; the order route is under the same prefix
// as one snippet's id and has to be found before the id is read.
func TestTheOrderRouteIsFoundBesideOneSnippetsId(t *testing.T) {
	h := newSnippetHarness(t)
	ids := []string{}
	for _, title := range []string{"one", "two", "three"} {
		rec := h.write(http.MethodPost, "/v1/snippets",
			`{"title":"`+title+`","body":"b","scope":"global"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var made snippetWire
		_ = json.Unmarshal(rec.Body.Bytes(), &made)
		ids = append(ids, made.ID)
	}
	order := `{"scope":"global","order":["` + ids[2] + `","` + ids[0] + `","` + ids[1] + `"]}`
	rec := h.write(http.MethodPost, "/v1/snippets/order", order)
	if rec.Code != http.StatusOK {
		t.Fatalf("order answered %d: %s", rec.Code, rec.Body.String())
	}
	var out snippetOrderedWire
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.Scope != "global" || len(out.Snippets) != 3 || out.Snippets[0].ID != ids[2] {
		t.Fatalf("%+v", out)
	}
	got := h.list("/v1/snippets")
	if len(got.Snippets) != 3 || got.Snippets[0].ID != ids[2] {
		t.Fatalf("read back as %+v", got.Snippets)
	}
}

// A global order that carries a project, and a project order that carries none,
// are each the one refusal that names the disagreement.
func TestAnOrderNamesOneGroupOrIsRefused(t *testing.T) {
	h := newSnippetHarness(t)
	for _, body := range []string{
		`{"scope":"global","project":"/p","order":[]}`,
		`{"scope":"project","order":[]}`,
	} {
		rec := h.write(http.MethodPost, "/v1/snippets/order", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s answered %d", body, rec.Code)
			continue
		}
		if got := snippetRefusalOf(t, rec); got.Error != "snippet_scope_mismatch" {
			t.Errorf("%s: %+v", body, got)
		}
	}
}

// A session this machine cannot see is the Swift route's 404, not a list
// filtered under a guessed project.
func TestAListForASessionNobodyHasIsNotFound(t *testing.T) {
	h := newSnippetHarness(t)
	rec := h.do(http.MethodGet, "/v1/snippets?session="+url.QueryEscape("%42"), "", false, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

// A session in an isolated worktree is in the project the worktree was cut
// from, so the snippets of the checkout are the snippets of the worktree.
//
// This is the whole reason the project is resolved on this machine and not in
// the browser: only here is there a git directory to ask.
func TestASessionInAWorktreeIsInTheProjectItWasCutFrom(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on this machine")
	}
	root := t.TempDir()
	checkout := filepath.Join(root, "checkout")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main", checkout},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Skipf("git %v: %v %s", args, err, out)
		}
	}
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v %s", args, err, out)
		}
	}
	run(checkout, "commit", "-q", "--allow-empty", "-m", "first")
	cut := filepath.Join(root, "cut")
	run(checkout, "worktree", "add", "-q", "-b", "side", cut)

	inside := filepath.Join(checkout, "deep", "deeper")
	if err := exec.Command("mkdir", "-p", inside).Run(); err != nil {
		t.Fatal(err)
	}
	h := newSnippetHarness(t,
		session.Session{ID: "%1", TTY: "ttys001", Assistant: "claude", CWD: cut},
		session.Session{ID: "%2", TTY: "ttys002", Assistant: "claude", CWD: inside})

	resolved := snippetPathSpelling(checkout)
	rec := h.write(http.MethodPost, "/v1/snippets",
		`{"title":"mine","body":"b","scope":"project","project":`+quote(resolved)+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	h.write(http.MethodPost, "/v1/snippets", `{"title":"everywhere","body":"b","scope":"global"}`)

	for _, id := range []string{"%1", "%2"} {
		// The pane id is escaped on the way in and used exactly as the parser
		// hands it over: a tmux pane is literally `%1`, and `%10` upwards is
		// what a second decoding turns into a control character.
		got := h.list("/v1/snippets?session=" + url.QueryEscape(id))
		if got.Project == nil || got.Project.Key != resolved {
			t.Fatalf("%s: %+v", id, got.Project)
		}
		if len(got.Snippets) != 2 || got.Snippets[0].Title != "mine" {
			t.Fatalf("%s: %+v", id, got.Snippets)
		}
	}
}

// A session that is in no repository and in no registered project is in its own
// directory, which is still a project a snippet can belong to.
func TestASessionOutsideAnyRepositoryIsInItsOwnDirectory(t *testing.T) {
	dir := t.TempDir()
	h := newSnippetHarness(t, session.Session{ID: "%7", TTY: "ttys007", Assistant: "claude", CWD: dir})
	got := h.list("/v1/snippets?session=%257")
	if got.Project == nil || got.Project.Key != snippetPathSpelling(dir) {
		t.Fatalf("%+v", got.Project)
	}
	if got.Project.Label != filepath.Base(dir) {
		t.Errorf("label %q", got.Project.Label)
	}
}

func quote(s string) string {
	out, _ := json.Marshal(s)
	return string(out)
}
