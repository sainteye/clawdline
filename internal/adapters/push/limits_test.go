package push

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/capacity"
)

func subscriptionFor(device, endpoint string) Subscription {
	key := append([]byte{0x04}, bytes.Repeat([]byte{7}, SubscriberKeyBytes-1)...)
	return Subscription{
		ID: "id-" + device, Endpoint: endpoint, P256dh: key,
		Auth: bytes.Repeat([]byte{9}, AuthSecretBytes), Device: device,
		Origin: "https://app.clawdline.com", Created: time.Unix(1_700_000_000, 0),
	}
}

// limits N14: nothing bounded the subscriptions, and past the file's read
// bound every push fails. At the register's limit a new subscription is
// refused, typed, with nothing written; one that replaces a row still saves.
func TestSubscriptionsRefuseAnAdditionAtTheirLimit(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	s.SetLimit(2)
	for _, d := range []string{"a", "b"} {
		if err := s.Add(subscriptionFor(d, "https://push.example/"+d)); err != nil {
			t.Fatal(err)
		}
	}
	err = s.Add(subscriptionFor("c", "https://push.example/c"))
	if !errors.Is(err, ErrSubscriptionsFull) {
		t.Fatalf("a third device: %v, want ErrSubscriptionsFull", err)
	}
	// The same device again replaces its row: the count does not grow.
	again := subscriptionFor("a", "https://push.example/a2")
	again.ID = "id-a2"
	if err := s.Add(again); err != nil {
		t.Fatalf("a device re-subscribing at the limit: %v", err)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := reopened.Subscriptions()
	if err != nil || len(rows) != 2 {
		t.Fatalf("on disk: %d rows, %v", len(rows), err)
	}
	for _, r := range rows {
		if r.Device == "c" {
			t.Fatal("the refused subscription was written")
		}
	}
	r := s.Reading()
	if !r.Known || r.Used != 2 || r.Counters.Refused != 1 {
		t.Fatalf("reading: %+v", r)
	}
}

// The register's limit, every row at its longest, is under half of what the
// file is read up to: the limit speaks before the read bound can (limits N14).
func TestTheSubscriptionLimitSpeaksBeforeTheReadBound(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	limit := int(capacity.Default(capacity.PushSubscriptions))
	for i := 0; i < limit; i++ {
		// The longest endpoint FromBrowser takes, with the one character in it
		// an HTML-escaping encoder would have made six bytes.
		base := fmt.Sprintf("https://push.example/%04d?", i)
		endpoint := base + strings.Repeat("&", endpointLimit-len(base))
		if _, ok := FromBrowser(map[string]any{
			"endpoint": endpoint,
			"keys":     map[string]any{"p256dh": EncodeBase64URL(subscriptionFor("x", "").P256dh), "auth": EncodeBase64URL(make([]byte, AuthSecretBytes))},
		}, "x", "x", ""); !ok {
			t.Fatalf("FromBrowser refused an endpoint of %d bytes", len(endpoint))
		}
		device := fmt.Sprintf("%04d", i) + strings.Repeat("d", 124)
		sub := subscriptionFor(device, endpoint)
		sub.ID = fmt.Sprintf("%032d", i)
		if err := s.Add(sub); err != nil {
			t.Fatalf("row %d: %v", i, err)
		}
	}
	info, err := os.Stat(filepath.Join(s.Dir(), SubscriptionsFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size()*2 > subscriptionsLimit {
		t.Fatalf("%d subscriptions at their longest are %d bytes, not under half of the read bound %d", limit, info.Size(), subscriptionsLimit)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := reopened.Subscriptions(); err != nil || len(rows) != limit {
		t.Fatalf("the file the limit allows does not load: %d rows, %v", len(rows), err)
	}
	t.Logf("%d subscriptions at their longest: %d bytes of the %d the daemon reads", limit, info.Size(), subscriptionsLimit)
}

func TestFromBrowserRefusesAnEndpointTooLongOrOddlySpelled(t *testing.T) {
	keys := map[string]any{"p256dh": EncodeBase64URL(subscriptionFor("x", "").P256dh), "auth": EncodeBase64URL(make([]byte, AuthSecretBytes))}
	for name, endpoint := range map[string]string{
		"too long":  "https://push.example/" + strings.Repeat("a", endpointLimit),
		"space":     "https://push.example/a b",
		"quote":     `https://push.example/a"b`,
		"angle":     "https://push.example/<b>",
		"non-ascii": "https://push.example/é",
	} {
		if _, ok := FromBrowser(map[string]any{"endpoint": endpoint, "keys": keys}, "x", "x", ""); ok {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, ok := FromBrowser(map[string]any{"endpoint": "https://fcm.googleapis.com/fcm/send/abc:DEF-_ghi", "keys": keys}, "x", "x", ""); !ok {
		t.Error("an ordinary endpoint was refused")
	}
}
