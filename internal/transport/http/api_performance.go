package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/apiperformance"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/transport/cloud"
)

// StartAPIPerformance starts owner-local observation without blocking startup
// on storage. Nothing in this runtime is handed to health upload or Cloud.
func (s *Server) StartAPIPerformance(ctx context.Context) func() {
	rt := apiperformance.New(apiperformance.Options{Dir: s.cfg.Dir, Build: s.version,
		Limit: func(name string) int64 { return CapacityLimit("diagnostics.api_" + name) }})
	s.routeStatsTable().performance.Store(rt)
	var once sync.Once
	stop := func() {
		once.Do(func() {
			deadline, cancel := context.WithTimeout(context.Background(), apiperformance.MaxQuerySeconds*time.Second)
			defer cancel()
			_ = rt.Close(deadline)
		})
	}
	go func() {
		select {
		case <-ctx.Done():
			stop()
		case <-rt.Done():
		}
	}()
	return stop
}

func (s *Server) apiPerformanceRefusal(w http.ResponseWriter, status int, code, en, zh string) {
	if s.productLanguage() == "zh-Hant" {
		en = zh
	}
	writeRawRefusal(w, status, code, en)
}

func (s *Server) apiPerformanceRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if cloud.ViaCloud(r.Context()) && cloud.CloudViewerOf(r.Context()) == "" {
		s.apiPerformanceRefusal(w, 403, "api_performance_owner_local", "API performance requires a paired reader of this machine.", "API 效能資料僅供這台機器已配對且可讀取的裝置查看。")
		return
	}
	a := accessOf(r)
	if !a.verdict.Allowed || !a.verdict.Caps.Has(auth.Read) {
		s.apiPerformanceRefusal(w, 403, "forbidden", "A paired device with read permission is required.", "需要具有讀取權限的配對裝置。")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		s.apiPerformanceRefusal(w, 405, "bad_request", "API performance is read with GET.", "請使用 GET 讀取 API 效能資料。")
		return
	}
	rt := s.routeStatsTable().performance.Load()
	if rt == nil {
		s.apiPerformanceRefusal(w, 503, "api_performance_unavailable", "API performance monitoring is unavailable in this daemon.", "這個 daemon 目前無法提供 API 效能監測。")
		return
	}
	// Parsing is bounded before url.Values can allocate an arbitrary query.
	if len(r.URL.RawQuery) > apiperformance.MaxRouteBytes {
		s.badAPIPerformanceQuery(w)
		return
	}
	q, err := parseAPIPerformanceQuery(r)
	if err != nil {
		s.badAPIPerformanceQuery(w)
		return
	}
	deadline := time.Now().Add(time.Duration(rt.Limit("query_seconds")) * time.Second)
	if at, ok := r.Context().Deadline(); ok && at.Before(deadline) {
		deadline = at
	}
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(deadline)
	defer controller.SetWriteDeadline(time.Time{})
	v, err := rt.Query(r.Context(), q.hours, q.until)
	if err != nil {
		if errors.Is(err, apiperformance.ErrQuery) {
			s.badAPIPerformanceQuery(w)
			return
		}
		s.apiPerformanceRefusal(w, 503, "api_performance_unavailable", "API performance is temporarily unavailable; retry later.", "API 效能資料暫時無法讀取，請稍後再試。")
		return
	}
	payload, err := json.Marshal(v)
	if err != nil || int64(len(payload)) > rt.Limit("query_bytes") || r.Context().Err() != nil {
		s.apiPerformanceRefusal(w, 503, "api_performance_unavailable", "API performance is temporarily unavailable; retry later.", "API 效能資料暫時無法讀取，請稍後再試。")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	if r.Method == http.MethodGet {
		_, _ = w.Write(payload)
	}
}

type apiPerformanceQuery struct {
	hours int
	until time.Time
}

func parseAPIPerformanceQuery(r *http.Request) (apiPerformanceQuery, error) {
	out := apiPerformanceQuery{hours: 1}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return out, err
	}
	for k, v := range values {
		if (k != "hours" && k != "until") || len(v) != 1 {
			return out, apiperformance.ErrQuery
		}
	}
	if v := values.Get("hours"); v != "" {
		out.hours, err = strconv.Atoi(v)
		if err != nil {
			return out, err
		}
	}
	if out.hours != 1 && out.hours != 6 && out.hours != 24 && out.hours != 168 {
		return out, apiperformance.ErrQuery
	}
	if v := values.Get("until"); v != "" {
		n, e := strconv.ParseInt(v, 10, 64)
		if e != nil || n < 0 {
			return out, apiperformance.ErrQuery
		}
		out.until = time.UnixMilli(n)
	}
	return out, nil
}

func (s *Server) badAPIPerformanceQuery(w http.ResponseWriter) {
	s.apiPerformanceRefusal(w, 400, "api_performance_bad_query", "Use hours=1, 6, 24 or 168 and an optional recent Unix millisecond until value.", "請使用 hours=1、6、24 或 168；until 可選填最近七天內的 Unix 毫秒時間。")
}
