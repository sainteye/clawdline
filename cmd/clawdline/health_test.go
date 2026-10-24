package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/healthjournal"
	"github.com/sainteye/clawdline/internal/adapters/healthsealed"
	"github.com/sainteye/clawdline/internal/adapters/healthupload"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/config"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
	httptransport "github.com/sainteye/clawdline/internal/transport/http"
)

func healthRunAgainst(t *testing.T, handler http.HandlerFunc, args ...string) (int, string, string, *http.Request) {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()
	var asked *http.Request
	newRequest := func(path string, q url.Values) (*http.Request, error) {
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
		req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer local-token")
		}
		asked = req
		return req, err
	}
	var stdout, stderr bytes.Buffer
	code := healthRun(&stdout, &stderr, args, time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC), newRequest, srv.Client().Do)
	return code, stdout.String(), stderr.String(), asked
}

// Usage mistakes exit 2 before any request: an unknown subcommand, a cursor
// that does not fit a sequence number, a limit past the page bound, a time
// that is neither RFC 3339 nor a duration, a correlation outside the opaque
// alphabet.
func TestHealthCommandRefusesBadArgumentsBeforeAsking(t *testing.T) {
	never := func(w http.ResponseWriter, r *http.Request) { t.Errorf("asked %s", r.URL) }
	for _, args := range [][]string{
		nil, {"nope"}, {"events", "--after-seq", "18446744073709551616"}, {"events", "--after-seq", "-1"},
		{"events", "--limit", "201"}, {"export", "--since", "yesterday"}, {"trace", "--correlation", "../etc"},
		{"trace"},
	} {
		if code, _, _, _ := healthRunAgainst(t, never, args...); code != 2 {
			t.Errorf("%v: exit %d", args, code)
		}
	}
}

// The window is sent as RFC 3339 times, a duration counted back from now.
func TestHealthCommandSendsTheWindow(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"records":[]}`) }
	code, out, _, req := healthRunAgainst(t, ok, "events", "--since", "15m", "--until", "2026-10-20T12:00:00Z", "--after-seq", "3", "--limit", "5")
	if code != 0 || !strings.Contains(out, `"records": []`) {
		t.Fatalf("exit %d: %s", code, out)
	}
	q := req.URL.Query()
	if req.URL.Path != "/v1/connection-health/events" || q.Get("since") != "2026-10-20T11:45:00Z" ||
		q.Get("until") != "2026-10-20T12:00:00Z" || q.Get("after_seq") != "3" || q.Get("limit") != "5" {
		t.Fatalf("asked %s", req.URL)
	}
}

// A refusal prints its code and exits 1; an unreachable daemon says the
// state is unknown, never that it is fine.
func TestHealthCommandSaysRefusedOrUnknown(t *testing.T) {
	refuse := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":"forbidden","detail":"Only this machine's own token may manage devices."}`)
	}
	code, _, errOut, _ := healthRunAgainst(t, refuse, "status")
	if code != 1 || !strings.Contains(errOut, "403 forbidden") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var stderr bytes.Buffer
	down := func(*http.Request) (*http.Response, error) { return nil, errors.New("connection refused") }
	newRequest := func(path string, _ url.Values) (*http.Request, error) {
		return http.NewRequest(http.MethodGet, "http://127.0.0.1:1"+path, nil)
	}
	if code := healthRun(io.Discard, &stderr, []string{"status"}, time.Now(), newRequest, down); code != 1 || !strings.Contains(stderr.String(), "unknown") {
		t.Fatalf("unreachable: exit %d: %s", code, stderr.String())
	}
}

// The status summary leads with the quality and lists only the findings
// that are slow or stuck, with their stage, location and age.
func TestHealthStatusSummary(t *testing.T) {
	body := `{"schema":"clawdline.connection-health.status/1","boot_id":"boot-1","build_commit":"abc","running":true,
	"quality":"complete","journal_coverage":{"quality":"complete","first_retained_seq":1,"last_seq":3,"evicted_total":0,"refused_total":0,"truncated":false,"next_after_seq":0,"durability":"memory_only_lost_on_restart"},
	"watchdog":{"rule":"watchdog/1","scan_seq":9,"trace_coverage":"complete","transitions":"complete","overflow":{"total":0,"critical":0},
	"findings":[{"phase_id":2,"stage":"handler_started","location":"cloud.link.read_handler","state":"blocked","age_ns":16000000000,"correlation":"op-2"},
	{"phase_id":1,"stage":"idle","location":"cloud.transport.read_loop","state":"idle","age_ns":1}]}}`
	code, out, errOut, _ := healthRunAgainst(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }, "status")
	if code != 0 || !strings.Contains(out, "quality complete") || !strings.Contains(out, "cloud.link.read_handler") ||
		!strings.Contains(out, "16s") || strings.Contains(out, "read_loop") {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
}

