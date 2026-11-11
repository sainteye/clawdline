package cloud

// The machine's half of the browser-invitation routes (docs/browser-invite.md).
// Every call carries this machine's credential; the browser's half of the
// protocol talks to Cloud with its own short-lived join token instead.

import (
	"context"
	"errors"
	"fmt"
)

// Browser invitation statuses Cloud reports to the inviting machine.
const (
	InviteStatusWaiting   = "waiting"   // nobody has typed the code
	InviteStatusJoined    = "joined"    // a browser sent its CPace message
	InviteStatusConfirmed = "confirmed" // that browser sealed its confirmation
	InviteStatusAdmitted  = "admitted"  // this machine admitted it
	InviteStatusClaimed   = "claimed"   // the browser took its handover
	InviteStatusDenied    = "denied"
	InviteStatusExpired   = "expired"
)

// ErrInviteUnusable is an invitation Cloud no longer serves: expired, spent,
// denied or unknown. It is terminal; the person makes a new one.
var ErrInviteUnusable = errors.New("that browser invitation can no longer be used")

// BrowserInviteStart is Cloud's answer to a new invitation.
type BrowserInviteStart struct {
	InviteID string `json:"invite_id"`
	// ExpiresAt is when the code stops being accepted, Unix milliseconds.
	ExpiresAt int64 `json:"expires_at"`
}

// BrowserInvitePoll is what Cloud knows about one invitation.
type BrowserInvitePoll struct {
	InviteID        string `json:"invite_id"`
	MachineID       string `json:"machine_id"`
	Status          string `json:"status"`
	BrowserMessage  string `json:"browser_message,omitempty"`
	DeviceID        string `json:"device_id,omitempty"`
	DevicePublicKey string `json:"device_public_key,omitempty"`
	BrowserConfirm  string `json:"browser_confirm,omitempty"`
}

// BrowserInviteAdmit is Cloud's record of the admitted browser.
type BrowserInviteAdmit struct {
	DeviceID string `json:"device_id"`
}

// StartBrowserInvite asks Cloud for an invitation id. The expiry is the
// admitted browser's, recorded so Cloud ends its session at the same time.
func (c *AccountClient) StartBrowserInvite(ctx context.Context, credential, expiry string) (BrowserInviteStart, error) {
	var out BrowserInviteStart
	if err := c.inviteCall(ctx, "/v1/pairing/browser-invites", credential, map[string]any{"expiry": expiry}, &out); err != nil {
		return BrowserInviteStart{}, err
	}
	if out.InviteID == "" || out.ExpiresAt <= 0 {
		return BrowserInviteStart{}, fmt.Errorf("%w: an invitation with no id or expiry", ErrInvalidToken)
	}
	return out, nil
}

// PollBrowserInvite reads one invitation's state.
func (c *AccountClient) PollBrowserInvite(ctx context.Context, credential, inviteID string) (BrowserInvitePoll, error) {
	var out BrowserInvitePoll
	if err := c.inviteCall(ctx, "/v1/pairing/browser-invites/poll", credential, map[string]any{"invite_id": inviteID}, &out); err != nil {
		return BrowserInvitePoll{}, err
	}
	if out.InviteID != inviteID {
		return BrowserInvitePoll{}, fmt.Errorf("%w: Cloud answered for invitation %q", ErrInvalidToken, out.InviteID)
	}
	return out, nil
}

// AnswerBrowserInvite leaves this machine's CPace message for the browser.
func (c *AccountClient) AnswerBrowserInvite(ctx context.Context, credential, inviteID, machineMessage string) error {
	return c.inviteCall(ctx, "/v1/pairing/browser-invites/answer", credential,
		map[string]any{"invite_id": inviteID, "machine_message": machineMessage}, &struct{}{})
}

// AdmitBrowserInvite leaves the sealed handover and asks Cloud to create the
// invited browser's identity, bound to this machine, ending at expiresAt
// (Unix seconds; 0 is never).
func (c *AccountClient) AdmitBrowserInvite(ctx context.Context, credential, inviteID, deviceID, handover string, expiresAt int64) (BrowserInviteAdmit, error) {
	var out BrowserInviteAdmit
	err := c.inviteCall(ctx, "/v1/pairing/browser-invites/admit", credential, map[string]any{
		"invite_id": inviteID, "device_id": deviceID, "handover": handover, "expires_at": expiresAt,
	}, &out)
	if err != nil {
		return BrowserInviteAdmit{}, err
	}
	if out.DeviceID != deviceID {
		return BrowserInviteAdmit{}, fmt.Errorf("%w: Cloud admitted %q, not %q", ErrInvalidToken, out.DeviceID, deviceID)
	}
	return out, nil
}

// DenyBrowserInvite spends an invitation without admitting anybody.
func (c *AccountClient) DenyBrowserInvite(ctx context.Context, credential, inviteID string) error {
	return c.inviteCall(ctx, "/v1/pairing/browser-invites/deny", credential, map[string]any{"invite_id": inviteID}, &struct{}{})
}

// RevokeInvitedBrowser ends an invited browser's Cloud identity. Only an
// invited browser bound to this machine can be named.
func (c *AccountClient) RevokeInvitedBrowser(ctx context.Context, credential, deviceID string) error {
	return c.inviteCall(ctx, "/v1/pairing/browser-invites/revoke-device", credential, map[string]any{"device_id": deviceID}, &struct{}{})
}

func (c *AccountClient) inviteCall(ctx context.Context, path, credential string, body, out any) error {
	err := c.postPairing(ctx, path, credential, body, out)
	if errors.Is(err, ErrInvitationGone) || errors.Is(err, ErrPairingUnknown) {
		return fmt.Errorf("%w: %v", ErrInviteUnusable, err)
	}
	return err
}
