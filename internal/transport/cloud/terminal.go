package cloud

// Signed Cloud terminal requests enter here, never through the HTTP Router.
// The transport has already verified the envelope signature and replay window;
// the sender in Inbound is the only principal this file accepts.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/app/terminals"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

const (
	CloudTerminalConnectionsLimit             = 16
	CloudTerminalViewerConnectionsLimit       = 2
	CloudTerminalRequestBytesLimit            = 6<<20 + 4<<10
	CloudTerminalReceiptsLimit                = 64
	CloudTerminalKeySecondsLimit              = 600
	CloudTerminalIngressLimit                 = 16
	CloudTerminalListIngressLimit             = 16
	CloudTerminalRefusalsLimit                = 16
	CloudTerminalRevocationRetireSecondsLimit = 3
	CloudTerminalFrameHeartbeatSecondsLimit   = 3
	CloudTerminalUnconfirmedSecondsLimit      = 15
	CloudTerminalHistoryReceiptBytesLimit     = 8 << 10
	CloudTerminalHistoryLineBytesLimit        = 4 << 10
	CloudTerminalHistoryCaptureBytesLimit     = 4 << 20
	CloudTerminalObservationRowsLimit         = 128
	// After a connection's first CloudTerminalObservationRowsLimit routine
	// stages, every CloudTerminalStageRoutineEvery-th is logged; after its
	// first CloudTerminalObservationRowsLimit failure stages, every
	// CloudTerminalStageNotableEvery-th (terminalStageLocked).
	CloudTerminalStageRoutineEvery = 64
	CloudTerminalStageNotableEvery = 16
	// CloudTerminalReceiptBusyRetriesLimit is how many times a receipt the
	// relay refused with rate_limited is published again, each after
	// CloudTerminalReceiptBusyRetrySecondsLimit. The relay refreshes the
	// account's terminal budget every two seconds, and the viewer waits ten
	// seconds for a receipt, so a busy relay costs a delay, not the connection.
	CloudTerminalReceiptBusyRetriesLimit      = 3
	CloudTerminalReceiptBusyRetrySecondsLimit = 2
	// CloudTerminalSweepSecondsLimit is how often every connection is
	// re-checked: authority, deadlines, and a direct connection's ack and
	// relay probe.
	CloudTerminalSweepSecondsLimit = 1
)

// TerminalCapacity reports the terminal rail without exposing its keys or
// terminal contents to diagnostics.
func (l *Link) TerminalCapacity(name string) capacity.Reading {
	if name == capacity.CloudTerminalDirectPeers {
		// directMu and terminalMu are never nested, so this row is read on its own.
		l.directMu.Lock()
		defer l.directMu.Unlock()
		r := capacity.Reading{Known: true}
		for _, p := range l.directPeers {
			if p != nil {
				r.Used++
			}
		}
		return r
	}
	l.terminalMu.Lock()
	defer l.terminalMu.Unlock()
	r := capacity.Reading{Known: true}
	switch name {
	case capacity.CloudTerminalConnections:
		r.Used = int64(len(l.terminalConnections))
	case capacity.CloudTerminalViewerConnections:
		counts := map[string]int64{}
		for _, c := range l.terminalConnections {
			counts[c.viewer]++
			if counts[c.viewer] > r.Used {
				r.Used = counts[c.viewer]
			}
		}
	case capacity.CloudTerminalReceipts:
		for _, c := range l.terminalConnections {
			if int64(len(c.receiptOrder)) > r.Used {
				r.Used = int64(len(c.receiptOrder))
			}
		}
	case capacity.CloudTerminalIngress:
		r.Used = int64(len(l.terminalRequests))
	case capacity.CloudTerminalListIngress:
		r.Used = int64(len(l.terminalLists))
	case capacity.CloudTerminalRefusals:
		r.Used = int64(len(l.terminalRefusals))
	case capacity.CloudTerminalRosterRefresh, capacity.CloudTerminalRosterDeadline,
		capacity.CloudTerminalRequestBytes, capacity.CloudTerminalKeySeconds, capacity.CloudTerminalRevocationRetire,
		capacity.CloudTerminalFrameHeartbeat, capacity.CloudTerminalUnconfirmed,
		capacity.CloudTerminalHistoryReceipt, capacity.CloudTerminalHistoryLine, capacity.CloudTerminalHistoryCapture,
		capacity.CloudTerminalDirectOffers, capacity.CloudTerminalDirectSDP, capacity.CloudTerminalDirectCandidates,
		capacity.CloudTerminalDirectNegotiate, capacity.CloudTerminalDirectGather, capacity.CloudTerminalDirectMessage,
		capacity.CloudTerminalDirectChunk, capacity.CloudTerminalDirectAck, capacity.CloudTerminalDirectProbe,
		capacity.CloudTerminalDirectProbeUnsettled, capacity.CloudTerminalSweep,
		capacity.CloudTerminalReceiptBusyRetries, capacity.CloudTerminalReceiptBusyRetry,
		capacity.CloudTerminalRosterRetry, capacity.CloudTerminalUnverifiedRetire:
		r.Note = "per-operation limit; no requests retained"
	default:
		return capacity.Unmeasured("unknown terminal capacity row")
	}
	return r
}

var terminalUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type terminalRequest struct {
	V          int             `json:"v"`
	Type       string          `json:"type"`
	RequestID  string          `json:"request_id"`
	Connection string          `json:"connection"`
	Operation  string          `json:"operation"`
	TerminalID string          `json:"terminal_id,omitempty"`
	ProjectID  string          `json:"project_id,omitempty"`
	Client     string          `json:"client,omitempty"`
	Epoch      uint64          `json:"epoch,omitempty"`
	Seq        uint64          `json:"seq,omitempty"`
	KeyID      string          `json:"key_id,omitempty"`
	Key        string          `json:"key,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
}

type terminalReceipt struct {
	V          int    `json:"v"`
	Type       string `json:"type"`
	RequestID  string `json:"request_id"`
	Connection string `json:"connection"`
	Operation  string `json:"operation"`
	TerminalID string `json:"terminal_id,omitempty"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	Result     any    `json:"result,omitempty"`
}

type terminalConnection struct {
	viewer, id, keyID        string
	key                      domaincloud.ContentKey
	expires                  time.Time
	terminalID               terminal.ID
	client                   string
	frameSeq                 uint64
	publishedFrameSeq        uint64
	framePendingSeq          uint64
	framePending             bool
	frameDeltaV1             bool
	frameBase                *terminal.Frame
	frameBaseSeq             uint64
	frameBaseTerminalID      terminal.ID
	frameCandidate           *terminal.Frame
	frameCandidateSeq        uint64
	frameCandidateTerminalID terminal.ID
	framePendingChannel      string
	watchReceiptSeq          uint64
	watchReceiptPending      bool
	watchReceiptDeadline     time.Time
	rekeyReceiptSeq          uint64
	rekeyReceiptPending      bool
	activateReceiptSeq       uint64
	activateReceiptPending   bool
	activateOld              *terminalConnection
	unconfirmedDeadline      time.Time
	watchCancel              context.CancelFunc
	watchReady               chan struct{}
	receipts                 map[string][]byte
	pendingReceipts          map[string]bool
	receiptOrder             []string
	rekeyPending             bool
	denied                   bool
	deniedAt                 time.Time
	// unverifiedSince is when the sweep first found this viewer's authority
	// unverifiable (TerminalUnverified) in the current streak; zero while it
	// is verified. The connection is paused meanwhile and retired, without a
	// revocation, after CloudTerminalUnverifiedRetireSecondsLimit.
	unverifiedSince time.Time
	// stageLines counts every receipt stage; stageRoutine and stageNotable
	// count each kind, which decides whether a stage is logged.
	stageLines   int
	stageRoutine int
	stageNotable int
	receiptOps   map[uint64]string
	// receiptRetries holds each relay-published receipt until it settles, so
	// one the relay refused with rate_limited can be published again.
	receiptRetries map[uint64]receiptRetry
	// directReceipts is set when the viewer asked, in its direct rekey, for
	// the receipts of its everyday requests on the data channel.
	directReceipts bool
	// carrier is "direct" for a connection whose frames travel on the
	// viewer's data channel (direct.go), and empty on the relay.
	carrier        string
	directSeq      uint64
	framePendingAt time.Time
	probePending   bool
	probeSeq       uint64
	probeAt        time.Time
	// probeSince is when the oldest probe the relay has not yet delivered was
	// sent; a probe it refused for its own load leaves it set.
	probeSince time.Time
	probeN     uint64
}

type receiptRetry struct {
	receipt terminalReceipt
	attempt int
}

