package push

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
)

func TestSyntheticEgressPush(t *testing.T) {
	for _, declarative := range []bool{false, true} {
		name := "worker"
		if declarative {
			name = "declarative"
		}
		t.Run(name, func(t *testing.T) {
			b := newBrowser(t)
			endpoint := "https://push.example.invalid/synthetic"
			if declarative {
				endpoint = "https://web.push.apple.com/synthetic"
			}
			sub := cloudRow(b, "synthetic", endpoint, "synthetic-viewer")
			sub.Origin = "https://viewer.example.invalid"
			n := Notification{Title: "CANARY_PUSH_TITLE", Body: "CANARY_PUSH_BODY", URL: "/#session=synthetic", Tag: "synthetic", At: time.Unix(1700000000, 0)}
			message, _, ok := Build(n, sub)
			if !ok || !bytes.Contains(message.Plaintext, []byte(n.Title)) || !bytes.Contains(message.Plaintext, []byte(n.Body)) {
				t.Fatal("pre-encryption canaries missing")
			}
			calls := 0
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				calls++
				raw, err := io.ReadAll(req.Body)
				if err != nil {
					t.Error(err)
					return
				}
				var payload struct {
					Ciphertext     string `json:"ciphertext"`
					SubscriptionID string `json:"subscription_id"`
				}
				if err := json.Unmarshal(raw, &payload); err != nil {
					t.Error(err)
					return
				}
				sealed, err := DecodeBase64URL(payload.Ciphertext)
				if err != nil {
					t.Error(err)
					return
				}
				leaked := syntheticPushPlaintext(raw, sealed)
				if req.URL.Path == "/negative/v1/push/send" {
					if !leaked {
						t.Error("detector missed base64-wrapped plaintext")
					}
					w.WriteHeader(http.StatusUnprocessableEntity)
					return
				}
				if leaked {
					t.Error("Cloud receiver got notification plaintext")
				}
				if payload.SubscriptionID != cloudSubscription {
					t.Error("subscription routing metadata missing")
				}
				if !bytes.Equal(b.open(t, sealed), message.Plaintext) {
					t.Error("subscriber did not recover exact lock-screen content")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"sent","push_status":201}`))
			}))
			defer receiver.Close()
			store := openStore(t)
			if err := store.Add(sub); err != nil {
				t.Fatal(err)
			}
			var slept []time.Duration
			sender := senderFor(t, store, &slept)
			// An accidental direct-send regression must fail locally, even for
			// the Apple-shaped subscription needed to select declarative JSON.
			sender.Client = &http.Client{Transport: syntheticPushNoNetwork{}}
			sender.Cloud = courier{adaptercloud.NewPushClient(receiver.URL, machineCredential)}
			delivery, err := sender.Send(context.Background(), n, "")
			if err != nil || delivery.Sent != 1 || delivery.Failed != 0 {
				t.Fatalf("send: %+v %v", delivery, err)
			}
			negative := adaptercloud.NewPushClient(receiver.URL+"/negative", machineCredential)
			reply, err := negative.Send(context.Background(), adaptercloud.PushSendRequest{SubscriptionID: cloudSubscription, Ciphertext: message.Plaintext})
			if err != nil || reply.Status != http.StatusUnprocessableEntity {
				t.Fatal("negative control not refused", err)
			}
			if calls != 2 {
				t.Fatalf("got %d calls, want one sealed and one negative", calls)
			}
			t.Logf("SYNTHETIC_EGRESS sample=push-%s fixture_sha256=%x calls=%d negative_controls=1 subscriber_open=pass lock_screen_contains_title_body=true", name, sha256.Sum256(message.Plaintext), calls)
		})
	}
}

func syntheticPushPlaintext(raw, decoded []byte) bool {
	return bytes.Contains(raw, []byte("CANARY_PUSH_")) || bytes.Contains(decoded, []byte("CANARY_PUSH_"))
}

type syntheticPushNoNetwork struct{}

func (syntheticPushNoNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("synthetic egress forbids direct push-service traffic")
}
