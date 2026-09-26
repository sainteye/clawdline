// Package schedulewebhook owns the Cloud schedule-webhook wire contract.
package schedulewebhook

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

const (
	ProtocolSchema = "clawdline.schedule_webhook.v1"
	ClaimSchema    = "clawdline.schedule_webhook.claim.v1"
	ReceiptSchema  = "clawdline.schedule_webhook.receipt.v1"
)

var (
	hookPattern     = regexp.MustCompile(`^swh_[0-9abcdefghjkmnpqrstvwxyz]{26}$`)
	deliveryPattern = regexp.MustCompile(`^swd_[0-9abcdefghjkmnpqrstvwxyz]{26}$`)
	leasePattern    = regexp.MustCompile(`^swhl_[0-9A-Za-z_-]{43}$`)
)

type Identity struct {
	AccountID string
	MachineID string
}

type Lease struct {
	Token     string `json:"token"`
	Revision  int    `json:"revision"`
	ExpiresAt string `json:"expires_at"`
}

type Claim struct {
	DeliveryID     string `json:"delivery_id"`
	HookID         string `json:"hook_id"`
	HookGeneration int    `json:"hook_generation"`
	AcceptedAt     string `json:"accepted_at"`
	ExpiresAt      string `json:"expires_at"`
	DeliveryDigest string `json:"delivery_digest"`
	Attempt        int    `json:"attempt"`
	Lease          Lease  `json:"lease"`
}

type ClaimResult struct {
	Delivery   *Claim
	ServerTime string
	PollAfter  time.Duration
}

type Receipt struct {
	Schema                 string  `json:"schema"`
	ReceiptVersion         int     `json:"receipt_version"`
	PreviousReceiptVersion int     `json:"previous_receipt_version"`
	Kind                   string  `json:"kind"`
	OccurredAt             string  `json:"occurred_at"`
	MacBuild               string  `json:"mac_build"`
	LeaseToken             *string `json:"lease_token"`
	TaskID                 *string `json:"task_id"`
	OutcomeCode            *string `json:"outcome_code"`
	TaskTerminalState      *string `json:"task_terminal_state"`
	RetryAt                *string `json:"retry_at"`
}

type ReceiptAck struct {
	DeliveryID     string
	ReceiptVersion int
	State          string
	AcknowledgedAt string
	Duplicate      bool
}

type Cloud interface {
	// Activate turns a bound hook on at the revision it is at: 0 for a new
	// hook, the one `/move` answered for a moved one.
	Activate(ctx context.Context, hookID, requestID string, expectedRevision int64) (int64, error)
	Claim(context.Context, int) (ClaimResult, error)
	Receipt(context.Context, string, Receipt) (ReceiptAck, error)
}

func (c Claim) Validate(identity Identity, now time.Time) error {
	deadline, err := time.Parse(time.RFC3339Nano, c.Lease.ExpiresAt)
	if err != nil {
		deadline, err = time.Parse(time.RFC3339, c.Lease.ExpiresAt)
	}
	if !deliveryPattern.MatchString(c.DeliveryID) || !hookPattern.MatchString(c.HookID) ||
		c.HookGeneration <= 0 || c.Attempt <= 0 || c.Lease.Revision <= 0 ||
		!leasePattern.MatchString(c.Lease.Token) || err != nil || !deadline.After(now) ||
		len(c.DeliveryDigest) != 64 {
		return errors.New("invalid schedule webhook claim")
	}
	canonical, _ := json.Marshal(map[string]any{
		"schema": ProtocolSchema, "delivery_id": c.DeliveryID, "hook_id": c.HookID,
		"hook_generation": c.HookGeneration, "account_id": identity.AccountID,
		"machine_id": identity.MachineID, "accepted_at": c.AcceptedAt, "expires_at": c.ExpiresAt,
	})
	sum := sha256.Sum256(canonical)
	if hex.EncodeToString(sum[:]) != c.DeliveryDigest {
		return errors.New("invalid schedule webhook delivery digest")
	}
	return nil
}
