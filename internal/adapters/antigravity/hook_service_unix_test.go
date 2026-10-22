//go:build darwin || linux

package antigravity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
)

func hookTestDir(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(base) > 40 {
		base, err = filepath.EvalSymlinks("/tmp")
		if err != nil {
			t.Fatal(err)
		}
	}
	dir, err := os.MkdirTemp(base, "agh-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return dir
}

// The socket credentials are real; only the bridge's provider ancestry and
// transcript are synthetic. This does not claim this test process is a provider.
func hookFixture(t *testing.T) *bridgeFixture {
	f := fixture(t)
	f.b.now = time.Now
	p := f.procs[42]
	delete(f.procs, 42)
	p.PID = os.Getpid()
	f.procs[p.PID] = p
	return f
}
func startHookTest(t *testing.T, s *HookService) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("service did not reap accepted connections")
		}
	})
	return cancel, done
}
func hookRequestFor(f *bridgeFixture, event string, n int) hookRequest {
	data, _ := io.ReadAll(payload(conversationA, f.pathA, map[string]any{"invocationNum": n, "pid": 999999}))
	return hookRequest{Version: 1, Event: event, Payload: data}
}
func hookExchange(t *testing.T, dir string, r hookRequest) hookReply {
	t.Helper()
	c, err := net.Dial("unix", HookSocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	if err = writeHookFrame(c, r); err != nil {
		t.Fatal(err)
	}
	data, err := readHookFrame(c)
	if err != nil {
		t.Fatal(err)
	}
	var reply hookReply
	if err = decodeHookEnvelope(data, &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}

func TestHookServiceAcceptedKernelPeerAndNativeBirth(t *testing.T) {
	f := hookFixture(t)
	dir := hookTestDir(t)
	s, err := ListenHookService(HookServiceConfig{StateDir: dir, Bridge: f.b})
	if err != nil {
		t.Fatal(err)
	}
	seen := make(chan NativeUnixPeer, 1)
	s.peer = func(ctx context.Context, c *net.UnixConn) (NativeUnixPeer, error) {
		p, e := ReadNativeUnixPeer(ctx, c)
		if e == nil {
			seen <- p
		}
		return p, e
	}
	startHookTest(t, s)
	reply := hookExchange(t, dir, hookRequestFor(f, "PreInvocation", 0))
	if reply.Code != "" || string(reply.Response) != "{}" {
		t.Fatalf("reply %+v", reply)
	}
	p := <-seen
	if p.PID != os.Getpid() || !p.UIDKnown || p.UID != uint32(os.Geteuid()) {
		t.Fatalf("kernel connector differs: %+v", p)
	}
	// Separately inspect the actual connector. No synthetic PID/identity is
	// used for this native birth/boot/generation assertion.
	native, err := NewNativeProcessReader(nil).ReadAntigravityProcess(context.Background(), p.PID)
	if err != nil {
		t.Fatal(err)
	}
	if native.PID != p.PID || native.Birth == "" || native.KernelGeneration == "" {
		t.Fatal("native identity unknown")
	}
	if binding, err := f.b.AntigravityBinding(context.Background(), f.s); err != nil || binding.Process.PID != 41 {
		t.Fatalf("synthetic bridge binding %v %v", binding, err)
	}
}

func TestHookServiceRejectsUnknownUIDAndIdentityChanges(t *testing.T) {
	for _, which := range []string{"wrong_uid", "unknown_uid", "unknown_pid", "birth", "boot", "exec"} {
		t.Run(which, func(t *testing.T) {
			f := hookFixture(t)
			dir := hookTestDir(t)
			s, err := ListenHookService(HookServiceConfig{StateDir: dir, Bridge: f.b})
			if err != nil {
				t.Fatal(err)
			}
			s.peer = func(ctx context.Context, c *net.UnixConn) (NativeUnixPeer, error) {
				p, e := ReadNativeUnixPeer(ctx, c)
				switch which {
				case "wrong_uid":
					p.UID++
				case "unknown_uid":
					p.UIDKnown = false
				case "unknown_pid":
					p.PID = 0
				}
				return p, e
			}
			if which == "birth" || which == "boot" || which == "exec" {
				s.config.Bridge.reader = &changingHookReader{base: f.procs, which: which}
			}
			startHookTest(t, s)
			reply := hookExchange(t, dir, hookRequestFor(f, "PreInvocation", 0))
			if reply.Code == "" {
				t.Fatal("unverified peer became authoritative")
			}
			if len(f.b.entries) != 0 {
				t.Fatal("failed identity published binding")
			}
		})
	}
}

type changingHookReader struct {
	base  processFixture
	which string
	reads int
}

func (r *changingHookReader) ReadAntigravityProcess(ctx context.Context, pid int) (ports.AntigravityProcess, error) {
	p, e := r.base.ReadAntigravityProcess(ctx, pid)
	if pid == os.Getpid() {
		r.reads++
		if r.reads > 1 {
			switch r.which {
			case "birth":
				p.Birth = "changed"
			case "boot":
				p.BootID = "changed"
			case "exec":
				p.KernelGeneration = "changed"
			}
		}
	}
	return p, e
}

func TestHookServicePersistsSequenceAcrossRestart(t *testing.T) {
	f := hookFixture(t)
	dir := hookTestDir(t)
	s, err := ListenHookService(HookServiceConfig{StateDir: dir, Bridge: f.b})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx) }()
	if r := hookExchange(t, dir, hookRequestFor(f, "PreInvocation", 0)); r.Code != "" {
		t.Fatal(r.Code)
	}
	first := f.b.entries[processKey(f.procs[41])].Binding.Sequence
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	b, err := NewBridge(f.home, f.state, f.exe, f.procs, f.b.resolve)
	if err != nil {
		t.Fatal(err)
	}
	if b.entries[processKey(f.procs[41])].Binding.Verified {
		t.Fatal("restart trusted recovered evidence")
	}
	s, err = ListenHookService(HookServiceConfig{StateDir: dir, Bridge: b})
	if err != nil {
		t.Fatal(err)
	}
	startHookTest(t, s)
	// Native counters reset on resume; a different event is still assigned
	// a strictly greater durable service sequence by the same live provider.
	if r := hookExchange(t, dir, hookRequestFor(f, "PostInvocation", 0)); r.Code != "" {
		t.Fatal(r.Code)
	}
	if next := b.entries[processKey(f.procs[41])].Binding.Sequence; next <= first {
		t.Fatalf("sequence reused %d <= %d", next, first)
	}
}

