package cloud

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	domain "github.com/sainteye/clawdline-go/internal/domain/cloud"
)

// tokenServer answers the one control-plane route the transport needs.
func tokenServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tokens/device" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(DeviceToken{
			Token: "a-device-token", TokenType: "Bearer",
			ExpiresIn: 300, ExpiresAt: time.Now().Add(5 * time.Minute), Kid: "dev",
		})
	}))
	t.Cleanup(server.Close)
	return server
}

type testLine struct {
	relay     *fakeRelay
	transport *Transport
	spool     *Spool
	status    *StatusRecorder
	key       domain.DeviceKey
	secret    domain.ContentKey
	identity  Identity
	logs      chan string
}

func newTestLine(t *testing.T) *testLine {
	t.Helper()
	key, err := domain.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatalf("device key: %v", err)
	}
	secret, err := domain.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatalf("master secret: %v", err)
	}
	identity := Identity{AccountID: "usr_test", MachineID: "mac_test", MachineCredential: "credential"}
	relay := newFakeRelay(identity.AccountID, identity.MachineID, key.PublicKey())
	t.Cleanup(relay.Close)

	spool, err := NewSpool(DefaultSpoolLimits(), &MemoryFence{}, time.Now)
	if err != nil {
		t.Fatalf("spool: %v", err)
	}
	status := NewStatusRecorder(time.Now())
	logs := make(chan string, 256)

	transport, err := New(Options{
		RelayURL: relay.URL(),
		Role:     "machine",
		Token:    &TokenSource{Client: NewAccountClient(tokenServer(t).URL), Credential: identity.MachineCredential},
		Identity: identity,
		Signer:   key,
		Spool:    spool,
		Status:   status,
		Replay:   domain.NewReplayWindow(0),
		PublicKeyFor: func(sender string) (ed25519.PublicKey, bool) {
			if sender == "viewer-1" {
				return key.PublicKey(), true
			}
			return nil, false
		},
		ContentKey: secret,
		Log: func(line string) {
			select {
			case logs <- line:
			default:
			}
		},
		// A fixed jitter makes the ladder's numbers exact rather than merely
		// plausible.
		Jitter: func() float64 { return 0.5 },
	})
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	return &testLine{relay: relay, transport: transport, spool: spool, status: status, key: key, secret: secret, identity: identity, logs: logs}
}

func (l *testLine) run(t *testing.T) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = l.transport.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		l.transport.Stop()
		<-done
	})
	select {
	case <-l.transport.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("the handshake did not complete")
	}
	return cancel
}

// The whole handshake, over a real socket: challenge, a signature over the
// four-field string, ready.
func TestTheHandshakeCompletes(t *testing.T) {
	t.Parallel()
	line := newTestLine(t)
	line.run(t)
	if got := line.transport.State(); got != StateConnected {
		t.Fatalf("state: %s", got)
	}
	if line.relay.Connections() != 1 {
		t.Fatalf("connections: %d", line.relay.Connections())
	}
}

