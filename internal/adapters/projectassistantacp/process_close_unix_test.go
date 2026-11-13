//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package projectassistantacp

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

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
	return &Process{cmd: cmd, stdin: stdin, stdout: io.NopCloser(strings.NewReader("")), stderrReader: io.NopCloser(strings.NewReader("")), limits: testLimits(), done: done, surveyDone: surveyed, stopSurvey: make(chan struct{}), owned: map[int]string{cmd.Process.Pid: "already-reaped-peer"}, rootObserved: true, rootStamp: "already-reaped-peer", censusOverride: census}
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
