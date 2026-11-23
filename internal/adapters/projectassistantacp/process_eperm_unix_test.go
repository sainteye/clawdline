//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package projectassistantacp

import (
	"context"
	"errors"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

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

// A census that fails for a passing reason (the 2026-11-24 ENOMEM race, a
// denied read, a timeout) leaves a gap in what was seen. That gap used to be
// stored as final, so every later Close returned the first error and the Bot
// kept its owner until Clawdline restarted. A later Close now takes a complete
// census again and releases once nothing from the gap can still be ours.
func TestProcessCloseReinventoriesAfterATransientCensusFailure(t *testing.T) {
	for _, cause := range []error{syscall.ENOMEM, syscall.EPERM, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			calls := 0
			p := reapedCloseFixture(t, func(context.Context) ([]processIdentity, error) {
				calls++
				if calls == 1 {
					return nil, cause
				}
				return []processIdentity{}, nil
			})
			err := p.Close(context.Background())
			var detail *ProcessSignalFailure
			if !errors.As(err, &detail) || detail.Phase != "descendant_inventory" || !errors.Is(err, cause) {
				t.Fatalf("the injected census failure was not reported first: %v", err)
			}
			if err := p.Close(context.Background()); err != nil {
				t.Fatalf("a later complete census did not release cleanup: %v", err)
			}
			if err := p.Close(context.Background()); err != nil {
				t.Fatalf("a settled receipt changed: %v", err)
			}
		})
	}
}

// What a gap can hide is a descendant that left our process group and whose
// parent exited unseen: it is reparented to the reaper and can no longer be
// attributed. A live orphan that started during the gap therefore keeps
// cleanup unknown until it is gone.
func TestProcessCloseKeepsAnOrphanFromTheGapUnknown(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	orphan := true
	p := reapedCloseFixture(t, func(context.Context) ([]processIdentity, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return nil, syscall.ENOMEM
		}
		if !orphan {
			return []processIdentity{}, nil
		}
		born := time.Now().UTC().Format(time.RFC3339Nano)
		return []processIdentity{{pid: 4242, ppid: 1, pgid: 4242, state: "Ss", stamp: born}}, nil
	})
	if err := p.Close(context.Background()); !errors.Is(err, syscall.ENOMEM) {
		t.Fatalf("first close lost the census failure: %v", err)
	}
	if err := p.Close(context.Background()); !errors.Is(err, syscall.ENOMEM) {
		t.Fatalf("an orphan born during the gap was released: %v", err)
	}
	mu.Lock()
	orphan = false
	mu.Unlock()
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("cleanup stayed unknown after the orphan was gone: %v", err)
	}
}

// Phases whose evidence can never become complete stay locked however many
// later censuses succeed.
func TestProcessCloseKeepsFinalPhasesLocked(t *testing.T) {
	for _, phase := range []string{"root_identity", "owned_descendants"} {
		t.Run(phase, func(t *testing.T) {
			p := reapedCloseFixture(t, func(context.Context) ([]processIdentity, error) { return []processIdentity{}, nil })
			p.setCleanupError(refusal(pa.ExecutionEffectUnknown, phase))
			for range 2 {
				if err := p.Close(context.Background()); pa.Code(err) != pa.ExecutionEffectUnknown {
					t.Fatalf("a final phase was released: %v", err)
				}
			}
		})
	}
}

// Whether a failure can be re-verified follows from its phase: every phase is
// re-verifiable unless finalCleanupPhases names it with a reason, so a phase
// added later does not silently lock a Bot.
func TestCleanupPhasesAreReverifiableUnlessMarkedFinal(t *testing.T) {
	for phase, why := range finalCleanupPhases {
		if strings.TrimSpace(why) == "" {
			t.Errorf("final phase %s has no reason", phase)
		}
		if reverifiable(refusal(pa.ExecutionEffectUnknown, phase)) || reverifiable(signalFailure(phase, syscall.EPERM)) {
			t.Errorf("final phase %s was re-verifiable", phase)
		}
	}
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{signalFailure("signal_group_live", syscall.EPERM), true},
		{signalFailure("descendant_inventory", syscall.ENOMEM), true},
		{signalFailure("cleanup_survey", context.DeadlineExceeded), true},
		{signalFailure("a_phase_added_later", errors.New("x")), true},
		{refusal(pa.ExecutionEffectUnknown, "another_phase_added_later"), true},
		{errors.New("an error with no phase"), true},
		{errors.Join(signalFailure("signal_pid_live", syscall.EPERM), signalFailure("descendant_inventory", syscall.ENOMEM)), true},
		{errors.Join(signalFailure("signal_group_live", syscall.EPERM), refusal(pa.CapacityFull, "owned_descendants")), false},
		{nil, false},
	} {
		if got := reverifiable(tc.err); got != tc.want {
			t.Errorf("reverifiable(%v)=%v, want %v", tc.err, got, tc.want)
		}
	}
}
