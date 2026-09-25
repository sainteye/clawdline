package http

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// usageFixture is a server over a store seeded with ledger rows, behind the
// real gate, and a home directory the ledger looks for transcripts in.
type usageFixture struct {
	t    *testing.T
	s    *Server
	f    *gateFixture
	h    http.Handler
	home string
	at   time.Time
}

func newUsageFixture(t *testing.T) *usageFixture {
	t.Helper()
	f, _ := newGateFixture(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := &Server{cfg: config.Config{Dir: t.TempDir(), Port: 7757}, store: st}
	home := t.TempDir()
	usageByServer.Store(s, app.NewUsageLedger(st, home))
	t.Cleanup(func() { usageByServer.Delete(s) })
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/usage/", s.usageRoute)
	mux.HandleFunc("/v1/diagnostics", s.diagnostics)
	return &usageFixture{t: t, s: s, f: f, h: f.g.wrap(mux), home: home, at: time.Unix(1_790_000_000, 0)}
}

// row stores one transcript's reading: spent is cost by category (every
// category a token count of ten times its cost), state the reader's.
func (u *usageFixture) row(r store.UsageRow, spent map[string]float64, state string) {
	u.t.Helper()
	cats := map[string]map[string]float64{}
	var measured float64
	for c, cost := range spent {
		cats[c] = map[string]float64{"input": cost * 4, "cache_read": cost * 5, "output": cost, "cost": cost}
		measured += cost * 10
	}
	r.Spent, _ = json.Marshal(cats)
	r.Measured, _ = json.Marshal(map[string]float64{"input": measured * 0.4, "cache_read": measured * 0.5, "output": measured * 0.1})
	if state == "" {
		state = "{}"
	}
	r.State = json.RawMessage(state)
	if r.Path == "" {
		r.Path = "/nowhere/" + r.Conversation + ".jsonl"
	}
	if err := u.s.store.SaveUsageRow(context.Background(), r); err != nil {
		u.t.Fatal(err)
	}
}

func (u *usageFixture) get(path string, headers map[string]string) (int, []byte) {
	u.t.Helper()
	if headers == nil {
		headers = map[string]string{machineHeader: u.f.machine}
	}
	rec := call{method: http.MethodGet, path: path, headers: headers}.do(u.h)
	return rec.Code, rec.Body.Bytes()
}

// seed is the ledger every route test reads: a session read and dispatched as
// a child, its subagent, a session whose transcript is gone, a task only the
// broker knows, a transcript on disk the ledger has not reached, and a Board
// item the first session owns.
func (u *usageFixture) seed() {
	u.t.Helper()
	u.row(store.UsageRow{Assistant: "claude", Conversation: "sess-read", TaskID: "task-1", ReadAt: u.at, OpeningRead: true},
		map[string]float64{"impl": 6, "rules": 2, "protocol": 1},
		`{"calls":12,"peak_context":150000,"compactions":1,"calls_above":2,"above":{"input":10,"cost":0.5}}`)
	u.row(store.UsageRow{Assistant: "claude", Conversation: "agent-1", Parent: "sess-read", ReadAt: u.at},
		map[string]float64{"impl": 1}, `{"calls":3,"peak_context":9000}`)
	u.row(store.UsageRow{Assistant: "claude", Conversation: "sess-gone", ReadAt: u.at, Reason: store.UsageTranscriptMissing},
		map[string]float64{"talk": 4}, `{"calls":5,"peak_context":20000}`)
	task := store.BrokerRow{ID: "task-2", Project: "/p", Assistant: "claude", State: "running", CreatedAt: u.at,
		UpdatedAt: u.at, SecretHash: "h", Record: json.RawMessage(`{"id":"task-2","root":{"session_id":"sess-read"}}`)}
	if err := u.s.store.SaveBrokerTask(context.Background(), task, nil); err != nil {
		u.t.Fatal(err)
	}
	dir := filepath.Join(u.home, ".claude", "projects", "-p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		u.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sess-new.jsonl"), []byte("{}\n"), 0o644); err != nil {
		u.t.Fatal(err)
	}
	item := work.ItemV2{ID: "10000000-0000-4000-8000-000000000001", ProjectID: "p", ProjectPath: "/p",
		Kind: work.KindIssue, Title: "Fix it", Description: "Fix it", Phase: work.PhaseImplementing,
		DeploymentPolicy: work.DeployAgentDecides, OwnerSession: "sess-read", CreatedBy: "local",
		CreatedAt: u.at.Add(-time.Hour), UpdatedAt: u.at, Cycle: 1, Version: 1}
	if err := u.s.store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		return tx.CreateItem(item, "local", `{}`)
	}); err != nil {
		u.t.Fatal(err)
	}
}

