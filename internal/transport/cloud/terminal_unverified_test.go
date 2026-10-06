package cloud

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/app/terminals"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

// unverifiedFixture is a Link whose terminal authority comes from a real
// roster read against a server the test can make fail, on a clock the test
// moves, with the terminal service asking the Link exactly as the daemon does.
type unverifiedFixture struct {
	t      *testing.T
	l      *Link
	svc    *terminals.Service
	spool  *adaptercloud.Spool
	signer domaincloud.DeviceKey
	key    domaincloud.ContentKey

	mu      sync.Mutex
	now     time.Time
	fail    bool
	row     adaptercloud.RosterDevice
	reads   int
	logs    []string
	retired int
}

func newUnverifiedFixture(t *testing.T) *unverifiedFixture {
	t.Helper()
	f := &unverifiedFixture{t: t, now: time.Now()}
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f.row = adaptercloud.RosterDevice{ID: "viewer", PublicKey: base64.StdEncoding.EncodeToString(pub), Caps: []string{TerminalCapability}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.reads++
		fail, row := f.fail, f.row
		f.mu.Unlock()
		if fail {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": []adaptercloud.RosterDevice{row}})
	}))
	t.Cleanup(server.Close)
	pins := adaptercloud.NewPinnedStore(t.TempDir())
	if err := pins.Pin("account", adaptercloud.PinnedDevice{DeviceID: "viewer", PublicKey: f.row.PublicKey}); err != nil {
		t.Fatal(err)
	}
	file := nextconfig.Open(t.TempDir())
	if _, err := file.Set(map[string]any{adaptercloud.KeyEnabled: true, adaptercloud.KeyCommands: true}); err != nil {
		t.Fatal(err)
	}
	f.spool, err = adaptercloud.NewSpool(adaptercloud.DefaultSpoolLimits(), nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	f.signer, err = domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	master, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f.key, err = domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now }
	f.l = &Link{file: file, settings: adaptercloud.Settings{Enabled: true}, pinned: pins,
		roster: adaptercloud.NewRoster(server.URL, "credential", clock), identity: adaptercloud.Identity{MachineID: "machine"},
		machineIncarnation: "incarnation"}
	f.svc = terminals.New(nil, func(p terminals.Principal) error { return f.l.TerminalViewerAccess(p.Device) })
	f.l.opts = LinkOptions{Now: clock, TerminalService: func() (*terminals.Service, error) { return f.svc, nil },
		Log: func(format string, args ...any) {
			f.mu.Lock()
			f.logs = append(f.logs, fmt.Sprintf(format, args...))
			f.mu.Unlock()
		}}
	f.l.relay = &Relay{Transport: &adaptercloud.Transport{}, Spool: f.spool, MachineID: "machine", Signer: f.signer,
		Secret: master, KeyID: adaptercloud.MasterKeyID}
	f.l.terminalMetadata = func(_ context.Context, action string, _ *terminalConnection) error {
		if action == "retire" {
			f.mu.Lock()
			f.retired++
			f.mu.Unlock()
		}
		return nil
	}
	return f
}

func (f *unverifiedFixture) set(fn func(f *unverifiedFixture)) {
	f.mu.Lock()
	fn(f)
	f.mu.Unlock()
}

func (f *unverifiedFixture) advance(d time.Duration) {
	f.set(func(f *unverifiedFixture) { f.now = f.now.Add(d) })
}

// register puts a live, confirmed connection for viewer in place.
func (f *unverifiedFixture) register() *terminalConnection {
	c := &terminalConnection{viewer: "viewer", id: testConnection, keyID: testKeyID, key: f.key,
		expires: f.now.Add(time.Minute), receipts: map[string][]byte{}}
	f.l.terminalConnections = map[string]*terminalConnection{terminalConnectionID("viewer", testConnection): c}
	return c
}

// published is every payload the machine queued for the relay, opened.
func (f *unverifiedFixture) published() []map[string]any {
	f.t.Helper()
	var out []map[string]any
	for seq := uint64(0); ; seq++ {
		row, ok := f.spool.Row(seq)
		if !ok {
			return out
		}
		env, err := domaincloud.DecodeEnvelope(row.Sealed)
		if err != nil {
			f.t.Fatal(err)
		}
		plain, err := env.Open(f.key, func(string) (ed25519.PublicKey, bool) { return f.signer.PublicKey(), true })
		if err != nil {
			f.t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(plain, &got); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, got)
	}
}

func (f *unverifiedFixture) request(operation string) Inbound {
	return Inbound{Channel: "termi/machine/viewer", Class: string(domaincloud.ClassCtl), Sender: "viewer",
		Plaintext: []byte(`{"v":1,"type":"terminal_request","request_id":"` + testRequestID + `","connection":"` +
			testConnection + `","operation":"` + operation + `"}`)}
}

