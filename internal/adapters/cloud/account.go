package cloud

// The control plane: how this machine gets an identity and, from then on, a
// device token every few minutes.
//
// Two endpoints and one long-lived secret:
//
//   - `POST /v1/auth/device/start` and `/poll` are RFC 8628 device
//     authorization. The machine declares itself and its public key, a person
//     approves the short code in a browser, and the poll then answers an
//     account id, a machine id and a **machine credential**.
//   - `POST /v1/tokens/device` trades that credential for the short-lived
//     EdDSA JWT the relay verifies. It lives 300 seconds; the transport
//     fetches a new one a minute before that.
//
// **This machine registers as a new machine.** It does not read, copy or reuse
// the Swift app's identity or keys, and it must not: that app is running on
// this same Mac right now, and two producers sharing one `sender` would share
// one sequence space, so each would make the other's envelopes look like
// replays to every viewer. The key material comes from
// `internal/adapters/cloudkeys`, under this app's own directory.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/cloud"
)

// AccountClient talks to the control plane.
type AccountClient struct {
	// BaseURL is the api, e.g. https://api.clawdline.com.
	BaseURL string
	HTTP    *http.Client
}

// NewAccountClient returns a client with the timeouts the transport expects.
func NewAccountClient(baseURL string) *AccountClient {
	return &AccountClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		// The Swift app uses 15 seconds for both the request and the resource
		// (`CloudTransport.swift:143-144`); the whole point of the opening
		// deadline is that a token fetch cannot outlive the connect attempt
		// it is part of.
		HTTP: &http.Client{Timeout: OpeningTimeout},
	}
}

// maxResponseBytes bounds what a control-plane answer may be. The Swift app
// caps it at 64 KiB (`CloudTransport.swift:146`); an answer larger than that is
// not a token, it is something else entirely.
const maxResponseBytes = 64 << 10

// LoginStart is what a device-authorization attempt gives the person.
type LoginStart struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// StartLogin declares this machine and asks for a code for the person to
// approve. publicKey is this machine's Ed25519 public key.
func (c *AccountClient) StartLogin(ctx context.Context, name, platform string, publicKey []byte, appVersion string) (LoginStart, error) {
	if len(publicKey) != 32 {
		return LoginStart{}, cloud.ErrPublicKeyLength
	}
	body := map[string]any{
		"name":     name,
		"platform": platform,
		// Standard padded base64 of exactly 32 bytes, which is what the api
		// checks for.
		"public_key": base64.StdEncoding.EncodeToString(publicKey),
	}
	if appVersion != "" {
		body["app_version"] = appVersion
	}
	var out LoginStart
	if err := c.post(ctx, "/v1/auth/device/start", "", body, &out); err != nil {
		return LoginStart{}, err
	}
	if out.DeviceCode == "" || out.UserCode == "" {
		return LoginStart{}, fmt.Errorf("%w: the start response names no code", ErrInvalidToken)
	}
	return out, nil
}

// LoginPoll is one answer from the poll endpoint.
type LoginPoll struct {
	Status            string `json:"status"`
	RetryAfterSeconds int    `json:"retry_after_seconds"`
	AccountID         string `json:"account_id"`
	MachineID         string `json:"machine_id"`
	MachineCredential string `json:"machine_credential"`
}

// Device-authorization statuses.
const (
	LoginPending  = "authorization_pending"
	LoginSlowDown = "slow_down"
	LoginDenied   = "access_denied"
	LoginExpired  = "expired_token"
	LoginComplete = "complete"
)

// PollLogin asks once whether the person has approved yet.
func (c *AccountClient) PollLogin(ctx context.Context, deviceCode string) (LoginPoll, error) {
	var out LoginPoll
	if err := c.post(ctx, "/v1/auth/device/poll", "", map[string]any{"device_code": deviceCode}, &out); err != nil {
		return LoginPoll{}, err
	}
	return out, nil
}

// DeviceToken is one short-lived credential for the relay.
type DeviceToken struct {
	Token     string    `json:"token"`
	TokenType string    `json:"token_type"`
	ExpiresIn int       `json:"expires_in"`
	ExpiresAt time.Time `json:"expires_at"`
	Kid       string    `json:"kid"`
	// RelayURL is what the control plane says the relay is. It is derived
	// from the token audience rather than from any relay setting, so a local
	// api answers the production hostname; a caller that already knows where
	// its relay is should keep its own answer.
	RelayURL string `json:"relay_url"`
}

// MintDeviceToken trades the machine credential for a device token.
func (c *AccountClient) MintDeviceToken(ctx context.Context, machineCredential string) (DeviceToken, error) {
	var out DeviceToken
	if err := c.post(ctx, "/v1/tokens/device", machineCredential, nil, &out); err != nil {
		return DeviceToken{}, err
	}
	if out.Token == "" {
		return DeviceToken{}, fmt.Errorf("%w: no token in the answer", ErrInvalidToken)
	}
	if out.ExpiresAt.IsZero() {
		if out.ExpiresIn <= 0 {
			return DeviceToken{}, fmt.Errorf("%w: the token names no expiry", ErrInvalidToken)
		}
		out.ExpiresAt = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	}
	if !out.ExpiresAt.After(time.Now()) {
		// A token that is already expired is not a token. Accepting it would
		// spend one connect attempt and one backoff rung to learn nothing.
		return DeviceToken{}, fmt.Errorf("%w: the token has already expired", ErrInvalidToken)
	}
	return out, nil
}

func (c *AccountClient) post(ctx context.Context, path, bearer string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
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
		// Terminal: the credential is wrong or withdrawn. Retrying it is how
		// a machine gets itself rate-limited while the user waits.
		return fmt.Errorf("%w: %s answered %d", ErrUnauthorized, path, resp.StatusCode)
	case resp.StatusCode >= 400:
		return fmt.Errorf("%s answered %d: %s", path, resp.StatusCode, truncate(string(data), 200))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: %s answered unreadable JSON: %v", ErrInvalidToken, path, err)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// TokenSource hands the transport a usable device token, minting a new one
// when the current one is inside RefreshAhead of its expiry.
//
// It is one small type rather than a closure because the caching rule is the
// thing worth being able to read: a token is reused only while it has more
// than RefreshAhead left, so a connection never opens with a credential that
// will die during the handshake.
type TokenSource struct {
	Client     *AccountClient
	Credential string

	token DeviceToken
}

// Token answers a token good for at least RefreshAhead.
func (s *TokenSource) Token(ctx context.Context) (DeviceToken, error) {
	if s.Client == nil || s.Credential == "" {
		return DeviceToken{}, ErrNoIdentity
	}
	if s.token.Token != "" && time.Until(s.token.ExpiresAt) > RefreshAhead {
		return s.token, nil
	}
	token, err := s.Client.MintDeviceToken(ctx, s.Credential)
	if err != nil {
		return DeviceToken{}, err
	}
	s.token = token
	return token, nil
}

// Forget drops the cached token, which is what a refusal on an authenticated
// socket asks for.
func (s *TokenSource) Forget() { s.token = DeviceToken{} }