// A store that cannot be opened — here its directory's parent is a file —
// is a typed failure, and the runtime keeps scanning in memory.
func TestABrokenStoreDirectoryLeavesTheRuntimeRunning(t *testing.T) {
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(state, "connection-health"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, err := connectionhealth.NewRuntime("abc", 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rt.Run(ctx)
	store, err := openConnectionHealthStore(rt, state, nil)
	if err == nil || store != nil || healthjournal.CodeOf(err) == "" {
		t.Fatalf("a store under a file opened: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if r, ok := rt.Watchdog.Latest(); ok && r.ScanSeq > 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the runtime stopped scanning after the store failed")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// history pages the durable store with its own cursor and bounded limit.
func TestHealthHistoryAsksTheDurableStore(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"entries":[]}`) }
	code, _, _, req := healthRunAgainst(t, ok, "history", "--cursor", "v1.3.120", "--limit", "50")
	if code != 0 || req.URL.Path != "/v1/connection-health/history" || req.URL.Query().Get("cursor") != "v1.3.120" || req.URL.Query().Get("limit") != "50" {
		t.Fatalf("exit %d, asked %s", code, req.URL)
	}
	never := func(w http.ResponseWriter, r *http.Request) { t.Errorf("asked %s", r.URL) }
	if code, _, _, _ := healthRunAgainst(t, never, "history", "--limit", "500"); code != 2 {
		t.Fatalf("an over-bound limit: exit %d", code)
	}
}

func newHealthServer(t *testing.T) (string, *httptransport.Server) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "next")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	srv, err := httptransport.New(config.Config{Dir: dir, Host: "127.0.0.1", Port: 1})
	if err != nil {
		t.Fatal(err)
	}
	return dir, srv
}

// Stopping flushes and closes the store, frees its lock, and the next boot
// reads this boot's records back from disk.
func TestStoppingFlushesAndClosesTheStoreForTheNextBoot(t *testing.T) {
	dir, srv := newHealthServer(t)
	h := startConnectionHealth(dir, srv)
	if h == nil {
		t.Fatal("monitoring is off")
	}
	deadline := time.Now().Add(5 * time.Second)
	for h.store.Load() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the store did not open")
		}
		time.Sleep(5 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), connectionHealthStopTimeout)
	defer cancel()
	if err := h.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.store.Load().Done():
	case <-time.After(time.Second):
		t.Fatal("the store's worker is still running after Stop")
	}
	if st := h.upload.Status(); st.State != healthupload.CodeStopped {
		t.Fatalf("uploader %s after Stop", st.State)
	}
	// The next boot: the lock is free and this boot's boot record is there.
	next, err := healthjournal.Open(healthjournal.Options{Dir: connectionHealthStoreDir(dir), BootID: "boot-next", Build: "abc"})
	if err != nil {
		t.Fatalf("the next boot cannot open the store: %v", err)
	}
	defer func() { _ = next.Close(context.Background()) }()
	page, err := next.Read(healthjournal.Query{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range page.Entries {
		if e.BootID == h.rt.BootID && e.Record != nil && e.Record.Event == connectionhealth.EventBoot {
			found = true
		}
	}
	if !found {
		t.Fatalf("the stopped boot's records are not on disk: %+v", page.Entries)
	}
}

// Stop immediately after startup. The worker may already have published the
// store, or may still be opening it; either branch must release its lock before
// the next boot. Cancellation alone does not join an already-published store.
func TestStoppingWhileTheStoreOpensDoesNotLeakIt(t *testing.T) {
	dir, srv := newHealthServer(t)
	h := startConnectionHealth(dir, srv)
	ctx, cancel := context.WithTimeout(context.Background(), connectionHealthStopTimeout)
	defer cancel()
	if err := h.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	next, err := healthjournal.Open(healthjournal.Options{Dir: connectionHealthStoreDir(dir), BootID: "boot-next", Build: "abc"})
	if err != nil {
		t.Fatalf("the lock leaked: %v", err)
	}
	_ = next.Close(context.Background())
}

// waitHealthStore waits for the durable store to open.
func waitHealthStore(t *testing.T, h *connectionHealthLife) *healthjournal.Store {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for h.store.Load() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the store did not open")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return h.store.Load()
}

// waitStall starts a phase that is blocked within milliseconds and waits for
// the daemon's own watchdog scan to report it.
func waitStall(t *testing.T, h *connectionHealthLife) {
	t.Helper()
	h.rt.Tracker.Start(connectionhealth.Spec{
		Source: connectionhealth.SourceDaemon, Stage: connectionhealth.StageHandlerStarted,
		Class: connectionhealth.ClassRead, Kind: connectionhealth.Finite,
		Location: "cloud.link.read_handler", Warn: time.Millisecond, Block: 2 * time.Millisecond,
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if r, ok := h.rt.Watchdog.Latest(); ok {
			for _, f := range r.Findings {
				if f.State == connectionhealth.StateBlocked {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the watchdog never reported the stall")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func readStoreEvents(t *testing.T, dir, boot string) []connectionhealth.Event {
	t.Helper()
	next, err := healthjournal.Open(healthjournal.Options{Dir: connectionHealthStoreDir(dir), BootID: "boot-next", Build: "abc"})
	if err != nil {
		t.Fatalf("the next boot cannot open the store: %v", err)
	}
	defer func() { _ = next.Close(context.Background()) }()
	page, err := next.Read(healthjournal.Query{})
	if err != nil {
		t.Fatal(err)
	}
	var out []connectionhealth.Event
	for _, e := range page.Entries {
		if e.BootID == boot && e.Record != nil {
			out = append(out, e.Record.Event)
		}
	}
	return out
}

// A stall the watchdog observed is on disk for the next boot after Stop.
func TestAStallObservedBeforeStopIsOnDiskForTheNextBoot(t *testing.T) {
	dir, srv := newHealthServer(t)
	h := startConnectionHealth(dir, srv)
	if h == nil {
		t.Fatal("monitoring is off")
	}
	waitHealthStore(t, h)
	waitStall(t, h)
	ctx, cancel := context.WithTimeout(context.Background(), connectionHealthStopTimeout)
	defer cancel()
	if err := h.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	got := readStoreEvents(t, dir, h.rt.BootID)
	for _, e := range got {
		if e == connectionhealth.EventStallDetected {
			return
		}
	}
	t.Fatalf("the observed stall is not on disk: %v", got)
}

// A durable sink that never returns holds the watchdog's writer. Stop still
// returns by its deadline, names the held writer, and flushes and closes the
// store so the next boot can open it.
func TestAHeldWatchdogWriterDoesNotHoldStopPastItsDeadline(t *testing.T) {
	dir, srv := newHealthServer(t)
	h := startConnectionHealth(dir, srv)
	if h == nil {
		t.Fatal("monitoring is off")
	}
	store := waitHealthStore(t, h)
	release := make(chan struct{})
	held := make(chan struct{}, 1)
	h.rt.Watchdog.Attach(func(r connectionhealth.Record) {
		if r.Event != connectionhealth.EventStallDetected {
			_, _ = store.Offer(r)
			return
		}
		select {
		case held <- struct{}{}:
		default:
		}
		<-release
	})
	defer func() {
		close(release)
		select {
		case <-h.runDone:
		case <-time.After(5 * time.Second):
			t.Error("the watchdog did not finish after its sink let go")
		}
	}()
	waitStall(t, h)
	<-held
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	began := time.Now()
	err := h.Stop(ctx)
	if took := time.Since(began); took > time.Second {
		t.Fatalf("Stop waited %v on a held writer", took)
	}
	if err == nil || !strings.Contains(err.Error(), "watchdog (writer held;") {
		t.Fatalf("Stop must name the held watchdog writer: %v", err)
	}
	select {
	case <-store.Done():
	case <-time.After(time.Second):
		t.Fatal("the store was not closed while the writer was held")
	}
	readStoreEvents(t, dir, h.rt.BootID)
}

// The settings file takes the opt-in through the same writer the settings
// route uses, and the uploader's identity reads it: absent and false are
// not_opted_in (no request), true moves on to Cloud's own switch.
func TestTheHealthUploadOptInIsASettingTheUploaderReads(t *testing.T) {
	dir := t.TempDir()
	identity := healthUploadIdentity(dir, nil)
	code := func() healthupload.Code {
		_, err := identity()
		var e *healthupload.Error
		if !errors.As(err, &e) {
			t.Fatalf("identity answered %v", err)
		}
		return e.Code
	}
	if c := code(); c != healthupload.CodeNotOptedIn {
		t.Fatalf("absent: %s", c)
	}
	// The settings route admits only a listed key: `clawdline setting` and
	// the console write through it.
	if k, ok := nextconfig.SettableByName(healthUploadKey); !ok || !k.Allows(true) || k.Allows("yes") {
		t.Fatalf("%s is not a bool the settings route accepts", healthUploadKey)
	}
	file := nextconfig.Open(dir)
	if _, err := file.Set(map[string]any{healthUploadKey: false}); err != nil {
		t.Fatal(err)
	}
	if c := code(); c != healthupload.CodeNotOptedIn {
		t.Fatalf("false: %s", c)
	}
	if _, err := file.Set(map[string]any{healthUploadKey: true}); err != nil {
		t.Fatalf("the settings file refuses the opt-in: %v", err)
	}
	if c := code(); c == healthupload.CodeNotOptedIn {
		t.Fatal("true still reads as not opted in")
	}
}

// The owner-diagnostic retention opt-in is its own key: turning the metadata
// upload on must leave it off, and it reads off when absent.
func TestOwnerRetentionIsASeparateOptIn(t *testing.T) {
	stored := map[string]any{}
	call := func(method string, body any) (int, []byte, error) {
		if method == http.MethodPost {
			for key, value := range body.(map[string]any) {
				stored[key] = value
			}
		}
		data, _ := json.Marshal(stored)
		return http.StatusOK, data, nil
	}
	run := func(args ...string) (int, string) {
		var out, errs bytes.Buffer
		code := runSetting(&out, &errs, args, call)
		return code, out.String()
	}
	if code, out := run("set", "cloud_health_upload", "on"); code != 0 || !strings.HasPrefix(out, "cloud_health_upload: on") {
		t.Fatalf("upload on: %d %q", code, out)
	}
	if code, out := run("get", "cloud_health_owner_retention"); code != 0 || !strings.HasPrefix(out, "cloud_health_owner_retention: off") {
		t.Fatalf("the metadata switch must not turn retention on: %d %q", code, out)
	}
	if _, ok := stored["cloud_health_owner_retention"]; ok {
		t.Fatalf("retention was written by the upload switch: %v", stored)
	}
	if code, out := run("set", "cloud_health_owner_retention", "on"); code != 0 || !strings.HasPrefix(out, "cloud_health_owner_retention: on") || stored["cloud_health_owner_retention"] != true {
		t.Fatalf("on: %d %q %v", code, out, stored)
	}
	if code, _ := run("set", "cloud_health_owner_retention", "maybe"); code != 2 {
		t.Fatalf("a word: %d", code)
	}
}

// --- owner-sealed diagnostics ---

// sealedCloud is a loopback Cloud for the sealed sender: GET /v1/machines
// names this machine's identity epoch, and POST /v1/health/sealed/attachments
// keeps revisions with §13.2's idempotency (same body replays as duplicate).
type sealedCloud struct {
	mu       sync.Mutex
	identity int
	machines atomic.Int64
	posts    [][]byte
	stored   map[string][]byte
	// hold, when set, answers a POST only when it returns.
	hold func(n int, w http.ResponseWriter, r *http.Request, body []byte) bool
}

func (c *sealedCloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer cred-test" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/machines":
		c.machines.Add(1)
		c.mu.Lock()
		epoch := c.identity
		c.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"machines": []map[string]any{
			{"id": "mac_other", "identity_epoch": 99}, {"id": "mac_test", "identity_epoch": epoch}}})
	case r.Method == http.MethodPost && r.URL.Path == healthsealed.UploadPath:
		raw, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.posts = append(c.posts, raw)
		n := len(c.posts)
		hold := c.hold
		c.mu.Unlock()
		if hold != nil && hold(n, w, r, raw) {
			return
		}
		c.ingest(w, raw)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (c *sealedCloud) ingest(w http.ResponseWriter, raw []byte) {
	var b healthsealed.UploadBody
	if json.Unmarshal(raw, &b) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	id := healthsealed.AttachmentID("acct_test", "mac_test", b.BootID, b.SubjectKind, b.SubjectID)
	aad := healthsealed.AAD{Alg: b.Alg, AttachmentID: id, BootID: b.BootID, ContentKeyEpoch: b.ContentKeyEpoch,
		ExpiresAtMS: b.ExpiresAtMS, IdentityEpoch: b.IdentityEpoch, MachineID: "mac_test",
		PreviousCiphertextSHA256: b.PreviousCiphertextSHA256, Revision: b.Revision, Schema: healthsealed.AADSchema,
		SubjectID: b.SubjectID, SubjectKind: b.SubjectKind, Tenant: "acct_test"}
	digest, _ := aad.Digest()
	key := id + "/" + strconv.FormatInt(b.Revision, 10)
	c.mu.Lock()
	prior, seen := c.stored[key]
	if !seen {
		c.stored[key] = raw
	}
	c.mu.Unlock()
	status, word := http.StatusCreated, "stored"
	if seen {
		if string(prior) != string(raw) {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"revision_conflict"}}`))
			return
		}
		status, word = http.StatusOK, "duplicate"
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(healthsealed.Receipt{Schema: healthsealed.ReceiptSchema, Status: word, AttachmentID: id,
		Revision: b.Revision, CiphertextSHA256: b.CiphertextSHA256, AADSHA256: digest, ContentKeyEpoch: b.ContentKeyEpoch,
		IdentityEpoch: b.IdentityEpoch, MachineKeyEpoch: 1, ExpiresAtMS: b.ExpiresAtMS})
}

