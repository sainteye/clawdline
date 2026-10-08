package cloud

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
)

// ViewerPairingStart is the one-time routing handle for an encrypted handover.
type ViewerPairingStart struct {
	PairingID  string `json:"pairing_id"`
	ClaimNonce string `json:"claim_nonce"`
	ExpiresAt  string `json:"expires_at"`
	ExpiresIn  int    `json:"expires_in"`
}

// StartViewerPairing gives the viewer a handle that an already paired machine can answer.
func (c *AccountClient) StartViewerPairing(ctx context.Context, credential, fingerprint string) (ViewerPairingStart, error) {
	var out ViewerPairingStart
	if err := c.post(ctx, "/v1/pairing/start", credential, map[string]any{"fingerprint": fingerprint}, &out); err != nil {
		return ViewerPairingStart{}, err
	}
	if out.PairingID == "" || out.ClaimNonce == "" || out.ExpiresIn <= 0 {
		return ViewerPairingStart{}, fmt.Errorf("%w: pairing start was incomplete", ErrIncompatible)
	}
	return out, nil
}

type ViewerPairingClaim struct {
	Ciphertext     string `json:"ciphertext"`
	SenderDeviceID string `json:"sender_device_id"`
}

// ClaimViewerPairing returns ErrPairingPending while the machine has not answered.
func (c *AccountClient) ClaimViewerPairing(ctx context.Context, credential, pairingID, claimNonce string) (ViewerPairingClaim, error) {
	var out ViewerPairingClaim
	err := c.post(ctx, "/v1/pairing/claim", credential, map[string]any{
		"pairing_id": pairingID, "claim_nonce": claimNonce,
	}, &out)
	if err != nil {
		return ViewerPairingClaim{}, err
	}
	if out.Ciphertext == "" && out.SenderDeviceID == "" {
		return ViewerPairingClaim{}, ErrPairingPending
	}
	if out.Ciphertext == "" || out.SenderDeviceID == "" {
		return ViewerPairingClaim{}, fmt.Errorf("%w: pairing claim was incomplete", ErrIncompatible)
	}
	if _, err := base64.StdEncoding.DecodeString(out.Ciphertext); err != nil {
		return ViewerPairingClaim{}, errors.Join(ErrIncompatible, err)
	}
	return out, nil
}