func category(t *testing.T, bill contract.UsageBill, name contract.UsageCategoryName) contract.UsageCategory {
	t.Helper()
	for _, c := range bill.Categories {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("the bill has no %s: %+v", name, bill)
	return contract.UsageCategory{}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// A session read: every category in the ledger's order, its subagent folded
// into delegate, shares of the cost that sum to one, rules marked as an upper
// bound, and the session's own calls, peak, compactions and calls above 200k.
func TestUsageSessionAnswersItsBill(t *testing.T) {
	u := newUsageFixture(t)
	u.seed()
	code, body := u.get("/v1/usage/sessions/sess-read", nil)
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, body)
	}
	var got contract.UsageSession
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Reason != "" || got.ReadAt != u.at.Unix() || got.TaskID != "task-1" || got.Assistant != "claude" {
		t.Fatalf("session: %+v", got)
	}
	if got.Calls != 12 || got.PeakContext != 150000 || got.Compactions != 1 || got.CallsAbove != 2 || !near(got.Above.Cost, 0.5) {
		t.Fatalf("long-session numbers: %+v", got)
	}
	if len(got.Bill.Categories) != 9 || got.Bill.Categories[0].Name != contract.UsageCategoryNameBoard {
		t.Fatalf("categories: %+v", got.Bill.Categories)
	}
	if got.Bill.ShareOf != contract.UsageShareOfCost || !near(got.Bill.Total.Cost, 10) || !got.Bill.Total.CostKnown {
		t.Fatalf("total: %+v", got.Bill)
	}
	sum := 0.0
	for _, c := range got.Bill.Categories {
		sum += c.Share
		if c.UpperBound != (c.Name == contract.UsageCategoryNameRules) {
			t.Errorf("%s upper_bound = %v", c.Name, c.UpperBound)
		}
	}
	if !near(sum, 1) {
		t.Fatalf("shares sum to %v", sum)
	}
	impl, rules, delegate := category(t, got.Bill, "impl"), category(t, got.Bill, "rules"), category(t, got.Bill, "delegate")
	if !near(impl.Share, 0.6) || !near(rules.Share, 0.2) || !near(delegate.Share, 0.1) || !near(impl.Tokens.CacheRead, 30) {
		t.Fatalf("impl %+v rules %+v delegate %+v", impl, rules, delegate)
	}
	if len(got.Subagents) != 1 || got.Subagents[0].Conversation != "agent-1" || got.Subagents[0].Calls != 3 {
		t.Fatalf("subagents: %+v", got.Subagents)
	}
	if len(got.Gaps) != 0 || got.Composition != nil {
		t.Fatalf("gaps %+v composition %+v", got.Gaps, got.Composition)
	}
}

// A session with no current reading answers why, never an empty total: gone
// keeps its last reading and says so; one whose transcript is on disk and not
// yet read says not_yet_read.
func TestUsageSessionSaysWhyItHasNoCurrentReading(t *testing.T) {
	u := newUsageFixture(t)
	u.seed()
	for _, tc := range []struct {
		id     string
		reason contract.UsageReason
		cost   float64
	}{
		{"sess-gone", contract.UsageReasonTranscriptMissing, 4},
		{"sess-new", contract.UsageReasonNotYetRead, 0},
	} {
		code, body := u.get("/v1/usage/sessions/"+tc.id, nil)
		var got contract.UsageSession
		if code != http.StatusOK || json.Unmarshal(body, &got) != nil {
			t.Fatalf("%s: %d %s", tc.id, code, body)
		}
		if got.Reason != tc.reason || !near(got.Bill.Total.Cost, tc.cost) {
			t.Errorf("%s: reason %q cost %v", tc.id, got.Reason, got.Bill.Total.Cost)
		}
		if len(got.Gaps) != 1 || got.Gaps[0].Reason != tc.reason || got.Gaps[0].Counted != (tc.cost > 0) {
			t.Errorf("%s: gaps %+v", tc.id, got.Gaps)
		}
	}
}

