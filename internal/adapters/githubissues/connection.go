package githubissues

import (
	"context"
	"errors"
	"sync"
	"time"
)

// State is what the Console shows for this machine's GitHub connection.
type State string

const (
	StateDisconnected State = "disconnected"
	StatePending      State = "pending"
	StateConnected    State = "connected"
	// StateDenied, StateExpired and StateFailed are how the last device flow
	// ended without a token. They last until the next start or disconnect.
	StateDenied  State = "denied"
	StateExpired State = "expired"
	StateFailed  State = "failed"
	// StateReauthorize is a saved token GitHub no longer refreshes.
	StateReauthorize State = "reauthorize"
)

// Status never carries a token or the device code.
type Status struct {
	State           State      `json:"state"`
	Login           string     `json:"login,omitempty"`
	UserCode        string     `json:"user_code,omitempty"`
	VerificationURI string     `json:"verification_uri,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	Error           string     `json:"error,omitempty"`
}

// refreshMargin is how long before expiry a token is replaced, so a call
// that starts with it does not fail halfway.
const refreshMargin = 5 * time.Minute

// Connection is this machine's one GitHub authorization. One device flow runs
// at a time; asking again while it is waiting answers the same code.
type Connection struct {
	client *Client
	store  TokenStore
	life   context.Context
	stop   context.CancelFunc
	// Sleep waits between polls; a test replaces it.
	Sleep func(context.Context, time.Duration) error

	mu      sync.Mutex
	flow    *Device
	cancel  context.CancelFunc
	outcome State
	reason  string
	gen     int

	refreshMu sync.Mutex
}

func NewConnection(client *Client, dir string) *Connection {
	life, stop := context.WithCancel(context.Background())
	return &Connection{client: client, store: TokenStore{Dir: dir}, life: life, stop: stop, Sleep: sleepContext}
}

// Close stops a waiting device flow. The saved token is kept.
func (c *Connection) Close() { c.stop() }

// Start begins a device flow, or answers the one already waiting.
func (c *Connection) Start(ctx context.Context) (Status, error) {
	c.mu.Lock()
	if c.flow != nil && c.client.now().Before(c.flow.ExpiresAt) {
		defer c.mu.Unlock()
		return c.pendingStatus(), nil
	}
	c.mu.Unlock()

	device, err := c.client.StartDevice(ctx)
	if err != nil {
		return Status{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.flow != nil && c.client.now().Before(c.flow.ExpiresAt) {
		// Another start won the race; its code is the one the person sees.
		return c.pendingStatus(), nil
	}
	if c.cancel != nil {
		c.cancel()
	}
	// ExpiresAt is on the client's clock, which a test or a skewed caller may
	// not share with the runtime's: the context gets the time that remains.
	poll, cancel := context.WithTimeout(c.life, device.ExpiresAt.Sub(c.client.now()))
	c.gen++
	c.flow, c.cancel, c.outcome, c.reason = &device, cancel, "", ""
	go c.poll(poll, device, c.gen)
	return c.pendingStatus(), nil
}

func (c *Connection) pendingStatus() Status {
	at := c.flow.ExpiresAt
	return Status{State: StatePending, UserCode: c.flow.UserCode, VerificationURI: c.flow.VerificationURI, ExpiresAt: &at}
}

func (c *Connection) poll(ctx context.Context, device Device, gen int) {
	interval := device.Interval
	for {
		if err := c.Sleep(ctx, interval); err != nil {
			c.finish(gen, StateExpired, "expired_token")
			return
		}
		token, err := c.client.PollDevice(ctx, device.DeviceCode)
		switch {
		case err == nil:
			login, lerr := c.client.Login(ctx, token.AccessToken)
			if lerr != nil {
				c.finish(gen, StateFailed, "github_login_unreadable")
				return
			}
			token.Login = login
			if serr := c.saveIfCurrent(gen, token); serr != nil {
				c.finish(gen, StateFailed, "github_token_unwritable")
				return
			}
			c.finish(gen, "", "")
			return
		case errors.Is(err, ErrPending):
		case errors.Is(err, ErrSlowDown):
			interval += 5 * time.Second
		case errors.Is(err, ErrDenied):
			c.finish(gen, StateDenied, "access_denied")
			return
		case errors.Is(err, ErrExpired):
			c.finish(gen, StateExpired, "expired_token")
			return
		case errors.Is(err, ErrFlowDisabled):
			c.finish(gen, StateFailed, "device_flow_disabled")
			return
		default:
			// A network failure is retried until the code expires: the
			// person may still be typing it.
			if ctx.Err() != nil {
				c.finish(gen, StateExpired, "expired_token")
				return
			}
		}
	}
}

// saveIfCurrent writes the token only for the flow still in charge, so a
// disconnect during the last poll is not undone by it.
func (c *Connection) saveIfCurrent(gen int, t Token) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen {
		return nil
	}
	return c.store.Save(t)
}

func (c *Connection) finish(gen int, outcome State, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen {
		return
	}
	if c.cancel != nil {
		c.cancel()
	}
	c.flow, c.cancel, c.outcome, c.reason = nil, nil, outcome, reason
}

// Status reads the saved token first: a connected machine is connected even
// when a later attempt to connect again was declined.
func (c *Connection) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.flow != nil && c.client.now().Before(c.flow.ExpiresAt) {
		return c.pendingStatus()
	}
	t, err := c.store.Load()
	switch {
	case err == nil:
		if t.RefreshToken != "" && !t.RefreshExpiresAt.IsZero() && !c.client.now().Before(t.RefreshExpiresAt) {
			return Status{State: StateReauthorize, Login: t.Login, Error: ErrReauthorize.Error()}
		}
		return Status{State: StateConnected, Login: t.Login, Error: c.reason}
	case errors.Is(err, ErrNoToken):
		if c.outcome != "" {
			return Status{State: c.outcome, Error: c.reason}
		}
		return Status{State: StateDisconnected}
	default:
		return Status{State: StateFailed, Error: "github_token_unreadable"}
	}
}

// Authorized is IssueSyncGate's question: is there a saved token to use.
func (c *Connection) Authorized() bool {
	s := c.Status()
	return s.State == StateConnected
}

// Disconnect stops a waiting flow and forgets the token. GitHub keeps the
// authorization until the person revokes it in their GitHub settings: an
// OAuth App can revoke a token only with its client secret, which this
// binary does not carry.
func (c *Connection) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	c.gen++
	c.flow, c.cancel, c.outcome, c.reason = nil, nil, "", ""
	return c.store.Delete()
}

// AccessToken answers a usable access token, refreshing it first when it is
// about to expire. GitHub retires the old pair the moment it answers a
// refresh, so the new pair is saved before it is handed out, and refreshes
// are serialized so two callers never spend the same refresh token.
func (c *Connection) AccessToken(ctx context.Context) (string, error) {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	t, err := c.store.Load()
	if err != nil {
		return "", err
	}
	now := c.client.now()
	if t.ExpiresAt.IsZero() || now.Add(refreshMargin).Before(t.ExpiresAt) {
		return t.AccessToken, nil
	}
	if t.RefreshToken == "" || (!t.RefreshExpiresAt.IsZero() && !now.Before(t.RefreshExpiresAt)) {
		return "", ErrReauthorize
	}
	fresh, err := c.client.Refresh(ctx, t.RefreshToken)
	if err != nil {
		return "", err
	}
	fresh.Login = t.Login
	if err := c.store.Save(fresh); err != nil {
		return "", err
	}
	return fresh.AccessToken, nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
