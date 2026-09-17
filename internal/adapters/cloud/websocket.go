package cloud

// A WebSocket client, written here rather than taken from a library.
//
// This repository has no direct dependency on anything (`go.mod` lists only
// what SQLite drags in), and the client half of RFC 6455 is small: one HTTP/1.1
// upgrade, a four-byte mask on every frame this side sends, and a reassembler
// for continuation frames. The server half — where the interesting attacks
// live — is not implemented at all, because this daemon never accepts a
// WebSocket, it only makes one.
//
// What is deliberately missing: permessage-deflate (never negotiated, so a
// compressed frame is a protocol error rather than a silent mis-read), and any
// support for a redirect during the upgrade (a 3xx answer to an upgrade with a
// bearer token would send that token somewhere the caller did not name).

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// The WebSocket GUID from RFC 6455 §1.3. The server hashes it with the key we
// send and returns the digest; checking it is what proves the answer came from
// something that speaks WebSocket and not from a cache or a proxy that echoed
// a 101.
const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// Frame opcodes, RFC 6455 §5.2.
const (
	opContinuation byte = 0x0
	opText         byte = 0x1
	opBinary       byte = 0x2
	opClose        byte = 0x8
	opPing         byte = 0x9
	opPong         byte = 0xA
)

// maxFrameBytes bounds one reassembled message. The relay's own ceiling is
// Cloudflare's 32 MiB WebSocket message (docs/cloud-wire.md §2.3), and a
// message past it cannot be anything this daemon asked for, so it is refused
// before it is allocated rather than after.
const maxFrameBytes = 32 << 20

// ErrConnClosed is the answer once the connection has been closed, by either
// side. It is deliberately one error for both: a caller that must distinguish
// "they closed" from "we closed" reads CloseError from the read side.
var ErrConnClosed = errors.New("the websocket is closed")

// CloseError is a close frame the peer sent. Code is the RFC 6455 status; the
// relay uses the 4400..4429 range for its own refusals (docs/cloud-wire.md
// §9.1), and that number is often the only thing a refused connection carries.
type CloseError struct {
	Code   int
	Reason string
}

func (e *CloseError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("websocket closed: %d", e.Code)
	}
	return fmt.Sprintf("websocket closed: %d %s", e.Code, e.Reason)
}

// UpgradeError is an HTTP answer that was not a 101. Status and Body are kept
// because the relay answers a *rejected* upgrade with 101 and a close frame
// (PROTOCOL.md:87-92), so a plain HTTP status here means something else went
// wrong — a wrong path, a proxy, a daemon that is not the relay.
type UpgradeError struct {
	Status int
	Body   string
}

func (e *UpgradeError) Error() string {
	body := strings.TrimSpace(e.Body)
	if len(body) > 200 {
		body = body[:200] + "…"
	}
	if body == "" {
		return fmt.Sprintf("websocket upgrade refused: HTTP %d", e.Status)
	}
	return fmt.Sprintf("websocket upgrade refused: HTTP %d: %s", e.Status, body)
}

// Conn is one open WebSocket. It is safe for one reader and one writer at a
// time; Write and Close serialise against each other, Read does not and must
// be called from a single goroutine.
type Conn struct {
	conn net.Conn
	br   *bufio.Reader

	// Subprotocol is what the server selected, "" when it selected none.
	Subprotocol string

	writeMu sync.Mutex
	closed  bool

	// readBuf accumulates a fragmented message across continuation frames.
	readBuf []byte
	readOp  byte
}

// DialOptions are the knobs the transport needs and nothing more.
type DialOptions struct {
	// Header carries Authorization and anything else the relay wants. The
	// handshake headers themselves are set here and cannot be overridden.
	Header http.Header
	// Subprotocols is the `Sec-WebSocket-Protocol` list. The relay accepts a
	// token this way for browsers, which cannot set headers; a Go client uses
	// Authorization and leaves this empty.
	Subprotocols []string
	// HandshakeTimeout bounds dial + TLS + the HTTP exchange.
	HandshakeTimeout time.Duration
	// TLSConfig is used for wss://. Nil means the platform defaults.
	TLSConfig *tls.Config
}

