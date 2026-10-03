package http

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/devices"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/app/terminals"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/terminal"
	"github.com/sainteye/clawdline/internal/transport/cloud"
)

type cloudTerminalGrantStub struct{ allowed bool }

func (s *cloudTerminalGrantStub) Status() cloud.Status { return cloud.Status{Enabled: true} }
func (s *cloudTerminalGrantStub) PinnedTerminalViewer(device string) (string, bool) {
	if s.allowed && device == "cloud-viewer" {
		return "Cloud viewer", true
	}
	return "", false
}
func (s *cloudTerminalGrantStub) TerminalViewerAllowed(device string) bool {
	return s.allowed && device == "cloud-viewer"
}

func TestCloudPinAndSendPermissionNeedNoLocalGrant(t *testing.T) {
	f := newTermFixture(t, newFakeTerms())
	stub := &cloudTerminalGrantStub{allowed: true}
	SetCloudLine(f.dir, stub)
	t.Cleanup(func() { SetCloudLine(f.dir, nil) })
	p := terminals.Principal{Device: "cloud-viewer", Cloud: true}
	svc, err := f.s.CloudTerminalService()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Allow(p); err != nil {
		t.Fatalf("pinned sender without a separate grant: %v", err)
	}
	router := cloud.Router{Handler: f.h, Authorize: cloud.LocalAuthorizer(f.local, f.machine)}
	if got, err := router.Do(context.Background(), cloudops.LocalRequest{Method: http.MethodPost,
		Path: "/v1/auth/devices/cloud-viewer/terminal", Body: []byte(`{"grant":false}`)}); err != nil ||
		got.Status != http.StatusForbidden || !strings.Contains(string(got.Body), "terminal_cloud_not_supported") {
		t.Fatalf("Cloud grant route: %+v, %v", got, err)
	}
	stub.allowed = false
	if err := svc.Allow(p); err == nil {
		t.Fatal("a revoked pin retained terminal access")
	}
}

func TestQueuedTerminalInputRechecksSendBeforeHostEffect(t *testing.T) {
	host := newFakeTerms()
	f := newTermFixture(t, host)
	term := f.open(f.local)
	device, token := f.device("phone", true)
	if _, rec := f.control(term.ID, token, "tab", "acquire"); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	svc, err := f.s.terminalService()
	if err != nil {
		t.Fatal(err)
	}
	host.mu.Lock()
	host.keysDelay = 300 * time.Millisecond
	host.mu.Unlock()
	first := make(chan string, 1)
	second := make(chan string, 1)
	go func() { first <- f.input(term.ID, token, "tab", 1, 1, "first").Body.String() }()
	deadline := time.Now().Add(time.Second)
	for svc.Lanes().Stats().Admitted < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	go func() { second <- f.input(term.ID, token, "tab", 1, 2, "second").Body.String() }()
	for svc.Lanes().Stats().Admitted < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if svc.Lanes().Stats().Admitted < 2 {
		t.Fatal("the second input never entered the terminal lane")
	}
	if _, err := f.s.gate().auth.SetCapabilities(device, auth.NewCaps(auth.Read)); err != nil {
		t.Fatalf("remove send: %v", err)
	}
	<-first
	if got := <-second; !strings.Contains(got, "terminal_forbidden") {
		t.Fatalf("queued input after revoke: %s", got)
	}
	for _, typed := range host.typedNow() {
		if typed == "second" {
			t.Fatal("revoked queued input reached the host")
		}
	}
}

func TestRevocationDuringInitialCaptureNeverSendsFrame(t *testing.T) {
	host := newFakeTerms()
	f := newTermFixture(t, host)
	term := f.open(f.local)
	device, _ := f.device("phone", true)
	svc, err := f.s.terminalService()
	if err != nil {
		t.Fatal(err)
	}
	host.frameEntered = make(chan struct{}, 1)
	host.frameRelease = make(chan struct{})
	type answer struct {
		watch *terminals.Watch
		err   error
	}
	ready := make(chan answer, 1)
	p := terminals.Principal{Device: device}
	go func() {
		w, err := svc.Watch(context.Background(), p, terminal.ID(term.ID), "tab")
		ready <- answer{w, err}
	}()
	select {
	case <-host.frameEntered:
	case <-time.After(time.Second):
		t.Fatal("capture did not begin")
	}
	if _, err := f.s.gate().auth.SetCapabilities(device, auth.NewCaps(auth.Read)); err != nil {
		t.Fatalf("remove send: %v", err)
	}
	close(host.frameRelease)
	a := <-ready
	if a.err != nil {
		t.Fatal(a.err)
	}
	defer a.watch.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var events []terminals.EventKind
	_ = a.watch.Run(ctx, func(e terminals.Event) error { events = append(events, e.Kind); return nil })
	for _, kind := range events {
		if kind == terminals.EventFrame {
			t.Fatalf("revoked first frame escaped: %v", events)
		}
	}
	if len(events) == 0 || events[0] != terminals.EventRefusal {
		t.Fatalf("revocation events: %v", events)
	}
}