// A challenge that names another account is refused **before** anything is
// signed. A device key that signs whatever it is handed is an oracle for every
// account on the relay.
func TestAChallengeForAnotherAccountIsNeverSigned(t *testing.T) {
	t.Parallel()
	key, err := domain.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatalf("device key: %v", err)
	}
	// The relay names a different account than the machine's identity.
	relay := newFakeRelay("usr_somebody_else", "mac_test", key.PublicKey())
	defer relay.Close()

	transport, err := New(Options{
		RelayURL: relay.URL(),
		Token:    &TokenSource{Client: NewAccountClient(tokenServer(t).URL), Credential: "credential"},
		Identity: Identity{AccountID: "usr_test", MachineID: "mac_test", MachineCredential: "credential"},
		Signer:   key,
		Replay:   domain.NewReplayWindow(0),
	})
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	conn, err := Dial(relay.URL()+"?role=machine", DialOptions{
		Header:           http.Header{"Authorization": []string{"Bearer t"}},
		HandshakeTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	err = transport.handshake(conn)
	if err == nil || FailureCode(err) != "identity_binding" {
		t.Fatalf("handshake against a foreign account: %v (%s)", err, FailureCode(err))
	}
}

// A socket that dies is redialled, and the ladder's first rung is 250 ms
// however long the outage.
func TestADroppedSocketIsRedialled(t *testing.T) {
	t.Parallel()
	line := newTestLine(t)
	line.run(t)

	before := line.transport.Ready()
	line.relay.DropLive()
	select {
	case <-before:
		t.Fatal("the pre-drop ready channel closed again")
	default:
	}
	select {
	case <-line.transport.Ready():
	case <-time.After(10 * time.Second):
		t.Fatal("the line did not come back")
	}
	if line.relay.Connections() < 2 {
		t.Fatalf("connections: %d, want at least 2", line.relay.Connections())
	}
	status := line.status.Snapshot()
	if status.Reconnects == 0 {
		t.Fatal("the reconnect was not counted")
	}
}

// An envelope written and never answered is re-sent after the reconnect —
// with the **same bytes**, which is the whole point. Fresh bytes would be a
// second identity for one command.
func TestAnUnansweredEnvelopeIsResentByteForByte(t *testing.T) {
	t.Parallel()
	line := newTestLine(t)
	// The relay answers nothing, so the row stays in flight across the drop.
	line.relay.mu.Lock()
	line.relay.ackStatus = ""
	line.relay.mu.Unlock()
	line.run(t)

	sealed := line.seal(t, "orch/mac_test", domain.ClassStream, 0)
	if err := line.spool.Seal(0, sealed, time.Now()); err != nil {
		t.Fatalf("seal: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool { return len(line.relay.Published()) == 1 })

	line.relay.DropLive()
	select {
	case <-line.transport.Ready():
	case <-time.After(10 * time.Second):
		t.Fatal("the line did not come back")
	}
	waitFor(t, 5*time.Second, func() bool { return len(line.relay.Published()) >= 2 })

	published := line.relay.Published()
	if string(published[0]) != string(published[1]) {
		t.Fatalf("the re-send differs from the original:\n%s\n%s", published[0], published[1])
	}
}

// An inbound envelope with a sequence that has already been claimed is
// dropped as a replay, and the drop is counted under that name.
func TestAnInboundReplayIsDroppedAndCounted(t *testing.T) {
	t.Parallel()
	line := newTestLine(t)
	line.run(t)

	envelope, err := domain.Seal([]byte(`{"type":"probe","v":1}`), domain.SealParams{
		Ch: "ctl/mac_test", Seq: 9, Ts: uint64(time.Now().UnixMilli()),
		Class: domain.ClassCtl, KeyID: MasterKeyID, Sender: "viewer-1",
		Key: line.secret, Signer: line.key, Rand: rand.Reader,
	})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	body, err := envelope.CanonicalJSON()
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}

	if err := line.relay.deliver(body); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool { return line.status.Snapshot().InboundTotal == 1 })

	// The identical bytes again: same sender, same sequence.
	if err := line.relay.deliver(body); err != nil {
		t.Fatalf("re-deliver: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool { return line.status.Snapshot().InboundDropped[DropReplay] == 1 })

	status := line.status.Snapshot()
	if status.InboundTotal != 1 {
		t.Fatalf("accepted %d envelopes, want 1", status.InboundTotal)
	}
}

// An envelope from a sender this machine has no pinned key for is dropped as
// `unknown_sender`, before its signature is even looked at.
func TestAnUnknownSenderIsDropped(t *testing.T) {
	t.Parallel()
	line := newTestLine(t)
	line.run(t)

	envelope, err := domain.Seal([]byte(`{"type":"probe","v":1}`), domain.SealParams{
		Ch: "ctl/mac_test", Seq: 1, Ts: uint64(time.Now().UnixMilli()),
		Class: domain.ClassCtl, KeyID: MasterKeyID, Sender: "a-stranger",
		Key: line.secret, Signer: line.key, Rand: rand.Reader,
	})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	body, _ := envelope.CanonicalJSON()
	if err := line.relay.deliver(body); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool { return line.status.Snapshot().InboundDropped[DropUnknownSender] == 1 })
}

// An envelope on somebody else's channel is dropped whatever the relay
// thought it was doing.
func TestAnEnvelopeForAnotherMachineIsDropped(t *testing.T) {
	t.Parallel()
	line := newTestLine(t)
	line.run(t)

	envelope, err := domain.Seal([]byte(`{"type":"probe","v":1}`), domain.SealParams{
		Ch: "ctl/mac_somebody_else", Seq: 1, Ts: uint64(time.Now().UnixMilli()),
		Class: domain.ClassCtl, KeyID: MasterKeyID, Sender: "viewer-1",
		Key: line.secret, Signer: line.key, Rand: rand.Reader,
	})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	body, _ := envelope.CanonicalJSON()
	if err := line.relay.deliver(body); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool { return line.status.Snapshot().InboundDropped[DropWrongChannel] == 1 })
}

// A publish_error settles the row as rejected rather than leaving it in
// flight for the attempt window to burn.
func TestAPublishErrorSettlesTheRow(t *testing.T) {
	t.Parallel()
	line := newTestLine(t)
	line.relay.mu.Lock()
	line.relay.publishError = CodeTooLarge
	line.relay.mu.Unlock()
	line.run(t)

	sealed := line.seal(t, "orch/mac_test", domain.ClassStream, 0)
	if err := line.spool.Seal(0, sealed, time.Now()); err != nil {
		t.Fatalf("seal: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool {
		row, ok := line.spool.Row(0)
		return ok && row.State == SpoolRejected
	})
	if line.status.Snapshot().PublishErrors != 1 {
		t.Fatalf("publish errors: %d", line.status.Snapshot().PublishErrors)
	}
}

// seal reserves and seals one envelope on the spool, and answers its bytes.
func (l *testLine) seal(t *testing.T, channel string, class domain.Class, wantSeq uint64) []byte {
	t.Helper()
	kind := SpoolChannel(channel[:len(channel)-len(channel[indexByte(channel, '/'):])])
	seq, err := l.spool.Reserve(kind, channel, channel, 32)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if seq != wantSeq {
		t.Fatalf("sequence %d, want %d", seq, wantSeq)
	}
	envelope, err := domain.Seal([]byte(`{"type":"probe","v":1}`), domain.SealParams{
		Ch: channel, Seq: seq, Ts: uint64(time.Now().UnixMilli()),
		Class: class, KeyID: MasterKeyID, Sender: l.identity.MachineID,
		Key: l.secret, Signer: l.key, Rand: rand.Reader,
	})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	body, err := envelope.CanonicalJSON()
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	return body
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return len(s)
}

func waitFor(t *testing.T, limit time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the condition never held")
}
