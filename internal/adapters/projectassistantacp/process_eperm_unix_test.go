//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package projectassistantacp

import (
	"context"
	"errors"
	"sync"
	"syscall"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// On macOS, kill(-pgid) answers EPERM when every member of the group is a
// zombie or is still tearing itself down after SIGKILL. ps shows the latter as
// "?E" ("trying to exit"), which the census used to read as a live process, so
// a Bot kept its owner until Clawdline restarted (2026-11-23, Beacon and Lens).
func TestProcessGroupOfExitingMembersIsStopped(t *testing.T) {
	permissionErr := syscall.EPERM
	exiting := []processIdentity{{pid: 10, pgid: 10, state: "Z", stamp: "root"}, {pid: 11, ppid: 1, pgid: 10, state: "?E", stamp: "child"}}
	if err := confirmStoppedGroup(exiting, 10, "root", permissionErr); err != nil {
		t.Fatalf("a group whose only members are exiting was reported live: %v", err)
	}
	stillRunning := []processIdentity{{pid: 10, pgid: 10, state: "Z", stamp: "root"}, {pid: 11, pgid: 10, state: "R", stamp: "child"}}
	if err := confirmStoppedGroup(stillRunning, 10, "root", permissionErr); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatalf("a running member was hidden: %v", err)
	}
}

// A process group id cannot be allocated again while the group exists. A
// leader with our group id but another start time therefore proves that our
// group is gone and that a stranger's group answered the signal.
func TestProcessGroupReusedByAnotherProcessIsNotOurs(t *testing.T) {
	reused := []processIdentity{{pid: 10, ppid: 1, pgid: 10, state: "Ss", stamp: "stranger"}, {pid: 12, ppid: 10, pgid: 10, state: "S", stamp: "stranger-child"}}
	if err := confirmStoppedGroup(reused, 10, "root", syscall.EPERM); err != nil {
		t.Fatalf("a stranger reusing the group id kept our Bot unknown: %v", err)
	}
	ours := []processIdentity{{pid: 10, pgid: 10, state: "S", stamp: "root"}}
	if err := confirmStoppedGroup(ours, 10, "root", syscall.EPERM); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatalf("our own live leader looked stopped: %v", err)
	}
	orphaned := []processIdentity{{pid: 12, ppid: 1, pgid: 10, state: "S", stamp: "uncaptured"}}
	if err := confirmStoppedGroup(orphaned, 10, "root", syscall.EPERM); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatalf("a leaderless remnant of our group looked stopped: %v", err)
	}
}

type signalRecorder struct {
	mu    sync.Mutex
	calls []int
	deny  func(pid int) bool
}

func (s *signalRecorder) kill(pid int, _ syscall.Signal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, pid)
	if s.deny != nil && s.deny(pid) {
		return syscall.EPERM
	}
	return nil
}

// A denied group signal that a fresh census still sees as live is unknown,
// but a later Close call re-verifies with a new census instead of returning
// the first answer forever, and releases once our processes are gone.
func TestProcessCloseReverifiesDeniedGroupSignal(t *testing.T) {
	var mu sync.Mutex
	alive := true
	var p *Process
	p = reapedCloseFixture(t, func(context.Context) ([]processIdentity, error) {
		mu.Lock()
		defer mu.Unlock()
		if !alive {
			return []processIdentity{}, nil
		}
		root := p.cmd.Process.Pid
		return []processIdentity{{pid: root, pgid: root, state: "S", stamp: "already-reaped-peer"}}, nil
	})
	group := p.cmd.Process.Pid
	signals := &signalRecorder{deny: func(pid int) bool { return pid == -group }}
	p.signal = signals.kill
	err := p.Close(context.Background())
	var detail *ProcessSignalFailure
	if !errors.As(err, &detail) || detail.Phase != "signal_group_live" || !errors.Is(err, syscall.EPERM) {
		t.Fatalf("injected EPERM against a live group was not unknown: %v", err)
	}
	mu.Lock()
	alive = false
	mu.Unlock()
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("a fresh census without our processes did not release cleanup: %v", err)
	}
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("a settled receipt changed: %v", err)
	}
}

// An inventory gap means ownership itself is incomplete. A later complete
// census cannot prove that an uncaptured descendant stopped, so it stays.
func TestProcessCloseKeepsInventoryGapsUnknown(t *testing.T) {
	cause := errors.New("inventory denied")
	p := reapedCloseFixture(t, func(context.Context) ([]processIdentity, error) { return []processIdentity{}, nil })
	p.cleanupErr = signalFailure("descendant_inventory", cause)
	p.cleanupFinal = true
	if err := p.Close(context.Background()); !errors.Is(err, cause) {
		t.Fatalf("first close lost the inventory gap: %v", err)
	}
	if err := p.Close(context.Background()); !errors.Is(err, cause) {
		t.Fatalf("a re-verification erased an inventory gap: %v", err)
	}
}

func TestReverifiablePhases(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{signalFailure("signal_group_live", syscall.EPERM), true},
		{signalFailure("signal_pid_live", syscall.EPERM), true},
		{errors.Join(signalFailure("signal_pid_live", syscall.EPERM), signalFailure("signal_group_live", syscall.EPERM)), true},
		{signalFailure("descendant_inventory", errors.New("x")), false},
		{errors.Join(signalFailure("signal_group_live", syscall.EPERM), signalFailure("cleanup_survey", context.DeadlineExceeded)), false},
		{refusal(pa.ExecutionEffectUnknown, "root_identity"), false},
	} {
		if got := reverifiable(tc.err); got != tc.want {
			t.Errorf("reverifiable(%v)=%v, want %v", tc.err, got, tc.want)
		}
	}
}
