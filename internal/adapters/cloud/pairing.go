package cloud

// The control plane's pairing routes, from the machine's side.
//
// Five calls, and the machine only ever makes four of them:
//
//	POST /v1/pairing/invitations/start   show a one-time QR; the cloud is told
//	                                     SHA-256 of the secret and nothing else
//	POST /v1/pairing/invitations/poll    collect the browser's encrypted offer
//	POST /v1/pairing/complete            leave the sealed handover in the one slot
//	POST /v1/machines/:id/identity/rotate   replace this machine's signing key
//
// `POST /v1/pairing/start` and `/claim` belong to the browser: it asks for the
// routing handle and it takes the blob. Nothing here does either.
//
// Everything travels as opaque base64. `internal/domain/cloud` owns what those
// bytes mean; this file owns getting them there and reading the typed refusals
// back, because "the QR expired" and "that browser is on another account" send
// a person to two different places and a 409 says neither by itself.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/cloud"
)

// Pairing refusals, typed. They are `errors.Is`-able because the settings page
// and the CLI both turn them into a sentence, and a string match would drift.
var (
	// ErrPairingPending is not a failure: the browser has not answered yet.
	// Reading it as an error is what made pairing impossible rather than slow
	// in the Swift app's first attempt.
	ErrPairingPending = errors.New("the browser has not answered this invitation yet")
	// ErrInvitationGone is an invitation that expired or was already used.
	ErrInvitationGone = errors.New("that pairing invitation has expired or was already used")
	// ErrPairingRefused is the control plane saying this is not ours to do.
	ErrPairingRefused = errors.New("the control plane refused this pairing")
	// ErrPairingUnknown is a pairing id the control plane does not hold.
	ErrPairingUnknown = errors.New("the control plane holds no such pairing")
)

// InvitationStart is what `invitations/start` answers.
type InvitationStart struct {
	InvitationID string `json:"invitation_id"`
	ExpiresAt    string `json:"expires_at"`
	ExpiresIn    int    `json:"expires_in"`
}

// InvitationPoll is what `invitations/poll` answers. `Status` is `pending` or
// `ready`.
type InvitationPoll struct {
	Status         string `json:"status"`
	AccountID      string `json:"account_id"`
	MachineID      string `json:"machine_id"`
	ViewerDeviceID string `json:"viewer_device_id"`
	EncryptedOffer string `json:"encrypted_offer"`
	OfferSHA256    string `json:"offer_sha256"`
	RecordedAtMS   int64  `json:"recorded_at_ms"`
}

// PairingDelivery is what `complete` answers: the fingerprint the control
// plane recorded at `start`, echoed back.
type PairingDelivery struct {
	Status      string `json:"status"`
	Fingerprint string `json:"fingerprint"`
}

// StartInvitation asks for an invitation id, telling the cloud only the hash.
//
// The secret itself stays in this process and in whatever a person carries. A
// control plane that learned it could decrypt the browser's offer, which is
// the one thing this whole route exists to prevent.
func (c *AccountClient) StartInvitation(ctx context.Context, machineCredential string, secretHash []byte) (InvitationStart, error) {
	if len(secretHash) != 32 {
		return InvitationStart{}, fmt.Errorf("%w: an invitation hash is 32 bytes", ErrInvalidToken)
	}
	var out InvitationStart
	err := c.postPairing(ctx, "/v1/pairing/invitations/start", machineCredential, map[string]any{
		"secret_hash": base64.StdEncoding.EncodeToString(secretHash),
	}, &out)
	if err != nil {
		return InvitationStart{}, err
	}
	if out.InvitationID == "" {
		return InvitationStart{}, fmt.Errorf("%w: the answer names no invitation", ErrInvalidToken)
	}
	return out, nil
}

// PollInvitation asks once whether a browser has answered.
//
// `202` is the ordinary waiting state and is reported as ErrPairingPending, not
// as a failure. A `404` or `409` means the invitation is gone, which is
// terminal: the person shows a fresh one rather than waiting longer.
func (c *AccountClient) PollInvitation(ctx context.Context, machineCredential, invitationID string) (InvitationPoll, error) {
	var out InvitationPoll
	err := c.postPairing(ctx, "/v1/pairing/invitations/poll", machineCredential, map[string]any{
		"invitation_id": invitationID,
	}, &out)
	if err != nil {
		return InvitationPoll{}, err
	}
	if out.Status != "ready" {
		return InvitationPoll{}, ErrPairingPending
	}
	if out.EncryptedOffer == "" || out.ViewerDeviceID == "" {
		return InvitationPoll{}, fmt.Errorf("%w: a ready invitation with no offer", ErrInvalidToken)
	}
	return out, nil
}

