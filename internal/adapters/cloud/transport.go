package cloud

// The transport: dial, handshake, read, keep alive, reconnect.
//
// Ported from `Sources/CloudTransport.swift:1867-2142`. The shape is a single
// goroutine owning one socket at a time, with a generation number so that a
// dying socket's error can never clear the state of the socket that replaced
// it — the bug that generation numbers exist for, and the one the Swift
// original guards at four separate sites.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/cloud"
)

// Options configure one transport.
type Options struct {
	// RelayURL is ws:// or wss:// and either has no path or ends in
	// /v1/connect. A path this does not recognise is refused rather than
	// rewritten, because silently sending a device token to somebody else's
	// path is the mistake worth being loud about.
	RelayURL string
	// Role is "machine" for this daemon. It is a field rather than a constant
	// so that the viewer half, when it exists, is the same code.
	Role string
	// Token hands over a device token good for at least RefreshAhead.
	Token *TokenSource
	// Identity is who this machine claims to be. The challenge is checked
	// against it before anything is signed.
	Identity Identity
	// Signer is this machine's device key.
	Signer cloud.DeviceKey
	// Spool is the outbound queue, which may be nil for a read-only line.
	Spool *Spool
	// Status records what happened.
	Status *StatusRecorder
	// Replay is the shared inbound replay window. A nil one is refused:
	// starting a transport with no window means accepting every replay.
	Replay *cloud.ReplayWindow
	// Inbound is called for every envelope that passed decoding, the channel
	// check, signature verification and the replay window. It must not block.
	Inbound func(cloud.Envelope, []byte)
	// PublicKeyFor answers the pinned public key for a sender.
	PublicKeyFor cloud.PublicKeyFor
	// ContentKey opens an inbound envelope. A nil one means inbound envelopes
	// are verified and counted but not opened.
	ContentKey cloud.ContentKey
	// Log is one line per material event. nil means silence.
	Log func(string)
	// Now and Jitter exist so a test can drive the clock.
	Now    func() time.Time
	Jitter func() float64
}

// Transport is one line to the relay.
type Transport struct {
	opts Options

	mu         sync.Mutex
	conn       *Conn
	generation uint64
	state      string
	// rotating names the generation whose socket this side closed on purpose
	// to pick up a new token, so that its death is reported as a rotation
	// rather than as a fault.
	rotating uint64
	// ready is closed and replaced on each successful handshake; a waiter
	// takes a copy under the lock.
	ready chan struct{}

	stopOnce sync.Once
	stopped  chan struct{}
}

// New builds a transport. It connects nothing; Run does that.
func New(opts Options) (*Transport, error) {
	if opts.RelayURL == "" {
		return nil, errors.New("no relay url")
	}
	if opts.Role == "" {
		opts.Role = "machine"
	}
	if opts.Token == nil {
		return nil, ErrNoIdentity
	}
	if !opts.Identity.Valid() {
		return nil, ErrNoIdentity
	}
	if !opts.Signer.Valid() {
		return nil, cloud.ErrSeedLength
	}
	if opts.Replay == nil {
		return nil, errors.New("a transport with no replay window would accept every replay")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Status == nil {
		opts.Status = NewStatusRecorder(opts.Now())
	}
	return &Transport{
		opts:    opts,
		state:   StateIdle,
		ready:   make(chan struct{}),
		stopped: make(chan struct{}),
	}, nil
}

// State is what the line is doing.
func (t *Transport) State() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.state
}

// Status is the snapshot the status route answers.
func (t *Transport) Status() Status { return t.opts.Status.Snapshot() }

// Ready answers a channel that is closed once a handshake has completed. A
// caller waiting on it gets a fresh one after each reconnect.
func (t *Transport) Ready() <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ready
}

// Stop ends the line. It is safe to call more than once.
func (t *Transport) Stop() {
	t.stopOnce.Do(func() {
		close(t.stopped)
		t.mu.Lock()
		conn := t.conn
		t.conn = nil
		t.state = StateStopped
		t.mu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
	})
}

