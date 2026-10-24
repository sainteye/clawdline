package healthupload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/healthjournal"
	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

type clock struct {
	mu  sync.Mutex
	now connectionhealth.Instant
}

func (c *clock) Now() connectionhealth.Instant {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now.Mono += time.Millisecond
	c.now.Wall = c.now.Wall.Add(time.Millisecond)
	return c.now
}

var t0 = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

// fixture is a real durable store fed by a real foundation journal, the way
// the daemon feeds it.
type fixture struct {
	t     *testing.T
	dir   string
	boot  string
	j     *connectionhealth.Journal
	store *healthjournal.Store
	last  uint64
}

func newFixture(t *testing.T, dir, boot string) *fixture {
	t.Helper()
	j, err := connectionhealth.NewJournal(&clock{now: connectionhealth.Instant{Mono: time.Second, Wall: t0}}, boot, "c9fe745db066")
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, dir: dir, boot: boot, j: j}
	f.open()
	return f
}

func (f *fixture) open() {
	f.t.Helper()
	s, err := healthjournal.Open(healthjournal.Options{Dir: filepath.Join(f.dir, "journal"), BootID: f.boot, Build: "c9fe745db066"})
	if err != nil {
		f.t.Fatal(err)
	}
	f.store = s
	f.t.Cleanup(func() { f.close() })
}

func (f *fixture) close() {
	if f.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = f.store.Close(ctx)
	<-f.store.Done()
	f.store = nil
}

// add appends n stall records carrying every owner-private field, offers
// what the journal stamped to the store and flushes it.
func (f *fixture) add(n int) {
	f.t.Helper()
	for i := 0; i < n; i++ {
		if err := f.j.Append(connectionhealth.Record{
			Event: connectionhealth.EventStallDetected, Source: connectionhealth.SourceDaemon,
			Stage: connectionhealth.StageHandlerStarted, Class: connectionhealth.ClassRead,
			Kind: connectionhealth.Finite, Location: "cloud.link.read_handler", PhaseID: uint64(i + 1),
			Correlation: "tr-1", AgeNS: 8_000_000_000 + int64(i), DeadlineNS: 7_000_000_000, Rule: connectionhealth.RuleVersion,
		}); err != nil {
			f.t.Fatal(err)
		}
	}
	page, err := f.j.Read(connectionhealth.Query{AfterSeq: f.last})
	if err != nil {
		f.t.Fatal(err)
	}
	for _, r := range page.Records {
		if _, err := f.store.Offer(r); err != nil {
			f.t.Fatal(err)
		}
	}
	f.last = page.Coverage.NextAfterSeq
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := f.store.Flush(ctx); err != nil {
		f.t.Fatal(err)
	}
}

// cloud is a fake ingest with the real contract's idempotency: a batch is
// keyed by batch_id, an exact replay answers 200 with the stored receipt,
// a changed body under the same id answers 409.
type cloud struct {
	mu       sync.Mutex
	stored   map[string]string // batch_id -> body digest
	receipts map[string][]byte
	bodies   [][]byte
	requests int
	// answer, when set, decides the response instead of the ingest.
	answer func(n int, w http.ResponseWriter, r *http.Request, body []byte) bool
	auth   string
}

func newCloud() *cloud {
	return &cloud{stored: map[string]string{}, receipts: map[string][]byte{}, auth: "Bearer machine-secret"}
}

func (c *cloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	c.requests++
	n := c.requests
	c.bodies = append(c.bodies, body)
	answer := c.answer
	c.mu.Unlock()
	if r.URL.Path != IngestPath || r.Method != http.MethodPost || r.URL.RawQuery != "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.Header.Get("Authorization") != c.auth || r.Header.Get("Cookie") != "" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"no"}}`))
		return
	}
	if answer != nil && answer(n, w, r, body) {
		return
	}
	c.ingest(w, body)
}

