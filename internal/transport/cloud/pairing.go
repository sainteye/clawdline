package cloud

// One pairing, from the link this machine shows to the browser that can read it.
//
// `internal/domain/cloud` owns the bytes and `internal/adapters/cloud` owns the
// HTTP; this file owns the *order*, which is the part that goes wrong:
//
//	1. draw a 32-byte secret, tell the control plane only its SHA-256, and
//	   show the person a link carrying the secret in its fragment
//	2. poll until a signed-in browser has left its offer there, encrypted
//	   under that secret
//	3. open the offer, seal this account's master secret and this machine's
//	   signing key for exactly that offer, and deliver the one ciphertext
//	4. **check the echoed fingerprint**, then pin the browser's key locally
//
// Two of those steps are load-bearing in a way that is easy to lose:
//
//   - **The fingerprint echo is a check, not a display.** The control plane
//     recorded the browser's fingerprint when the browser asked for a pairing
//     id. If it disagrees with the offer this machine just answered, the offer
//     was not the one that opened this pairing — the substitution a person
//     comparing two screens cannot see.
//   - **The pin is written last.** Pinning is what lets a browser drive this
//     machine. A browser that never received the key cannot produce a command
//     anyway, so pinning earlier would only leave a pinned viewer behind every
//     failed delivery.
//
// The secret never reaches a log, a status route or the control plane. What
// leaves this file is an invitation id, a fragment a person carries, and
// fingerprints.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	adaptercloud "github.com/sainteye/clawdline-go/internal/adapters/cloud"
	domaincloud "github.com/sainteye/clawdline-go/internal/domain/cloud"
)

// Pairing phases, as the status route and the settings page name them.
const (
	// PairingIdle is no pairing in progress.
	PairingIdle = "idle"
	// PairingWaiting is a live invitation nobody has answered yet.
	PairingWaiting = "waiting"
	// PairingSealing is an offer received and the handover being delivered.
	PairingSealing = "sealing"
	// PairingPaired is a completed handover with the viewer pinned.
	PairingPaired = "paired"
	// PairingFailed is a pairing that ended without one.
	PairingFailed = "failed"
)

// PairingPollInterval is how often a live invitation is asked about. It is the
// browser's own polling interval (`showCloudPairing`'s 2000 ms default), so
// neither side waits materially longer than the other.
const PairingPollInterval = 2 * time.Second

// PairingState is what a person can be shown about the pairing in progress.
//
// **There is no secret in it.** The fragment carries one, which is why it is
// only ever handed to the local settings page behind this machine's own token
// and never written to a log — but the state a route answers is otherwise
// ids, fingerprints and times.
type PairingState struct {
	// Phase is idle | waiting | sealing | paired | failed.
	Phase string `json:"phase"`
	// InvitationID is the control plane's handle for this invitation.
	InvitationID string `json:"invitation_id,omitempty"`
	// Link is the URL to open in the browser being paired. Its fragment
	// carries the one-time secret.
	Link string `json:"link,omitempty"`
	// ExpiresAt is when the invitation stops being answerable, Unix seconds.
	ExpiresAt int64 `json:"expires_at,omitempty"`
	// MachineFingerprint is what the browser will show once it has opened the
	// handover, so the person can compare two screens.
	MachineFingerprint string `json:"machine_fingerprint,omitempty"`
	// ViewerDeviceID and ViewerFingerprint name the browser that answered.
	ViewerDeviceID    string `json:"viewer_device_id,omitempty"`
	ViewerFingerprint string `json:"viewer_fingerprint,omitempty"`
	// PairedAt is when the handover was delivered and pinned, Unix seconds.
	PairedAt int64 `json:"paired_at,omitempty"`
	// Error is why a pairing ended without one.
	Error   string `json:"error,omitempty"`
	ErrorAt int64  `json:"error_at,omitempty"`
}

// PairingKeys is what a pairing needs from the key store, read at the moment
// it is needed rather than captured: a rotation between two pairings must not
// hand the second browser the key the first one got.
type PairingKeys func() (domaincloud.DeviceKey, domaincloud.ContentKey, error)

