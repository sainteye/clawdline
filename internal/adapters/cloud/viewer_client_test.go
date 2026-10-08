package cloud

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	domain "github.com/sainteye/clawdline/internal/domain/cloud"
)

func TestViewerClientRechecksRosterBeforeUsingAnyLocalPin(t *testing.T) {
	viewer, _ := domain.NewDeviceKey(nil)
	machineA, _ := domain.NewDeviceKey(nil)
	machineB, _ := domain.NewDeviceKey(nil)
	secretA, _ := domain.NewContentKey(nil)
	secretB, _ := domain.NewContentKey(nil)
	pin := func(id string, machine domain.DeviceKey, secret domain.ContentKey) ViewerMachinePin {
		return ViewerMachinePin{MachineID: id, MachineSigningKey: base64.StdEncoding.EncodeToString(machine.PublicKey()),
			MachineFingerprint: machine.Fingerprint(), MasterSecret: base64.StdEncoding.EncodeToString(secret.Bytes()), KeyID: MasterKeyID}
	}
	a := pin("machine-a", machineA, secretA)
	b := pin("machine-b", machineB, secretB)
	revoked := false
	unavailable := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/machines" || r.Header.Get("Authorization") != "Bearer viewer-credential" {
			t.Errorf("unexpected roster request: %s %s", r.URL.Path, r.Header.Get("Authorization"))
			w.WriteHeader(403)
			return
		}
		if unavailable {
			w.WriteHeader(503)
			return
		}
		var revokedAt *string
		if revoked {
			s := "2026-10-09T00:00:00Z"
			revokedAt = &s
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"machines": []MachineRecord{
			{ID: a.MachineID, PublicKey: a.MachineSigningKey, KeyFingerprint: a.MachineFingerprint},
			{ID: b.MachineID, PublicKey: b.MachineSigningKey, KeyFingerprint: b.MachineFingerprint, RevokedAt: revokedAt},
		}})
	}))
	defer server.Close()
	options := ViewerClientOptions{
		Settings: Settings{Enabled: true, APIBase: server.URL, RelayURL: "wss://relay.example.test"},
		Identity: ViewerIdentity{AccountID: "account", DeviceID: "viewer", ViewerCredential: "viewer-credential", APIBase: server.URL},
		Signer:   viewer, Fence: &MemoryFence{}, Pins: map[string]ViewerMachinePin{a.MachineID: a, b.MachineID: b},
	}
	options.Settings.Enabled = false
	if _, err := NewViewerClient(options); err != ErrDisabled {
		t.Fatalf("single-machine switch allowed viewer line: %v", err)
	}
	options.Settings.Enabled = true
	client, err := NewViewerClient(options)
	if err != nil {
		t.Fatal(err)
	}
	if client.Paired(a.MachineID) || client.Paired(b.MachineID) {
		t.Fatal("local pins were trusted before an account roster check")
	}
	if err := client.RefreshMachinePins(context.Background()); err != nil || !client.Paired(a.MachineID) || !client.Paired(b.MachineID) {
		t.Fatalf("initial roster: A=%v B=%v err=%v", client.Paired(a.MachineID), client.Paired(b.MachineID), err)
	}
	revoked = true
	if err := client.RefreshMachinePins(context.Background()); err != nil || !client.Paired(a.MachineID) || client.Paired(b.MachineID) || client.pairingRefusal(b.MachineID) != ErrViewerRevoked {
		t.Fatalf("revoked B: A=%v B=%v err=%v", client.Paired(a.MachineID), client.Paired(b.MachineID), err)
	}
	unavailable = true
	if err := client.RefreshMachinePins(context.Background()); err == nil || client.Paired(a.MachineID) {
		t.Fatalf("unreadable roster kept access: A=%v err=%v", client.Paired(a.MachineID), err)
	}
	unavailable = false
	if err := client.RefreshMachinePins(context.Background()); err != nil || !client.Paired(a.MachineID) || client.Paired(b.MachineID) {
		t.Fatalf("recovered roster: A=%v B=%v err=%v", client.Paired(a.MachineID), client.Paired(b.MachineID), err)
	}
}