func terminalReceiptSettleID(channel string, seq uint64) string {
	return channel + "/" + strconv.FormatUint(seq, 10)
}

func (l *Link) terminalReceiptSettled(channel string, seq uint64, kind adaptercloud.SettleKind) {
	l.terminalMu.Lock()
	var failed *terminalConnection
	var startRekey *terminalConnection
	var retireOld *terminalConnection
	var stage string
	var resend *terminalConnection
	var again receiptRetry
	for _, connection := range l.terminalConnections {
		if channel == connection.framePendingChannel && connection.framePending && connection.framePendingSeq == seq {
			connection.framePending = false
			if kind != adaptercloud.SettleDelivered && kind != adaptercloud.SettleRateLimited {
				failed = connection
			} else if kind == adaptercloud.SettleDelivered {
				connection.frameBase = connection.frameCandidate
				connection.frameBaseSeq = connection.frameCandidateSeq
				connection.frameBaseTerminalID = connection.frameCandidateTerminalID
			}
			connection.frameCandidate = nil
			connection.framePendingChannel = ""
			break
		}
		if channel == terminalReceiptChannel(l.identity.MachineID, connection) {
			if connection.probePending && connection.probeSeq == seq {
				connection.probePending = false
				if kind == adaptercloud.SettleDelivered {
					connection.probeSince = time.Time{}
				}
				// The relay refused it for its own load, which says nothing about
				// this viewer: the next probe asks again, and probeSince bounds it.
				if kind == adaptercloud.SettleRateLimited {
					break
				}
			}
			if operation, ok := connection.receiptOps[seq]; ok {
				delete(connection.receiptOps, seq)
				stage = l.terminalStageLocked(connection, "receipt_settled", operation, string(kind))
			}
			retry, retrying := connection.receiptRetries[seq]
			delete(connection.receiptRetries, seq)
			if kind == adaptercloud.SettleRateLimited && retrying && retry.attempt < CloudTerminalReceiptBusyRetriesLimit {
				resend, again = connection, receiptRetry{receipt: retry.receipt, attempt: retry.attempt + 1}
				break
			}
			if kind != adaptercloud.SettleDelivered {
				failed = connection
			} else if connection.rekeyReceiptPending && connection.rekeyReceiptSeq == seq {
				connection.rekeyReceiptPending = false
				startRekey = connection
			} else if connection.activateReceiptPending && connection.activateReceiptSeq == seq {
				connection.activateReceiptPending = false
				connection.rekeyPending = false
				retireOld = connection.activateOld
				connection.activateOld = nil
			} else if connection.watchReceiptPending && connection.watchReceiptSeq == seq {
				connection.watchReceiptPending = false
				connection.watchReceiptDeadline = time.Time{}
				if connection.watchReady != nil {
					close(connection.watchReady)
					connection.watchReady = nil
				}
			}
			break
		}
	}
	id := terminalReceiptSettleID(channel, seq)
	c := l.terminalRetireAfterReceipt[id]
	delete(l.terminalRetireAfterReceipt, id)
	l.terminalMu.Unlock()
	if stage != "" {
		l.logf("%s", stage)
	}
	if failed != nil {
		l.closeTerminalConnection(failed)
	}
	if resend != nil {
		l.terminalAfterFunc(CloudTerminalReceiptBusyRetrySecondsLimit*time.Second, func() {
			if l.getTerminalConnection(resend.viewer, resend.id) == resend {
				_ = l.sendTerminalReceiptAttempt(context.Background(), resend, again.receipt, again.attempt)
			}
		})
	}
	if startRekey != nil {
		go l.startRekeyTerminalWatch(startRekey)
	}
	if retireOld != nil {
		l.closeTerminalConnection(retireOld)
	}
	if c != nil {
		l.closeTerminalConnection(c)
	}
}

func (l *Link) terminalAfterFunc(d time.Duration, f func()) {
	if l.terminalAfter != nil {
		l.terminalAfter(d, f)
		return
	}
	time.AfterFunc(d, f)
}

func (l *Link) startRekeyTerminalWatch(c *terminalConnection) {
	svc, err := l.terminalService()
	if err != nil {
		l.closeTerminalConnection(c)
		return
	}
	name, _ := l.PinnedTerminalViewer(c.viewer)
	p := terminals.Principal{Device: c.viewer, Name: name, Cloud: true}
	if err := svc.Allow(p); terminals.Unverified(err) {
		// Not a revocation: the viewer rekeys or reopens once it can be verified.
		l.closeTerminalConnection(c)
		return
	} else if err != nil {
		l.revokeRegisteredTerminal(context.Background(), c)
		return
	}
	l.terminalMu.Lock()
	id, client := c.terminalID, c.client
	valid := l.terminalConnections[terminalConnectionID(c.viewer, c.id)] == c && !c.denied && c.expires.After(l.opts.Now())
	l.terminalMu.Unlock()
	if !valid || !id.Valid() {
		l.closeTerminalConnection(c)
		return
	}
	if err := l.watchTerminal(context.Background(), svc, p, c, id, client); err != nil {
		l.closeTerminalConnection(c)
		return
	}
	l.terminalMu.Lock()
	if l.terminalConnections[terminalConnectionID(c.viewer, c.id)] == c && c.watchReady != nil {
		close(c.watchReady)
		c.watchReady = nil
	}
	l.terminalMu.Unlock()
}

func (l *Link) terminalMetadataAction(ctx context.Context, action string, c *terminalConnection) error {
	if l.terminalMetadata != nil {
		return l.terminalMetadata(ctx, action, c)
	}
	if l.transport == nil && l.terminalMetadata == nil {
		return ErrRelayNotReady
	}
	return l.transport.TerminalConnection(ctx, action, c.viewer, c.id, c.expires)
}

// Refusal is queued while relay registration remains active. The connection
// stops host effects immediately; retirement follows this receipt's relay
// settlement, or the bounded revocation tombstone when no ack arrives.
func (l *Link) refuseRegisteredTerminal(ctx context.Context, c *terminalConnection, receipt terminalReceipt) {
	l.terminalMu.Lock()
	if l.terminalConnections[terminalConnectionID(c.viewer, c.id)] != c {
		l.terminalMu.Unlock()
		return
	}
	c.denied = true
	c.frameBase = nil
	c.frameCandidate = nil
	if c.deniedAt.IsZero() {
		c.deniedAt = l.opts.Now()
	}
	for id, pending := range l.terminalRetireAfterReceipt {
		if pending == c {
			delete(l.terminalRetireAfterReceipt, id)
		}
	}
	if c.watchCancel != nil {
		c.watchCancel()
	}
	l.terminalMu.Unlock()
	data, err := json.Marshal(receipt)
	if err != nil {
		l.closeTerminalConnection(c)
		return
	}
	if l.relay == nil {
		l.closeTerminalConnection(c)
		return
	}
	channel := terminalReceiptChannel(l.identity.MachineID, c)
	l.terminalMu.Lock()
	seq, err := l.relay.PublishTracked(ctx, Outbound{Channel: channel,
		Class: string(domaincloud.ClassCtl), Payload: data, Key: c.key, KeyID: c.keyID})
	if err == nil {
		if l.terminalRetireAfterReceipt == nil {
			l.terminalRetireAfterReceipt = map[string]*terminalConnection{}
		}
		l.terminalRetireAfterReceipt[terminalReceiptSettleID(channel, seq)] = c
	}
	l.terminalMu.Unlock()
	if err != nil {
		l.closeTerminalConnection(c)
		return
	}
	l.logTerminalStage(c, "receipt_published", receipt.Operation, "refused")
}

func (l *Link) revokeRegisteredTerminal(ctx context.Context, c *terminalConnection) {
	l.terminalMu.Lock()
	if c.denied || l.terminalConnections[terminalConnectionID(c.viewer, c.id)] != c {
		l.terminalMu.Unlock()
		return
	}
	c.denied = true
	c.frameBase = nil
	c.frameCandidate = nil
	c.deniedAt = l.opts.Now()
	if c.watchCancel != nil {
		c.watchCancel()
	}
	l.terminalMu.Unlock()
	data, err := json.Marshal(map[string]any{"v": 1, "type": "terminal_notice", "connection": c.id,
		"code": "terminal_access_revoked", "machine_incarnation": l.machineIncarnation})
	if err != nil {
		l.closeTerminalConnection(c)
		return
	}
	if l.relay == nil {
		l.closeTerminalConnection(c)
		return
	}
	channel := terminalReceiptChannel(l.identity.MachineID, c)
	l.terminalMu.Lock()
	seq, err := l.relay.PublishTracked(ctx, Outbound{Channel: channel,
		Class: string(domaincloud.ClassCtl), Payload: data, Key: c.key, KeyID: c.keyID})
	if err == nil {
		if l.terminalRetireAfterReceipt == nil {
			l.terminalRetireAfterReceipt = map[string]*terminalConnection{}
		}
		l.terminalRetireAfterReceipt[terminalReceiptSettleID(channel, seq)] = c
	}
	l.terminalMu.Unlock()
	if err != nil {
		l.closeTerminalConnection(c)
	}
}

