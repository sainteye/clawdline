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
	"crypto/rand"
	"errors"
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
}

// Viewer is one enrolled device, as the status route shows it. It carries no
// key material: a fingerprint is what a person compares, a public key is not.
type Viewer struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind,omitempty"`
	Name        string   `json:"name,omitempty"`
	Caps        []string `json:"caps,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
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

	key, err := domaincloud.LoadOrCreateDeviceKey(keys, rand.Reader)
	if err != nil {
		link.fail(err)
		return link, nil
	}
	secret, err := domaincloud.LoadOrCreateMasterSecret(keys, rand.Reader)
	if err != nil {
		link.fail(err)
		return link, nil
	}
	link.fingerprint = key.Fingerprint()

	fence := adaptercloud.NewFileFence(keys.Dir(), identity.MachineID)
	spool, err := adaptercloud.NewSpool(adaptercloud.DefaultSpoolLimits(), fence, opts.Now)
	if err != nil {
		link.fail(err)
		return link, nil
	}

	status := adaptercloud.NewStatusRecorder(opts.Now())
	status.SetEnabled(true)
	status.SetIdentity(identity.AccountID, identity.MachineID, settings.RelayURL)
	link.status = status

	link.roster = adaptercloud.NewRoster(settings.APIBase, identity.MachineCredential, opts.Now)
	link.relay = &Relay{
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
		Inbound:      link.relay.Deliver,
		PublicKeyFor: link.roster.PublicKeyFor,
		ContentKey:   secret,
		Log:          func(line string) { link.logf("%s", line) },
		Now:          opts.Now,
	})
	if err != nil {
		link.fail(err)
		return link, nil
	}
	link.transport = transport
	link.relay.Transport = transport
	link.service = Service{
		MachineID: identity.MachineID,
		Bridge: cloudops.Bridge{
			MachineID:     identity.MachineID,
			Router:        Router{Handler: opts.Handler, Authorize: opts.Authorize},
			AllowCommands: link.allowCommands,
			Authority:     link.authority,
		},
		Transport: link.relay,
		Log:       opts.Log,
	}
	link.publisher = &Publisher{
		MachineID:   identity.MachineID,
		MachineName: machineName(identity, settings),
		Platform:    runtime.GOOS,
		Version:     opts.Version,
		Router:      Router{Handler: opts.Handler, Authorize: opts.Authorize},
		Publish:     link.relay.Publish,
		Log:         opts.Log,
	}
	link.state = adaptercloud.StateIdle
	return link, nil
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
	if l.transport == nil {
		return errors.New("the cloud line could not be opened: " + l.lastError())
	}
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

// authority is cloudops' re-read at the point of no return.
//
// The roster is the account's, not this machine's: a device the person removed
// in the hosted console stops being able to act here at the next refresh. The
// capability check is the same borrowing — a viewer enrolled without
// `send_prompt` may read this Mac and may not type into it, and that is the
// account's decision rather than one this file makes up.
func (l *Link) authority(ctx context.Context, sender string, requiresWriteGate bool) cloudops.Authority {
	readable, _ := l.roster.Readable()
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
	if l.roster != nil {
		readable, _ := l.roster.Readable()
		out.RosterReadable = readable
		for _, device := range l.roster.Devices() {
			if device.RevokedAt != nil && *device.RevokedAt != "" {
				continue
			}
			out.Devices = append(out.Devices, Viewer{
				ID: device.ID, Kind: device.Kind, Name: device.Name,
				Caps: device.Caps, Fingerprint: device.Fingerprint,
			})
		}
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