// Run keeps the line up until ctx is cancelled, Stop is called, or a terminal
// authorization failure ends it.
//
// The ladder: a failure closes the socket, the backoff is reset only if that
// socket had been up for BackoffResetAfter, and then the loop waits, doubles
// and dials again. There is no attempt cap — a Mac reconnects for ever, and
// the browser client's attempt bounds are the browser's policy, not this one's
// (`CloudBridgeLifecycle.swift:116-122`).
func (t *Transport) Run(ctx context.Context) error {
	backoff := NewBackoff()
	backoff.Jitter = t.opts.Jitter

	var lastErr error
	first := true
	for {
		select {
		case <-ctx.Done():
			t.opts.Status.Idle()
			return ctx.Err()
		case <-t.stopped:
			return nil
		default:
		}

		if !first {
			delay := backoff.Next()
			t.opts.Status.Reconnecting(FailureCode(lastErr))
			t.logf("cloud reconnect waiting reason=%s retry_in_ms=%d", FailureCode(lastErr), delay.Milliseconds())
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.stopped:
				return nil
			case <-time.After(delay):
			}
		}
		first = false

		connectedAt := t.opts.Now()
		err := t.session(ctx)
		if err == nil {
			// A clean close from the far end is still a disconnection.
			err = ErrConnClosed
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, ErrStopped) {
			return nil
		}

		upFor := t.opts.Now().Sub(connectedAt)

		// Token expiry is asked about *before* terminal authorization,
		// because `unauthorized` is in both tables and an in-band one means
		// the token aged out on an open socket, not that the credential is
		// wrong (`CloudTransport.swift:2055-2056`).
		switch {
		case IsTokenExpiry(err):
			t.opts.Token.Forget()
			t.logf("cloud token refused in band reason=%s, fetching a new one", FailureCode(err))
		case IsTerminalAuthorization(err):
			t.opts.Status.Stopped(FailureCode(err))
			t.logf("cloud stopped reason=%s", FailureCode(err))
			t.setState(StateStopped)
			return err
		}

		backoff.ResetIfStable(upFor)
		lastErr = err
	}
}

// ErrStopped is Run's answer when Stop was called.
var ErrStopped = errors.New("the transport was stopped")

// session opens one socket, handshakes, and reads until something ends it.
func (t *Transport) session(ctx context.Context) error {
	openCtx, cancel := context.WithTimeout(ctx, OpeningTimeout)
	defer cancel()

	token, err := t.opts.Token.Token(openCtx)
	if err != nil {
		return err
	}

	conn, err := Dial(t.connectURL(), DialOptions{
		Header:           http.Header{"Authorization": []string{"Bearer " + token.Token}},
		HandshakeTimeout: OpeningTimeout,
	})
	if err != nil {
		// A 401/403 at the upgrade is the credential being wrong, and the
		// cached token is dropped so a retry cannot present the same one.
		if IsTerminalAuthorization(err) {
			t.opts.Token.Forget()
		}
		return err
	}
	defer conn.Close()

	if err := t.handshake(conn); err != nil {
		return err
	}

	generation := t.promote(conn, token.ExpiresAt)
	t.logf("cloud ready account=%s machine=%s generation=%d", t.opts.Identity.AccountID, t.opts.Identity.MachineID, generation)

	// Three things run on one socket: the keepalive, the token rotation and
	// the reader. The first two only ever *close* the socket, which is how
	// they hand control back to the reader without a second channel.
	done := make(chan struct{})
	defer close(done)
	go t.keepalive(conn, done)
	go t.rotateToken(conn, token.ExpiresAt, generation, done)
	if t.opts.Spool != nil {
		go t.drain(conn, done, true)
	}

	err = t.read(ctx, conn)
	t.demote(generation)
	if t.tookRotation(generation) {
		// This side closed the socket on purpose; say so rather than reporting
		// a fault every four minutes on a perfectly healthy machine.
		return ErrTokenRotated
	}
	return err
}

func (t *Transport) connectURL() string {
	url := t.opts.RelayURL
	// A relay URL with no path gets the connect path; one that already names
	// it is left alone. Anything else is somebody's mistake and Dial will
	// send the token there, so it is refused at New time instead — see
	// ValidateRelayURL.
	if !hasConnectPath(url) {
		url = trimSlash(url) + "/v1/connect"
	}
	return url + "?role=" + t.opts.Role
}