func (c *sealedCloud) sent() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]byte(nil), c.posts...)
}

// sealedMachine writes an isolated state directory: Cloud on against the
// loopback server, the owner-retention opt-in, a machine identity and a
// synthetic master secret. withEpoch records the content-key epoch for that
// secret as a completed handover would.
func sealedMachine(t *testing.T, base string, withEpoch int64) (string, []byte) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "next")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := nextconfig.Open(dir).Set(map[string]any{adaptercloud.KeyEnabled: true, adaptercloud.KeyAPIBase: base,
		healthsealed.SettingKey: true}); err != nil {
		t.Fatal(err)
	}
	keyDir := filepath.Join(dir, cloudkeys.DirName)
	if err := adaptercloud.NewIdentityStore(keyDir).Save(adaptercloud.Identity{AccountID: "acct_test", MachineID: "mac_test",
		MachineCredential: "cred-test", APIBase: base}); err != nil {
		t.Fatal(err)
	}
	raw := bytes.Repeat([]byte{7}, domaincloud.ContentKeyBytes)
	key, err := domaincloud.ContentKeyFromBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	files, err := cloudkeys.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := files.SaveMasterSecret(key); err != nil {
		t.Fatal(err)
	}
	if withEpoch > 0 {
		recordEpoch(t, dir, raw, "acct_test", withEpoch)
	}
	return dir, raw
}

