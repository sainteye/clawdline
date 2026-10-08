package cloud

import (
	"crypto/ed25519"
	"encoding/json"
	"testing"
	"time"

	domain "github.com/sainteye/clawdline/internal/domain/cloud"
)

func TestViewerTransportOpensOnlyTheSendingPairedMachinesChannels(t *testing.T) {
	viewer, _ := domain.NewDeviceKey(nil)
	machineA, _ := domain.NewDeviceKey(nil)
	machineB, _ := domain.NewDeviceKey(nil)
	secretA, _ := domain.NewContentKey(nil)
	secretB, _ := domain.NewContentKey(nil)
	keys := map[string]domain.DeviceKey{"machine-a": machineA, "machine-b": machineB}
	secrets := map[string]domain.ContentKey{"machine-a": secretA, "machine-b": secretB}
	var received []string
	line, err := New(Options{
		RelayURL: "wss://relay.example.test", Role: "viewer",
		Token:    &TokenSource{Credential: "viewer-credential"},
		Identity: Identity{AccountID: "account", MachineID: "viewer", MachineCredential: "viewer-credential"},
		Signer:   viewer, Replay: domain.NewReplayWindow(0),
		PublicKeyFor: func(sender string) (ed25519.PublicKey, bool) {
			key, ok := keys[sender]
			return key.PublicKey(), ok
		},
		ContentKeyFor: func(sender, keyID string) (domain.ContentKey, bool) {
			if keyID != MasterKeyID {
				return domain.ContentKey{}, false
			}
			key, ok := secrets[sender]
			return key, ok
		},
		Inbound: func(envelope domain.Envelope, plaintext []byte, _ ed25519.PublicKey) {
			received = append(received, envelope.Sender+":"+string(plaintext))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	seal := func(sender, channel string, seq uint64, key domain.ContentKey, signer domain.DeviceKey) []byte {
		t.Helper()
		class := domain.ClassStream
		if channel == "ctl/machine-a" {
			class = domain.ClassCtl
		}
		envelope, err := domain.Seal([]byte("row"), domain.SealParams{
			Ch: channel, Seq: seq, Ts: uint64(time.Now().UnixMilli()), Class: class,
			KeyID: MasterKeyID, Sender: sender, Key: key, Signer: signer,
		})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := envelope.CanonicalJSON()
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	if !line.admitEnvelope(seal("machine-a", "ss/machine-a/same", 1, secretA, machineA), "") ||
		!line.admitEnvelope(seal("machine-b", "t/machine-b/same", 1, secretB, machineB), "") {
		t.Fatal("paired machines' exact channels were refused")
	}
	if line.admitEnvelope(seal("machine-a", "ss/machine-b/same", 2, secretA, machineA), "") {
		t.Fatal("a signer claimed another machine's status channel")
	}
	if line.admitEnvelope(seal("machine-a", "ctl/machine-a", 3, secretA, machineA), "") {
		t.Fatal("a viewer accepted a command channel")
	}
	if line.admitEnvelope(seal("machine-a", "s/machine-a/same", 4, secretB, machineA), "") {
		t.Fatal("a paired signer opened a row sealed with another machine's secret")
	}
	if len(received) != 2 || received[0] != "machine-a:row" || received[1] != "machine-b:row" {
		t.Fatalf("inbound rows: %v", received)
	}
	retained := seal("machine-a", "t/machine-a/same", 5, secretA, machineA)
	frame, err := json.Marshal(EnvelopeFrame{Type: FrameEnvelope, Realign: true, Envelope: retained})
	if err != nil {
		t.Fatal(err)
	}
	line.handleEnvelope(frame)
	if len(received) != 2 {
		t.Fatalf("retained transcript answer settled a new read: %v", received)
	}
	if !line.admitEnvelope(retained, "") || len(received) != 3 {
		t.Fatal("retained answer incorrectly spent the live reply sequence")
	}
}
