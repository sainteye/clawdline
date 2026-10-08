package cloud

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// StartViewerLogin asks the Cloud account service to register a distinct viewer
// device. Existing servers answer bad_machine; that refusal must never trigger
// a fallback to machine login.
func (c *AccountClient) StartViewerLogin(ctx context.Context, name string, publicKey []byte) (LoginStart, error) {
	if name == "" || len(publicKey) != 32 {
		return LoginStart{}, errors.New("a viewer name and Ed25519 public key are required")
	}
	var out LoginStart
	err := c.post(ctx, "/v1/auth/device/start", "", map[string]any{
		"kind": "viewer", "name": name, "public_key": base64.StdEncoding.EncodeToString(publicKey),
	}, &out)
	if err != nil {
		return LoginStart{}, err
	}
	if out.DeviceCode == "" || out.UserCode == "" || out.VerificationURIComplete == "" || out.ExpiresIn <= 0 {
		return LoginStart{}, fmt.Errorf("%w: the viewer authorization response is incomplete", ErrIncompatible)
	}
	return out, nil
}

type ViewerLoginPoll struct {
	Status            string `json:"status"`
	RetryAfterSeconds int    `json:"retry_after_seconds"`
	AccountID         string `json:"account_id"`
	DeviceID          string `json:"viewer_device_id"`
	ViewerCredential  string `json:"viewer_credential"`
}

func (c *AccountClient) PollViewerLogin(ctx context.Context, deviceCode string) (ViewerLoginPoll, error) {
	var out ViewerLoginPoll
	if err := c.post(ctx, "/v1/auth/device/poll", "", map[string]any{"device_code": deviceCode}, &out); err != nil {
		return ViewerLoginPoll{}, err
	}
	return out, nil
}

// WaitForViewerApproval follows the server's interval and slow_down instruction.
// It accepts only a viewer-specific completion, never a machine credential.
func (c *AccountClient) WaitForViewerApproval(ctx context.Context, start LoginStart, wait time.Duration, sleep func(context.Context, time.Duration) error) (ViewerLoginPoll, error) {
	if sleep == nil {
		sleep = sleepContext
	}
	interval := time.Duration(start.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if err := sleep(ctx, interval); err != nil {
			return ViewerLoginPoll{}, err
		}
		poll, err := c.PollViewerLogin(ctx, start.DeviceCode)
		if err != nil {
			return ViewerLoginPoll{}, err
		}
		switch poll.Status {
		case LoginPending:
		case LoginSlowDown:
			if poll.RetryAfterSeconds > 0 {
				interval = time.Duration(poll.RetryAfterSeconds) * time.Second
			} else {
				interval += time.Second
			}
		case LoginDenied:
			return ViewerLoginPoll{}, ErrLoginDenied
		case LoginExpired:
			return ViewerLoginPoll{}, ErrLoginExpired
		case LoginComplete:
			if poll.AccountID == "" || poll.DeviceID == "" || poll.ViewerCredential == "" {
				return ViewerLoginPoll{}, fmt.Errorf("%w: approval did not return a viewer device identity", ErrIncompatible)
			}
			return poll, nil
		default:
			return ViewerLoginPoll{}, fmt.Errorf("%w: unrecognized viewer authorization state %q", ErrIncompatible, poll.Status)
		}
	}
	return ViewerLoginPoll{}, ErrLoginTimeout
}

// ViewerTokenMatches checks the trusted control plane's token response before
// the CLI reports success. The relay still verifies the JWT signature itself.
func ViewerTokenMatches(token, accountID, deviceID string, publicKey []byte) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Role      string `json:"role"`
		Account   string `json:"acct"`
		Device    string `json:"dev"`
		Machine   string `json:"mid"`
		PublicKey string `json:"pk"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return false
	}
	return claims.Role == "viewer" && claims.Account == accountID && claims.Device == deviceID &&
		claims.Machine == "" && claims.PublicKey == base64.StdEncoding.EncodeToString(publicKey)
}