// Dial opens a WebSocket to rawURL, which must be ws:// or wss://.
func Dial(rawURL string, opts DialOptions) (*Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("relay url: %w", err)
	}
	secure := false
	switch u.Scheme {
	case "ws":
	case "wss":
		secure = true
	default:
		return nil, fmt.Errorf("relay url: %q is not ws:// or wss://", u.Scheme)
	}
	host := u.Host
	if u.Port() == "" {
		if secure {
			host = net.JoinHostPort(u.Hostname(), "443")
		} else {
			host = net.JoinHostPort(u.Hostname(), "80")
		}
	}

	timeout := opts.HandshakeTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	deadline := time.Now().Add(timeout)

	netConn, err := (&net.Dialer{Timeout: timeout}).Dial("tcp", host)
	if err != nil {
		return nil, err
	}
	// Every failure from here on has to close the socket; a half-open dial is
	// a file descriptor that nobody ever comes back for.
	ok := false
	defer func() {
		if !ok {
			_ = netConn.Close()
		}
	}()
	if err := netConn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	if secure {
		cfg := opts.TLSConfig
		if cfg == nil {
			cfg = &tls.Config{}
		}
		if cfg.ServerName == "" {
			cfg = cfg.Clone()
			cfg.ServerName = u.Hostname()
		}
		tlsConn := tls.Client(netConn, cfg)
		if err := tlsConn.Handshake(); err != nil {
			return nil, err
		}
		netConn = tlsConn
	}

	keyRaw := make([]byte, 16)
	if _, err := rand.Read(keyRaw); err != nil {
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyRaw)

	req := &strings.Builder{}
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	fmt.Fprintf(req, "GET %s HTTP/1.1\r\n", path)
	fmt.Fprintf(req, "Host: %s\r\n", u.Host)
	req.WriteString("Upgrade: websocket\r\n")
	req.WriteString("Connection: Upgrade\r\n")
	fmt.Fprintf(req, "Sec-WebSocket-Key: %s\r\n", key)
	req.WriteString("Sec-WebSocket-Version: 13\r\n")
	if len(opts.Subprotocols) > 0 {
		fmt.Fprintf(req, "Sec-WebSocket-Protocol: %s\r\n", strings.Join(opts.Subprotocols, ", "))
	}
	for name, values := range opts.Header {
		switch http.CanonicalHeaderKey(name) {
		case "Upgrade", "Connection", "Sec-Websocket-Key", "Sec-Websocket-Version", "Sec-Websocket-Protocol", "Host":
			// Set above. Letting a caller overwrite these turns a typo into a
			// connection that is not a WebSocket at all.
			continue
		}
		for _, v := range values {
			fmt.Fprintf(req, "%s: %s\r\n", name, v)
		}
	}
	req.WriteString("\r\n")
	if _, err := io.WriteString(netConn, req.String()); err != nil {
		return nil, err
	}

	br := bufio.NewReaderSize(netConn, 4096)
	// A raw HTTP request needs a matching http.Request for ReadResponse only so
	// that it knows the method; the body rules for GET are what we want.
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return nil, &UpgradeError{Status: resp.StatusCode, Body: string(body)}
	}
	if !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") ||
		!headerListContains(resp.Header.Get("Connection"), "upgrade") {
		return nil, errors.New("websocket upgrade: the answer is a 101 that is not a websocket")
	}
	sum := sha1.Sum([]byte(key + websocketGUID))
	if resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(sum[:]) {
		return nil, errors.New("websocket upgrade: Sec-WebSocket-Accept does not match the key we sent")
	}
	selected := resp.Header.Get("Sec-WebSocket-Protocol")
	if selected != "" && !containsFold(opts.Subprotocols, selected) {
		return nil, fmt.Errorf("websocket upgrade: the server chose subprotocol %q, which we did not offer", selected)
	}
	// permessage-deflate is never offered, so a server that turns it on is
	// about to send frames this reader would mis-parse as plain text.
	if ext := resp.Header.Get("Sec-WebSocket-Extensions"); strings.TrimSpace(ext) != "" {
		return nil, fmt.Errorf("websocket upgrade: the server turned on extension %q, which we did not offer", ext)
	}
	if err := netConn.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}
	ok = true
	return &Conn{conn: netConn, br: br, Subprotocol: selected}, nil
}

// WriteText sends one whole text message.
func (c *Conn) WriteText(data []byte, deadline time.Time) error {
	return c.writeFrame(opText, data, deadline)
}

// WritePing sends a ping with an empty payload.
func (c *Conn) WritePing(deadline time.Time) error {
	return c.writeFrame(opPing, nil, deadline)
}

// WriteClose sends a close frame and does not wait for the peer's answer. The
// caller closes the socket afterwards; a peer that never answers must not be
// able to hold this side open.
func (c *Conn) WriteClose(code int, reason string, deadline time.Time) error {
	payload := make([]byte, 2, 2+len(reason))
	binary.BigEndian.PutUint16(payload, uint16(code))
	payload = append(payload, reason...)
	return c.writeFrame(opClose, payload, deadline)
}

func (c *Conn) writeFrame(op byte, payload []byte, deadline time.Time) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed {
		return ErrConnClosed
	}
	if err := c.conn.SetWriteDeadline(deadline); err != nil {
		return err
	}

	n := len(payload)
	head := make([]byte, 0, 14)
	head = append(head, 0x80|op) // FIN, no reserved bits.
	switch {
	case n < 126:
		head = append(head, 0x80|byte(n))
	case n <= 0xFFFF:
		head = append(head, 0x80|126, byte(n>>8), byte(n))
	default:
		head = append(head, 0x80|127)
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(n))
		head = append(head, size[:]...)
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	head = append(head, mask[:]...)

	// The payload is masked into a copy: masking in place would corrupt a
	// caller's buffer, and the caller here hands us the same sealed envelope
	// bytes again on a re-send.
	masked := make([]byte, n)
	for i := 0; i < n; i++ {
		masked[i] = payload[i] ^ mask[i%4]
	}
	if _, err := c.conn.Write(head); err != nil {
		return err
	}
	if n > 0 {
		if _, err := c.conn.Write(masked); err != nil {
			return err
		}
	}
	return nil
}

