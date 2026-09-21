package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// pushes is a push service that records what it was handed.
type pushes struct {
	mu     sync.Mutex
	calls  []string // tag, title
	answer func() (int, int, error)
}

func (p *pushes) push(_ context.Context, title, body, terminal, tag string) (int, int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, tag+" "+title)
	if p.answer != nil {
		return p.answer()
	}
	return 1, 0, nil
}

func (p *pushes) sent() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.calls...)
}

// pushBeat is fakeBeat with the push path wired: the store's outbox, a broker
// that runs it against p, and the read-back of the last pushes. readings is
// read on every pass, so a test moves a row by writing to it. seed=false leaves
// the read-back out, for the control that shows what it holds back.
func pushBeat(t *testing.T, s *Server, readings map[string]capacity.Reading, limits map[string]int64,
	p *pushes, seed bool) *capacityBeat {
	t.Helper()
	var resolved []capacity.Resolved
	measures := map[string]func() capacity.Reading{}
	for _, e := range capacity.Register() {
		name := e.Name
		limit := e.Limit
		if l, ok := limits[name]; ok {
			limit = l
		}
		resolved = append(resolved, capacity.Resolved{Entry: e, Limit: limit, Overridden: limit != e.Limit})
		measures[name] = func() capacity.Reading {
			if r, ok := readings[name]; ok {
				return r
			}
			return capacity.Reading{Known: true}
		}
	}
	broker := &orchestrator.Broker{Store: s.store, Push: p.push}
	b := &capacityBeat{resolved: resolved, tracker: capacity.NewTracker(), measure: measures,
		record: s.store.Append, since: time.Now(), started: time.Now(), tick: time.Minute,
		intent: s.store.RecordIntent, run: broker.RunEffects, loc: time.UTC}
	if seed {
		b.latest = func(ctx context.Context) ([]store.Effect, error) {
			return s.store.LatestEffects(ctx, orchestrator.EffectCapacityPush)
		}
	}
	capacityByServer.Store(s, b)
	t.Cleanup(func() { capacityByServer.Delete(s) })
	return b
}

func passAndSend(b *capacityBeat) {
	b.pass(context.Background())
	b.sending.Wait()
}

