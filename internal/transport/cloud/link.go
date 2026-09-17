package cloud

// The Cloud line, as the daemon owns it.
//
// Three waves built the halves: `internal/domain/cloud` knows what an envelope
// is, `internal/adapters/cloud` knows how to hold a socket open, and
// `internal/app/cloudops` knows what each of the 27 operation words is allowed
// to mean. None of them was reachable from `clawdline serve`: the line only
// existed while somebody typed `clawdline cloud connect`. This file is the
// wiring, and it is the whole of it.
//
// **Off is the default and off is checked first.** `Open` reads the settings
// file before it reads a key, a credential or a hostname, and a link whose
// switch is off holds no key material at all — it answers the status route
// `enabled: false` and starts nothing. `docs/remote.md` design principle 1:
// the free product does not depend on Cloud, so a machine that was never
// enrolled must behave exactly as it did before this file existed.
//
// **Two switches, not one.** `cloud_enabled` is whether the line is up;
// `cloud_commands` is whether a viewer may cause an effect. The second is read
// per request rather than captured at start, because a person turning writes
// off means the request in flight too.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	adaptercloud "github.com/sainteye/clawdline-go/internal/adapters/cloud"
	"github.com/sainteye/clawdline-go/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline-go/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline-go/internal/app/cloudops"
	domaincloud "github.com/sainteye/clawdline-go/internal/domain/cloud"
)

// SendCapability is the roster capability a Cloud viewer needs before this
// machine will act on anything it says.
//
// It is the control plane's word (`api/src/types.ts:85`), not this daemon's:
// the account decides what a device may do, and a machine that made up its own
// answer would be a second permission model. `start_session` is listed beside
// it because starting a session is the other way a viewer causes code to run
// here, and a device trusted with that is trusted with typing.
const (
	SendCapability  = "send_prompt"
	StartCapability = "start_session"
)

// Status is what the status route answers: enough to tell "the switch is off"
// from "the relay refused us" from "it is up and nothing has arrived".
type Status struct {
	// Enabled is the settings switch. It is first because it is the answer to
	// "why is nothing happening" nine times out of ten.
	Enabled bool `json:"enabled"`
	// Commands is the remote-write switch.
	Commands bool `json:"commands"`
	// Configured is whether this machine has an identity and keys — that is,
	// whether `clawdline cloud login` has ever completed here.
	Configured bool `json:"configured"`
	// State is idle | connected | reconnecting | stopped, or "off" when the
	// switch is off. The four are the transport's own vocabulary.
	State string `json:"state"`

	RelayURL string `json:"relay_url,omitempty"`
	APIBase  string `json:"api_base,omitempty"`

	Account     string `json:"account,omitempty"`
	MachineID   string `json:"machine_id,omitempty"`
	MachineName string `json:"machine_name,omitempty"`
	// Fingerprint is this machine's device key as a person can read it back to
	// somebody: the grouped base32 of `CloudKeys.fingerprint`.
	Fingerprint string `json:"fingerprint,omitempty"`

	// LastError is the most recent thing that went wrong, in words, with the
	// moment it happened. A status route that can only say "not connected" is
	// the one that sends a person to the logs.
	LastError   string `json:"last_error,omitempty"`
	LastErrorAt int64  `json:"last_error_at,omitempty"`
	// LastClose is the transport's code for whatever ended the last socket.
	LastClose string `json:"last_close,omitempty"`

	ConnectedSince int64 `json:"connected_since,omitempty"`
	TokenExpiresAt int64 `json:"token_expires_at,omitempty"`

	Connects       int            `json:"connects"`
	Reconnects     int            `json:"reconnects"`
	Published      int            `json:"published"`
	Acked          int            `json:"acked"`
	PublishErrors  int            `json:"publish_errors"`
	InboundTotal   int            `json:"inbound_total"`
	InboundDropped map[string]int `json:"inbound_dropped,omitempty"`
	// Answered counts requests this machine answered, and Refused those it
	// answered with a refusal code. Both are process-lifetime.
	Answered int `json:"answered"`
	Refused  int `json:"refused"`
	// QueueDropped counts requests the bridge never saw because the queue was
	// full. It is separate from the transport's own drop counters, which are
	// about envelopes that never authenticated.
	QueueDropped int `json:"queue_dropped"`

	// Devices is the account's enrolled viewers as this machine last read them.
	Devices []Viewer `json:"devices,omitempty"`
	// RosterReadable is false when the last roster fetch failed. An unreadable
	// roster is not an empty one.
	RosterReadable bool `json:"roster_readable"`

	// Commandset is what this machine would advertise it can answer.
	Commandset []string `json:"commandset,omitempty"`

	// Pairing is the handover in progress, if there is one.
	Pairing PairingState `json:"pairing"`
	// PinnedReadable is false when the local paired-device file could not be
	// read. An unreadable pin file is not an empty one: see `pinned.go`.
	PinnedReadable bool `json:"pinned_readable"`
	// PinnedError says why the pin file could not be read.
	PinnedError string `json:"pinned_error,omitempty"`
}

