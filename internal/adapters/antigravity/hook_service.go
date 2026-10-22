package antigravity

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
)

const (
	MaxHookConnections = 8
	MaxHookFrameBytes  = 2 * MaxHookPayloadBytes
	MaxHookRequestTime = 2 * time.Second
)

// HookLaunch is trusted launch metadata passed explicitly by the caller. It
// never supplies a PID, birth, clock or sequence. The bridge checks the private
// capability file and registered launch commitment independently.
type HookLaunch struct {
	ID         string `json:"id"`
	Capability string `json:"capability"`
}

type hookRequest struct {
	Version int             `json:"version"`
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload"`
	Launch  *HookLaunch     `json:"launch,omitempty"`
}
type hookReply struct {
	Version  int             `json:"version"`
	Response json.RawMessage `json:"response,omitempty"`
	Code     string          `json:"code,omitempty"`
}

// HookResponse composes native PreInvocation context only after a verified,
// persisted observation. It must honor ctx. A returned error is a hook failure;
// observing a binding does not prove that context was loaded by the provider.
type HookResponse func(context.Context, ports.AntigravityBinding) ([]byte, error)

// HookAdmission runs after bounded decoding and kernel/process verification,
// before Observe. It may register a private prepared LaunchCommitment through
// the bridge and return its trusted launch capability. It cannot replace the
// kernel identity, server time or allocated sequence. A nil launch preserves
// explicit client launch metadata. Payload is a bounded copy, never authority.
// Like the response callback, admission must honor ctx and must not log input.
type HookAdmission func(context.Context, NativeUnixPeer, Witness, string, json.RawMessage) (*HookLaunch, error)

type HookServiceConfig struct {
	// StateDir must be a canonical, private, locally owned directory. Its
	// parent must already exist. No preexisting socket is ever removed.
	StateDir              string
	Bridge                *Bridge
	Admit                 HookAdmission
	PreInvocationResponse HookResponse
}

// HookService owns its listener and accepted connections. Serve owns all
// goroutines and joins them before returning. Close interrupts I/O; the owner
// must wait for Serve to return before disposing the bridge or its reader.
type HookService struct {
	config          HookServiceConfig
	listener        *net.UnixListener
	socket          os.FileInfo
	mu              sync.Mutex
	connections     map[*net.UnixConn]struct{}
	rejecting       *net.UnixConn
	closed, serving bool
	workers         sync.WaitGroup
	observeMu       sync.Mutex
	sequence        uint64
	peer            func(context.Context, *net.UnixConn) (NativeUnixPeer, error)
}

func HookSocketPath(stateDir string) string { return filepath.Join(stateDir, "antigravity-hook.sock") }

// ListenHookService binds once without starting goroutines. Windows and other
// unsupported kernels fail with a typed unsupported code before touching disk.
func ListenHookService(c HookServiceConfig) (*HookService, error) {
	if err := hookPlatform(); err != nil {
		return nil, err
	}
	if c.Bridge == nil || !filepath.IsAbs(c.StateDir) || filepath.Clean(c.StateDir) != c.StateDir {
		return nil, refuse("antigravity_hook_configuration_invalid")
	}
	if err := privateHookDirectory(c.StateDir, true); err != nil {
		return nil, err
	}
	path := HookSocketPath(c.StateDir)
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return nil, refuse("antigravity_hook_socket_exists")
	}
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, refuse("antigravity_hook_listen_failed")
	}
	l.SetUnlinkOnClose(false)
	info, err := os.Lstat(path)
	if err != nil {
		_ = l.Close()
		return nil, refuse("antigravity_hook_socket_invalid")
	}
	s := &HookService{config: c, listener: l, socket: info, connections: map[*net.UnixConn]struct{}{}, peer: func(ctx context.Context, conn *net.UnixConn) (NativeUnixPeer, error) {
		return ReadNativeUnixPeer(ctx, conn)
	}}
	if err = os.Chmod(path, 0600); err != nil {
		s.Close()
		return nil, refuse("antigravity_hook_socket_invalid")
	}
	if err = validateHookSocket(c.StateDir); err != nil {
		s.Close()
		return nil, err
	}
	s.sequence, err = c.Bridge.observationSequenceFloor()
	if err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *HookService) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	_ = s.listener.Close()
	for conn := range s.connections {
		_ = conn.Close()
	}
	if s.rejecting != nil {
		_ = s.rejecting.Close()
	}
	// net.UnixListener's automatic unlink is disabled: replacement bytes
	// belong to their new owner even if they occupy our original pathname.
	path := HookSocketPath(s.config.StateDir)
	if info, err := os.Lstat(path); err == nil && os.SameFile(s.socket, info) {
		_ = os.Remove(path)
	}
}