// A paired sender inherits terminal access; pairing alone and remote_write do not.
func TestAReadOnlyDeviceIsForbiddenTerminals(t *testing.T) {
	f := newTermFixture(t, newFakeTerms())
	term := f.open(f.local)
	srv := f.server()
	if _, err := nextconfig.Open(f.dir).Set(map[string]any{"remote_write": true}); err != nil {
		t.Fatal(err)
	}
	_, reader := f.device("phone", false)

	if rec := f.do(http.MethodGet, "/v1/terminals", reader, ""); rec.Code != http.StatusForbidden || termCode(rec) != "terminal_forbidden" {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if _, status := f.stream(srv, term.ID, reader, "c1"); status != http.StatusForbidden {
		t.Fatalf("stream: %d", status)
	}
	if rec := f.input(term.ID, reader, "c1", 1, 1, "ls\r"); rec.Code != http.StatusForbidden || termCode(rec) != "terminal_forbidden" {
		t.Fatalf("input: %d %s", rec.Code, rec.Body)
	}
	if _, rec := f.control(term.ID, reader, "c1", "acquire"); termCode(rec) != "terminal_forbidden" {
		t.Fatalf("acquire: %d %s", rec.Code, rec.Body)
	}
	// A device may not give itself a grant: the grant route takes this
	// machine's own token.
	if rec := f.do(http.MethodPost, "/v1/auth/devices/x/terminal", reader, `{"grant":true}`); rec.Code != http.StatusForbidden {
		t.Fatalf("a device granting: %d %s", rec.Code, rec.Body)
	}

	// With the grant it may look, and is still read-only until it holds the
	// lease.
	_, granted := f.device("tablet", true)
	if rec := f.do(http.MethodGet, "/v1/terminals", granted, ""); rec.Code != http.StatusOK {
		t.Fatalf("a granted list: %d %s", rec.Code, rec.Body)
	}
	if rec := f.input(term.ID, granted, "c2", 1, 1, "ls\r"); termCode(rec) != "not_controller" {
		t.Fatalf("a granted input without the lease: %d %s", rec.Code, rec.Body)
	}
	var list contract.DeviceList
	_ = json.Unmarshal(f.do(http.MethodGet, "/v1/auth/devices", f.local, "").Body.Bytes(), &list)
	flags := map[string]bool{}
	for _, d := range list.Devices {
		flags[d.Name] = d.Terminal
	}
	if flags["phone"] || !flags["tablet"] {
		t.Fatalf("the device list's terminal flags: %v", flags)
	}
}

// Acceptance 2: one controller at a time, a takeover, and what the one taken
// over from is told.
func TestOneControllerAndATakeover(t *testing.T) {
	host := newFakeTerms()
	f := newTermFixture(t, host)
	term := f.open(f.local)
	srv := f.server()
	_, b := f.device("phone", true)

	a, rec := f.control(term.ID, f.local, "a", "acquire")
	if rec.Code != http.StatusOK || !a.Held || a.Epoch != 1 {
		t.Fatalf("A acquires: %d %s", rec.Code, rec.Body)
	}
	aStream, _ := f.stream(srv, term.ID, f.local, "a")
	aStream.until(t, 2*time.Second, "A's first frame", func(e sseEvent) bool { return e.name == "frame" })

	_, rec = f.control(term.ID, b, "b", "acquire")
	if rec.Code != http.StatusConflict || termCode(rec) != "terminal_controlled" {
		t.Fatalf("B acquires a held lease: %d %s", rec.Code, rec.Body)
	}
	var refused contract.TerminalRefusal
	_ = json.Unmarshal(rec.Body.Bytes(), &refused)
	if refused.Holder == nil || !refused.Holder.Local || refused.Holder.SameDevice {
		t.Fatalf("the holder B is told of: %+v", refused.Holder)
	}
	// Not the controller: neither a size nor a close.
	if rec := f.do(http.MethodPost, "/v1/terminals/"+term.ID+"/resize", b,
		`{"epoch":1,"client":"b","cols":100,"rows":30}`); termCode(rec) != "not_controller" {
		t.Fatalf("B resizes: %d %s", rec.Code, rec.Body)
	}
	if rec := f.do(http.MethodDelete, "/v1/terminals/"+term.ID, b,
		`{"epoch":1,"client":"b"}`); termCode(rec) != "not_controller" {
		t.Fatalf("B closes: %d %s", rec.Code, rec.Body)
	}
	if rec := f.input(term.ID, f.local, "a", 1, 1, "one"); rec.Code != http.StatusOK {
		t.Fatalf("A types: %d %s", rec.Code, rec.Body)
	}

	took, rec := f.control(term.ID, b, "b", "takeover")
	if rec.Code != http.StatusOK || took.Epoch != 2 || took.Holder == nil || !took.Holder.SameClient {
		t.Fatalf("B takes over: %d %s", rec.Code, rec.Body)
	}
	aStream.until(t, 2*time.Second, "A's control event naming B", func(e sseEvent) bool {
		var c contract.TerminalControl
		return e.name == "control" && json.Unmarshal([]byte(e.data), &c) == nil &&
			c.Epoch == 2 && c.Holder != nil && !c.Holder.SameDevice
	})
	if rec := f.input(term.ID, f.local, "a", 1, 2, "two"); rec.Code != http.StatusConflict || termCode(rec) != "lease_superseded" {
		t.Fatalf("A types after the takeover: %d %s", rec.Code, rec.Body)
	}
	if got := host.typedNow(); len(got) != 1 || got[0] != "one" {
		t.Fatalf("typed %q", got)
	}
}

// Acceptance 3: the lane, duplicates, gaps, and an answer tmux never gave.
func TestInputIsTypedOnceInOrderAndNeverGuessed(t *testing.T) {
	host := newFakeTerms()
	f := newTermFixture(t, host)
	term := f.open(f.local)
	if _, rec := f.control(term.ID, f.local, "a", "acquire"); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	host.mu.Lock()
	host.keysDelay = 200 * time.Millisecond
	host.mu.Unlock()

	var wg sync.WaitGroup
	answers := make([]string, 2)
	for i := range answers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := f.input(term.ID, f.local, "a", 1, 1, "echo once\r")
			answers[i] = rec.Body.String()
		}()
	}
	wg.Wait()
	if got := host.typedNow(); len(got) != 1 {
		t.Fatalf("the same seq twice at once typed %q; answers %q", got, answers)
	}
	if !strings.Contains(answers[0]+answers[1], `"duplicate":true`) {
		t.Fatalf("neither answer was the duplicate: %q", answers)
	}

	rec := f.input(term.ID, f.local, "a", 1, 3, "skipped")
	var gap contract.TerminalRefusal
	_ = json.Unmarshal(rec.Body.Bytes(), &gap)
	if rec.Code != http.StatusConflict || gap.Error != "input_gap" || gap.AppliedThrough != 1 {
		t.Fatalf("a gap: %d %s", rec.Code, rec.Body)
	}

	svc, err := f.s.terminalService()
	if err != nil {
		t.Fatal(err)
	}
	svc.InputTimeout = 150 * time.Millisecond
	host.mu.Lock()
	host.keysDelay, host.keysHang = 0, true
	host.mu.Unlock()
	if rec := f.input(term.ID, f.local, "a", 1, 2, "lost"); termCode(rec) != "input_state_unknown" {
		t.Fatalf("a tmux that never answered: %d %s", rec.Code, rec.Body)
	}
	if control := svc.Control(terminal.ID(term.ID)); !control.Unknown || control.Applied != 1 {
		t.Fatalf("unknown host effect must not look like known high water: %+v", control)
	}
	for _, seq := range []int64{2, 3} {
		if rec := f.input(term.ID, f.local, "a", 1, seq, "again"); termCode(rec) != "input_state_unknown" {
			t.Fatalf("seq %d after the unknown: %d %s", seq, rec.Code, rec.Body)
		}
	}
	if got := host.typedNow(); len(got) != 1 {
		t.Fatalf("something was typed again: %q", got)
	}
	// A new acquire is a clean slate.
	c, _ := f.control(term.ID, f.local, "a", "acquire")
	if rec := f.input(term.ID, f.local, "a", c.Epoch, 1, "fresh"); rec.Code != http.StatusOK {
		t.Fatalf("after a new acquire: %d %s", rec.Code, rec.Body)
	}
}

