package http

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/auth"
)

type gateFixture struct {
	g       *gate
	local   string
	send    string
	read    string
	machine string
}

// newGateFixture opens a gate over a fresh directory, behind which every route
// answers 200 "reached", with a device of each kind.
func newGateFixture(t *testing.T) (*gateFixture, http.Handler) {
	t.Helper()
	t.Setenv("CLAWDLINE_SWIFT_DIR", filepath.Join(t.TempDir(), "swift"))
	g := openGate(config.Config{Dir: filepath.Join(t.TempDir(), "next"), Port: 7757})
	if g.err != nil {
		t.Fatal(g.err)
	}
	f := &gateFixture{g: g}
	var err error
	if f.local, err = g.auth.LocalToken(); err != nil {
		t.Fatal(err)
	}
	if _, f.send, err = g.auth.AddDevice("sender", auth.NewCaps(auth.Read, auth.Send), false); err != nil {
		t.Fatal(err)
	}
	if _, f.read, err = g.auth.AddDevice("reader", auth.NewCaps(auth.Read), false); err != nil {
		t.Fatal(err)
	}
	if f.machine, err = g.files.MachineToken(); err != nil {
		t.Fatal(err)
	}
	reached := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "reached")
	})
	return f, g.wrap(reached)
}

type call struct {
	method, path, host, body string
	headers                  map[string]string
}