func (s *HookService) Serve(ctx context.Context) error {
	s.mu.Lock()
	if s.serving || s.closed {
		s.mu.Unlock()
		return refuse("antigravity_hook_service_closed")
	}
	s.serving = true
	s.mu.Unlock()
	done := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
			s.Close()
		case <-done:
		}
	}()
	defer func() { s.Close(); s.workers.Wait(); close(done); <-watchDone }()
	for {
		conn, err := s.listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return nil
			}
			return refuse("antigravity_hook_accept_failed")
		}
		requestDeadline := time.Now().Add(MaxHookRequestTime)
		_ = conn.SetDeadline(requestDeadline)
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = conn.Close()
			continue
		}
		if len(s.connections) >= MaxHookConnections {
			s.rejecting = conn
			s.mu.Unlock()
			// There is no work queue and no rejection goroutine. The socket
			// deadline bounds this single small refusal, including a stalled peer.
			_ = writeHookFrame(conn, hookReply{Version: 1, Code: "antigravity_hook_busy"})
			_ = conn.CloseWrite()
			// Closing with unread request bytes can reset a stream and hide
			// the refusal. Drain at most one bounded frame, under the SAME
			// deadline, after half-closing the reply. Close also owns this FD.
			_, _ = io.CopyN(io.Discard, conn, MaxHookFrameBytes+4)
			_ = conn.Close()
			s.mu.Lock()
			s.rejecting = nil
			s.mu.Unlock()
			continue
		}
		s.connections[conn] = struct{}{}
		s.config.Bridge.hookConnections.Add(1)
		s.workers.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.workers.Done()
			defer s.config.Bridge.hookConnections.Add(-1)
			defer func() { _ = conn.Close(); s.mu.Lock(); delete(s.connections, conn); s.mu.Unlock() }()
			requestCtx, cancel := context.WithDeadline(ctx, requestDeadline)
			defer cancel()
			reply := s.handle(requestCtx, conn)
			_ = writeHookFrame(conn, reply)
		}()
	}
}

func (s *HookService) handle(ctx context.Context, conn *net.UnixConn) hookReply {
	data, err := s.receive(ctx, conn)
	if err != nil {
		return hookReply{Version: 1, Code: hookErrorCode(err)}
	}
	return hookReply{Version: 1, Response: data}
}

