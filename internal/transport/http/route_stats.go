package http

import (
	"context"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/transport/cloud"
)

// Every API call this daemon answers, counted by route and by who asked
// (`/v1/diagnostics.routes`). It exists so a before/after number for a change
// to what the console asks comes from the daemon itself rather than from a
// browser somebody ran by hand.
//
// **A key is a route's shape, never its spelling.** The method, the pattern
// the mux matched, and below a prefix pattern only words this file names: any
// other segment — a session id, a pane, a task, a file name — is `:id`, and
// the query is never read. So nothing a person typed or named reaches the
// table, whatever a later route puts in its path; a word missing from
// routeWords costs precision, not privacy.
//
// Counters run from the daemon's start and are never reset: two readings are
// compared by subtracting them, which a reset would make wrong for every other
// reader.

const (
	// MaxRouteStatKeys is how many distinct route shapes are counted. Past it
	// a new shape is counted in `overflow` instead of a row of its own.
	MaxRouteStatKeys = 512
	// MaxRouteLatencySamples is how many recent durations each route keeps
	// for its percentiles; the oldest is overwritten first.
	MaxRouteLatencySamples = 256
)

// Caller kinds, as `/v1/diagnostics.routes` names them.
const (
	callerDevice    = "device"    // a paired browser or phone
	callerLocal     = "local"     // this machine's own token: the CLI, scripts, the orchestrator
	callerCloud     = "cloud"     // a Cloud viewer, answered in process (cloud.Router)
	callerTask      = "task"      // a child let through by its task secret
	callerAnonymous = "anonymous" // nobody the gate let in: open paths and refusals
)

// routeWords are the path segments a key may spell out below a prefix
// pattern. Everything else is `:id`.
var routeWords = func() map[string]bool {
	words := map[string]bool{}
	for _, w := range strings.Fields(`
		acceptance-revision accepted actions adopt agent agents all app apply archive archived artifacts
		assets assign assistants auth auto-candidates bearings bind board browser busy callbacks cancel
		capacity catalog children claim claude claude-code close closeability cloud codex
		compare-compaction compare-handoff complete completions confirm control convert coordinator
		criteria decisions default-models definition definitions delete deploy detached-tasks detail
		devices devstacks diagnostics diff digests documents durable-reports edit entry events exit export
		fail file files finish focus gate-decision gate-evidence gate-export gate-purge gate-result git
		graphs handoffs head health history human human-interventions icon images inbox inflight info
		input intents interrupt inventory items key keys landing landings language lease leases links
		logout machine machines manifest memory memory-groups messages mirror motion next notes notify
		obligations observed offer open orchestrator outbox pair pairing pairings password paste pauses
		peer persona-suggestion personas phase places preview progress project project-sync projects
		promotions proposals push rebind receipts reclaim reconcile refresh register release remind
		removing renew reopen reports resize restorable restore resume resumed retry revoke
		root-assignments rotate run running runs safe schedule-exports schedule-imports
		schedule-webhooks schedules scopes screen screens seen send session-bindings session-events
		session-name session-snapshots session-todos sessions settings shells skill skill-sources skills
		smart-title snippets squad squad-packages stall start status steps stop stream strings subscribe
		successions task tasks team terminal terminals timeline title todos tracks transcript tree tunnel
		unassign unify unsubscribe update usage v1 v2 verification-note verifications viewer voice waits
		wake whoami work work-gates work-samples work-units worktrees`) {
		words[w] = true
	}
	return words
}()

// routeKey is the shape a request is counted under: `GET /v1/sessions/:id/info`.
// pattern is what the mux matched; "" is a request the mux has no pattern for.
func routeKey(method, pattern, escapedPath string) string {
	method = strings.ToUpper(method)
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodOptions:
	default:
		method = "OTHER"
	}
	// A Go 1.22 pattern may carry a method or host; only its path is a shape.
	if i := strings.IndexByte(pattern, '/'); i >= 0 {
		pattern = pattern[i:]
	} else {
		pattern = ""
	}
	if pattern == "" {
		return method + " (unmatched)"
	}
	if !strings.HasSuffix(pattern, "/") {
		return method + " " + pattern
	}
	rest := strings.TrimPrefix(cleanPath(escapedPath), pattern)
	if rest == cleanPath(escapedPath) && pattern != "/" {
		// The path does not sit under the pattern it matched (a redirect).
		return method + " " + pattern
	}
	var b strings.Builder
	b.WriteString(method + " " + strings.TrimSuffix(pattern, "/"))
	if pattern == "/" && rest == "" {
		b.WriteString("/")
	}
	for _, seg := range strings.Split(rest, "/") {
		if seg == "" {
			continue
		}
		if routeWords[seg] {
			b.WriteString("/" + seg)
		} else {
			b.WriteString("/:id")
		}
	}
	return b.String()
}