func (c call) do(h http.Handler) *httptest.ResponseRecorder {
	method := c.method
	if method == "" {
		method = http.MethodPost
	}
	req := httptest.NewRequest(method, "http://127.0.0.1:7757"+c.path, strings.NewReader(c.body))
	if c.body == "" {
		req.Body = http.NoBody
		req.ContentLength = 0
	}
	req.Host = "127.0.0.1:7757"
	if c.host != "" {
		req.Host = c.host
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func refusalCode(rec *httptest.ResponseRecorder) string {
	var r struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	return r.Error.Code
}

// A change is let through only from this request's own origin, with the
// cookie only when the request says where it came from, and only with a JSON
// body or none.
func TestGateChangeChecks(t *testing.T) {
	f, h := newGateFixture(t)
	const board = `{"operation":"note","requestId":"r","expectedRevision":0,"actor":"t"}`
	cookie := "clawdline-next=" + f.send
	here := "http://127.0.0.1:7757"
	for _, tc := range []struct {
		name string
		call call
		want int
	}{
		{"the review's request: another port, same-site, cookie, text/plain",
			call{path: "/v1/board", body: board, headers: map[string]string{
				"Origin": "http://127.0.0.1:9999", "Sec-Fetch-Site": "same-site", "Cookie": cookie, "Content-Type": "text/plain"}}, 403},
		{"another port, cookie, json",
			call{path: "/v1/board", body: board, headers: map[string]string{
				"Origin": "http://127.0.0.1:9999", "Cookie": cookie, "Content-Type": "application/json"}}, 403},
		{"another scheme",
			call{path: "/v1/board", body: board, headers: map[string]string{
				"Origin": "https://127.0.0.1:7757", "Cookie": cookie, "Content-Type": "application/json"}}, 403},
		{"another host on this port",
			call{path: "/v1/board", body: board, headers: map[string]string{
				"Origin": "http://localhost:7757", "Cookie": cookie, "Content-Type": "application/json"}}, 403},
		{"cookie with no Origin",
			call{path: "/v1/board", body: board, headers: map[string]string{
				"Cookie": cookie, "Content-Type": "application/json"}}, 403},
		{"cookie, our Origin, but same-site",
			call{path: "/v1/board", body: board, headers: map[string]string{
				"Origin": here, "Sec-Fetch-Site": "same-site", "Cookie": cookie, "Content-Type": "application/json"}}, 403},
		{"Origin null",
			call{path: "/v1/board", body: board, headers: map[string]string{
				"Origin": "null", "Authorization": "Bearer " + f.send, "Content-Type": "application/json"}}, 403},
		{"Origin with a path",
			call{path: "/v1/board", body: board, headers: map[string]string{
				"Origin": here + "/x", "Cookie": cookie, "Content-Type": "application/json"}}, 403},
		{"cookie, our Origin, text/plain",
			call{path: "/v1/board", body: board, headers: map[string]string{
				"Origin": here, "Sec-Fetch-Site": "same-origin", "Cookie": cookie, "Content-Type": "text/plain"}}, 415},
		{"cookie, our Origin, a form",
			call{path: "/v1/board", body: "a=b", headers: map[string]string{
				"Origin": here, "Cookie": cookie, "Content-Type": "application/x-www-form-urlencoded"}}, 415},
		{"cookie, our Origin, a body with no type",
			call{path: "/v1/board", body: board, headers: map[string]string{
				"Origin": here, "Cookie": cookie}}, 415},
		{"cookie, our Origin, same-origin, json: the console's own write",
			call{path: "/v1/board", body: board, headers: map[string]string{
				"Origin": here, "Sec-Fetch-Site": "same-origin", "Cookie": cookie, "Content-Type": "application/json; charset=utf-8"}}, 200},
		{"cookie, our Origin, no Sec-Fetch-Site, json",
			call{path: "/v1/board", body: board, headers: map[string]string{
				"Origin": here, "Cookie": cookie, "Content-Type": "application/json"}}, 200},
		{"a header token and no Origin: a script",
			call{path: "/v1/board", body: board, headers: map[string]string{
				"Authorization": "Bearer " + f.send, "Content-Type": "application/json"}}, 200},
		{"a header token wins over a cookie, and needs no Origin",
			call{path: "/v1/board", body: board, headers: map[string]string{
				"Authorization": "Bearer " + f.send, "Cookie": "clawdline-next=stale", "Content-Type": "application/json"}}, 200},
		{"a header token and a form body",
			call{path: "/v1/board", body: "a=b", headers: map[string]string{
				"Authorization": "Bearer " + f.send, "Content-Type": "application/x-www-form-urlencoded"}}, 415},
		{"a header token and no body",
			call{path: "/v1/board", headers: map[string]string{"Authorization": "Bearer " + f.send}}, 200},
		{"a read-only device is refused for the capability before the body type",
			call{path: "/v1/board", body: "x", headers: map[string]string{
				"Authorization": "Bearer " + f.read, "Content-Type": "text/plain"}}, 403},
		{"PUT is a change too",
			call{method: http.MethodPut, path: "/v1/board", body: "x", headers: map[string]string{
				"Origin": here, "Cookie": cookie, "Content-Type": "text/plain"}}, 415},
		{"a read with a foreign Origin is a read",
			call{method: http.MethodGet, path: "/v1/board", headers: map[string]string{
				"Origin": "http://127.0.0.1:9999", "Cookie": cookie}}, 200},
		{"logout from another port with the cookie",
			call{path: "/v1/auth/logout", headers: map[string]string{
				"Origin": "http://127.0.0.1:9999", "Cookie": cookie}}, 403},
		{"logout from this page with the cookie",
			call{path: "/v1/auth/logout", headers: map[string]string{"Origin": here, "Cookie": cookie}}, 200},
		{"pair from another port",
			call{path: "/v1/auth/pair", body: `{}`, headers: map[string]string{
				"Origin": "http://127.0.0.1:9999", "Content-Type": "application/json"}}, 403},
		{"pair as plain text",
			call{path: "/v1/auth/pair", body: `{}`, headers: map[string]string{"Content-Type": "text/plain"}}, 415},
		{"localhost, spelled the same both ways",
			call{path: "/v1/board", host: "localhost:7757", body: board, headers: map[string]string{
				"Origin": "http://localhost:7757", "Cookie": cookie, "Content-Type": "application/json"}}, 200},
		{"IPv6 loopback",
			call{path: "/v1/board", host: "[::1]:7757", body: board, headers: map[string]string{
				"Origin": "http://[::1]:7757", "Cookie": cookie, "Content-Type": "application/json"}}, 200},
		{"through a tunnel",
			call{path: "/v1/board", host: "abc.trycloudflare.com", body: board, headers: map[string]string{
				"Origin": "https://abc.trycloudflare.com", "X-Forwarded-Proto": "https", "Cookie": cookie, "Content-Type": "application/json"}}, 200},
		{"through a tunnel, the default port written out",
			call{path: "/v1/board", host: "abc.trycloudflare.com", body: board, headers: map[string]string{
				"Origin": "https://ABC.trycloudflare.com:443", "X-Forwarded-Proto": "https", "Cookie": cookie, "Content-Type": "application/json"}}, 200},
		{"through a tunnel, another quick tunnel's page",
			call{path: "/v1/board", host: "abc.trycloudflare.com", body: board, headers: map[string]string{
				"Origin": "https://evil.trycloudflare.com", "X-Forwarded-Proto": "https", "Cookie": cookie, "Content-Type": "application/json"}}, 403},
		{"through a tunnel, an http page",
			call{path: "/v1/board", host: "abc.trycloudflare.com", body: board, headers: map[string]string{
				"Origin": "http://abc.trycloudflare.com", "X-Forwarded-Proto": "https", "Cookie": cookie, "Content-Type": "application/json"}}, 403},
	} {
		rec := tc.call.do(h)
		if rec.Code != tc.want {
			t.Errorf("%s: %d %s, want %d", tc.name, rec.Code, rec.Body, tc.want)
		}
		if tc.want == 415 && refusalCode(rec) != "unsupported_media_type" {
			t.Errorf("%s: code %q", tc.name, refusalCode(rec))
		}
	}
}

// Every route that changes something, whoever owns its handler, goes through
// the same two checks: a page on another port is refused with this machine's
// own cookie, and a body that is not JSON is refused with every credential
// there is.
func TestGateCoversEveryChange(t *testing.T) {
	f, h := newGateFixture(t)
	writes := []string{
		"/v1/board",
		"/v1/orchestrator/schedules",
		"/v1/next/coordinator",
		"/v1/orchestrator/tasks", "/v1/orchestrator/tasks/t1/respawn",
		"/v1/orchestrator/usage",
		"/v1/sessions/%25x/send", "/v1/sessions/%25x/interrupt", "/v1/sessions/%25x/close",
		"/v1/settings",
		"/v1/projects/p/worktrees/refresh",
		"/v1/auth/pair", "/v1/auth/pair/confirm", "/v1/auth/adopt", "/v1/auth/password", "/v1/auth/logout",
		"/v1/auth/devices/revoke-all", "/v1/auth/devices/browser", "/v1/auth/devices/password",
		"/v1/auth/devices/d1/revoke", "/v1/auth/devices/d1/caps",
		"/v1/some/route/the/proxy/would/carry",
	}
	sort.Strings(writes)
	for _, p := range writes {
		rec := call{path: p, body: `{}`, headers: map[string]string{
			"Origin": "http://127.0.0.1:9999", "Cookie": "clawdline-next=" + f.local,
			"X-Clawdline-Orchestrator": f.machine, "Content-Type": "application/json"}}.do(h)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s from another port: %d %s", p, rec.Code, rec.Body)
		}
		rec = call{path: p, body: `{}`, headers: map[string]string{
			"Authorization": "Bearer " + f.local, "X-Clawdline-Orchestrator": f.machine,
			"Content-Type": "text/plain"}}.do(h)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("%s as text/plain: %d %s", p, rec.Code, rec.Body)
		}
		rec = call{path: p, body: `{}`, headers: map[string]string{
			"Authorization": "Bearer " + f.local, "X-Clawdline-Orchestrator": f.machine,
			"Content-Type": "application/json"}}.do(h)
		if rec.Code != http.StatusOK {
			t.Errorf("%s as json with every credential: %d %s", p, rec.Code, rec.Body)
		}
	}
}

