package cloud

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

// The switch is the whole of `docs/remote.md` design principle 1: the free
// product does not depend on Cloud, so a daemon whose settings say nothing must
// behave exactly as it did before this package existed.

func TestALineIsOffUntilSomebodySaysOtherwise(t *testing.T) {
	for _, config := range []struct {
		name string
		body string
	}{
		{"no settings file at all", ""},
		{"a settings file with no cloud keys", `{"remote":true}`},
		{"the switch written false", `{"cloud_enabled":false}`},
		{"the switch written as a string", `{"cloud_enabled":"true"}`},
	} {
		t.Run(config.name, func(t *testing.T) {
			dir := t.TempDir()
			if config.body != "" {
				if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(config.body), 0o600); err != nil {
					t.Fatalf("write: %v", err)
				}
			}
			link, err := Open(LinkOptions{Dir: dir})
			if config.name == "the switch written as a string" {
				// A malformed switch is a refusal, not a default: the
				// alternative is a daemon that decides for itself.
				if err == nil {
					t.Fatal("a switch that is not a boolean was accepted")
				}
				return
			}
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if link.Enabled() {
				t.Fatal("the line was on")
			}
			if got := link.Status(); got.Enabled || got.State != StateOff {
				t.Errorf("status said %+v", got)
			}
			if err := link.Run(context.Background()); !errors.Is(err, ErrDisabledLink) {
				t.Errorf("Run answered %v", err)
			}
			// Nothing was opened: no key was minted in a directory whose owner
			// never asked for a Cloud account.
			if _, err := os.Stat(filepath.Join(dir, "cloud")); !os.IsNotExist(err) {
				t.Errorf("a line that is off created a key store: %v", err)
			}
		})
	}
}

// A finite page burst needs no stuck handler, retry, or leaked slot to fill
// the lane. Keep the first eight ordinary routes open for a measured interval
// and send the rest from three synthetic viewers, with no retries.
func TestCloudReadBurstEvidence(t *testing.T) {
	var mu sync.Mutex
	var entered int
	var holds []time.Duration
	release := make(chan struct{})
	releaseReads := sync.OnceFunc(func() { close(release) })
	router := routerFunc(func(ctx context.Context, req cloudops.LocalRequest) (cloudops.LocalResponse, error) {
		started := time.Now()
		mu.Lock()
		entered++
		mu.Unlock()
		select {
		case <-release:
		case <-ctx.Done():
		}
		mu.Lock()
		holds = append(holds, time.Since(started))
		mu.Unlock()
		return cloudops.LocalResponse{Status: 200, Body: []byte(`{"places":[]}`)}, nil
	})
	l, fake := laneLink(t, router)
	stop, _ := serve(t, l)
	defer stop()
	defer releaseReads()
	ask := func(i int, name string) {
		l.relay.Deliver(domaincloud.Envelope{Ch: "ctl/mac-01", Class: "ctl", Sender: fmt.Sprintf("viewer-%d", i%3), Seq: uint64(i + 1)},
			plaintext(t, placesRead(name)), nil)
	}
	for i := 0; i < CloudReadConcurrencyLimit; i++ {
		ask(i, fmt.Sprintf("first-%d", i))
	}
	waitFor(t, "eight finite routes", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return entered == CloudReadConcurrencyLimit
	})
	for i := 0; i < 4; i++ {
		ask(100+i, fmt.Sprintf("burst-%d", i))
	}
	waitFor(t, "the entire burst to be sorted", func() bool {
		waiting, _, _ := l.relay.Queue()
		return waiting == 0
	})
	time.Sleep(20 * time.Millisecond)
	refused := 0
	for _, out := range fake.Published() {
		if out.Reply.Code == "cloud_ingress_busy" {
			refused++
		}
	}
	mu.Lock()
	t.Logf("burst=12 viewers=3 active=%d immediate_busy=%d retries=0 finite_hold_ms=20", entered, refused)
	mu.Unlock()
	if refused != 0 {
		t.Fatalf("a finite burst was refused before a worker could finish: busy=%d, want 0", refused)
	}
	releaseReads()
	waitFor(t, "all twelve finite reads to answer", func() bool { return len(fake.Published()) == 12 })
	waitFor(t, "all read slots to release", func() bool {
		l.mu.Lock()
		defer l.mu.Unlock()
		return l.reading == 0
	})
	mu.Lock()
	defer mu.Unlock()
	if entered != 12 || len(holds) != 12 {
		t.Fatalf("entered=%d completed=%d, want twelve each", entered, len(holds))
	}
	var longest time.Duration
	for _, held := range holds {
		longest = max(longest, held)
	}
	t.Logf("completed=%d longest_slot_ms=%d released_slots=8", len(holds), longest.Milliseconds())
}