// handshake is the two-step challenge, docs/cloud-wire.md §7.1.
func (t *Transport) handshake(conn *Conn) error {
	deadline := t.opts.Now().Add(AuthenticationTimeout)
	data, err := conn.Read(deadline)
	if err != nil {
		if isTimeout(err) {
			return ErrChallengeTimeout
		}
		return err
	}
	challenge, err := decodeChallenge(data)
	if err != nil {
		return err
	}

	// Before anything is signed, the challenge is checked against the identity
	// this machine is pinned to. Signing a challenge that names another
	// account turns this device key into an oracle for that account.
	if challenge.Account != t.opts.Identity.AccountID || challenge.Device != t.opts.Identity.MachineID {
		return fmt.Errorf("%w: the challenge names %s/%s, this machine is %s/%s",
			ErrIdentityBinding, challenge.Account, challenge.Device,
			t.opts.Identity.AccountID, t.opts.Identity.MachineID)
	}

	signature := t.opts.Signer.Sign([]byte(ChallengeString(challenge.Account, challenge.Device, challenge.Challenge)))
	hello, err := json.Marshal(HelloFrame{Type: FrameHello, Sig: base64.StdEncoding.EncodeToString(signature)})
	if err != nil {
		return err
	}
	if err := conn.WriteText(hello, t.opts.Now().Add(AuthenticationTimeout)); err != nil {
		return err
	}

	deadline = t.opts.Now().Add(AuthenticationTimeout)
	data, err = conn.Read(deadline)
	if err != nil {
		if isTimeout(err) {
			return ErrReadyTimeout
		}
		return err
	}
	if _, err := decodeReady(data, challenge, t.opts.Role); err != nil {
		return err
	}
	return nil
}

// promote installs the socket as the current one and answers its generation.
func (t *Transport) promote(conn *Conn, tokenExpiry time.Time) uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.generation++
	t.conn = conn
	t.state = StateConnected
	close(t.ready)
	t.ready = make(chan struct{})
	t.opts.Status.Ready(t.opts.Now(), tokenExpiry)
	return t.generation
}

// demote clears the socket, but only if it is still the current one.
func (t *Transport) demote(generation uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.generation != generation {
		return
	}
	t.conn = nil
	if t.state == StateConnected {
		t.state = StateReconnecting
	}
}

func (t *Transport) tookRotation(generation uint64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.rotating == generation {
		t.rotating = 0
		return true
	}
	return false
}

func (t *Transport) setState(state string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = state
}

// keepalive sends `{"type":"ping"}` every KeepaliveInterval. A send that fails
// closes the socket, which is how the reader finds out.
func (t *Transport) keepalive(conn *Conn, done <-chan struct{}) {
	ticker := time.NewTicker(KeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.stopped:
			return
		case <-ticker.C:
			if err := conn.WriteText(PingFrame, t.opts.Now().Add(KeepaliveInterval)); err != nil {
				t.logf("cloud keepalive failed reason=%s", FailureCode(err))
				_ = conn.Close()
				return
			}
		}
	}
}

// rotateToken closes the socket a minute before the device token expires, so
// that the next dial carries a fresh one. Waiting for the relay to close it
// costs one refused frame and a backoff rung.
func (t *Transport) rotateToken(conn *Conn, expiresAt time.Time, generation uint64, done <-chan struct{}) {
	if expiresAt.IsZero() {
		return
	}
	wait := time.Until(expiresAt) - RefreshAhead
	if wait <= 0 {
		wait = time.Until(expiresAt)
	}
	if wait <= 0 {
		return
	}
	select {
	case <-done:
		return
	case <-t.stopped:
		return
	case <-time.After(wait):
	}
	t.mu.Lock()
	if t.generation != generation {
		t.mu.Unlock()
		return
	}
	t.rotating = generation
	t.mu.Unlock()
	t.opts.Token.Forget()
	t.logf("cloud rotating the device token")
	_ = conn.Close()
}

// read is the receive loop.
func (t *Transport) read(ctx context.Context, conn *Conn) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.stopped:
			return ErrStopped
		default:
		}
		data, err := conn.Read(t.opts.Now().Add(ReceiveTimeout))
		if err != nil {
			if isTimeout(err) {
				return ErrReceiveTimeout
			}
			return err
		}
		if err := t.handle(conn, data); err != nil {
			return err
		}
	}
}

// handle deals with one inbound frame.
//
// Only a frame that ends the connection returns an error. A malformed ack, an
// unknown status, an uncorrelated error: all of those are logged and the
// socket kept, because the alternative is that one bad frame from the relay
// costs every open session its line.
func (t *Transport) handle(conn *Conn, data []byte) error {
	kind, err := frameType(data)
	if err != nil {
		t.logf("cloud dropped a frame reason=%s", err)
		return nil
	}
	switch kind {
	case FramePing:
		return conn.WriteText([]byte(`{"type":"pong"}`), t.opts.Now().Add(KeepaliveInterval))
	case FramePong, FrameSubscriptions:
		return nil
	case FrameError:
		relay := decodeError(data)
		if terminalRelayCodes[relay.Code] || tokenExpiryCodes[relay.Code] {
			return relay
		}
		// Everything else — over_capacity, rate_limited, internal — arrives
		// without a channel or a sequence, so there is nothing to correlate
		// it with and nothing to undo. The Swift transport logs it and keeps
		// the socket (`CloudTransport.swift:2309-2310`); so does this.
		t.logf("cloud uncorrelated relay error code=%s", relay.Code)
		return nil
	case FrameAck:
		t.handleAck(data)
		return nil
	case FramePublishError:
		t.handlePublishError(data)
		return nil
	case FrameEnvelope:
		t.handleEnvelope(data)
		return nil
	}
	t.logf("cloud ignored an unknown frame type=%s", kind)
	return nil
}

