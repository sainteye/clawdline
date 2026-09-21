package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/schedulewebhook"
)

func TestScheduleWebhookClientUsesMachineCredentialAndIdempotencyKeys(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/schedule-webhooks/swh_test/activate", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer machine-secret" || r.Header.Get("Idempotency-Key") != "request-1" {
			t.Errorf("activation headers: %v", r.Header)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["expected_revision"] != float64(0) {
			t.Errorf("activation body: %v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"schema": "clawdline.schedule_webhook.management.v1",
			"hook": map[string]any{"hook_id": "swh_test", "state": "active", "revision": 1}})
	})
	mux.HandleFunc("/v1/schedule-webhook-deliveries/claim", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer machine-secret" {
			t.Errorf("claim authorization: %v", r.Header)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"schema": schedulewebhook.ClaimSchema,
			"delivery": nil, "server_time": "2026-09-21T00:00:00Z", "poll_after_ms": 250})
	})
	mux.HandleFunc("/v1/schedule-webhook-deliveries/swd_test/receipts", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") != "receipt:swd_test:1" {
			t.Errorf("receipt headers: %v", r.Header)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["task_id"]; !ok || body["task_id"] != nil {
			t.Errorf("receipt must carry task_id:null: %v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"schema": "clawdline.schedule_webhook.receipt_ack.v1",
			"delivery_id": "swd_test", "receipt_version": 1, "state": "accepted",
			"acknowledged_at": "2026-09-21T00:00:00Z", "duplicate": false})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := ScheduleWebhookClient{Client: NewAccountClient(server.URL), Credential: "machine-secret"}
	if revision, err := client.Activate(context.Background(), "swh_test", "request-1"); err != nil || revision != 1 {
		t.Fatalf("activate: %d %v", revision, err)
	}
	if claim, err := client.Claim(context.Background(), 20); err != nil || claim.Delivery != nil || claim.PollAfter.Milliseconds() != 250 {
		t.Fatalf("claim: %+v %v", claim, err)
	}
	receipt := schedulewebhook.Receipt{Schema: schedulewebhook.ReceiptSchema, ReceiptVersion: 1,
		Kind: "mac_durable_accepted", OccurredAt: "2026-09-21T00:00:00Z", MacBuild: "test"}
	if ack, err := client.Receipt(context.Background(), "swd_test", receipt); err != nil || ack.ReceiptVersion != 1 {
		t.Fatalf("receipt: %+v %v", ack, err)
	}
}