func fillReadWorkers(t *testing.T, l *Link, router *gatedRouter) {
	t.Helper()
	for i := 0; i < CloudReadConcurrencyLimit; i++ {
		deliver(t, l, uint64(i+1), placesRead(fmt.Sprintf("active-%d", i)))
		waitFor(t, "each read worker to start", func() bool { return router.readsInside() == i+1 })
	}
}

func TestCloudReadQueueOverrideCanOnlyLowerItsBound(t *testing.T) {
	for _, tc := range []struct {
		name           string
		override, want int
	}{
		{"default", 0, 64}, {"lower", 2, 2}, {"cannot raise", 128, 64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, _ := laneLink(t, newGatedRouter())
			l.opts.ReadQueueDepth = tc.override
			l.opts.RefusalQueueDepth = tc.override
			serve(t, l)
			waitFor(t, "the read queue to be built", func() bool { l.mu.Lock(); defer l.mu.Unlock(); return l.readQueue != nil })
			l.mu.Lock()
			got := cap(l.readQueue)
			refusalDepth := cap(l.laneRefusals)
			l.mu.Unlock()
			if got != tc.want {
				t.Fatalf("read queue depth=%d, want %d", got, tc.want)
			}
			wantRefusal := CloudLaneRefusalsLimit
			if tc.override > 0 {
				wantRefusal = min(tc.override, CloudLaneRefusalsLimit)
			}
			if refusalDepth != wantRefusal {
				t.Fatalf("refusal depth=%d, want %d", refusalDepth, wantRefusal)
			}
		})
	}
}

type gatedBusyPublication struct {
	*Fake
	hold    <-chan struct{}
	started chan struct{}
	once    *sync.Once
}

func (g gatedBusyPublication) Publish(ctx context.Context, out Outbound) error {
	if out.Refusal {
		g.once.Do(func() { close(g.started) })
		select {
		case <-g.hold:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return g.Fake.Publish(ctx, out)
}

func TestCloudBusyPublicationIsBoundedAndDoesNotHoldCommands(t *testing.T) {
	router := newGatedRouter()
	router.holdRead = make(chan struct{})
	l, fake := laneLink(t, router)
	l.opts.ReadQueueDepth = 1
	l.opts.RefusalQueueDepth = 2
	started := make(chan struct{})
	l.service.Transport = gatedBusyPublication{Fake: fake, hold: make(chan struct{}), started: started, once: &sync.Once{}}
	serve(t, l)
	fillReadWorkers(t, l, router)
	deliver(t, l, 100, placesRead("waiting"))
	waitFor(t, "the read waiting queue to fill", func() bool { _, q := l.ReadLaneReadings(); return q.Used == 1 })
	deliver(t, l, 101, placesRead("busy-blocked"))
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("busy publication never started")
	}
	for i := 0; i < 2; i++ {
		deliver(t, l, uint64(102+i), placesRead(fmt.Sprintf("busy-queued-%d", i)))
		waitFor(t, "the bounded busy queue", func() bool { return l.LaneRefusalReading().Used == int64(i+1) })
	}
	deliver(t, l, 104, placesRead("busy-unanswered"))
	waitFor(t, "a counted unsent busy reply", func() bool { return l.LaneRefusalReading().Counters.Dropped == 1 })
	deliver(t, l, 105, sendCommand("passes"))
	close(router.gate("passes"))
	waitFor(t, "a command to pass blocked and overflowing busy publication", func() bool { _, ok := publishedNamed(fake, "action:a-passes"); return ok })
	if reading := l.LaneRefusalReading(); reading.Used != 2 || reading.Counters.Dropped != 1 {
		t.Fatalf("busy queue did not stay bounded: %+v", reading)
	}
}

func TestCloudIngressRefusalIsAnsweredAndNormalCloseDrainsIt(t *testing.T) {
	l, fake := laneLink(t, newGatedRouter())
	l.relay.Depth = 1
	// Fill ingress before the service starts, so the second request is put on
	// Relay.Refused rather than on any worker lane.
	deliver(t, l, 1, placesRead("taken"))
	deliver(t, l, 2, placesRead("ingress-overflow"))
	l.relay.Close()
	_, done := serve(t, l)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("closed ingress/refusal queues did not drain")
	}
	out, ok := publishedNamed(fake, "read:ingress-overflow")
	if !ok || out.Reply.Code != "cloud_ingress_busy" || out.Reply.Status != 429 || !out.Refusal {
		t.Fatalf("the ingress refusal was never answered by name: ok=%t reply=%+v", ok, out.Reply)
	}
	if _, ok := publishedNamed(fake, "read:taken"); !ok {
		t.Fatal("the accepted ingress read was lost")
	}
	if refused, unanswered := l.relay.Refusals(); refused != 1 || unanswered != 0 {
		t.Fatalf("ingress counters refused=%d unanswered=%d", refused, unanswered)
	}
}

