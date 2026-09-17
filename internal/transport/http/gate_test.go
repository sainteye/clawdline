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

	"github.com/sainteye/clawdline-go/internal/config"
	"github.com/sainteye/clawdline-go/internal/domain/auth"
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
		"/v1/board", "/v1/next/board",
		"/v1/orchestrator/schedules", "/v1/next/schedules",
		"/v1/next/coordinator",
		"/v1/orchestrator/tasks", "/v1/orchestrator/tasks/t1/settle",
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

// The open health says it is alive and nothing else; the rest is behind this
// machine's own token.
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
	if strings.Join(keys, ",") != "at,ok,served_by" || strings.Contains(rec.Body.String(), "/secret") {
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
