package cloud

// The terminal's direct carrier (docs/cloud-terminal-wire.md, Direct carrier;
// docs/terminal-direct-path.md for why).
//
// A viewer whose browser can reach this machine gets a WebRTC data channel to
// it. The channel carries exactly the envelopes the relay would: `termi` in,
// `term`/`termd` out, each signed and sealed as before. Receipts stay on the
// relay, and so does every registration, so the relay remains the authority
// over who may hold a terminal; this file only moves bytes off the Pacific.

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"

	"github.com/sainteye/clawdline/internal/app/terminals"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

const (
	CloudTerminalDirectPeersLimit                 = 8
	CloudTerminalDirectOffersPerMinuteLimit       = 6
	CloudTerminalDirectSDPBytesLimit              = 16 << 10
	CloudTerminalDirectCandidatesLimit            = 32
	CloudTerminalDirectNegotiateSecondsLimit      = 5
	CloudTerminalDirectMessageBytesLimit          = 9 << 20
	CloudTerminalDirectChunkSecondsLimit          = 2
	CloudTerminalDirectAckSecondsLimit            = 3
	CloudTerminalDirectProbeSecondsLimit          = 1
	CloudTerminalDirectProbeUnsettledSecondsLimit = 2
	// CloudTerminalDirectGatherSecondsLimit bounds how long the machine waits
	// for its own ICE candidates before answering with what it has.
	CloudTerminalDirectGatherSecondsLimit = 3
	// directChunkUnits is the largest `d` of one chunk, in UTF-16 code units
	// as the browser counts them; the envelopes are ASCII, so bytes here.
	directChunkUnits = 16384
	directLabel      = "clawdline-terminal-v1"
	directOfferAAD   = "clawdline-direct-offer-v1/"
	carrierDirect    = "direct"
)

// directSTUN are the public STUN servers both ends use. They learn each
// end's public address and nothing else (docs/design-decisions.md).
var directSTUN = []string{"stun:stun.cloudflare.com:3478", "stun:stun.l.google.com:19302"}

// directPeer is one viewer's data channel.
//
// A peer opened by a terminal's `direct_offer` belongs to one connection,
// `owner`; see moveDirectOwner and closeTerminalConnection. A peer opened by a
// `carrier_offer` belongs to the viewer itself: `carrier` is set, `owner` stays
// empty, and no terminal connection's retirement takes it away — a page
// reading Session transcripts has no terminal to tie its carrier to, and that
// is the case this exists for. A terminal that opens later **borrows** the
// same peer rather than negotiating a second one, which is what
// `rekey_connection` with `carrier:"direct"` already asks for: it requires
// only that this viewer have an open peer.
type directPeer struct {
	l       *Link
	viewer  string
	owner   string
	carrier bool
	pc      *webrtc.PeerConnection
	dc      *webrtc.DataChannel

	mu      sync.Mutex
	open    bool
	closed  bool
	chunkID uint64
	// carrierSeq is this peer's own envelope sequence for Session answers.
	//
	// It is separate from the relay's because the browser reads a sender's
	// envelope sequence as strictly increasing, and two carriers drawing from
	// one counter reorder against each other: the answer that arrived second
	// would be read as a replay of a sequence already seen. The terminal
	// settled this the same way, with a per-connection counter checked on its
	// own (docs/cloud-terminal-wire.md).
	carrierSeq uint64

	// sendMu keeps one message's chunks contiguous on the channel.
	sendMu sync.Mutex

	// The message being reassembled, owned by the channel's read callback.
	partID    uint64
	partNext  int
	partCount int
	partBytes int
	part      strings.Builder
	partTimer *time.Timer
}

type directMessage struct {
	T          string          `json:"t"`
	E          json.RawMessage `json:"e,omitempty"`
	ID         uint64          `json:"id,omitempty"`
	I          int             `json:"i"`
	N          int             `json:"n,omitempty"`
	D          string          `json:"d,omitempty"`
	Connection string          `json:"connection,omitempty"`
	FrameSeq   uint64          `json:"frame_seq,omitempty"`
}

