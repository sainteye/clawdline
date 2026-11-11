package cloud

// Browser invitations from this machine's side (docs/browser-invite.md).
//
// The order, and what each step proves:
//
//  1. Start: draw the secret, ask Cloud for an id, show `IIIII-SSSSSS` to the
//     person through this machine's own route (or the encrypted Cloud word).
//  2. A browser typed it and left its CPace message: run CPace as responder,
//     leave this machine's message. Cloud has seen two messages and no secret.
//  3. The browser sealed its confirmation under the CPace key: open it and
//     check it names this invitation, this machine, and the very device id
//     and signing key Cloud reported. Show the six digits.
//  4. The person compares the digits on both screens and approves here or on
//     a paired device. Pin first — invited, operate, with its expiry — then
//     seal the handover and ask Cloud to admit. A pin with no handover is
//     harmless; a handover with no pin would be a browser the roster admits
//     with no end, which is the one order this file refuses to allow.
//
// The secret is never logged and never leaves this file except inside the
// code shown to the person.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

// Browser invitation phases, as the local route names them.
const (
	InvitePhaseWaiting   = "waiting"      // the code is live; nobody typed it
	InvitePhaseExchange  = "exchanging"   // a browser typed it; keys are being agreed
	InvitePhaseConfirm   = "confirm_code" // compare the digits, then approve or deny
	InvitePhaseAdmitting = "admitting"
	InvitePhaseAdmitted  = "admitted"
	InvitePhaseDenied    = "denied"
	InvitePhaseCancelled = "cancelled"
	InvitePhaseFailed    = "failed"
)

// BrowserInvitesLiveLimit bounds invitations in flight on one machine: more
// live codes than this is a person who has lost track of which is which.
const BrowserInvitesLiveLimit = 3

// BrowserInviteHistoryLimit bounds finished invitations kept for the status
// route, newest first.
const BrowserInviteHistoryLimit = 8

// BrowserInvitePollInterval is how often Cloud is asked about a live one.
const BrowserInvitePollInterval = 2 * time.Second

// BrowserInviteNameLimit bounds the name a browser asks to be known by.
const BrowserInviteNameLimit = 64

// browserInviteGrace lets the last poll after Cloud's expiry answer
// `expired` rather than the context cutting it short.
const browserInviteGrace = time.Minute

// Browser invitation refusals the local route branches on.
var (
	ErrInviteNoRoom     = errors.New("this machine already has the most live browser invitations it keeps")
	ErrInviteExpiry     = errors.New("an invitation's expiry is 8h, 7d or never")
	ErrInviteUnknown    = errors.New("no browser invitation with that id")
	ErrInviteNotWaiting = errors.New("that browser invitation is not waiting for a decision")
)

// BrowserInviteView is what a person is shown about one invitation. Code is
// present only while the invitation is waiting for a browser to type it;
// ComparisonCode only while it waits for a decision.
type BrowserInviteView struct {
	InviteID       string `json:"invite_id"`
	Phase          string `json:"phase"`
	Expiry         string `json:"expiry"`
	Code           string `json:"code,omitempty"`
	CodeExpiresAt  int64  `json:"code_expires_at,omitempty"`
	ComparisonCode string `json:"comparison_code,omitempty"`
	BrowserName    string `json:"browser_name,omitempty"`
	DeviceID       string `json:"device_id,omitempty"`
	// AdmissionEndsAt is when the admitted browser stops being admitted,
	// Unix seconds; 0 with Phase admitted is never.
	AdmissionEndsAt int64  `json:"admission_ends_at,omitempty"`
	Error           string `json:"error,omitempty"`
}

