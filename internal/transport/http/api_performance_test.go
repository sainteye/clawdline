package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/apiperformance"
	"github.com/sainteye/clawdline/internal/transport/cloud"
)

func apiPerformanceFixture(t *testing.T) (*Server, *gateFixture, http.Handler) {
	t.Helper()
	f, _ := newGateFixture(t)
	s := &Server{cfg: config.Config{Dir: f.g.dir, Port: 7757}, version: "synthetic-api-performance"}
	gates.Store(f.g.dir, f.g)
	t.Cleanup(func() { gates.Delete(f.g.dir) })
	ctx, cancel := context.WithCancel(context.Background())
	stop := s.StartAPIPerformance(ctx)
	t.Cleanup(func() { cancel(); stop() })
	return s, f, s.Handler()
}

func TestAPIPerformanceOwnerGateAndCloudDenial(t *testing.T) {
	s, f, h := apiPerformanceFixture(t)
	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"anonymous", nil, 401},
		{"unrelated", map[string]string{"Authorization": "Bearer unrelated"}, 401},
		{"task secret", map[string]string{"X-Clawdline-Task-Secret": "synthetic-task-secret"}, 401},
		{"machine", map[string]string{machineHeader: f.machine}, 403},
		{"paired reader", map[string]string{"Authorization": "Bearer " + f.read}, 200},
		{"local", map[string]string{"Authorization": "Bearer " + f.local}, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := call{method: "GET", path: "/v1/api-performance", headers: tc.headers}.do(h)
			if v.Code != tc.want || v.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%d %s cache=%s", v.Code, v.Body.String(), v.Header().Get("Cache-Control"))
			}
			if tc.want == 200 {
				var r apiperformance.Reading
				if err := json.Unmarshal(v.Body.Bytes(), &r); err != nil {
					t.Fatal(err)
				}
				if r.Source != "owner_local" || r.Schema != 1 || len(r.RecentMinutes) != 6 || r.Build != s.version {
					t.Fatalf("contract: %+v", r)
				}
			}
		})
	}
	router := cloud.Router{Handler: h, Authorize: cloud.LocalAuthorizer(f.local, f.machine)}
	res, err := router.Do(context.Background(), cloudops.LocalRequest{Method: "GET", Path: "/v1/api-performance"})
	if err != nil || res.Status != 403 || refusalError(t, string(res.Body)) != "api_performance_owner_local" {
		t.Fatalf("cloud read: %+v %v", res, err)
	}
	if strings.Contains(string(res.Body), "boot_id") || strings.Contains(string(res.Body), "slow_routes") {
		t.Fatal("owner data leaked to Cloud")
	}
}

func TestAPIPerformanceVerifiedRemoteViewerUsesTheSameReadGate(t *testing.T) {
	_, f, h := apiPerformanceFixture(t)
	for _, tc := range []struct {
		name, token string
		want        int
	}{
		{"paired read-only", f.read, 200}, {"local authorizer", f.local, 200}, {"unrelated", "unrelated", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := cloud.Router{Handler: h, Authorize: cloud.LocalAuthorizer(tc.token, f.machine)}
			res, err := router.Do(context.Background(), cloudops.LocalRequest{Method: "GET", Path: "/v1/api-performance", Query: map[string]string{"hours": "6"}, Viewer: "verified-paired-viewer"})
			if err != nil || res.Status != tc.want {
				t.Fatalf("remote read: %d %v", res.Status, err)
			}
		})
	}
}