// routeStat is one key's counts. Its fields are guarded by routeStats.mu.
type routeStat struct {
	count            int64
	s2, s3, s4, s5   int64
	callers          map[string]int64
	stream           bool
	opens, open      int64
	samples          [MaxRouteLatencySamples]uint32 // microseconds
	sampled, nextIdx int
	maxMicros        uint32
}

type routeStats struct {
	since    time.Time
	mu       sync.Mutex
	routes   map[string]*routeStat
	overflow int64
}

func newRouteStats(now time.Time) *routeStats {
	return &routeStats{since: now, routes: map[string]*routeStat{}}
}

// stat is key's row, or nil when the table is full and key is not in it.
// The caller holds mu.
func (t *routeStats) stat(key string) *routeStat {
	if st, ok := t.routes[key]; ok {
		return st
	}
	if len(t.routes) >= MaxRouteStatKeys {
		return nil
	}
	st := &routeStat{callers: map[string]int64{}}
	t.routes[key] = st
	return st
}

// opened counts a stream as open from the moment it answered as one.
func (t *routeStats) opened(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if st := t.stat(key); st != nil {
		st.stream = true
		st.opens++
		st.open++
	}
}

// done counts one finished request.
func (t *routeStats) done(key, caller string, status int, took time.Duration, wasStream bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.stat(key)
	if st == nil {
		t.overflow++
		return
	}
	st.count++
	switch {
	case status >= 500:
		st.s5++
	case status >= 400:
		st.s4++
	case status >= 300:
		st.s3++
	default:
		st.s2++
	}
	st.callers[caller]++
	if wasStream {
		st.open--
		return
	}
	us := took.Microseconds()
	if us < 0 {
		us = 0
	}
	if us > math.MaxUint32 {
		us = math.MaxUint32
	}
	st.samples[st.nextIdx] = uint32(us)
	st.nextIdx = (st.nextIdx + 1) % MaxRouteLatencySamples
	if st.sampled < MaxRouteLatencySamples {
		st.sampled++
	}
	if uint32(us) > st.maxMicros {
		st.maxMicros = uint32(us)
	}
}

// reading is the table as `/v1/diagnostics.routes` publishes it, sorted by key.
func (t *routeStats) reading() *contract.RouteStats {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := &contract.RouteStats{
		Since:          t.since.Unix(),
		KeyLimit:       MaxRouteStatKeys,
		LatencySamples: MaxRouteLatencySamples,
		Overflow:       t.overflow,
		Routes:         make([]contract.RouteStat, 0, len(t.routes)),
	}
	for key, st := range t.routes {
		method, route, _ := strings.Cut(key, " ")
		row := contract.RouteStat{
			Method: method, Route: route, Count: st.count,
			Status2xx: st.s2, Status3xx: st.s3, Status4xx: st.s4, Status5xx: st.s5,
			Callers: contract.RouteCallers{
				Device: st.callers[callerDevice], Local: st.callers[callerLocal],
				Cloud: st.callers[callerCloud], Task: st.callers[callerTask],
				Anonymous: st.callers[callerAnonymous],
			},
			Stream: st.stream,
		}
		if st.stream {
			row.Opens, row.Open = st.opens, st.open
		} else if st.sampled > 0 {
			sorted := append([]uint32(nil), st.samples[:st.sampled]...)
			sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
			row.LatencyMs = &contract.RouteLatency{
				P50:     millis(percentile(sorted, 50)),
				P90:     millis(percentile(sorted, 90)),
				Max:     millis(st.maxMicros),
				Samples: int64(st.sampled),
			}
		}
		out.Routes = append(out.Routes, row)
	}
	sort.Slice(out.Routes, func(i, j int) bool {
		a, b := out.Routes[i], out.Routes[j]
		if a.Route != b.Route {
			return a.Route < b.Route
		}
		return a.Method < b.Method
	})
	return out
}