func (c *cloud) ingest(w http.ResponseWriter, body []byte) {
	var b struct {
		BatchID string            `json:"batch_id"`
		Events  []json.RawMessage `json:"events"`
	}
	if json.Unmarshal(body, &b) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	sum := sha256.Sum256(body)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.stored[b.BatchID]; ok {
		if old != digest {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"idempotency_conflict","message":"x","details":{"field":"batch_id"}}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(c.receipts[b.BatchID])
		return
	}
	rc, _ := json.Marshal(map[string]any{"schema_version": 1, "receipt_id": "hr_" + b.BatchID, "batch_id": b.BatchID,
		"digest": digest, "persisted_at": "2026-10-20T12:00:17.000Z", "retain_until": "2026-10-21T12:00:17.000Z",
		"accepted": len(b.Events), "rejected": []any{}, "coverage": map[string]any{}})
	c.stored[b.BatchID] = digest
	c.receipts[b.BatchID] = rc
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(rc)
}

func (c *cloud) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests
}

type harness struct {
	u     *Uploader
	cloud *cloud
	srv   *httptest.Server
	now   time.Time
	cred  string
	f     *fixture
	state string
	// transport, when set, replaces the loopback server's.
	transport http.RoundTripper
}

func newHarness(t *testing.T, f *fixture, c *cloud, state string) *harness {
	t.Helper()
	srv := httptest.NewServer(c)
	t.Cleanup(srv.Close)
	h := &harness{cloud: c, srv: srv, now: t0.Add(time.Minute), cred: "machine-secret", f: f, state: state}
	h.u = h.make()
	return h
}

func (h *harness) make() *Uploader {
	u := New(Options{
		StateDir: h.state,
		Identity: func() (Identity, error) { return Identity{APIBase: h.srv.URL, Credential: h.cred}, nil },
		Source: func() Source {
			if h.f.store == nil {
				return nil
			}
			return h.f.store
		},
		Now:               func() time.Time { return h.now },
		Transport:         h.transport,
		AllowLoopbackHTTP: true,
		AttemptTimeout:    300 * time.Millisecond,
	})
	st, _, err := loadState(h.state)
	if err != nil {
		panic(err)
	}
	u.st = st
	return u
}

func (h *harness) step(t *testing.T) time.Duration {
	t.Helper()
	return h.u.Step(context.Background())
}

func TestADroppedAnswerIsResentByteForByteAndReceiptedOnce(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-a")
	f.add(5)
	c := newCloud()
	c.answer = func(n int, w http.ResponseWriter, r *http.Request, body []byte) bool {
		if n != 1 {
			return false
		}
		// Store the batch, then lose the answer on the way back.
		c.ingest(httptest.NewRecorder(), body)
		panic(http.ErrAbortHandler)
	}
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	if wait := h.step(t); wait == 0 {
		t.Fatalf("a lost answer must wait before the retry")
	}
	if st := h.u.Status(); st.State != CodeUpstreamUnavailable || st.Pending == nil || st.Pending.Persisted != "unknown" {
		t.Fatalf("after a lost answer: %+v", st)
	}
	h.now = h.now.Add(time.Minute)
	h.step(t)
	if c.count() != 2 || !bytes.Equal(c.bodies[0], c.bodies[1]) {
		t.Fatalf("the retry must resend the same bytes: %d requests", c.count())
	}
	if len(c.stored) != 1 {
		t.Fatalf("Cloud holds %d batches, want one", len(c.stored))
	}
	st := h.u.Status()
	if st.LastReceipt == nil || !st.LastReceipt.Replay || st.Totals.Batches != 1 || st.Pending != nil {
		t.Fatalf("one durable receipt, from the replay: %+v", st)
	}
	// The boot record and five stalls.
	if st.Totals.Receipted != 6 {
		t.Fatalf("receipted %d events, want 6", st.Totals.Receipted)
	}
}