func eventCount(t *testing.T, s *Server, kind string) int {
	t.Helper()
	n, err := s.store.EventCount(context.Background(), kind)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// limits §4.7, layer 4 in small: a row driven to full produces one notice,
// and that notice is recorded with the push it owes in one commit, sent after
// it, and finished with how it ended — on the wire as last_push, and in the
// store as one `capacity.notify` and one `capacity.notify.pushed`. A pass that
// sees nothing new sends nothing.
func TestANoticeOwesOnePushSentAfterItsCommit(t *testing.T) {
	s := capacityServer(t)
	p := &pushes{}
	b := pushBeat(t, s, map[string]capacity.Reading{capacity.StoreDB: {Known: true, Used: 10}},
		map[string]int64{capacity.StoreDB: 10}, p, true)
	passAndSend(b)

	got := p.sent()
	if len(got) != 1 || !strings.HasPrefix(got[0], "capacity-store.db 容量已滿：store.db") {
		t.Fatalf("pushed %q", got)
	}
	if eventCount(t, s, capacity.EventNotify) != 1 || eventCount(t, s, "capacity.notify.pushed") != 1 {
		t.Fatalf("notify %d, pushed %d", eventCount(t, s, capacity.EventNotify), eventCount(t, s, "capacity.notify.pushed"))
	}
	effects, err := s.store.Effects(context.Background(), orchestrator.EffectCapacityPush, capacity.StoreDB)
	if err != nil || len(effects) != 1 || effects[0].State != store.EffectDone || effects[0].Outcome != "pushed" {
		t.Fatalf("the outbox: %+v %v", effects, err)
	}

	// The wire reads the push back once it has finished.
	b.pass(context.Background())
	d, _ := s.capacityDiagnostics()
	e := entry(t, d, capacity.StoreDB)
	if e.LastPush == nil || e.LastPush.Push != contract.CapacityPushStatePushed ||
		e.LastPush.State != contract.CapacityStateFull || e.LastPush.FinishedAt == 0 || e.LastNoticeAt == 0 {
		t.Fatalf("store.db on the wire: %+v last_push %+v", e, e.LastPush)
	}
	if !e.Exhausted || healthOf(t, s).Reason != contract.HealthReasonCapacityExhausted {
		t.Fatalf("store.db full and health not capacity_exhausted: %+v", e)
	}
	if len(p.sent()) != 1 {
		t.Fatalf("a pass that saw nothing new pushed: %q", p.sent())
	}
}

// A row whose register line answers the sender — a lease queue — writes its
// notice down and pushes nothing: the one person a push reaches cannot do
// anything about it.
func TestOnlyARowThatTellsAPersonPushes(t *testing.T) {
	s := capacityServer(t)
	p := &pushes{}
	b := pushBeat(t, s, map[string]capacity.Reading{capacity.LeasesQueue: {Known: true, Used: 4}},
		map[string]int64{capacity.LeasesQueue: 4}, p, true)
	passAndSend(b)
	if len(p.sent()) != 0 {
		t.Fatalf("pushed %q", p.sent())
	}
	if eventCount(t, s, capacity.EventNotify) != 1 {
		t.Fatal("the notice was not written down")
	}
	if effects, _ := s.store.Effects(context.Background(), orchestrator.EffectCapacityPush, capacity.LeasesQueue); len(effects) != 0 {
		t.Fatalf("a push was owed: %+v", effects)
	}
	d, _ := s.capacityDiagnostics()
	if e := entry(t, d, capacity.LeasesQueue); e.LastPush != nil || e.LastNoticeAt == 0 {
		t.Fatalf("leases.queue on the wire: %+v", e)
	}
}

// A daemon that restarts with a row still full does not push again the same
// day: the new beat reads what the last one pushed and holds the notice back,
// and says it held one. The control is the same restart without the read-back,
// which pushes a second time — the read-back is what holds it.
func TestARestartDoesNotSpendTheDailyPushAgain(t *testing.T) {
	s := capacityServer(t)
	p := &pushes{}
	full := map[string]capacity.Reading{capacity.StoreDB: {Known: true, Used: 10}}
	limits := map[string]int64{capacity.StoreDB: 10}
	passAndSend(pushBeat(t, s, full, limits, p, true))
	first, _ := s.capacityDiagnostics()
	pushedAt := entry(t, first, capacity.StoreDB).LastNoticeAt

	restarted := pushBeat(t, s, full, limits, p, true)
	passAndSend(restarted)
	if len(p.sent()) != 1 {
		t.Fatalf("the restart pushed again: %q", p.sent())
	}
	d, _ := s.capacityDiagnostics()
	e := entry(t, d, capacity.StoreDB)
	// The earlier notice's time as the outbox kept it, to the second it was
	// committed in.
	if e.NoticesSuppressed != 1 || e.LastNoticeAt < pushedAt || e.LastNoticeAt > pushedAt+1 || e.LastPush == nil ||
		e.LastPush.Push != contract.CapacityPushStatePushed {
		t.Fatalf("after the restart: %+v last_push %+v", e, e.LastPush)
	}

	control := pushBeat(t, s, full, limits, p, false)
	passAndSend(control)
	if len(p.sent()) != 2 {
		t.Fatalf("the control without the read-back pushed %d in all; the test cannot see the seed", len(p.sent()))
	}
}

// Nothing is sent for a notice the store would not take: a push with no
// record is one a restart would send again and nobody could account for. The
// failure is counted where the beat's other write failures are.
func TestANoticeTheStoreRefusedIsNotPushed(t *testing.T) {
	s := capacityServer(t)
	p := &pushes{}
	b := pushBeat(t, s, map[string]capacity.Reading{capacity.StoreDB: {Known: true, Used: 10}},
		map[string]int64{capacity.StoreDB: 10}, p, true)
	b.intent = func(context.Context, []store.Event, []store.Effect) ([]int64, error) {
		return nil, errors.New("database or disk is full")
	}
	passAndSend(b)
	if len(p.sent()) != 0 {
		t.Fatalf("pushed %q for a notice with no record", p.sent())
	}
	d, _ := s.capacityDiagnostics()
	if d.Beat.EventErrors != 1 || !strings.Contains(d.Beat.LastEventError, "disk is full") {
		t.Fatalf("beat: %+v", d.Beat)
	}
}

// A push no service accepted is finished failed and said so, and it is not
// retried on the next pass: the notice was about a crossing, and there has
// not been another one.
func TestAPushNobodyAcceptedIsFailedAndNotRetried(t *testing.T) {
	s := capacityServer(t)
	p := &pushes{answer: func() (int, int, error) { return 0, 1, nil }}
	b := pushBeat(t, s, map[string]capacity.Reading{capacity.AuditSecurity: {Known: true, Used: 99}},
		map[string]int64{capacity.AuditSecurity: 100}, p, true)
	passAndSend(b)
	passAndSend(b)
	if len(p.sent()) != 1 {
		t.Fatalf("pushed %q", p.sent())
	}
	d, _ := s.capacityDiagnostics()
	e := entry(t, d, capacity.AuditSecurity)
	if e.LastPush == nil || e.LastPush.Push != contract.CapacityPushStateFailed ||
		e.LastPush.State != contract.CapacityStateCritical || !strings.Contains(e.LastPush.Detail, "no push service") {
		t.Fatalf("last_push %+v", e.LastPush)
	}
}

// The budget is the register's own. An hour in which the agents have spent
// all thirty of theirs does not silence a full store, and a capacity push is
// not one of the agents' thirty.
func TestTheAgentsSpentHourDoesNotSilenceCapacity(t *testing.T) {
	s := capacityServer(t)
	ctx := context.Background()
	for i := 0; i < 30; i++ {
		if _, _, err := s.store.RecordNotification(ctx, "some-task", "t", "b"); err != nil {
			t.Fatal(err)
		}
	}
	p := &pushes{}
	passAndSend(pushBeat(t, s, map[string]capacity.Reading{capacity.StoreDB: {Known: true, Used: 10}},
		map[string]int64{capacity.StoreDB: 10}, p, true))
	if len(p.sent()) != 1 {
		t.Fatalf("pushed %q with the agents' hour spent", p.sent())
	}
	if _, perHour, _ := s.store.NotificationCounts(ctx, "some-task"); perHour != 30 {
		t.Fatalf("the agents' hour counts %d; a capacity push was charged to it", perHour)
	}
}

// The panel is a person's screen, and a person reads it from a browser that is
// a paired device: /v1/capacity answers a read-only device where
// /v1/diagnostics refuses the same device, and it says nothing about where this
// daemon keeps its state. Without a token it is refused like any other read.
func TestAPairedDeviceReadsTheCapacityPanel(t *testing.T) {
	f, _ := newGateFixture(t)
	s := capacityServer(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/capacity", s.capacityRoute)
	mux.HandleFunc("/v1/diagnostics", s.diagnostics)
	h := f.g.wrap(mux)
	fakeBeat(t, s, map[string]capacity.Reading{capacity.StoreDB: {Known: true, Used: 10}},
		map[string]int64{capacity.StoreDB: 10})

	reader := map[string]string{"Authorization": "Bearer " + f.read}
	rec := call{method: http.MethodGet, path: "/v1/capacity", headers: reader}.do(h)
	var panel contract.CapacityPanel
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &panel) != nil {
		t.Fatalf("a read-only device: %d %s", rec.Code, rec.Body)
	}
	if e := entry(t, panel.Capacity, capacity.StoreDB); e.State != contract.CapacityStateFull || !e.Exhausted {
		t.Fatalf("store.db on the panel: %+v", e)
	}
	if panel.Completions == nil || panel.CompletionsError != "" {
		t.Fatalf("the completion notices: %+v %q", panel.Completions, panel.CompletionsError)
	}
	if strings.Contains(rec.Body.String(), s.cfg.Dir) || strings.Contains(rec.Body.String(), `"dir"`) {
		t.Fatalf("the panel named this disk: %s", rec.Body)
	}
	if rec := (call{method: http.MethodGet, path: "/v1/diagnostics", headers: reader}).do(h); rec.Code != http.StatusForbidden {
		t.Fatalf("the same device read /v1/diagnostics: %d", rec.Code)
	}
	if rec := (call{method: http.MethodGet, path: "/v1/capacity"}).do(h); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d %s", rec.Code, rec.Body)
	}
}
