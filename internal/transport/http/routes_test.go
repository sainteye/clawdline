package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
)

// On 2026-10-04 a console newer than its daemon said 「讀取失敗」 beside features
// the daemon simply lacked (docs/updates.md). A top-level route it lacked said
// 501 not_implemented, but a sub-route under one of the prefix handlers said
// 404 not_found — the same answer as a record that is not there. Both now say
// 501 and name the route, through the one helper the fallback uses.
func TestAnUnknownSubrouteNamesItselfLikeAnUnknownRoute(t *testing.T) {
	withoutInheritedNextEnv(t)
	h, token := upstreamFixture(t, config.Load())
	for _, ask := range []struct{ method, path string }{
		{http.MethodGet, "/v1/a-route-nobody-has-written-yet"},
		{http.MethodGet, "/v1/work/nosuch"},
		{http.MethodGet, "/v1/work/v2/nosuch"},
		{http.MethodGet, "/v1/squad/nosuch"},
		{http.MethodPost, "/v1/squad-packages/nosuch"},
		{http.MethodGet, "/v1/push/nosuch"},
		{http.MethodGet, "/v1/auth/nosuch"},
		{http.MethodGet, "/v1/usage/nosuch"},
		{http.MethodGet, "/v1/orchestrator/waits/nosuch"},
		{http.MethodPost, "/v1/orchestrator/tasks/some-task/nosuch"},
		{http.MethodGet, "/v1/verifications/some-id/nosuch"},
		{http.MethodGet, "/v1/places/some-place/nosuch"},
	} {
		rec := ask2(t, h, token, ask.method, ask.path)
		var refusal contract.Refusal
		_ = json.Unmarshal(rec.Body.Bytes(), &refusal)
		if rec.Code != http.StatusNotImplemented || refusal.Error != "not_implemented" || refusal.Route != ask.path {
			t.Errorf("%s %s answered %d: %s", ask.method, ask.path, rec.Code, rec.Body)
		}
	}
}

// A route that exists, asked for a record that does not, is still 404: that
// is the answer a console must keep reading as "not found", never as "needs an
// update".
func TestAMissingRecordStaysNotFound(t *testing.T) {
	withoutInheritedNextEnv(t)
	h, token := upstreamFixture(t, config.Load())
	for _, path := range []string{
		"/v1/verifications/no-such-verification",
		"/v1/orchestrator/schedules/no-such-schedule",
	} {
		rec := ask2(t, h, token, http.MethodGet, path)
		if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "not_implemented") {
			t.Errorf("%s answered %d: %s", path, rec.Code, rec.Body)
		}
	}
}

// Health says which build answered and which routes it has, so a console can
// tell a machine to update before it asks for what the machine lacks.
func TestHealthSaysTheVersionAndTheAPILevel(t *testing.T) {
	withoutInheritedNextEnv(t)
	t.Setenv("CLAWDLINE_SWIFT_DIR", filepath.Join(t.TempDir(), "swift"))
	t.Setenv("CLAWDLINE_NEXT_WEB", "")
	cfg := config.Load()
	cfg.Dir = filepath.Join(t.TempDir(), "next")
	cfg.Port = 7757
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.store.Close() })
	s.SetVersion("v9.9.9-test")
	rec := ask2(t, s.Handler(), "", http.MethodGet, "/v1/health")
	var health contract.Health
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatalf("health: %d %s", rec.Code, rec.Body)
	}
	if health.Version != "v9.9.9-test" || health.APILevel != APILevel {
		t.Errorf("health says version %q and api_level %d: %s", health.Version, health.APILevel, rec.Body)
	}
}

// The route table Handler registers and api/v1/routes.json are one list. A
// route added to the table without running contract-gen — or a routes file
// edited by hand — fails here, and contract-gen refuses to write a changed
// set at an unchanged level (tools/contract-gen planRoutes).
func TestRouteTableMatchesRoutesJSON(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "v1", "routes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		APILevel int `json:"api_level"`
		Routes   []struct {
			Method  string `json:"method"`
			Pattern string `json:"pattern"`
		} `json:"routes"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	written := []Route{}
	for _, r := range file.Routes {
		written = append(written, Route{Method: r.Method, Pattern: r.Pattern})
	}
	if table := Routes(); !reflect.DeepEqual(table, written) {
		inTable, inFile := routeDiff(table, written), routeDiff(written, table)
		t.Errorf("the route table and api/v1/routes.json differ: only in the table %v, only in the file %v; "+
			"raise APILevel if a route was added or removed, then run: go run ./tools/contract-gen", inTable, inFile)
	}
	if file.APILevel != APILevel {
		t.Errorf("api/v1/routes.json is at api_level %d and APILevel is %d; run: go run ./tools/contract-gen", file.APILevel, APILevel)
	}
}

func routeDiff(a, b []Route) []Route {
	in := map[Route]bool{}
	for _, r := range b {
		in[r] = true
	}
	out := []Route{}
	for _, r := range a {
		if !in[r] {
			out = append(out, r)
		}
	}
	return out
}

func ask2(t *testing.T, h http.Handler, token, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Host = "127.0.0.1:7757"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