func TestOwnerPrivateFieldsNeverLeaveTheMachine(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-p")
	f.add(3)
	c := newCloud()
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	h.step(t)
	if c.count() != 1 {
		t.Fatalf("%d requests", c.count())
	}
	body := c.bodies[0]
	for _, banned := range []string{"location", "age_ns", "deadline_ns", "cloud.link", "read_handler", "8000000000", "7000000000", "owner_sealed", "state\""} {
		if bytes.Contains(body, []byte(banned)) {
			t.Fatalf("the body carries %q", banned)
		}
	}
	// Every key is one Cloud's record/1 parser lists.
	allowed := map[string]bool{}
	for _, k := range []string{"schema", "producer_seq", "event_id", "boot_id", "event", "wall", "mono_ns", "build_commit",
		"source", "stage", "class", "kind", "phase_id", "correlation", "count", "rule"} {
		allowed[k] = true
	}
	var b map[string]json.RawMessage
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatal(err)
	}
	for k := range b {
		switch k {
		case "schema_version", "batch_id", "producer_instance", "boot_id", "build_commit", "events":
		default:
			t.Fatalf("batch key %q", k)
		}
	}
	var events []map[string]json.RawMessage
	_ = json.Unmarshal(b["events"], &events)
	for _, e := range events {
		for k := range e {
			if !allowed[k] {
				t.Fatalf("event key %q", k)
			}
		}
		var wall string
		_ = json.Unmarshal(e["wall"], &wall)
		if !strings.HasSuffix(wall, "Z") {
			t.Fatalf("wall %q is not UTC", wall)
		}
	}
	if strings.Contains(string(body), "machine-secret") {
		t.Fatal("the credential is in the body")
	}
}

// Cloud record/1 accepts state_after_loss with its watchdog state (Cloud
// contract §12); the owner-private fields still stay here, and no other
// event carries a state.
func TestAStateAfterLossRecordIsSentWithItsStateOnly(t *testing.T) {
	r := connectionhealth.Record{Schema: connectionhealth.RecordSchema, Seq: 4, EventID: "b-4", BootID: "b",
		Event: connectionhealth.EventStateAfterLoss, Wall: t0, State: connectionhealth.StateBlocked,
		Source: connectionhealth.SourceDaemon, Stage: connectionhealth.StageHandlerStarted,
		Location: "cloud.link.read_handler", AgeNS: 9_000_000_000, DeadlineNS: 7_000_000_000, Rule: connectionhealth.RuleVersion}
	for _, st := range []connectionhealth.State{connectionhealth.StateSlow, connectionhealth.StateBlocked, connectionhealth.StateUnverified} {
		r.State = st
		w, reason := toWire(r)
		if reason != "" || w.State != string(st) {
			t.Fatalf("state_after_loss %s: reason %q, wire state %q", st, reason, w.State)
		}
		body, _ := json.Marshal(w)
		for _, private := range []string{"location", "age_ns", "deadline_ns", "read_handler"} {
			if strings.Contains(string(body), private) {
				t.Fatalf("%s left the machine: %s", private, body)
			}
		}
	}
	r.State = connectionhealth.StateProgressing
	if _, reason := toWire(r); reason != ExcludedState {
		t.Fatalf("a state Cloud does not list must be excluded and counted: %q", reason)
	}
	r.Event, r.State = connectionhealth.EventStallDetected, connectionhealth.StateBlocked
	if w, _ := toWire(r); w.State != "" {
		t.Fatalf("state on %s: Cloud refuses it there", r.Event)
	}
	r.Event, r.MonoNS = connectionhealth.EventStallDetected, jsSafeInteger+1
	if _, reason := toWire(r); reason != ExcludedUnsafeInteger {
		t.Fatalf("unsafe mono_ns: %q", reason)
	}
}

