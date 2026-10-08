package cloud

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	domain "github.com/sainteye/clawdline/internal/domain/cloud"
)

var (
	ErrViewerNotPaired         = errors.New("viewer_machine_not_paired")
	ErrViewerRevoked           = errors.New("viewer_machine_revoked")
	ErrViewerOffline           = errors.New("viewer_machine_offline")
	ErrViewerStatusUnavailable = errors.New("viewer_status_unavailable")
)

type viewerActivePin struct {
	pin     ViewerMachinePin
	public  ed25519.PublicKey
	content domain.ContentKey
}

// ViewerClient owns one local viewer relay line. The caller keeps it in the
// daemon, so its signing sequence and replay window have one process owner.
// Both the local Console and CLI address this same service after local auth.
type ViewerClient struct {
	identity   ViewerIdentity
	settings   Settings
	account    *AccountClient
	transport  *Transport
	status     *ViewerStatusStore
	update     chan struct{}
	fence      SequenceFence
	signer     domain.DeviceKey
	sequence   uint64
	seqMu      sync.Mutex
	pendingMu  sync.Mutex
	pending    map[string]*viewerPending
	pendingSeq map[uint64]*viewerPending
	richMu     sync.Mutex
	rich       map[string]viewerRich
	richActive map[string]bool
	richUpdate chan struct{}
	pinsMu     sync.RWMutex
	configured map[string]viewerActivePin
	pins       map[string]viewerActivePin
	denied     map[string]error
}

type ViewerClientOptions struct {
	Settings Settings
	Identity ViewerIdentity
	Signer   domain.DeviceKey
	Fence    SequenceFence
	Account  *AccountClient
	Pins     map[string]ViewerMachinePin
}

func NewViewerClient(opts ViewerClientOptions) (*ViewerClient, error) {
	if !opts.Settings.Enabled {
		return nil, ErrDisabled
	}
	if !opts.Identity.Valid() || opts.Identity.APIBase != opts.Settings.APIBase || !opts.Signer.Valid() || opts.Fence == nil {
		return nil, ErrNoIdentity
	}
	if opts.Account == nil {
		opts.Account = NewAccountClient(opts.Settings.APIBase)
	}
	ceiling, err := opts.Fence.Ceiling()
	if err != nil {
		return nil, err
	}
	client := &ViewerClient{identity: opts.Identity, settings: opts.Settings, account: opts.Account,
		status: NewViewerStatusStore(), update: make(chan struct{}, 1), fence: opts.Fence,
		signer: opts.Signer, sequence: ceiling, configured: make(map[string]viewerActivePin),
		pins: make(map[string]viewerActivePin), denied: make(map[string]error),
		pending: make(map[string]*viewerPending), pendingSeq: make(map[uint64]*viewerPending)}
	client.rich = make(map[string]viewerRich)
	client.richActive = make(map[string]bool)
	client.richUpdate = make(chan struct{}, 1)
	for id, pin := range opts.Pins {
		if id != pin.MachineID || !validViewerPin(pin) {
			return nil, errors.New("invalid viewer machine pin")
		}
		public, _ := base64.StdEncoding.DecodeString(pin.MachineSigningKey)
		secret, _ := base64.StdEncoding.DecodeString(pin.MasterSecret)
		content, err := domain.ContentKeyFromBytes(secret)
		if err != nil {
			return nil, err
		}
		client.configured[id] = viewerActivePin{pin: pin, public: ed25519.PublicKey(public), content: content}
	}
	line, err := New(Options{
		RelayURL: opts.Settings.RelayURL, Role: "viewer",
		Token: &TokenSource{Client: opts.Account, Credential: opts.Identity.ViewerCredential},
		Identity: Identity{AccountID: opts.Identity.AccountID, MachineID: opts.Identity.DeviceID,
			MachineCredential: opts.Identity.ViewerCredential, APIBase: opts.Identity.APIBase},
		Signer: opts.Signer, Replay: domain.NewReplayWindow(0),
		PublicKeyFor: client.publicKeyFor, ContentKeyFor: client.contentKeyFor,
		Inbound: client.inbound, OnAck: client.onAck, OnPublishRefusal: client.onPublishRefusal,
		OnDisconnect: client.onDisconnect,
	})
	if err != nil {
		return nil, err
	}
	client.transport = line
	return client, nil
}

func (c *ViewerClient) publicKeyFor(sender string) (ed25519.PublicKey, bool) {
	c.pinsMu.RLock()
	defer c.pinsMu.RUnlock()
	pin, ok := c.pins[sender]
	return pin.public, ok
}

func (c *ViewerClient) contentKeyFor(sender, keyID string) (domain.ContentKey, bool) {
	c.pinsMu.RLock()
	defer c.pinsMu.RUnlock()
	pin, ok := c.pins[sender]
	return pin.content, ok && pin.pin.KeyID == keyID
}

func (c *ViewerClient) inbound(envelope domain.Envelope, plaintext []byte, _ ed25519.PublicKey) {
	if c.status.Apply(envelope, plaintext) {
		select {
		case c.update <- struct{}{}:
		default:
		}
	}
	c.acceptReply(envelope, plaintext)
	c.acceptRich(envelope, plaintext)
}

func (c *ViewerClient) Run(ctx context.Context) error { return c.transport.Run(ctx) }
func (c *ViewerClient) Stop()                         { c.transport.Stop() }

