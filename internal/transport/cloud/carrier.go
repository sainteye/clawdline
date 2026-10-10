package cloud

// The viewer's shared direct carrier (docs/cloud-terminal-wire.md, Session
// reads on the carrier).
//
// `direct.go` opens a data channel for a terminal connection. This file opens
// the same channel for a viewer that has no terminal at all, so that a phone
// reading a transcript gets the local network instead of a round trip to San
// Jose, and so that a terminal opened later borrows that one channel rather
// than negotiating a second (the machine allows one peer per viewer).
//
// Two things are deliberately not here. There is no new relay channel: the
// offer is an ordinary signed `termi` request, which only this machine
// receives, and the answer is an ordinary read answer on the machine's reply
// channel, which the page already reads `sessions.list` from. And there is no
// new authority: the offer is admitted by exactly the terminal's rules, and
// every read that then arrives on the channel is authorized again, one read at
// a time, by the same `read_transcript` check as a read off the relay.

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/app/terminals"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

// carrierOfferOperation is the `termi` operation that opens a carrier.
const carrierOfferOperation = "carrier_offer"

// carrierAnswerAAD binds a sealed answer SDP to the offer it answers.
const carrierAnswerAAD = "clawdline-direct-answer-v1/"

// carrierOfferBody is the one field a `carrier_offer` carries.
//
// The offer SDP is in the clear inside the envelope. Nobody but this machine
// receives a `termi` envelope, and its plaintext is already sealed under the
// account content key — a second seal would hide the viewer's addresses from
// the one reader allowed to have them. The **answer** is sealed again, because
// it travels on a channel every device of the account may read and it carries
// this machine's own addresses, which the relay has never seen.
type carrierOfferBody struct {
	SDP string `json:"sdp"`
}

// handleCarrierOffer answers a `carrier_offer` off the terminal lane.
//
// Gathering candidates takes a round trip to a STUN server, so the answer is
// made on a goroutine of its own: a read waiting in the ordinary Cloud queue
// must not wait behind it, and neither must someone typing into a terminal.
func (l *Link) handleCarrierOffer(ctx context.Context, svc *terminals.Service, p terminals.Principal, req terminalRequest) {
	viewer := p.Device
	refuse := func(err error) {
		code, known := terminal.CodeOf(err)
		if !known {
			code = terminal.CodeUnreachable
		}
		status := 503
		switch code {
		case terminal.CodeForbidden:
			status = 403
		case terminal.CodeInvalid:
			status = 400
		}
		l.publishCarrierAnswer(ctx, req.RequestID, nil, status, string(code))
	}
	key, err := parseConnectionKey(req)
	if err != nil {
		refuse(err)
		return
	}
	var body carrierOfferBody
	if !strictTerminalBody(req.Body, &body) || body.SDP == "" || len(body.SDP) > CloudTerminalDirectSDPBytesLimit {
		refuse(terminal.Refuse(terminal.CodeInvalid, "the carrier offer is not an SDP"))
		return
	}
	if err := svc.Allow(p); terminals.Unverified(err) {
		refuse(err) // terminal_busy: the page stays on the relay and may offer again
		return
	} else if err != nil {
		refuse(terminal.Refuse(terminal.CodeForbidden, "this device may not open a direct carrier to this machine"))
		return
	}
	l.supersedeCarrier(viewer)
	if err := l.directOfferAdmitted(viewer); err != nil {
		refuse(err)
		return
	}
	go func() {
		answer, err := l.answerDirectOffer(viewer, "", true, body.SDP)
		if err != nil {
			l.releaseDirectReservation(viewer)
			refuse(err)
			return
		}
		sealed, err := sealCarrierAnswer(key, req.Connection, answer)
		if err != nil {
			l.closeDirectPeerOf(viewer, "the carrier answer could not be sealed")
			refuse(terminal.Refuse(terminal.CodeUnreachable, "the carrier answer could not be sealed"))
			return
		}
		// `direct_receipts` is the same offer the terminal's own `direct_offer`
		// makes: a terminal that borrows this carrier may ask for its everyday
		// receipts on the channel rather than the relay.
		if l.publishCarrierAnswer(context.Background(), req.RequestID,
			map[string]any{"carrier": req.Connection, "sdp_sealed": sealed, "direct_receipts": true},
			200, "") != nil {
			l.closeDirectPeerOf(viewer, "the carrier answer could not be sent")
		}
	}()
}

// publishCarrierAnswer puts one carrier answer on this machine's reply channel
// in the exact shape of every other read answer, so the page's existing read
// machinery settles it without a new rule (`cloudops.Bridge.contentReadAnswer`).
func (l *Link) publishCarrierAnswer(ctx context.Context, request string, body map[string]any, status int, code string) error {
	if l.relay == nil {
		return ErrRelayNotReady
	}
	answer := map[string]any{
		"read":       "read:" + request,
		"status":     status,
		"machine_id": l.identity.MachineID,
		"session_id": cloudops.MachineReplySession,
	}
	if code != "" {
		answer["error"] = map[string]any{"code": code,
			"message": "This machine could not open a direct carrier for this device."}
	} else {
		answer["body"] = body
	}
	payload, err := json.Marshal(answer)
	if err != nil {
		return err
	}
	channel := AnswerChannel(l.identity.MachineID, cloudops.MachineReplySession)
	if err := domaincloud.ProducibleChannel(channel); err != nil {
		return err
	}
	err = l.relay.Publish(ctx, Outbound{Channel: channel, Class: string(domaincloud.ClassStream),
		Payload: payload, Headroom: true,
		Reply: Reply{Name: "read:" + request, Status: status, Code: code}})
	if err != nil {
		l.logf("cloud carrier: a carrier answer was not delivered: %v", err)
	}
	return err
}

