package cloud

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

const testConnection = "AAAAAAAAAAAAAAAAAAAAAA"
const testKeyID = "rk-AQEBAQEBAQEBAQEBAQEBAQ"

var testRequestID = strings.Join([]string{"12345678", "1234", "4123", "8123", "123456789abc"}, "-")

func TestTerminalRequestRejectsForgedShapeAndKey(t *testing.T) {
	base := `{"v":1,"type":"terminal_request","request_id":"` + testRequestID + `","connection":"` + testConnection + `","operation":"open_connection"}`
	if _, err := decodeTerminalRequest([]byte(base)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		base + ` {}`,
		`{"v":1,"type":"terminal_request","request_id":"` + testRequestID + `","connection":"` + testConnection + `","operation":"open_connection","sender":"another-viewer"}`,
		`{"v":1,"type":"terminal_request","request_id":"bad","connection":"` + testConnection + `","operation":"open_connection"}`,
		`{"v":1,"type":"terminal_request","request_id":"` + testRequestID + `","connection":"not-random","operation":"open_connection"}`,
	} {
		if _, err := decodeTerminalRequest([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if _, err := parseConnectionKey(terminalRequest{KeyID: testKeyID, Key: base64.StdEncoding.EncodeToString(key)}); err != nil {
		t.Fatalf("independent connection and key ids: %v", err)
	}
	if _, err := parseConnectionKey(terminalRequest{KeyID: testKeyID, Key: base64.StdEncoding.EncodeToString(key[:31])}); err == nil {
		t.Fatal("short key was accepted")
	}
}

func TestTerminalAdmissionNeedsExactPinRosterCapabilityAndFreshRead(t *testing.T) {
	dir := t.TempDir()
	file := nextconfig.Open(dir)
	if _, err := file.Set(map[string]any{adaptercloud.KeyEnabled: true, adaptercloud.KeyCommands: true}); err != nil {
		t.Fatal(err)
	}
	pinned := adaptercloud.NewPinnedStore(filepath.Join(dir, "pins"))
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := pinned.Pin("account", adaptercloud.PinnedDevice{DeviceID: "viewer", PublicKey: base64.StdEncoding.EncodeToString(pub)}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	row := adaptercloud.RosterDevice{ID: "viewer", PublicKey: base64.StdEncoding.EncodeToString(pub), Caps: []string{TerminalCapability}}
	failed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		current, fail := row, failed
		mu.Unlock()
		if fail {
			http.Error(w, "unavailable", http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": []adaptercloud.RosterDevice{current}})
	}))
	defer server.Close()
	l := &Link{opts: LinkOptions{Now: time.Now}, file: file,
		settings: adaptercloud.Settings{Enabled: true}, pinned: pinned,
		roster: adaptercloud.NewRoster(server.URL, "credential", time.Now)}
	if !l.TerminalViewerAllowed("viewer") {
		t.Fatal("exact pin and terminal_control were refused")
	}
	if l.TerminalViewerAllowed("another-viewer") {
		t.Fatal("another viewer inherited the authorized viewer connection")
	}
	mu.Lock()
	row.Caps = []string{SendCapability}
	mu.Unlock()
	l.terminalRosterAt = time.Time{}
	if l.TerminalViewerAllowed("viewer") {
		t.Fatal("send_prompt gained terminal access")
	}
	mu.Lock()
	row.Caps = []string{TerminalCapability}
	row.PublicKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
	mu.Unlock()
	l.terminalRosterAt = time.Time{}
	if l.TerminalViewerAllowed("viewer") {
		t.Fatal("roster key substitution beat the local pin")
	}
	mu.Lock()
	failed = true
	mu.Unlock()
	l.terminalRosterAt = time.Time{}
	if l.TerminalViewerAllowed("viewer") {
		t.Fatal("failed roster refresh kept terminal authority")
	}
}

func TestTerminalReceiptUsesOnlyViewerConnectionKey(t *testing.T) {
	spool, err := adaptercloud.NewSpool(adaptercloud.DefaultSpoolLimits(), nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	master, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	connectionKey, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	r := &Relay{Transport: &adaptercloud.Transport{}, Spool: spool, MachineID: "machine", Signer: signer,
		Secret: master, KeyID: adaptercloud.MasterKeyID}
	payload := []byte(`{"v":1,"type":"terminal_receipt","status":"ok"}`)
	if err := r.Publish(context.Background(), Outbound{Channel: "termr/machine/viewer/" + testConnection,
		Class: string(domaincloud.ClassCtl), Payload: payload, Key: connectionKey, KeyID: testKeyID}); err != nil {
		t.Fatal(err)
	}
	disposition := spool.SendNext()
	if disposition.Row == nil {
		t.Fatalf("no sealed receipt: %+v", disposition)
	}
	envelope, err := domaincloud.DecodeEnvelope(disposition.Row.Sealed)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.KeyID != testKeyID {
		t.Fatalf("key_id: %s", envelope.KeyID)
	}
	pin := func(string) (ed25519.PublicKey, bool) { return signer.PublicKey(), true }
	plain, err := envelope.Open(connectionKey, pin)
	if err != nil || string(plain) != string(payload) {
		t.Fatalf("connection decrypt: %s %v", plain, err)
	}
	if _, err := envelope.Open(master, pin); err == nil {
		t.Fatal("account master key opened private receipt")
	}
	otherKey, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := envelope.Open(otherKey, pin); err == nil {
		t.Fatal("another viewer connection key opened private receipt")
	}
}

func TestLocalGrantRefusalRegistersBeforeReceiptAndRetiresAfterSettlement(t *testing.T) {
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		t.Fatal(err)
	}
	connectionKey, err := domaincloud.ContentKeyFromBytes(keyBytes)
	if err != nil {
		t.Fatal(err)
	}
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pins := adaptercloud.NewPinnedStore(t.TempDir())
	if err := pins.Pin("account", adaptercloud.PinnedDevice{DeviceID: "viewer", PublicKey: base64.StdEncoding.EncodeToString(pub)}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": []adaptercloud.RosterDevice{{ID: "viewer", PublicKey: base64.StdEncoding.EncodeToString(pub), Caps: []string{TerminalCapability}}}})
	}))
	defer server.Close()
	file := nextconfig.Open(t.TempDir())
	if _, err := file.Set(map[string]any{adaptercloud.KeyEnabled: true, adaptercloud.KeyCommands: true}); err != nil {
		t.Fatal(err)
	}
	spool, err := adaptercloud.NewSpool(adaptercloud.DefaultSpoolLimits(), nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	master, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	l := &Link{opts: LinkOptions{Now: time.Now}, file: file, settings: adaptercloud.Settings{Enabled: true}, pinned: pins,
		roster: adaptercloud.NewRoster(server.URL, "credential", time.Now), identity: adaptercloud.Identity{MachineID: "machine"}}
	l.relay = &Relay{Transport: &adaptercloud.Transport{}, Spool: spool, MachineID: "machine", Signer: signer, Secret: master, KeyID: adaptercloud.MasterKeyID}
	registered := false
	retired := make(chan struct{}, 1)
	l.terminalMetadata = func(_ context.Context, action string, _ *terminalConnection) error {
		if action == "register" {
			registered = true
		} else if action == "retire" {
			retired <- struct{}{}
		}
		return nil
	}
	svc := terminals.New(nil, func(terminals.Principal) error { return terminal.Refuse(terminal.CodeForbidden, "local grant absent") })
	req := terminalRequest{RequestID: testRequestID, Operation: "open_connection", Connection: testConnection,
		KeyID: testKeyID, Key: base64.StdEncoding.EncodeToString(keyBytes)}
	l.openTerminalConnection(context.Background(), svc, terminals.Principal{Device: "viewer", Cloud: true}, req)
	if !registered {
		t.Fatal("local grant was checked before relay registration")
	}
	row, ok := spool.Row(0)
	if !ok {
		t.Fatal("no encrypted refusal receipt")
	}
	env, err := domaincloud.DecodeEnvelope(row.Sealed)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := env.Open(connectionKey, func(string) (ed25519.PublicKey, bool) { return signer.PublicKey(), true })
	if err != nil {
		t.Fatal(err)
	}
	var receipt terminalReceipt
	if err := json.Unmarshal(plain, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "refused" || receipt.Error != string(terminal.CodeForbidden) {
		t.Fatalf("grant refusal: %+v", receipt)
	}
	select {
	case <-retired:
		t.Fatal("retired before receipt settlement")
	default:
	}
	l.terminalReceiptSettled(env.Ch, env.Seq)
	select {
	case <-retired:
	case <-time.After(time.Second):
		t.Fatal("registration was not retired after receipt settlement")
	}
}

func TestMissingLocalPinGetsEncryptedForbiddenAfterCloudRegistration(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": []adaptercloud.RosterDevice{{ID: "viewer", PublicKey: base64.StdEncoding.EncodeToString(pub), Caps: []string{TerminalCapability}}}})
	}))
	defer server.Close()
	file := nextconfig.Open(t.TempDir())
	if _, err := file.Set(map[string]any{adaptercloud.KeyEnabled: true, adaptercloud.KeyCommands: true}); err != nil {
		t.Fatal(err)
	}
	spool, err := adaptercloud.NewSpool(adaptercloud.DefaultSpoolLimits(), nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	master, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	l := &Link{opts: LinkOptions{Now: time.Now}, file: file, settings: adaptercloud.Settings{Enabled: true},
		pinned: adaptercloud.NewPinnedStore(t.TempDir()), roster: adaptercloud.NewRoster(server.URL, "credential", time.Now),
		identity: adaptercloud.Identity{MachineID: "machine"}}
	l.relay = &Relay{Transport: &adaptercloud.Transport{}, Spool: spool, MachineID: "machine", Signer: signer, Secret: master, KeyID: adaptercloud.MasterKeyID}
	registered := false
	l.terminalMetadata = func(_ context.Context, action string, _ *terminalConnection) error {
		if action == "register" {
			registered = true
		}
		return nil
	}
	svc := terminals.New(nil, func(terminals.Principal) error { return nil }) // a permissive grant must not bypass the pin
	l.openTerminalConnection(context.Background(), svc, terminals.Principal{Device: "viewer", Cloud: true},
		terminalRequest{RequestID: testRequestID, Operation: "open_connection", Connection: testConnection,
			KeyID: testKeyID, Key: base64.StdEncoding.EncodeToString(key.Bytes())})
	if !registered {
		t.Fatal("missing local pin was refused before keyed receipt could be delivered")
	}
	row, ok := spool.Row(0)
	if !ok {
		t.Fatal("missing-pin refusal was not queued")
	}
	env, err := domaincloud.DecodeEnvelope(row.Sealed)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := env.Open(key, func(string) (ed25519.PublicKey, bool) { return signer.PublicKey(), true })
	if err != nil {
		t.Fatal(err)
	}
	var receipt terminalReceipt
	if err := json.Unmarshal(plain, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "refused" || receipt.Error != string(terminal.CodeForbidden) {
		t.Fatalf("missing pin: %+v", receipt)
	}
}

func TestRevokedTerminalNoticesViewerAndRefusesNextInput(t *testing.T) {
	spool, err := adaptercloud.NewSpool(adaptercloud.DefaultSpoolLimits(), nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	master, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c := &terminalConnection{viewer: "viewer", id: testConnection, keyID: testKeyID, key: key,
		expires: time.Now().Add(time.Minute), receipts: map[string][]byte{}}
	svc := terminals.New(nil, func(terminals.Principal) error { t.Fatal("denied connection reached terminal access"); return nil })
	l := &Link{identity: adaptercloud.Identity{MachineID: "machine"}, opts: LinkOptions{Now: time.Now,
		TerminalService: func() (*terminals.Service, error) { return svc, nil }}, machineIncarnation: "incarnation",
		terminalConnections: map[string]*terminalConnection{terminalConnectionID("viewer", testConnection): c}}
	l.relay = &Relay{Transport: &adaptercloud.Transport{}, Spool: spool, MachineID: "machine", Signer: signer, Secret: master, KeyID: adaptercloud.MasterKeyID}
	retired := make(chan struct{}, 1)
	l.terminalMetadata = func(_ context.Context, action string, _ *terminalConnection) error {
		if action == "retire" {
			retired <- struct{}{}
		}
		return nil
	}
	l.revokeRegisteredTerminal(context.Background(), c)
	if !c.denied {
		t.Fatal("revocation did not stop input immediately")
	}
	select {
	case <-retired:
		t.Fatal("metadata retirement overtook the queued notice")
	default:
	}
	in := Inbound{Channel: "termi/machine/viewer", Class: string(domaincloud.ClassCtl), Sender: "viewer",
		Plaintext: []byte(`{"v":1,"type":"terminal_request","request_id":"` + testRequestID + `","connection":"` + testConnection + `","operation":"input","terminal_id":"trm_` + strings.Join([]string{"00000000", "0000", "4000", "8000", "000000000000"}, "-") + `","client":"tab","epoch":1,"seq":1,"body":{"data":"YQ=="}}`)}
	l.handleTerminal(context.Background(), in)
	for seq, wantType := range []string{"terminal_notice", "terminal_receipt"} {
		row, ok := spool.Row(uint64(seq))
		if !ok {
			t.Fatalf("missing %s", wantType)
		}
		env, err := domaincloud.DecodeEnvelope(row.Sealed)
		if err != nil {
			t.Fatal(err)
		}
		plain, err := env.Open(key, func(string) (ed25519.PublicKey, bool) { return signer.PublicKey(), true })
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(plain, &got); err != nil {
			t.Fatal(err)
		}
		if got["type"] != wantType {
			t.Fatalf("got %v, want %s", got, wantType)
		}
		if wantType == "terminal_receipt" && got["error"] != string(terminal.CodeForbidden) {
			t.Fatalf("not a typed refusal: %v", got)
		}
	}
	select {
	case <-retired:
		t.Fatal("metadata retirement overtook the correlated refusal")
	default:
	}
	l.terminalReceiptSettled("termr/machine/viewer/"+testConnection, 1)
	select {
	case <-retired:
	case <-time.After(time.Second):
		t.Fatal("refusal settlement did not retire registration")
	}
}

func TestRevocationNoticeFailureStopsInputAndRetires(t *testing.T) {
	now := time.Now()
	key, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c := &terminalConnection{viewer: "viewer", id: testConnection, keyID: testKeyID, key: key,
		expires: now.Add(time.Minute), receipts: map[string][]byte{}}
	retired := make(chan struct{}, 1)
	l := &Link{opts: LinkOptions{Now: func() time.Time { return now }}, identity: adaptercloud.Identity{MachineID: "machine"},
		terminalConnections: map[string]*terminalConnection{terminalConnectionID("viewer", testConnection): c}}
	l.terminalMetadata = func(_ context.Context, action string, _ *terminalConnection) error {
		if action == "retire" {
			retired <- struct{}{}
		}
		return nil
	}
	l.revokeRegisteredTerminal(context.Background(), c) // relay is unavailable: publish fails
	if !c.denied || l.getTerminalConnection("viewer", testConnection) != nil {
		t.Fatal("failed notice left input active")
	}
	select {
	case <-retired:
	case <-time.After(time.Second):
		t.Fatal("failed notice did not retire")
	}
}

func TestRevocationTombstoneRetiresWithoutRelayAck(t *testing.T) {
	now := time.Now()
	c := &terminalConnection{viewer: "viewer", id: testConnection, expires: now.Add(time.Minute),
		denied: true, deniedAt: now}
	retired := make(chan struct{}, 1)
	l := &Link{opts: LinkOptions{Now: func() time.Time { return now }},
		terminalConnections: map[string]*terminalConnection{terminalConnectionID("viewer", testConnection): c}}
	l.terminalMetadata = func(_ context.Context, action string, _ *terminalConnection) error {
		if action == "retire" {
			retired <- struct{}{}
		}
		return nil
	}
	now = now.Add(CloudTerminalRevocationRetireSecondsLimit*time.Second - time.Millisecond)
	l.sweepTerminalConnections()
	if l.getTerminalConnection("viewer", testConnection) == nil {
		t.Fatal("tombstone retired before its bound")
	}
	now = now.Add(2 * time.Millisecond)
	l.sweepTerminalConnections()
	if l.getTerminalConnection("viewer", testConnection) != nil {
		t.Fatal("unsettled notice left registration beyond tombstone")
	}
	select {
	case <-retired:
	case <-time.After(time.Second):
		t.Fatal("relay retirement did not run")
	}
}
