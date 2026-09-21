package app

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math/rand/v2"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/schedulewebhook"
)

const (
	webhookPrepared = "prepared"
	webhookDurable  = "mac_durable_accepted"
	webhookDeferred = "schedule_dispatch_deferred"
	webhookAccepted = "schedule_dispatch_accepted"
	webhookRefused  = "schedule_dispatch_refused"
	webhookTerminal = "task_execution_terminal"
)

// ScheduleWebhookRuntime turns Cloud deliveries into ordinary scheduled broker
// tasks. Its Store row is the authority across crashes; Cloud receipts are
// always persisted before they leave the machine.
type ScheduleWebhookRuntime struct {
	Cloud    schedulewebhook.Cloud
	Identity schedulewebhook.Identity
	Store    *store.Store
	Book     *ScheduleBook
	Build    string
	Now      func() time.Time
	Log      func(string, ...any)
}

func (r *ScheduleWebhookRuntime) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *ScheduleWebhookRuntime) logf(format string, args ...any) {
	if r.Log != nil {
		r.Log(format, args...)
		return
	}
	log.Printf(format, args...)
}

func (r *ScheduleWebhookRuntime) Run(ctx context.Context) {
	failures := 0
	for ctx.Err() == nil {
		delay, err := r.PollOnce(ctx, 20)
		if err == nil {
			failures = 0
			if delay < 0 {
				delay = 0
			}
			if delay > 5*time.Minute {
				delay = 5 * time.Minute
			}
			delay = time.Duration(float64(delay) * (0.8 + rand.Float64()*0.4))
		} else {
			var api *adaptercloud.APIError
			if errors.As(err, &api) && api.Status == 401 && api.Code == "no_machine_credential" {
				r.logf("schedule webhooks: machine credential was refused: %v", err)
				return
			}
			failures++
			cap := time.Second << min(failures-1, 5)
			delay = time.Duration(rand.Int64N(int64(cap) + 1))
			r.logf("schedule webhooks: poll failed; retrying in %s: %v", delay, err)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (r *ScheduleWebhookRuntime) PollOnce(ctx context.Context, waitSeconds int) (time.Duration, error) {
	rows, err := r.Store.ScheduleWebhookDeliveriesToResume(ctx)
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		if err := r.advance(ctx, row); err != nil {
			return 0, err
		}
	}
	_ = r.Store.CleanupScheduleWebhookDeliveries(ctx, r.now().Add(-30*24*time.Hour))
	result, err := r.Cloud.Claim(ctx, waitSeconds)
	if err != nil {
		return 0, err
	}
	if result.Delivery == nil {
		return result.PollAfter, nil
	}
	claim := *result.Delivery
	if err := claim.Validate(r.Identity, r.now()); err != nil {
		return 0, err
	}
	return 0, r.admit(ctx, claim)
}

func (r *ScheduleWebhookRuntime) admit(ctx context.Context, claim schedulewebhook.Claim) error {
	scheduleID, bound, bindingErr := r.binding(ctx, claim.HookID)
	outcome := ""
	if bindingErr != nil {
		outcome = "binding_store_unavailable"
	}
	if bindingErr == nil && !bound {
		outcome = "hook_unbound"
	}
	taskID := ""
	if scheduleID != "" {
		taskID = newUUID()
	}
	now := r.now().Unix()
	fresh := store.ScheduleWebhookDelivery{
		Version: 1, DeliveryID: claim.DeliveryID, DeliveryDigest: claim.DeliveryDigest,
		HookID: claim.HookID, HookGeneration: claim.HookGeneration, ScheduleID: scheduleID,
		PreflightOutcome: outcome, State: webhookPrepared, TaskID: taskID,
		LeaseToken: claim.Lease.Token, LeaseRevision: claim.Lease.Revision,
		CreatedAt: now, UpdatedAt: now,
	}
	row, _, err := r.Store.ReserveScheduleWebhookDelivery(ctx, fresh)
	if err != nil {
		return err
	}
	if row.CloudAcknowledgedThrough == 0 {
		row.LeaseToken, row.LeaseRevision, row.UpdatedAt = claim.Lease.Token, claim.Lease.Revision, now
		if len(row.PendingReceipt) != 0 {
			var pending schedulewebhook.Receipt
			if json.Unmarshal(row.PendingReceipt, &pending) == nil && pending.ReceiptVersion == 1 {
				pending.LeaseToken = &row.LeaseToken
				row.PendingReceipt, _ = json.Marshal(pending)
			}
		}
		if err := r.Store.SaveScheduleWebhookDelivery(ctx, row); err != nil {
			return err
		}
	}
	return r.advance(ctx, row)
}

func (r *ScheduleWebhookRuntime) binding(ctx context.Context, hookID string) (string, bool, error) {
	return r.Book.Store.ScheduleForWebhook(ctx, hookID)
}

func (r *ScheduleWebhookRuntime) advance(ctx context.Context, row store.ScheduleWebhookDelivery) error {
	if len(row.PendingReceipt) != 0 {
		var receipt schedulewebhook.Receipt
		if err := json.Unmarshal(row.PendingReceipt, &receipt); err != nil {
			return err
		}
		ack, err := r.Cloud.Receipt(ctx, row.DeliveryID, receipt)
		if err != nil {
			var api *adaptercloud.APIError
			if errors.As(err, &api) {
				switch api.Code {
				case "delivery_canceled":
					return r.finishWithoutReceipt(ctx, row, "canceled")
				case "delivery_expired":
					return r.finishWithoutReceipt(ctx, row, "expired")
				case "stale_lease":
					if receipt.ReceiptVersion == 1 {
						return nil
					}
				}
			}
			return err
		}
		row.CloudAcknowledgedThrough = ack.ReceiptVersion
		row.PendingReceipt = nil
		row.UpdatedAt = r.now().Unix()
		if err := r.Store.SaveScheduleWebhookDelivery(ctx, row); err != nil {
			return err
		}
	}
	if row.CloudAcknowledgedThrough == 0 {
		return r.queueAndAdvance(ctx, row, webhookDurable, webhookDurable, "", "", time.Time{}, false)
	}
	if row.State == webhookDurable || row.State == webhookDeferred {
		if row.RetryAt > r.now().Unix() {
			return nil
		}
		if row.PreflightOutcome != "" {
			return r.queueAndAdvance(ctx, row, webhookRefused, webhookRefused, row.PreflightOutcome, "", time.Time{}, true)
		}
		if row.ScheduleID == "" || row.TaskID == "" {
			return r.queueAndAdvance(ctx, row, webhookRefused, "outcome_unknown", "outcome_unknown", "", time.Time{}, true)
		}
		reply := r.Book.RunWebhook(ctx, row.ScheduleID, row.TaskID)
		if reply.OK() {
			return r.queueAndAdvance(ctx, row, webhookAccepted, webhookAccepted, "", "", time.Time{}, false)
		}
		if reply.Code == "over_capacity" || reply.Code == "terminal_busy" {
			retry := r.now().Add(time.Duration(rand.Int64N(int64(time.Minute) + 1)))
			return r.queueAndAdvance(ctx, row, webhookDeferred, webhookDeferred, reply.Code, "", retry, false)
		}
		code := reply.Code
		switch code {
		case "schedule_not_found", "schedule_disabled", "schedule_spent", "schedule_active", "orchestrator_disabled":
		default:
			code = "dispatch_failed"
		}
		return r.queueAndAdvance(ctx, row, webhookRefused, webhookRefused, code, "", time.Time{}, true)
	}
	if row.State == webhookAccepted {
		state, err := r.Book.WebhookTaskState(ctx, row.ScheduleID, row.TaskID)
		if err != nil {
			return err
		}
		terminal := map[string]string{"success": "success", "failure": "failure", "timeout": "timed_out", "cancelled": "cancelled", "spawn_failed": "spawn_failed"}
		if cloudState := terminal[state]; cloudState != "" {
			return r.queueAndAdvance(ctx, row, webhookTerminal, webhookTerminal, "", cloudState, time.Time{}, true)
		}
	}
	return nil
}

func (r *ScheduleWebhookRuntime) queueAndAdvance(ctx context.Context, row store.ScheduleWebhookDelivery, kind, state, outcome, terminal string, retry time.Time, isTerminal bool) error {
	now := r.now()
	next := row.LastReceiptVersion + 1
	receipt := schedulewebhook.Receipt{Schema: schedulewebhook.ReceiptSchema, ReceiptVersion: next,
		PreviousReceiptVersion: row.LastReceiptVersion, Kind: kind, OccurredAt: now.UTC().Format(time.RFC3339Nano), MacBuild: r.Build}
	if next == 1 {
		receipt.LeaseToken = &row.LeaseToken
	}
	if kind != webhookDurable && row.TaskID != "" {
		receipt.TaskID = &row.TaskID
	}
	if outcome != "" {
		receipt.OutcomeCode = &outcome
	}
	if terminal != "" {
		receipt.TaskTerminalState = &terminal
	}
	if !retry.IsZero() {
		value := retry.UTC().Format(time.RFC3339Nano)
		receipt.RetryAt = &value
		row.RetryAt = retry.Unix()
	} else {
		row.RetryAt = 0
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	row.PendingReceipt, row.LastReceiptVersion, row.State, row.UpdatedAt = raw, next, state, now.Unix()
	if isTerminal {
		row.TerminalAt = row.UpdatedAt
	}
	if err := r.Store.SaveScheduleWebhookDelivery(ctx, row); err != nil {
		return err
	}
	return r.advance(ctx, row)
}

func (r *ScheduleWebhookRuntime) finishWithoutReceipt(ctx context.Context, row store.ScheduleWebhookDelivery, state string) error {
	row.State, row.PendingReceipt, row.TerminalAt, row.UpdatedAt = state, nil, r.now().Unix(), r.now().Unix()
	return r.Store.SaveScheduleWebhookDelivery(ctx, row)
}