// A device revoked, or its send permission removed, loses its stream
// within a second and cannot type.
func TestRevokingEndsTheStreamWithinASecond(t *testing.T) {
	f := newTermFixture(t, newFakeTerms())
	term := f.open(f.local)
	srv := f.server()

	for _, how := range []string{"revoke the device", "remove send permission"} {
		t.Run(how, func(t *testing.T) {
			id, token := f.device("phone-"+strings.ReplaceAll(how, " ", "-"), true)
			c, rec := f.control(term.ID, token, "p", "takeover")
			if rec.Code != http.StatusOK {
				t.Fatalf("takeover: %d %s", rec.Code, rec.Body)
			}
			stream, _ := f.stream(srv, term.ID, token, "p")
			stream.until(t, 2*time.Second, "the first frame", func(e sseEvent) bool { return e.name == "frame" })

			began := time.Now()
			want, wantCode := http.StatusUnauthorized, ""
			if how == "revoke the device" {
				if rec := f.do(http.MethodPost, "/v1/auth/devices/"+id+"/revoke", f.local, ""); rec.Code != http.StatusOK {
					t.Fatalf("revoke: %d %s", rec.Code, rec.Body)
				}
			} else {
				if _, err := f.s.gate().auth.SetCapabilities(id, auth.NewCaps(auth.Read)); err != nil {
					t.Fatal(err)
				}
				want, wantCode = http.StatusForbidden, "terminal_forbidden"
			}
			ended, seen := stream.ended(t, time.Second)
			took := ended.Sub(began)
			if len(seen) == 0 || !strings.Contains(seen[len(seen)-1].data, "terminal_access_revoked") {
				t.Fatalf("the stream's last events: %v", seen)
			}
			t.Logf("%s: the stream closed %v after the change", how, took.Round(time.Millisecond))
			rec = f.input(term.ID, token, "p", c.Epoch, 1, "after")
			if rec.Code != want || (wantCode != "" && termCode(rec) != wantCode) {
				t.Fatalf("the next input: %d %s", rec.Code, rec.Body)
			}
			if ctl, _ := f.control(term.ID, f.local, "a", "acquire"); !ctl.Held {
				t.Fatal("the lease was not given up")
			}
		})
	}
}