func recordEpoch(t *testing.T, dir string, master []byte, account string, epoch int64) {
	t.Helper()
	id, err := domaincloud.ContentKeyID(master)
	if err != nil {
		t.Fatal(err)
	}
	if err := (adaptercloud.ContentEpochStore{Dir: filepath.Join(dir, cloudkeys.DirName)}).Save(adaptercloud.ContentEpoch{
		V: 1, ContentKeyEpoch: epoch, KeyID: id, AccountID: account, MachineID: "mac_test"}); err != nil {
		t.Fatal(err)
	}
}

func newSealedCloud(t *testing.T) (*sealedCloud, string) {
	t.Helper()
	c := &sealedCloud{stored: map[string][]byte{}, identity: 3}
	srv := httptest.NewServer(c)
	t.Cleanup(srv.Close)
	return c, srv.URL
}

func stallRecord(phase uint64) connectionhealth.Record {
	return connectionhealth.Record{EventID: "boot-s-1", BootID: "boot-s", Event: connectionhealth.EventStallDetected,
		Wall: time.UnixMilli(1787745600000), Source: connectionhealth.SourceDaemon, Stage: connectionhealth.StageHandlerStarted,
		Location: "cloud.link.read_handler", PhaseID: phase, Correlation: "op-1", AgeNS: 9_000_000_000,
		DeadlineNS: 8_000_000_000, Build: "c9fe745db066"}
}