// Read returns the next whole text message. Control frames are handled here —
// a ping is answered with a pong, a pong is discarded, a close is returned as
// a *CloseError — so a caller only ever sees application messages.
//
// deadline bounds the wait for the next frame. A zero deadline means no bound.
func (c *Conn) Read(deadline time.Time) ([]byte, error) {
	for {
		if err := c.conn.SetReadDeadline(deadline); err != nil {
			return nil, err
		}
		fin, op, payload, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		switch op {
		case opPing:
			// A pong echoes the ping's payload (RFC 6455 §5.5.3). Its deadline
			// is the read deadline: a peer that pings and then stops reading
			// must not be able to block this side for ever.
			if err := c.writeFrame(opPong, payload, deadline); err != nil {
				return nil, err
			}
			continue
		case opPong:
			continue
		case opClose:
			code := 1005 // "no status received", RFC 6455 §7.4.1.
			reason := ""
			if len(payload) >= 2 {
				code = int(binary.BigEndian.Uint16(payload))
				reason = string(payload[2:])
			}
			return nil, &CloseError{Code: code, Reason: reason}
		case opText, opBinary, opContinuation:
		default:
			return nil, fmt.Errorf("websocket: unknown opcode 0x%x", op)
		}

		if op == opContinuation {
			if c.readOp == 0 {
				return nil, errors.New("websocket: a continuation frame with nothing to continue")
			}
		} else {
			if c.readOp != 0 {
				return nil, errors.New("websocket: a new message started inside a fragmented one")
			}
			c.readOp = op
			c.readBuf = c.readBuf[:0]
		}
		if len(c.readBuf)+len(payload) > maxFrameBytes {
			return nil, fmt.Errorf("websocket: a message past %d bytes", maxFrameBytes)
		}
		c.readBuf = append(c.readBuf, payload...)
		if !fin {
			continue
		}
		op, c.readOp = c.readOp, 0
		message := c.readBuf
		c.readBuf = nil
		if op == opBinary {
			// The relay refuses binary frames with `bad_request`
			// (docs/cloud-wire.md §9.1); this side refuses them too rather
			// than handing a caller bytes it would have to guess at.
			return nil, errors.New("websocket: a binary frame, and this wire is JSON text")
		}
		if !utf8.Valid(message) {
			return nil, errors.New("websocket: a text frame that is not UTF-8")
		}
		return message, nil
	}
}

func (c *Conn) readFrame() (fin bool, op byte, payload []byte, err error) {
	var head [2]byte
	if _, err = io.ReadFull(c.br, head[:]); err != nil {
		return false, 0, nil, err
	}
	fin = head[0]&0x80 != 0
	if head[0]&0x70 != 0 {
		// Reserved bits are only meaningful with a negotiated extension, and
		// none was negotiated.
		return false, 0, nil, errors.New("websocket: a reserved bit is set")
	}
	op = head[0] & 0x0F
	masked := head[1]&0x80 != 0
	if masked {
		// A server must not mask (RFC 6455 §5.1). Accepting it anyway would
		// make this client the only one on the wire that tolerates a broken
		// peer, which is how a mis-framing bug survives to production.
		return false, 0, nil, errors.New("websocket: the server masked a frame")
	}
	size := uint64(head[1] & 0x7F)
	switch size {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return false, 0, nil, err
		}
		size = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return false, 0, nil, err
		}
		size = binary.BigEndian.Uint64(ext[:])
	}
	if op >= 0x8 {
		// Control frames: ≤ 125 bytes and never fragmented (RFC 6455 §5.5).
		if size > 125 || !fin {
			return false, 0, nil, errors.New("websocket: a malformed control frame")
		}
	}
	if size > maxFrameBytes {
		return false, 0, nil, fmt.Errorf("websocket: a frame of %d bytes", size)
	}
	payload = make([]byte, size)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return false, 0, nil, err
	}
	return fin, op, payload, nil
}

// Close shuts the socket down. It is safe to call more than once and from any
// goroutine; the reader unblocks with an error.
func (c *Conn) Close() error {
	c.writeMu.Lock()
	if c.closed {
		c.writeMu.Unlock()
		return nil
	}
	c.closed = true
	c.writeMu.Unlock()
	return c.conn.Close()
}

// headerListContains reports whether a comma-separated header value carries
// the given token, ignoring case and surrounding space.
func headerListContains(value, token string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func containsFold(list []string, want string) bool {
	for _, item := range list {
		if strings.EqualFold(item, want) {
			return true
		}
	}
	return false
}
