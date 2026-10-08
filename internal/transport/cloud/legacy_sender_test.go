package cloud

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

// A pre-pin browser has the machine's content key and a verified account
// identity. Reading a machine envelope and writing one must agree about its
// pairing; a mere account login without that content key still cannot act.
func TestLegacyViewerReadingMachineCanSendOnlyWithItsContentKey(t *testing.T) {
	viewer, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherSecret, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	row := adaptercloud.RosterDevice{ID: "viewer", PublicKey: base64.StdEncoding.EncodeToString(viewer.PublicKey())}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": []adaptercloud.RosterDevice{row}})
	}))
	defer server.Close()
	dir := t.TempDir()
	file := nextconfig.Open(dir)
	if _, err := file.Set(map[string]any{adaptercloud.KeyEnabled: true, adaptercloud.KeyCommands: true}); err != nil {
		t.Fatal(err)
	}
	l := &Link{file: file, settings: adaptercloud.Settings{Enabled: true}, opts: LinkOptions{Now: time.Now},
		pinned:           adaptercloud.NewPinnedStore(t.TempDir()),
		roster:           adaptercloud.NewRoster(server.URL, "credential", time.Now),
		identity:         adaptercloud.Identity{MachineID: "machine"},
		terminalRequests: make(chan Inbound, 2), terminalRefusals: make(chan Inbound, 2)}
	if err := l.roster.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	opened := 0
	transport, err := adaptercloud.New(adaptercloud.Options{
		RelayURL: "wss://relay.invalid", Token: &adaptercloud.TokenSource{},
		Identity: adaptercloud.Identity{AccountID: "account", MachineID: "machine", MachineCredential: "credential"},
		Signer:   machine, Replay: domaincloud.NewReplayWindow(64), ContentKey: secret,
		PublicKeyFor: l.publicKeyFor,
		Inbound: func(env domaincloud.Envelope, plain []byte, verifiedKey ed25519.PublicKey) {
			opened++
			l.deliverTerminal(Inbound{Channel: env.Ch, Class: string(env.Class), Sender: env.Sender,
				Sequence: env.Seq, Plaintext: plain, VerifiedKey: verifiedKey})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	seal := func(seq uint64, key domaincloud.ContentKey) []byte {
		t.Helper()
		env, err := domaincloud.Seal([]byte(`{"type":"send","session":"%1","text":"hello"}`),
			domaincloud.SealParams{Ch: "termi/machine/viewer", Seq: seq, Ts: uint64(time.Now().UnixMilli()),
				Class: domaincloud.ClassCtl, KeyID: adaptercloud.MasterKeyID,
				Sender: "viewer", Key: key, Signer: viewer})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := env.CanonicalJSON()
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	if !transport.AcceptDirect("viewer", seal(1, secret)) {
		t.Fatal("legacy viewer's authenticated machine envelope was refused")
	}
	a := l.authority(context.Background(), "viewer", viewer.PublicKey(), true)
	if opened != 1 || !a.RosterAllowsSender || !a.WriteGateAllows {
		t.Fatal("a readable legacy pairing did not grant command authority")
	}
	if l.authority(context.Background(), "viewer", nil, true).RosterAllowsSender {
		t.Fatal("account membership without an envelope's verified key granted a command")
	}
	if !l.TerminalViewerAllowed("viewer") {
		t.Fatal("the decrypted legacy terminal envelope did not grant terminal authority")
	}
	if transport.AcceptDirect("viewer", seal(2, otherSecret)) || opened != 1 {
		t.Fatal("account login without this machine's content key reached its commands")
	}
	if _, err := file.Set(map[string]any{adaptercloud.KeyCommands: false}); err != nil {
		t.Fatal(err)
	}
	if l.authority(context.Background(), "viewer", viewer.PublicKey(), true).WriteGateAllows {
		t.Fatal("the per-machine command switch was ignored")
	}
	if err := l.pinned.Pin("account", adaptercloud.PinnedDevice{DeviceID: "viewer", PublicKey: row.PublicKey}); err != nil {
		t.Fatal(err)
	}
	if changed, err := l.pinned.Revoke("viewer", time.Now()); err != nil || !changed {
		t.Fatalf("revoke: changed=%v error=%v", changed, err)
	}
	if transport.AcceptDirect("viewer", seal(3, secret)) || l.authority(context.Background(), "viewer", viewer.PublicKey(), true).RosterAllowsSender {
		t.Fatal("local revocation did not beat the account roster and machine key")
	}
}
