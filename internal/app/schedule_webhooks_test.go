package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/schedulewebhook"
)

type webhookCloudFake struct {
	claim    *schedulewebhook.Claim
	receipts []schedulewebhook.Receipt
	// refuse, when set, is Cloud's answer to a receipt it will not take.
	refuse func(schedulewebhook.Receipt) error
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
	if f.refuse != nil {
		if err := f.refuse(receipt); err != nil {
			return schedulewebhook.ReceiptAck{}, err
		}
	}
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

// countingBroker is the broker with a count in front of it, so a test can say
// DispatchScheduled was never reached rather than that no tab happened to open.
type countingBroker struct {
	inner ScheduleBroker
	calls int
}

func (c *countingBroker) DispatchScheduled(ctx context.Context, run orchestrator.ScheduledRun) (orchestrator.ScheduledDispatch, error) {
	c.calls++
	return c.inner.DispatchScheduled(ctx, run)
}

// webhookFixture is one bound schedule and a runtime around a fake Cloud.
func webhookFixture(t *testing.T, when map[string]any, enabled bool) (*w4Fixture, *webhookCloudFake, *countingBroker, *ScheduleWebhookRuntime, schedulewebhook.Claim) {
	t.Helper()
	f := newW4(t)
	id := "5c000021-0000-4000-8000-000000000021"
	body, _ := json.Marshal(map[string]any{
		"clawdline_schedule": 1, "schedule_id": id, "title": "deploy",
		"when": when, "enabled": enabled, "created_at": f.at().Add(-72 * time.Hour).Unix(),
		"task": map[string]any{"assistant": "claude", "project_dir": f.dir, "title": "deploy",
			"instructions": "deploy it", "claims": []string{}},
	})
	if err := f.st.CreateScheduleFile(context.Background(), id, body, f.at().Add(-72*time.Hour), time.Time{}); err != nil {
		t.Fatal(err)
	}
	hook := "swh_" + strings.Repeat("b", 26)
	if err := f.st.BindScheduleWebhook(context.Background(), hook, id, "", f.at()); err != nil {
		t.Fatal(err)
	}
	counting := &countingBroker{inner: f.b}
	f.book.Broker = counting
	identity := schedulewebhook.Identity{AccountID: "acct_test", MachineID: "mac_test"}
	cloud := &webhookCloudFake{}
	claim := webhookClaim(identity, f.at(), hook)
	runtime := &ScheduleWebhookRuntime{Cloud: cloud, Identity: identity, Store: f.st, Book: f.book,
		Build: "test+1", Now: f.at, Log: func(string, ...any) {}}
	return f, cloud, counting, runtime, claim
}

// C2, failure injected: Cloud refuses the version-1 receipt because the
// delivery's deadline passed. The task must not start — not on this poll, not
// on a later one, and not if Cloud offers the same delivery again — and the
// refusal is not retried.
func TestAnExpiredDeliveryNeverReachesTheBroker(t *testing.T) {
	f, cloud, counting, runtime, claim := webhookFixture(t, map[string]any{"at": "09:00", "days": "daily"}, true)
	cloud.refuse = func(r schedulewebhook.Receipt) error {
		return &adaptercloud.APIError{Status: 409, Code: "delivery_expired", Message: "the delivery's deadline has passed"}
	}
	var logged []string
	runtime.Log = func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }
	cloud.claim = &claim
	if _, err := runtime.PollOnce(context.Background(), 0); err != nil {
		t.Fatalf("an expired delivery is an outcome, not an error: %v", err)
	}
	row, err := f.st.ScheduleWebhookDelivery(context.Background(), claim.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	if row.State != webhookExpired || row.TerminalAt == 0 || len(row.PendingReceipt) != 0 {
		t.Fatalf("journal after delivery_expired: %+v", row)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "expired") {
		t.Fatalf("log lines: %q", logged)
	}
	// Later polls, and the same delivery offered again, change nothing.
	for i := 0; i < 2; i++ {
		cloud.claim = &claim
		if _, err := runtime.PollOnce(context.Background(), 0); err != nil {
			t.Fatal(err)
		}
	}
	if counting.calls != 0 || f.term.opened() != 0 {
		t.Fatalf("an expired delivery reached the broker %d times and opened %d tabs", counting.calls, f.term.opened())
	}
	if len(cloud.receipts) != 1 || cloud.receipts[0].ReceiptVersion != 1 {
		t.Fatalf("receipts sent: %+v, want only the refused version 1", cloud.receipts)
	}
	if runs := f.runs(t, row.ScheduleID); len(runs) != 0 {
		t.Fatalf("an expired delivery made runs: %+v", runs)
	}
}

// Drive-it-red control for the test above: the same fixture, with Cloud
// accepting the receipt, does reach the broker once.
func TestAnAcceptedDeliveryReachesTheBrokerOnce(t *testing.T) {
	_, cloud, counting, runtime, claim := webhookFixture(t, map[string]any{"at": "09:00", "days": "daily"}, true)
	cloud.claim = &claim
	if _, err := runtime.PollOnce(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if counting.calls != 1 {
		t.Fatalf("broker reached %d times, want once", counting.calls)
	}
}

// C5: a trigger-only schedule runs by webhook while enabled, and a disabled
// one is refused with the stable code a refusal receipt carries.
func TestATriggerOnlyScheduleRunsByWebhookOnlyWhileEnabled(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		_, cloud, counting, runtime, claim := webhookFixture(t, map[string]any{"trigger_only": true}, enabled)
		cloud.claim = &claim
		if _, err := runtime.PollOnce(context.Background(), 0); err != nil {
			t.Fatal(err)
		}
		if len(cloud.receipts) != 2 || cloud.receipts[0].Kind != webhookDurable {
			t.Fatalf("enabled=%v receipts: %+v", enabled, cloud.receipts)
		}
		last := cloud.receipts[1]
		if enabled {
			if counting.calls != 1 || last.Kind != webhookAccepted {
				t.Fatalf("an enabled trigger-only schedule: %d dispatches, receipt %+v", counting.calls, last)
			}
			continue
		}
		if counting.calls != 0 || last.Kind != webhookRefused || last.OutcomeCode == nil ||
			*last.OutcomeCode != "schedule_disabled" {
			t.Fatalf("a disabled trigger-only schedule: %d dispatches, receipt %+v", counting.calls, last)
		}
	}
}

// Every state the broker ends a task in is reported as a terminal outcome,
// and no other state is: a new terminal state with no mapping would leave the
// caller of `webhook fire` waiting forever.
func TestEveryTerminalTaskStateHasAWebhookOutcome(t *testing.T) {
	want := map[orchestrator.State]string{
		orchestrator.StateSuccess: "success", orchestrator.StateFailure: "failure",
		orchestrator.StateTimeout: "timed_out", orchestrator.StateCancelled: "cancelled",
		orchestrator.StateSpawnFailed: "spawn_failed",
	}
	for _, s := range []orchestrator.State{orchestrator.StateQueued, orchestrator.StateSpawning,
		orchestrator.StateBriefed, orchestrator.StateSuccess, orchestrator.StateFailure,
		orchestrator.StateTimeout, orchestrator.StateCancelled, orchestrator.StateSpawnFailed, ""} {
		got := webhookTerminalOutcome(string(s))
		if got != want[s] || (got != "") != s.Terminal() {
			t.Fatalf("state %q reported as %q, want %q", s, got, want[s])
		}
	}
}