// browserInviteClient is the slice of the Cloud client this file uses.
type browserInviteClient interface {
	StartBrowserInvite(ctx context.Context, credential, expiry string) (adaptercloud.BrowserInviteStart, error)
	PollBrowserInvite(ctx context.Context, credential, inviteID string) (adaptercloud.BrowserInvitePoll, error)
	AnswerBrowserInvite(ctx context.Context, credential, inviteID, machineMessage string) error
	AdmitBrowserInvite(ctx context.Context, credential, inviteID, deviceID, handover string, expiresAt int64) (adaptercloud.BrowserInviteAdmit, error)
	DenyBrowserInvite(ctx context.Context, credential, inviteID string) error
	RevokeInvitedBrowser(ctx context.Context, credential, deviceID string) error
}

// BrowserInvites drives this machine's browser invitations.
type BrowserInvites struct {
	AccountID    string
	MachineID    string
	Credential   string
	Client       browserInviteClient
	Keys         PairingKeys
	ContentState func() (domaincloud.ContentKeyState, error)
	Pinned       *adaptercloud.PinnedStore
	// Authority is the lock key delivery and rekeying share: the content key
	// sealed for the browser must be the one in force when it is pinned.
	Authority *sync.Mutex
	Random    io.Reader
	Now       func() time.Time
	Log       func(format string, args ...any)
	// PollInterval replaces BrowserInvitePollInterval in tests.
	PollInterval time.Duration
	// Revoke is this machine's ordinary revocation (Pairing.Revoke), which
	// also rotates the content key and tells Cloud about an invited browser.
	Revoke func(deviceID string) (bool, error)

	mu      sync.Mutex
	live    map[string]*browserInvite
	history []BrowserInviteView
}

type browserInvite struct {
	view      BrowserInviteView
	secret    string
	expires   time.Time
	cancel    context.CancelFunc
	decide    chan bool
	keys      domaincloud.InviteKeys
	deviceID  string
	publicKey string
	name      string
}

// Start makes a new invitation for a browser that will stay admitted for
// expiry ("8h", "7d" or "never").
func (b *BrowserInvites) Start(ctx context.Context, expiry string) (BrowserInviteView, error) {
	if b == nil || b.Client == nil || b.Credential == "" || b.MachineID == "" || b.Pinned == nil {
		return BrowserInviteView{}, ErrPairingUnavailable
	}
	if _, ok := domaincloud.InviteExpirySeconds(expiry); !ok {
		return BrowserInviteView{}, ErrInviteExpiry
	}
	b.mu.Lock()
	if len(b.live) >= BrowserInvitesLiveLimit {
		b.mu.Unlock()
		return BrowserInviteView{}, ErrInviteNoRoom
	}
	b.mu.Unlock()
	secret, err := domaincloud.NewInviteSecret(b.random())
	if err != nil {
		return BrowserInviteView{}, err
	}
	started, err := b.Client.StartBrowserInvite(ctx, b.Credential, expiry)
	if err != nil {
		return BrowserInviteView{}, err
	}
	if !domaincloud.ValidInviteID(started.InviteID) {
		return BrowserInviteView{}, fmt.Errorf("Cloud answered an invitation id that is not one: %q", started.InviteID)
	}
	expires := time.UnixMilli(started.ExpiresAt)
	if ceiling := b.now().Add(adaptercloud.InvitationLifetime); expires.After(ceiling) {
		expires = ceiling
	}
	run, cancel := context.WithDeadline(context.WithoutCancel(ctx), expires.Add(browserInviteGrace))
	inv := &browserInvite{
		view: BrowserInviteView{InviteID: started.InviteID, Phase: InvitePhaseWaiting, Expiry: expiry,
			Code: domaincloud.FormatInviteCode(started.InviteID, secret), CodeExpiresAt: expires.Unix()},
		secret: secret, expires: expires, cancel: cancel, decide: make(chan bool, 1),
	}
	b.mu.Lock()
	if b.live == nil {
		b.live = map[string]*browserInvite{}
	}
	if len(b.live) >= BrowserInvitesLiveLimit {
		b.mu.Unlock()
		cancel()
		return BrowserInviteView{}, ErrInviteNoRoom
	}
	b.live[started.InviteID] = inv
	view := inv.view
	b.mu.Unlock()
	go b.run(run, inv)
	b.logf("cloud: browser invitation %s is waiting until %s", started.InviteID, expires.Format(time.RFC3339))
	return view, nil
}