// directAPI builds the WebRTC API once per link: UDP only, mDNS names from a
// browser resolved but never published, and the timeouts this file's bounds
// promise.
func (l *Link) directAPI() *webrtc.API {
	l.directMu.Lock()
	defer l.directMu.Unlock()
	if l.directWebRTC != nil {
		return l.directWebRTC
	}
	se := webrtc.SettingEngine{}
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeQueryOnly)
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6})
	se.SetICETimeouts(4*time.Second, 8*time.Second, time.Second)
	if !l.directLoopback {
		se.SetIPFilter(func(ip net.IP) bool { return directAddressAllowed(ip) })
	} else {
		se.SetIncludeLoopbackCandidate(true)
	}
	l.directWebRTC = webrtc.NewAPI(webrtc.WithSettingEngine(se))
	return l.directWebRTC
}

// directAddressAllowed is the rule for every address this machine will
// gather on or send a connectivity check to: not loopback, link-local,
// multicast or unspecified. A viewer with send permission still chooses the
// private addresses a check goes to, a few per offer within the offer rate;
// docs/design-decisions.md records that.
func directAddressAllowed(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() &&
		!ip.IsMulticast() && !ip.IsUnspecified() && !ip.IsInterfaceLocalMulticast()
}

// filterDirectOffer keeps the offer's UDP candidates whose address passes
// directAddressAllowed (or is an mDNS name), and refuses an offer with more
// candidate lines than the bound.
func (l *Link) filterDirectOffer(sdp string) (string, error) {
	lines := strings.Split(sdp, "\n")
	out := make([]string, 0, len(lines))
	candidates := 0
	for _, line := range lines {
		trimmed := strings.TrimSuffix(line, "\r")
		if !strings.HasPrefix(trimmed, "a=candidate:") {
			out = append(out, line)
			continue
		}
		candidates++
		if candidates > CloudTerminalDirectCandidatesLimit {
			return "", terminal.Refuse(terminal.CodeInvalid, "the offer has too many candidates")
		}
		fields := strings.Fields(strings.TrimPrefix(trimmed, "a=candidate:"))
		if len(fields) < 8 || !strings.EqualFold(fields[2], "udp") {
			continue
		}
		address := fields[4]
		if strings.HasSuffix(address, ".local") {
			out = append(out, line)
			continue
		}
		ip := net.ParseIP(address)
		if ip == nil || (!directAddressAllowed(ip) && !(l.directLoopback && ip.IsLoopback())) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n"), nil
}

// openDirectOffer decrypts a `direct_offer` body under the connection key.
func openDirectOffer(c *terminalConnection, body json.RawMessage) (string, error) {
	var sealed struct {
		Sealed string `json:"sealed"`
	}
	if !strictTerminalBody(body, &sealed) || sealed.Sealed == "" ||
		len(sealed.Sealed) > base64.StdEncoding.EncodedLen(12+CloudTerminalDirectSDPBytesLimit+16) {
		return "", terminal.Refuse(terminal.CodeInvalid, "the offer is not a sealed SDP")
	}
	raw, err := base64.StdEncoding.DecodeString(sealed.Sealed)
	if err != nil || len(raw) < 12+16 {
		return "", terminal.Refuse(terminal.CodeInvalid, "the offer is not a sealed SDP")
	}
	block, err := aes.NewCipher(c.key.Bytes())
	if err != nil {
		return "", terminal.Refuse(terminal.CodeInvalid, "the connection key cannot open the offer")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", terminal.Refuse(terminal.CodeInvalid, "the connection key cannot open the offer")
	}
	plain, err := gcm.Open(nil, raw[:12], raw[12:], []byte(directOfferAAD+c.id))
	if err != nil || len(plain) == 0 || len(plain) > CloudTerminalDirectSDPBytesLimit {
		return "", terminal.Refuse(terminal.CodeInvalid, "the offer did not open under this connection's key")
	}
	return string(plain), nil
}

// directOfferAdmitted applies every rule of an offer except the SDP's own:
// the switch, one peer per viewer, the machine's peer bound and the
// viewer's offer rate. It reserves the viewer's place when it admits.
func (l *Link) directOfferAdmitted(viewer string) error {
	if !l.directEnabled() {
		return terminal.Refuse(terminal.CodeDirectDisabled, "this machine does not offer direct terminal connections")
	}
	now := l.opts.Now()
	l.directMu.Lock()
	defer l.directMu.Unlock()
	if l.directOffers == nil {
		l.directOffers = map[string][]time.Time{}
	}
	recent := l.directOffers[viewer][:0]
	for _, at := range l.directOffers[viewer] {
		if now.Sub(at) < time.Minute {
			recent = append(recent, at)
		}
	}
	l.directOffers[viewer] = recent
	if len(recent) >= CloudTerminalDirectOffersPerMinuteLimit {
		return terminal.Refuse(terminal.CodeBusy, "this device asked for too many direct connections this minute")
	}
	if _, held := l.directPeers[viewer]; held {
		return terminal.Refuse(terminal.CodeBusy, "this device already has a direct connection")
	}
	if len(l.directPeers) >= CloudTerminalDirectPeersLimit {
		return terminal.Refuse(terminal.CodeBusy, "this machine already serves as many direct connections as it can")
	}
	l.directOffers[viewer] = append(l.directOffers[viewer], now)
	if l.directPeers == nil {
		l.directPeers = map[string]*directPeer{}
	}
	l.directPeers[viewer] = nil // reserved while the answer is made
	return nil
}

func (l *Link) directEnabled() bool {
	return l.settings.TerminalDirect && l.transport != nil
}

// answerDirectOffer makes this machine's answer to one admitted offer. The
// caller holds the viewer's reservation and gives it back on error. `owner` is
// the terminal connection the peer belongs to, or "" with `carrier` set for a
// peer the viewer itself owns.
func (l *Link) answerDirectOffer(viewer, owner string, carrier bool, sdp string) (string, error) {
	sdp, err := l.filterDirectOffer(sdp)
	if err != nil {
		return "", err
	}
	servers := []webrtc.ICEServer{{URLs: directSTUN}}
	if l.directNoSTUN {
		servers = nil
	}
	pc, err := l.directAPI().NewPeerConnection(webrtc.Configuration{ICEServers: servers})
	if err != nil {
		return "", terminal.Refuse(terminal.CodeUnreachable, "the direct connection could not be created")
	}
	peer := &directPeer{l: l, viewer: viewer, owner: owner, carrier: carrier, pc: pc}
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != directLabel || dc.Ordered() != true {
			_ = dc.Close()
			return
		}
		peer.mu.Lock()
		if peer.dc != nil || peer.closed {
			peer.mu.Unlock()
			_ = dc.Close()
			return
		}
		peer.dc = dc
		peer.mu.Unlock()
		dc.OnOpen(func() {
			peer.mu.Lock()
			peer.open = !peer.closed
			peer.mu.Unlock()
			if carrier {
				l.logf("cloud carrier=direct opened for a viewer's Session reads")
			} else {
				l.logf("cloud terminal carrier=direct opened")
			}
		})
		dc.OnClose(func() { l.closeDirectPeer(peer, "the data channel closed") })
		dc.OnMessage(func(m webrtc.DataChannelMessage) { peer.receive(m) })
	})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed {
			l.closeDirectPeer(peer, "the peer connection "+s.String())
		}
	})
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdp}); err != nil {
		_ = pc.Close()
		return "", terminal.Refuse(terminal.CodeInvalid, "the offer is not a usable WebRTC offer")
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		_ = pc.Close()
		return "", terminal.Refuse(terminal.CodeInvalid, "the offer is not a usable WebRTC offer")
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		_ = pc.Close()
		return "", terminal.Refuse(terminal.CodeUnreachable, "the answer could not be prepared")
	}
	select {
	case <-gathered:
	case <-time.After(CloudTerminalDirectGatherSecondsLimit * time.Second):
	}
	local := pc.LocalDescription()
	if local == nil {
		_ = pc.Close()
		return "", terminal.Refuse(terminal.CodeUnreachable, "the answer could not be prepared")
	}
	l.directMu.Lock()
	if existing, reserved := l.directPeers[viewer]; !reserved || existing != nil {
		l.directMu.Unlock()
		_ = pc.Close()
		return "", terminal.Refuse(terminal.CodeBusy, "this device already has a direct connection")
	}
	l.directPeers[viewer] = peer
	l.directMu.Unlock()
	time.AfterFunc(CloudTerminalDirectNegotiateSecondsLimit*time.Second, func() {
		peer.mu.Lock()
		open := peer.open
		peer.mu.Unlock()
		if !open {
			l.closeDirectPeer(peer, "the data channel did not open in time")
		}
	})
	return local.SDP, nil
}

