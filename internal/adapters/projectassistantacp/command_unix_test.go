//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package projectassistantacp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestOwnedCommandPeer(t *testing.T) {
	mode := os.Getenv("CLAWDLINE_SYNTHETIC_COMMAND")
	if mode == "" {
		return
	}
	if mode == "fail" {
		os.Exit(7)
	}
	if mode == "streams" {
		fmt.Fprintln(os.Stdout, "synthetic stdout")
		fmt.Fprintln(os.Stderr, "synthetic stderr")
		os.Exit(0)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(8)
	}
	fmt.Fprintln(os.Stdout, listener.Addr().String())
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, os.Getenv("SYNTHETIC_PROVIDER_SECRET"))
		fmt.Fprint(w, "synthetic foreground server")
	})}
	stopped := make(chan struct{})
	go func() {
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		close(stopped)
	}()
	_ = server.Serve(listener)
	<-stopped
	os.Exit(0)
}

func startOwnedCommand(t *testing.T, ctx context.Context, mode string) *Process {
	return startOwnedCommandOutput(t, ctx, mode, false)
}

func startOwnedCommandOutput(t *testing.T, ctx context.Context, mode string, combined bool) *Process {
	t.Helper()
	if inventoryUnavailable() {
		t.Skip("owned command startup requires process inventory")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	l := testLimits()
	l.CleanupTimeout = 5 * time.Second
	p, err := StartProcess(ctx, ProcessOptions{Configuration: Configuration{
		Executable: executable, Arguments: []string{"-test.run=^TestOwnedCommandPeer$"},
		Environment: []string{"CLAWDLINE_SYNTHETIC_COMMAND=" + mode, "GORACE=atexit_sleep_ms=0"},
	}, Directory: filepath.Clean(t.TempDir()), Limits: l, CombinedOutput: combined})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		if err := p.Close(ctx); err != nil {
			t.Errorf("owned command cleanup: %v", err)
		}
	})
	return p
}