// The open health says it is alive, and the two things about the door a page
// needs before it is let in (TestHealthAnswersTheDoor), and nothing else; the
// rest is behind this machine's own token.
func TestHealthAndDiagnostics(t *testing.T) {
	f, _ := newGateFixture(t)
	s := &Server{cfg: config.Config{Dir: "/secret/state/dir", Port: 7757, UpstreamPort: 7717}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.health)
	mux.HandleFunc("/v1/diagnostics", s.diagnostics)
	h := f.g.wrap(mux)

	rec := call{method: http.MethodGet, path: "/v1/health"}.do(h)
	var open map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &open); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("health: %d %s", rec.Code, rec.Body)
	}
	var keys []string
	for k := range open {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if strings.Join(keys, ",") != "at,authed,ok,password,served_by" || strings.Contains(rec.Body.String(), "/secret") {
		t.Fatalf("the open health says more than it should: %s", rec.Body)
	}

	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"no token", nil, http.StatusUnauthorized},
		{"a paired device", map[string]string{"Authorization": "Bearer " + f.send}, http.StatusForbidden},
		{"the machine token", map[string]string{"X-Clawdline-Orchestrator": f.machine}, http.StatusUnauthorized},
		{"this machine's token", map[string]string{"Authorization": "Bearer " + f.local}, http.StatusOK},
	} {
		rec := call{method: http.MethodGet, path: "/v1/diagnostics", headers: tc.headers}.do(h)
		if rec.Code != tc.want {
			t.Errorf("diagnostics, %s: %d %s", tc.name, rec.Code, rec.Body)
		}
		if tc.want != http.StatusOK && strings.Contains(rec.Body.String(), "/secret") {
			t.Errorf("diagnostics, %s: the path leaked", tc.name)
		}
		if tc.want == http.StatusOK && !strings.Contains(rec.Body.String(), `"dir":"/secret/state/dir"`) {
			t.Errorf("diagnostics: %s", rec.Body)
		}
	}
}

