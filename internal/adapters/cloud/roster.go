package cloud

// Who this machine will listen to.
//
// The transport refuses an inbound envelope whose sender it holds no pinned
// public key for (`transport.go`'s `DropUnknownSender`), and until this file
// existed there was no way to hold one: `cmd/clawdline cloud connect` passed a
// `PublicKeyFor` that answered `false` for everybody, so a real daemon counted
// every inbound envelope as `unknown_sender` and answered nothing.
//
// The roster is the account's own list of viewer devices, `GET /v1/devices`,
// read with this machine's credential. That route is the control plane's
// answer to "which browsers and phones has this person enrolled", and each row
// carries the device's Ed25519 `public_key` — the same key the viewer signs
// its envelopes with. Approval happens in the account, on a device the person
// already trusts; this machine does not get a second vote and does not keep a
// list of its own.
//
// Three properties that are not decoration:
//
//   - **A revoked device is not on it.** A row with `revoked_at` set is
//     dropped, so a device the person removed stops verifying here at the next
//     refresh rather than at the next restart.
//   - **An unreadable roster is not an empty one.** A failed fetch keeps the
//     last good answer and is reported as `roster_unreadable`, because an empty
//     roster reads exactly like "nobody is paired" and the two call for
//     opposite responses (`status.go`'s DropRosterUnreadable).
//   - **It is re-read on a clock, not per envelope.** A viewer that pairs while
//     the line is up is admitted within RosterRefresh rather than at the next
//     reconnect, and a burst of 800 envelopes is still one fetch.

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RosterRefresh is how stale a roster may be before the next lookup refetches
// it. The Swift app re-reads on every pairing change it hears about and every
// five minutes otherwise; nothing here hears about pairing changes yet, so the
// clock is the only refresh and it is the shorter one.
const RosterRefresh = 60 * time.Second

// RosterDevice is one enrolled viewer as the control plane describes it.
type RosterDevice struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Caps        []string `json:"caps"`
	PublicKey   string   `json:"public_key"`
	Fingerprint string   `json:"key_fingerprint"`
	RevokedAt   *string  `json:"revoked_at"`
}

// Roster is this machine's cached view of the account's viewer devices.
type Roster struct {
	apiBase    string
	credential string
	client     *http.Client
	now        func() time.Time

	mu        sync.Mutex
	keys      map[string]ed25519.PublicKey
	devices   []RosterDevice
	fetchedAt time.Time
	lastErr   error
}

// NewRoster builds a roster that reads `apiBase` with `credential`.
func NewRoster(apiBase, credential string, now func() time.Time) *Roster {
	if now == nil {
		now = time.Now
	}
	return &Roster{
		apiBase:    strings.TrimRight(apiBase, "/"),
		credential: credential,
		client:     &http.Client{Timeout: 10 * time.Second},
		now:        now,
		keys:       map[string]ed25519.PublicKey{},
	}
}

// ErrRosterUnreadable is what a caller gets when the list could not be read and
// nothing was ever read before. It is deliberately distinct from an empty
// roster.
var ErrRosterUnreadable = errors.New("the account's device roster could not be read")

// PublicKeyFor answers the pinned key for a sender, refreshing when stale.
//
// It never blocks on the network while holding a verdict: a refresh that fails
// leaves the previous answer standing, because a viewer that was admitted a
// second ago should not stop being admitted because the control plane hiccuped.
func (r *Roster) PublicKeyFor(sender string) (ed25519.PublicKey, bool) {
	r.mu.Lock()
	stale := r.fetchedAt.IsZero() || r.now().Sub(r.fetchedAt) > RosterRefresh
	if !stale {
		key, ok := r.keys[sender]
		r.mu.Unlock()
		return key, ok
	}
	r.mu.Unlock()

	_ = r.Refresh(context.Background())

	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.keys[sender]
	return key, ok
}

// Refresh fetches the roster now.
func (r *Roster) Refresh(ctx context.Context) error {
	devices, err := r.fetch(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		r.lastErr = err
		// The clock still moves so that a control plane that is down does not
		// turn every inbound envelope into an outbound request.
		r.fetchedAt = r.now()
		return err
	}
	keys := make(map[string]ed25519.PublicKey, len(devices))
	for _, device := range devices {
		if device.RevokedAt != nil && *device.RevokedAt != "" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(device.PublicKey)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			// A row this build cannot read is left out rather than guessed at.
			// It is not an error for the whole roster: one malformed device
			// must not lock out the other three.
			continue
		}
		keys[device.ID] = ed25519.PublicKey(raw)
	}
	r.keys, r.devices, r.fetchedAt, r.lastErr = keys, devices, r.now(), nil
	return nil
}

// Devices is what the last good fetch said, for the status route.
func (r *Roster) Devices() []RosterDevice {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]RosterDevice(nil), r.devices...)
}

// Readable reports whether the last fetch succeeded, and why not when it did
// not. It is what cloudops.Authority's RosterReadable is wired to.
func (r *Roster) Readable() (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fetchedAt.IsZero() {
		return false, ErrRosterUnreadable
	}
	return r.lastErr == nil, r.lastErr
}

func (r *Roster) fetch(ctx context.Context) ([]RosterDevice, error) {
	if r.apiBase == "" || r.credential == "" {
		return nil, ErrRosterUnreadable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.apiBase+"/v1/devices", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.credential)
	req.Header.Set("Accept", "application/json")
	res, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: the control plane answered %d", ErrRosterUnreadable, res.StatusCode)
	}
	var answer struct {
		Devices []RosterDevice `json:"devices"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRosterUnreadable, err)
	}
	return answer.Devices, nil
}
