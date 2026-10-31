// Package githubissues connects this machine to GitHub for issue sync
// (docs/issue-sync.md, D73): the OAuth device flow that authorizes it, the
// owner-only file that keeps the token, and the Project's repository target.
//
// The OAuth App is Clawdline's own. Its client id is public by design; the
// device flow needs no client secret, neither to authorize nor to refresh, so
// no secret ships in this binary.
package githubissues

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ClientID is the Clawdline OAuth App registered on 2026-10-31 with device
// flow and expiring user tokens enabled.
const ClientID = "Ov23lioodvbqh76e5rtY"

// Scope is what opening and closing an issue needs: OAuth Apps have no
// narrower scope that reaches a private repository's issues.
const Scope = "repo"

// MaxResponseBytes bounds every answer read from GitHub's OAuth and REST
// endpoints. Each answer is a few hundred bytes; anything larger is refused.
const MaxResponseBytes = 64 << 10

// Typed outcomes of one device-flow poll and of a refresh. They are the
// error codes GitHub documents for the device flow.
var (
	ErrPending      = errors.New("authorization_pending")
	ErrSlowDown     = errors.New("slow_down")
	ErrExpired      = errors.New("expired_token")
	ErrDenied       = errors.New("access_denied")
	ErrFlowDisabled = errors.New("device_flow_disabled")
	// ErrReauthorize is a refresh token GitHub no longer accepts: the person
	// has to connect again.
	ErrReauthorize = errors.New("github_reauthorization_required")
)

// Client talks to GitHub. The two bases are fields so a test can point them
// at a local server.
type Client struct {
	HTTP     *http.Client
	OAuthURL string // https://github.com
	APIURL   string // https://api.github.com
	ClientID string
	Now      func() time.Time
}

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 20 * time.Second}, OAuthURL: "https://github.com",
		APIURL: "https://api.github.com", ClientID: ClientID, Now: time.Now}
}

// Device is one started device flow. DeviceCode is a secret: it never leaves
// the daemon. UserCode and VerificationURI are what the person sees.
type Device struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	ExpiresAt       time.Time
	Interval        time.Duration
}

// Token is a user access token and, when the App issues expiring tokens, the
// refresh token that replaces it. A zero ExpiresAt is a token that does not
// expire.
type Token struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token,omitempty"`
	ExpiresAt        time.Time `json:"expires_at,omitempty"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at,omitempty"`
	Scope            string    `json:"scope"`
	Login            string    `json:"login"`
}

func (c *Client) now() time.Time {
	if c.Now == nil {
		return time.Now()
	}
	return c.Now()
}

// StartDevice asks GitHub for a device and user code.
func (c *Client) StartDevice(ctx context.Context) (Device, error) {
	var answer struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
		Error           string `json:"error"`
	}
	form := url.Values{"client_id": {c.ClientID}, "scope": {Scope}}
	if err := c.postForm(ctx, "/login/device/code", form, &answer); err != nil {
		return Device{}, err
	}
	if answer.Error != "" {
		return Device{}, oauthError(answer.Error)
	}
	if answer.DeviceCode == "" || answer.UserCode == "" || !strings.HasPrefix(answer.VerificationURI, "https://") {
		return Device{}, errors.New("GitHub answered the device request without a usable code")
	}
	interval := time.Duration(answer.Interval) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	life := time.Duration(answer.ExpiresIn) * time.Second
	if life <= 0 || life > MaxDeviceFlowSeconds*time.Second {
		life = MaxDeviceFlowSeconds * time.Second
	}
	return Device{DeviceCode: answer.DeviceCode, UserCode: answer.UserCode, VerificationURI: answer.VerificationURI,
		ExpiresAt: c.now().Add(life), Interval: interval}, nil
}

// MaxDeviceFlowSeconds caps how long one device flow is polled, whatever
// GitHub's expires_in says. GitHub's default is the same fifteen minutes.
const MaxDeviceFlowSeconds = 900

// PollDevice asks once whether the person has entered the code. It answers
// ErrPending, ErrSlowDown, ErrExpired, ErrDenied, ErrFlowDisabled, or a token.
func (c *Client) PollDevice(ctx context.Context, deviceCode string) (Token, error) {
	return c.token(ctx, url.Values{"client_id": {c.ClientID}, "device_code": {deviceCode},
		"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}})
}

// Refresh exchanges a refresh token. GitHub invalidates the old access and
// refresh tokens as soon as it answers, so the caller saves the new pair
// before it uses the new access token.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	t, err := c.token(ctx, url.Values{"client_id": {c.ClientID}, "refresh_token": {refreshToken},
		"grant_type": {"refresh_token"}})
	if err != nil && !errors.Is(err, ErrSlowDown) && isOAuthError(err) {
		return Token{}, ErrReauthorize
	}
	return t, err
}

func (c *Client) token(ctx context.Context, form url.Values) (Token, error) {
	var answer struct {
		AccessToken           string `json:"access_token"`
		RefreshToken          string `json:"refresh_token"`
		ExpiresIn             int    `json:"expires_in"`
		RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
		Scope                 string `json:"scope"`
		Error                 string `json:"error"`
	}
	if err := c.postForm(ctx, "/login/oauth/access_token", form, &answer); err != nil {
		return Token{}, err
	}
	if answer.Error != "" {
		return Token{}, oauthError(answer.Error)
	}
	if answer.AccessToken == "" {
		return Token{}, errors.New("GitHub answered without an access token")
	}
	now := c.now()
	t := Token{AccessToken: answer.AccessToken, RefreshToken: answer.RefreshToken, Scope: answer.Scope}
	if answer.ExpiresIn > 0 {
		t.ExpiresAt = now.Add(time.Duration(answer.ExpiresIn) * time.Second)
	}
	if answer.RefreshTokenExpiresIn > 0 {
		t.RefreshExpiresAt = now.Add(time.Duration(answer.RefreshTokenExpiresIn) * time.Second)
	}
	return t, nil
}

// Login names the account a token belongs to.
func (c *Client) Login(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.APIURL+"/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	var answer struct {
		Login string `json:"login"`
	}
	if err := c.do(req, &answer); err != nil {
		return "", err
	}
	if answer.Login == "" {
		return "", errors.New("GitHub answered without an account name")
	}
	return answer.Login, nil
}

func (c *Client) postForm(ctx context.Context, path string, form url.Values, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.OAuthURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return c.do(req, into)
}

// do reads one bounded JSON answer. The OAuth endpoints answer their errors
// with 200 and an error field, which the caller reads; any other status is a
// failure whose body is not repeated, since it could echo a secret.
func (c *Client) do(req *http.Request, into any) error {
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub could not be reached: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("GitHub's answer could not be read: %w", err)
	}
	if len(body) > MaxResponseBytes {
		return errors.New("GitHub's answer is larger than expected")
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrReauthorize
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("GitHub answered %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return errors.New("GitHub's answer is not JSON")
	}
	return nil
}

type oauthErr struct{ code string }

func (e oauthErr) Error() string { return e.code }

func oauthError(code string) error {
	for _, known := range []error{ErrPending, ErrSlowDown, ErrExpired, ErrDenied, ErrFlowDisabled} {
		if known.Error() == code {
			return known
		}
	}
	return oauthErr{code: code}
}

func isOAuthError(err error) bool {
	var o oauthErr
	return errors.As(err, &o) || errors.Is(err, ErrExpired) || errors.Is(err, ErrDenied)
}