func terminalConnectionID(viewer, connection string) string { return viewer + "/" + connection }

func (l *Link) deliverTerminal(in Inbound) {
	lane := l.terminalRequests
	if in.Channel == "termi/"+l.identity.MachineID+"/"+in.Sender && in.Class == string(domaincloud.ClassCtl) {
		if req, err := decodeTerminalRequest(in.Plaintext); err == nil && req.Operation == "list" {
			lane = l.terminalLists
		}
	}
	select {
	case lane <- in:
	default:
		select {
		case l.terminalRefusals <- in:
		default:
			l.logf("cloud terminal: ingress and refusal lanes are full; sender=%s seq=%d was unanswered", in.Sender, in.Sequence)
		}
	}
}

func (l *Link) runTerminal(ctx context.Context) {
	refusalDone := make(chan struct{})
	listDone := make(chan struct{})
	sweepDone := make(chan struct{})
	go func() {
		defer close(sweepDone)
		ticker := time.NewTicker(CloudTerminalSweepSecondsLimit * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				l.sweepTerminalConnections()
			}
		}
	}()
	go func() {
		defer close(listDone)
		for {
			select {
			case <-ctx.Done():
				return
			case in := <-l.terminalLists:
				l.handleTerminal(ctx, in)
			}
		}
	}()
	go func() {
		defer close(refusalDone)
		for {
			select {
			case <-ctx.Done():
				return
			case in := <-l.terminalRefusals:
				l.refuseTerminalBusy(ctx, in)
			}
		}
	}()
	defer func() { <-listDone; <-refusalDone; <-sweepDone }()
	for {
		select {
		case <-ctx.Done():
			return
		case in := <-l.terminalRequests:
			l.handleTerminal(ctx, in)
		}
	}
}

func (l *Link) sweepTerminalConnections() {
	l.terminalMu.Lock()
	connections := make([]*terminalConnection, 0, len(l.terminalConnections))
	for _, c := range l.terminalConnections {
		connections = append(connections, c)
	}
	l.terminalMu.Unlock()
	svc, err := l.terminalService()
	for _, c := range connections {
		l.terminalMu.Lock()
		denied, deniedAt := c.denied, c.deniedAt
		l.terminalMu.Unlock()
		l.terminalMu.Lock()
		unconfirmedDeadline, watchReceiptDeadline := c.unconfirmedDeadline, c.watchReceiptDeadline
		l.terminalMu.Unlock()
		if !c.expires.After(l.opts.Now()) || (!unconfirmedDeadline.IsZero() && !unconfirmedDeadline.After(l.opts.Now())) ||
			(!watchReceiptDeadline.IsZero() && !watchReceiptDeadline.After(l.opts.Now())) ||
			(denied && !deniedAt.Add(CloudTerminalRevocationRetireSecondsLimit*time.Second).After(l.opts.Now())) {
			l.closeTerminalConnection(c)
			continue
		} else if denied {
			l.sweepDirect(c)
			continue
		}
		access := err
		if access == nil {
			access = svc.Allow(terminals.Principal{Device: c.viewer, Cloud: true})
		}
		switch {
		case access == nil:
			l.terminalMu.Lock()
			c.unverifiedSince = time.Time{}
			l.terminalMu.Unlock()
			l.sweepDirect(c)
		case err == nil && terminals.Unverified(access):
			// Paused, not revoked: frames and requests are refused as they come
			// while the roster cannot be read. A streak that outlasts the bound
			// retires the registration without terminal_access_revoked; the
			// viewer reconnects.
			now := l.opts.Now()
			l.terminalMu.Lock()
			if c.unverifiedSince.IsZero() {
				c.unverifiedSince = now
			}
			expired := !c.unverifiedSince.Add(CloudTerminalUnverifiedRetireSecondsLimit * time.Second).After(now)
			l.terminalMu.Unlock()
			if expired {
				l.logf("cloud terminal: retired connection of %s after %d s without verifiable authority", c.viewer,
					CloudTerminalUnverifiedRetireSecondsLimit)
				l.closeTerminalConnection(c)
			} else {
				l.sweepDirect(c)
			}
		default:
			l.revokeRegisteredTerminal(context.Background(), c)
		}
	}
}

func (l *Link) refuseTerminalBusy(ctx context.Context, in Inbound) {
	req, err := decodeTerminalRequest(in.Plaintext)
	if err != nil {
		return
	}
	c := l.getTerminalConnection(in.Sender, req.Connection)
	if c == nil && (req.Operation == "open_connection" || req.Operation == "rekey_connection") {
		key, err := parseConnectionKey(req)
		if err != nil {
			return
		}
		c = &terminalConnection{viewer: in.Sender, id: req.Connection, keyID: req.KeyID, key: key,
			receipts: map[string][]byte{}}
	}
	if c == nil {
		return
	}
	l.sendTerminalReceipt(ctx, c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
		Connection: req.Connection, Operation: req.Operation, TerminalID: req.TerminalID,
		Status: "refused", Error: string(terminal.CodeBusy)})
}

func (l *Link) closeAllTerminalConnections() {
	l.terminalMu.Lock()
	retired := make([]*terminalConnection, 0, len(l.terminalConnections))
	for id, c := range l.terminalConnections {
		c.frameBase = nil
		c.frameCandidate = nil
		if c.watchCancel != nil {
			c.watchCancel()
		}
		delete(l.terminalConnections, id)
		retired = append(retired, c)
	}
	l.terminalRetireAfterReceipt = nil
	l.terminalMu.Unlock()
	for _, c := range retired {
		l.retireTerminalConnection(c)
	}
}

func (l *Link) revokeTerminalViewer(viewer string) error {
	l.terminalMu.Lock()
	denied := []*terminalConnection{}
	for _, c := range l.terminalConnections {
		if c.viewer == viewer {
			denied = append(denied, c)
		}
	}
	l.terminalMu.Unlock()
	for _, c := range denied {
		l.revokeRegisteredTerminal(context.Background(), c)
	}
	if l.opts.DropTerminalGrant != nil {
		return l.opts.DropTerminalGrant(viewer)
	}
	return nil
}