// Pairing drives one pairing at a time.
//
// One at a time on purpose. Two live invitations mean two secrets on two
// screens and one person, and the second thing a person does with two codes is
// use the wrong one. `Begin` replaces whatever was in flight, which is the
// honest reading of "show me a new code".
type Pairing struct {
	AccountID  string
	MachineID  string
	Credential string
	AppOrigin  string
	Client     *adaptercloud.AccountClient
	Keys       PairingKeys
	Pinned     *adaptercloud.PinnedStore
	Now        func() time.Time
	Log        func(format string, args ...any)

	mu         sync.Mutex
	state      PairingState
	invitation domaincloud.PairingInvitation
	cancel     context.CancelFunc
}

// ErrPairingUnavailable is what a link with no identity answers. It is not a
// pairing failure: there is nothing to pair a browser *to* yet.
var ErrPairingUnavailable = errors.New("this machine is not connected to a Clawdline Cloud account")

// State is the current pairing, for the status route.
func (p *Pairing) State() PairingState {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.state
	if state.Phase == PairingWaiting && state.ExpiresAt > 0 && p.now().Unix() > state.ExpiresAt {
		// An invitation whose window closed reads as failed even if the poll
		// loop has not noticed yet: a page showing "waiting" for a code that
		// can no longer be used is worse than no page.
		state.Phase = PairingFailed
		state.Error = adaptercloud.ErrInvitationGone.Error()
	}
	return state
}

// Begin draws a fresh invitation and starts waiting for a browser.
//
// The returned state carries the link. It is handed to the local settings page
// and to the terminal, and to nothing else.
func (p *Pairing) Begin(ctx context.Context) (PairingState, error) {
	if p == nil || p.Client == nil || p.Credential == "" || p.MachineID == "" {
		return PairingState{}, ErrPairingUnavailable
	}
	key, _, err := p.Keys()
	if err != nil {
		return PairingState{}, err
	}
	secret, err := domaincloud.NewPairingInvitationSecret(rand.Reader)
	if err != nil {
		return PairingState{}, err
	}
	invitation := domaincloud.PairingInvitation{Secret: secret}
	started, err := p.Client.StartInvitation(ctx, p.Credential, invitation.SecretHash())
	if err != nil {
		p.failed(err)
		return p.State(), err
	}
	invitation.InvitationID = started.InvitationID
	expires := p.now().Add(time.Duration(started.ExpiresIn) * time.Second)
	if parsed, err := time.Parse(time.RFC3339, started.ExpiresAt); err == nil {
		expires = parsed
	}
	// The control plane's TTL is authoritative, but a machine whose clock ran
	// away would otherwise show a code it thinks is good for hours.
	if ceiling := p.now().Add(adaptercloud.InvitationLifetime); expires.After(ceiling) {
		expires = ceiling
	}
	invitation.ExpiresAt = expires.UnixMilli()
	link, err := invitation.URL(p.appOrigin())
	if err != nil {
		p.failed(err)
		return p.State(), err
	}

	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
	}
	p.invitation = invitation
	p.state = PairingState{
		Phase:              PairingWaiting,
		InvitationID:       invitation.InvitationID,
		Link:               link,
		ExpiresAt:          expires.Unix(),
		MachineFingerprint: key.Fingerprint(),
	}
	// The poll loop outlives the request that started it: the person is at
	// their browser now, not at this HTTP call.
	loop, cancel := context.WithDeadline(context.WithoutCancel(ctx), expires)
	p.cancel = cancel
	state := p.state
	p.mu.Unlock()

	go p.wait(loop, invitation)
	p.logf("cloud: pairing invitation %s is waiting until %s", invitation.InvitationID, expires.Format(time.RFC3339))
	return state, nil
}

// Cancel stops waiting. The invitation is left to expire on its own rather
// than withdrawn: there is no route that withdraws one, and pretending there
// is would make a person believe a code on a screen is dead when it is not.
func (p *Pairing) Cancel() PairingState {
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	if p.state.Phase == PairingWaiting || p.state.Phase == PairingSealing {
		p.state = PairingState{Phase: PairingIdle}
	}
	p.invitation = domaincloud.PairingInvitation{}
	state := p.state
	p.mu.Unlock()
	return state
}