func runSealed(t *testing.T, w *healthsealed.Worker) context.CancelFunc {
	t.Helper()
	w.Interval = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); w.Run(ctx) }()
	stop := func() { cancel(); <-done }
	t.Cleanup(stop)
	return stop
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAnUnknownContentEpochIsRefusedBeforeAnyRequest(t *testing.T) {
	c, base := newSealedCloud(t)
	dir, master := sealedMachine(t, base, 0)
	w, err := newSealedDiagnostics(dir, nil, sealedMachines)
	if err != nil {
		t.Fatal(err)
	}
	status := ownerDiagnosticsStatus(w, nil)
	runSealed(t, w)
	waitFor(t, "the opt-in to be read", w.Enabled)
	job, _ := sealedJob(stallRecord(1), 0)
	_ = w.Offer(job)
	waitFor(t, "the refusal", func() bool { return w.Status().Code != "" })
	if got := w.Status().Code; got != healthsealed.CodeKeyHandoverRequired {
		t.Fatalf("no recorded epoch must be key_handover_required, got %s", got)
	}
	if d := status(); d.Coverage != "failed" || d.Code != string(healthsealed.CodeKeyHandoverRequired) {
		t.Fatalf("the status must name the coverage failure: %+v", d)
	}
	// A record naming another account, or a secret that has since been
	// replaced, is not this secret's epoch: still no epoch is assumed.
	keys := sealedKeys{stateDir: dir, machines: sealedMachines}
	recordEpoch(t, dir, master, "acct_other", 4)
	if err := keys.Ready(); healthsealed.CodeOf(err) != healthsealed.CodeKeyHandoverRequired {
		t.Fatalf("an epoch of another account was accepted: %v", err)
	}
	recordEpoch(t, dir, bytes.Repeat([]byte{9}, len(master)), "acct_test", 4)
	if _, err := keys.Keys(context.Background()); healthsealed.CodeOf(err) != healthsealed.CodeKeyHandoverRequired {
		t.Fatalf("an epoch of another master secret was accepted: %v", err)
	}
	if n, posts := c.machines.Load(), len(c.sent()); n != 0 || posts != 0 {
		t.Fatalf("an unknown epoch must refuse before any request: %d machine reads, %d uploads", n, posts)
	}
}

func TestOwnerDiagnosticsAreSealedUnderTheRecordedEpochAndSentOnTheirOwnClient(t *testing.T) {
	c, base := newSealedCloud(t)
	dir, _ := sealedMachine(t, base, 2)
	w, err := newSealedDiagnostics(dir, nil, sealedMachines)
	if err != nil {
		t.Fatal(err)
	}
	status := ownerDiagnosticsStatus(w, nil)
	runSealed(t, w)
	waitFor(t, "the opt-in to be read", w.Enabled)
	if d := status(); d.Coverage != "waiting" {
		t.Fatalf("ready with nothing sealed yet is waiting: %+v", d)
	}
	job, ok := sealedJob(stallRecord(7), 3)
	if !ok {
		t.Fatal("a stall record must make a job")
	}
	if err := w.Offer(job); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the receipt", func() bool { return w.Status().Stored == 1 })
	posts := c.sent()
	if len(posts) != 1 {
		t.Fatalf("one upload, got %d", len(posts))
	}
	var b healthsealed.UploadBody
	if err := json.Unmarshal(posts[0], &b); err != nil {
		t.Fatal(err)
	}
	if b.ContentKeyEpoch != 2 || b.IdentityEpoch != 3 || b.SubjectKind != healthsealed.SubjectIncident || b.SubjectID != "phase-7" {
		t.Fatalf("the body must carry the recorded content epoch and Cloud's identity epoch: %+v", b)
	}
	for _, private := range []string{"cloud.link.read_handler", "op-1", "9000000000", "cred-test"} {
		if strings.Contains(string(posts[0]), private) {
			t.Fatalf("owner-private %q left the machine outside the ciphertext", private)
		}
	}
	if d := status(); d.Coverage != "active" || d.Stored != 1 {
		t.Fatalf("status: %+v", d)
	}
}