func decodeTerminalRequest(raw []byte) (terminalRequest, error) {
	var req terminalRequest
	if len(raw) == 0 || len(raw) > CloudTerminalRequestBytesLimit {
		return req, terminal.Refuse(terminal.CodeInvalid, "terminal request size is invalid")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || dec.Decode(new(any)) != io.EOF ||
		req.V != 1 || req.Type != "terminal_request" || !terminalUUID.MatchString(req.RequestID) ||
		!validConnection(req.Connection) {
		return terminalRequest{}, terminal.Refuse(terminal.CodeInvalid, "terminal request shape is invalid")
	}
	return req, nil
}

func validConnection(id string) bool {
	if len(id) != 22 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(id)
	return err == nil && len(raw) == 16 && base64.RawURLEncoding.EncodeToString(raw) == id
}

func parseConnectionKey(req terminalRequest) (domaincloud.ContentKey, error) {
	if len(req.KeyID) != 25 || !strings.HasPrefix(req.KeyID, "rk-") || !validConnection(req.KeyID[3:]) {
		return domaincloud.ContentKey{}, terminal.Refuse(terminal.CodeInvalid, "terminal key id is invalid")
	}
	raw, err := base64.StdEncoding.DecodeString(req.Key)
	if err != nil || base64.StdEncoding.EncodeToString(raw) != req.Key {
		return domaincloud.ContentKey{}, terminal.Refuse(terminal.CodeInvalid, "terminal key is invalid")
	}
	return domaincloud.ContentKeyFromBytes(raw)
}

func (l *Link) terminalService() (*terminals.Service, error) {
	if l.opts.TerminalService == nil {
		return nil, terminal.Refuse(terminal.CodeUnsupported, "Cloud terminals are unavailable")
	}
	return l.opts.TerminalService()
}

// handleTerminal handles only the dedicated termi rail. It is deliberately
// separate from cloudops.Bridge and Router, which cannot grant terminal access.
func (l *Link) handleTerminal(ctx context.Context, in Inbound) {
	if in.Channel != "termi/"+l.identity.MachineID+"/"+in.Sender || in.Class != string(domaincloud.ClassCtl) {
		return
	}
	req, err := decodeTerminalRequest(in.Plaintext)
	if err != nil {
		l.logf("cloud terminal: malformed signed request from %s: %v", in.Sender, err)
		return
	}
	svc, err := l.terminalService()
	if err != nil {
		l.logf("cloud terminal: service unavailable: %v", err)
		name, _ := l.PinnedTerminalViewer(in.Sender)
		p := terminals.Principal{Device: in.Sender, Name: name, Cloud: true}
		if req.Operation == "open_connection" || req.Operation == "rekey_connection" {
			l.openTerminalConnection(ctx, nil, p, req)
			return
		}
		c := l.getTerminalConnection(in.Sender, req.Connection)
		if c != nil {
			l.refuseRegisteredTerminal(ctx, c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
				Connection: req.Connection, Operation: req.Operation, TerminalID: req.TerminalID,
				Status: "refused", Error: string(terminal.CodeUnsupported)})
		}
		return
	}
	name, _ := l.PinnedTerminalViewer(in.Sender)
	p := terminals.Principal{Device: in.Sender, Name: name, Cloud: true}
	if req.Operation == "open_connection" || req.Operation == "rekey_connection" {
		l.openTerminalConnection(ctx, svc, p, req)
		return
	}
	c := l.getTerminalConnection(in.Sender, req.Connection)
	if c == nil {
		return // No key exists to encrypt a trustworthy receipt to this tab.
	}
	l.terminalMu.Lock()
	denied := c.denied
	l.terminalMu.Unlock()
	if denied {
		l.refuseRegisteredTerminal(ctx, c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
			Connection: req.Connection, Operation: req.Operation, TerminalID: req.TerminalID,
			Status: "refused", Error: string(terminal.CodeForbidden)})
		return
	}
	if err := svc.Allow(p); terminals.Unverified(err) {
		// Cannot tell right now: a retryable refusal, and the connection stays.
		l.sendTerminalReceipt(ctx, c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
			Connection: req.Connection, Operation: req.Operation, TerminalID: req.TerminalID,
			Status: "refused", Error: string(terminal.CodeBusy)})
		return
	} else if err != nil {
		l.refuseRegisteredTerminal(ctx, c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
			Connection: req.Connection, Operation: req.Operation, TerminalID: req.TerminalID,
			Status: "refused", Error: string(terminal.CodeForbidden)})
		return
	}
	l.terminalMu.Lock()
	c.unconfirmedDeadline = time.Time{}
	l.terminalMu.Unlock()
	if req.Operation == "direct_offer" {
		l.handleDirectOffer(ctx, svc, p, c, req)
		return
	}
	if previous := l.terminalReceipt(c, req.RequestID); previous != nil {
		var original terminalReceipt
		if json.Unmarshal(previous, &original) != nil || original.Operation != req.Operation || original.TerminalID != req.TerminalID {
			bad, _ := json.Marshal(terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
				Connection: req.Connection, Operation: req.Operation, TerminalID: req.TerminalID,
				Status: "refused", Error: string(terminal.CodeInvalid)})
			_ = l.publishTerminal(ctx, Outbound{Channel: terminalReceiptChannel(l.identity.MachineID, c),
				Class: string(domaincloud.ClassCtl), Payload: bad, Key: c.key, KeyID: c.keyID})
			l.logTerminalStage(c, "receipt_published", req.Operation, "refused")
			return
		}
		_ = l.sendTerminalReceipt(ctx, c, original)
		return
	}
	if !l.terminalReceiptAvailable(c) {
		l.sendTerminalReceipt(ctx, c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
			Connection: req.Connection, Operation: req.Operation, TerminalID: req.TerminalID,
			Status: "refused", Error: string(terminal.CodeBusy)})
		return
	}
	l.terminalMu.Lock()
	if c.pendingReceipts == nil {
		c.pendingReceipts = map[string]bool{}
	}
	if previous := c.receipts[req.RequestID]; previous != nil {
		l.terminalMu.Unlock()
		var original terminalReceipt
		if json.Unmarshal(previous, &original) == nil && original.Operation == req.Operation && original.TerminalID == req.TerminalID {
			_ = l.sendTerminalReceipt(ctx, c, original)
		} else {
			bad, _ := json.Marshal(terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
				Connection: req.Connection, Operation: req.Operation, TerminalID: req.TerminalID,
				Status: "refused", Error: string(terminal.CodeInvalid)})
			_ = l.publishTerminal(ctx, Outbound{Channel: terminalReceiptChannel(l.identity.MachineID, c),
				Class: string(domaincloud.ClassCtl), Payload: bad, Key: c.key, KeyID: c.keyID})
			l.logTerminalStage(c, "receipt_published", req.Operation, "refused")
		}
		return
	}
	if c.pendingReceipts[req.RequestID] {
		l.terminalMu.Unlock()
		busy, _ := json.Marshal(terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
			Connection: req.Connection, Operation: req.Operation, TerminalID: req.TerminalID,
			Status: "refused", Error: string(terminal.CodeBusy)})
		_ = l.publishTerminal(ctx, Outbound{Channel: terminalReceiptChannel(l.identity.MachineID, c),
			Class: string(domaincloud.ClassCtl), Payload: busy, Key: c.key, KeyID: c.keyID})
		l.logTerminalStage(c, "receipt_published", req.Operation, "refused")
		return
	}
	c.pendingReceipts[req.RequestID] = true
	l.terminalMu.Unlock()
	defer func() {
		l.terminalMu.Lock()
		delete(c.pendingReceipts, req.RequestID)
		l.terminalMu.Unlock()
	}()
	result, err := l.terminalOperation(ctx, svc, p, c, req)
	receipt := terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
		Connection: req.Connection, Operation: req.Operation, TerminalID: req.TerminalID,
		Status: "ok", Result: result}
	if err != nil {
		code, known := terminal.CodeOf(err)
		if !known {
			code = terminal.CodeUnreachable
		}
		receipt.Status, receipt.Error, receipt.Result = "refused", string(code), nil
		if code == terminal.CodeInputStateUnknown {
			receipt.Status = "unknown"
		}
		var refusal *terminals.Refusal
		if errors.As(err, &refusal) && refusal.Applied > 0 {
			receipt.Result = map[string]any{"applied_through": refusal.Applied}
		}
	}
	publicationErr := l.sendTerminalReceipt(ctx, c, receipt)
	if req.Operation == "release_connection" && receipt.Status == "ok" && publicationErr == nil {
		// Keep the key registered until the relay settles this final receipt.
		// A list-only viewer would otherwise consume a slot for the full key lifetime.
		l.terminalMu.Lock()
		c.denied = true
		c.deniedAt = l.opts.Now()
		l.terminalMu.Unlock()
	}
	if (req.Operation == "open" || req.Operation == "read" || req.Operation == "capture") && receipt.Status == "ok" && publicationErr != nil {
		l.closeTerminalConnection(c)
	}
	if (req.Operation == "open" || req.Operation == "read" || req.Operation == "capture") && receipt.Status != "ok" {
		l.terminalMu.Lock()
		if c.watchReady != nil && c.watchCancel != nil {
			c.watchCancel()
			c.watchReady = nil
		}
		l.terminalMu.Unlock()
	}
}

