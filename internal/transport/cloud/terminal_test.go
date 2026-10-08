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
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/app/terminals"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

const testConnection = "AAAAAAAAAAAAAAAAAAAAAA"
const testKeyID = "rk-AQEBAQEBAQEBAQEBAQEBAQ"

type stillCloudHost struct{ ports.OwnedTerminals }

type blockingCloudListHost struct {
	ports.OwnedTerminals
	entered chan struct{}
	release chan struct{}
}

func (h *blockingCloudListHost) List(ctx context.Context) ([]terminal.Terminal, error) {
	select {
	case h.entered <- struct{}{}:
	default:
	}
	select {
	case <-h.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestSlowCloudListDoesNotBlockInputAdmission(t *testing.T) {
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
	host := &blockingCloudListHost{entered: make(chan struct{}, 1), release: make(chan struct{})}
	svc := terminals.New(host, func(terminals.Principal) error { return nil })
	c := &terminalConnection{viewer: "viewer", id: testConnection, keyID: testKeyID, key: key,
		expires: time.Now().Add(time.Minute), receipts: map[string][]byte{}}
	l := &Link{identity: adaptercloud.Identity{MachineID: "machine"}, opts: LinkOptions{Now: time.Now,
		TerminalService: func() (*terminals.Service, error) { return svc, nil }},
		terminalConnections: map[string]*terminalConnection{terminalConnectionID("viewer", testConnection): c},
		terminalRequests:    make(chan Inbound, CloudTerminalIngressLimit),
		terminalLists:       make(chan Inbound, CloudTerminalListIngressLimit),
		terminalRefusals:    make(chan Inbound, CloudTerminalRefusalsLimit)}
	l.relay = &Relay{Transport: &adaptercloud.Transport{}, Spool: spool, MachineID: "machine", Signer: signer,
		Secret: master, KeyID: adaptercloud.MasterKeyID}
	request := func(operation, id string) Inbound {
		data, err := json.Marshal(terminalRequest{V: 1, Type: "terminal_request", RequestID: id,
			Connection: testConnection, Operation: operation, TerminalID: "invalid"})
		if err != nil {
			t.Fatal(err)
		}
		return Inbound{Channel: "termi/machine/viewer", Class: string(domaincloud.ClassCtl), Sender: "viewer", Plaintext: data}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.runTerminal(ctx); close(done) }()
	defer func() { close(host.release); cancel(); <-done }()
	l.deliverTerminal(request("list", testRequestID))
	select {
	case <-host.entered:
	case <-time.After(time.Second):
		t.Fatal("list did not reach the blocking host")
	}
	for i := 0; i < CloudTerminalListIngressLimit; i++ {
		l.deliverTerminal(request("list", fmt.Sprintf("12345678-1234-4123-8123-%012x", i+10)))
	}
	busyID := fmt.Sprintf("12345678-1234-4123-8123-%012x", 99)
	l.deliverTerminal(request("list", busyID))
	busyDeadline := time.After(500 * time.Millisecond)
	for l.terminalReceipt(c, busyID) == nil {
		select {
		case <-busyDeadline:
			t.Fatal("full list queue did not return a keyed busy receipt")
		case <-time.After(time.Millisecond):
		}
	}
	var busy terminalReceipt
	if err := json.Unmarshal(l.terminalReceipt(c, busyID), &busy); err != nil || busy.Error != string(terminal.CodeBusy) {
		t.Fatalf("full list queue receipt: %+v, %v", busy, err)
	}
	start := time.Now()
	inputID := fmt.Sprintf("12345678-1234-4123-8123-%012x", 2)
	l.deliverTerminal(request("input", inputID))
	deadline := time.After(500 * time.Millisecond)
	for {
		if raw := l.terminalReceipt(c, inputID); raw != nil {
			var receipt terminalReceipt
			if err := json.Unmarshal(raw, &receipt); err != nil {
				t.Fatal(err)
			}
			if receipt.Operation != "input" || receipt.Status != "refused" || receipt.Error != string(terminal.CodeInvalid) {
				t.Fatalf("unexpected input receipt: %+v", receipt)
			}
			if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
				t.Fatalf("input waited %s behind list", elapsed)
			} else {
				t.Logf("input refusal completed in %s while the list host remained blocked", elapsed)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("input waited behind blocked list")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestFullCloudListLaneStillAdmitsConnectionRequest(t *testing.T) {
	l := &Link{identity: adaptercloud.Identity{MachineID: "machine"},
		terminalRequests: make(chan Inbound, CloudTerminalIngressLimit),
		terminalLists:    make(chan Inbound, CloudTerminalListIngressLimit),
		terminalRefusals: make(chan Inbound, CloudTerminalRefusalsLimit)}
	for i := 0; i < CloudTerminalListIngressLimit; i++ {
		l.terminalLists <- Inbound{}
	}
	data, err := json.Marshal(terminalRequest{V: 1, Type: "terminal_request", RequestID: testRequestID,
		Connection: testConnection, Operation: "open_connection"})
	if err != nil {
		t.Fatal(err)
	}
	l.deliverTerminal(Inbound{Channel: "termi/machine/viewer", Class: string(domaincloud.ClassCtl),
		Sender: "viewer", Plaintext: data})
	if len(l.terminalRequests) != 1 || len(l.terminalRefusals) != 0 {
		t.Fatalf("connection admission waited behind full list lane: stateful=%d refusals=%d",
			len(l.terminalRequests), len(l.terminalRefusals))
	}
}

func (*stillCloudHost) Frame(context.Context, terminal.ID) (terminal.Frame, error) {
	return terminal.Frame{Rev: "still", At: time.Now(), Cols: 80, Rows: 24, Lines: []string{"$ "}}, nil
}

func (*stillCloudHost) Changed(context.Context, terminal.ID) (<-chan struct{}, func(), error) {
	return make(chan struct{}), func() {}, nil
}

func TestTerminalFramesWaitForRelaySettlementAndKeepNewestCapture(t *testing.T) {
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
	id := terminal.NewID()
	c := &terminalConnection{viewer: "viewer", id: testConnection, keyID: testKeyID, key: key,
		expires: time.Now().Add(time.Minute), terminalID: id}
	l := &Link{identity: adaptercloud.Identity{MachineID: "machine"}, opts: LinkOptions{Now: time.Now},
		terminalConnections: map[string]*terminalConnection{terminalConnectionID("viewer", testConnection): c}}
	l.relay = &Relay{Transport: &adaptercloud.Transport{}, Spool: spool, MachineID: "machine", Signer: signer, Secret: master, KeyID: adaptercloud.MasterKeyID}
	svc := terminals.New(nil, func(terminals.Principal) error { return nil })
	p := terminals.Principal{Device: "viewer", Cloud: true}
	first := time.Now().Add(-4 * time.Second)
	for index, at := range []time.Time{first, first.Add(time.Second), first.Add(2 * time.Second)} {
		err := l.sendTerminalFrame(context.Background(), svc, p, c, id, terminal.Frame{Rev: "still", At: at, Lines: []string{"$ "}})
		if index == 0 && err != nil || index > 0 && !errors.Is(err, terminals.ErrFrameDeferred) {
			t.Fatalf("frame %d: %v", index, err)
		}
	}
	if _, ok := spool.Row(1); ok {
		t.Fatal("a second frame entered the spool before ACK")
	}
	if c.publishedFrameSeq != 1 {
		t.Fatalf("published %d frames before ACK", c.publishedFrameSeq)
	}
	l.terminalReceiptSettled("term/machine/viewer/"+testConnection, c.framePendingSeq, adaptercloud.SettleDelivered)
	latest := time.Now()
	if err := l.sendTerminalFrame(context.Background(), svc, p, c, id, terminal.Frame{Rev: "still", At: latest, Lines: []string{"$ "}}); err != nil {
		t.Fatal(err)
	}
	row, ok := spool.Row(1)
	if !ok {
		t.Fatal("fresh frame was not sent after ACK")
	}
	env, err := domaincloud.DecodeEnvelope(row.Sealed)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := env.Open(key, func(string) (ed25519.PublicKey, bool) { return signer.PublicKey(), true })
	if err != nil {
		t.Fatal(err)
	}
	var frame struct {
		FrameSeq   uint64  `json:"frame_seq"`
		CapturedAt float64 `json:"captured_at"`
		Frame      struct {
			At float64 `json:"at"`
		} `json:"frame"`
	}
	if err := json.Unmarshal(plain, &frame); err != nil {
		t.Fatal(err)
	}
	if frame.FrameSeq != 2 || frame.CapturedAt != float64(latest.UnixMilli())/1000 || frame.Frame.At != frame.CapturedAt {
		t.Fatalf("ACK did not release a freshly signed full frame: %+v", frame)
	}
}

func TestTerminalDeltaUsesOnlyObservedCompleteBase(t *testing.T) {
	l, _, c, svc, _ := terminalLifecycleFixture(t)
	c.frameDeltaV1 = true
	p := terminals.Principal{Device: c.viewer, Cloud: true}
	base := terminal.Frame{Rev: "base", At: time.Now(), Cols: 80, Rows: 3,
		Lines: []string{strings.Repeat("a", 80), strings.Repeat("b", 80), strings.Repeat("c", 80)}}
	if err := l.sendTerminalFrame(context.Background(), svc, p, c, c.terminalID, base); err != nil {
		t.Fatal(err)
	}
	if c.frameBase != nil || c.frameCandidate == nil || c.framePendingChannel != "term/machine/viewer/"+c.id {
		t.Fatal("first frame did not remain a pending complete candidate")
	}
	newest := base
	newest.Rev = "newest"
	newest.Lines = append([]string(nil), base.Lines...)
	newest.Lines[1] = strings.Repeat("z", 80)
	if err := l.sendTerminalFrame(context.Background(), svc, p, c, c.terminalID, newest); !errors.Is(err, terminals.ErrFrameDeferred) {
		t.Fatalf("first deferred change: %v", err)
	}
	newest.Lines[1] = strings.Repeat("y", 80)
	if err := l.sendTerminalFrame(context.Background(), svc, p, c, c.terminalID, newest); !errors.Is(err, terminals.ErrFrameDeferred) {
		t.Fatalf("second deferred change: %v", err)
	}
	l.terminalReceiptSettled(c.framePendingChannel, c.framePendingSeq, adaptercloud.SettleDelivered)
	if c.frameBase == nil || c.frameBase.Rev != "base" {
		t.Fatal("delivered candidate did not become base")
	}
	delta, ok := terminalDeltaPayload(c.terminalID, c, 2, newest)
	if !ok {
		t.Fatal("eligible single-row change did not make a delta")
	}
	var got struct {
		BaseSeq     uint64               `json:"base_seq"`
		BaseRev     string               `json:"base_rev"`
		ChangedRows []terminalChangedRow `json:"changed_rows"`
		ScreenHash  string               `json:"screen_hash"`
	}
	if err := json.Unmarshal(delta, &got); err != nil {
		t.Fatal(err)
	}
	if got.BaseSeq != 1 || got.BaseRev != "base" || len(got.ChangedRows) != 1 || got.ChangedRows[0].Row != 1 || got.ChangedRows[0].Line != newest.Lines[1] {
		t.Fatalf("wrong newest row delta: %+v", got)
	}
	hash, _ := terminalScreenHash(newest.Lines)
	if got.ScreenHash != hash {
		t.Fatal("screen hash mismatch")
	}
	full, _ := json.Marshal(map[string]any{"v": 1, "type": "terminal_frame", "terminal_id": string(c.terminalID), "connection": c.id, "frame_seq": 2, "captured_at": float64(newest.At.UnixMilli()) / 1000, "frame": cloudWireFrame(newest)})
	if len(delta) >= len(full) {
		t.Fatalf("delta %d is not smaller than full frame %d", len(delta), len(full))
	}
	resized := newest
	resized.Cols++
	if _, ok := terminalDeltaPayload(c.terminalID, c, 2, resized); ok {
		t.Fatal("resize accepted as delta")
	}
	alt := newest
	alt.Modes.Alt = true
	if _, ok := terminalDeltaPayload(c.terminalID, c, 2, alt); ok {
		t.Fatal("alt transition accepted as delta")
	}
	if _, ok := terminalDeltaPayload(terminal.ID("another-terminal"), c, 2, newest); ok {
		t.Fatal("another terminal reused the prior terminal's screen base")
	}
	c.frameDeltaV1 = false
	if _, ok := terminalDeltaPayload(c.terminalID, c, 2, newest); ok {
		t.Fatal("old viewer accepted a delta")
	}
}

func TestTerminalDeltaRejectedSettlementNeverPromotesCandidate(t *testing.T) {
	l, _, c, svc, _ := terminalLifecycleFixture(t)
	c.frameDeltaV1 = true
	frame := terminal.Frame{Rev: "first", At: time.Now(), Cols: 80, Rows: 1, Lines: []string{"first"}}
	if err := l.sendTerminalFrame(context.Background(), svc, terminals.Principal{Device: c.viewer, Cloud: true}, c, c.terminalID, frame); err != nil {
		t.Fatal(err)
	}
	l.terminalReceiptSettled(c.framePendingChannel, c.framePendingSeq, adaptercloud.SettlePeerError)
	if c.frameBase != nil || c.frameCandidate != nil {
		t.Fatal("failed settlement retained a screen base")
	}
	if l.getTerminalConnection(c.viewer, c.id) != nil {
		t.Fatal("failed settlement left connection active")
	}
}

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

func TestTerminalAdmissionUsesCommandKeyPinAndFreshRoster(t *testing.T) {
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
	row := adaptercloud.RosterDevice{ID: "viewer", PublicKey: base64.StdEncoding.EncodeToString(pub), Caps: []string{"send_prompt"}}
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
		t.Fatal("exact pin was refused")
	}
	if l.TerminalViewerAllowed("another-viewer") {
		t.Fatal("another viewer inherited the authorized viewer connection")
	}
	mu.Lock()
	row.Caps = []string{"read_sessions"}
	mu.Unlock()
	l.terminalRosterAt = time.Time{}
	if !l.TerminalViewerAllowed("viewer") {
		t.Fatal("legacy read-only viewer was refused")
	}
	mu.Lock()
	row.Caps = []string{"send_prompt"}
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
	mu.Lock()
	failed = false
	row.PublicKey = base64.StdEncoding.EncodeToString(pub)
	mu.Unlock()
	legacy := &Link{opts: LinkOptions{Now: time.Now}, file: file,
		settings: adaptercloud.Settings{Enabled: true},
		pinned:   adaptercloud.NewPinnedStore(filepath.Join(dir, "legacy-pins")),
		roster:   adaptercloud.NewRoster(server.URL, "credential", time.Now)}
	if legacy.TerminalViewerAllowed("viewer") {
		t.Fatal("a roster-only viewer gained terminal access without pairing")
	}
	if revoked, err := pinned.Revoke("viewer", time.Now()); err != nil || !revoked {
		t.Fatalf("revoke pinned viewer: changed=%v err=%v", revoked, err)
	}
	l.terminalRosterAt = time.Time{}
	if l.TerminalViewerAllowed("viewer") {
		t.Fatal("local revocation did not beat the roster fallback")
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
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": []adaptercloud.RosterDevice{{ID: "viewer", PublicKey: base64.StdEncoding.EncodeToString(pub), Caps: []string{"send_prompt"}}}})
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
	l.terminalReceiptSettled(env.Ch, env.Seq, adaptercloud.SettleDelivered)
	select {
	case <-retired:
	case <-time.After(time.Second):
		t.Fatal("registration was not retired after receipt settlement")
	}
}

func TestRosterOnlyCommandSenderGetsEncryptedTerminalRefusal(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": []adaptercloud.RosterDevice{{ID: "viewer", PublicKey: base64.StdEncoding.EncodeToString(pub), Caps: []string{"send_prompt"}}}})
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
	svc := terminals.New(nil, func(terminals.Principal) error { return nil })
	l.openTerminalConnection(context.Background(), svc, terminals.Principal{Device: "viewer", Cloud: true},
		terminalRequest{RequestID: testRequestID, Operation: "open_connection", Connection: testConnection,
			KeyID: testKeyID, Key: base64.StdEncoding.EncodeToString(key.Bytes())})
	if !registered {
		t.Fatal("roster-only sender did not register for a keyed refusal")
	}
	row, ok := spool.Row(0)
	if !ok {
		t.Fatal("roster-only connection receipt was not queued")
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
		t.Fatalf("roster-only sender: %+v", receipt)
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
	l.terminalReceiptSettled("termr/machine/viewer/"+testConnection, 1, adaptercloud.SettleDelivered)
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