// A task is every session that names it; a task only the broker knows is
// not_yet_read. An item is its owner's bill plus what it dispatched.
func TestUsageTaskAndItemAnswerTheirBills(t *testing.T) {
	u := newUsageFixture(t)
	u.seed()
	code, body := u.get("/v1/usage/tasks/task-1", nil)
	var task contract.UsageTask
	if code != http.StatusOK || json.Unmarshal(body, &task) != nil {
		t.Fatalf("task-1: %d %s", code, body)
	}
	if task.Reason != "" || len(task.Sessions) != 1 || task.Calls != 12 || task.PeakContext != 150000 || !near(task.Bill.Total.Cost, 10) {
		t.Fatalf("task-1: %+v", task)
	}
	code, body = u.get("/v1/usage/tasks/task-2", nil)
	var unread contract.UsageTask
	if code != http.StatusOK || json.Unmarshal(body, &unread) != nil {
		t.Fatalf("task-2: %d %s", code, body)
	}
	if unread.Reason != contract.UsageReasonNotYetRead || len(unread.Gaps) != 1 || unread.Gaps[0].Kind != "task" {
		t.Fatalf("task-2: %+v", unread)
	}
	code, body = u.get("/v1/usage/items/10000000-0000-4000-8000-000000000001", nil)
	var item contract.UsageItem
	if code != http.StatusOK || json.Unmarshal(body, &item) != nil {
		t.Fatalf("item: %d %s", code, body)
	}
	if len(item.Owners) != 1 || item.Owners[0].Session != "sess-read" || len(item.Sessions) != 1 ||
		len(item.Tasks) != 1 || item.Tasks[0].TaskID != "task-2" || !near(item.Bill.Total.Cost, 10) || item.Calls != 12 {
		t.Fatalf("item: %+v", item)
	}
}

// An id nobody knows is a typed 404; one that cannot be an id is a 400; a
// subagent asked for as a session names its session.
func TestUsageRefusesWhatItDoesNotKnow(t *testing.T) {
	u := newUsageFixture(t)
	u.seed()
	for _, tc := range []struct {
		path string
		want int
		code string
	}{
		{"/v1/usage/sessions/sess-none", http.StatusNotFound, "unknown_session"},
		{"/v1/usage/sessions/agent-1", http.StatusNotFound, "unknown_session"},
		{"/v1/usage/tasks/task-9", http.StatusNotFound, "unknown_task"},
		{"/v1/usage/items/10000000-0000-4000-8000-00000000ffff", http.StatusNotFound, "unknown_item"},
		{"/v1/usage/sessions/a*b", http.StatusBadRequest, "bad_request"},
		{"/v1/usage/projects/p", http.StatusNotFound, "not_found"},
	} {
		code, body := u.get(tc.path, nil)
		var r contract.Refusal
		if code != tc.want || json.Unmarshal(body, &r) != nil || r.Error != tc.code {
			t.Errorf("%s: %d %s", tc.path, code, body)
		}
	}
}

// Read like the daemon's other machine-read routes: a paired device or this
// machine's orchestrator token, and nobody else.
func TestUsageIsReadWithADeviceOrTheOrchestratorToken(t *testing.T) {
	u := newUsageFixture(t)
	u.seed()
	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"no token", map[string]string{}, http.StatusUnauthorized},
		{"a wrong orchestrator token", map[string]string{machineHeader: "not-it"}, http.StatusUnauthorized},
		{"a paired device", map[string]string{"Authorization": "Bearer " + u.f.read}, http.StatusOK},
		{"the orchestrator token", map[string]string{machineHeader: u.f.machine}, http.StatusOK},
	} {
		if code, body := u.get("/v1/usage/sessions/sess-read", tc.headers); code != tc.want {
			t.Errorf("%s: %d %s", tc.name, code, body)
		}
	}
}

// The reading loop reports itself in /v1/diagnostics: fresh after a pass,
// stalled once no pass has ended for three intervals.
func TestDiagnosticsSayWhetherTheLedgerIsStalled(t *testing.T) {
	u := newUsageFixture(t)
	var now atomic.Int64
	now.Store(u.at.UnixNano())
	ledger := app.NewUsageLedger(u.s.store, u.home)
	ledger.Now = func() time.Time { return time.Unix(0, now.Load()) }
	ledger.Log = func(string, ...any) {}
	usageByServer.Store(u.s, ledger)

	read := func() contract.UsageDiagnostics {
		t.Helper()
		rec := call{method: http.MethodGet, path: "/v1/diagnostics",
			headers: map[string]string{"Authorization": "Bearer " + u.f.local}}.do(u.h)
		var d contract.Diagnostics
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &d) != nil || d.Usage == nil {
			t.Fatalf("diagnostics: %d %s", rec.Code, rec.Body)
		}
		return *d.Usage
	}
	if d := read(); d.Running || d.Stalled || d.At != 0 {
		t.Fatalf("before Run: %+v", d)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ledger.Run(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for ledger.Pulse().At.IsZero() {
		if time.Now().After(deadline) {
			t.Fatal("no pass ended")
		}
		time.Sleep(5 * time.Millisecond)
	}
	d := read()
	if !d.Running || d.Stalled || d.At != u.at.Unix() || d.EverySeconds != 60 || d.StallAfterSeconds != 180 {
		t.Fatalf("fresh: %+v", d)
	}
	now.Store(u.at.Add(3*time.Minute + time.Second).UnixNano())
	if d := read(); !d.Stalled || d.At != u.at.Unix() {
		t.Fatalf("three intervals on: %+v", d)
	}
}