func (c *ViewerClient) WaitReady(ctx context.Context) error {
	for {
		if c.transport.State() == StateConnected {
			return nil
		}
		ready := c.transport.Ready()
		if c.transport.State() == StateConnected {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ready:
		}
	}
}

// RefreshMachinePins checks the current account roster before any cached
// status can be exposed. Removed or changed keys immediately lose local use.
func (c *ViewerClient) RefreshMachinePins(ctx context.Context) error {
	machines, err := c.account.Machines(ctx, c.identity.ViewerCredential)
	if err != nil {
		c.pinsMu.Lock()
		c.pins = map[string]viewerActivePin{}
		c.denied = map[string]error{}
		c.pinsMu.Unlock()
		return err
	}
	roster := make(map[string]MachineRecord, len(machines))
	for _, machine := range machines {
		roster[machine.ID] = machine
	}
	c.pinsMu.Lock()
	active := make(map[string]viewerActivePin)
	denied := make(map[string]error)
	for id, pin := range c.configured {
		machine, ok := roster[id]
		if ok && machine.RevokedAt == nil && machine.PublicKey == pin.pin.MachineSigningKey &&
			machine.KeyFingerprint == pin.pin.MachineFingerprint {
			active[id] = pin
		} else {
			if !ok || machine.RevokedAt != nil {
				denied[id] = ErrViewerRevoked
			} else {
				denied[id] = ErrViewerNotPaired
			}
			c.status.Forget(id)
		}
	}
	c.pins = active
	c.denied = denied
	c.pinsMu.Unlock()
	return nil
}

func (c *ViewerClient) Paired(machineID string) bool {
	c.pinsMu.RLock()
	defer c.pinsMu.RUnlock()
	_, ok := c.pins[machineID]
	return ok
}

type ViewerMachineState struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	Pairing     string `json:"pairing"`
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
}

// MachineStates exposes only roster metadata and status, never handover keys.
func (c *ViewerClient) MachineStates(ctx context.Context) ([]ViewerMachineState, error) {
	if err := c.RefreshMachinePins(ctx); err != nil {
		return nil, err
	}
	records, err := c.account.Machines(ctx, c.identity.ViewerCredential)
	if err != nil {
		return nil, err
	}
	out := make([]ViewerMachineState, 0, len(records))
	for _, record := range records {
		state := ViewerMachineState{ID: record.ID, Name: record.Name, Fingerprint: record.KeyFingerprint,
			Pairing: "unpaired", Status: "unavailable", Reason: "not_paired"}
		if record.RevokedAt != nil {
			state.Pairing, state.Reason = "revoked", "revoked"
		} else if c.Paired(record.ID) {
			state.Pairing = "paired"
			projection := c.status.Project(record.ID, time.Now())
			state.Status, state.Reason = projection.Kind, projection.Reason
			if c.transport.State() != StateConnected {
				state.Status, state.Reason = "unavailable", "offline"
			}
		}
		out = append(out, state)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (c *ViewerClient) pairingRefusal(machineID string) error {
	c.pinsMu.RLock()
	defer c.pinsMu.RUnlock()
	if refusal := c.denied[machineID]; refusal != nil {
		return refusal
	}
	return ErrViewerNotPaired
}

// ReadMachine waits for the authenticated inventory marker and all rows. It
// does not translate absence into an empty list, and never reads rich s/ rows.
func (c *ViewerClient) ReadMachine(ctx context.Context, machineID string) (ViewerProjection, error) {
	if err := c.RefreshMachinePins(ctx); err != nil {
		return ViewerProjection{Kind: "unavailable", Reason: "no_permission"}, err
	}
	if !c.Paired(machineID) {
		return ViewerProjection{Kind: "unavailable", Reason: "no_permission"}, c.pairingRefusal(machineID)
	}
	if err := c.WaitReady(ctx); err != nil {
		return ViewerProjection{Kind: "unavailable", Reason: "offline"}, err
	}
	recovering := ""
	defer func() {
		if recovering != "" {
			_ = c.transport.Unsubscribe(recovering)
		}
	}()
	for {
		if c.transport.State() != StateConnected {
			return ViewerProjection{Kind: "unavailable", Reason: "offline"}, ErrViewerOffline
		}
		projection := c.status.Project(machineID, time.Now())
		if projection.Kind == "ready" || projection.Reason != "unknown" && projection.Reason != "event_gap" {
			return projection, nil
		}
		if projection.Reason == "event_gap" {
			if sessionID, _, ok := c.status.GapTarget(machineID); ok {
				next := "ss/" + viewerChannelSegment(machineID) + "/" + viewerChannelSegment(sessionID)
				if next != recovering {
					if recovering != "" {
						_ = c.transport.Unsubscribe(recovering)
					}
					recovering = next
					if err := c.transport.Subscribe(recovering); err != nil {
						return projection, err
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			if c.transport.State() != StateConnected {
				return ViewerProjection{Kind: "unavailable", Reason: "offline"}, fmt.Errorf("%w: %v", ErrViewerOffline, ctx.Err())
			}
			return projection, fmt.Errorf("%w: %v", ErrViewerStatusUnavailable, ctx.Err())
		case <-c.update:
		}
	}
}

func viewerChannelSegment(value string) string {
	const safe = "-_.!~*'()"
	const hex = "0123456789ABCDEF"
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(safe, c) >= 0 {
			out.WriteByte(c)
		} else {
			out.WriteByte('%')
			out.WriteByte(hex[c>>4])
			out.WriteByte(hex[c&0x0f])
		}
	}
	return out.String()
}