func TestOwnedCommandCanCombineBothOutputStreamsWithoutAnACPReader(t *testing.T) {
	p := startOwnedCommandOutput(t, context.Background(), "streams", true)
	body, err := io.ReadAll(p.Output())
	if err != nil || string(body) != "synthetic stdout\nsynthetic stderr\n" {
		t.Fatalf("combined output=%q %v", body, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.WaitExit(ctx); err != nil {
		t.Fatal(err)
	}
	if p.Stderr() != "" {
		t.Fatal("combined output was also retained by a second diagnostic consumer")
	}
}

func commandAddress(t *testing.T, p *Process) string {
	t.Helper()
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(p.Output())
		if scanner.Scan() {
			ready <- scanner.Text()
		} else {
			ready <- ""
		}
	}()
	select {
	case address := <-ready:
		if _, _, err := net.SplitHostPort(address); err != nil {
			t.Fatalf("command did not announce a listener: %q (%v)", address, err)
		}
		return address
	case <-time.After(5 * time.Second):
		t.Fatal("command output did not arrive")
		return ""
	}
}

func TestOwnedForegroundCommandOutlivesLaunchingRequest(t *testing.T) {
	t.Setenv("SYNTHETIC_PROVIDER_SECRET", "must not reach the command")
	ctx, cancel := context.WithCancel(context.Background())
	p := startOwnedCommand(t, ctx, "server")
	address := commandAddress(t, p)
	cancel()
	if err := p.WaitExit(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait = %v", err)
	}
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + address + "/")
	if err != nil {
		t.Fatalf("launch cancellation stopped the server: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil || string(body) != "synthetic foreground server" {
		t.Fatalf("command output/environment = %q, %v", body, readErr)
	}
	cleanup, stop := context.WithTimeout(context.Background(), 6*time.Second)
	defer stop()
	if err := p.Close(cleanup); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get("http://" + address + "/"); err == nil {
		t.Fatal("owned server still accepted requests after cleanup")
	}
	if err := p.WaitExit(cleanup); err == context.DeadlineExceeded {
		t.Fatal("cleanup did not obtain the direct child's exit receipt")
	}
}

func TestOwnedCommandExitReceiptIsStable(t *testing.T) {
	for _, mode := range []string{"fail", "server"} {
		t.Run(mode, func(t *testing.T) {
			p := startOwnedCommand(t, context.Background(), mode)
			if mode == "server" {
				_ = commandAddress(t, p)
				if err := p.Write(context.Background(), []byte("exit\n")); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			first := p.WaitExit(ctx)
			cancel()
			second := p.WaitExit(ctx)
			if first != second {
				t.Fatalf("completed receipt changed after observer cancellation: %v -> %v", first, second)
			}
			if mode == "server" && first != nil {
				t.Fatal(first)
			}
			if mode == "fail" {
				var exit *exec.ExitError
				if !errors.As(first, &exit) || exit.ExitCode() != 7 {
					t.Fatalf("failed command lost its actual exit: %v", first)
				}
			}
		})
	}
}

type commandJournal struct {
	mu      sync.Mutex
	record  servers.Record
	changed chan servers.Record
}

func (j *commandJournal) Load(context.Context, servers.Identity) (servers.Record, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.record, nil
}
func (j *commandJournal) Save(_ context.Context, version int64, r servers.Record) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if version != j.record.Version {
		return errors.New("synthetic CAS conflict")
	}
	j.record = r
	j.changed <- r
	return nil
}

func TestServerAttemptOwnsARealCommandAfterItsRequestEnds(t *testing.T) {
	if inventoryUnavailable() {
		t.Skip("owned command startup requires process inventory")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	j := &commandJournal{changed: make(chan servers.Record, 8)}
	processes := make(chan *Process, 1)
	limits := testLimits()
	limits.CleanupTimeout = 5 * time.Second
	owner, err := servers.Open(context.Background(), servers.Options{
		Identity: servers.Identity{Key: pa.BotKey{MachineID: "synthetic-machine", BotID: "synthetic-bot"},
			Project:  pa.ProjectScope{MachineID: "synthetic-machine", ProjectID: "synthetic-project", CanonicalID: "synthetic-repository", Kind: pa.RepositoryProject},
			ServerID: "synthetic-server", AttemptID: "synthetic-attempt", Fingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Lifetime: context.Background(), Journal: j, CleanupTimeout: 6 * time.Second,
		Check: func(ctx context.Context) error { return ctx.Err() },
		Launch: func(ctx context.Context) (servers.Process, error) {
			p, err := StartProcess(ctx, ProcessOptions{Configuration: Configuration{Executable: exe,
				Arguments: []string{"-test.run=^TestOwnedCommandPeer$"}, Environment: []string{"CLAWDLINE_SYNTHETIC_COMMAND=server", "GORACE=atexit_sleep_ms=0"}}, Directory: dir, Limits: limits})
			if err != nil {
				return nil, err
			}
			processes <- p
			return p, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		if err := owner.Close(ctx); err != nil {
			t.Errorf("server owner cleanup: %v", err)
		}
	})
	request, cancel := context.WithCancel(context.Background())
	defer cancel()
	if r, err := owner.Start(request); err != nil || r.State != servers.Accepted {
		t.Fatalf("accepted=%+v %v", r, err)
	}
	var p *Process
	select {
	case p = <-processes:
	case <-time.After(5 * time.Second):
		t.Fatal("command was not started")
	}
	address := commandAddress(t, p)
	running := false
	for !running {
		select {
		case r := <-j.changed:
			running = r.State == servers.Running
		case <-time.After(5 * time.Second):
			t.Fatal("real command did not receive a running receipt")
		}
	}
	cancel()
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + address + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil || string(body) != "synthetic foreground server" {
		t.Fatalf("real owner output=%q %v", body, readErr)
	}
	if err := owner.Close(context.Background()); err != nil || owner.Snapshot().State != servers.Stopped {
		t.Fatalf("real owner stop=%+v %v", owner.Snapshot(), err)
	}
	if _, err := client.Get("http://" + address + "/"); err == nil {
		t.Fatal("server attempt lost its real process cleanup")
	}
}
