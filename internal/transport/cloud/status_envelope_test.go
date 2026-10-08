package cloud

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

func TestPublishedStatusRowsSealAndOpenOnTheirActualChannels(t *testing.T) {
	out := &collector{}
	publisher := newPublisher(&fixedRouter{body: completeScan}, out)
	publisher.firstPass(context.Background())
	secret, err := domaincloud.NewContentKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := domaincloud.NewDeviceKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	row, marker := false, false
	for index, published := range out.out {
		if published.Channel != "ss/mac-01/"+cloudops.ChannelSegment("%19") &&
			published.Channel != "ss/mac-01/"+InventorySessionID {
			continue
		}
		sealed, err := domaincloud.Seal(published.Payload, domaincloud.SealParams{
			Ch: published.Channel, Seq: uint64(index + 1), Ts: uint64(time.Now().UnixMilli()),
			Class: domaincloud.Class(published.Class), KeyID: "test-master",
			Sender: "mac-01", Key: secret, Signer: signer,
		})
		if err != nil {
			t.Fatalf("publisher channel %s could not be sealed: %v", published.Channel, err)
		}
		wire, err := sealed.CanonicalJSON()
		if err != nil {
			t.Fatal(err)
		}
		openedEnvelope, err := domaincloud.DecodeEnvelope(wire)
		if err != nil {
			t.Fatal(err)
		}
		plain, err := openedEnvelope.Open(secret, func(sender string) (ed25519.PublicKey, bool) {
			if sender == "mac-01" {
				return signer.PublicKey(), true
			}
			return nil, false
		})
		if err != nil || !bytes.Equal(plain, published.Payload) {
			t.Fatalf("publisher channel %s could not be opened: %v", published.Channel, err)
		}
		if published.Channel == "ss/mac-01/"+InventorySessionID {
			marker = true
		} else {
			row = true
		}
	}
	if !row || !marker {
		t.Fatalf("publisher did not provide both status row and marker: %v", out.channels())
	}
}
