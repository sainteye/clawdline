package cloud

import (
	"bytes"
	"crypto/ed25519"
	"testing"

	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

func TestPeerFramesNeverEnterViewerEnvelopeCallback(t *testing.T) {
	var ingress, ack []byte
	transport := &Transport{opts: Options{
		PeerInbound: func(frame []byte) { ingress = append([]byte(nil), frame...) },
		PeerAck:     func(frame []byte) { ack = append([]byte(nil), frame...) },
		Inbound: func(_ domaincloud.Envelope, _ []byte, _ ed25519.PublicKey) {
			t.Fatal("machine peer traffic entered the viewer envelope lane")
		},
	}}
	// The callback itself has no trust authority. It only selects a separate
	// queue where the locally pinned key and grant are later checked.
	envelope := []byte(`{"type":"peer_envelope","request":{}}`)
	if err := transport.handle(nil, envelope); err != nil || !bytes.Equal(ingress, envelope) {
		t.Fatalf("peer ingress = %q, %v", ingress, err)
	}
	receipt := []byte(`{"type":"peer_ack","request_id":"id","status":"delivered"}`)
	if err := transport.handle(nil, receipt); err != nil || !bytes.Equal(ack, receipt) {
		t.Fatalf("peer ack = %q, %v", ack, err)
	}
}
