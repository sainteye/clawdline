package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/transport/cloud"
)

// A key is the method, the pattern the mux matched, and below a prefix only
// route words: an id, a pane, a file name or a query never becomes part of it.
func TestRouteKeyIsAShapeNotAPath(t *testing.T) {
	for _, c := range []struct{ method, pattern, path, want string }{
		{"GET", "/v1/sessions/", "/v1/sessions/%250/info", "GET /v1/sessions/:id/info"},
		{"GET", "/v1/sessions/", "/v1/sessions/claude-7f3a9c/info", "GET /v1/sessions/:id/info"},
		{"POST", "/v1/sessions/", "/v1/sessions/abc/send", "POST /v1/sessions/:id/send"},
		{"GET", "/v1/sessions/", "/v1/sessions/abc/agents/xyz", "GET /v1/sessions/:id/agents/:id"},
		{"GET", "/v1/sessions/", "/v1/sessions/abc/git/diff", "GET /v1/sessions/:id/git/diff"},
		{"GET", "/v1/sessions/", "/v1/sessions/abc/documents/notes%2Fsecret.md", "GET /v1/sessions/:id/documents/:id"},
		{"GET", "/v1/sessions", "/v1/sessions", "GET /v1/sessions"},
		{"GET", "/v1/transcript", "/v1/transcript", "GET /v1/transcript"},
		{"GET", "/v1/orchestrator/tasks/", "/v1/orchestrator/tasks/0f5672d8/notify", "GET /v1/orchestrator/tasks/:id/notify"},
		{"GET", "/", "/", "GET /"},
		{"GET", "/", "/assets/index-3f2a.js", "GET /assets/:id"},
		{"BREW", "/v1/health", "/v1/health", "OTHER /v1/health"},
		{"GET", "", "/nowhere", "GET (unmatched)"},
		{"GET", "GET /v1/x/", "/v1/x/yz", "GET /v1/x/:id"},
	} {
		if got := routeKey(c.method, c.pattern, c.path); got != c.want {
			t.Errorf("routeKey(%s, %q, %q) = %q, want %q", c.method, c.pattern, c.path, got, c.want)
		}
	}
}

// The table holds MaxRouteStatKeys shapes and counts the rest in overflow;
// each row keeps MaxRouteLatencySamples durations and its max apart.
func TestRouteStatsAreBounded(t *testing.T) {
	stats := newRouteStats(time.Unix(1_700_000_000, 0))
	for i := 0; i < MaxRouteStatKeys+5; i++ {
		stats.done(fmt.Sprintf("GET /r%d", i), callerLocal, 200, time.Millisecond, false)
	}
	got := stats.reading()
	if len(got.Routes) != MaxRouteStatKeys || got.Overflow != 5 || got.KeyLimit != MaxRouteStatKeys {
		t.Fatalf("rows %d overflow %d limit %d", len(got.Routes), got.Overflow, got.KeyLimit)
	}
	if r := stats.keys(); r.Used != MaxRouteStatKeys {
		t.Fatalf("the capacity reading: %+v", r)
	}

	stats = newRouteStats(time.Now())
	stats.done("GET /slow", callerLocal, 200, 900*time.Millisecond, false)
	for i := 0; i < MaxRouteLatencySamples+44; i++ {
		stats.done("GET /slow", callerLocal, 200, time.Duration(i%10+1)*time.Millisecond, false)
	}
	row := stats.reading().Routes[0]
	if row.Count != int64(MaxRouteLatencySamples+45) || row.LatencyMs == nil ||
		row.LatencyMs.Samples != MaxRouteLatencySamples {
		t.Fatalf("the row: %+v %+v", row, row.LatencyMs)
	}
	if row.LatencyMs.Max != 900 || row.LatencyMs.P90 > 10 || row.LatencyMs.P50 < 1 {
		t.Fatalf("the latency: %+v", row.LatencyMs)
	}
}