func (s *HookService) receive(ctx context.Context, conn *net.UnixConn) ([]byte, error) {
	// Acquire kernel credentials and immediately read the connector's full
	// identity BEFORE waiting for any untrusted frame. Observe rechecks it.
	peer, err := s.peer(ctx, conn)
	if err != nil {
		return nil, refuse("antigravity_hook_peer_unverified")
	}
	if !peer.UIDKnown || peer.UID != uint32(os.Geteuid()) || peer.PID <= 0 {
		return nil, refuse("antigravity_hook_peer_unverified")
	}
	p, err := s.config.Bridge.reader.ReadAntigravityProcess(ctx, peer.PID)
	if err != nil || p.PID != peer.PID || p.Birth == "" || p.KernelGeneration == "" {
		return nil, refuse("antigravity_hook_peer_unverified")
	}
	data, err := readHookFrame(conn)
	if err != nil {
		return nil, err
	}
	var r hookRequest
	if err = decodeHookEnvelope(data, &r); err != nil || r.Version != 1 || !eventAllowed(r.Event) || len(r.Payload) > MaxHookPayloadBytes {
		return nil, refuse("antigravity_hook_request_invalid")
	}
	if _, err = DecodeObservation(bytes.NewReader(r.Payload)); err != nil {
		return nil, err
	}
	if !s.observeMu.TryLock() {
		return nil, refuse("antigravity_hook_busy")
	}
	defer s.observeMu.Unlock()
	if ctx.Err() != nil {
		return nil, refuse("antigravity_hook_timeout")
	}
	floor, err := s.config.Bridge.observationSequenceFloor()
	if err != nil {
		return nil, err
	}
	s.sequence = max(s.sequence, floor)
	if s.sequence == ^uint64(0) {
		return nil, refuse("antigravity_hook_sequence_exhausted")
	}
	s.sequence++
	w := Witness{HelperPID: peer.PID, HelperBirth: p.Birth, HelperBootID: p.BootID, HelperGeneration: p.KernelGeneration, At: time.Now(), Sequence: s.sequence}
	if r.Launch != nil {
		w.LaunchID, w.Capability = r.Launch.ID, r.Launch.Capability
	}
	if s.config.Admit != nil {
		launch, err := s.config.Admit(ctx, peer, w, r.Event, bytes.Clone(r.Payload))
		if err != nil {
			return nil, refuse("antigravity_hook_admission_failed")
		}
		if launch != nil {
			if len(launch.ID) > MaxHookPayloadBytes || len(launch.Capability) > MaxHookPayloadBytes {
				return nil, refuse("antigravity_hook_admission_failed")
			}
			w.LaunchID, w.Capability = launch.ID, launch.Capability
		}
		if ctx.Err() != nil {
			return nil, refuse("antigravity_hook_timeout")
		}
	}
	binding, err := s.config.Bridge.Observe(ctx, w, r.Event, bytes.NewReader(r.Payload))
	if err != nil {
		return nil, err
	}
	var response []byte
	if r.Event == "PreInvocation" && s.config.PreInvocationResponse != nil {
		response, err = s.config.PreInvocationResponse(ctx, binding)
	} else {
		response, err = ObserverResponse(r.Event)
	}
	if err != nil {
		return nil, refuse("antigravity_hook_response_failed")
	}
	if ctx.Err() != nil {
		return nil, refuse("antigravity_hook_timeout")
	}
	if len(response) > MaxHookPayloadBytes {
		return nil, refuse("antigravity_hook_response_invalid")
	}
	if _, err = rawObject(response); err != nil {
		return nil, refuse("antigravity_hook_response_invalid")
	}
	return response, nil
}

func hookErrorCode(err error) string {
	var b *BridgeError
	if errors.As(err, &b) {
		return b.Code
	}
	return "antigravity_hook_failed"
}

func decodeHookEnvelope(data []byte, v any) error {
	fields, err := rawObject(data)
	if err != nil {
		return refuse("antigravity_hook_frame_invalid")
	}
	for key := range fields {
		allowed := false
		switch v.(type) {
		case *hookRequest:
			allowed = key == "version" || key == "event" || key == "payload" || key == "launch"
		case *hookReply:
			allowed = key == "version" || key == "response" || key == "code"
		}
		if !allowed {
			return refuse("antigravity_hook_frame_invalid")
		}
	}
	if launch, ok := fields["launch"]; ok {
		metadata, err := rawObject(launch)
		if err != nil {
			return refuse("antigravity_hook_frame_invalid")
		}
		for key := range metadata {
			if key != "id" && key != "capability" {
				return refuse("antigravity_hook_frame_invalid")
			}
		}
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return refuse("antigravity_hook_frame_invalid")
	}
	return nil
}

func readHookFrame(r io.Reader) ([]byte, error) {
	var size [4]byte
	if _, err := io.ReadFull(r, size[:]); err != nil {
		return nil, hookIOError(err)
	}
	n := binary.BigEndian.Uint32(size[:])
	if n == 0 || n > MaxHookFrameBytes {
		return nil, refuse("antigravity_hook_frame_too_large")
	}
	data := make([]byte, int(n))
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, hookIOError(err)
	}
	return data, nil
}

func writeHookFrame(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return refuse("antigravity_hook_frame_invalid")
	}
	if len(data) > MaxHookFrameBytes {
		return refuse("antigravity_hook_frame_too_large")
	}
	frame := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(data)))
	copy(frame[4:], data)
	for len(frame) > 0 {
		n, e := w.Write(frame)
		if e != nil {
			return hookIOError(e)
		}
		if n <= 0 {
			return refuse("antigravity_hook_disconnected")
		}
		frame = frame[n:]
	}
	return nil
}

func hookIOError(err error) error {
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return refuse("antigravity_hook_timeout")
	}
	return refuse("antigravity_hook_disconnected")
}