// reading for the capacity register: how many keys the table holds.
func (t *routeStats) keys() capacity.Reading {
	t.mu.Lock()
	defer t.mu.Unlock()
	return capacity.Reading{Known: true, Used: int64(len(t.routes))}
}

// percentile is the nearest-rank p-th percentile of sorted, which is not empty.
func percentile(sorted []uint32, p int) uint32 {
	rank := (p*len(sorted) + 99) / 100
	if rank < 1 {
		rank = 1
	}
	return sorted[rank-1]
}

func millis(us uint32) float64 { return math.Round(float64(us)/10) / 100 }

// callerSlot is where the gate's judgement of a request is left for the
// counter outside it, which cannot read the context the gate builds.
type callerSlot struct{ kind string }

type callerSlotKey struct{}

// countRoutes is the outermost layer: it sees every request, the gate's
// refusals included, and keys it by what mux would match.
func (t *routeStats) countRoutes(mux *http.ServeMux, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pattern := mux.Handler(r)
		key := routeKey(r.Method, pattern, r.URL.EscapedPath())
		slot := &callerSlot{kind: callerAnonymous}
		if cloud.ViaCloud(r.Context()) {
			slot.kind = callerCloud
		}
		r = r.WithContext(context.WithValue(r.Context(), callerSlotKey{}, slot))
		cw := &countingWriter{ResponseWriter: w, stats: t, key: key}
		start := time.Now()
		defer func() {
			t.done(key, slot.kind, cw.statusOr(), time.Since(start), cw.stream)
		}()
		if f, ok := w.(http.Flusher); ok {
			next.ServeHTTP(&flushingCountingWriter{countingWriter: cw, flusher: f}, r)
			return
		}
		next.ServeHTTP(cw, r)
	})
}

// noteCaller sits just inside the gate and names who it let in.
func noteCaller(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if slot, ok := r.Context().Value(callerSlotKey{}).(*callerSlot); ok && slot.kind != callerCloud {
			slot.kind = callerKind(r)
		}
		next.ServeHTTP(w, r)
	})
}

// callerKind is who the gate let this request in as. A Cloud request carries
// this machine's own credentials by construction, so it is asked first.
func callerKind(r *http.Request) string {
	if cloud.ViaCloud(r.Context()) {
		return callerCloud
	}
	a := accessOf(r)
	switch {
	case a.machine || (a.verdict.Allowed && a.verdict.Local):
		return callerLocal
	case a.verdict.Allowed:
		return callerDevice
	case taskSecretRoute(r.Method, routePath(r)):
		return callerTask
	}
	return callerAnonymous
}

// countingWriter keeps the status a handler answered with and marks an event
// stream as open the moment its header goes out.
type countingWriter struct {
	http.ResponseWriter
	stats  *routeStats
	key    string
	status int
	stream bool
}

func (c *countingWriter) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
		if status < 300 && strings.HasPrefix(c.Header().Get("Content-Type"), "text/event-stream") {
			c.stream = true
			c.stats.opened(c.key)
		}
	}
	c.ResponseWriter.WriteHeader(status)
}

func (c *countingWriter) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.WriteHeader(http.StatusOK)
	}
	return c.ResponseWriter.Write(b)
}

func (c *countingWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }

func (c *countingWriter) statusOr() int {
	if c.status == 0 {
		return http.StatusOK
	}
	return c.status
}

// flushingCountingWriter is a countingWriter over a writer that can flush. A
// writer that cannot keeps saying so, because the event routes refuse on it.
type flushingCountingWriter struct {
	*countingWriter
	flusher http.Flusher
}

func (f *flushingCountingWriter) Flush() {
	if f.status == 0 {
		f.WriteHeader(http.StatusOK)
	}
	f.flusher.Flush()
}

// routeStatsTable is this server's table, made the first time it is asked for.
func (s *Server) routeStatsTable() *routeStats {
	s.routeStatsOnce.Do(func() { s.routeStats = newRouteStats(time.Now()) })
	return s.routeStats
}