func TestHookServiceConcurrentAllocationIsBoundedAndUnique(t *testing.T) {
	f := hookFixture(t)
	dir := hookTestDir(t)
	var sequences []uint64
	s, err := ListenHookService(HookServiceConfig{StateDir: dir, Bridge: f.b, PreInvocationResponse: func(_ context.Context, b ports.AntigravityBinding) ([]byte, error) {
		sequences = append(sequences, b.Sequence)
		return []byte(`{}`), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	startHookTest(t, s)
	var wg sync.WaitGroup
	for i := 0; i < MaxHookConnections; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r := hookExchange(t, dir, hookRequestFor(f, "PreInvocation", n))
			if r.Code != "" && r.Code != "antigravity_hook_busy" {
				t.Errorf("unexpected rejection %s", r.Code)
			}
		}(i)
	}
	wg.Wait()
	if len(sequences) == 0 {
		t.Fatal("no concurrent request succeeded")
	}
	for i, n := range sequences {
		if n == 0 || (i > 0 && n <= sequences[i-1]) {
			t.Fatal("sequence allocation raced")
		}
	}
}

func TestHookServiceFailureDoesNotReturnNativeSuccess(t *testing.T) {
	for _, which := range []string{"disk", "response_error", "response_invalid", "malformed", "oversize", "version"} {
		t.Run(which, func(t *testing.T) {
			f := hookFixture(t)
			dir := hookTestDir(t)
			cfg := HookServiceConfig{StateDir: dir, Bridge: f.b}
			if which == "disk" {
				f.b.statePath = filepath.Join(f.home, "missing", "state")
			}
			if which == "response_error" {
				cfg.PreInvocationResponse = func(context.Context, ports.AntigravityBinding) ([]byte, error) {
					return nil, fmt.Errorf("synthetic private error")
				}
			}
			if which == "response_invalid" {
				cfg.PreInvocationResponse = func(context.Context, ports.AntigravityBinding) ([]byte, error) { return []byte(`[]`), nil }
			}
			s, err := ListenHookService(cfg)
			if err != nil {
				t.Fatal(err)
			}
			startHookTest(t, s)
			r := hookRequestFor(f, "PreInvocation", 0)
			switch which {
			case "malformed":
				r.Payload = []byte(`{"pid":123}`)
			case "oversize":
				r.Payload = json.RawMessage(`"` + string(bytes.Repeat([]byte{'a'}, MaxHookPayloadBytes)) + `"`)
			case "version":
				r.Version = 2
			}
			reply := hookExchange(t, dir, r)
			if reply.Code == "" || len(reply.Response) != 0 {
				t.Fatalf("failure returned native success: %+v", reply)
			}
			if which == "disk" && len(f.b.entries) != 0 {
				t.Fatal("disk failure published binding")
			}
		})
	}
}

func TestHookServiceBusyPartialFrameDeadlineAndShutdown(t *testing.T) {
	f := hookFixture(t)
	dir := hookTestDir(t)
	s, err := ListenHookService(HookServiceConfig{StateDir: dir, Bridge: f.b})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx) }()
	defer cancel()
	var held []net.Conn
	defer func() {
		for _, c := range held {
			_ = c.Close()
		}
	}()
	for i := 0; i < MaxHookConnections; i++ {
		c, e := net.Dial("unix", HookSocketPath(dir))
		if e != nil {
			t.Fatal(e)
		}
		held = append(held, c)
		_, _ = c.Write([]byte{0, 0})
	}
	deadline := time.Now().Add(time.Second)
	for f.b.hookConnections.Load() != MaxHookConnections && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if r := hookExchange(t, dir, hookRequestFor(f, "PreInvocation", 0)); r.Code != "antigravity_hook_busy" {
		t.Fatalf("full server: %+v", r)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown left a blocked accepted connection")
	}
	if f.b.hookConnections.Load() != 0 {
		t.Fatal("connections not reaped")
	}
	if _, err = os.Lstat(HookSocketPath(dir)); !os.IsNotExist(err) {
		t.Fatal("owned socket survived shutdown")
	}
	// A fresh accepted connection with an incomplete frame reaches its
	// deadline and closes without publishing a binding.
	s, err = ListenHookService(HookServiceConfig{StateDir: dir, Bridge: f.b})
	if err != nil {
		t.Fatal(err)
	}
	startHookTest(t, s)
	c, err := net.Dial("unix", HookSocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = c.Write([]byte{0, 0})
	var one [1]byte
	start := time.Now()
	_, err = c.Read(one[:])
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("partial frame not bounded: %v", err)
	}
	if len(f.b.entries) != 0 {
		t.Fatal("partial frame published authority")
	}
}