// CompletePairing leaves the sealed handover in the one slot the browser will
// claim, and returns the fingerprint the control plane recorded for the
// requester.
//
// That echo is not decoration. The control plane stored the browser's
// fingerprint when the browser called `start`; if it disagrees with the offer
// this machine just sealed for, the offer was not the one that opened this
// pairing — which is exactly the substitution a person comparing codes on two
// screens cannot see.
func (c *AccountClient) CompletePairing(ctx context.Context, machineCredential, pairingID string, wrapper []byte) (PairingDelivery, error) {
	var out PairingDelivery
	err := c.postPairing(ctx, "/v1/pairing/complete", machineCredential, map[string]any{
		"pairing_id": pairingID,
		"ciphertext": base64.StdEncoding.EncodeToString(wrapper),
	}, &out)
	if err != nil {
		return PairingDelivery{}, err
	}
	if out.Fingerprint == "" {
		return PairingDelivery{}, fmt.Errorf("%w: the delivery echoed no fingerprint", ErrInvalidToken)
	}
	return out, nil
}

// RotatedIdentity is what a key rotation answers: the epoch boundaries the
// relay will judge tokens by.
type RotatedIdentity struct {
	KeyEpoch       int    `json:"key_epoch"`
	IdentityEpoch  int    `json:"identity_epoch"`
	KeyFingerprint string `json:"key_fingerprint"`
}

// RotateMachineKey replaces this machine's signing key in the control plane.
//
// It is compare-and-swap on `expected_key_epoch`: a machine that rotated
// somewhere else is refused rather than silently overwritten, which is the
// difference between "two daemons raced" and "one daemon lost its identity".
func (c *AccountClient) RotateMachineKey(ctx context.Context, machineCredential, machineID string, publicKey []byte, fingerprint string, expectedKeyEpoch int) (RotatedIdentity, error) {
	if len(publicKey) != 32 {
		return RotatedIdentity{}, cloud.ErrPublicKeyLength
	}
	var out RotatedIdentity
	err := c.postPairing(ctx, "/v1/machines/"+machineID+"/identity/rotate", machineCredential, map[string]any{
		"expected_key_epoch": expectedKeyEpoch,
		"public_key":         base64.StdEncoding.EncodeToString(publicKey),
		"fingerprint":        fingerprint,
	}, &out)
	if err != nil {
		return RotatedIdentity{}, err
	}
	return out, nil
}

// MachineRecord is one machine as the control plane describes it. The Go
// daemon reads its own row to learn the key epoch a rotation must name.
type MachineRecord struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Platform       string  `json:"platform"`
	KeyFingerprint string  `json:"key_fingerprint"`
	KeyEpoch       int     `json:"key_epoch"`
	IdentityEpoch  int     `json:"identity_epoch"`
	RevokedAt      *string `json:"revoked_at"`
}

// Machines lists the account's machines.
func (c *AccountClient) Machines(ctx context.Context, machineCredential string) ([]MachineRecord, error) {
	var out struct {
		Machines []MachineRecord `json:"machines"`
	}
	if err := c.getJSON(ctx, "/v1/machines", machineCredential, &out); err != nil {
		return nil, err
	}
	return out.Machines, nil
}

// postPairing is `post` with the pairing routes' typed refusals. It is a
// separate path rather than a flag on `post` because those refusals are only
// meaningful here: a `409` from a token route is not an expired QR.
func (c *AccountClient) postPairing(ctx context.Context, path, bearer string, body any, out any) error {
	err := c.post(ctx, path, bearer, body, out)
	if err == nil {
		return nil
	}
	// `post` answers a status-bearing error for everything past 400 except
	// 401/403, which it maps to ErrUnauthorized. Pairing needs three more
	// distinctions, and they are read off the status text `post` carries.
	switch {
	case errors.Is(err, ErrUnauthorized):
		return fmt.Errorf("%w: %v", ErrPairingRefused, err)
	case containsStatus(err, http.StatusConflict):
		return fmt.Errorf("%w: %v", ErrInvitationGone, err)
	case containsStatus(err, http.StatusNotFound):
		return fmt.Errorf("%w: %v", ErrPairingUnknown, err)
	}
	return err
}

// containsStatus reports whether the error `post` built names this status. The
// status is in the message because `post` has one error shape for every route;
// rather than change that shape for every caller, the two callers that need
// the number look for it here, in one place, spelled once.
func containsStatus(err error, status int) bool {
	return err != nil && strings.Contains(err.Error(), fmt.Sprintf("answered %d", status))
}

// getJSON is a GET with the machine credential, for the machine list.
func (c *AccountClient) getJSON(ctx context.Context, path, bearer string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, OpeningTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w: %s answered %d", ErrUnauthorized, path, resp.StatusCode)
	case resp.StatusCode >= 400:
		return fmt.Errorf("%s answered %d: %s", path, resp.StatusCode, truncate(string(data), 200))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: %s answered unreadable JSON: %v", ErrInvalidToken, path, err)
	}
	return nil
}

// InvitationLifetime is how long this machine will keep polling one
// invitation before it says the window closed. The control plane's own TTL is
// authoritative; this is the ceiling for a machine whose clock disagrees.
const InvitationLifetime = 10 * time.Minute