// wait polls one invitation until it is answered, expires, or is replaced.
func (p *Pairing) wait(ctx context.Context, invitation domaincloud.PairingInvitation) {
	defer func() {
		p.mu.Lock()
		if p.invitation.InvitationID == invitation.InvitationID {
			p.cancel = nil
		}
		p.mu.Unlock()
	}()
	ticker := time.NewTicker(PairingPollInterval)
	defer ticker.Stop()
	for {
		answer, err := p.Client.PollInvitation(ctx, p.Credential, invitation.InvitationID)
		switch {
		case err == nil:
			p.answer(ctx, invitation, answer)
			return
		case errors.Is(err, adaptercloud.ErrPairingPending):
			// The ordinary state. Nothing is recorded for it: a status route
			// that wrote a line every two seconds would bury the one that
			// matters.
		case ctx.Err() != nil:
			return
		default:
			p.failed(err)
			return
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				p.failed(adaptercloud.ErrInvitationGone)
			}
			return
		case <-ticker.C:
		}
	}
}

// answer opens what the browser left and completes the handover.
func (p *Pairing) answer(ctx context.Context, invitation domaincloud.PairingInvitation, poll adaptercloud.InvitationPoll) {
	if poll.AccountID != p.AccountID || poll.MachineID != p.MachineID {
		// The route is authenticated, but its answer is still input. Refusing
		// here keeps a mismatched response from reaching the seal.
		p.failed(fmt.Errorf("that invitation was answered for %s/%s, not %s/%s",
			poll.AccountID, poll.MachineID, p.AccountID, p.MachineID))
		return
	}
	p.setPhase(PairingSealing)
	fragment, err := invitation.OpenEncryptedOffer(poll.EncryptedOffer, p.now().UnixMilli())
	if err != nil {
		p.failed(err)
		return
	}
	offer, err := domaincloud.DecodePairingOfferFragment(fragment, p.now().UnixMilli())
	if err != nil {
		p.failed(err)
		return
	}
	if offer.ViewerDeviceID != poll.ViewerDeviceID {
		p.failed(fmt.Errorf("the offer names viewer %s and the control plane named %s",
			offer.ViewerDeviceID, poll.ViewerDeviceID))
		return
	}
	if _, err := p.deliver(ctx, offer); err != nil {
		p.failed(err)
	}
}

// Complete is the other way in: a person copies the offer the browser is
// showing and pastes it here.
//
// It is the path a desktop browser takes, because a machine showing a QR to a
// laptop in front of it is a camera that is not there. The cryptography is
// identical — the invitation exists only to carry these same bytes when the
// browser is a phone.
func (p *Pairing) Complete(ctx context.Context, offerFragment string) (PairingState, error) {
	if p == nil || p.Client == nil || p.Credential == "" || p.MachineID == "" {
		return PairingState{}, ErrPairingUnavailable
	}
	offer, err := domaincloud.DecodePairingOfferFragment(offerFragment, p.now().UnixMilli())
	if err != nil {
		p.failed(err)
		return p.State(), err
	}
	if offer.AccountID != p.AccountID {
		err := fmt.Errorf("that pairing code belongs to account %s, not %s", offer.AccountID, p.AccountID)
		p.failed(err)
		return p.State(), err
	}
	p.setPhase(PairingSealing)
	state, err := p.deliver(ctx, offer)
	if err != nil {
		p.failed(err)
		return p.State(), err
	}
	return state, nil
}