func TestCloudReadQueueCapacityIsFiniteAndCommandsStillPass(t *testing.T) {
	router := newGatedRouter()
	hold := make(chan struct{})
	router.holdRead = hold
	l, fake := laneLink(t, router)
	serve(t, l)
	defer close(hold)
	fillReadWorkers(t, l, router)
	for i := 0; i < CloudReadQueueLimit; i++ {
		deliver(t, l, uint64(100+i), placesRead(fmt.Sprintf("queued-%d", i)))
		waitFor(t, "each read to enter the bounded waiting queue", func() bool {
			_, queued := l.ReadLaneReadings()
			return queued.Used == int64(i+1)
		})
	}
	deliver(t, l, 300, placesRead("overflow"))
	waitFor(t, "a typed refusal for queue overflow", func() bool { _, ok := publishedNamed(fake, "read:overflow"); return ok })
	out, _ := publishedNamed(fake, "read:overflow")
	var payload answerPayload
	if err := json.Unmarshal(out.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if out.Reply.Status != 429 || out.Reply.Code != "cloud_ingress_busy" || !out.Refusal ||
		payload.Error.Detail["limit"] != float64(CloudReadQueueLimit) || payload.Error.Detail["retry_after"] != float64(1) {
		t.Fatalf("overflow lost its typed, reserved reply: out=%+v detail=%v", out.Reply, payload.Error.Detail)
	}
	active, queued := l.ReadLaneReadings()
	if active.Used != 8 || queued.Used != 64 || queued.Counters.Refused != 1 {
		t.Fatalf("read measurements: active=%+v queued=%+v", active, queued)
	}
	deliver(t, l, 301, sendCommand("passes"))
	close(router.gate("passes"))
	waitFor(t, "a command to pass the completely full read queue", func() bool { _, ok := publishedNamed(fake, "action:a-passes"); return ok })
	if router.readsInside() != CloudReadConcurrencyLimit {
		t.Fatal("a queued or refused read ran beyond the worker bound")
	}
}

func TestCloudReadCancellationDiscardsQueuedRoutesAndReleasesSlots(t *testing.T) {
	router := newGatedRouter()
	router.holdRead = make(chan struct{})
	l, _ := laneLink(t, router)
	stop, _ := serve(t, l)
	fillReadWorkers(t, l, router)
	for i := 0; i < 4; i++ {
		deliver(t, l, uint64(100+i), placesRead(fmt.Sprintf("queued-%d", i)))
	}
	waitFor(t, "four waiting reads", func() bool { _, q := l.ReadLaneReadings(); return q.Used == 4 })
	stop()
	active, queued := l.ReadLaneReadings()
	if router.readsInside() != 8 || active.Used != 0 || queued.Used != 0 {
		t.Fatalf("cancellation ran queued work or retained slots: routes=%d active=%+v queue=%+v", router.readsInside(), active, queued)
	}
}

func TestCloudReadQueueDrainsOnNormalClose(t *testing.T) {
	router := newGatedRouter()
	hold := make(chan struct{})
	release := sync.OnceFunc(func() { close(hold) })
	router.holdRead = hold
	l, fake := laneLink(t, router)
	_, done := serve(t, l)
	defer release()
	fillReadWorkers(t, l, router)
	for i := 0; i < 4; i++ {
		deliver(t, l, uint64(100+i), placesRead(fmt.Sprintf("queued-%d", i)))
	}
	waitFor(t, "four waiting reads", func() bool { _, q := l.ReadLaneReadings(); return q.Used == 4 })
	l.relay.Close()
	release()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("normal close did not drain accepted reads")
	}
	active, queued := l.ReadLaneReadings()
	if len(fake.Published()) != 12 || router.readsInside() != 12 || active.Used != 0 || queued.Used != 0 {
		t.Fatalf("close lost accepted reads: answers=%d routes=%d active=%+v queue=%+v", len(fake.Published()), router.readsInside(), active, queued)
	}
}