func (t *Transport) handleAck(data []byte) {
	var frame AckFrame
	if err := json.Unmarshal(data, &frame); err != nil || frame.Ch == "" {
		t.logf("cloud dropped a receipt reason=malformed_ack")
		return
	}
	t.logf("cloud ack seq=%d ch=%s status=%s fanout=%d", frame.Seq, frame.Ch, frame.Status, frame.Fanout)
	var kind SettleKind
	switch frame.Status {
	case AckDelivered:
		kind = SettleDelivered
		t.opts.Status.Acked()
	case AckViewerOffline, AckMachineOffline:
		kind = SettleViewerOffline
	default:
		t.logf("cloud dropped a receipt reason=unknown_ack_status status=%s", frame.Status)
		return
	}
	if t.opts.Spool == nil {
		return
	}
	result, err := t.opts.Spool.Settle(frame.Seq, frame.Ch, kind)
	if err != nil {
		t.logf("cloud could not settle seq=%d reason=%v", frame.Seq, err)
		return
	}
	if result == SettleResultLateIgnored {
		// A receipt for a row that is already terminal. Counted, never acted
		// on: reviving a burned row would re-send bytes whose sequence the
		// far side has already claimed.
		t.logf("cloud receipt ignored seq=%d ch=%s reason=already_settled", frame.Seq, frame.Ch)
	}
}

func (t *Transport) handlePublishError(data []byte) {
	var frame PublishErrorFrame
	if err := json.Unmarshal(data, &frame); err != nil || frame.Ch == "" {
		t.logf("cloud dropped a refusal reason=malformed_publish_error")
		return
	}
	t.opts.Status.PublishError()
	t.logf("cloud publish refused seq=%d ch=%s code=%s field=%s", frame.Seq, frame.Ch, frame.Code, frame.Field)
	if t.opts.Spool == nil {
		return
	}
	if _, err := t.opts.Spool.Settle(frame.Seq, frame.Ch, SettlePeerError); err != nil {
		t.logf("cloud could not settle a refusal seq=%d reason=%v", frame.Seq, err)
	}
}

// handleEnvelope runs the inbound admission ladder, docs/cloud-wire.md §9.4.
//
// The order is load-bearing and it is the Swift order: decode, channel,
// signature, and **only then** the replay claim. Claiming first would let an
// unauthenticated envelope spend a sequence number that the real sender then
// could not use (`CloudTransport.swift:2371-2374`).
func (t *Transport) handleEnvelope(data []byte) {
	var frame EnvelopeFrame
	if err := json.Unmarshal(data, &frame); err != nil || len(frame.Envelope) == 0 {
		t.opts.Status.Dropped(DropMalformed)
		return
	}
	envelope, err := cloud.DecodeEnvelope(frame.Envelope)
	if err != nil {
		t.opts.Status.Dropped(DropMalformed)
		t.logf("cloud dropped an envelope reason=%s detail=%v", DropMalformed, err)
		return
	}
	// A machine receives `ctl/<its own machine>` and `ho/`. Anything else on
	// this socket is not addressed to it, whatever the relay thought.
	if !t.deliverable(envelope.Ch) {
		t.opts.Status.Dropped(DropWrongChannel)
		t.logf("cloud dropped an envelope reason=%s ch=%s", DropWrongChannel, envelope.Ch)
		return
	}
	if t.opts.PublicKeyFor == nil {
		t.opts.Status.Dropped(DropRosterUnreadable)
		return
	}
	if _, known := t.opts.PublicKeyFor(envelope.Sender); !known {
		t.opts.Status.Dropped(DropUnknownSender)
		t.logf("cloud dropped an envelope reason=%s sender=%s", DropUnknownSender, envelope.Sender)
		return
	}
	if !envelope.Verify(t.opts.PublicKeyFor) {
		t.opts.Status.Dropped(DropBadSignature)
		t.logf("cloud dropped an envelope reason=%s sender=%s seq=%d", DropBadSignature, envelope.Sender, envelope.Seq)
		return
	}

	switch t.opts.Replay.Claim(envelope.Sender, envelope.Seq) {
	case cloud.ReplayRefused:
		t.opts.Status.Dropped(DropReplay)
		t.logf("cloud dropped an envelope reason=%s sender=%s seq=%d", DropReplay, envelope.Sender, envelope.Seq)
		return
	case cloud.ReplaySenderCapacity:
		t.opts.Status.Dropped(DropReplayWindowFull)
		t.logf("cloud dropped an envelope reason=%s sender=%s", DropReplayWindowFull, envelope.Sender)
		return
	}

	var plaintext []byte
	if t.opts.ContentKey.Valid() {
		plaintext, err = envelope.Open(t.opts.ContentKey, t.opts.PublicKeyFor)
		if err != nil {
			// The claim is **not** given back. It was spent when the envelope
			// authenticated, and a sender that re-sends the same sequence
			// after a decrypt failure is replaying
			// (`CloudTransport.swift:2449-2450`).
			t.opts.Status.Dropped(DropDecryptFailed)
			t.logf("cloud dropped an envelope reason=%s sender=%s seq=%d", DropDecryptFailed, envelope.Sender, envelope.Seq)
			return
		}
	}
	t.opts.Status.Inbound()
	if t.opts.Inbound != nil {
		t.opts.Inbound(envelope, plaintext)
	}
}