// List is every live invitation, then the recent finished ones.
func (b *BrowserInvites) List() []BrowserInviteView {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]BrowserInviteView, 0, len(b.live)+len(b.history))
	for _, inv := range b.live {
		out = append(out, b.viewLocked(inv))
	}
	return append(out, b.history...)
}

// InvitedBrowser is one browser an invitation admitted, as the owner's list
// shows it. Nothing secret: ids, a name, a fingerprint and times.
type InvitedBrowser struct {
	DeviceID    string `json:"device_id"`
	Name        string `json:"name,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	PairedAt    int64  `json:"paired_at"`
	// ExpiresAt is Unix seconds; 0 is never.
	ExpiresAt int64 `json:"expires_at"`
	// Active is false once it expired or was revoked.
	Active    bool  `json:"active"`
	RevokedAt int64 `json:"revoked_at,omitempty"`
}

// InvitedBrowsers lists every invited browser this machine pinned, newest
// first. An unreadable pin file is an error, never an empty list.
func (b *BrowserInvites) InvitedBrowsers() ([]InvitedBrowser, error) {
	if b == nil || b.Pinned == nil {
		return nil, ErrPairingUnavailable
	}
	rows, err := b.Pinned.Devices()
	if err != nil {
		return nil, err
	}
	out := []InvitedBrowser{}
	for _, row := range rows {
		if !row.Invited {
			continue
		}
		out = append(out, InvitedBrowser{DeviceID: row.DeviceID, Name: row.Name, Fingerprint: row.Fingerprint,
			PairedAt: row.PairedAt, ExpiresAt: row.ExpiresAt, Active: row.Active(), RevokedAt: row.RevokedAt})
	}
	return out, nil
}

// Counts is how many invitations are live and how many finished ones are
// kept, for the capacity register.
func (b *BrowserInvites) Counts() (live, kept int) {
	if b == nil {
		return 0, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.live), len(b.history)
}

// Status is one invitation, live or recently finished.
func (b *BrowserInvites) Status(id string) (BrowserInviteView, error) {
	if b == nil {
		return BrowserInviteView{}, ErrInviteUnknown
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if inv, ok := b.live[id]; ok {
		return b.viewLocked(inv), nil
	}
	for _, v := range b.history {
		if v.InviteID == id {
			return v, nil
		}
	}
	return BrowserInviteView{}, ErrInviteUnknown
}

func (b *BrowserInvites) viewLocked(inv *browserInvite) BrowserInviteView {
	v := inv.view
	if v.Phase == InvitePhaseWaiting && b.now().After(inv.expires) {
		v.Code = ""
	}
	return v
}

// Decide answers an invitation waiting on its comparison code.
func (b *BrowserInvites) Decide(id string, approve bool) (BrowserInviteView, error) {
	if b == nil {
		return BrowserInviteView{}, ErrInviteUnknown
	}
	b.mu.Lock()
	inv, ok := b.live[id]
	if !ok {
		b.mu.Unlock()
		return BrowserInviteView{}, ErrInviteUnknown
	}
	if inv.view.Phase != InvitePhaseConfirm {
		b.mu.Unlock()
		return BrowserInviteView{}, ErrInviteNotWaiting
	}
	select {
	case inv.decide <- approve:
	default:
		b.mu.Unlock()
		return BrowserInviteView{}, ErrInviteNotWaiting
	}
	if approve {
		inv.view.Phase = InvitePhaseAdmitting
	}
	view := inv.view
	b.mu.Unlock()
	return view, nil
}

// Cancel withdraws a live invitation; Cloud is told so the code dies now.
func (b *BrowserInvites) Cancel(ctx context.Context, id string) (BrowserInviteView, error) {
	if b == nil {
		return BrowserInviteView{}, ErrInviteUnknown
	}
	b.mu.Lock()
	inv, ok := b.live[id]
	if !ok || inv.view.Phase == InvitePhaseAdmitting {
		b.mu.Unlock()
		if ok {
			return BrowserInviteView{}, ErrInviteNotWaiting
		}
		return BrowserInviteView{}, ErrInviteUnknown
	}
	inv.cancel()
	b.mu.Unlock()
	if err := b.Client.DenyBrowserInvite(ctx, b.Credential, id); err != nil && !errors.Is(err, adaptercloud.ErrInviteUnusable) {
		b.logf("cloud: browser invitation %s was cancelled here but Cloud was not told: %v", id, err)
	}
	return b.finish(inv, InvitePhaseCancelled, ""), nil
}

// RevokeInvited ends an invited browser's admission through this machine's
// ordinary revocation. It refuses a device that is not an invited browser, so
// the invitation page cannot be used to throw out account devices.
func (b *BrowserInvites) RevokeInvited(deviceID string) (bool, error) {
	if b == nil || b.Pinned == nil || b.Revoke == nil {
		return false, ErrPairingUnavailable
	}
	rows, err := b.Pinned.Devices()
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if row.DeviceID == deviceID && row.Invited {
			return b.Revoke(deviceID)
		}
	}
	return false, ErrInviteUnknown
}

// run is one invitation's life.
func (b *BrowserInvites) run(ctx context.Context, inv *browserInvite) {
	defer inv.cancel()
	id := inv.view.InviteID
	poll, err := b.waitFor(ctx, id, adaptercloud.InviteStatusJoined)
	if err != nil {
		b.end(inv, err)
		return
	}
	b.setPhase(inv, InvitePhaseExchange)
	browserMessage, err := base64.StdEncoding.DecodeString(poll.BrowserMessage)
	if err != nil {
		b.end(inv, fmt.Errorf("the browser's CPace message is not base64"))
		return
	}
	prs, ci, sid := domaincloud.InviteCPaceInputs(id, inv.secret)
	party, err := domaincloud.NewCPaceParty(b.random(), prs, ci, sid, domaincloud.InviteMachineAD, false)
	if err != nil {
		b.end(inv, err)
		return
	}
	isk, err := party.Finish(browserMessage, domaincloud.InviteBrowserAD)
	if err != nil {
		// An invalid element is an attack or a broken browser; either way the
		// invitation is spent, which Cloud is told.
		_ = b.Client.DenyBrowserInvite(context.WithoutCancel(ctx), b.Credential, id)
		b.end(inv, err)
		return
	}
	keys, err := domaincloud.DeriveInviteKeys(isk, id)
	if err != nil {
		b.end(inv, err)
		return
	}
	if err := b.Client.AnswerBrowserInvite(ctx, b.Credential, id, base64.StdEncoding.EncodeToString(party.Message())); err != nil {
		b.end(inv, err)
		return
	}
	poll, err = b.waitFor(ctx, id, adaptercloud.InviteStatusConfirmed)
	if err != nil {
		b.end(inv, err)
		return
	}
	confirm, err := domaincloud.OpenInviteConfirm(keys, poll.BrowserConfirm, id, b.MachineID, poll.DeviceID, poll.DevicePublicKey)
	if err != nil {
		// A wrong secret lands here: the browser's key is not this one. The
		// invitation is spent rather than left open for another guess.
		_ = b.Client.DenyBrowserInvite(context.WithoutCancel(ctx), b.Credential, id)
		b.end(inv, err)
		return
	}
	b.mu.Lock()
	inv.keys, inv.deviceID, inv.publicKey = keys, confirm.DeviceID, confirm.PublicKey
	inv.name = inviteBrowserName(confirm.Name)
	inv.view.Phase = InvitePhaseConfirm
	inv.view.Code = ""
	inv.view.ComparisonCode = keys.Code
	inv.view.BrowserName = inv.name
	inv.view.DeviceID = confirm.DeviceID
	b.mu.Unlock()

	var approve bool
	select {
	case approve = <-inv.decide:
	case <-ctx.Done():
		_ = b.Client.DenyBrowserInvite(context.WithoutCancel(ctx), b.Credential, id)
		b.end(inv, ctx.Err())
		return
	}
	if !approve {
		if err := b.Client.DenyBrowserInvite(context.WithoutCancel(ctx), b.Credential, id); err != nil && !errors.Is(err, adaptercloud.ErrInviteUnusable) {
			b.logf("cloud: browser invitation %s was denied here but Cloud was not told: %v", id, err)
		}
		b.finish(inv, InvitePhaseDenied, "")
		return
	}
	ends, err := b.admit(context.WithoutCancel(ctx), inv)
	if err != nil {
		b.finish(inv, InvitePhaseFailed, inviteErrorCode(err))
		b.logf("cloud: browser invitation %s could not admit its browser: %v", id, err)
		return
	}
	b.mu.Lock()
	inv.view.AdmissionEndsAt = ends
	b.mu.Unlock()
	b.finish(inv, InvitePhaseAdmitted, "")
	b.logf("cloud: browser invitation %s admitted %s", id, inv.deviceID)
}

// admit pins, seals and asks Cloud to admit, in that order.
func (b *BrowserInvites) admit(ctx context.Context, inv *browserInvite) (int64, error) {
	if b.Authority != nil {
		b.Authority.Lock()
		defer b.Authority.Unlock()
	}
	seconds, _ := domaincloud.InviteExpirySeconds(inv.view.Expiry)
	now := b.now()
	var ends int64
	if seconds > 0 {
		ends = now.Unix() + seconds
	}
	key, secret, err := b.Keys()
	if err != nil {
		return 0, err
	}
	if !key.Valid() || !secret.Valid() {
		return 0, ErrPairingUnavailable
	}
	keyID, master := adaptercloud.MasterKeyID, secret.Bytes()
	if b.ContentState != nil {
		state, err := b.ContentState()
		if err != nil {
			return 0, err
		}
		keyID, master = state.KeyID, state.Key.Bytes()
	}
	if err := b.Pinned.Pin(b.AccountID, adaptercloud.PinnedDevice{
		Scope: adaptercloud.ScopeOperate, DeviceID: inv.deviceID, PublicKey: inv.publicKey,
		Fingerprint: domaincloud.Fingerprint(mustKey(inv.publicKey)), PairedAt: now.Unix(),
		Name: inv.name, Invited: true, ExpiresAt: ends,
	}); err != nil {
		return 0, fmt.Errorf("the browser was not pinned, so nothing was handed over: %w", err)
	}
	nonce := make([]byte, domaincloud.NonceBytes)
	if _, err := io.ReadFull(b.random(), nonce); err != nil {
		return 0, b.unpin(inv, err)
	}
	sealed, err := domaincloud.SealInviteHandover(inv.keys, domaincloud.InviteHandover{
		InviteID: inv.view.InviteID, AccountID: b.AccountID, MachineID: b.MachineID,
		MachineSigningKey:  base64.StdEncoding.EncodeToString(key.PublicKey()),
		MachineFingerprint: key.Fingerprint(), KeyID: keyID,
		MasterSecret: base64.StdEncoding.EncodeToString(master),
		DeviceID:     inv.deviceID, Scope: adaptercloud.ScopeOperate, ExpiresAt: ends,
	}, nonce)
	if err != nil {
		return 0, b.unpin(inv, err)
	}
	if _, err := b.Client.AdmitBrowserInvite(ctx, b.Credential, inv.view.InviteID, inv.deviceID, sealed, ends); err != nil {
		return 0, b.unpin(inv, err)
	}
	return ends, nil
}

// unpin takes back a pin whose handover never left, so a failed admission
// leaves nothing admitted.
func (b *BrowserInvites) unpin(inv *browserInvite, cause error) error {
	if _, err := b.Pinned.Revoke(inv.deviceID, b.now()); err != nil {
		return fmt.Errorf("%v; and the pin could not be taken back: %w", cause, err)
	}
	return cause
}

func mustKey(public string) []byte {
	raw, _ := base64.StdEncoding.DecodeString(public)
	return raw
}

// waitFor polls until the invitation reaches want, or ends.
func (b *BrowserInvites) waitFor(ctx context.Context, id, want string) (adaptercloud.BrowserInvitePoll, error) {
	interval := b.PollInterval
	if interval <= 0 {
		interval = BrowserInvitePollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		poll, err := b.Client.PollBrowserInvite(ctx, b.Credential, id)
		switch {
		case err != nil && ctx.Err() != nil:
			return poll, ctx.Err()
		case err != nil:
			return poll, err
		case poll.MachineID != "" && poll.MachineID != b.MachineID:
			return poll, fmt.Errorf("Cloud answered invitation %s for machine %s", id, poll.MachineID)
		case poll.Status == want:
			return poll, nil
		case poll.Status == adaptercloud.InviteStatusExpired, poll.Status == adaptercloud.InviteStatusDenied:
			return poll, adaptercloud.ErrInviteUnusable
		}
		select {
		case <-ctx.Done():
			return poll, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (b *BrowserInvites) setPhase(inv *browserInvite, phase string) {
	b.mu.Lock()
	inv.view.Phase = phase
	if phase != InvitePhaseWaiting {
		inv.view.Code = ""
	}
	b.mu.Unlock()
}

// end is a run that stopped on an error: cancelled if the person cancelled.
func (b *BrowserInvites) end(inv *browserInvite, err error) {
	b.mu.Lock()
	_, live := b.live[inv.view.InviteID]
	b.mu.Unlock()
	if !live {
		return // Cancel already finished it.
	}
	b.finish(inv, InvitePhaseFailed, inviteErrorCode(err))
}

func (b *BrowserInvites) finish(inv *browserInvite, phase, code string) BrowserInviteView {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, live := b.live[inv.view.InviteID]; !live {
		return inv.view
	}
	delete(b.live, inv.view.InviteID)
	inv.secret = ""
	inv.keys = domaincloud.InviteKeys{}
	inv.view.Phase, inv.view.Error = phase, code
	inv.view.Code, inv.view.ComparisonCode = "", ""
	b.history = append([]BrowserInviteView{inv.view}, b.history...)
	if len(b.history) > BrowserInviteHistoryLimit {
		b.history = b.history[:BrowserInviteHistoryLimit]
	}
	return inv.view
}

// inviteErrorCode is a stable code for the person's screen, never a message.
func inviteErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case strings.Contains(err.Error(), "device_limit_reached"):
		// Cloud's typed refusal for an account with no viewer-device slot left. It reaches
		// here as a 409, which would otherwise read as a spent invitation.
		return "device_limit"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, adaptercloud.ErrInviteUnusable):
		return "expired"
	case errors.Is(err, domaincloud.ErrInviteSealed), errors.Is(err, domaincloud.ErrInviteMismatch),
		errors.Is(err, domaincloud.ErrCPaceInvalidPeer):
		return "code_mismatch"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	}
	return "failed"
}

// inviteBrowserName keeps one short printable line. A name past the limit
// is refused, and the browser is shown as "Browser".
func inviteBrowserName(name string) string {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > BrowserInviteNameLimit {
		return "Browser"
	}
	var out strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			r = ' '
		}
		out.WriteRune(r)
	}
	if s := strings.TrimSpace(out.String()); s != "" {
		return s
	}
	return "Browser"
}

func (b *BrowserInvites) random() io.Reader {
	if b.Random != nil {
		return b.Random
	}
	return rand.Reader
}

func (b *BrowserInvites) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func (b *BrowserInvites) logf(format string, args ...any) {
	if b.Log != nil {
		b.Log(format, args...)
	}
}