// releaseDirectReservation gives back a viewer's place when no peer was made.
func (l *Link) releaseDirectReservation(viewer string) {
	l.directMu.Lock()
	if peer, ok := l.directPeers[viewer]; ok && peer == nil {
		delete(l.directPeers, viewer)
	}
	l.directMu.Unlock()
}

// handleDirectOffer answers `direct_offer` off the terminal lane: gathering
// candidates can take seconds, and typing into other terminals must not wait
// for it. Its receipt is an ordinary keyed receipt on the relay.
func (l *Link) handleDirectOffer(ctx context.Context, svc *terminals.Service, p terminals.Principal,
	c *terminalConnection, req terminalRequest) {
	refuse := func(err error) {
		code, known := terminal.CodeOf(err)
		if !known {
			code = terminal.CodeUnreachable
		}
		l.sendTerminalReceipt(ctx, c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
			Connection: req.Connection, Operation: req.Operation, Status: "refused", Error: string(code)})
	}
	l.terminalMu.Lock()
	live := l.terminalConnections[terminalConnectionID(c.viewer, c.id)] == c && !c.denied && c.expires.After(l.opts.Now())
	l.terminalMu.Unlock()
	if !live {
		refuse(terminal.Refuse(terminal.CodeInvalid, "the connection is not live"))
		return
	}
	if err := svc.Allow(p); terminals.Unverified(err) {
		refuse(err) // terminal_busy: the browser stays on the relay and may offer again
		return
	} else if err != nil {
		refuse(terminal.Refuse(terminal.CodeForbidden, "this device may not use this machine's terminals"))
		return
	}
	sdp, err := openDirectOffer(c, req.Body)
	if err != nil {
		refuse(err)
		return
	}
	if err := l.directOfferAdmitted(c.viewer); err != nil {
		refuse(err)
		return
	}
	go func() {
		answer, err := l.answerDirectOffer(c.viewer, c.id, false, sdp)
		if err != nil {
			l.releaseDirectReservation(c.viewer)
			refuse(err)
			return
		}
		if l.sendTerminalReceipt(context.Background(), c, terminalReceipt{V: 1, Type: "terminal_receipt",
			RequestID: req.RequestID, Connection: req.Connection, Operation: req.Operation, Status: "ok",
			Result: map[string]any{"sdp": answer, "direct_receipts": true}}) != nil {
			l.closeDirectPeerOf(c.viewer, "the answer could not be sent")
		}
	}()
}