// Viewer is one enrolled device, as the status route shows it. It carries no
// key material: a fingerprint is what a person compares, a public key is not.
type Viewer struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind,omitempty"`
	Name        string   `json:"name,omitempty"`
	Caps        []string `json:"caps,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	// Pinned is whether this machine itself handed that device the account
	// key. A row that is only in the account's roster is listed too, because
	// it is still admitted — but the two are different amounts of evidence and
	// the settings page says which is which.
	Pinned bool `json:"pinned"`
	// PairedAt is when this machine pinned it, Unix seconds.
	PairedAt int64 `json:"paired_at,omitempty"`
	// Revoked is a device this machine threw out. It stays on the list so that
	// "I revoked it" is visible rather than a row quietly disappearing.
	Revoked   bool  `json:"revoked,omitempty"`
	RevokedAt int64 `json:"revoked_at,omitempty"`
}

// StateOff is the state of a link whose switch is off. It is deliberately not
// one of the transport's four: `idle` means a line that is trying, and a line
// nobody asked for is not trying.
const StateOff = "off"

// LinkOptions is everything the wiring needs from the daemon.
type LinkOptions struct {
	// Dir is this daemon's own state directory. Keys, the identity record and
	// the sequence fence live under it.
	Dir string
	// ForeignDirs are directories this link must never read or write — the
	// Swift app's, by every name it goes by.
	ForeignDirs []string
	// Handler is the daemon's own routes, gate and all. A Cloud request is
	// answered by exactly the handler a paired browser on this machine's own
	// network reaches.
	Handler http.Handler
	// Authorize stamps the credential the gate judges an in-process request by.
	Authorize func(*http.Request)
	// Version is this build, for the machine registration record.
	Version string
	// Log is one line per material event. Nil is silence.
	Log func(format string, args ...any)
	// Now exists so a test can drive the clock.
	Now func() time.Time
}

// Link is one machine's Cloud line.
type Link struct {
	opts     LinkOptions
	file     *nextconfig.File
	settings adaptercloud.Settings

	identity adaptercloud.Identity
	keys     *cloudkeys.Files
	deviceID string
	pinned   *adaptercloud.PinnedStore
	pairing  *Pairing

	transport *adaptercloud.Transport
	relay     *Relay
	publisher *Publisher
	roster    *adaptercloud.Roster
	status    *adaptercloud.StatusRecorder
	service   Service

	mu          sync.Mutex
	state       string
	lastErr     string
	lastErrAt   time.Time
	answered    int
	refused     int
	fingerprint string
	// stopRun cancels the socket currently up, and rotated says the next
	// `Run` iteration should rebuild rather than return. They are one pair:
	// a cancel with no flag is a shutdown, a cancel with it is a rotation.
	stopRun context.CancelFunc
	rotated bool
}

// ErrDisabledLink is Run's answer for a link whose switch is off. It is not a
// failure: it is the daemon reporting that it did what the settings said.
var ErrDisabledLink = errors.New("the cloud line is off in this app's settings")

// Open reads the settings and, when the switch is on, everything the line
// needs. It connects nothing; Run does that.
//
// A link is returned even when the switch is off or the machine was never
// enrolled, because the status route has to be able to say which of those it
// is. The error return is for a settings file that cannot be read or is
// malformed — a relay URL with a typo stops the line and says so rather than
// falling back to the production relay.
func Open(opts LinkOptions) (*Link, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	file := nextconfig.Open(opts.Dir, opts.ForeignDirs...)
	settings, err := adaptercloud.ReadSettings(file)
	if err != nil {
		return nil, err
	}
	link := &Link{opts: opts, file: file, settings: settings, state: StateOff}
	if !settings.Enabled {
		return link, nil
	}
	// Past this line the switch is on, so key material may be opened.
	keys, err := cloudkeys.Open(opts.Dir, opts.ForeignDirs...)
	if err != nil {
		link.fail(err)
		return link, nil
	}
	link.keys = keys
	identity, found, err := adaptercloud.NewIdentityStore(keys.Dir()).Load()
	switch {
	case err != nil:
		// An identity that exists and cannot be read is reported as itself.
		// Saying "not enrolled" here is how a permission error turns into a
		// second machine registration.
		link.fail(err)
		return link, nil
	case !found:
		link.fail(adaptercloud.ErrNoIdentity)
		return link, nil
	}
	if identity.APIBase != "" && identity.APIBase != settings.APIBase {
		link.fail(errors.New("this machine registered with " + identity.APIBase +
			", the settings say " + settings.APIBase))
		return link, nil
	}
	link.identity = identity
	link.pinned = adaptercloud.NewPinnedStore(keys.Dir())
	link.pairing = &Pairing{
		AccountID:  identity.AccountID,
		MachineID:  identity.MachineID,
		Credential: identity.MachineCredential,
		AppOrigin:  settings.AppOrigin,
		Client:     adaptercloud.NewAccountClient(settings.APIBase),
		// Read from the store rather than captured, so that a rotation
		// between two pairings cannot hand the second browser the first
		// browser's key.
		Keys:   link.pairingKeys,
		Pinned: link.pinned,
		Now:    opts.Now,
		Log:    opts.Log,
	}
	if err := link.wire(); err != nil {
		link.fail(err)
		return link, nil
	}
	link.state = adaptercloud.StateIdle
	return link, nil
}

// wire builds everything downstream of the keys: the spool, the transport, the
// relay, the bridge and the publisher.
//
// It is a method rather than the rest of `Open` because it is called twice: at
// startup, and again after a key rotation. A rotated signing key cannot be
// swapped into a live transport — the relay judges every envelope's signature
// against the `pk` in the device token, and a socket that changed keys halfway
// would sign the second half with a key the relay has stopped accepting — so
// rotating means building this again and reconnecting.
//
// The spool is rebuilt with it, and that is the deliberate part: envelopes
// queued under the old key are dropped rather than sent, because they would be
// refused on arrival and would spend a sequence number doing it.
func (l *Link) wire() error {
	keys, identity, settings, opts := l.keys, l.identity, l.settings, l.opts

	key, err := domaincloud.LoadOrCreateDeviceKey(keys, rand.Reader)
	if err != nil {
		return err
	}
	secret, err := domaincloud.LoadOrCreateMasterSecret(keys, rand.Reader)
	if err != nil {
		return err
	}
	l.mu.Lock()
	l.fingerprint = key.Fingerprint()
	l.mu.Unlock()

	fence := adaptercloud.NewFileFence(keys.Dir(), identity.MachineID)
	spool, err := adaptercloud.NewSpool(adaptercloud.DefaultSpoolLimits(), fence, opts.Now)
	if err != nil {
		return err
	}

	status := adaptercloud.NewStatusRecorder(opts.Now())
	status.SetEnabled(true)
	status.SetIdentity(identity.AccountID, identity.MachineID, settings.RelayURL)
	l.status = status

	if l.roster == nil {
		l.roster = adaptercloud.NewRoster(settings.APIBase, identity.MachineCredential, opts.Now)
	}
	l.relay = &Relay{
		Spool:     spool,
		MachineID: identity.MachineID,
		Signer:    key,
		Secret:    secret,
		KeyID:     adaptercloud.MasterKeyID,
		Now:       opts.Now,
		Log:       opts.Log,
	}

	transport, err := adaptercloud.New(adaptercloud.Options{
		RelayURL: settings.RelayURL,
		Role:     "machine",
		Token: &adaptercloud.TokenSource{
			Client:     adaptercloud.NewAccountClient(settings.APIBase),
			Credential: identity.MachineCredential,
		},
		Identity:     identity,
		Signer:       key,
		Spool:        spool,
		Status:       status,
		Replay:       domaincloud.NewReplayWindow(0),
		Inbound:      l.relay.Deliver,
		PublicKeyFor: l.publicKeyFor,
		ContentKey:   secret,
		Log:          func(line string) { l.logf("%s", line) },
		Now:          opts.Now,
	})
	if err != nil {
		return err
	}
	l.transport = transport
	l.relay.Transport = transport
	l.service = Service{
		MachineID: identity.MachineID,
		Bridge: cloudops.Bridge{
			MachineID:     identity.MachineID,
			Router:        Router{Handler: opts.Handler, Authorize: opts.Authorize},
			AllowCommands: l.allowCommands,
			Authority:     l.authority,
		},
		Transport: l.relay,
		Log:       opts.Log,
	}
	l.publisher = &Publisher{
		MachineID:   identity.MachineID,
		MachineName: machineName(identity, settings),
		Platform:    runtime.GOOS,
		Version:     opts.Version,
		Router:      Router{Handler: opts.Handler, Authorize: opts.Authorize},
		Publish:     l.relay.Publish,
		Log:         opts.Log,
	}
	return nil
}

// machineName is what a person picks this Mac out by in their machine list.
// The identity's name is what was registered; the settings key overrides it
// without re-registering, because renaming a machine should not need the
// control plane.
func machineName(identity adaptercloud.Identity, settings adaptercloud.Settings) string {
	if settings.MachineName != "" {
		return settings.MachineName
	}
	if identity.Name != "" {
		return identity.Name
	}
	return "Mac"
}

// Enabled reports whether the switch was on when this link was opened.
func (l *Link) Enabled() bool { return l.settings.Enabled }

// Run holds the line up until ctx is done.
//
// Two goroutines and no more: the transport owns the socket and the service
// owns the answers. The service stops when the transport's request channel
// closes, which is what makes a cancelled context end both.
func (l *Link) Run(ctx context.Context) error {
	if !l.settings.Enabled {
		return ErrDisabledLink
	}
	for {
		if l.transport == nil {
			return errors.New("the cloud line could not be opened: " + l.lastError())
		}
		inner, stop := context.WithCancel(ctx)
		l.mu.Lock()
		l.stopRun = stop
		l.mu.Unlock()
		err := l.runOnce(inner)
		stop()
		l.mu.Lock()
		l.stopRun = nil
		rotated := l.rotated
		l.rotated = false
		l.mu.Unlock()
		if ctx.Err() != nil || !rotated {
			return err
		}
		// A rotation took the socket down on purpose. Rebuild on the key the
		// store now holds and go back up; anything else would leave a machine
		// signing with a key the relay has stopped accepting.
		if err := l.wire(); err != nil {
			l.fail(err)
			return err
		}
		l.setState(adaptercloud.StateIdle)
		l.logf("cloud: the line is coming back on the rotated key %s", l.Fingerprint())
	}
}

// runOnce holds one socket up until its context is done.
func (l *Link) runOnce(ctx context.Context) error {
	// The roster is fetched once before the socket opens, so that a viewer's
	// first envelope is not dropped as `unknown_sender` while the first lookup
	// is still in flight.
	if err := l.roster.Refresh(ctx); err != nil {
		l.logf("cloud: the device roster could not be read: %v", err)
		l.fail(err)
	}

	answers := make(chan struct{})
	go func() {
		defer close(answers)
		_ = l.runService(ctx)
	}()
	// The roster on a clock as well as on demand. `PublicKeyFor` refreshes a
	// stale roster when an envelope arrives, which is enough to admit a new
	// viewer — but a machine that has received nothing all morning would then
	// report a roster from this morning, and the status route is read by a
	// person asking "does this Mac know about my phone yet".
	go l.refreshRoster(ctx)
	// The snapshots this machine publishes without being asked. They start
	// only once the first handshake has completed: a snapshot reserved before
	// the socket is up spends a sequence the relay has not seen a connection
	// for, and the spool would hold it until the line came up anyway.
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-l.transport.Ready():
		}
		_ = l.publisher.Run(ctx)
	}()

	err := l.transport.Run(ctx)
	// Closing the queue ends Service.Run. It is closed here rather than by the
	// transport because this is the only place that knows the transport will
	// deliver nothing more.
	l.relay.Close()
	<-answers
	if err != nil && !errors.Is(err, context.Canceled) {
		l.fail(err)
	}
	return err
}

// refreshRoster re-reads the account's viewer devices until ctx is done.
func (l *Link) refreshRoster(ctx context.Context) {
	ticker := time.NewTicker(adaptercloud.RosterRefresh)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := l.roster.Refresh(ctx); err != nil {
				l.logf("cloud: the device roster could not be read: %v", err)
			}
		}
	}
}

// runService answers requests, counting what it answered.
func (l *Link) runService(ctx context.Context) error {
	requests := l.relay.Requests()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case request, open := <-requests:
			if !open {
				return nil
			}
			answer := l.service.Answer(ctx, request)
			l.mu.Lock()
			l.answered++
			if !answer.OK() {
				l.refused++
			}
			l.mu.Unlock()
		}
	}
}

// allowCommands reads the write switch from the file, now.
//
// From the file rather than from the reading taken at start: a person who turns
// writes off wants the request in flight refused too, and a value captured at
// startup would keep saying yes until the daemon restarted. A file that cannot
// be read is a no — an unreadable switch is not an open one.
func (l *Link) allowCommands() bool {
	settings, err := adaptercloud.ReadSettings(l.file)
	if err != nil {
		return false
	}
	return settings.Commands
}

// publicKeyFor is who this machine will verify an envelope against.
//
// The order is the whole point, and it is PROTOCOL.md §3's "held locally, not
// trusted from the cloud":
//
//  1. **A device this machine threw out is refused**, whatever anything else
//     says. A local revocation that the account's roster could undo would not
//     be a revocation.
//  2. **A pin wins.** The key came out of a sealed offer this machine opened
//     itself; the control plane never saw it in the clear and so cannot
//     substitute one.
//  3. **The roster is the fallback**, for a viewer paired before the pin file
//     existed. It is the fourth wave's behaviour, kept rather than removed so
//     that a browser that already works does not stop working — and it is the
//     weaker answer, which is why the settings page marks which rows are pins.
//
// An unreadable pin file refuses everybody rather than falling through, for
// the reason `pinned.go` gives: falling through is exactly the substitution
// the pins exist against.
func (l *Link) publicKeyFor(sender string) (ed25519.PublicKey, bool) {
	if l.pinned != nil {
		refused, err := l.pinned.Refused(sender)
		if err != nil {
			l.logf("cloud: the paired-device file cannot be read, so no sender is admitted: %v", err)
			return nil, false
		}
		if refused {
			return nil, false
		}
		if key, ok, err := l.pinned.PublicKeyFor(sender); err != nil {
			l.logf("cloud: the paired-device file cannot be read, so no sender is admitted: %v", err)
			return nil, false
		} else if ok {
			return key, true
		}
	}
	if l.roster == nil {
		return nil, false
	}
	return l.roster.PublicKeyFor(sender)
}

// Pairing is the handover this link drives, for the routes that start one.
func (l *Link) Pairing() *Pairing { return l.pairing }

// pairingKeys reads the two keys a handover carries, from the store, now.
func (l *Link) pairingKeys() (domaincloud.DeviceKey, domaincloud.ContentKey, error) {
	if l.keys == nil {
		return domaincloud.DeviceKey{}, domaincloud.ContentKey{}, ErrPairingUnavailable
	}
	key, err := domaincloud.LoadOrCreateDeviceKey(l.keys, rand.Reader)
	if err != nil {
		return domaincloud.DeviceKey{}, domaincloud.ContentKey{}, err
	}
	secret, err := domaincloud.LoadOrCreateMasterSecret(l.keys, rand.Reader)
	if err != nil {
		return domaincloud.DeviceKey{}, domaincloud.ContentKey{}, err
	}
	return key, secret, nil
}

// authority is cloudops' re-read at the point of no return.
//
// The roster is the account's, not this machine's: a device the person removed
// in the hosted console stops being able to act here at the next refresh. The
// capability check is the same borrowing — a viewer enrolled without
// `send_prompt` may read this Mac and may not type into it, and that is the
// account's decision rather than one this file makes up.
func (l *Link) authority(ctx context.Context, sender string, requiresWriteGate bool) cloudops.Authority {
	readable, _ := l.roster.Readable()
	if l.pinned != nil {
		if refused, err := l.pinned.Refused(sender); err != nil || refused {
			// A device this machine threw out, or a pin file it cannot read.
			// Either way the answer is no, and it is no before the roster is
			// consulted: the roster is the thing a revocation has to beat.
			return cloudops.Authority{ClockReady: true, RosterReadable: readable}
		}
	}
	a := cloudops.Authority{
		// The clock guard is not wired yet; the transport admits on the relay's
		// own timestamp window today. Saying `true` here is reporting what is
		// actually checked rather than claiming a check that does not run —
		// see docs/cloud-wire.md §16.4.
		ClockReady:     true,
		RosterReadable: readable,
	}
	if !readable {
		return a
	}
	for _, device := range l.roster.Devices() {
		if device.ID != sender {
			continue
		}
		if device.RevokedAt != nil && *device.RevokedAt != "" {
			return a
		}
		a.RosterAllowsSender = true
		if !requiresWriteGate {
			a.WriteGateAllows = true
			return a
		}
		a.WriteGateAllows = l.allowCommands() && hasAny(device.Caps, SendCapability, StartCapability)
		return a
	}
	return a
}

func hasAny(have []string, want ...string) bool {
	for _, item := range have {
		for _, candidate := range want {
			if item == candidate {
				return true
			}
		}
	}
	return false
}

// Status is what the status route answers.
func (l *Link) Status() Status {
	l.mu.Lock()
	out := Status{
		Enabled:     l.settings.Enabled,
		State:       l.state,
		RelayURL:    l.settings.RelayURL,
		APIBase:     l.settings.APIBase,
		Account:     l.identity.AccountID,
		MachineID:   l.identity.MachineID,
		MachineName: l.identity.Name,
		Fingerprint: l.fingerprint,
		LastError:   l.lastErr,
		Answered:    l.answered,
		Refused:     l.refused,
	}
	if !l.lastErrAt.IsZero() {
		out.LastErrorAt = l.lastErrAt.Unix()
	}
	l.mu.Unlock()

	out.Configured = l.identity.Valid()
	out.Commands = l.allowCommands()
	out.Commandset = cloudops.Implemented()
	if l.relay != nil {
		out.QueueDropped = l.relay.Dropped()
	}
	// The viewer list is the two sources joined, in the order that decides
	// admission: this machine's own pins, then the account's roster for
	// anything it has never pinned. A row that is only in the roster is shown
	// as such rather than silently equated with a pin.
	seen := map[string]int{}
	if l.pinned != nil {
		devices, err := l.pinned.Devices()
		out.PinnedReadable = err == nil
		if err != nil {
			out.PinnedError = err.Error()
		}
		for _, device := range devices {
			seen[device.DeviceID] = len(out.Devices)
			out.Devices = append(out.Devices, Viewer{
				ID: device.DeviceID, Name: device.Name, Fingerprint: device.Fingerprint,
				Pinned: true, PairedAt: device.PairedAt,
				Revoked: !device.Active(), RevokedAt: device.RevokedAt,
			})
		}
	}
	if l.roster != nil {
		readable, _ := l.roster.Readable()
		out.RosterReadable = readable
		for _, device := range l.roster.Devices() {
			if device.RevokedAt != nil && *device.RevokedAt != "" {
				continue
			}
			if at, ok := seen[device.ID]; ok {
				// The account knows this device's kind, name and capabilities;
				// the pin knows its key. Both belong on the one row.
				out.Devices[at].Kind = device.Kind
				out.Devices[at].Caps = device.Caps
				if device.Name != "" {
					out.Devices[at].Name = device.Name
				}
				continue
			}
			out.Devices = append(out.Devices, Viewer{
				ID: device.ID, Kind: device.Kind, Name: device.Name,
				Caps: device.Caps, Fingerprint: device.Fingerprint,
			})
		}
	}
	if l.pairing != nil {
		out.Pairing = l.pairing.State()
	} else {
		out.Pairing = PairingState{Phase: PairingIdle}
	}
	if l.transport != nil {
		out.State = l.transport.State()
		snapshot := l.transport.Status()
		out.LastClose = snapshot.LastClose
		out.Connects, out.Reconnects = snapshot.Connects, snapshot.Reconnects
		out.Published, out.Acked, out.PublishErrors = snapshot.Published, snapshot.Acked, snapshot.PublishErrors
		out.InboundTotal, out.InboundDropped = snapshot.InboundTotal, snapshot.InboundDropped
		if !snapshot.ConnectedSince.IsZero() {
			out.ConnectedSince = snapshot.ConnectedSince.Unix()
		}
		if !snapshot.TokenExpiresAt.IsZero() {
			out.TokenExpiresAt = snapshot.TokenExpiresAt.Unix()
		}
	}
	return out
}

// fail records the most recent thing that went wrong.
func (l *Link) fail(err error) {
	if err == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lastErr, l.lastErrAt = err.Error(), l.opts.Now()
}

func (l *Link) lastError() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lastErr == "" {
		return "no reason was recorded"
	}
	return l.lastErr
}

func (l *Link) logf(format string, args ...any) {
	if l.opts.Log != nil {
		l.opts.Log(format, args...)
	}
}

// LocalAuthorizer stamps a request with this machine's own credentials.
//
// **One permission model, not two.** The router dispatches in-process, so the
// request reaches the same gate, the same capability checks and the same
// refusal words a paired browser on this machine's own network would. What a
// Cloud viewer may ask for is decided before this point — by `cloudops`'
// closed vocabulary, by the account's roster, and by the write switch — so the
// credential here only has to be one the gate accepts for the routes that
// vocabulary names. Minting a narrower device credential of its own would be a
// second permission model, and the second one is the one that goes wrong.
//
// The orchestrator header is carried too, because `/v1/orchestrator/*` and
// `/v1/board` are machine-scoped and a device token is not accepted there. It
// is a different credential for a different door, which is why it is a
// different header rather than the same bearer.
func LocalAuthorizer(localToken, machineToken string) func(*http.Request) {
	return func(r *http.Request) {
		if localToken != "" {
			r.Header.Set("Authorization", "Bearer "+localToken)
		}
		if machineToken != "" && machineScoped(r.URL.Path) {
			r.Header.Set("X-Clawdline-Orchestrator", machineToken)
		}
		// A change must say where it came from, and an in-process request
		// truthfully comes from this daemon itself.
		r.Header.Set("Origin", "http://127.0.0.1")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	}
}

// machineScoped is the gate's own list of paths where the orchestrator
// credential is accepted, narrowed to what cloudops can ask for. It is spelled
// here rather than imported because the gate's copy is unexported, and it is
// deliberately the smaller list: a path this does not name simply goes out with
// the device credential, which is the one the route wants.
func machineScoped(path string) bool {
	if strings.HasPrefix(path, "/v1/orchestrator/") {
		return true
	}
	return path == "/v1/board"
}

// MARK: rotation

// RotationOutcome is what a key rotation did, and what it cost.
type RotationOutcome struct {
	// Fingerprint is the new key, as a person reads it aloud.
	Fingerprint string `json:"fingerprint"`
	// Previous is the key that was replaced.
	Previous string `json:"previous_fingerprint"`
	// KeyEpoch is what the control plane now judges tokens by.
	KeyEpoch      int `json:"key_epoch"`
	IdentityEpoch int `json:"identity_epoch"`
	// Repair names the viewers that pinned the old key and must be paired
	// again. It is the whole reason this operation asks before it acts.
	Repair []string `json:"repair,omitempty"`
	// Reconnected is whether the live line was taken down and brought back on
	// the new key. False means the line was not running, so nothing had to be.
	Reconnected bool `json:"reconnected"`
}

// ErrRotationUnconfirmed is a rotation nobody agreed to the cost of.
var ErrRotationUnconfirmed = errors.New("rotating this machine's signing key makes every paired browser re-pair")

// RotationCost answers what a rotation would break, without doing it.
//
// It exists so that "are you sure" is a sentence with names in it rather than a
// generic warning: the person is being asked to make *these* browsers stop
// working until they pair again.
func (l *Link) RotationCost() []string {
	if l.pinned == nil {
		return nil
	}
	devices, err := l.pinned.Devices()
	if err != nil {
		return nil
	}
	var out []string
	for _, device := range devices {
		if !device.Active() {
			continue
		}
		name := device.Fingerprint
		if device.DeviceID != "" {
			name = device.DeviceID + " (" + device.Fingerprint + ")"
		}
		out = append(out, name)
	}
	return out
}

// RotateSigningKey replaces this machine's Ed25519 identity.
//
// **Every browser that pinned the old key stops being able to verify this
// machine.** They pinned it during their own handover, from bytes the cloud
// never saw; that is the property the whole pairing exists for, and its price
// is that a new key means a new pairing. So this refuses unless `confirm` is
// true, and it answers the list of viewers that will need repairing either way.
//
// The order is: read the epoch the control plane holds, mint, compare-and-swap,
// and only then write the new key to the store. A store written first would,
// on a refused swap, leave a machine holding a key the account has never heard
// of — which looks exactly like a machine that was revoked.
func (l *Link) RotateSigningKey(ctx context.Context, confirm bool) (RotationOutcome, error) {
	if l.keys == nil || !l.identity.Valid() {
		return RotationOutcome{}, ErrPairingUnavailable
	}
	cost := l.RotationCost()
	if !confirm {
		return RotationOutcome{Repair: cost}, ErrRotationUnconfirmed
	}
	client := adaptercloud.NewAccountClient(l.settings.APIBase)
	machines, err := client.Machines(ctx, l.identity.MachineCredential)
	if err != nil {
		return RotationOutcome{Repair: cost}, err
	}
	epoch := 0
	for _, machine := range machines {
		if machine.ID == l.identity.MachineID {
			epoch = machine.KeyEpoch
		}
	}
	if epoch < 1 {
		// Not a detail to paper over with a 1: the swap is compare-and-swap,
		// and guessing the value it compares against is how two daemons
		// overwrite each other's identity.
		return RotationOutcome{Repair: cost}, fmt.Errorf(
			"the control plane does not list %s, so there is no key epoch to rotate from", l.identity.MachineID)
	}
	previous := l.Fingerprint()

	fresh, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		return RotationOutcome{Repair: cost}, err
	}
	rotated, err := client.RotateMachineKey(ctx, l.identity.MachineCredential,
		l.identity.MachineID, fresh.PublicKey(), fresh.Fingerprint(), epoch)
	if err != nil {
		return RotationOutcome{Repair: cost}, err
	}
	if err := l.keys.SaveDeviceKey(fresh); err != nil {
		// The account already moved. Saying so is the only useful thing left:
		// this machine now holds a key the control plane has replaced, and the
		// person has to be told rather than left to read it as a revocation.
		return RotationOutcome{Repair: cost}, fmt.Errorf(
			"the control plane accepted the new key but this machine could not store it, "+
				"so it is now signing with a key the account has replaced: %w", err)
	}
	l.logf("cloud: this machine's signing key is now %s (was %s)", fresh.Fingerprint(), previous)

	outcome := RotationOutcome{
		Fingerprint:   fresh.Fingerprint(),
		Previous:      previous,
		KeyEpoch:      rotated.KeyEpoch,
		IdentityEpoch: rotated.IdentityEpoch,
		Repair:        cost,
	}
	l.mu.Lock()
	stop := l.stopRun
	if stop != nil {
		l.rotated = true
	}
	l.mu.Unlock()
	if stop != nil {
		outcome.Reconnected = true
		stop()
	}
	return outcome, nil
}

// setState records the line's own word for what it is doing.
func (l *Link) setState(state string) {
	l.mu.Lock()
	l.state = state
	l.mu.Unlock()
}

// Fingerprint is this machine's signing key, as a person reads it aloud.
func (l *Link) Fingerprint() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fingerprint
}
