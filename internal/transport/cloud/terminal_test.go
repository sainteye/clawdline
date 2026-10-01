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
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
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
}