func TestConsentWithdrawnBeforeTheSendMakesNoRequest(t *testing.T) {
	c, base := newSealedCloud(t)
	dir, _ := sealedMachine(t, base, 1)
	w, err := newSealedDiagnostics(dir, nil, sealedMachines)
	if err != nil {
		t.Fatal(err)
	}
	w.Interval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); w.Run(ctx) }()
	defer func() { cancel(); <-done }()
	waitFor(t, "the opt-in to be read", w.Enabled)
	// The cached opt-in still says on; the file now says off.
	if _, err := nextconfig.Open(dir).Set(map[string]any{healthsealed.SettingKey: false}); err != nil {
		t.Fatal(err)
	}
	job, _ := sealedJob(stallRecord(2), 0)
	if err := w.Offer(job); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the withdrawal to be read", func() bool { return !w.Enabled() })
	if n, posts := c.machines.Load(), len(c.sent()); n != 0 || posts != 0 {
		t.Fatalf("withdrawn consent must stop the seal and the send: %d machine reads, %d uploads", n, posts)
	}
}

func TestARevisionPendingAtShutdownIsResentByteForByteAfterRestart(t *testing.T) {
	c, base := newSealedCloud(t)
	dir, _ := sealedMachine(t, base, 1)
	inFlight := make(chan struct{})
	release := make(chan struct{})
	c.hold = func(n int, w http.ResponseWriter, r *http.Request, body []byte) bool {
		if n != 1 {
			return false
		}
		close(inFlight)
		select {
		case <-r.Context().Done():
		case <-release:
		}
		panic(http.ErrAbortHandler)
	}
	defer close(release)
	w, err := newSealedDiagnostics(dir, nil, sealedMachines)
	if err != nil {
		t.Fatal(err)
	}
	stop := runSealed(t, w)
	waitFor(t, "the opt-in to be read", w.Enabled)
	job, _ := sealedJob(stallRecord(5), 0)
	if err := w.Offer(job); err != nil {
		t.Fatal(err)
	}
	<-inFlight
	stop() // the daemon stops with the upload in flight
	w2, err := newSealedDiagnostics(dir, nil, sealedMachines)
	if err != nil {
		t.Fatal(err)
	}
	runSealed(t, w2)
	waitFor(t, "the resend's receipt", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.stored) == 1
	})
	posts := c.sent()
	if len(posts) < 2 || string(posts[0]) != string(posts[len(posts)-1]) {
		t.Fatalf("the restart must resend the stored body, not reseal: %d posts", len(posts))
	}
}

func TestALostReceiptIsReplayedAsADuplicateNotAConflict(t *testing.T) {
	c, base := newSealedCloud(t)
	dir, _ := sealedMachine(t, base, 1)
	c.hold = func(n int, w http.ResponseWriter, r *http.Request, body []byte) bool {
		if n != 1 {
			return false
		}
		c.ingest(httptest.NewRecorder(), body) // Cloud stored it; the answer is lost.
		panic(http.ErrAbortHandler)
	}
	w, err := newSealedDiagnostics(dir, nil, sealedMachines)
	if err != nil {
		t.Fatal(err)
	}
	runSealed(t, w)
	waitFor(t, "the opt-in to be read", w.Enabled)
	job, _ := sealedJob(stallRecord(6), 0)
	if err := w.Offer(job); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the replayed receipt", func() bool { return w.Status().Stored == 1 })
	posts := c.sent()
	if len(posts) != 2 || string(posts[0]) != string(posts[1]) || w.Status().Refused != 0 {
		t.Fatalf("the retry must be the same bytes and settle once: %d posts, %+v", len(posts), w.Status())
	}
}

