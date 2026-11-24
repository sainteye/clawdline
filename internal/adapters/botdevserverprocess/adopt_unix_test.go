//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package botdevserverprocess

import (
	"bufio"
	"context"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
)

// orphanServer starts the synthetic server as a previous Host would have: in
// its own process group, still listening after its owner is gone.
func orphanServer(t *testing.T) (*exec.Cmd, string, <-chan struct{}) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestForegroundServerPeer$", "--", "--synthetic-foreground-server")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }() // Reap like launchd would.
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = stdin.Close(); <-exited })
	address, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return cmd, strings.TrimSpace(address), exited
}

func TestAdoptTakesBackALiveServerAndStopsItsGroupAndPort(t *testing.T) {
	l, err := New(limits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd, address, exited := orphanServer(t)
	mark := markOf(cmd.Process.Pid)
	if mark.PID != cmd.Process.Pid || mark.Started <= 0 {
		t.Fatalf("no kernel start time for the server: %+v", mark)
	}
	_, spec := projectFixture(t)
	p, err := l.Adopt(context.Background(), spec, mark)
	if err != nil || p == nil {
		t.Fatalf("a live server with a matching start time was not adopted: %v", err)
	}
	if conn, err := net.DialTimeout("tcp", address, time.Second); err != nil {
		t.Fatalf("adopted server is not listening: %v", err)
	} else {
		_ = conn.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Close(ctx); err != nil {
		t.Fatalf("adopted server did not stop: %v", err)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("adopted server is still running after Close")
	}
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatalf("port was not released: %v", err)
	}
	_ = listener.Close()
}

func TestAdoptRefusesAStartTimeThatDoesNotMatch(t *testing.T) {
	l, err := New(limits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd, _, _ := orphanServer(t)
	mark := markOf(cmd.Process.Pid)
	_, spec := projectFixture(t)
	for _, other := range []servers.Mark{{PID: mark.PID, Started: mark.Started - 60}, {PID: mark.PID, Started: mark.Started + 60}} {
		if p, err := l.Adopt(context.Background(), spec, other); p != nil || err != nil {
			t.Fatalf("a reused PID was adopted: %+v %v", other, err)
		}
	}
}