func TestCloudReadFailuresReleaseWorkerSlots(t *testing.T) {
	for _, where := range []string{"handler", "publish"} {
		t.Run(where, func(t *testing.T) {
			var failing = true
			var mu sync.Mutex
			router := routerFunc(func(context.Context, cloudops.LocalRequest) (cloudops.LocalResponse, error) {
				mu.Lock()
				defer mu.Unlock()
				if failing && where == "handler" {
					return cloudops.LocalResponse{}, errors.New("injected route failure")
				}
				return cloudops.LocalResponse{Status: 200, Body: []byte(`{"places":[]}`)}, nil
			})
			l, fake := laneLink(t, router)
			if where == "publish" {
				fake.Fail = errors.New("injected publication failure")
			}
			serve(t, l)
			for i := 0; i < 12; i++ {
				deliver(t, l, uint64(i+1), placesRead(fmt.Sprintf("failure-%d", i)))
			}
			waitFor(t, "all failed reads to finish and release", func() bool { l.mu.Lock(); defer l.mu.Unlock(); return l.answered == 12 && l.reading == 0 })
			mu.Lock()
			failing = false
			mu.Unlock()
			fake.mu.Lock()
			fake.Fail = nil
			fake.mu.Unlock()
			deliver(t, l, 100, placesRead("recovered"))
			waitFor(t, "the next read to reuse a released slot", func() bool { out, ok := publishedNamed(fake, "read:recovered"); return ok && out.Reply.Status == 200 })
		})
	}
}

// A blocked publication holds the worker too. Observe that phase separately
// from a blocked route, and prove commands still take their independent lane.
type gatedReadPublication struct {
	*Fake
	hold <-chan struct{}
}