func (l *Link) openTerminalConnection(ctx context.Context, svc *terminals.Service, p terminals.Principal, req terminalRequest) {
	key, err := parseConnectionKey(req)
	if err != nil {
		return
	}
	preCode := ""
	var old *terminalConnection
	if req.Operation == "rekey_connection" {
		old, preCode = l.terminalRekeySource(p.Device, req)
	}
	c := &terminalConnection{viewer: p.Device, id: req.Connection, keyID: req.KeyID, key: key,
		expires: l.opts.Now().Add(CloudTerminalKeySecondsLimit * time.Second), receipts: map[string][]byte{},
		unconfirmedDeadline: l.opts.Now().Add(CloudTerminalUnconfirmedSecondsLimit * time.Second),
		rekeyPending:        req.Operation == "rekey_connection"}
	if req.Operation == "open_connection" {
		var body struct {
			FrameDeltaV1 bool `json:"frame_delta_v1"`
		}
		if len(req.Body) == 0 || strictTerminalBody(req.Body, &body) {
			c.frameDeltaV1 = body.FrameDeltaV1
		}
	} else {
		var body struct {
			OldConnection  string `json:"old_connection"`
			FrameDeltaV1   bool   `json:"frame_delta_v1"`
			Carrier        string `json:"carrier"`
			DirectReceipts bool   `json:"direct_receipts"`
		}
		if strictTerminalBody(req.Body, &body) {
			c.frameDeltaV1 = body.FrameDeltaV1
			if body.Carrier == carrierDirect && preCode == "" {
				switch {
				case !l.directEnabled():
					preCode = string(terminal.CodeDirectDisabled)
				case !l.directPeerOpen(p.Device):
					preCode = string(terminal.CodeDirectUnavailable)
				default:
					c.carrier = carrierDirect
					c.directReceipts = body.DirectReceipts
				}
			} else if body.Carrier != "" && preCode == "" {
				preCode = string(terminal.CodeInvalid)
			}
		}
	}
	if preCode == "" && old != nil {
		l.terminalMu.Lock()
		c.terminalID, c.client = old.terminalID, old.client
		l.terminalMu.Unlock()
	}
	switch verdict, _ := l.terminalAuthority(p.Device, false); verdict {
	case TerminalDenied:
		return // A viewer the account does not let in is not someone to encrypt to.
	case TerminalUnverified:
		// The viewer's own key is in the request, so it can be told to try
		// again rather than left to time out. Nothing is registered.
		l.sendTerminalReceipt(ctx, c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
			Connection: req.Connection, Operation: req.Operation, Status: "refused", Error: string(terminal.CodeBusy)})
		return
	}
	l.terminalMu.Lock()
	if l.terminalConnections == nil {
		l.terminalConnections = map[string]*terminalConnection{}
	}
	id := terminalConnectionID(p.Device, req.Connection)
	if old := l.terminalConnections[id]; old != nil {
		l.terminalMu.Unlock()
		if preCode == "" && !old.denied && old.keyID == c.keyID && bytes.Equal(old.key.Bytes(), c.key.Bytes()) {
			l.sendTerminalReceipt(ctx, old, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
				Connection: req.Connection, Operation: req.Operation, Status: "ok", Result: l.connectionResult(old)})
		} else if old.keyID == c.keyID && bytes.Equal(old.key.Bytes(), c.key.Bytes()) {
			l.sendTerminalReceipt(ctx, old, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
				Connection: req.Connection, Operation: req.Operation, Status: "refused", Error: string(terminal.CodeInvalid)})
		}
		return
	}
	viewerConnections := 0
	for _, existing := range l.terminalConnections {
		if existing.viewer == p.Device {
			viewerConnections++
		}
	}
	if len(l.terminalConnections) >= CloudTerminalConnectionsLimit || viewerConnections >= CloudTerminalViewerConnectionsLimit {
		l.terminalMu.Unlock()
		l.sendTerminalReceipt(ctx, c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
			Connection: req.Connection, Operation: req.Operation, Status: "refused", Error: string(terminal.CodeBusy)})
		return
	}
	l.terminalConnections[id] = c
	l.terminalMu.Unlock()
	if l.terminalMetadataAction(ctx, "register", c) != nil {
		l.closeTerminalConnection(c)
		return
	}
	code := preCode
	if err := l.TerminalViewerAccess(p.Device); terminals.Unverified(err) {
		code = string(terminal.CodeBusy)
	} else if err != nil {
		code = string(terminal.CodeForbidden)
	} else if svc == nil {
		code = string(terminal.CodeUnsupported)
	} else if err := svc.Allow(p); terminals.Unverified(err) {
		code = string(terminal.CodeBusy)
	} else if err != nil {
		code = string(terminal.CodeForbidden)
	}
	if code != "" {
		l.refuseRegisteredTerminal(ctx, c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
			Connection: req.Connection, Operation: req.Operation, Status: "refused", Error: code})
		return
	}
	if err := l.sendTerminalReceipt(ctx, c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
		Connection: req.Connection, Operation: req.Operation, Status: "ok", Result: l.connectionResult(c)}); err != nil {
		l.closeTerminalConnection(c)
	}
}

func (l *Link) terminalRekeySource(viewer string, req terminalRequest) (*terminalConnection, string) {
	var body struct {
		OldConnection  string `json:"old_connection"`
		FrameDeltaV1   bool   `json:"frame_delta_v1"`
		Carrier        string `json:"carrier"`
		DirectReceipts bool   `json:"direct_receipts"`
	}
	if !strictTerminalBody(req.Body, &body) || !validConnection(body.OldConnection) || body.OldConnection == req.Connection {
		return nil, string(terminal.CodeInvalid)
	}
	old := l.getTerminalConnection(viewer, body.OldConnection)
	if old == nil {
		return nil, string(terminal.CodeInvalid)
	}
	l.terminalMu.Lock()
	valid := !old.denied && old.terminalID.Valid() && old.expires.After(l.opts.Now())
	l.terminalMu.Unlock()
	if !valid {
		return nil, string(terminal.CodeInvalid)
	}
	return old, ""
}

func (l *Link) connectionResult(c *terminalConnection) any {
	result := map[string]any{"connection": c.id, "key_id": c.keyID,
		"expires_at": float64(c.expires.UnixMilli()) / 1000, "machine_incarnation": l.machineIncarnation}
	if c.frameDeltaV1 {
		result["frame_delta_v1"] = true
	}
	if c.carrier == carrierDirect {
		result["carrier"] = carrierDirect
		if c.directReceipts {
			result["direct_receipts"] = true
		}
	}
	return result
}

func (l *Link) getTerminalConnection(viewer, connection string) *terminalConnection {
	l.terminalMu.Lock()
	c := l.terminalConnections[terminalConnectionID(viewer, connection)]
	if c != nil && !c.expires.After(l.opts.Now()) {
		if c.watchCancel != nil {
			c.watchCancel()
		}
		delete(l.terminalConnections, terminalConnectionID(viewer, connection))
		l.terminalMu.Unlock()
		l.retireTerminalConnection(c)
		return nil
	}
	l.terminalMu.Unlock()
	return c
}

func (l *Link) closeTerminalConnection(c *terminalConnection) {
	l.terminalMu.Lock()
	removed := false
	if l.terminalConnections[terminalConnectionID(c.viewer, c.id)] == c {
		c.frameBase = nil
		c.frameCandidate = nil
		delete(l.terminalConnections, terminalConnectionID(c.viewer, c.id))
		removed = true
		for id, pending := range l.terminalRetireAfterReceipt {
			if pending == c {
				delete(l.terminalRetireAfterReceipt, id)
			}
		}
		if c.watchCancel != nil {
			c.watchCancel()
		}
	}
	l.terminalMu.Unlock()
	if removed {
		l.retireTerminalConnection(c)
		l.directOwnerRetired(c)
	}
}

func (l *Link) retireTerminalConnection(c *terminalConnection) {
	if l.transport == nil && l.terminalMetadata == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := l.terminalMetadataAction(ctx, "retire", c); err != nil {
			l.logf("cloud terminal: relay retirement failed viewer=%s connection=%s: %v", c.viewer, c.id, err)
		}
	}()
}

func (l *Link) terminalReceipt(c *terminalConnection, requestID string) []byte {
	l.terminalMu.Lock()
	defer l.terminalMu.Unlock()
	return append([]byte(nil), c.receipts[requestID]...)
}

func (l *Link) terminalReceiptAvailable(c *terminalConnection) bool {
	l.terminalMu.Lock()
	defer l.terminalMu.Unlock()
	return len(c.receiptOrder) < CloudTerminalReceiptsLimit
}

func (l *Link) sendTerminalReceipt(ctx context.Context, c *terminalConnection, receipt terminalReceipt) error {
	return l.sendTerminalReceiptAttempt(ctx, c, receipt, 0)
}

