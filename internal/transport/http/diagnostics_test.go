package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/artifacts"
	"github.com/sainteye/clawdline-go/internal/adapters/logs"
	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/transcript"
	"github.com/sainteye/clawdline-go/internal/app"
	"github.com/sainteye/clawdline-go/internal/config"
	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/capacity"
)

func capacityServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv("CLAWDLINE_SWIFT_DIR", filepath.Join(t.TempDir(), "swift"))
	dir := filepath.Join(t.TempDir(), "next")
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	// What `clawdline serve` builds that the register measures: the log moved
	// into the directory, and the inventory's and usage route's caches.
	w, err := logs.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close(); daemonLogs.Delete(dir) })
	SetDaemonLog(dir, w)
	return &Server{cfg: config.Config{Dir: dir}, store: st, ledger: transcript.NewLedger(),
		inventory: app.Inventory{Identity: transcript.NewHost()},
		screenBus: newScreenBus(),
		pictures:  pictures{store: artifacts.NewStore(dir), drops: artifacts.NewDrops(dir)}}
}

// The register guard's other half (limits §4.1): every registered row has a
// measurement wired, and on a fresh directory every one of them can be read.
func TestEveryRegisteredRowIsMeasured(t *testing.T) {
	s := capacityServer(t)
	measures := s.capacityMeasures()
	for _, e := range capacity.Register() {
		m := measures[e.Name]
		if m == nil {
			t.Errorf("%s is registered and nothing measures it", e.Name)
			continue
		}
		r := m()
		if !r.Known {
			t.Errorf("%s could not be read on a fresh directory: %s", e.Name, r.Err)
		}
		if e.Name == capacity.StoreDB && (r.Used == 0 || !r.HasDiskFree) {
			t.Errorf("store.db on an open store: %+v", r)
		}
	}
	if len(measures) != len(capacity.Register()) {
		t.Errorf("%d measures for %d rows", len(measures), len(capacity.Register()))
	}
}

// fakeBeat installs a beat over rows the test chooses, measured by readings the
// test chooses, and runs one pass of it.
func fakeBeat(t *testing.T, s *Server, rows map[string]capacity.Reading, limits map[string]int64) *capacityBeat {
	t.Helper()
	var resolved []capacity.Resolved
	measures := map[string]func() capacity.Reading{}
	for _, e := range capacity.Register() {
		r, ok := rows[e.Name]
		if !ok {
			r = capacity.Reading{Known: true}
		}
		limit := e.Limit
		if l, ok := limits[e.Name]; ok {
			limit = l
		}
		resolved = append(resolved, capacity.Resolved{Entry: e, Limit: limit, Overridden: limit != e.Limit})
		measures[e.Name] = func() capacity.Reading { return r }
	}
	b := &capacityBeat{resolved: resolved, tracker: capacity.NewTracker(), measure: measures,
		record: s.store.Append, since: time.Now(), started: time.Now(), tick: time.Minute}
	capacityByServer.Store(s, b)
	t.Cleanup(func() { capacityByServer.Delete(s) })
	b.pass(context.Background())
	return b
}

func healthOf(t *testing.T, s *Server) contract.Health {
	t.Helper()
	rec := httptest.NewRecorder()
	s.health(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	var h contract.Health
	if err := json.Unmarshal(rec.Body.Bytes(), &h); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Body.String(), "store.db") || strings.Contains(rec.Body.String(), "board.receipts") {
		t.Fatalf("the open route named a row: %s", rec.Body)
	}
	return h
}

func entry(t *testing.T, d contract.CapacityDiagnostics, name string) contract.CapacityEntry {
	t.Helper()
	for _, e := range d.Entries {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("no %s in %+v", name, d.Entries)
	return contract.CapacityEntry{}
}

// Full is not one behaviour. Evidence that is full turns the open health route
// red; a full idempotency ledger and a full request queue are seen in
// diagnostics and heard as notices, and health stays ok; a security audit at
// its segment size rotates and is not an alarm unless it cannot be written.
func TestOnlyExhaustedEvidenceTurnsHealthRed(t *testing.T) {
	cases := []struct {
		name    string
		rows    map[string]capacity.Reading
		limits  map[string]int64
		ok      bool
		reason  contract.HealthReason
		full    string
		evicted int64
	}{
		{name: "nothing full", ok: true},
		{
			name:   "store.db full",
			rows:   map[string]capacity.Reading{capacity.StoreDB: {Known: true, Used: 300 << 10}},
			limits: map[string]int64{capacity.StoreDB: 200 << 10},
			ok:     false, reason: contract.HealthReasonCapacityExhausted, full: capacity.StoreDB,
		},
		{
			name:   "board.receipts full and evicting",
			rows:   map[string]capacity.Reading{capacity.BoardReceipts: {Known: true, Used: 4, Counters: capacity.Counters{Evicted: 3}, DurableCounters: true}},
			limits: map[string]int64{capacity.BoardReceipts: 4},
			ok:     true, full: capacity.BoardReceipts, evicted: 3,
		},
		{
			name:   "cloud.relay_queue full",
			rows:   map[string]capacity.Reading{capacity.CloudRelayQueue: {Known: true, Used: 8, Counters: capacity.Counters{Dropped: 2}}},
			limits: map[string]int64{capacity.CloudRelayQueue: 8},
			ok:     true, full: capacity.CloudRelayQueue,
		},
		{
			name:   "audit.security at its segment size",
			rows:   map[string]capacity.Reading{capacity.AuditSecurity: {Known: true, Used: 5000}},
			limits: map[string]int64{capacity.AuditSecurity: 4096},
			ok:     true, full: capacity.AuditSecurity,
		},
		{
			name: "audit.security cannot be written",
			rows: map[string]capacity.Reading{capacity.AuditSecurity: {Known: true, Used: 10, Failing: true,
				Note: "the last audit line was not written: disk full"}},
			ok: false, reason: contract.HealthReasonCapacityExhausted,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := capacityServer(t)
			fakeBeat(t, s, c.rows, c.limits)
			h := healthOf(t, s)
			if h.OK != c.ok || h.Reason != c.reason {
				t.Fatalf("health %+v, want ok=%v reason=%q", h, c.ok, c.reason)
			}
			d, ok := s.capacityDiagnostics()
			if ok != c.ok {
				t.Fatalf("diagnostics says ok=%v, health %v", ok, h.OK)
			}
			if c.full != "" {
				e := entry(t, d, c.full)
				if e.State != contract.CapacityStateFull || e.Used == nil || *e.Used < e.Limit {
					t.Fatalf("%s: %+v", c.full, e)
				}
				if e.Exhausted == c.ok {
					t.Fatalf("%s exhausted=%v with health ok=%v", c.full, e.Exhausted, c.ok)
				}
				if e.Evicted != c.evicted {
					t.Fatalf("%s evicted %d", c.full, e.Evicted)
				}
				if e.Notices != 1 {
					t.Fatalf("%s: %d notices on becoming full", c.full, e.Notices)
				}
			}
		})
	}
}

