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
//   - **Operate pins are written last.** Readonly pairings instead reserve the
//     durable ceiling before key delivery; a failed final write must not turn
//     an explicitly readonly browser into a legacy roster-only operator. That
//     reservation keeps a revoked browser revoked until the handover completes.
//
// The secret never reaches a log, a status route or the control plane. What
// leaves this file is an invitation id, a fragment a person carries, and
// fingerprints.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
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
	Scope string `json:"scope,omitempty"`
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
	// OnRevoke clears a legacy grant record and the active Cloud connection
	// after the pin has been refused. A failure is reported as partial revoke.
	Authority     *sync.Mutex
	Generation    *atomic.Uint64
	ContentState  func() (domaincloud.ContentKeyState, error)
	RevokeContent func(string) (bool, error)
	OnRevoke      func(string) error
	OnScopeChange func(string) error

	mu         sync.Mutex
	state      PairingState
	invitation domaincloud.PairingInvitation
	cancel     context.CancelFunc
}

// ErrViewerScopeApplied distinguishes persisted authority from a failed lease sweep.
var ErrViewerScopeApplied = errors.New("viewer scope was persisted but terminal lease revalidation failed")

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
type pairingGenerationContext struct{}

func (p *Pairing) withGeneration(ctx context.Context) context.Context {
	if p != nil && p.Generation != nil {
		return context.WithValue(ctx, pairingGenerationContext{}, p.Generation.Load())
	}
	return ctx
}