// Acceptance 6: through Clawdline Cloud, with no app origin at all, every
// terminal route and the grant route are refused by name.
func TestCloudNeverReachesATerminal(t *testing.T) {
	host := newFakeTerms()
	f := newTermFixture(t, host)
	term := f.open(f.local)
	id, _ := f.device("phone", false)
	router := cloud.Router{Handler: f.h, Authorize: cloud.LocalAuthorizer(f.local, f.machine)}
	asks := []cloudops.LocalRequest{
		{Method: http.MethodGet, Path: "/v1/terminals"},
		{Method: http.MethodPost, Path: "/v1/terminals", Body: []byte(`{"project_id":"prj_test","cols":80,"rows":24}`)},
		{Method: http.MethodGet, Path: "/v1/terminals/" + term.ID},
		{Method: http.MethodGet, Path: "/v1/terminals/" + term.ID + "/stream"},
		{Method: http.MethodGet, Path: "/v1/terminals/" + term.ID + "/history"},
		{Method: http.MethodPost, Path: "/v1/terminals/" + term.ID + "/control", Body: []byte(`{"action":"acquire","client":"c"}`)},
		{Method: http.MethodPost, Path: "/v1/terminals/" + term.ID + "/input", Body: []byte(`{"epoch":1,"client":"c","seq":1,"data":"bHMN"}`)},
		{Method: http.MethodPost, Path: "/v1/terminals/" + term.ID + "/paste", Body: []byte(`{"epoch":1,"client":"c","seq":1,"text":"ls"}`)},
		{Method: http.MethodPost, Path: "/v1/terminals/" + term.ID + "/resize", Body: []byte(`{"epoch":1,"client":"c","cols":9,"rows":9}`)},
		{Method: http.MethodDelete, Path: "/v1/terminals/" + term.ID, Body: []byte(`{"epoch":1,"client":"c"}`)},
		{Method: http.MethodPost, Path: "/v1/auth/devices/" + id + "/terminal", Body: []byte(`{"grant":true}`)},
	}
	for _, ask := range asks {
		got, err := router.Do(context.Background(), ask)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != http.StatusForbidden || !strings.Contains(string(got.Body), "terminal_cloud_not_supported") {
			t.Errorf("%s %s through Cloud: %d %s", ask.Method, ask.Path, got.Status, got.Body)
		}
	}
	if typed := host.typedNow(); len(typed) != 0 {
		t.Fatalf("typed through Cloud: %q", typed)
	}
	if ok, _ := f.s.gate().grants.Granted(id); ok {
		t.Fatal("a grant was given through Cloud")
	}
}