// Every row on the wire answers the four questions, and a row that could not
// be read says null, not zero.
func TestTheWireCarriesTheFourCellsAndUnknownIsNull(t *testing.T) {
	s := capacityServer(t)
	fakeBeat(t, s, map[string]capacity.Reading{capacity.StoreDB: capacity.Unmeasured("stat: permission denied")}, nil)
	d, _ := s.capacityDiagnostics()
	if len(d.Entries) != len(capacity.Register()) || !d.Beat.Running || d.Beat.Passes != 1 || d.Beat.At == 0 {
		t.Fatalf("%+v", d)
	}
	body, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	for _, e := range wire.Entries {
		for _, cell := range []string{"limit", "unit", "at_limit", "told", "evicted_by", "state", "used", "ratio"} {
			if _, ok := e[cell]; !ok {
				t.Errorf("%v has no %s", e["name"], cell)
			}
		}
		if told, _ := e["told"].([]any); len(told) == 0 {
			t.Errorf("%v tells nobody", e["name"])
		}
		if e["name"] == capacity.StoreDB {
			if e["used"] != nil || e["ratio"] != nil || e["state"] != "unknown" || e["error"] == "" {
				t.Errorf("an unreadable store.db on the wire: %v", e)
			}
		} else if e["used"] == nil {
			t.Errorf("%v: a known reading without used", e["name"])
		}
	}
}

// The beat is watched the way the broker's is: no finished pass in three
// ticks is ok:false on the open route, and before the beat was started every
// row is unknown rather than zero.
func TestAStoppedCapacityBeatTurnsHealthRed(t *testing.T) {
	s := capacityServer(t)
	b := fakeBeat(t, s, nil, nil)
	if h := healthOf(t, s); !h.OK {
		t.Fatalf("a beat that just passed: %+v", h)
	}
	b.mu.Lock()
	b.at = time.Now().Add(-3*b.tick - time.Second)
	b.mu.Unlock()
	if h := healthOf(t, s); h.OK || h.Reason != contract.HealthReasonCapacityBeatStalled {
		t.Fatalf("a beat quiet for three ticks: %+v", h)
	}

	idle := capacityServer(t)
	d, ok := idle.capacityDiagnostics()
	if !ok || d.Beat.Running || len(d.Entries) != len(capacity.Register()) {
		t.Fatalf("a daemon that never started the beat: %+v", d)
	}
	for _, e := range d.Entries {
		if e.State != contract.CapacityStateUnknown || e.Used != nil || e.Error == "" {
			t.Fatalf("before any pass: %+v", e)
		}
	}
	if h := healthOf(t, idle); !h.OK {
		t.Fatalf("a beat never started is absent, not stalled: %+v", h)
	}
}

// What a pass saw is in the store: the transition, the notice and the batch
// of evictions, one event each.
func TestAPassRecordsItsEvents(t *testing.T) {
	s := capacityServer(t)
	before, _, _, err := s.store.Counts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b := fakeBeat(t, s, map[string]capacity.Reading{capacity.StoreDB: {Known: true, Used: 10}}, map[string]int64{capacity.StoreDB: 10})
	after, _, _, _ := s.store.Counts(context.Background())
	// store.db first seen full: one state event and one notice.
	if after-before != 2 {
		t.Fatalf("%d events for a row first seen full", after-before)
	}
	b.pass(context.Background())
	again, _, _, _ := s.store.Counts(context.Background())
	if again != after {
		t.Fatalf("a pass that saw nothing new wrote %d events", again-after)
	}
}
