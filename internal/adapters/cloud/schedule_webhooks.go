package cloud

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/sainteye/clawdline/internal/domain/schedulewebhook"
)

// ScheduleWebhookClient is the machine-credential side of the public webhook
// protocol. It deliberately carries no trigger URL: that one-time secret is a
// browser concern and must never enter this daemon's store or logs.
type ScheduleWebhookClient struct {
	Client     *AccountClient
	Credential string
}

func (c ScheduleWebhookClient) Activate(ctx context.Context, hookID, requestID string) (int64, error) {
	path := "/v1/schedule-webhooks/" + url.PathEscape(hookID) + "/activate"
	var out struct {
		Schema string `json:"schema"`
		Hook   struct {
			HookID   string `json:"hook_id"`
			State    string `json:"state"`
			Revision int64  `json:"revision"`
		} `json:"hook"`
	}
	if err := c.Client.postWithHeaders(ctx, path, c.Credential,
		map[string]any{"expected_revision": 0}, map[string]string{"Idempotency-Key": requestID}, &out); err != nil {
		return 0, err
	}
	if out.Schema != "clawdline.schedule_webhook.management.v1" || out.Hook.HookID != hookID ||
		out.Hook.State != "active" || out.Hook.Revision != 1 {
		return 0, fmt.Errorf("%w: invalid schedule webhook activation", ErrIncompatible)
	}
	return out.Hook.Revision, nil
}

func (c ScheduleWebhookClient) Claim(ctx context.Context, waitSeconds int) (schedulewebhook.ClaimResult, error) {
	if waitSeconds < 0 || waitSeconds > 25 {
		return schedulewebhook.ClaimResult{}, fmt.Errorf("%w: invalid webhook wait", ErrIncompatible)
	}
	var out struct {
		Schema     string                 `json:"schema"`
		Delivery   *schedulewebhook.Claim `json:"delivery"`
		ServerTime string                 `json:"server_time"`
		PollAfter  int                    `json:"poll_after_ms"`
	}
	if err := c.Client.post(ctx, "/v1/schedule-webhook-deliveries/claim", c.Credential,
		map[string]any{"protocol": "clawdline.schedule_webhook.v1", "wait_seconds": waitSeconds}, &out); err != nil {
		return schedulewebhook.ClaimResult{}, err
	}
	if out.Schema != schedulewebhook.ClaimSchema || out.ServerTime == "" || out.PollAfter < 0 || out.PollAfter > 300000 {
		return schedulewebhook.ClaimResult{}, fmt.Errorf("%w: invalid schedule webhook claim", ErrIncompatible)
	}
	return schedulewebhook.ClaimResult{Delivery: out.Delivery, ServerTime: out.ServerTime,
		PollAfter: time.Duration(out.PollAfter) * time.Millisecond}, nil
}

func (c ScheduleWebhookClient) Receipt(ctx context.Context, deliveryID string, receipt schedulewebhook.Receipt) (schedulewebhook.ReceiptAck, error) {
	path := "/v1/schedule-webhook-deliveries/" + url.PathEscape(deliveryID) + "/receipts"
	var out struct {
		Schema         string `json:"schema"`
		DeliveryID     string `json:"delivery_id"`
		ReceiptVersion int    `json:"receipt_version"`
		State          string `json:"state"`
		AcknowledgedAt string `json:"acknowledged_at"`
		Duplicate      bool   `json:"duplicate"`
	}
	key := fmt.Sprintf("receipt:%s:%d", deliveryID, receipt.ReceiptVersion)
	if err := c.Client.postWithHeaders(ctx, path, c.Credential, receipt,
		map[string]string{"Idempotency-Key": key}, &out); err != nil {
		return schedulewebhook.ReceiptAck{}, err
	}
	if out.Schema != "clawdline.schedule_webhook.receipt_ack.v1" || out.DeliveryID != deliveryID ||
		out.ReceiptVersion != receipt.ReceiptVersion || out.State == "" || out.AcknowledgedAt == "" {
		return schedulewebhook.ReceiptAck{}, fmt.Errorf("%w: invalid schedule webhook receipt acknowledgement", ErrIncompatible)
	}
	return schedulewebhook.ReceiptAck{DeliveryID: out.DeliveryID, ReceiptVersion: out.ReceiptVersion,
		State: out.State, AcknowledgedAt: out.AcknowledgedAt, Duplicate: out.Duplicate}, nil
}