// Legacy grant corruption is reported but cannot remove a sender's access.
func TestACorruptGrantsFileDoesNotChangeSendAuthority(t *testing.T) {
	f := newTermFixture(t, newFakeTerms())
	_, token := f.device("phone", true)
	if rec := f.do(http.MethodGet, "/v1/terminals", token, ""); rec.Code != http.StatusOK {
		t.Fatalf("before: %d %s", rec.Code, rec.Body)
	}
	path := filepath.Join(f.s.gate().files.Dir(), devices.GrantsFile)
	if err := os.WriteFile(path, []byte(`{"not":"a grant"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if rec := f.do(http.MethodGet, "/v1/terminals", token, ""); rec.Code != http.StatusOK {
		t.Fatalf("a paired sender with the old file broken: %d %s", rec.Code, rec.Body)
	}
	if rec := f.do(http.MethodGet, "/v1/terminals", f.local, ""); rec.Code != http.StatusOK {
		t.Fatalf("this machine's own token with the file broken: %d %s", rec.Code, rec.Body)
	}
	if rec := f.do(http.MethodGet, "/v1/sessions", token, ""); rec.Code != http.StatusOK {
		t.Fatalf("/v1/sessions with the file broken: %d %s", rec.Code, rec.Body)
	}
	var diag contract.Diagnostics
	_ = json.Unmarshal(f.do(http.MethodGet, "/v1/diagnostics", f.local, "").Body.Bytes(), &diag)
	if diag.Terminals == nil || diag.Terminals.GrantsOK || !strings.Contains(diag.Terminals.GrantsError, devices.GrantsFile) {
		t.Fatalf("diagnostics: %+v", diag.Terminals)
	}
	t.Logf("diagnostics says: %s", diag.Terminals.GrantsError)
	// And a grant is not written over what could not be read.
	if rec := f.do(http.MethodPost, "/v1/auth/devices/x/terminal", f.local, `{"grant":true}`); rec.Code == http.StatusOK {
		t.Fatalf("a grant over a broken file: %d %s", rec.Code, rec.Body)
	}
}

// Acceptance 8: terminals wait in lanes of their own, so a full terminal lane
// never holds up an Agent Session.
func TestAFullTerminalLaneLeavesAgentSessionsAlone(t *testing.T) {
	f := newTermFixture(t, newFakeTerms())
	term := f.open(f.local)
	c, _ := f.control(term.ID, f.local, "a", "acquire")
	svc, err := f.s.terminalService()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < terminals.LaneLimit; i++ {
		release, err := svc.Lanes().Acquire(context.Background(), "filler-"+string(rune('a'+i)))
		if err != nil {
			t.Fatalf("filling slot %d: %v", i, err)
		}
		defer release()
	}
	if rec := f.input(term.ID, f.local, "a", c.Epoch, 1, "x"); rec.Code != http.StatusTooManyRequests || termCode(rec) != "terminal_busy" {
		t.Fatalf("a terminal input with its lane full: %d %s", rec.Code, rec.Body)
	}
	rec := f.do(http.MethodPost, "/v1/sessions/%254/send", f.local, `{"text":"hello"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("an Agent Session send with the terminal lane full: %d %s", rec.Code, rec.Body)
	}
}

// The viewer bounds: a ninth stream of one terminal is refused by name, and a
// stream that ends gives its place back.
func TestAFullTerminalRefusesTheNextViewer(t *testing.T) {
	f := newTermFixture(t, newFakeTerms())
	term := f.open(f.local)
	srv := f.server()
	var open []*sse
	for i := 0; i < terminals.MaxViewers; i++ {
		s, status := f.stream(srv, term.ID, f.local, "c"+string(rune('a'+i)))
		if status != http.StatusOK {
			t.Fatalf("viewer %d: %d", i+1, status)
		}
		s.until(t, 2*time.Second, "a first frame", func(e sseEvent) bool { return e.name == "frame" })
		open = append(open, s)
	}
	if _, status := f.stream(srv, term.ID, f.local, "extra"); status != http.StatusTooManyRequests {
		t.Fatalf("a ninth viewer: %d", status)
	}
	open[0].cancel()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s, status := f.stream(srv, term.ID, f.local, "later")
		if status == http.StatusOK {
			s.cancel()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a closed stream never gave its place back")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