// deliver seals, delivers, checks the echo and pins. It is the one place any
// of those four happen.
func (p *Pairing) deliver(ctx context.Context, offer domaincloud.PairingOffer) (PairingState, error) {
	key, secret, err := p.Keys()
	if err != nil {
		return PairingState{}, err
	}
	if !key.Valid() || !secret.Valid() {
		return PairingState{}, ErrPairingUnavailable
	}
	handover := domaincloud.PairingHandover{
		AccountID:          p.AccountID,
		MachineID:          p.MachineID,
		MachineSigningKey:  base64.StdEncoding.EncodeToString(key.PublicKey()),
		MachineFingerprint: key.Fingerprint(),
		KeyID:              adaptercloud.MasterKeyID,
		MasterSecret:       base64.StdEncoding.EncodeToString(secret.Bytes()),
	}
	ephemeral, err := domaincloud.NewX25519PrivateKey(rand.Reader)
	if err != nil {
		return PairingState{}, err
	}
	nonce := make([]byte, domaincloud.NonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return PairingState{}, err
	}
	wrapper, err := domaincloud.SealPairingHandover(handover, offer, p.MachineID, ephemeral, nonce, p.now().UnixMilli())
	if err != nil {
		return PairingState{}, err
	}
	delivery, err := p.Client.CompletePairing(ctx, p.Credential, offer.PairingID, wrapper.CanonicalJSON())
	if err != nil {
		return PairingState{}, err
	}
	if delivery.Fingerprint != offer.ViewerFingerprint {
		return PairingState{}, fmt.Errorf(
			"the pairing service recorded fingerprint %s and this code carries %s",
			delivery.Fingerprint, offer.ViewerFingerprint)
	}

	now := p.now()
	if err := p.Pinned.Pin(p.AccountID, adaptercloud.PinnedDevice{
		DeviceID:    offer.ViewerDeviceID,
		PublicKey:   offer.ViewerSigningKey,
		Fingerprint: offer.ViewerFingerprint,
		PairedAt:    now.Unix(),
	}); err != nil {
		// The browser already holds the key by now, so this is not "pairing
		// failed"; it is "this machine will not remember it". Both halves of
		// that go in the error rather than only the tidy one.
		return PairingState{}, fmt.Errorf(
			"the handover reached the browser but this machine could not pin it: %w", err)
	}

	p.mu.Lock()
	p.state.Phase = PairingPaired
	p.state.ViewerDeviceID = offer.ViewerDeviceID
	p.state.ViewerFingerprint = offer.ViewerFingerprint
	p.state.MachineFingerprint = handover.MachineFingerprint
	p.state.PairedAt = now.Unix()
	p.state.Error, p.state.ErrorAt = "", 0
	p.state.Link = ""
	state := p.state
	p.mu.Unlock()
	p.logf("cloud: paired viewer %s (%s)", offer.ViewerDeviceID, offer.ViewerFingerprint)
	return state, nil
}

// Revoke throws a pinned viewer out of this machine.
//
// It is local and it is immediate. The control plane's own revocation needs a
// browser session, which a daemon does not have; and even when the account
// revokes a device, this machine learns it at the next roster refresh. A
// refusal written here stops that sender at its next envelope and keeps
// stopping it whatever the roster later says.
func (p *Pairing) Revoke(deviceID string) (bool, error) {
	if p == nil || p.Pinned == nil {
		return false, ErrPairingUnavailable
	}
	changed, err := p.Pinned.Revoke(deviceID, p.now())
	if err == nil && changed {
		p.logf("cloud: viewer %s is no longer allowed on this machine", deviceID)
	}
	return changed, err
}

func (p *Pairing) setPhase(phase string) {
	p.mu.Lock()
	p.state.Phase = phase
	p.mu.Unlock()
}

func (p *Pairing) failed(err error) {
	if err == nil {
		return
	}
	p.mu.Lock()
	p.state.Phase = PairingFailed
	p.state.Error = err.Error()
	p.state.ErrorAt = p.now().Unix()
	p.state.Link = ""
	p.mu.Unlock()
	p.logf("cloud: pairing did not complete: %v", err)
}

func (p *Pairing) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Pairing) appOrigin() string {
	if p.AppOrigin != "" {
		return p.AppOrigin
	}
	return adaptercloud.DefaultAppOrigin
}

func (p *Pairing) logf(format string, args ...any) {
	if p.Log != nil {
		p.Log(format, args...)
	}
}