func TestARotatedIdentityEpochIsNotSentTheOldRevision(t *testing.T) {
	c, base := newSealedCloud(t)
	dir, _ := sealedMachine(t, base, 1)
	var fail atomic.Bool
	fail.Store(true)
	c.hold = func(n int, w http.ResponseWriter, r *http.Request, body []byte) bool {
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"code":"health_sealed_deadline"}}`))
			return true
		}
		return false
	}
	w, err := newSealedDiagnostics(dir, nil, sealedMachines)
	if err != nil {
		t.Fatal(err)
	}
	w.Interval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); w.Run(ctx) }()
	waitFor(t, "the opt-in to be read", w.Enabled)
	job, _ := sealedJob(stallRecord(8), 0)
	if err := w.Offer(job); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the bounded attempts", func() bool { return len(c.sent()) == healthsealed.MaxAttempts })
	waitFor(t, "the attempts to settle", func() bool { return w.Status().Code == healthsealed.CodeUpstream })
	cancel()
	<-done
	// Cloud rotates the account's identity epoch; the pending body names
	// the old one and must never be sent again.
	c.mu.Lock()
	c.identity = 4
	c.mu.Unlock()
	fail.Store(false)
	w2, err := newSealedDiagnostics(dir, nil, sealedMachines)
	if err != nil {
		t.Fatal(err)
	}
	runSealed(t, w2)
	waitFor(t, "the local refusal", func() bool { return w2.Status().Code == healthsealed.CodeIdentityChanged })
	if n := len(c.sent()); n != healthsealed.MaxAttempts {
		t.Fatalf("the old revision was sent after the rotation: %d posts", n)
	}
}

func sampleCapture(phase uint64, frames int, fn string) *connectionhealth.Capture {
	g := connectionhealth.CaptureGoroutine{ID: 41, State: "sync.Mutex.Lock", MatchesLocation: true}
	for i := 0; i < frames; i++ {
		g.Frames = append(g.Frames, connectionhealth.CaptureFrame{Function: fn,
			Source: "internal/adapters/cloud/websocket.go:" + strconv.Itoa(200+i), SourceKind: connectionhealth.SourceKindModule})
	}
	return &connectionhealth.Capture{Schema: connectionhealth.CaptureSchema, Seq: 1, BootID: "boot-s", Build: "c9fe745db066",
		Trigger: connectionhealth.CaptureTrigger{PhaseID: phase, Location: "cloud.websocket.writer_lock",
			Stage: connectionhealth.StageWriterLockWaiting, AgeNS: 15_000_000_000, DeadlineNS: 15_000_000_000, RequestedMonoNS: 20_000_000_000},
		DurationNS: 1_200_000, GoroutinesTotal: 3,
		Join: connectionhealth.CaptureJoin{Location: "cloud.websocket.writer_lock", Function: fn, Mapped: true,
			Goroutines: 1, Sources: []string{"internal/adapters/cloud/websocket.go:200"}},
		Goroutines: []connectionhealth.CaptureGoroutine{g, {ID: 1, State: "select", Frames: []connectionhealth.CaptureFrame{{Function: "main.other"}}}}}
}

// A stall, its capture and its recovery are three revisions of one owner
// attachment: the first stays /1 for a reader that knows only /1, the
// later two are /2 and carry the join, the stalled goroutine and the
// recovery; nothing of them leaves the machine outside the ciphertext.
func TestAStallsCaptureAndRecoveryReviseItsOwnerAttachment(t *testing.T) {
	const fn = "github.com/sainteye/clawdline/internal/adapters/cloud.(*Conn).writeFrame"
	c, base := newSealedCloud(t)
	dir, _ := sealedMachine(t, base, 2)
	w, err := newSealedDiagnostics(dir, nil, sealedMachines)
	if err != nil {
		t.Fatal(err)
	}
	runSealed(t, w)
	waitFor(t, "the opt-in to be read", w.Enabled)
	o := newOwnerIncidents()
	stall := stallRecord(7)
	// A real stall record carries no build (only the boot record does).
	stall.MonoNS, stall.Build = 20_000_000_000, ""
	job, ok := sealedJob(stall, 0)
	if !ok {
		t.Fatal("no stall job")
	}
	o.remember(stall, job)
	captured, ok := o.captured(sampleCapture(7, 3, fn))
	if !ok {
		t.Fatal("no capture revision")
	}
	rec := stall
	rec.Event, rec.MonoNS = connectionhealth.EventRecovered, 23_500_000_000
	recovered, ok := o.recovered(rec)
	if !ok {
		t.Fatal("no recovery revision")
	}
	if _, again := o.recovered(rec); again {
		t.Fatal("a second recovery for one stall made another revision")
	}
	if job.Snapshot.Schema != healthsealed.SnapshotSchema || captured.Snapshot.Schema != healthsealed.SnapshotSchemaV2 ||
		recovered.Snapshot.Schema != healthsealed.SnapshotSchemaV2 {
		t.Fatalf("schemas: %s %s %s", job.Snapshot.Schema, captured.Snapshot.Schema, recovered.Snapshot.Schema)
	}
	rv := recovered.Snapshot
	if rv.Capture == nil || rv.Capture.Function != fn || len(rv.Capture.Frames) != 3 || rv.Capture.Sources[0] != "internal/adapters/cloud/websocket.go:200" ||
		rv.Recovery == nil || rv.Recovery.State != "recovered" || rv.Recovery.RecoveryNS.Value != 3_500_000_000 ||
		rv.Location != "cloud.link.read_handler" || rv.CorrelationID != "op-1" || rv.Build != "c9fe745db066" {
		t.Fatalf("the recovery revision does not join stall, capture and recovery: %+v", rv)
	}
	plain, err := rv.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := healthsealed.DecodeSnapshot(plain)
	if err != nil || back.Capture == nil || back.Capture.Frames[0].Source != "internal/adapters/cloud/websocket.go:200" {
		t.Fatalf("the /2 snapshot does not round-trip: %v %+v", err, back.Capture)
	}
	for _, j := range []healthsealed.Job{job, captured, recovered} {
		if err := w.Offer(j); err != nil {
			t.Fatal(err)
		}
		n := w.Status().Stored + 1
		waitFor(t, "the receipt", func() bool { return w.Status().Stored == n })
	}
	posts := c.sent()
	if len(posts) != 3 {
		t.Fatalf("three uploads, got %d", len(posts))
	}
	for i, p := range posts {
		var b healthsealed.UploadBody
		if err := json.Unmarshal(p, &b); err != nil {
			t.Fatal(err)
		}
		if b.Revision != int64(i+1) || b.SubjectID != "phase-7" {
			t.Fatalf("upload %d: revision %d subject %s", i, b.Revision, b.SubjectID)
		}
		for _, private := range []string{"writeFrame", "websocket.go", "sync.Mutex", "recovered"} {
			if strings.Contains(string(p), private) {
				t.Fatalf("%q left the machine outside the ciphertext", private)
			}
		}
	}
}

// The feed remembers maxOwnerIncidents stalls; a forgotten stall's
// recovery is not sealed, and its capture is sealed from its own trigger.
func TestOwnerIncidentsAreBounded(t *testing.T) {
	o := newOwnerIncidents()
	for i := uint64(1); i <= maxOwnerIncidents+1; i++ {
		r := stallRecord(i)
		job, _ := sealedJob(r, 0)
		o.remember(r, job)
	}
	if len(o.rows) != maxOwnerIncidents || len(o.order) != maxOwnerIncidents {
		t.Fatalf("rows %d order %d", len(o.rows), len(o.order))
	}
	rec := stallRecord(1)
	rec.Event = connectionhealth.EventRecovered
	if _, ok := o.recovered(rec); ok {
		t.Fatal("a forgotten stall's recovery was sealed")
	}
	j, ok := o.captured(sampleCapture(1, 1, "main.main"))
	if !ok || j.Snapshot.Location != "cloud.websocket.writer_lock" || j.SubjectID != "phase-1" {
		t.Fatalf("a forgotten stall's capture: %v %+v", ok, j.Snapshot)
	}
}

// A capture too big for the owner bound is trimmed frame by frame, and
// says so.
func TestAnOwnerCaptureIsTrimmedToTheSnapshotBound(t *testing.T) {
	fn := "github.com/sainteye/clawdline/internal/adapters/cloud." + strings.Repeat("x", 130)
	o := newOwnerIncidents()
	r := stallRecord(3)
	job, _ := sealedJob(r, 0)
	o.remember(r, job)
	j, ok := o.captured(sampleCapture(3, 64, fn))
	if !ok {
		t.Fatal("no revision")
	}
	plain, err := j.Snapshot.Encode()
	if err != nil || len(plain) > healthsealed.MaxSnapshotBytes {
		t.Fatalf("%v, %d bytes", err, len(plain))
	}
	c := j.Snapshot.Capture
	if !c.Truncated || len(c.Frames) == 0 || int64(len(c.Frames))+c.FramesOmitted.Value != 64 {
		t.Fatalf("frames %d omitted %d truncated %v", len(c.Frames), c.FramesOmitted.Value, c.Truncated)
	}
}

// TestWriteSyntheticOwnerSnapshot turns the synthetic bundle's local
// export (internal/domain/connectionhealth TestWriteSyntheticBundle) into
// the /2 owner snapshot the daemon would seal for it. It runs only with
// HEALTH_SYNTHETIC_BUNDLE_DIR set (the package clears CLAWDLINE_* variables).
func TestWriteSyntheticOwnerSnapshot(t *testing.T) {
	dir := os.Getenv("HEALTH_SYNTHETIC_BUNDLE_DIR")
	if dir == "" {
		t.Skip("set HEALTH_SYNTHETIC_BUNDLE_DIR to write the owner snapshot")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "export.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ev connectionhealth.Evidence
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.StallCapture == nil || ev.StallCapture.Latest == nil {
		t.Fatal("the export has no capture")
	}
	c := ev.StallCapture.Latest
	var stall connectionhealth.Record
	for _, r := range ev.Journal.Records {
		if r.Event == connectionhealth.EventStallDetected && r.PhaseID == c.Trigger.PhaseID {
			stall = r
		}
	}
	if stall.PhaseID == 0 {
		t.Fatal("the export's journal has no stall for the capture")
	}
	o := newOwnerIncidents()
	job, ok := sealedJob(stall, ev.Watchdog.TransitionsLost)
	if !ok {
		t.Fatal("no stall job")
	}
	o.remember(stall, job)
	rev, ok := o.captured(c)
	if !ok {
		t.Fatal("no capture revision")
	}
	plain, err := rev.Snapshot.Encode()
	if err != nil {
		t.Fatal(err)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, plain, "", "  "); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "owner-snapshot-v2.json"), append(pretty.Bytes(), '\n'), 0o444); err != nil {
		t.Fatal(err)
	}
}