// sendTerminalReceiptAttempt publishes a receipt; attempt counts the times
// the relay already refused this one with rate_limited.
func (l *Link) sendTerminalReceiptAttempt(ctx context.Context, c *terminalConnection, receipt terminalReceipt, attempt int) error {
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	if l.sendDirectReceipt(c, receipt, data) {
		return nil
	}
	if l.relay == nil {
		return ErrRelayNotReady
	}
	l.terminalMu.Lock()
	seq, err := l.relay.PublishTracked(ctx, Outbound{Channel: terminalReceiptChannel(l.identity.MachineID, c),
		Class: string(domaincloud.ClassCtl), Payload: data, Key: c.key, KeyID: c.keyID})
	if err == nil && receipt.Status == "ok" && (receipt.Operation == "open" || receipt.Operation == "read" || receipt.Operation == "capture") && c.watchReady != nil {
		c.watchReceiptPending = true
		c.watchReceiptSeq = seq
		c.watchReceiptDeadline = l.opts.Now().Add(CloudTerminalUnconfirmedSecondsLimit * time.Second)
	}
	// A later signed viewer request advances this opening handshake. Relay
	// delivery of a frame cannot prove browser observation of this receipt.
	if err == nil && receipt.Status == "ok" && (receipt.Operation == "open" || receipt.Operation == "read" || receipt.Operation == "rekey_connection") {
		c.unconfirmedDeadline = l.opts.Now().Add(CloudTerminalUnconfirmedSecondsLimit * time.Second)
	}
	if err == nil && receipt.Status == "ok" && receipt.Operation == "rekey_connection" && c.rekeyPending {
		c.rekeyReceiptPending = true
		c.rekeyReceiptSeq = seq
	}
	if err == nil && receipt.Status == "ok" && receipt.Operation == "activate_connection" {
		if result, ok := receipt.Result.(map[string]any); ok {
			if oldID, ok := result["retired_connection"].(string); ok {
				c.activateOld = l.terminalConnections[terminalConnectionID(c.viewer, oldID)]
				c.activateReceiptSeq = seq
				c.activateReceiptPending = true
			}
		}
	}
	if err == nil && receipt.Status == "ok" && receipt.Operation == "release_connection" {
		if l.terminalRetireAfterReceipt == nil {
			l.terminalRetireAfterReceipt = map[string]*terminalConnection{}
		}
		l.terminalRetireAfterReceipt[terminalReceiptSettleID(terminalReceiptChannel(l.identity.MachineID, c), seq)] = c
	}
	if len(c.receiptOrder) < CloudTerminalReceiptsLimit {
		if _, present := c.receipts[receipt.RequestID]; !present {
			c.receiptOrder = append(c.receiptOrder, receipt.RequestID)
		}
		c.receipts[receipt.RequestID] = data
	}
	stage := ""
	if err == nil {
		if c.receiptOps == nil {
			c.receiptOps = map[uint64]string{}
		}
		if len(c.receiptOps) < CloudTerminalReceiptsLimit {
			c.receiptOps[seq] = receipt.Operation
		}
		if c.receiptRetries == nil {
			c.receiptRetries = map[uint64]receiptRetry{}
		}
		if len(c.receiptRetries) < CloudTerminalReceiptsLimit {
			c.receiptRetries[seq] = receiptRetry{receipt: receipt, attempt: attempt}
		}
		stage = l.terminalStageLocked(c, "receipt_published", receipt.Operation, receiptStatus(receipt.Status))
	}
	l.terminalMu.Unlock()
	if stage != "" {
		l.logf("%s", stage)
	}
	if err != nil {
		l.logf("cloud terminal: %s receipt was not accepted for delivery: %v", receipt.Operation, err)
	}
	return err
}

// directReceiptOperations are the requests whose receipt starts nothing on
// the machine, so it needs no relay settlement: a direct connection whose
// viewer asked for it gets them on the data channel, and its typing costs
// the relay nothing (docs/cloud-terminal-wire.md, Direct carrier).
var directReceiptOperations = map[string]bool{"input": true, "paste": true, "resize": true, "control": true, "history": true}

// sendDirectReceipt sends receipt on c's data channel when it may go there,
// and reports whether it did; otherwise the relay carries it.
func (l *Link) sendDirectReceipt(c *terminalConnection, receipt terminalReceipt, data []byte) bool {
	l.terminalMu.Lock()
	direct := c.carrier == carrierDirect && c.directReceipts && directReceiptOperations[receipt.Operation] &&
		l.terminalConnections[terminalConnectionID(c.viewer, c.id)] == c
	l.terminalMu.Unlock()
	if !direct || l.sendDirectEnvelope(c, terminalReceiptChannel(l.identity.MachineID, c), domaincloud.ClassCtl, data) != nil {
		return false
	}
	l.terminalMu.Lock()
	if len(c.receiptOrder) < CloudTerminalReceiptsLimit {
		if _, present := c.receipts[receipt.RequestID]; !present {
			c.receiptOrder = append(c.receiptOrder, receipt.RequestID)
		}
		c.receipts[receipt.RequestID] = data
	}
	stage := l.terminalStageLocked(c, "receipt_published", receipt.Operation, receiptStatus(receipt.Status)+"_direct")
	l.terminalMu.Unlock()
	if stage != "" {
		l.logf("%s", stage)
	}
	return true
}

// terminalStageLocked formats one content-free receipt stage, the daemon
// half of the browser's timeline in terminal-observation.ts, or returns ""
// when this stage is not logged. It names only a fixed stage, operation and
// outcome: no viewer, connection, request, sequence, key or terminal text.
//
// The log stays bounded without going silent. A connection logs its first
// CloudTerminalObservationRowsLimit routine stages (ok, ok_direct, delivered)
// and then every CloudTerminalStageRoutineEvery-th; failure stages (refused,
// unknown, rate_limited, viewer_offline, peer_error, ...) are counted apart, so
// the first CloudTerminalObservationRowsLimit of them are logged however much
// typing came before, then every CloudTerminalStageNotableEvery-th. n counts
// every stage, so a gap in n shows the sampling. Until 2026-10 a connection
// logged its first 128 stages and nothing after, so a break after a long
// stretch of typing never reached the log.
func (l *Link) terminalStageLocked(c *terminalConnection, stage, operation, outcome string) string {
	c.stageLines++
	if routineStageOutcomes[outcome] {
		c.stageRoutine++
		if c.stageRoutine > CloudTerminalObservationRowsLimit && c.stageRoutine%CloudTerminalStageRoutineEvery != 0 {
			return ""
		}
	} else {
		c.stageNotable++
		if c.stageNotable > CloudTerminalObservationRowsLimit && c.stageNotable%CloudTerminalStageNotableEvery != 0 {
			return ""
		}
	}
	return fmt.Sprintf("cloud terminal stage n=%d stage=%s op=%s outcome=%s",
		c.stageLines, stage, terminalStageWord(operation), terminalStageWord(outcome))
}

// routineStageOutcomes are the outcomes of a receipt that went as expected;
// every other outcome is a failure stage.
var routineStageOutcomes = map[string]bool{"ok": true, "ok_direct": true, "delivered": true}

// logTerminalStage logs one stage for a receipt that does not pass through
// sendTerminalReceipt, such as a refusal published directly.
func (l *Link) logTerminalStage(c *terminalConnection, stage, operation, outcome string) {
	l.terminalMu.Lock()
	line := l.terminalStageLocked(c, stage, operation, outcome)
	l.terminalMu.Unlock()
	if line != "" {
		l.logf("%s", line)
	}
}

var terminalStageWordPattern = regexp.MustCompile(`^[a-z_]{1,48}$`)

func terminalStageWord(word string) string {
	if word == "" {
		return "-"
	}
	if !terminalStageWordPattern.MatchString(word) {
		return "unrecognized"
	}
	return word
}

func receiptStatus(status string) string {
	if status == "ok" || status == "unknown" {
		return status
	}
	return "refused"
}

func terminalReceiptChannel(machine string, c *terminalConnection) string {
	return "termr/" + machine + "/" + c.viewer + "/" + c.id
}

func (l *Link) publishTerminal(ctx context.Context, out Outbound) error {
	if l.relay == nil {
		return ErrRelayNotReady
	}
	return l.relay.Publish(ctx, out)
}