func (p *Pairing) Begin(ctx context.Context, scopes ...string) (PairingState, error) {
	ctx = p.withGeneration(ctx)
	scope, err := pairingScope(scopes)
	if err != nil {
		return PairingState{}, err
	}
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
		Scope:              scope,
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

	go p.wait(loop, invitation, scope)
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
func (p *Pairing) wait(ctx context.Context, invitation domaincloud.PairingInvitation, scopes ...string) {
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
			p.answer(ctx, invitation, answer, scopes...)
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
func (p *Pairing) answer(ctx context.Context, invitation domaincloud.PairingInvitation, poll adaptercloud.InvitationPoll, scopes ...string) {
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
	if _, err := p.deliver(ctx, offer, scopes...); err != nil {
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
func (p *Pairing) Complete(ctx context.Context, offerFragment string, scopes ...string) (PairingState, error) {
	ctx = p.withGeneration(ctx)
	if _, err := pairingScope(scopes); err != nil {
		return PairingState{}, err
	}
	if p == nil || p.Client == nil || p.Credential == "" || p.MachineID == "" {
		return PairingState{}, ErrPairingUnavailable
	}
	offer, err := p.CheckOffer(offerFragment)
	if err != nil {
		p.failed(err)
		return p.State(), err
	}
	p.setPhase(PairingSealing)
	state, err := p.deliver(ctx, offer, scopes...)
	if err != nil {
		p.failed(err)
		return p.State(), err
	}
	return state, nil
}

// CheckOffer reads an offer the way Complete does — it decodes, it has not
// expired, and it belongs to this machine's account — without touching the
// pairing in progress. It is how a caller that carries an offer somewhere
// else (a hand-off to another machine) refuses one Complete would refuse.
func (p *Pairing) CheckOffer(offerFragment string) (domaincloud.PairingOffer, error) {
	if p == nil {
		return domaincloud.PairingOffer{}, ErrPairingUnavailable
	}
	offer, err := domaincloud.DecodePairingOfferFragment(offerFragment, p.now().UnixMilli())
	if err != nil {
		return domaincloud.PairingOffer{}, err
	}
	if offer.AccountID != p.AccountID {
		return domaincloud.PairingOffer{}, fmt.Errorf("that pairing code belongs to account %s, not %s", offer.AccountID, p.AccountID)
	}
	return offer, nil
}

// deliver seals, delivers, checks the echo and pins. It is the one place any
// of those four happen.
func (p *Pairing) deliver(ctx context.Context, offer domaincloud.PairingOffer, scopes ...string) (PairingState, error) {
	var generation uint64
	if p.Generation != nil {
		generation = p.Generation.Load()
		if started, ok := ctx.Value(pairingGenerationContext{}).(uint64); ok {
			generation = started
		}
	}
	if p.Authority != nil {
		p.Authority.Lock()
		defer p.Authority.Unlock()
	}
	if p.Generation != nil && generation != p.Generation.Load() {
		return PairingState{}, domaincloud.ErrRekey
	}
	scope, err := pairingScope(scopes)
	if err != nil {
		return PairingState{}, err
	}
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
	if p.ContentState != nil {
		state, err := p.ContentState()
		if err != nil {
			return PairingState{}, err
		}
		handover.KeyID = state.KeyID
		handover.MasterSecret = base64.StdEncoding.EncodeToString(state.Key.Bytes())
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
	// No scope chosen is not a choice of operate: a device this machine already
	// holds keeps its own ceiling, read before the content key is released so
	// an unreadable pin file or an unknown scope refuses the pairing outright.
	kept := scope == ""
	reserve := scope
	if kept {
		if reserve, err = p.Pinned.RepairScope(offer.ViewerDeviceID); err != nil {
			return PairingState{}, err
		}
	}
	// Reserve the readonly ceiling before releasing the content key. Otherwise a
	// failed pin write after delivery would fall back to legacy roster authority.
	// The reservation keeps a refusal: a revoked browser is restored only by the
	// pin below, after the handover has actually completed.
	if reserve == adaptercloud.ScopeReadOnly {
		if err := p.Pinned.ReserveReadOnly(p.AccountID, adaptercloud.PinnedDevice{DeviceID: offer.ViewerDeviceID,
			PublicKey: offer.ViewerSigningKey, Fingerprint: offer.ViewerFingerprint, Scope: reserve,
			PairedAt: p.now().Unix()}); err != nil {
			return PairingState{}, err
		}
		if p.OnScopeChange != nil {
			if err := p.OnScopeChange(offer.ViewerDeviceID); err != nil {
				return PairingState{}, err
			}
		}
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
	pinned := adaptercloud.PinnedDevice{
		Scope:       scope,
		DeviceID:    offer.ViewerDeviceID,
		PublicKey:   offer.ViewerSigningKey,
		Fingerprint: offer.ViewerFingerprint,
		PairedAt:    now.Unix(),
	}
	if kept {
		// Kept under the write's own lock, so a grant that landed during the
		// handover is what survives, not the scope read before it.
		scope, err = p.Pinned.PinKeepingScope(p.AccountID, pinned)
	} else {
		err = p.Pinned.Pin(p.AccountID, pinned)
	}
	if err != nil {
		// The browser already holds the key by now, so this is not "pairing
		// failed"; it is "this machine will not remember it". Both halves of
		// that go in the error rather than only the tidy one.
		return PairingState{}, fmt.Errorf(
			"the handover reached the browser but this machine could not pin it: %w", err)
	}

	p.mu.Lock()
	p.state.Scope = scope
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
	if p.RevokeContent != nil {
		return p.RevokeContent(deviceID)
	}
	changed, err := p.Pinned.Revoke(deviceID, p.now())
	if err == nil && changed {
		p.logf("cloud: viewer %s is no longer allowed on this machine", deviceID)
		if p.OnRevoke != nil {
			if dropErr := p.OnRevoke(deviceID); dropErr != nil {
				return true, fmt.Errorf("the viewer pin was revoked but its terminal grant could not be removed: %w", dropErr)
			}
		}
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

// MARK: identity pairing

// Identity pairing is the second handover (`internal/adapters/cloud`
// MachineIdentityPairing): the one that gives a browser the master secret
// with a content-key epoch Cloud recorded, which is what owner-sealed
// diagnostics need. The legacy invitation above stays as it is; this one runs
// beside it and never touches its pins.
//
// The order a person sees:
//
//  1. Begin: this machine starts a pairing and the local page shows a
//     `#pairid=` link and QR.
//  2. The browser opens it, shows a six-digit code and the machine
//     fingerprint, and sends its offer once its person confirms.
//  3. This machine opens the offer and shows the same code locally. Nothing
//     is handed over until the person here answers Decide(approve=true);
//     there is no automatic approval and no route a Cloud viewer can reach.
//  4. The grant, the browser's activate and this machine's attested confirm
//     follow without further input.

// Identity pairing phases, as the local route names them.
const (
	IdentityPairingIdle    = "idle"
	IdentityPairingWaiting = "waiting_viewer"
	IdentityPairingConfirm = "confirm_code"
	IdentityPairingHanding = "handing_over"
	IdentityPairingPaired  = "paired"
	IdentityPairingDenied  = "denied"
	IdentityPairingFailed  = "failed"
	IdentityPairingStopped = "cancelled"
)

// IdentityPairingLifetimeLimit bounds one run: Cloud's pairing row lives at most
// 600 s, and the minute past it lets the last poll answer `expired` rather
// than the context cutting it short.
const IdentityPairingLifetimeLimit = 11 * time.Minute

// IdentityPairingBeginTimeout bounds how long Begin waits for Cloud's start
// answer before it reports the run as not started.
const IdentityPairingBeginTimeout = 20 * time.Second

// Identity pairing refusals a route branches on.
var (
	ErrIdentityPairingUnknown    = errors.New("no identity pairing with that id is waiting for a decision")
	ErrIdentityPairingNotWaiting = errors.New("the identity pairing is not waiting for a code decision")
)

// IdentityPairingState is what the local page is shown. Like PairingState it
// carries no secret apart from the link's fragment, which is a one-time QR
// only this machine's own token can read.
type IdentityPairingState struct {
	Phase              string `json:"phase"`
	PairingID          string `json:"pairing_id,omitempty"`
	Link               string `json:"link,omitempty"`
	ExpiresAt          int64  `json:"expires_at,omitempty"`
	MachineFingerprint string `json:"machine_fingerprint,omitempty"`
	// Code and ViewerFingerprint are present only while Phase is
	// confirm_code: the person compares them with the browser's screen.
	Code              string `json:"code,omitempty"`
	ViewerFingerprint string `json:"viewer_fingerprint,omitempty"`
	ViewerDeviceID    string `json:"viewer_device_id,omitempty"`
	ContentKeyEpoch   int64  `json:"content_key_epoch,omitempty"`
	PairedAt          int64  `json:"paired_at,omitempty"`
	// Error is a stable code (identityErrorCode), never Cloud's message.
	Error   string `json:"error,omitempty"`
	ErrorAt int64  `json:"error_at,omitempty"`
}

// IdentityPairing drives one identity pairing at a time, for the same reason
// Pairing does: two live codes and one person is how the wrong one is used.
type IdentityPairing struct {
	Authority    *sync.Mutex
	Generation   *atomic.Uint64
	ContentState func() (domaincloud.ContentKeyState, error)
	Pinned       *adaptercloud.PinnedStore
	AccountID    string
	AppOrigin    string
	Client       *adaptercloud.IdentityClient
	Keys         PairingKeys
	Epochs       adaptercloud.ContentEpochStore
	Now          func() time.Time
	Log          func(format string, args ...any)
	// Run replaces the adapter's run in tests; nil runs MachineIdentityPairing.
	Run func(ctx context.Context, m *adaptercloud.MachineIdentityPairing, accountID string) (adaptercloud.IdentityPairingResult, error)

	mu     sync.Mutex
	state  IdentityPairingState
	gen    uint64
	cancel context.CancelFunc
	decide chan bool
}

// State is the current identity pairing.
func (p *IdentityPairing) State() IdentityPairingState {
	if p == nil {
		return IdentityPairingState{Phase: IdentityPairingIdle}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.state
	if state.Phase == "" {
		state.Phase = IdentityPairingIdle
	}
	if (state.Phase == IdentityPairingWaiting || state.Phase == IdentityPairingConfirm) &&
		state.ExpiresAt > 0 && p.now().Unix() > state.ExpiresAt {
		state = IdentityPairingState{Phase: IdentityPairingFailed, PairingID: state.PairingID,
			Error: "expired", ErrorAt: state.ExpiresAt}
	}
	return state
}

// Begin starts a fresh identity pairing, replacing one in flight, and
// answers once Cloud has issued the QR (or refused).
func (p *IdentityPairing) Begin(ctx context.Context) (IdentityPairingState, error) {
	if p == nil || p.Client == nil || p.Keys == nil || p.AccountID == "" {
		return IdentityPairingState{}, ErrPairingUnavailable
	}
	var authorityGeneration uint64
	if p.Generation != nil {
		authorityGeneration = p.Generation.Load()
	}
	key, _, err := p.Keys()
	if err != nil {
		return IdentityPairingState{}, err
	}
	if !key.Valid() {
		return IdentityPairingState{}, ErrPairingUnavailable
	}

	if p.Authority != nil {
		p.Authority.Lock()
	}
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
	}
	p.gen++
	gen := p.gen
	run, cancel := context.WithTimeout(context.WithoutCancel(ctx), IdentityPairingLifetimeLimit)
	p.cancel = cancel
	decide := make(chan bool, 1)
	p.decide = decide
	p.state = IdentityPairingState{Phase: IdentityPairingWaiting, MachineFingerprint: key.Fingerprint()}
	p.mu.Unlock()
	if p.Authority != nil {
		p.Authority.Unlock()
	}

	shown := make(chan struct{})
	finished := make(chan error, 1)
	m := &adaptercloud.MachineIdentityPairing{
		Client:     p.Client,
		MachineKey: key,
		Master: func() ([]byte, bool, error) {
			_, secret, err := p.Keys()
			if err != nil {
				return nil, false, err
			}
			if !secret.Valid() {
				return nil, false, nil
			}
			return bytes.Clone(secret.Bytes()), true, nil
		},
		Epochs: p.Epochs,
		Now:    p.Now,
		Show: func(fragment string) {
			p.shown(gen, fragment)
			close(shown)
		},
		Approve: func(ctx context.Context, viewerFingerprint, code string) (bool, error) {
			return p.awaitDecision(ctx, gen, decide, viewerFingerprint, code)
		},
	}
	m.ContentState = p.ContentState
	m.Authorize = func(ctx context.Context, fn func() error) error {
		if p.Authority != nil {
			p.Authority.Lock()
			defer p.Authority.Unlock()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p.Generation != nil && p.Generation.Load() != authorityGeneration {
			return domaincloud.ErrRekey
		}
		p.mu.Lock()
		current := p.gen == gen
		p.mu.Unlock()
		if !current {
			return context.Canceled
		}
		return fn()
	}
	if p.ContentState != nil {
		m.Reserve = func(offer domaincloud.IdentityOffer) error {
			if p.Pinned == nil {
				return adaptercloud.ErrViewerNotPinned
			}
			rows, err := p.Pinned.Devices()
			if err != nil {
				return err
			}
			scope := adaptercloud.ScopeReadOnly
			for _, row := range rows {
				if row.DeviceID == offer.ViewerDeviceID {
					scope = row.EffectiveScope()
				}
			}
			if !adaptercloud.ValidViewerScope(scope) {
				return adaptercloud.ErrViewerScope
			}
			if scope == adaptercloud.ScopeReadOnly {
				return p.Pinned.ReserveReadOnly(p.AccountID, adaptercloud.PinnedDevice{DeviceID: offer.ViewerDeviceID, PublicKey: offer.ViewerSigningKey, Fingerprint: offer.ViewerFingerprint, PairedAt: p.now().Unix()})
			}
			return nil
		}
		m.Complete = func(result adaptercloud.IdentityPairingResult) error {
			if p.Pinned == nil {
				return adaptercloud.ErrViewerNotPinned
			}
			_, err := p.Pinned.PinKeepingScope(p.AccountID, adaptercloud.PinnedDevice{DeviceID: result.ViewerDeviceID, PublicKey: result.ViewerSigningKey, Fingerprint: result.ViewerFingerprint, PairedAt: p.now().Unix()})
			return err
		}
	}
	go func() {
		defer cancel()
		runner := p.Run
		if runner == nil {
			runner = func(ctx context.Context, m *adaptercloud.MachineIdentityPairing, accountID string) (adaptercloud.IdentityPairingResult, error) {
				return m.Run(ctx, accountID)
			}
		}
		result, err := runner(run, m, p.AccountID)
		p.finish(gen, result, err)
		finished <- err
	}()

	wait := time.NewTimer(IdentityPairingBeginTimeout)
	defer wait.Stop()
	select {
	case <-shown:
		return p.State(), nil
	case err := <-finished:
		if err == nil {
			return p.State(), nil
		}
		return p.State(), err
	case <-wait.C:
		cancel()
		return p.State(), context.DeadlineExceeded
	case <-ctx.Done():
		// The request went away; the run keeps going so a reload still
		// finds the link through State.
		return p.State(), nil
	}
}

// Decide answers the code comparison for the named pairing. Only the local
// route calls it, and only the person's click on the local page reaches it.
func (p *IdentityPairing) Decide(pairingID string, approve bool) (IdentityPairingState, error) {
	if p == nil {
		return IdentityPairingState{}, ErrPairingUnavailable
	}
	p.mu.Lock()
	if p.state.PairingID == "" || p.state.PairingID != pairingID {
		p.mu.Unlock()
		return p.State(), ErrIdentityPairingUnknown
	}
	if p.state.Phase != IdentityPairingConfirm || p.decide == nil {
		p.mu.Unlock()
		return p.State(), ErrIdentityPairingNotWaiting
	}
	decide := p.decide
	p.decide = nil
	if approve {
		p.state.Phase = IdentityPairingHanding
	}
	p.mu.Unlock()
	decide <- approve
	return p.State(), nil
}

// Cancel stops the run in flight. Cloud's row is left to expire, as with the
// legacy invitation: there is no withdraw route to call.
func (p *IdentityPairing) Cancel() IdentityPairingState {
	if p == nil {
		return IdentityPairingState{Phase: IdentityPairingIdle}
	}
	if p.Authority != nil {
		p.Authority.Lock()
		defer p.Authority.Unlock()
	}
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.gen++
	p.decide = nil
	switch p.state.Phase {
	case IdentityPairingWaiting, IdentityPairingConfirm, IdentityPairingHanding:
		p.state = IdentityPairingState{Phase: IdentityPairingStopped, PairingID: p.state.PairingID, ErrorAt: p.now().Unix()}
	}
	p.mu.Unlock()
	return p.State()
}

func (p *IdentityPairing) shown(gen uint64, fragment string) {
	link := strings.TrimRight(p.appOrigin(), "/") + "/#" + domaincloud.IdentityPairingFragmentName + "=" + fragment
	var pairingID string
	var expires int64
	if raw, err := domaincloud.DecodeCanonicalBase64URL(fragment); err == nil {
		if q, err := domaincloud.DecodeIdentityPairingQR(raw, p.now().UnixMilli()); err == nil {
			pairingID, expires = q.PairingID, q.ExpiresAt/1000
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if gen != p.gen {
		return
	}
	p.state.PairingID = pairingID
	p.state.Link = link
	p.state.ExpiresAt = expires
	p.logf("cloud: identity pairing %s is waiting for a viewer", pairingID)
}

func (p *IdentityPairing) awaitDecision(ctx context.Context, gen uint64, decide chan bool, viewerFingerprint, code string) (bool, error) {
	p.mu.Lock()
	if gen != p.gen {
		p.mu.Unlock()
		return false, context.Canceled
	}
	p.state.Phase = IdentityPairingConfirm
	p.state.Code = code
	p.state.ViewerFingerprint = viewerFingerprint
	p.mu.Unlock()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case approved := <-decide:
		return approved, nil
	}
}

func (p *IdentityPairing) finish(gen uint64, result adaptercloud.IdentityPairingResult, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if gen != p.gen {
		return
	}
	p.cancel = nil
	p.decide = nil
	now := p.now().Unix()
	switch {
	case err == nil:
		p.state = IdentityPairingState{Phase: IdentityPairingPaired, PairingID: result.PairingID,
			MachineFingerprint: p.state.MachineFingerprint, ViewerDeviceID: result.ViewerDeviceID,
			ViewerFingerprint: result.ViewerFingerprint, ContentKeyEpoch: result.ContentKeyEpoch, PairedAt: now}
		p.logf("cloud: identity pairing %s handed content-key epoch %d to viewer %s",
			result.PairingID, result.ContentKeyEpoch, result.ViewerDeviceID)
	case errors.Is(err, domaincloud.ErrPairingNotApproved):
		p.state = IdentityPairingState{Phase: IdentityPairingDenied, PairingID: p.state.PairingID, ErrorAt: now}
		p.logf("cloud: identity pairing %s was not approved on this machine", p.state.PairingID)
	default:
		code := identityErrorCode(err)
		p.state = IdentityPairingState{Phase: IdentityPairingFailed, PairingID: p.state.PairingID,
			MachineFingerprint: p.state.MachineFingerprint, Error: code, ErrorAt: now}
		p.logf("cloud: identity pairing %s did not complete: %s", p.state.PairingID, code)
	}
}

// identityErrorCode is the stable word the page and the log show. Cloud's
// own code is kept (it is a fixed vocabulary); its message is not.
func identityErrorCode(err error) string {
	var refusal *adaptercloud.CloudRefusal
	switch {
	case errors.As(err, &refusal):
		if refusal.Code == "" {
			return fmt.Sprintf("cloud_http_%d", refusal.Status)
		}
		return "cloud_" + refusal.Code
	case errors.Is(err, adaptercloud.ErrIdentityPollExhausted), errors.Is(err, context.DeadlineExceeded):
		return "expired"
	case errors.Is(err, adaptercloud.ErrIdentityGrantMissing):
		return "grant_not_recorded"
	case errors.Is(err, adaptercloud.ErrIdentityNoMaster):
		return "no_master_secret"
	case errors.Is(err, domaincloud.ErrIdentityTranscript), errors.Is(err, domaincloud.ErrIdentityPhaseDigest):
		return "transcript_mismatch"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "unavailable"
	}
}

func (p *IdentityPairing) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *IdentityPairing) appOrigin() string {
	if p.AppOrigin != "" {
		return p.AppOrigin
	}
	return adaptercloud.DefaultAppOrigin
}

func (p *IdentityPairing) logf(format string, args ...any) {
	if p.Log != nil {
		p.Log(format, args...)
	}
}

// pairingScope keeps old callers compatible; scope is selected only on the machine.
// Omitted answers "": deliver resolves it against the device's existing pin,
// because "nobody chose" and "the person chose operate" are different requests.
func pairingScope(scopes []string) (string, error) {
	if len(scopes) > 1 {
		return "", adaptercloud.ErrViewerScope
	}
	if len(scopes) == 0 || scopes[0] == "" {
		return "", nil
	}
	if len(scopes) != 1 || !adaptercloud.ValidViewerScope(scopes[0]) {
		return "", adaptercloud.ErrViewerScope
	}
	return scopes[0], nil
}

// SetViewerScope persists first, then voids leases without closing read streams.
func (p *Pairing) SetViewerScope(device, scope string) error {
	if p == nil || p.Pinned == nil {
		return ErrPairingUnavailable
	}
	if err := p.Pinned.SetScope(device, scope); err != nil {
		return err
	}
	if p.OnScopeChange != nil {
		if err := p.OnScopeChange(device); err != nil {
			return fmt.Errorf("%w: %v", ErrViewerScopeApplied, err)
		}
	}
	return nil
}
