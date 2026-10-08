package cloud

import (
	"bytes"
	"crypto/ed25519"
	"testing"
	"time"

	domain "github.com/sainteye/clawdline/internal/domain/cloud"
)

func TestMachineAdmitsOnlyItsOwnSignedReadChannel(t *testing.T) {
	line := newTestLine(t)
	plaintext := []byte(`{"type":"info","machine_id":"mac_test","session":"test_session","parts":"full","expected_generation":"0123456789abcdef0123456789abcdef"}`)
	var received domain.Envelope
	var opened []byte
	var verified ed25519.PublicKey
	line.transport.opts.Inbound = func(e domain.Envelope, body []byte, key ed25519.PublicKey) {
		received, opened, verified = e, body, key
	}
	seal := func(channel string, seq uint64) []byte {
		t.Helper()
		e, err := domain.Seal(plaintext, domain.SealParams{
			Ch: channel, Seq: seq, Ts: uint64(time.Now().UnixMilli()),
			Class: domain.ClassCtl, KeyID: MasterKeyID, Sender: "viewer-1",
			Key: line.secret, Signer: line.key,
		})
		if err != nil {
			t.Fatal(err)
		}
		wire, err := e.CanonicalJSON()
		if err != nil {
			t.Fatal(err)
		}
		return wire
	}
	if !line.transport.admitEnvelope(seal("r/mac_test", 17), "") ||
		received.Ch != "r/mac_test" || received.Class != domain.ClassCtl ||
		received.Sender != "viewer-1" || received.Seq != 17 || received.Ts == 0 ||
		!bytes.Equal(opened, plaintext) || !bytes.Equal(verified, line.key.PublicKey()) {
		t.Fatalf("signed read was not delivered with its verified identity: %+v %s", received, opened)
	}
	if line.transport.admitEnvelope(seal("r/another_machine", 18), "") ||
		line.status.Snapshot().InboundDropped[DropWrongChannel] != 1 {
		t.Fatal("a read for another machine reached the bridge")
	}
}