func TestAPIPerformanceBoundedQueryHeadAndUnavailable(t *testing.T) {
	s, f, h := apiPerformanceFixture(t)
	auth := map[string]string{"Authorization": "Bearer " + f.read}
	for _, query := range []string{"hours=2", "hours=1&hours=6", "session=private", "until=-1", "until=9999999999999", "until=1", "x=" + strings.Repeat("z", 300), "hours=%zz"} {
		rec := call{method: "GET", path: "/v1/api-performance?" + query, headers: auth}.do(h)
		if rec.Code != 400 || refusalError(t, rec.Body.String()) != "api_performance_bad_query" {
			t.Fatalf("%s: %d %s", query, rec.Code, rec.Body.String())
		}
	}
	rec := call{method: "HEAD", path: "/v1/api-performance", headers: auth}.do(h)
	if rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatalf("HEAD: %d %s", rec.Code, rec.Body.String())
	}
	rec = call{method: "POST", path: "/v1/api-performance", headers: map[string]string{"Authorization": "Bearer " + f.local}}.do(h)
	if rec.Code != 405 {
		t.Fatalf("mutating local read: %d %s", rec.Code, rec.Body.String())
	}
	s.routeStatsTable().performance.Store(nil)
	rec = call{method: "GET", path: "/v1/api-performance", headers: auth}.do(h)
	if rec.Code != 503 || refusalError(t, rec.Body.String()) != "api_performance_unavailable" {
		t.Fatalf("off: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAPIPerformanceCountsFullHandlerTimeAndSanitizesRoutes(t *testing.T) {
	s, _, _ := apiPerformanceFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/sessions/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); time.Sleep(12 * time.Millisecond) })
	mux.HandleFunc("/v1/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		time.Sleep(3 * time.Millisecond)
	})
	h := s.routeStatsTable().countRoutes(mux, mux)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://127.0.0.1/v1/sessions/private-session/info?token=private-token", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://127.0.0.1/v1/events", nil))
	rt := s.routeStatsTable().performance.Load()
	var v apiperformance.Reading
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		var err error
		v, err = rt.Query(context.Background(), 1, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		var completed uint64
		for _, p := range v.RecentMinutes {
			completed += p.Totals.Completed
		}
		if completed == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	var sum apiperformance.Totals
	for _, p := range v.RecentMinutes {
		sum.Completed += p.Totals.Completed
		sum.Finite.Count += p.Totals.Finite.Count
		if p.Totals.Finite.MaxMS > sum.Finite.MaxMS {
			sum.Finite.MaxMS = p.Totals.Finite.MaxMS
		}
		sum.StreamLifetime.Count += p.Totals.StreamLifetime.Count
	}
	if sum.Completed != 2 || sum.Finite.Count != 1 || sum.StreamLifetime.Count != 1 || sum.Finite.MaxMS < 10 {
		t.Fatalf("wall time/stream isolation: %+v", sum)
	}
	payload, _ := json.Marshal(v)
	if !strings.Contains(string(payload), "GET /v1/sessions/:id/info") {
		t.Fatal("the test did not reach route summaries")
	}
	if strings.Contains(string(payload), "private-session") || strings.Contains(string(payload), "private-token") {
		t.Fatal("raw request data reached API history")
	}
	// Cumulative diagnostic counters still use their existing contract.
	reading := s.routeStatsTable().reading()
	if len(reading.Routes) != 2 || reading.Overflow != 0 {
		t.Fatalf("cumulative contract changed: %+v", reading)
	}
}

func TestAPIPerformanceCountsPanicsAndInformationalHeaders(t *testing.T) {
	s, _, _ := apiPerformanceFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/panic", func(w http.ResponseWriter, r *http.Request) { panic("synthetic panic") })
	mux.HandleFunc("/early", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(103); w.WriteHeader(503) })
	h := s.routeStatsTable().countRoutes(mux, mux)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("wrapper swallowed a handler panic")
			}
		}()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://127.0.0.1/panic", nil))
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://127.0.0.1/early", nil))
	rt := s.routeStatsTable().performance.Load()
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		v, err := rt.Query(context.Background(), 1, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		var completed, failures uint64
		for _, p := range v.RecentMinutes {
			completed += p.Totals.Completed
			failures += p.Totals.Status5xx
		}
		if completed == 2 {
			if failures != 2 {
				t.Fatalf("panic/103 responses classified as success: %d", failures)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("missing completions")
}

func TestAPIPerformanceCountsCancelled500Separately(t *testing.T) {
	s, _, _ := apiPerformanceFixture(t)
	mux := http.NewServeMux()
	ctx, cancel := context.WithCancel(context.Background())
	mux.HandleFunc("/v1/cancel", func(w http.ResponseWriter, r *http.Request) { cancel(); w.WriteHeader(500) })
	h := s.routeStatsTable().countRoutes(mux, mux)
	r := httptest.NewRequest("GET", "http://127.0.0.1/v1/cancel", nil).WithContext(ctx)
	h.ServeHTTP(httptest.NewRecorder(), r)
	rt := s.routeStatsTable().performance.Load()
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		v, err := rt.Query(context.Background(), 1, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range v.RecentMinutes {
			if p.Totals.Completed > 0 {
				if p.Totals.Cancelled != 1 || p.Totals.Status5xx != 0 {
					t.Fatalf("cancelled 500: %+v", p.Totals)
				}
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("completion never reached the worker")
}