func TestHookServiceRefusesForeignPathsAndKeepsReplacement(t *testing.T) {
	f := hookFixture(t)
	for _, which := range []string{"mode", "symlink", "file", "active", "stale"} {
		t.Run(which, func(t *testing.T) {
			dir := hookTestDir(t)
			switch which {
			case "mode":
				_ = os.Chmod(dir, 0755)
			case "symlink":
				target := hookTestDir(t)
				_ = os.Remove(dir)
				_ = os.Symlink(target, dir)
			case "file":
				_ = os.WriteFile(HookSocketPath(dir), []byte("foreign"), 0600)
			case "stale", "active":
				l, e := net.ListenUnix("unix", &net.UnixAddr{Name: HookSocketPath(dir), Net: "unix"})
				if e != nil {
					t.Fatal(e)
				}
				l.SetUnlinkOnClose(false)
				if which == "stale" {
					_ = l.Close()
				} else {
					defer l.Close()
				}
			}
			s, err := ListenHookService(HookServiceConfig{StateDir: dir, Bridge: f.b})
			if err == nil {
				s.Close()
				t.Fatal("unsafe or preexisting path accepted")
			}
			if which == "file" {
				data, _ := os.ReadFile(HookSocketPath(dir))
				if string(data) != "foreign" {
					t.Fatal("foreign bytes removed")
				}
			}
		})
	}
	dir := hookTestDir(t)
	s, err := ListenHookService(HookServiceConfig{StateDir: dir, Bridge: f.b})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(HookSocketPath(dir)); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(HookSocketPath(dir), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	s.Close()
	data, _ := os.ReadFile(HookSocketPath(dir))
	if string(data) != "replacement" {
		t.Fatal("cleanup removed a foreign inode")
	}
}

func TestHookServiceRejectsOversizedFrameBeforeBody(t *testing.T) {
	f := hookFixture(t)
	dir := hookTestDir(t)
	s, err := ListenHookService(HookServiceConfig{StateDir: dir, Bridge: f.b})
	if err != nil {
		t.Fatal(err)
	}
	startHookTest(t, s)
	c, err := net.Dial("unix", HookSocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	var frame [4]byte
	binary.BigEndian.PutUint32(frame[:], MaxHookFrameBytes+1)
	_, _ = c.Write(frame[:])
	data, err := readHookFrame(c)
	if err != nil {
		t.Fatal(err)
	}
	var reply hookReply
	_ = json.Unmarshal(data, &reply)
	if reply.Code != "antigravity_hook_frame_too_large" {
		t.Fatal(reply.Code)
	}
}

func TestHookClientRealTransportOnlyWritesApprovedResponse(t *testing.T) {
	f := hookFixture(t)
	dir := hookTestDir(t)
	s, err := ListenHookService(HookServiceConfig{StateDir: dir, Bridge: f.b, PreInvocationResponse: func(context.Context, ports.AntigravityBinding) ([]byte, error) {
		return []byte(`{"additionalContext":"synthetic immutable context"}`), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	startHookTest(t, s)
	for i, event := range []string{"PreInvocation", "PreToolUse", "PostInvocation"} {
		var out bytes.Buffer
		r := hookRequestFor(f, event, i)
		if err = ObserveHook(context.Background(), dir, event, nil, bytes.NewReader(r.Payload), &out); err != nil {
			t.Fatal(err)
		}
		want := "{}\n"
		if event == "PreInvocation" {
			want = "{\"additionalContext\":\"synthetic immutable context\"}\n"
		}
		if event == "PreToolUse" {
			want = "{\"decision\":\"ask\"}\n"
		}
		if out.String() != want {
			t.Fatalf("unapproved response %q", out.String())
		}
	}
	var out bytes.Buffer
	err = ObserveHook(context.Background(), dir, "PreInvocation", nil, bytes.NewReader([]byte(`{}`)), &out)
	wantCode(t, err, "antigravity_payload_invalid")
	if out.Len() != 0 {
		t.Fatal("rejection wrote native success")
	}
}

func TestHookClientRejectsMalformedDisconnectTimeoutAndOutputFailure(t *testing.T) {
	for _, which := range []string{"malformed", "disconnect", "timeout", "output", "error_payload"} {
		t.Run(which, func(t *testing.T) {
			dir := hookTestDir(t)
			l, err := net.ListenUnix("unix", &net.UnixAddr{Name: HookSocketPath(dir), Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			if err = os.Chmod(HookSocketPath(dir), 0600); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				c, e := l.AcceptUnix()
				if e != nil {
					return
				}
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(time.Second))
				_, _ = readHookFrame(c)
				switch which {
				case "malformed":
					_, _ = c.Write([]byte{0, 0, 0, 1, '['})
				case "disconnect":
					return
				case "timeout":
					var data [1]byte
					_, _ = c.Read(data[:])
				case "error_payload":
					_ = writeHookFrame(c, hookReply{Version: 1, Code: "private raw text", Response: []byte(`{}`)})
				case "output":
					_ = writeHookFrame(c, hookReply{Version: 1, Response: []byte(`{}`)})
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			var out bytes.Buffer
			var writer io.Writer = &out
			if which == "output" {
				writer = brokenHookWriter{}
			}
			err = ObserveHook(ctx, dir, "PreInvocation", nil, bytes.NewReader([]byte(`{}`)), writer)
			if err == nil {
				t.Fatal("bad server returned success")
			}
			if out.Len() != 0 {
				t.Fatal("failure produced native stdout")
			}
			<-done
		})
	}
}

func TestHookClientStalledNativeStdinHonorsCancellation(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- ObserveHook(ctx, "/synthetic-absent", "PreInvocation", nil, r, &out) }()
	cancel()
	select {
	case err = <-done:
		wantCode(t, err, "antigravity_hook_timeout")
	case <-time.After(time.Second):
		t.Fatal("stalled native stdin leaked a reader")
	}
	if out.Len() != 0 {
		t.Fatal("cancelled stdin produced native success")
	}
}

func TestHookServiceTrustedAdmissionPrecedesObservation(t *testing.T) {
	for _, which := range []string{"managed", "refused", "wrong_capability", "changed_process", "mutated_payload"} {
		t.Run(which, func(t *testing.T) {
			f := hookFixture(t)
			dir := hookTestDir(t)
			capability := "synthetic-private-capability"
			capPath := filepath.Join(f.home, "capability")
			if err := os.WriteFile(capPath, []byte(capability), 0600); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256([]byte(capability))
			admitted := false
			admit := func(ctx context.Context, peer NativeUnixPeer, w Witness, event string, raw json.RawMessage) (*HookLaunch, error) {
				if peer.PID != os.Getpid() || w.HelperPID != peer.PID || w.HelperBirth != f.procs[peer.PID].Birth || w.HelperBootID != f.procs[peer.PID].BootID || w.HelperGeneration != f.procs[peer.PID].KernelGeneration || w.Sequence == 0 || event != "PreInvocation" {
					t.Error("admission did not receive independent complete identity")
				}
				if len(f.b.entries) != 0 {
					t.Error("observation ran before admission")
				}
				if which == "refused" {
					return nil, fmt.Errorf("synthetic private admission failure")
				}
				p, err := f.b.VerifyHookAncestor(ctx, w)
				if err != nil {
					return nil, err
				}
				if err = f.b.CommitLaunch(LaunchCommitment{ID: "synthetic-launch", CapabilityPath: capPath, CapabilityDigest: hex.EncodeToString(digest[:]), Process: p}); err != nil {
					return nil, err
				}
				admitted = true
				if which == "mutated_payload" {
					other, _ := io.ReadAll(payload(conversationB, f.pathB, nil))
					copy(raw, other)
				}
				if which == "changed_process" {
					helper := f.procs[os.Getpid()]
					helper.KernelGeneration = "changed-after-admission"
					f.procs[helper.PID] = helper
				}
				if which == "wrong_capability" {
					return &HookLaunch{ID: "synthetic-launch", Capability: "wrong"}, nil
				}
				return &HookLaunch{ID: "synthetic-launch", Capability: capability}, nil
			}
			s, err := ListenHookService(HookServiceConfig{StateDir: dir, Bridge: f.b, Admit: admit, PreInvocationResponse: func(_ context.Context, b ports.AntigravityBinding) ([]byte, error) {
				if !admitted || !b.Verified || b.ConversationID != conversationA {
					t.Error("response ran without admitted exact binding")
				}
				return []byte(`{}`), nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			startHookTest(t, s)
			r := hookExchange(t, dir, hookRequestFor(f, "PreInvocation", 0))
			if which == "managed" || which == "mutated_payload" {
				if r.Code != "" {
					t.Fatal(r.Code)
				}
				if e := f.b.entries[processKey(f.procs[41])]; e.LaunchID != "synthetic-launch" || e.Binding.ConversationID != conversationA {
					t.Fatal("trusted launch or original payload lost")
				}
			} else {
				if r.Code == "" || len(f.b.entries) != 0 {
					t.Fatal("failed admission or changed identity published native success")
				}
			}
		})
	}
}

func TestHookServiceCompletesObservationBeforeAcknowledging(t *testing.T) {
	for _, which := range []string{"complete", "callback_failure", "unverified_peer"} {
		t.Run(which, func(t *testing.T) {
			f := hookFixture(t)
			dir := hookTestDir(t)
			seen := make(chan ports.AntigravityBinding, 1)
			s, err := ListenHookService(HookServiceConfig{StateDir: dir, Bridge: f.b,
				AfterObservation: func(ctx context.Context, b ports.AntigravityBinding) error {
					current, err := f.b.AntigravityBinding(ctx, f.s)
					if err != nil || !b.Verified || current.Sequence != b.Sequence || current.ConversationID != b.ConversationID {
						return fmt.Errorf("observation was not persisted")
					}
					seen <- b
					if which == "callback_failure" {
						return fmt.Errorf("synthetic completion failure")
					}
					return nil
				}})
			if err != nil {
				t.Fatal(err)
			}
			if which == "unverified_peer" {
				s.peer = func(context.Context, *net.UnixConn) (NativeUnixPeer, error) {
					return NativeUnixPeer{}, nil
				}
			}
			startHookTest(t, s)
			reply := hookExchange(t, dir, hookRequestFor(f, "Stop", 1))
			if which == "unverified_peer" {
				if reply.Code == "" || len(seen) != 0 {
					t.Fatal("unverified peer reached completion")
				}
				return
			}
			select {
			case b := <-seen:
				if b.Event != "Stop" {
					t.Fatal("completion received the wrong event")
				}
			default:
				t.Fatal("hook returned before observation completed")
			}
			if which == "callback_failure" {
				if reply.Code != "antigravity_hook_response_failed" || len(reply.Response) != 0 {
					t.Fatal("failed completion was acknowledged", reply)
				}
			} else if reply.Code != "" || string(reply.Response) != "{}" {
				t.Fatal("completion changed the native response", reply)
			}
		})
	}
}