func (g gatedReadPublication) Publish(ctx context.Context, out Outbound) error {
	if strings.HasPrefix(out.Reply.Name, "read:") {
		select {
		case <-g.hold:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return g.Fake.Publish(ctx, out)
}

func TestCloudReadPressureObservesPublicationAndSlotRelease(t *testing.T) {
	router := newGatedRouter()
	l, fake := laneLink(t, router)
	hold := make(chan struct{})
	release := sync.OnceFunc(func() { close(hold) })
	l.service.Transport = gatedReadPublication{Fake: fake, hold: hold}
	var mu sync.Mutex
	var logs []string
	l.opts.Log = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, fmt.Sprintf(format, args...))
	}
	serve(t, l)
	defer release()
	fillReadWorkers(t, l, router)
	deliver(t, l, 100, placesRead("queued-private-request"))
	waitFor(t, "a pressure snapshot showing publication", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.Contains(strings.Join(logs, "\n"), "ctl/places:publish:")
	})
	deliver(t, l, 101, sendCommand("passes"))
	close(router.gate("passes"))
	waitFor(t, "a command to pass blocked read publication", func() bool { _, ok := publishedNamed(fake, "action:a-passes"); return ok })
	time.Sleep(20 * time.Millisecond)
	release()
	waitFor(t, "all publications to finish", func() bool {
		a, q := l.ReadLaneReadings()
		return a.Used == 0 && q.Used == 0 && len(fake.Published()) == 10
	})
	waitFor(t, "the queued read's own timing to be recorded", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.Count(strings.Join(logs, "\n"), "read slot released:") == 9
	})
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(logs, "\n")
	for _, want := range []string{"read workers occupied:", "waiting=1 active=8", "read slot released:", "handler_ms=", "publish_ms=", "slot_ms="} {
		if !strings.Contains(joined, want) {
			t.Errorf("pressure log omitted %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "queued-private-request") || strings.Contains(joined, "viewer-01") || strings.Contains(joined, "mac-01") {
		t.Fatalf("pressure log exposed request identity: %s", joined)
	}
	queuedTiming := false
	for _, field := range strings.Fields(joined) {
		if strings.HasPrefix(field, "queue_ms=") {
			var milliseconds int64
			_, _ = fmt.Sscanf(field, "queue_ms=%d", &milliseconds)
			queuedTiming = queuedTiming || milliseconds > 0
		}
	}
	if !queuedTiming {
		t.Fatal("the pressure log lost the waiting read's queue duration")
	}
	t.Log(joined)
}

// The real roster singleflight is a synchronous dependency shared by all
// read workers. One short local HTTP wait is sufficient to occupy eight slots;
// it need not be a slow route, a restart, or a never-returning request.
func TestCloudReadBurstAcrossSharedRosterWait(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprintf("roster_unavailable=%t", unavailable), func(t *testing.T) {
			public, _, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			hold := make(chan struct{})
			release := sync.OnceFunc(func() { close(hold) })
			var mu sync.Mutex
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requests++
				mu.Unlock()
				select {
				case <-hold:
				case <-r.Context().Done():
					return
				}
				if unavailable {
					w.WriteHeader(503)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"devices": []adaptercloud.RosterDevice{{ID: "viewer-01",
					PublicKey: base64.StdEncoding.EncodeToString(public), Caps: []string{"read_transcript"}}}})
			}))
			defer server.Close()
			defer release()
			router := newGatedRouter()
			l, fake := laneLink(t, router)
			l.opts.Now = time.Now
			l.roster = adaptercloud.NewRoster(server.URL, "test-credential", time.Now)
			l.service.Bridge.TranscriptAuthority = l.transcriptAuthority
			l.service.Bridge.AdmitExecution = func(context.Context, string, string, string) error { return nil }
			serve(t, l)
			ask := func(i int) {
				l.relay.Deliver(domaincloud.Envelope{Ch: "r/mac-01", Class: "ctl", Sender: "viewer-01", Seq: uint64(i + 1)},
					plaintext(t, map[string]any{"type": "info", "session": fmt.Sprintf("%%%d", i), "parts": "full",
						"machine_id": "mac-01", "expected_generation": "0123456789abcdef0123456789abcdef"}), public)
			}
			for i := 0; i < 8; i++ {
				ask(i)
			}
			waitFor(t, "eight workers waiting on shared roster authority", func() bool { a, _ := l.ReadLaneReadings(); return a.Used == 8 })
			time.Sleep(20 * time.Millisecond)
			for i := 8; i < 12; i++ {
				ask(i)
			}
			waitFor(t, "four reads waiting behind roster authority", func() bool { _, q := l.ReadLaneReadings(); return q.Used == 4 })
			mu.Lock()
			fetches := requests
			mu.Unlock()
			if fetches != 1 || router.readsInside() != 0 || len(fake.Published()) != 0 {
				t.Fatalf("shared dependency was not isolated: fetches=%d routes=%d answers=%d", fetches, router.readsInside(), len(fake.Published()))
			}
			release()
			waitFor(t, "all roster-dependent reads to finish", func() bool {
				a, q := l.ReadLaneReadings()
				return a.Used == 0 && q.Used == 0 && len(fake.Published()) == 12
			})
			for _, out := range fake.Published() {
				if unavailable {
					if out.Reply.Status != 503 || out.Reply.Code != "command_roster_unreadable" {
						t.Fatalf("lost typed authority failure: %+v", out.Reply)
					}
				} else if out.Reply.Status != 200 {
					t.Fatalf("finite roster wait refused a read: %+v", out.Reply)
				}
			}
			mu.Lock()
			fetches = requests
			mu.Unlock()
			if fetches != 1 {
				t.Fatalf("roster wait caused retry amplification: fetches=%d", fetches)
			}
			t.Logf("roster_unavailable=%t read_burst=12 roster_http_requests=%d active_peak=8 queued=4 final_active=0 ingress_busy=0", unavailable, fetches)
		})
	}
}

