//go:build darwin || linux

package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNativePeerHelper(t *testing.T) {
	path := os.Getenv("CLAWDLINE_SYNTHETIC_PEER_SOCKET")
	if path == "" {
		return
	}
	conn, err := net.DialTimeout("unix", path, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	// Deliberately claim the server's PID. It must not influence credentials.
	if err := json.NewEncoder(conn).Encode(map[string]int{"pid": os.Getppid()}); err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, conn)
}

func TestNativeUnixPeerCredentialsIgnoreBody(t *testing.T) {
	// Keep below sockaddr_un's Darwin path ABI length even with long TMPDIR.
	dir, err := os.MkdirTemp("/tmp", "agy-peer-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "hook.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	_ = listener.SetDeadline(time.Now().Add(5 * time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativePeerHelper$")
	cmd.Env = append(os.Environ(), "CLAWDLINE_SYNTHETIC_PEER_SOCKET="+path)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	conn, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	before, err := ReadNativeUnixPeer(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		PID int `json:"pid"`
	}
	if err := json.NewDecoder(conn).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.PID != os.Getpid() {
		t.Fatal("forged-body control did not claim server PID")
	}
	after, err := ReadNativeUnixPeer(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	if before != after || after.PID != cmd.Process.Pid || after.PID == body.PID || !after.UIDKnown || after.UID != uint32(os.Getuid()) {
		t.Fatalf("socket witness differs from kernel connector: %+v", after)
	}
	process, err := NewNativeProcessReader(nil).ReadAntigravityProcess(ctx, after.PID)
	if err != nil || process.PID != cmd.Process.Pid || process.Birth == "" {
		t.Fatalf("peer birth could not be fenced: %v", err)
	}
	_ = conn.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	_, err = ReadNativeUnixPeer(context.Background(), conn)
	requireNativeCode(t, err, "antigravity_native_peer_unreadable")
}

func TestNativeUnixPeerCancellationAndNonUnix(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ReadNativeUnixPeer(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	_, err = ReadNativeUnixPeer(context.Background(), nil)
	requireNativeCode(t, err, "antigravity_native_peer_invalid")
	l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, err = ReadNativeUnixPeer(context.Background(), l)
	requireNativeCode(t, err, "antigravity_native_peer_invalid")
}