func (l *Link) terminalOperation(ctx context.Context, svc *terminals.Service, p terminals.Principal,
	c *terminalConnection, req terminalRequest) (any, error) {
	id := terminal.ID(req.TerminalID)
	if req.Operation != "list" && req.Operation != "open" && req.Operation != "renew_connection" && req.Operation != "activate_connection" && req.Operation != "release_connection" &&
		(!id.Valid() || req.TerminalID == "") {
		return nil, terminal.Refuse(terminal.CodeInvalid, "terminal_id is invalid")
	}
	switch req.Operation {
	case "activate_connection":
		var body struct {
			OldConnection string `json:"old_connection"`
			FirstFrameSeq uint64 `json:"first_frame_seq"`
		}
		if !strictTerminalBody(req.Body, &body) || !validConnection(body.OldConnection) || body.OldConnection == c.id || body.FirstFrameSeq == 0 {
			return nil, terminal.Refuse(terminal.CodeInvalid, "activation body is invalid")
		}
		old := l.getTerminalConnection(p.Device, body.OldConnection)
		if old == nil {
			return nil, terminal.Refuse(terminal.CodeInvalid, "old connection is unavailable")
		}
		l.terminalMu.Lock()
		valid := c.rekeyPending && c.terminalID.Valid() && old.terminalID == c.terminalID &&
			c.client != "" && old.client == c.client && body.FirstFrameSeq <= c.publishedFrameSeq
		l.terminalMu.Unlock()
		control := svc.Control(c.terminalID)
		valid = valid && control.Held && !control.Unknown && control.Holder.Device == p.Device &&
			control.Client == c.client && control.Expires.After(l.opts.Now())
		if !valid {
			return nil, terminal.Refuse(terminal.CodeInvalid, "new frame and lease identity are unverified")
		}
		if c.carrier == carrierDirect {
			// Before the receipt: settling it closes the old connection,
			// and the peer must not go with it.
			l.moveDirectOwner(p.Device, c.id)
		}
		return map[string]any{"connection": c.id, "retired_connection": old.id}, nil
	case "list":
		rows, err := svc.List(ctx, p, req.ProjectID)
		if err != nil {
			return nil, err
		}
		out := make([]contract.Terminal, 0, len(rows))
		for _, row := range rows {
			out = append(out, cloudWireTerminal(row, p, req.Client))
		}
		return map[string]any{"terminals": out}, nil
	case "open":
		var body struct {
			Cols int `json:"cols"`
			Rows int `json:"rows"`
		}
		if !strictTerminalBody(req.Body, &body) || body.Cols <= 0 || body.Rows <= 0 || l.opts.TerminalProject == nil {
			return nil, terminal.Refuse(terminal.CodeInvalid, "open needs project_id, cols and rows")
		}
		dir, ok := l.opts.TerminalProject(ctx, req.ProjectID)
		if !ok {
			return nil, terminal.Refuse(terminal.CodeInvalid, "project_id is unknown")
		}
		t, err := svc.Open(ctx, p, ports.OpenTerminal{ProjectPath: dir, ProjectID: req.ProjectID, Cols: body.Cols, Rows: body.Rows})
		if err != nil {
			return nil, err
		}
		row, err := svc.Find(ctx, p, t.ID)
		if err != nil {
			return nil, err
		}
		if err := l.watchTerminal(ctx, svc, p, c, t.ID, req.Client); err != nil {
			return nil, err
		}
		return cloudWireTerminal(row, p, req.Client), nil
	case "read", "capture":
		row, err := svc.Find(ctx, p, id)
		if err != nil {
			return nil, err
		}
		if err := l.watchTerminal(ctx, svc, p, c, id, req.Client); err != nil {
			return nil, err
		}
		if req.Operation == "capture" {
			return map[string]any{"terminal_id": req.TerminalID}, nil
		}
		return cloudWireTerminal(row, p, req.Client), nil
	case "history":
		var body struct {
			Lines int `json:"lines"`
		}
		if len(req.Body) > 0 && !strictTerminalBody(req.Body, &body) {
			return nil, terminal.Refuse(terminal.CodeInvalid, "history body is invalid")
		}
		lines, err := svc.HistoryBounded(ctx, p, id, body.Lines, CloudTerminalHistoryCaptureBytesLimit)
		if err != nil {
			return nil, err
		}
		if lines == nil {
			lines = []string{}
		}
		return boundedCloudHistory(lines, req)
	case "control":
		if req.Client == "" {
			return nil, terminal.Refuse(terminal.CodeInvalid, "control needs client")
		}
		var body struct {
			Action terminals.Action `json:"action"`
		}
		if len(req.Body) > 0 && !strictTerminalBody(req.Body, &body) {
			return nil, terminal.Refuse(terminal.CodeInvalid, "control body is invalid")
		}
		var control terminals.Control
		if body.Action == "" {
			if _, err := svc.Find(ctx, p, id); err != nil {
				return nil, err
			}
			control = svc.Control(id)
		} else {
			var err error
			control, err = svc.SetControl(ctx, p, id, req.Client, body.Action)
			if err != nil {
				return nil, err
			}
		}
		unknown := control.Held && control.Holder.Device == p.Device && control.Client == req.Client && control.Unknown
		if control.Held && control.Holder.Device == p.Device && control.Client == req.Client {
			l.terminalMu.Lock()
			if l.terminalConnections[terminalConnectionID(c.viewer, c.id)] == c {
				c.client = req.Client
			}
			l.terminalMu.Unlock()
		}
		return map[string]any{"machine_incarnation": l.machineIncarnation,
			"control": cloudWireControl(control, p, req.Client), "input_state_unknown": unknown}, nil
	case "input", "paste":
		if req.Client == "" || req.Epoch == 0 || req.Seq == 0 {
			return nil, terminal.Refuse(terminal.CodeInvalid, "input needs client, epoch and seq")
		}
		var body struct {
			Data string `json:"data"`
			Text string `json:"text"`
		}
		if !strictTerminalBody(req.Body, &body) {
			return nil, terminal.Refuse(terminal.CodeInvalid, "input body is invalid")
		}
		var applied uint64
		var dup bool
		var err error
		if req.Operation == "input" {
			data, decodeErr := base64.StdEncoding.DecodeString(body.Data)
			if decodeErr != nil || len(data) == 0 {
				return nil, terminal.Refuse(terminal.CodeInvalid, "input data is invalid")
			}
			applied, dup, err = svc.Input(ctx, p, id, req.Client, req.Epoch, req.Seq, data)
		} else {
			if body.Text == "" {
				return nil, terminal.Refuse(terminal.CodeInvalid, "paste text is empty")
			}
			applied, dup, err = svc.Paste(ctx, p, id, req.Client, req.Epoch, req.Seq, body.Text)
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{"applied_through": applied, "duplicate": dup}, nil
	case "resize":
		var body struct {
			Cols int `json:"cols"`
			Rows int `json:"rows"`
		}
		if !strictTerminalBody(req.Body, &body) || req.Client == "" || req.Epoch == 0 || body.Cols <= 0 || body.Rows <= 0 {
			return nil, terminal.Refuse(terminal.CodeInvalid, "resize body is invalid")
		}
		if err := svc.Resize(ctx, p, id, req.Client, req.Epoch, body.Cols, body.Rows); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true}, nil
	case "close":
		if req.Client == "" || req.Epoch == 0 {
			return nil, terminal.Refuse(terminal.CodeInvalid, "close needs client and epoch")
		}
		if err := svc.Close(ctx, p, id, req.Client, req.Epoch); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true}, nil
	case "renew_connection":
		// The connection key has an absolute ten-minute lifetime. Renewal is
		// an authenticated liveness query, not a way to extend that lifetime.
		return l.connectionResult(c), nil
	case "release_connection":
		return map[string]any{"released": true}, nil
	default:
		return nil, terminal.Refuse(terminal.CodeInvalid, "terminal operation is unknown")
	}
}

func strictTerminalBody(raw json.RawMessage, out any) bool {
	if len(raw) == 0 {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return false
	}
	return dec.Decode(new(any)) == io.EOF
}

func cloudWireControl(c terminals.Control, viewer terminals.Principal, client string) contract.TerminalControl {
	out := contract.TerminalControl{Held: c.Held, Epoch: int64(c.Epoch)}
	if !c.Held {
		return out
	}
	same := c.Holder.Device == viewer.Device
	name := c.Holder.Name
	if name == "" {
		name = "?"
	}
	out.Holder = &contract.TerminalHolder{Name: name, Local: c.Holder.Local,
		SameDevice: same, SameClient: same && c.Client == client}
	out.ExpiresAt = float64(c.Expires.UnixMilli()) / 1000
	if out.Holder.SameClient {
		out.AppliedThrough = int64(c.Applied)
	}
	return out
}

func cloudWireTerminal(row terminals.Listed, viewer terminals.Principal, client string) contract.Terminal {
	return contract.Terminal{ID: string(row.ID), ProjectID: row.ProjectID, Created: row.Created.Unix(),
		Status: contract.TerminalStatus(row.Status), Cols: int64(row.Cols), Rows: int64(row.Rows),
		Dir: row.Dir, Control: cloudWireControl(row.Control, viewer, client)}
}

func cloudWireFrame(frame terminal.Frame) contract.TerminalFrame {
	lines := frame.Lines
	if lines == nil {
		lines = []string{}
	}
	mouse := contract.TerminalMouseMode(frame.Modes.Mouse)
	if mouse == "" {
		mouse = contract.TerminalMouseMode(terminal.MouseNone)
	}
	return contract.TerminalFrame{Rev: frame.Rev, At: float64(frame.At.UnixMilli()) / 1000,
		Cols: int64(frame.Cols), Rows: int64(frame.Rows), Lines: lines, Dead: frame.Dead,
		Cursor: contract.TerminalCursor{X: int64(frame.Cursor.X), Y: int64(frame.Cursor.Y),
			Visible: frame.Cursor.Visible, Shape: contract.TerminalCursorShape(frame.Cursor.Shape), Blinking: frame.Cursor.Blinking},
		Modes: contract.TerminalModes{AppCursor: frame.Modes.AppCursor, AppKeypad: frame.Modes.AppKeypad,
			Mouse: mouse, MouseSgr: frame.Modes.MouseSGR, Alt: frame.Modes.Alt}}
}