// sealCarrierAnswer seals the machine's answer SDP under the key the viewer
// minted for it. The output is base64 of nonce||ciphertext, which is the shape
// the offer uses in the other direction (`openDirectOffer`).
func sealCarrierAnswer(key domaincloud.ContentKey, connection, sdp string) (string, error) {
	block, err := aes.NewCipher(key.Bytes())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, nonce, []byte(sdp), []byte(carrierAnswerAAD+connection))
	return base64.StdEncoding.EncodeToString(append(nonce, sealed...)), nil
}

// sendCarrierEnvelope seals one Session answer for a viewer's carrier and
// writes it to the data channel. This is `Relay.Direct`.
//
// The answer is sealed under the account content key, exactly as the relay
// would seal it, so the page opens it with the key it already holds. What is
// the carrier's own is the envelope sequence: the browser reads a sender's
// sequence as strictly increasing, and an answer that travelled the short way
// must not be able to sit in front of a relay answer's number.
func (l *Link) sendCarrierEnvelope(viewer, channel string, class domaincloud.Class, payload []byte) error {
	if !strings.HasPrefix(channel, "t/") {
		// Only a read's answer travels this way. A status row or an
		// orchestrator snapshot is the relay's retained value, and moving one
		// here would leave a reconnecting page realigning from a row this
		// machine never published there.
		return ErrNoDirectCarrier
	}
	l.directMu.Lock()
	peer := l.directPeers[viewer]
	l.directMu.Unlock()
	if peer == nil || l.relay == nil {
		return ErrNoDirectCarrier
	}
	peer.mu.Lock()
	if !peer.open || peer.closed {
		peer.mu.Unlock()
		return ErrNoDirectCarrier
	}
	peer.carrierSeq++
	seq := peer.carrierSeq
	peer.mu.Unlock()
	envelope, err := domaincloud.Seal(payload, domaincloud.SealParams{
		Ch: channel, Seq: seq, Ts: uint64(l.opts.Now().UnixMilli()), Class: class,
		KeyID: l.relay.keyID(), Sender: l.identity.MachineID, Key: l.relay.Secret, Signer: l.relay.Signer,
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

// mirrorStatusRow puts one Session row this machine has just published on
// every open carrier as well. This is `Relay.MirrorStatus`.
//
// It sends the very envelope the relay will deliver — same channel, same
// sequence, same seal — rather than resealing it per peer the way an answer is
// resealed. The browser orders a row against this machine's other rows by that
// number, so a carrier number would make a current row look older than the
// list it belongs to. The carrier is a second road for the envelope, not a
// second numbering, and the page accepts it from either road.
//
// Measured on 2026-10-11: the relay publish of `s/<machine>/<session>` settled
// `fanout=0` — no device was subscribed — while the page that wanted that row
// was spending a relay round trip and one of its eight subscription channels
// to ask for it. Nothing is spooled here and nothing is retried: a data
// channel either delivers the row or is gone, and the page that loses one is
// the page that goes back to the relay for it.
func (l *Link) mirrorStatusRow(channel string, sealed []byte) {
	data, err := json.Marshal(directMessage{T: "env", E: sealed})
	if err != nil {
		return
	}
	l.directMu.Lock()
	peers := make([]*directPeer, 0, len(l.directPeers))
	for _, peer := range l.directPeers {
		if peer != nil {
			peers = append(peers, peer)
		}
	}
	l.directMu.Unlock()
	for _, peer := range peers {
		// A channel that will not take it is not a failure of this publish:
		// the row is on the relay, where that page reads it.
		_ = peer.send(data)
	}
}

// supersedeCarrier lets go of the carrier a viewer already holds, because that
// viewer is asking for a new one.
//
// Measured on 2026-10-11: a page's carrier went at 04:17 without this machine
// hearing it go — no `OnClose`, no connection state change, nothing in
// daemon.log — and every offer the page made afterwards was refused `busy`
// because this machine still had the peer. A carrier has no idle bound on
// purpose, so nothing else would ever have retired it: that page read over the
// relay for the rest of its life (63 reads, all 922-1031 ms, while the carrier
// answered in 610 ms). The closed side cannot be the only one that may let go.
//
// The newest offer wins, and only over a carrier: a peer a terminal negotiated
// is borrowed by the page rather than offered for (terminal-transport.ts), and
// a reservation still being answered is left to the busy refusal. Closing one
// retires that viewer's direct terminal connections, which rekey back to the
// relay like any other carrier loss.
func (l *Link) supersedeCarrier(viewer string) {
	l.directMu.Lock()
	peer := l.directPeers[viewer]
	l.directMu.Unlock()
	if peer == nil || !peer.carrier {
		return
	}
	l.closeDirectPeer(peer, "its viewer offered a new carrier")
}

// sweepDirectCarriers closes a carrier whose viewer this machine or the
// account has thrown out. It is defence in depth rather than the gate: every
// read on a carrier is authorized by itself, so a revoked device reading one
// is refused whether or not its channel is still up. What this adds is that
// the channel does not stay up.
//
// "Cannot tell right now" closes nothing. The roster being unreadable is not a
// revocation, and tearing down a working channel over it would move every read
// back onto the relay for as long as the account API is unreachable.
func (l *Link) sweepDirectCarriers() {
	l.directMu.Lock()
	carriers := make([]*directPeer, 0, len(l.directPeers))
	for _, peer := range l.directPeers {
		if peer != nil && peer.carrier {
			carriers = append(carriers, peer)
		}
	}
	l.directMu.Unlock()
	for _, peer := range carriers {
		if verdict, why := l.terminalAuthority(peer.viewer, true); verdict == TerminalDenied {
			l.closeDirectPeer(peer, "the carrier's device may no longer read this machine: "+why)
		}
	}
}