// deliverable reports whether this machine is the intended reader of a
// channel. A machine receives its own `ctl/` and any `ho/`; it never receives
// another machine's anything.
func (t *Transport) deliverable(channel string) bool {
	switch {
	case channel == "ctl/"+t.opts.Identity.MachineID:
		return true
	case len(channel) > 3 && channel[:3] == "ho/":
		return true
	}
	return false
}

// Publish seals nothing and sends the exact bytes it is given. The caller
// owns sealing, because the seal has to happen against the sequence the spool
// handed out and re-sealing on a re-send would be a second identity for one
// command.
func (t *Transport) Publish(envelope []byte) error {
	t.mu.Lock()
	conn := t.conn
	t.mu.Unlock()
	if conn == nil {
		return ErrNotConnected
	}
	if err := conn.WriteText(PublishFrame(envelope), t.opts.Now().Add(OpeningTimeout)); err != nil {
		// A write that failed leaves the socket in an unknown state; closing
		// it is what makes the reader notice and reconnect.
		_ = conn.Close()
		return err
	}
	t.opts.Status.Published()
	return nil
}

// Subscribe opens transcript or handoff channels on this connection.
func (t *Transport) Subscribe(channels ...string) error {
	frame, err := SubscribeFrame(FrameSubscribe, channels)
	if err != nil {
		return err
	}
	t.mu.Lock()
	conn := t.conn
	t.mu.Unlock()
	if conn == nil {
		return ErrNotConnected
	}
	return conn.WriteText(frame, t.opts.Now().Add(OpeningTimeout))
}

// drain writes what the spool has. On a reconnect it first re-sends every row
// that was already written and never answered, in ascending sequence order,
// from a list snapshotted **once** — a row settling mid-drain must not be able
// to cause a second write or a skipped one.
func (t *Transport) drain(conn *Conn, done <-chan struct{}, reconnect bool) {
	if reconnect {
		for _, seq := range t.opts.Spool.InFlight() {
			sealed, err := t.opts.Spool.Resend(seq)
			if err != nil {
				continue
			}
			if err := conn.WriteText(PublishFrame(sealed), t.opts.Now().Add(OpeningTimeout)); err != nil {
				t.logf("cloud re-send failed seq=%d reason=%s", seq, FailureCode(err))
				return
			}
			t.logf("cloud re-sent seq=%d", seq)
		}
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.stopped:
			return
		case <-ticker.C:
		}
		t.opts.Spool.BurnExpired()
		for {
			disposition := t.opts.Spool.SendNext()
			if disposition.Row == nil {
				break
			}
			if err := conn.WriteText(PublishFrame(disposition.Row.Sealed), t.opts.Now().Add(OpeningTimeout)); err != nil {
				// The row stays `sent`. Whether those bytes reached the relay
				// is unknown, and only the attempt window decides.
				t.logf("cloud publish failed seq=%d reason=%s", disposition.Row.Seq, FailureCode(err))
				_ = conn.Close()
				return
			}
			t.opts.Status.Published()
		}
	}
}

func (t *Transport) logf(format string, args ...any) {
	if t.opts.Log == nil {
		return
	}
	t.opts.Log(fmt.Sprintf(format, args...))
}

// isTimeout reports a deadline having passed rather than the peer having done
// something.
func isTimeout(err error) bool {
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}

func hasConnectPath(url string) bool {
	return len(url) >= len("/v1/connect") && url[len(url)-len("/v1/connect"):] == "/v1/connect"
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
