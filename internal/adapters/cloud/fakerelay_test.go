package cloud

// A relay that speaks the handshake, in this process.
//
// The Swift app has the same thing at 1,066 lines
// (`Sources/CloudTransportFakes.swift`) and for the same reason: the parts of
// the transport that are worth testing — what happens when the socket dies
// mid-flight, what a second identical envelope does, whether a re-send carries
// the same bytes — are exactly the parts a real relay makes hard to arrange.
//
// It is a real WebSocket server, not a mock: `net/http/httptest` plus the
// server half of RFC 6455, so the client's framing, masking and continuation
// handling are under test too.

import (
	"bufio"
	"crypto/ed25519"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// fakeRelay accepts machine connections and records what they published.
type fakeRelay struct {
	server *httptest.Server

	mu sync.Mutex
	// account and device are what the challenge names.
	account string
	device  string
	// publicKey verifies the hello.
	publicKey ed25519.PublicKey
	// published holds every envelope, in arrival order, exactly as received.
	published []json.RawMessage
	// connections counts accepted handshakes.
	connections int
	// live is the socket of the newest connection, so a test can drop it.
	live net.Conn
	// refuse, when set, makes the next connection answer this error frame
	// instead of a challenge.
	refuse *ErrorFrame
	// ackStatus is what publishes are answered with.
	ackStatus string
	// publishError, when set, answers a publish_error with this code instead
	// of an ack.
	publishError string
	// silent makes the relay accept and then say nothing at all, for the
	// liveness bound.
	silent bool
	// refuseAll answers every connection with this error frame, the way a
	// plan that is full keeps refusing until a slot frees.
	refuseAll *ErrorFrame
	// challengeVersion is the `v` the challenge carries; zero means 1.
	challengeVersion int
}

func newFakeRelay(account, device string, publicKey ed25519.PublicKey) *fakeRelay {
	relay := &fakeRelay{account: account, device: device, publicKey: publicKey, ackStatus: AckDelivered}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/connect", relay.connect)
	relay.server = httptest.NewServer(mux)
	return relay
}

func (r *fakeRelay) URL() string {
	return "ws" + strings.TrimPrefix(r.server.URL, "http") + "/v1/connect"
}

func (r *fakeRelay) Close() { r.server.Close() }

// Published answers a copy of everything that arrived.
func (r *fakeRelay) Published() []json.RawMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]json.RawMessage(nil), r.published...)
}

// Connections is how many handshakes completed.
func (r *fakeRelay) Connections() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.connections
}

