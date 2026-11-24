//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package projectassistantacp

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Re-exec the same fixture to enter a separate Unix session without an
// external interpreter. The escaped child requires KILL after TERM.
func TestACPDescendantPeer(t *testing.T) {
	path := os.Getenv("CLAWDLINE_ACP_PID_FILE")
	if os.Getenv("CLAWDLINE_ACP_TEST_PEER") != "1" || path == "" {
		return
	}
	switch os.Getenv("CLAWDLINE_ACP_TEST_MODE") {
	case "descendant":
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		child := exec.Command(executable, "-test.run=^TestACPDescendantPeer$")
		child.Env = append(os.Environ(), "CLAWDLINE_ACP_TEST_MODE=escaped")
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, os.Stdin)
	case "escaped":
		signal.Ignore(syscall.SIGTERM)
		if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Minute)
	default:
		return
	}
	os.Exit(0)
}

// This process has actually exited and been reaped. Any failure fallback can
// only use its completed os.Process handle, never a synthetic live PID.
func reapedCloseFixture(t *testing.T, census func(context.Context) ([]processIdentity, error)) *Process {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestACPTestPeer$")
	cmd.Env = []string{"CLAWDLINE_ACP_TEST_PEER=0"}
	if err = cmd.Run(); err != nil {
		t.Fatal(err)
	}
	stdin, err := os.CreateTemp(t.TempDir(), "closed-peer-input")
	if err != nil {
		t.Fatal(err)
	}
	done, surveyed := make(chan struct{}), make(chan struct{})
	close(done)
	close(surveyed)
	return &Process{cmd: cmd, stdin: stdin, stdout: io.NopCloser(strings.NewReader("")), stderrReader: io.NopCloser(strings.NewReader("")), limits: testLimits(), done: done, surveyDone: surveyed, stopSurvey: func() {}, owned: map[int]string{cmd.Process.Pid: "already-reaped-peer"}, rootObserved: true, rootStamp: "already-reaped-peer", censusOverride: census, started: time.Now()}
}

func TestProcessCloseDoesNotReinventoryVerifiedEmptyOwnership(t *testing.T) {
	calls := 0
	p := reapedCloseFixture(t, func(ctx context.Context) ([]processIdentity, error) {
		calls++
		if calls > 2 {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return []processIdentity{}, nil
	})
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("fresh complete empty census and reaped peer must complete cleanup: %v; census calls=%d", err, calls)
	}
	if calls != 2 {
		t.Fatalf("census calls=%d, want the existing before/after grace observations only", calls)
	}
	if err := p.Close(context.Background()); err != nil || calls != 2 {
		t.Fatalf("repeated close changed its receipt: err=%v calls=%d", err, calls)
	}
}

func TestProcessCloseEmptyOwnershipDoesNotEraseUnknown(t *testing.T) {
	t.Run("cancelled cleanup census", func(t *testing.T) {
		p := reapedCloseFixture(t, func(ctx context.Context) ([]processIdentity, error) { return nil, ctx.Err() })
		clear(p.owned)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := p.Close(ctx)
		if pa.Code(err) != pa.ExecutionEffectUnknown || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled cleanup census was accepted as complete: %v", err)
		}
	})
	t.Run("earlier failure", func(t *testing.T) {
		cause := errors.New("earlier inventory unavailable")
		p := reapedCloseFixture(t, func(context.Context) ([]processIdentity, error) { return []processIdentity{}, nil })
		p.cleanupErr = signalFailure("descendant_inventory", cause)
		err := p.Close(context.Background())
		if pa.Code(err) != pa.ExecutionEffectUnknown || !errors.Is(err, cause) {
			t.Fatalf("empty ownership erased an earlier unknown: %v", err)
		}
	})
	t.Run("latest census failed", func(t *testing.T) {
		calls := 0
		cause := errors.New("fresh inventory unavailable")
		p := reapedCloseFixture(t, func(context.Context) ([]processIdentity, error) {
			calls++
			if calls == 1 {
				return []processIdentity{}, nil
			}
			return nil, cause
		})
		err := p.Close(context.Background())
		if pa.Code(err) != pa.ExecutionEffectUnknown || !errors.Is(err, cause) {
			t.Fatalf("empty ownership hid a fresh census failure: %v", err)
		}
	})
}
