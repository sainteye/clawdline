package cloud

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	domain "github.com/sainteye/clawdline/internal/domain/cloud"
)

// A direct-carrier envelope passes the relay's whole ladder plus two checks
// of its own, and shares the relay's replay window, so one signed request
// cannot be spent once on each carrier.
func TestAcceptDirectUsesTheRelayLadderAndItsReplayWindow(t *testing.T) {
	viewer, err := domain.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, err := domain.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	master, err := domain.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]ed25519.PublicKey{"viewer": viewer.PublicKey(), "other": other.PublicKey()}
	admitted := 0
	tr := &Transport{opts: Options{
		Identity:     Identity{MachineID: "machine"},
		Status:       NewStatusRecorder(time.Now()),
		Replay:       domain.NewReplayWindow(64),
		ContentKey:   master,
		PublicKeyFor: func(sender string) (ed25519.PublicKey, bool) { k, ok := keys[sender]; return k, ok },
		Inbound:      func(domain.Envelope, []byte, ed25519.PublicKey) { admitted++ },
	}}
	seal := func(ch, sender string, signer domain.DeviceKey, seq uint64) []byte {
		t.Helper()
		env, err := domain.Seal([]byte(`{"v":1}`), domain.SealParams{Ch: ch, Seq: seq, Ts: uint64(time.Now().UnixMilli()),
			Class: domain.ClassCtl, KeyID: MasterKeyID, Sender: sender, Key: master, Signer: signer})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := env.CanonicalJSON()
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	good := seal("termi/machine/viewer", "viewer", viewer, 1)
	if !tr.AcceptDirect("viewer", good) || admitted != 1 {
		t.Fatalf("a well-formed direct envelope was not admitted (admitted %d)", admitted)
	}
	if tr.AcceptDirect("viewer", good) {
		t.Fatal("the same envelope was admitted twice on the direct carrier")
	}
	relayCopy := seal("termi/machine/viewer", "viewer", viewer, 2)
	if !tr.admitEnvelope(relayCopy, "") {
		t.Fatal("the relay path refused a fresh envelope")
	}
	if tr.AcceptDirect("viewer", relayCopy) {
		t.Fatal("an envelope spent on the relay was admitted again on the direct carrier")
	}
	cases := map[string][]byte{
		// Another device's envelope, even a valid one, never rides this viewer's channel.
		"another sender": seal("termi/machine/other", "other", other, 1),
		"wrong channel":  seal("ctl/machine", "viewer", viewer, 3),
		"forged signer":  seal("termi/machine/viewer", "viewer", other, 4),
		"not JSON":       []byte("{"),
	}
	for name, raw := range cases {
		if tr.AcceptDirect("viewer", raw) {
			t.Errorf("%s: admitted", name)
		}
	}
	if tr.AcceptDirect("", seal("termi/machine/viewer", "viewer", viewer, 5)) {
		t.Fatal("a direct envelope with no viewer was admitted")
	}
	if admitted != 2 {
		t.Fatalf("admitted %d envelopes, want 2", admitted)
	}
}

func TestAdmissionUsesOneSenderKeyThroughVerificationAndDecryption(t *testing.T) {
	viewer, err := domain.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, err := domain.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	master, err := domain.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	lookups := 0
	var delivered ed25519.PublicKey
	tr := &Transport{opts: Options{Identity: Identity{MachineID: "machine"},
		Status: NewStatusRecorder(time.Now()), Replay: domain.NewReplayWindow(64), ContentKey: master,
		PublicKeyFor: func(string) (ed25519.PublicKey, bool) {
			lookups++
			if lookups == 1 {
				return viewer.PublicKey(), true
			}
			return other.PublicKey(), true
		},
		Inbound: func(_ domain.Envelope, _ []byte, key ed25519.PublicKey) { delivered = key },
	}}
	env, err := domain.Seal([]byte(`{"v":1}`), domain.SealParams{Ch: "termi/machine/viewer",
		Seq: 1, Ts: uint64(time.Now().UnixMilli()), Class: domain.ClassCtl, KeyID: MasterKeyID,
		Sender: "viewer", Key: master, Signer: viewer})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := env.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !tr.AcceptDirect("viewer", raw) || lookups != 1 || !bytes.Equal(viewer.PublicKey(), delivered) {
		t.Fatalf("admission changed the verified key: lookups=%d delivered=%v", lookups, delivered != nil)
	}
}
