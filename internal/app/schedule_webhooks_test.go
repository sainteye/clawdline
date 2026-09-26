package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/schedulewebhook"
)

type webhookCloudFake struct {
	claim    *schedulewebhook.Claim
	receipts []schedulewebhook.Receipt
}

func (f *webhookCloudFake) Activate(_ context.Context, _, _ string, at int64) (int64, error) {
	return at + 1, nil
}
func (f *webhookCloudFake) Claim(context.Context, int) (schedulewebhook.ClaimResult, error) {
	claim := f.claim
	f.claim = nil
	return schedulewebhook.ClaimResult{Delivery: claim, ServerTime: "2026-09-18T09:00:30Z"}, nil
}
func (f *webhookCloudFake) Receipt(_ context.Context, delivery string, receipt schedulewebhook.Receipt) (schedulewebhook.ReceiptAck, error) {
	f.receipts = append(f.receipts, receipt)
	return schedulewebhook.ReceiptAck{DeliveryID: delivery, ReceiptVersion: receipt.ReceiptVersion,
		State: "accepted", AcknowledgedAt: "2026-09-18T09:00:30Z"}, nil
}

func webhookClaim(identity schedulewebhook.Identity, now time.Time, hook string) schedulewebhook.Claim {
	c := schedulewebhook.Claim{DeliveryID: "swd_" + strings.Repeat("a", 26), HookID: hook,
		HookGeneration: 1, AcceptedAt: now.UTC().Format(time.RFC3339Nano),
		ExpiresAt: now.Add(time.Hour).UTC().Format(time.RFC3339Nano), Attempt: 1,
		Lease: schedulewebhook.Lease{Token: "swhl_" + strings.Repeat("a", 43), Revision: 1,
			ExpiresAt: now.Add(time.Minute).UTC().Format(time.RFC3339Nano)}}
	canonical, _ := json.Marshal(map[string]any{
		"schema": schedulewebhook.ProtocolSchema, "delivery_id": c.DeliveryID, "hook_id": c.HookID,
		"hook_generation": c.HookGeneration, "account_id": identity.AccountID,
		"machine_id": identity.MachineID, "accepted_at": c.AcceptedAt, "expires_at": c.ExpiresAt,
	})
	sum := sha256.Sum256(canonical)
	c.DeliveryDigest = hex.EncodeToString(sum[:])
	return c
}

// Before the webhook runtime existed the bound hook was never claimed. This
// exercises the real broker path and proves admission is journaled and
// receipted in order, with receipt v1 not leaking the preallocated task id.
func TestWebhookDeliveryDispatchesExactlyOnceAndReceiptsAdmission(t *testing.T) {
	f := newW4(t)
	id := f.schedule(t, "5c000020-0000-4000-8000-000000000020", "webhook repair", map[string]any{"claims": []string{}})
	hook := "swh_" + strings.Repeat("a", 26)
	if err := f.st.BindScheduleWebhook(context.Background(), hook, id, "", f.at()); err != nil {
		t.Fatal(err)
	}
	identity := schedulewebhook.Identity{AccountID: "acct_test", MachineID: "mac_test"}
	cloud := &webhookCloudFake{}
	claim := webhookClaim(identity, f.at(), hook)
	cloud.claim = &claim
	runtime := &ScheduleWebhookRuntime{Cloud: cloud, Identity: identity, Store: f.st, Book: f.book,
		Build: "test+1", Now: f.at}
	if _, err := runtime.PollOnce(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if got := f.term.opened(); got != 1 {
		t.Fatalf("opened %d tabs, want one", got)
	}
	if len(cloud.receipts) != 2 || cloud.receipts[0].Kind != webhookDurable ||
		cloud.receipts[0].TaskID != nil || cloud.receipts[1].Kind != webhookAccepted ||
		cloud.receipts[1].TaskID == nil {
		t.Fatalf("receipts: %+v", cloud.receipts)
	}
	row, err := f.st.ScheduleWebhookDelivery(context.Background(), claim.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	if row.State != webhookAccepted || row.CloudAcknowledgedThrough != 2 || row.TaskID == "" {
		t.Fatalf("journal after dispatch: %+v", row)
	}
	// Replaying the same durable delivery cannot open a second tab.
	cloud.claim = &claim
	if _, err := runtime.PollOnce(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if got := f.term.opened(); got != 1 {
		t.Fatalf("replay opened %d tabs, want one", got)
	}
}
