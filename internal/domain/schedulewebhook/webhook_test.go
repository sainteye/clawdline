package schedulewebhook

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDeliveryDigestUsesTheProtocolSchema(t *testing.T) {
	now := time.Date(2026, 9, 22, 1, 2, 3, 456000000, time.UTC)
	identity := Identity{AccountID: "account-1", MachineID: "machine-1"}
	claim := Claim{
		DeliveryID:     "swd_" + strings.Repeat("2", 26),
		HookID:         "swh_" + strings.Repeat("3", 26),
		HookGeneration: 1,
		AcceptedAt:     now.Format(time.RFC3339Nano),
		ExpiresAt:      now.Add(time.Hour).Format(time.RFC3339Nano),
		Attempt:        1,
		Lease: Lease{
			Token:     "swhl_" + strings.Repeat("A", 43),
			Revision:  1,
			ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano),
		},
	}
	canonical, err := json.Marshal(map[string]any{
		"schema": ProtocolSchema, "delivery_id": claim.DeliveryID, "hook_id": claim.HookID,
		"hook_generation": claim.HookGeneration, "account_id": identity.AccountID,
		"machine_id": identity.MachineID, "accepted_at": claim.AcceptedAt, "expires_at": claim.ExpiresAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	claim.DeliveryDigest = hex.EncodeToString(sum[:])
	if err := claim.Validate(identity, now); err != nil {
		t.Fatalf("a Cloud protocol digest was refused: %v", err)
	}
}