func TestUnreadableRosterIsUnverifiedAndAFreshNoIsADenial(t *testing.T) {
	f := newUnverifiedFixture(t)
	if err := f.l.TerminalViewerAccess("viewer"); err != nil {
		t.Fatalf("a pinned viewer with send_prompt was refused: %v", err)
	}
	f.set(func(f *unverifiedFixture) { f.fail = true })
	f.advance(CloudTerminalRosterRefreshLimit * time.Second)
	if code, _ := terminal.CodeOf(f.l.TerminalViewerAccess("viewer")); code != terminal.CodeBusy {
		t.Fatalf("an unreadable roster answered %q, want %q", code, terminal.CodeBusy)
	}
	// The failed read is not held against the machine for a whole refresh
	// interval: the next check after the retry gap reads again and recovers.
	f.set(func(f *unverifiedFixture) { f.fail = false })
	f.advance(CloudTerminalRosterRetrySecondsLimit * time.Second)
	if err := f.l.TerminalViewerAccess("viewer"); err != nil {
		t.Fatalf("a recovered roster still refused: %v", err)
	}
	f.set(func(f *unverifiedFixture) { f.row.Caps = []string{"read_sessions"} })
	f.advance(CloudTerminalRosterRefreshLimit * time.Second)
	if code, _ := terminal.CodeOf(f.l.TerminalViewerAccess("viewer")); code != terminal.CodeForbidden {
		t.Fatalf("a fresh roster without send_prompt answered %q, want %q", code, terminal.CodeForbidden)
	}
	revoked := "2026-10-06T00:00:00Z"
	f.set(func(f *unverifiedFixture) { f.row.Caps = []string{TerminalCapability}; f.row.RevokedAt = &revoked })
	f.advance(CloudTerminalRosterRefreshLimit * time.Second)
	if code, _ := terminal.CodeOf(f.l.TerminalViewerAccess("viewer")); code != terminal.CodeForbidden {
		t.Fatalf("a fresh roster revocation answered %q, want %q", code, terminal.CodeForbidden)
	}
	// A local revocation is a fact this machine holds; an unreadable roster
	// cannot turn it into "try again".
	f.set(func(f *unverifiedFixture) { f.row.RevokedAt = nil; f.fail = true })
	f.advance(CloudTerminalRosterRefreshLimit * time.Second)
	if _, err := f.l.pinned.Revoke("viewer", time.Now()); err != nil {
		t.Fatal(err)
	}
	if code, _ := terminal.CodeOf(f.l.TerminalViewerAccess("viewer")); code != terminal.CodeForbidden {
		t.Fatalf("a locally revoked viewer answered %q, want %q", code, terminal.CodeForbidden)
	}
}

func TestUnreadableRosterRefusesARequestAsBusyNotForbidden(t *testing.T) {
	f := newUnverifiedFixture(t)
	c := f.register()
	f.set(func(f *unverifiedFixture) { f.fail = true })
	f.l.handleTerminal(context.Background(), f.request("list"))
	got := f.published()
	if len(got) != 1 || got[0]["type"] != "terminal_receipt" || got[0]["error"] != string(terminal.CodeBusy) {
		t.Fatalf("request during an unreadable roster: %v", got)
	}
	f.l.terminalMu.Lock()
	denied := c.denied
	f.l.terminalMu.Unlock()
	if denied || f.l.getTerminalConnection("viewer", testConnection) != c {
		t.Fatal("an unverified request closed the connection as if it were revoked")
	}
}

func TestUnreadableRosterAnswersAnOpenWithARetryableReceipt(t *testing.T) {
	f := newUnverifiedFixture(t)
	f.set(func(f *unverifiedFixture) { f.fail = true })
	f.l.handleTerminal(context.Background(), Inbound{Channel: "termi/machine/viewer", Class: string(domaincloud.ClassCtl),
		Sender: "viewer", Plaintext: []byte(`{"v":1,"type":"terminal_request","request_id":"` + testRequestID +
			`","connection":"` + testConnection + `","operation":"open_connection","key_id":"` + testKeyID +
			`","key":"` + base64.StdEncoding.EncodeToString(f.key.Bytes()) + `"}`)})
	got := f.published()
	if len(got) != 1 || got[0]["type"] != "terminal_receipt" || got[0]["status"] != "refused" ||
		got[0]["error"] != string(terminal.CodeBusy) {
		t.Fatalf("open during an unreadable roster: %v", got)
	}
	// A fresh roster that says no still gets no answer: the sender is not
	// someone this machine will encrypt to.
	g := newUnverifiedFixture(t)
	g.set(func(f *unverifiedFixture) { f.row.Caps = []string{"read_sessions"} })
	g.l.handleTerminal(context.Background(), Inbound{Channel: "termi/machine/viewer", Class: string(domaincloud.ClassCtl),
		Sender: "viewer", Plaintext: []byte(`{"v":1,"type":"terminal_request","request_id":"` + testRequestID +
			`","connection":"` + testConnection + `","operation":"open_connection","key_id":"` + testKeyID +
			`","key":"` + base64.StdEncoding.EncodeToString(g.key.Bytes()) + `"}`)})
	if got := g.published(); len(got) != 0 {
		t.Fatalf("a viewer without send_prompt was answered: %v", got)
	}
}