// Who asked is the gate's judgement, and a Cloud viewer — which carries this
// machine's own token by construction — is counted as cloud, not local.
func TestRouteStatsCountCallersByKind(t *testing.T) {
	f, _ := newGateFixture(t)
	stats := newRouteStats(time.Now())
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/sessions/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "{}")
	})
	h := stats.countRoutes(mux, f.g.wrap(noteCaller(mux)))

	path := "/v1/sessions/%250/info"
	bearer := func(tok string) map[string]string { return map[string]string{"Authorization": "Bearer " + tok} }
	for _, c := range []call{
		{method: http.MethodGet, path: path, headers: bearer(f.local)},
		{method: http.MethodGet, path: path, headers: bearer(f.read)},
		{method: http.MethodGet, path: path, headers: bearer(f.send)},
		{method: http.MethodGet, path: path},
	} {
		c.do(h)
	}
	router := cloud.Router{Handler: h, Authorize: cloud.LocalAuthorizer(f.local, f.machine)}
	if _, err := router.Do(context.Background(), cloudops.LocalRequest{Method: http.MethodGet, Path: path}); err != nil {
		t.Fatal(err)
	}

	got := stats.reading().Routes
	if len(got) != 1 || got[0].Route != "/v1/sessions/:id/info" || got[0].Method != "GET" {
		t.Fatalf("rows: %+v", got)
	}
	want := contract.RouteCallers{Local: 1, Device: 2, Anonymous: 1, Cloud: 1}
	if got[0].Callers != want || got[0].Count != 5 || got[0].Status2xx != 4 || got[0].Status4xx != 1 {
		t.Fatalf("callers %+v count %d 2xx %d 4xx %d", got[0].Callers, got[0].Count, got[0].Status2xx, got[0].Status4xx)
	}
}

// An event stream is counted as open while it runs, and has no latency.
func TestRouteStatsCountAStreamAsOpen(t *testing.T) {
	stats := newRouteStats(time.Now())
	mux := http.NewServeMux()
	opened, release := make(chan struct{}), make(chan struct{})
	mux.HandleFunc("/v1/events", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			t.Error("the counter hid the flusher")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		close(opened)
		<-release
	})
	h := stats.countRoutes(mux, mux)
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/events", nil))
	}()
	<-opened
	if row := stats.reading().Routes[0]; !row.Stream || row.Open != 1 || row.Opens != 1 || row.Count != 0 {
		t.Fatalf("while open: %+v", row)
	}
	close(release)
	<-done
	row := stats.reading().Routes[0]
	if row.Open != 0 || row.Opens != 1 || row.Count != 1 || row.LatencyMs != nil {
		t.Fatalf("after: %+v", row)
	}
}

// A writer that cannot flush is not made to look as if it could: the event
// routes refuse a Cloud recorder on exactly that.
func TestRouteStatsKeepAWriterThatCannotFlush(t *testing.T) {
	stats := newRouteStats(time.Now())
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/events", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Flusher); ok {
			t.Error("the counter made a flusher out of a writer that is not one")
		}
	})
	stats.countRoutes(mux, mux).ServeHTTP(noFlush{httptest.NewRecorder()},
		httptest.NewRequest(http.MethodGet, "/v1/events", nil))
}

type noFlush struct{ w *httptest.ResponseRecorder }

func (n noFlush) Header() http.Header         { return n.w.Header() }
func (n noFlush) Write(b []byte) (int, error) { return n.w.Write(b) }
func (n noFlush) WriteHeader(code int)        { n.w.WriteHeader(code) }

// Through the whole daemon: what a session id, a document name and a query
// carried never reaches /v1/diagnostics.routes.
func TestRouteStatsNeverNameASession(t *testing.T) {
	p := shellPane(t.TempDir())
	_, handler, local, _ := wholeServer(t, p, p)
	ask := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "127.0.0.1:7757"
		req.Header.Set("Authorization", "Bearer "+local)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	for _, path := range []string{
		"/v1/sessions/%250/info",
		"/v1/sessions/sess-SECRETID42/info?text=PRIVATEWORDS",
		"/v1/sessions/sess-SECRETID42/documents/PRIVATEFILE.md",
		"/v1/transcript?session=sess-SECRETID42&q=PRIVATEWORDS",
	} {
		ask(path)
	}
	rec := ask("/v1/diagnostics")
	var diag contract.Diagnostics
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &diag) != nil || diag.Routes == nil {
		t.Fatalf("diagnostics: %d %s", rec.Code, rec.Body)
	}
	routes, _ := json.Marshal(diag.Routes)
	for _, secret := range []string{"SECRETID42", "PRIVATEWORDS", "PRIVATEFILE", "%250", "%0"} {
		if strings.Contains(string(routes), secret) {
			t.Errorf("routes named %q: %s", secret, routes)
		}
	}
	seen := map[string]int64{}
	for _, r := range diag.Routes.Routes {
		seen[r.Method+" "+r.Route] = r.Count
	}
	if seen["GET /v1/sessions/:id/info"] != 2 || seen["GET /v1/transcript"] != 1 ||
		seen["GET /v1/sessions/:id/documents/:id"] != 1 {
		t.Fatalf("rows: %v", seen)
	}
	if diag.Routes.Since == 0 || diag.Routes.LatencySamples != MaxRouteLatencySamples {
		t.Fatalf("the block: %+v", diag.Routes)
	}
}