// directPeerOpen reports whether viewer has a peer whose channel is open.
func (l *Link) directPeerOpen(viewer string) bool {
	l.directMu.Lock()
	peer := l.directPeers[viewer]
	l.directMu.Unlock()
	if peer == nil {
		return false
	}
	peer.mu.Lock()
	defer peer.mu.Unlock()
	return peer.open && !peer.closed
}

// moveDirectOwner hands the viewer's peer to connection, the direct
// connection whose activation the machine has just accepted. A carrier peer
// is the viewer's own and is only borrowed by a terminal, so it keeps no
// owner: the terminal's connections come and go while the carrier stays.
func (l *Link) moveDirectOwner(viewer, connection string) {
	l.directMu.Lock()
	if peer := l.directPeers[viewer]; peer != nil {
		peer.mu.Lock()
		if !peer.carrier {
			peer.owner = connection
		}
		peer.mu.Unlock()
	}
	l.directMu.Unlock()
}

// directOwnerRetired runs after c left the connection table. A peer owned by
// c moves to another live direct connection of the same viewer (a rekey in
// flight), or closes. A carrier peer has no owner and outlives every terminal.
func (l *Link) directOwnerRetired(c *terminalConnection) {
	l.directMu.Lock()
	peer := l.directPeers[c.viewer]
	l.directMu.Unlock()
	if peer == nil {
		return
	}
	peer.mu.Lock()
	owned := !peer.carrier && peer.owner == c.id
	peer.mu.Unlock()
	if !owned {
		return
	}
	l.terminalMu.Lock()
	next := ""
	for _, other := range l.terminalConnections {
		if other.viewer == c.viewer && other.carrier == carrierDirect && !other.denied {
			next = other.id
			break
		}
	}
	l.terminalMu.Unlock()
	if next != "" {
		l.moveDirectOwner(c.viewer, next)
		return
	}
	l.closeDirectPeer(peer, "its connection was retired")
}