func TestSweepPausesAnUnverifiedConnectionAndRetiresItWithoutRevoking(t *testing.T) {
	f := newUnverifiedFixture(t)
	c := f.register()
	f.set(func(f *unverifiedFixture) { f.fail = true })
	f.l.sweepTerminalConnections()
	if got := f.published(); len(got) != 0 {
		t.Fatalf("an unreadable roster sent the viewer %v", got)
	}
	f.l.terminalMu.Lock()
	denied := c.denied
	f.l.terminalMu.Unlock()
	if denied || f.l.getTerminalConnection("viewer", testConnection) != c {
		t.Fatal("the sweep treated an unreadable roster as a revocation")
	}
	// Frames stop while it cannot be verified, and are not a revocation either.
	if err := f.l.sendTerminalFrame(context.Background(), f.svc, terminals.Principal{Device: "viewer", Cloud: true},
		c, terminal.NewID(), terminal.Frame{Rev: "r", Cols: 1, Rows: 1, Lines: []string{"x"}}); err != terminals.ErrFrameDeferred {
		t.Fatalf("a frame during an unreadable roster: %v", err)
	}
	if got := f.published(); len(got) != 0 {
		t.Fatalf("an unverified frame published %v", got)
	}
	// Recovery inside the bound resumes it.
	f.set(func(f *unverifiedFixture) { f.fail = false })
	f.advance(CloudTerminalUnverifiedRetireSecondsLimit*time.Second - time.Second)
	f.l.sweepTerminalConnections()
	f.set(func(f *unverifiedFixture) { f.fail = true })
	f.advance(CloudTerminalRosterRefreshLimit * time.Second)
	f.l.sweepTerminalConnections()
	f.advance(CloudTerminalUnverifiedRetireSecondsLimit*time.Second - time.Second)
	f.l.sweepTerminalConnections()
	if f.l.getTerminalConnection("viewer", testConnection) != c {
		t.Fatal("the unverified bound was not restarted by a verified sweep")
	}
	f.advance(time.Second)
	f.l.sweepTerminalConnections()
	if f.l.getTerminalConnection("viewer", testConnection) != nil {
		t.Fatal("a connection stayed registered past the unverified bound")
	}
	if got := f.published(); len(got) != 0 {
		t.Fatalf("retiring an unverified connection told the viewer %v", got)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		retired := f.retired
		f.mu.Unlock()
		if retired > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the relay registration was not retired")
}

func TestSweepRevokesOnAFreshRosterThatSaysNo(t *testing.T) {
	f := newUnverifiedFixture(t)
	c := f.register()
	f.set(func(f *unverifiedFixture) { f.row.Caps = []string{"read_sessions"} })
	f.l.sweepTerminalConnections()
	got := f.published()
	if len(got) != 1 || got[0]["type"] != "terminal_notice" || got[0]["code"] != "terminal_access_revoked" {
		t.Fatalf("a fresh roster without send_prompt: %v", got)
	}
	f.l.terminalMu.Lock()
	denied := c.denied
	f.l.terminalMu.Unlock()
	if !denied {
		t.Fatal("input stayed open after a fresh denial")
	}
	f.l.handleTerminal(context.Background(), f.request("list"))
	if got := f.published(); len(got) != 2 || got[1]["error"] != string(terminal.CodeForbidden) {
		t.Fatalf("a request after a fresh denial: %v", got)
	}
}

func TestRosterFailureIsLoggedOncePerStreak(t *testing.T) {
	f := newUnverifiedFixture(t)
	f.set(func(f *unverifiedFixture) { f.fail = true })
	for i := 0; i < 3; i++ {
		_ = f.l.TerminalViewerAccess("viewer")
		f.advance(CloudTerminalRosterRefreshLimit * time.Second)
	}
	count := func() int {
		f.mu.Lock()
		defer f.mu.Unlock()
		n := 0
		for _, line := range f.logs {
			if strings.Contains(line, "terminal") && strings.Contains(line, "roster") && strings.Contains(line, "503") {
				n++
			}
		}
		return n
	}
	if n := count(); n != 1 {
		f.mu.Lock()
		t.Fatalf("three failed reads logged %d times, want once: %q", n, f.logs)
	}
	f.set(func(f *unverifiedFixture) { f.fail = false })
	_ = f.l.TerminalViewerAccess("viewer")
	f.set(func(f *unverifiedFixture) { f.fail = true })
	f.advance(CloudTerminalRosterRefreshLimit * time.Second)
	_ = f.l.TerminalViewerAccess("viewer")
	if n := count(); n != 2 {
		f.mu.Lock()
		t.Fatalf("a second failure streak logged %d lines in all, want 2: %q", n, f.logs)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, line := range f.logs {
		if strings.Contains(line, "credential") {
			t.Fatalf("the log line carried the machine credential: %q", line)
		}
	}
}