type terminalChangedRow struct {
	Row  int    `json:"row"`
	Line string `json:"line"`
}

// terminalScreenHash commits to the exact UTF-8 line sequence, including empty rows.
func terminalScreenHash(lines []string) (string, bool) {
	if uint64(len(lines)) > uint64(^uint32(0)) {
		return "", false
	}
	h := sha256.New()
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(lines)))
	_, _ = h.Write(size[:])
	for _, line := range lines {
		if uint64(len(line)) > uint64(^uint32(0)) {
			return "", false
		}
		binary.BigEndian.PutUint32(size[:], uint32(len(line)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(line))
	}
	return hex.EncodeToString(h.Sum(nil)), true
}

func terminalDeltaPayload(id terminal.ID, c *terminalConnection, seq uint64, frame terminal.Frame) ([]byte, bool) {
	base := c.frameBase
	if base == nil || !c.frameDeltaV1 || base.Cols != frame.Cols || base.Rows != frame.Rows ||
		base.Modes.Alt != frame.Modes.Alt || len(base.Lines) != len(frame.Lines) || c.frameBaseSeq == 0 ||
		c.frameBaseTerminalID != id ||
		frame.At.Sub(base.At) >= CloudTerminalFrameHeartbeatSecondsLimit*time.Second {
		return nil, false
	}
	hash, ok := terminalScreenHash(frame.Lines)
	if !ok {
		return nil, false
	}
	changed := make([]terminalChangedRow, 0)
	for row, line := range frame.Lines {
		if line != base.Lines[row] {
			changed = append(changed, terminalChangedRow{Row: row, Line: line})
		}
	}
	wire := cloudWireFrame(frame)
	payload, err := json.Marshal(map[string]any{"v": 1, "type": "terminal_frame_delta", "terminal_id": string(id),
		"connection": c.id, "frame_seq": seq, "base_seq": c.frameBaseSeq, "base_rev": base.Rev,
		"captured_at": wire.At, "rev": wire.Rev, "at": wire.At, "cols": wire.Cols, "rows": wire.Rows,
		"dead": wire.Dead, "cursor": wire.Cursor, "modes": wire.Modes,
		"changed_rows": changed, "screen_hash": hash})
	return payload, err == nil
}

func (l *Link) watchTerminal(ctx context.Context, svc *terminals.Service, p terminals.Principal,
	c *terminalConnection, id terminal.ID, client string) error {
	if client == "" {
		l.terminalMu.Lock()
		if c.terminalID == id {
			client = c.client
		}
		l.terminalMu.Unlock()
	}
	watch, err := svc.Watch(ctx, p, id, client)
	if err != nil {
		return err
	}
	watchCtx, cancel := context.WithCancel(context.Background())
	l.terminalMu.Lock()
	if l.terminalConnections[terminalConnectionID(c.viewer, c.id)] != c || c.denied {
		l.terminalMu.Unlock()
		watch.Stop()
		cancel()
		return context.Canceled
	}
	if c.watchCancel != nil {
		c.watchCancel()
	}
	ready := make(chan struct{})
	c.watchCancel, c.watchReady, c.terminalID, c.client = cancel, ready, id, client
	l.terminalMu.Unlock()
	go func() {
		defer watch.Stop()
		select {
		case <-ready:
		case <-watchCtx.Done():
			return
		}
		_ = watch.RunWithFrameHeartbeat(watchCtx, CloudTerminalFrameHeartbeatSecondsLimit*time.Second, func(e terminals.Event) error {
			if e.Kind != terminals.EventFrame {
				return nil
			}
			return l.sendTerminalFrame(watchCtx, svc, p, c, id, e.Frame)
		})
	}()
	return nil
}

func (l *Link) sendTerminalFrame(ctx context.Context, svc *terminals.Service, p terminals.Principal,
	c *terminalConnection, id terminal.ID, frame terminal.Frame) error {
	if err := svc.Allow(p); terminals.Unverified(err) {
		return terminals.ErrFrameDeferred // paused: offered again once verified
	} else if err != nil {
		l.revokeRegisteredTerminal(ctx, c)
		return err
	}
	l.terminalMu.Lock()
	if l.terminalConnections[terminalConnectionID(c.viewer, c.id)] != c || c.terminalID != id ||
		!c.expires.After(l.opts.Now()) || c.denied {
		l.terminalMu.Unlock()
		return context.Canceled
	}
	if c.framePending {
		l.terminalMu.Unlock()
		return terminals.ErrFrameDeferred
	}
	c.frameSeq++
	seq := c.frameSeq
	deltaState := terminalConnection{id: c.id, frameDeltaV1: c.frameDeltaV1,
		frameBase: c.frameBase, frameBaseSeq: c.frameBaseSeq, frameBaseTerminalID: c.frameBaseTerminalID}
	l.terminalMu.Unlock()
	payload, err := json.Marshal(map[string]any{"v": 1, "type": "terminal_frame",
		"terminal_id": string(id), "connection": c.id, "frame_seq": seq,
		"captured_at": float64(frame.At.UnixMilli()) / 1000, "frame": cloudWireFrame(frame)})
	if err != nil {
		return err
	}
	channel := "term/" + l.identity.MachineID + "/" + c.viewer + "/" + c.id
	if delta, ok := terminalDeltaPayload(id, &deltaState, seq, frame); ok && len(delta) < len(payload) {
		payload = delta
		channel = "termd/" + l.identity.MachineID + "/" + c.viewer + "/" + c.id
	}
	if err := svc.Allow(p); terminals.Unverified(err) {
		return terminals.ErrFrameDeferred
	} else if err != nil {
		l.revokeRegisteredTerminal(ctx, c)
		return err
	}
	l.terminalMu.Lock()
	if l.terminalConnections[terminalConnectionID(c.viewer, c.id)] != c || c.denied {
		l.terminalMu.Unlock()
		return context.Canceled
	}
	if c.framePending {
		l.terminalMu.Unlock()
		return terminals.ErrFrameDeferred
	}
	if c.carrier == carrierDirect {
		candidate := frame
		candidate.Lines = append([]string(nil), frame.Lines...)
		c.framePending = true
		c.framePendingChannel = carrierDirect
		c.framePendingAt = l.opts.Now()
		c.frameCandidate = &candidate
		c.frameCandidateSeq = seq
		c.frameCandidateTerminalID = id
		c.publishedFrameSeq = seq
		l.terminalMu.Unlock()
		if err := l.sendDirectEnvelope(c, channel, domaincloud.ClassStream, payload); err != nil {
			go l.closeTerminalConnection(c)
			return err
		}
		return nil
	}
	if l.relay == nil {
		l.terminalMu.Unlock()
		return ErrRelayNotReady
	}
	envelopeSeq, err := l.relay.PublishTracked(ctx, Outbound{Channel: channel,
		Class: string(domaincloud.ClassStream), Payload: payload, Key: c.key, KeyID: c.keyID})
	if err != nil && strings.HasPrefix(channel, "termd/") {
		// An older relay or channel vocabulary cannot accept deltas. Reissue the
		// current complete screen so the viewer remains synchronized.
		payload, _ = json.Marshal(map[string]any{"v": 1, "type": "terminal_frame",
			"terminal_id": string(id), "connection": c.id, "frame_seq": seq,
			"captured_at": float64(frame.At.UnixMilli()) / 1000, "frame": cloudWireFrame(frame)})
		channel = "term/" + l.identity.MachineID + "/" + c.viewer + "/" + c.id
		envelopeSeq, err = l.relay.PublishTracked(ctx, Outbound{Channel: channel,
			Class: string(domaincloud.ClassStream), Payload: payload, Key: c.key, KeyID: c.keyID})
	}
	if err == nil {
		c.framePending = true
		c.framePendingSeq = envelopeSeq
		c.framePendingChannel = channel
		candidate := frame
		candidate.Lines = append([]string(nil), frame.Lines...)
		c.frameCandidate = &candidate
		c.frameCandidateSeq = seq
		c.frameCandidateTerminalID = id
		c.publishedFrameSeq = seq
	}
	l.terminalMu.Unlock()
	return err
}