func (l *Link) closeDirectPeerOf(viewer, why string) {
	l.directMu.Lock()
	peer := l.directPeers[viewer]
	l.directMu.Unlock()
	if peer != nil {
		l.closeDirectPeer(peer, why)
	}
}

// closeDirectPeer ends a peer and retires every direct connection of its
// viewer: their frames have nowhere else to go, and the browser falls back
// to the relay by rekeying (docs/cloud-terminal-wire.md, Fallback).
func (l *Link) closeDirectPeer(peer *directPeer, why string) {
	peer.mu.Lock()
	if peer.closed {
		peer.mu.Unlock()
		return
	}
	peer.closed = true
	peer.open = false
	if peer.partTimer != nil {
		peer.partTimer.Stop()
	}
	peer.mu.Unlock()
	l.directMu.Lock()
	if l.directPeers[peer.viewer] == peer {
		delete(l.directPeers, peer.viewer)
	}
	l.directMu.Unlock()
	go func() { _ = peer.pc.Close() }()
	l.logf("cloud terminal carrier=direct closed: %s", why)
	l.terminalMu.Lock()
	direct := []*terminalConnection{}
	for _, c := range l.terminalConnections {
		if c.viewer == peer.viewer && c.carrier == carrierDirect {
			direct = append(direct, c)
		}
	}
	l.terminalMu.Unlock()
	for _, c := range direct {
		l.closeTerminalConnection(c)
	}
}

func (l *Link) closeAllDirectPeers() {
	l.directMu.Lock()
	peers := make([]*directPeer, 0, len(l.directPeers))
	for viewer, peer := range l.directPeers {
		if peer != nil {
			peers = append(peers, peer)
		} else {
			delete(l.directPeers, viewer)
		}
	}
	l.directMu.Unlock()
	for _, peer := range peers {
		l.closeDirectPeer(peer, "the relay connection ended")
	}
}

// sendDirectEnvelope seals payload on channel for a direct connection and
// writes it to the viewer's data channel. The outer sequence is the
// connection's own counter, which is what the browser's per-connection
// sequence check reads.
func (l *Link) sendDirectEnvelope(c *terminalConnection, channel string, class domaincloud.Class, payload []byte) error {
	l.directMu.Lock()
	peer := l.directPeers[c.viewer]
	l.directMu.Unlock()
	if peer == nil || l.relay == nil {
		return errDirectClosed
	}
	l.terminalMu.Lock()
	c.directSeq++
	seq := c.directSeq
	l.terminalMu.Unlock()
	envelope, err := domaincloud.Seal(payload, domaincloud.SealParams{
		Ch: channel, Seq: seq, Ts: uint64(l.opts.Now().UnixMilli()), Class: class,
		KeyID: c.keyID, Sender: l.identity.MachineID, Key: c.key, Signer: l.relay.Signer,
	})
	if err != nil {
		return err
	}
	sealed, err := envelope.CanonicalJSON()
	if err != nil {
		return err
	}
	data, err := json.Marshal(directMessage{T: "env", E: sealed})
	if err != nil {
		return err
	}
	return peer.send(data)
}

var errDirectClosed = errors.New("the direct terminal channel is not open")