func TestEveryGoEnumIsReconciledWithCloud(t *testing.T) {
	for _, e := range []connectionhealth.Event{
		connectionhealth.EventBoot, connectionhealth.EventSlowDetected, connectionhealth.EventStallDetected,
		connectionhealth.EventRecovered, connectionhealth.EventReleasedStalled, connectionhealth.EventOverflow,
		connectionhealth.EventSchedulingGap, connectionhealth.EventClockAnomaly, connectionhealth.EventTransitionsLost,
		connectionhealth.EventStateAfterLoss,
	} {
		_, excluded := excludedEvents[e]
		if cloudEvents[e] == excluded {
			t.Errorf("event %q must be either sent or explicitly excluded", e)
		}
	}
	for _, s := range append(append([]connectionhealth.Stage{}, connectionhealth.Lifecycle...), connectionhealth.StageIdle) {
		if !cloudStages[s] {
			t.Errorf("stage %q is not in Cloud's record/1 list: decide whether Cloud adds it", s)
		}
	}
}

func TestARestartResendsThePendingBatchAndKeepsTheCursor(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-r")
	f.add(4)
	c := newCloud()
	down := true
	c.answer = func(int, http.ResponseWriter, *http.Request, []byte) bool {
		if down {
			panic(http.ErrAbortHandler)
		}
		return false
	}
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	h.step(t)
	first := h.u.Status().Pending
	if first == nil {
		t.Fatal("no pending batch")
	}
	// The daemon restarts: the store reopens, more records arrive, and a
	// new uploader reads the progress file.
	f.close()
	f.open()
	f.add(2)
	down = false
	h.now = h.now.Add(time.Minute)
	h.u = h.make()
	h.step(t)
	if c.count() != 2 || !bytes.Equal(c.bodies[0], c.bodies[1]) {
		t.Fatal("after a restart the same batch must be resent byte for byte")
	}
	h.step(t)
	st := h.u.Status()
	if st.Totals.Batches != 2 || st.Totals.Receipted != 7 || st.Pending != nil {
		t.Fatalf("both batches receipted, nothing twice: %+v", st.Totals)
	}
	// Idle: nothing new, no request.
	n := c.count()
	h.u = h.make()
	h.step(t)
	h.step(t)
	if c.count() != n || h.u.Status().State != CodeIdle {
		t.Fatalf("an idle uploader made %d requests", c.count()-n)
	}
}

func TestARevokedCredentialIsNotRetried(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-x")
	f.add(1)
	c := newCloud()
	c.auth = "Bearer another"
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	h.step(t)
	for i := 0; i < 5; i++ {
		h.now = h.now.Add(time.Hour)
		h.step(t)
	}
	if c.count() != 1 || h.u.Status().State != CodeAuthRefused {
		t.Fatalf("a refused credential was used %d times", c.count())
	}
	// A new credential is tried once.
	h.cred, c.auth = "rotated", "Bearer rotated"
	h.step(t)
	if c.count() != 2 || h.u.Status().Totals.Batches != 1 {
		t.Fatalf("a new credential must be tried: %+v", h.u.Status())
	}
}

func TestTooManyRequestsWaitsForRetryAfter(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-q")
	f.add(1)
	c := newCloud()
	c.answer = func(n int, w http.ResponseWriter, r *http.Request, body []byte) bool {
		if n > 1 {
			return false
		}
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":"health_budget_exhausted","message":"m","details":{"retry_after":120}}}`))
		return true
	}
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	if wait := h.step(t); wait != 120*time.Second {
		t.Fatalf("wait %s, want Retry-After", wait)
	}
	h.now = h.now.Add(time.Minute)
	h.step(t)
	if c.count() != 1 || h.u.Status().State != CodeQuotaCloud {
		t.Fatal("a retry before Retry-After")
	}
	h.now = h.now.Add(2 * time.Minute)
	h.step(t)
	if c.count() != 2 || h.u.Status().Totals.Batches != 1 {
		t.Fatal("after Retry-After the batch is resent")
	}
}