func TestALineThatIsOnWithNoIdentitySaysWhy(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"cloud_enabled":true,"cloud_api_base":"http://127.0.0.1:9","cloud_relay_url":"ws://127.0.0.1:9/v1/connect"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	link, err := Open(LinkOptions{Dir: dir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	status := link.Status()
	if !status.Enabled || status.Configured {
		t.Errorf("status said enabled=%v configured=%v", status.Enabled, status.Configured)
	}
	if status.LastError == "" {
		// "Not connected" with no reason is what sends a person to the log.
		t.Error("the status route could not say why the line is not up")
	}
	if err := link.Run(context.Background()); err == nil {
		t.Error("Run reported success for a machine that was never enrolled")
	}
}

func TestTheSettingsRefuseARelayURLThisBuildWillNotDial(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"cloud_enabled":true,"cloud_relay_url":"wss://relay.example.com/v1/somewhere-else"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// A path this build does not recognise is refused rather than rewritten,
	// and the refusal must reach the caller rather than falling back to the
	// production relay.
	if _, err := Open(LinkOptions{Dir: dir}); err == nil {
		t.Fatal("a relay URL with a path this build will not dial was accepted")
	}
}

func TestTheCloudRouterCarriesThisMachinesOwnCredentials(t *testing.T) {
	var seen *http.Request
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r
		w.WriteHeader(http.StatusNoContent)
	})
	router := Router{Handler: handler, Authorize: LocalAuthorizer("local-token", "machine-token")}

	if _, err := router.Do(context.Background(), cloudops.LocalRequest{Method: http.MethodGet, Path: "/v1/sessions"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := seen.Header.Get("Authorization"); got != "Bearer local-token" {
		t.Errorf("a session route was asked with %q", got)
	}
	if got := seen.Header.Get("X-Clawdline-Orchestrator"); got != "" {
		// A session route is not machine-scoped, and carrying the
		// orchestrator credential everywhere is how one door becomes two.
		t.Errorf("a session route carried the orchestrator token: %q", got)
	}

	if _, err := router.Do(context.Background(), cloudops.LocalRequest{Method: http.MethodGet, Path: "/v1/orchestrator/schedules"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := seen.Header.Get("X-Clawdline-Orchestrator"); got != "machine-token" {
		t.Errorf("a machine-scoped route was asked with %q", got)
	}
}

func TestTheStatusRouteShapeIsWhatTheSettingsPageReads(t *testing.T) {
	dir := t.TempDir()
	link, err := Open(LinkOptions{Dir: dir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	encoded, err := json.Marshal(link.Status())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The three the card cannot draw without, and the one that answers "why is
	// nothing happening" nine times out of ten.
	for _, key := range []string{"enabled", "commands", "configured", "state"} {
		if _, ok := body[key]; !ok {
			t.Errorf("the status route answered without %q: %v", key, body)
		}
	}
}

// A server standing in for the daemon, only to prove the route is reachable
// in-process rather than over a socket.
func TestTheRouterDispatchesInProcess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("the Cloud router opened a socket")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	answered := false
	router := Router{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		answered = true
		w.WriteHeader(http.StatusOK)
	})}
	if _, err := router.Do(context.Background(), cloudops.LocalRequest{Method: http.MethodGet, Path: "/v1/places"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if !answered {
		t.Error("the handler was not called")
	}
}