// send writes one message, in chunks when it is long, with no other message
// between its chunks.
func (p *directPeer) send(data []byte) error {
	p.mu.Lock()
	dc, open := p.dc, p.open && !p.closed
	p.mu.Unlock()
	if !open || dc == nil {
		return errDirectClosed
	}
	if len(data) > CloudTerminalDirectMessageBytesLimit {
		return terminal.Refuse(terminal.CodeInvalid, "the message is larger than the direct channel carries")
	}
	p.sendMu.Lock()
	defer p.sendMu.Unlock()
	if len(data) <= directChunkUnits {
		return dc.SendText(string(data))
	}
	p.mu.Lock()
	p.chunkID++
	id := p.chunkID
	p.mu.Unlock()
	n := (len(data) + directChunkUnits - 1) / directChunkUnits
	for i := 0; i < n; i++ {
		end := min((i+1)*directChunkUnits, len(data))
		piece, err := json.Marshal(directMessage{T: "chunk", ID: id, I: i, N: n, D: string(data[i*directChunkUnits : end])})
		if err != nil {
			return err
		}
		if err := dc.SendText(string(piece)); err != nil {
			return err
		}
	}
	return nil
}

// receive is the data channel's read callback. pion calls it from one
// goroutine per channel, so the reassembly fields need no lock of their own
// beyond the timer, which closes the peer.
func (p *directPeer) receive(m webrtc.DataChannelMessage) {
	if !m.IsString || len(m.Data) > CloudTerminalDirectMessageBytesLimit {
		p.l.closeDirectPeer(p, "a message was not text or was too large")
		return
	}
	var msg directMessage
	if err := json.Unmarshal(m.Data, &msg); err != nil {
		p.l.closeDirectPeer(p, "a message was not JSON")
		return
	}
	switch msg.T {
	case "chunk":
		p.receiveChunk(msg)
	case "env", "ack":
		if p.partCount != 0 {
			p.l.closeDirectPeer(p, "a message arrived inside another one's chunks")
			return
		}
		p.deliver(msg)
	default:
		p.l.closeDirectPeer(p, "a message of an unknown kind arrived")
	}
}

func (p *directPeer) receiveChunk(msg directMessage) {
	if msg.N < 2 || msg.I < 0 || msg.I >= msg.N || len(msg.D) > directChunkUnits*3 ||
		msg.N > CloudTerminalDirectMessageBytesLimit/directChunkUnits+1 {
		p.l.closeDirectPeer(p, "a chunk was malformed")
		return
	}
	if p.partCount == 0 {
		if msg.I != 0 {
			p.l.closeDirectPeer(p, "a message began in the middle")
			return
		}
		p.partID, p.partCount, p.partNext, p.partBytes = msg.ID, msg.N, 0, 0
		p.part.Reset()
		p.mu.Lock()
		p.partTimer = time.AfterFunc(CloudTerminalDirectChunkSecondsLimit*time.Second, func() {
			p.l.closeDirectPeer(p, "a chunked message did not complete in time")
		})
		p.mu.Unlock()
	}
	if msg.ID != p.partID || msg.N != p.partCount || msg.I != p.partNext {
		p.l.closeDirectPeer(p, "a chunk arrived out of order")
		return
	}
	p.partBytes += len(msg.D)
	if p.partBytes > CloudTerminalDirectMessageBytesLimit {
		p.l.closeDirectPeer(p, "a chunked message was too large")
		return
	}
	p.part.WriteString(msg.D)
	p.partNext++
	if p.partNext < p.partCount {
		return
	}
	p.mu.Lock()
	if p.partTimer != nil {
		p.partTimer.Stop()
		p.partTimer = nil
	}
	p.mu.Unlock()
	whole := p.part.String()
	p.part.Reset()
	p.partCount, p.partNext, p.partBytes = 0, 0, 0
	var inner directMessage
	if err := json.Unmarshal([]byte(whole), &inner); err != nil || inner.T != "env" {
		p.l.closeDirectPeer(p, "a chunked message was not an envelope")
		return
	}
	p.deliver(inner)
}

func (p *directPeer) deliver(msg directMessage) {
	switch msg.T {
	case "env":
		if len(msg.E) == 0 || p.l.transport == nil {
			p.l.closeDirectPeer(p, "an envelope message was empty")
			return
		}
		// An envelope that fails the ladder is dropped and counted there,
		// exactly as on the relay; it does not close the channel.
		p.l.transport.AcceptDirect(p.viewer, msg.E)
	case "ack":
		p.l.directFrameAcked(p.viewer, msg.Connection, msg.FrameSeq)
	}
}