// stalledCloud answers no request: each one waits on its own context, so an
// attempt ends only when the uploader's deadline reaches the request. It
// records the time each request was given and how it ended.
type stalledCloud struct {
	mu      sync.Mutex
	budgets []time.Duration // the request's deadline less its arrival; 0 without one
	ended   []error
}

func (s *stalledCloud) RoundTrip(r *http.Request) (*http.Response, error) {
	arrived := time.Now()
	if r.Body != nil {
		_ = r.Body.Close()
	}
	var budget time.Duration
	if d, ok := r.Context().Deadline(); ok {
		budget = d.Sub(arrived)
	}
	var err error
	select {
	case <-r.Context().Done():
		err = r.Context().Err()
	case <-time.After(5 * time.Second):
		// Fails the test rather than hanging it when no deadline arrives.
		err = errors.New("the request outlived any deadline")
	}
	s.mu.Lock()
	s.budgets = append(s.budgets, budget)
	s.ended = append(s.ended, err)
	s.mu.Unlock()
	return nil, err
}

func (s *stalledCloud) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.ended)
}

// The bound under test is the one the request carries, not the wall clock
// of a whole Step: a Step also reads the journal and fsyncs its state twice,
// which a loaded CI disk stretched to 4.9s.
func TestATimeoutIsBoundedAndTheBatchIsAbandonedAfterItsAttempts(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-t")
	f.add(1)
	h := newHarness(t, f, newCloud(), filepath.Join(dir, "upload"))
	s := &stalledCloud{}
	h.transport = s
	h.u = h.make()
	h.step(t)
	s.mu.Lock()
	if len(s.ended) != 1 {
		s.mu.Unlock()
		t.Fatalf("the first Step returned after %d finished requests", len(s.ended))
	}
	budget, ended := s.budgets[0], s.ended[0]
	s.mu.Unlock()
	if budget <= 0 || budget > 300*time.Millisecond {
		t.Fatalf("the request was given %s, not the attempt's 300ms", budget)
	}
	if !errors.Is(ended, context.DeadlineExceeded) {
		t.Fatalf("the request ended with %v, not its deadline", ended)
	}
	if st := h.u.Status(); st.Totals.Attempts != 1 || st.Totals.AbandonedBatches != 0 {
		t.Fatalf("after one timed-out attempt, totals %+v", st.Totals)
	}
	for i := 0; i < 2*MaxAttempts; i++ {
		h.now = h.now.Add(10 * time.Minute)
		h.step(t)
	}
	st := h.u.Status()
	if s.count() != MaxAttempts || st.Totals.AbandonedBatches != 1 || st.Totals.AbandonedEvents != 2 {
		t.Fatalf("%d attempts, totals %+v", s.count(), st.Totals)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, err := range s.ended {
		if !errors.Is(err, context.DeadlineExceeded) || s.budgets[i] <= 0 || s.budgets[i] > 300*time.Millisecond {
			t.Fatalf("attempt %d: given %s, ended with %v", i+1, s.budgets[i], err)
		}
	}
}

func TestNothingIsSentWhenUploadIsOff(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-o")
	f.add(2)
	for _, code := range []Code{CodeNotOptedIn, CodeCloudOff, CodeNoIdentity, CodeIdentityUnread} {
		var calls int
		u := New(Options{
			StateDir: filepath.Join(dir, "upload"),
			Identity: func() (Identity, error) { return Identity{}, &Error{Code: code} },
			Source:   func() Source { return f.store },
			Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, io.EOF
			}),
		})
		u.Step(context.Background())
		if calls != 0 || u.Status().State != code {
			t.Fatalf("%s: %d requests, state %s", code, calls, u.Status().State)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "upload", stateFile)); !os.IsNotExist(err) {
		t.Fatal("an uploader that is off wrote progress")
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOnlyHTTPSOrAnIsolatedLoopbackIsAnOrigin(t *testing.T) {
	u := New(Options{})
	for _, base := range []string{"http://api.clawdline.com", "http://127.0.0.1:9", "ftp://x", "https://u:p@api.clawdline.com", "https://api.clawdline.com?x=1", ""} {
		if _, err := u.endpoint(base); err == nil {
			t.Errorf("%q accepted", base)
		}
	}
	if e, err := u.endpoint("https://api.clawdline.com/"); err != nil || e != "https://api.clawdline.com"+IngestPath {
		t.Fatalf("https refused: %v %s", err, e)
	}
	u = New(Options{AllowLoopbackHTTP: true})
	if _, err := u.endpoint("http://api.clawdline.com"); err == nil {
		t.Fatal("http to a remote host accepted")
	}
}

func TestTheHourlyBudgetStopsRequestsBeforeCloudDoes(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-h")
	f.add(1)
	c := newCloud()
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	h.u.st.Hour = hourBudget{Start: h.now.UTC().Truncate(time.Hour), Requests: MaxHourlyRequests}
	wait := h.step(t)
	if c.count() != 0 || h.u.Status().State != CodeQuotaLocal || wait <= 0 || wait > time.Hour {
		t.Fatalf("over budget: %d requests, wait %s", c.count(), wait)
	}
	h.now = h.now.Add(wait)
	h.step(t)
	if c.count() != 1 {
		t.Fatal("the next hour sends")
	}
}

func TestARefusedBatchIsCountedWithItsCodeAndNotResent(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-z")
	f.add(1)
	c := newCloud()
	c.answer = func(n int, w http.ResponseWriter, r *http.Request, body []byte) bool {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_field","message":"events[0].wall secret-value","details":{"field":"events[0].wall"}}}`))
		return true
	}
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	h.step(t)
	h.step(t)
	st := h.u.Status()
	if c.count() != 1 || st.Totals.RefusedBatches != 1 || st.LastRefusal == nil || st.LastRefusal.Code != "invalid_field" || st.LastRefusal.Field != "events[0].wall" {
		t.Fatalf("%d requests, %+v", c.count(), st)
	}
	raw, _ := json.Marshal(st)
	if bytes.Contains(raw, []byte("secret-value")) {
		t.Fatal("Cloud's message is echoed")
	}
}

func TestABatchRebuiltAfterALostProgressFileReplaysItsReceipt(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-l")
	f.add(3)
	c := newCloud()
	c.answer = func(n int, w http.ResponseWriter, r *http.Request, body []byte) bool {
		if n != 1 {
			return false
		}
		c.ingest(httptest.NewRecorder(), body)
		panic(http.ErrAbortHandler)
	}
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	h.step(t)
	if err := os.Remove(filepath.Join(dir, "upload", stateFile)); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(time.Hour)
	h.u = h.make()
	h.step(t)
	st := h.u.Status()
	if c.count() != 2 || st.LastReceipt == nil || !st.LastReceipt.Replay || st.Totals.RefusedBatches != 0 {
		t.Fatalf("the rebuilt batch must replay, not conflict: %+v", st)
	}
}

// TestWriteABodyForTheCloudParser writes the body this uploader sends for
// real stored records, so Cloud's own parser can be run on it
// (HEALTHUPLOAD_BODY_OUT=<file>; skipped otherwise).
func TestWriteABodyForTheCloudParser(t *testing.T) {
	out := os.Getenv("HEALTHUPLOAD_BODY_OUT")
	if out == "" {
		t.Skip("HEALTHUPLOAD_BODY_OUT is not set")
	}
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-7f3a")
	f.add(40)
	c := newCloud()
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	h.step(t)
	if c.count() != 1 {
		t.Fatal("no request")
	}
	if err := os.WriteFile(out, c.bodies[0], 0o600); err != nil {
		t.Fatal(err)
	}
	// A second body: one state_after_loss per Cloud watchdog state, as the
	// uploader projects it, for the same parser.
	var events []wireEvent
	for i, st := range []connectionhealth.State{connectionhealth.StateSlow, connectionhealth.StateBlocked, connectionhealth.StateUnverified} {
		w, reason := toWire(connectionhealth.Record{Schema: connectionhealth.RecordSchema, Seq: uint64(i + 1),
			EventID: fmt.Sprintf("boot-7f3a-%d", i+1), BootID: "boot-7f3a", Build: "c9fe745db066",
			Event: connectionhealth.EventStateAfterLoss, Wall: t0, MonoNS: int64(i + 1),
			Source: connectionhealth.SourceDaemon, Stage: connectionhealth.StageHandlerStarted,
			Class: connectionhealth.ClassRead, Kind: connectionhealth.Finite, PhaseID: 3, Correlation: "tr-1",
			Location: "cloud.link.read_handler", AgeNS: 9_000_000_000, DeadlineNS: 7_000_000_000,
			Rule: connectionhealth.RuleVersion, State: st})
		if reason != "" {
			t.Fatal(reason)
		}
		events = append(events, w)
	}
	body, err := encode(wireBatch{SchemaVersion: WireSchemaVersion, BatchID: "batch-state-1", ProducerInstance: ProducerInstance,
		BootID: "boot-7f3a", BuildCommit: "c9fe745db066", Events: events})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out+".state", body, 0o600); err != nil {
		t.Fatal(err)
	}
}