// DropLive closes the newest connection from the server side, which is what a
// relay restart looks like to a client.
func (r *fakeRelay) DropLive() {
	r.mu.Lock()
	conn := r.live
	r.live = nil
	r.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

// Refuse makes the next connection answer an error frame.
func (r *fakeRelay) Refuse(code, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refuse = &ErrorFrame{Type: FrameError, Code: code, Message: message}
}

func (r *fakeRelay) connect(w http.ResponseWriter, req *http.Request) {
	if req.URL.Query().Get("role") != "machine" {
		http.Error(w, "bad_request", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(req.Header.Get("Authorization"), "Bearer ") {
		// A missing credential is a 401 at the upgrade, which is the shape the
		// client must treat as terminal.
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	key := req.Header.Get("Sec-WebSocket-Key")
	sum := sha1.Sum([]byte(key + websocketGUID))
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n",
		base64.StdEncoding.EncodeToString(sum[:]))
	if err := rw.Flush(); err != nil {
		return
	}
	r.serve(conn, rw.Reader)
}

func (r *fakeRelay) serve(conn net.Conn, br *bufio.Reader) {
	r.mu.Lock()
	refuse := r.refuse
	r.refuse = nil
	if refuse == nil {
		refuse = r.refuseAll
	}
	account, device, publicKey, silent := r.account, r.device, r.publicKey, r.silent
	version := r.challengeVersion
	r.mu.Unlock()
	if version == 0 {
		version = 1
	}

	if refuse != nil {
		body, _ := json.Marshal(refuse)
		_ = writeServerFrame(conn, opText, body)
		return
	}

	nonce := make([]byte, ChallengeBytes)
	for i := range nonce {
		nonce[i] = byte(i * 7)
	}
	challenge := base64.StdEncoding.EncodeToString(nonce)
	body, _ := json.Marshal(ChallengeFrame{
		Type: FrameChallenge, V: version, Context: ChallengeContext,
		Account: account, Device: device, Challenge: challenge, ExpiresInMs: 15000,
	})
	if err := writeServerFrame(conn, opText, body); err != nil {
		return
	}

	hello, err := readServerFrame(br)
	if err != nil {
		return
	}
	var frame HelloFrame
	if err := json.Unmarshal(hello, &frame); err != nil || frame.Type != FrameHello {
		return
	}
	signature, err := base64.StdEncoding.DecodeString(frame.Sig)
	if err != nil || !ed25519.Verify(publicKey, []byte(ChallengeString(account, device, challenge)), signature) {
		body, _ := json.Marshal(ErrorFrame{Type: FrameError, Code: CodeUnauthorized, Message: "the challenge signature does not verify"})
		_ = writeServerFrame(conn, opText, body)
		return
	}

	body, _ = json.Marshal(ReadyFrame{
		Type: FrameReady, V: 1, Account: account, Device: device, Role: "machine",
		ConnectedAt: time.Now().UnixMilli(), TokenExpiresAt: time.Now().Add(5 * time.Minute).UnixMilli(),
	})
	if err := writeServerFrame(conn, opText, body); err != nil {
		return
	}

	r.mu.Lock()
	r.connections++
	r.live = conn
	r.mu.Unlock()

	if silent {
		// Accept and say nothing, so the client's liveness bound is what ends
		// the connection.
		io.Copy(io.Discard, br)
		return
	}

	for {
		data, err := readServerFrame(br)
		if err != nil {
			return
		}
		kind, err := frameType(data)
		if err != nil {
			continue
		}
		switch kind {
		case FramePing:
			_ = writeServerFrame(conn, opText, []byte(`{"type":"pong"}`))
		case FramePublish:
			var publish EnvelopeFrame
			if err := json.Unmarshal(data, &publish); err != nil {
				continue
			}
			var header struct {
				Ch  string `json:"ch"`
				Seq uint64 `json:"seq"`
			}
			if err := json.Unmarshal(publish.Envelope, &header); err != nil {
				continue
			}
			r.mu.Lock()
			r.published = append(r.published, append(json.RawMessage(nil), publish.Envelope...))
			failure := r.publishError
			status := r.ackStatus
			r.mu.Unlock()
			if failure != "" {
				body, _ := json.Marshal(PublishErrorFrame{Type: FramePublishError, Ch: header.Ch, Seq: header.Seq, Code: failure})
				_ = writeServerFrame(conn, opText, body)
				continue
			}
			body, _ := json.Marshal(AckFrame{Type: FrameAck, Ch: header.Ch, Seq: header.Seq, Fanout: 1, Status: status})
			_ = writeServerFrame(conn, opText, body)
		}
	}
}

// deliver pushes one envelope frame to the live connection.
func (r *fakeRelay) deliver(envelope []byte) error {
	r.mu.Lock()
	conn := r.live
	r.mu.Unlock()
	if conn == nil {
		return errors.New("no live connection")
	}
	body, err := json.Marshal(EnvelopeFrame{Type: FrameEnvelope, Envelope: envelope})
	if err != nil {
		return err
	}
	return writeServerFrame(conn, opText, body)
}

// writeServerFrame writes an unmasked frame, as a server must.
func writeServerFrame(conn net.Conn, op byte, payload []byte) error {
	head := []byte{0x80 | op}
	n := len(payload)
	switch {
	case n < 126:
		head = append(head, byte(n))
	case n <= 0xFFFF:
		head = append(head, 126, byte(n>>8), byte(n))
	default:
		head = append(head, 127)
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(n))
		head = append(head, size[:]...)
	}
	if _, err := conn.Write(head); err != nil {
		return err
	}
	_, err := conn.Write(payload)
	return err
}

// readServerFrame reads one masked client frame.
func readServerFrame(br *bufio.Reader) ([]byte, error) {
	var head [2]byte
	if _, err := io.ReadFull(br, head[:]); err != nil {
		return nil, err
	}
	masked := head[1]&0x80 != 0
	if !masked {
		// RFC 6455 §5.1: every client frame is masked. A server that accepts
		// an unmasked one hides a client bug.
		return nil, errors.New("the client did not mask a frame")
	}
	size := uint64(head[1] & 0x7F)
	switch size {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			return nil, err
		}
		size = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			return nil, err
		}
		size = binary.BigEndian.Uint64(ext[:])
	}
	var mask [4]byte
	if _, err := io.ReadFull(br, mask[:]); err != nil {
		return nil, err
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(br, payload); err != nil {
		return nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	return payload, nil
}