// directFrameAcked settles a direct connection's in-flight frame on the
// viewer's carrier acknowledgement, as a relay `delivered` ack settles one on
// the relay path.
func (l *Link) directFrameAcked(viewer, connection string, frameSeq uint64) {
	l.terminalMu.Lock()
	c := l.terminalConnections[terminalConnectionID(viewer, connection)]
	if c == nil || c.carrier != carrierDirect || !c.framePending || c.frameCandidateSeq != frameSeq {
		l.terminalMu.Unlock()
		return
	}
	c.framePending = false
	c.frameBase = c.frameCandidate
	c.frameBaseSeq = c.frameCandidateSeq
	c.frameBaseTerminalID = c.frameCandidateTerminalID
	c.frameCandidate = nil
	c.framePendingChannel = ""
	c.framePendingAt = time.Time{}
	l.terminalMu.Unlock()
}

// sweepDirect is the part of the terminal sweep that only direct
// connections need: a frame whose ack never came, and the relay probe that
// keeps the relay the authority (docs/cloud-terminal-wire.md, The relay
// stays the authority).
func (l *Link) sweepDirect(c *terminalConnection) {
	now := l.opts.Now()
	l.terminalMu.Lock()
	if c.carrier != carrierDirect || c.denied || l.terminalConnections[terminalConnectionID(c.viewer, c.id)] != c {
		l.terminalMu.Unlock()
		return
	}
	ackLate := c.framePending && !c.framePendingAt.IsZero() &&
		now.Sub(c.framePendingAt) >= CloudTerminalDirectAckSecondsLimit*time.Second
	// A probe the relay refused for its own load is asked again; the relay has
	// still to deliver one within a probe interval more than the unsettled bound.
	probeLate := (c.probePending && now.Sub(c.probeAt) >= CloudTerminalDirectProbeUnsettledSecondsLimit*time.Second) ||
		(!c.probeSince.IsZero() && now.Sub(c.probeSince) >=
			(CloudTerminalDirectProbeUnsettledSecondsLimit+CloudTerminalDirectProbeSecondsLimit)*time.Second)
	probeDue := !c.probePending && now.Sub(c.probeAt) >= CloudTerminalDirectProbeSecondsLimit*time.Second
	l.terminalMu.Unlock()
	// The peer goes too, even when a rekey not yet activated leaves it owned by
	// another connection: otherwise the browser keeps waiting on a channel the
	// machine no longer feeds, instead of falling back to the relay.
	switch {
	case ackLate:
		l.logf("cloud terminal carrier=direct retired: a frame was not acknowledged in time")
		l.closeTerminalConnection(c)
		l.closeDirectPeerOf(c.viewer, "a frame was not acknowledged in time")
	case probeLate:
		l.logf("cloud terminal carrier=direct retired: the relay did not settle a probe in time")
		l.closeTerminalConnection(c)
		l.closeDirectPeerOf(c.viewer, "the relay did not settle a probe in time")
	case probeDue:
		l.sendDirectProbe(c)
	}
}

func (l *Link) sendDirectProbe(c *terminalConnection) {
	if l.relay == nil {
		return
	}
	l.terminalMu.Lock()
	defer l.terminalMu.Unlock()
	c.probeN++
	data, err := json.Marshal(map[string]any{"v": 1, "type": "terminal_carrier_probe", "connection": c.id, "n": c.probeN})
	if err != nil {
		return
	}
	seq, err := l.relay.PublishTracked(context.Background(), Outbound{Channel: terminalReceiptChannel(l.identity.MachineID, c),
		Class: string(domaincloud.ClassCtl), Payload: data, Key: c.key, KeyID: c.keyID})
	c.probeAt = l.opts.Now()
	if c.probeSince.IsZero() {
		c.probeSince = c.probeAt
	}
	if err != nil {
		// Unsent is unsettled: the deadline above retires the connection.
		c.probePending = true
		c.probeSeq = 0
		return
	}
	c.probePending = true
	c.probeSeq = seq
}