// One path, read one way — the gate and the mux, and every decision either of
// them makes.
//
// The bypass this pins was real on 2026-09-18, found by an independent review:
// the gate decided on `r.URL.Path`, which net/http had already percent-decoded, so
// `/v1/sessions/..%2F..%2Fv1%2Fauth%2Fx/git` read as `/v1/auth/x/git` — on the
// open list — while `http.ServeMux` matched the same request against
// `/v1/sessions/` and ran the session handler. No token at all, and `/git` and
// `/info` read the whole machine (ps, tmux, osascript) before looking anything
// up. A second spelling of the same fault was found while fixing it: with four
// `../` a documents path cleaned to `/v1/health` at the gate while
// `withDocuments`, which matches the raw path, still recognised it as the
// documents route.
//
// So these are about the spelling of a path rather than about who is asking,
// and each one is asked twice: with no credential and with a good one.
func TestGateReadsAPathOneWay(t *testing.T) {
	f, h := newGateFixture(t)
	unreadable := []struct {
		name, path string
	}{
		{"the reviewer's /git", "/v1/sessions/..%2F..%2Fv1%2Fauth%2Fx/git"},
		{"the reviewer's /info", "/v1/sessions/..%2F..%2Fv1%2Fauth%2Fx/info"},
		{"the reviewer's /places", "/v1/places/..%2F..%2Fv1%2Fauth%2Fx/sessions"},
		{"the reviewer's /documents", "/v1/sessions/..%2F..%2Fv1%2Fauth%2Fx/documents/project/notes.md"},
		{"lowercase %2f", "/v1/sessions/..%2f..%2fv1%2fauth%2fx/git"},
		{"the dots encoded too", "/v1/sessions/%2e%2e%2F%2e%2e%2Fv1%2Fauth%2Fx/git"},
		{"a bare encoded separator", "/v1/sessions/a%2Fb/git"},
		{"out of the open list with dots", "/v1/auth/x/..%2F..%2Fv1%2Fsessions"},
		{"the documents matcher's own spelling", "/v1/sessions/%2579/documents/../../../../v1/health"},
		{"a literal dot segment", "/v1/sessions/./%2579/git"},
		{"a backslash in a name", "/v1/sessions/a%5Cb/git"},
		{"a NUL in a name", "/v1/sessions/a%00b/git"},
	}
	for _, tc := range unreadable {
		for _, who := range []struct {
			name    string
			headers map[string]string
		}{
			{"no credential", nil},
			{"a good token", map[string]string{"Authorization": "Bearer " + f.local}},
		} {
			rec := call{method: http.MethodGet, path: tc.path, headers: who.headers}.do(h)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s (%s): status %d, want 400", tc.name, who.name, rec.Code)
			}
			if strings.Contains(rec.Body.String(), "reached") {
				t.Errorf("%s (%s): reached the handler", tc.name, who.name)
			}
		}
	}

	// And the other half of the same rule: an ordinary spelling is answered as
	// it always was, including the percent sign in a tmux pane's name.
	for _, tc := range []struct {
		name, path string
		headers    map[string]string
		want       int
	}{
		{"a pane id, no credential", "/v1/sessions/%2579/git", nil, 401},
		{"a pane id, a token", "/v1/sessions/%2579/git",
			map[string]string{"Authorization": "Bearer " + f.read}, 200},
		{"a place, a token", "/v1/places/470885724e5330e1/sessions",
			map[string]string{"Authorization": "Bearer " + f.read}, 200},
		{"documents, a token", "/v1/sessions/%2579/documents/project/notes.md",
			map[string]string{"Authorization": "Bearer " + f.read}, 200},
		{"the open page", "/v1/health", nil, 200},
		{"the door", "/v1/auth/open", nil, 200},
		{"the bundle", "/assets/index-abc123.js", nil, 200},
		{"the strings", "/strings/zh-Hant.json", nil, 200},
		{"a project's worktrees, the orchestrator's token",
			"/v1/projects/project-470885724e5330e17b907e43/worktrees",
			map[string]string{machineHeader: f.machine}, 200},
	} {
		rec := call{method: http.MethodGet, path: tc.path, headers: tc.headers}.do(h)
		if rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d (%s)", tc.name, rec.Code, tc.want, rec.Body.String())
		}
	}
}

// readablePath states the rule rather than the spellings above, so it is worth
// its own table: a segment is a name, and a name holds no separator.
func TestReadablePath(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/", true},
		{"/v1/health", true},
		{"/v1/sessions/%2579/git", true},
		{"/v1/auth/", true},
		{"/v1/projects/project-abc/worktrees", true},
		{"/assets/index-a.b.c.js", true},
		{"/v1/sessions/a.b/git", true},
		{"/v1/sessions/..%2F../git", false},
		{"/v1/sessions/%2f/git", false},
		{"/v1/sessions/%2E%2E/git", false},
		{"/v1/sessions/../git", false},
		{"/v1/sessions/./git", false},
		{"/v1/sessions/a%5Cb/git", false},
		{"/v1/sessions/a%00b/git", false},
		{"/v1/sessions/%zz/git", false},
	} {
		if got := readablePath(tc.path); got != tc.want {
			t.Errorf("readablePath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