// The daemon's journal is moved aside and recreated while the progress file
// keeps its cursor. The uploader runs on its own goroutine inside the
// business daemon, so the old cursor must restart from the new store's
// oldest entry, counted as a gap, and never panic.
func TestARecreatedStoreRestartsAnOldCursorWithoutAPanic(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-old")
	f.add(3)
	c := newCloud()
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	h.step(t)
	before := h.u.Status().Totals.Receipted
	if h.u.Status().Cursor == "" || c.count() != 1 || before < 3 {
		t.Fatalf("first batch not uploaded: %+v", h.u.Status())
	}
	f.close()
	if err := os.Rename(filepath.Join(dir, "journal"), filepath.Join(dir, "journal.aside")); err != nil {
		t.Fatal(err)
	}
	f.open()
	h.now = h.now.Add(time.Minute)
	h.u = h.make()
	h.step(t)
	st := h.u.Status()
	if st.Totals.Gaps["invalid_cursor"] != 1 {
		t.Fatalf("the stale cursor must be counted once as an invalid_cursor gap: %+v", st.Totals)
	}
	f.add(2)
	h.now = h.now.Add(time.Minute)
	h.step(t)
	if got := h.u.Status().Totals.Receipted; got < before+2 {
		t.Fatalf("records of the recreated store must upload after the restart: receipted %d after %d", got, before)
	}
}

// The status reports the progress file's real size once it has been read
// or written, and null before; the capacity row reads it from there.
func TestTheStatusReportsTheProgressFileSize(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-s")
	f.add(3)
	state := filepath.Join(dir, "upload")
	if New(Options{StateDir: state}).Status().StateBytes != nil {
		t.Fatal("an uploader that has not read its file reports a size")
	}
	h := newHarness(t, f, newCloud(), state)
	if n := h.u.Status().StateBytes; n == nil || *n != 0 {
		t.Fatalf("no file yet is a known zero: %v", n)
	}
	h.step(t)
	info, err := os.Stat(filepath.Join(state, stateFile))
	if err != nil {
		t.Fatal(err)
	}
	if n := h.u.Status().StateBytes; n == nil || int64(*n) != info.Size() {
		t.Fatalf("state_bytes %v, file on disk %d", n, info.Size())
	}
	h.u = h.make()
	if n := h.u.Status().StateBytes; n == nil || int64(*n) != info.Size() {
		t.Fatalf("after a reload state_bytes %v, file on disk %d", n, info.Size())
	}
}
