//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package projectassistantacp

import (
	"context"
	"errors"
	"os"
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
// attributed. A live orphan of ours born inside the gap therefore keeps
// cleanup unknown until it is gone. Birth times are fixed relative to the
// gap's bounds (from is the Agent's start), never to the moment the census
// answers, so the result does not depend on gapSlack or on machine load.
func TestProcessCloseKeepsAnOrphanFromTheGapUnknown(t *testing.T) {
	var p *Process
	var mu sync.Mutex
	var stray *processIdentity
	p = reapedCloseFixture(t, gapCensus(syscall.ENOMEM, func() []processIdentity {
		mu.Lock()
		defer mu.Unlock()
		if stray == nil {
			return []processIdentity{}
		}
		return []processIdentity{*stray}
	}))
	set := func(row *processIdentity) { mu.Lock(); stray = row; mu.Unlock() }
	inside := p.started.Add(time.Millisecond).UTC().Format(time.RFC3339Nano)
	set(&processIdentity{pid: 4242, ppid: 1, pgid: 4242, uid: os.Getuid(), state: "Ss", stamp: inside})
	if err := p.Close(context.Background()); !errors.Is(err, syscall.ENOMEM) {
		t.Fatalf("first close lost the census failure: %v", err)
	}
	for range 2 {
		if err := p.Close(context.Background()); !errors.Is(err, syscall.ENOMEM) {
			t.Fatalf("an orphan born during the gap was released: %v", err)
		}
	}
	set(nil)
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("cleanup stayed unknown after the orphan was gone: %v", err)
	}
}

// Processes born before or after the gap, or owned by another user, are not
// what a gap can hide and release at once even when every gap is scanned.
func TestProcessCloseReleasesStrangersOutsideTheGap(t *testing.T) {
	for name, row := range map[string]func(p *Process) processIdentity{
		"born before": func(p *Process) processIdentity {
			return processIdentity{pid: 4242, ppid: 1, pgid: 4242, uid: os.Getuid(), state: "Ss", stamp: p.started.Add(-time.Hour).UTC().Format(time.RFC3339Nano)}
		},
		"born after": func(*Process) processIdentity {
			return processIdentity{pid: 4242, ppid: 1, pgid: 4242, uid: os.Getuid(), state: "Ss", stamp: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}
		},
		"other uid": func(p *Process) processIdentity {
			return processIdentity{pid: 4242, ppid: 1, pgid: 4242, uid: os.Getuid() + 1, state: "Ss", stamp: p.started.Add(time.Millisecond).UTC().Format(time.RFC3339Nano)}
		},
	} {
		t.Run(name, func(t *testing.T) {
			var p *Process
			p = reapedCloseFixture(t, gapCensus(syscall.ENOMEM, func() []processIdentity { return []processIdentity{row(p)} }))
			p.limits.UnscannedCensusGap = 0
			_ = p.Close(context.Background())
			if err := p.Close(context.Background()); err != nil {
				t.Fatalf("a stranger outside the gap kept cleanup unknown: %v", err)
			}
		})
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

// gapCensus fails its first call with cause and afterwards answers rows(),
// so the first Close records a census gap and later Closes re-verify it.
func gapCensus(cause error, rows func() []processIdentity) func(context.Context) ([]processIdentity, error) {
	var mu sync.Mutex
	calls := 0
	return func(context.Context) ([]processIdentity, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return nil, cause
		}
		return rows(), nil
	}
}

func bornNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// macOS parents almost every XPC service and app to launchd, so a live
// stranger born near a census gap is ordinary (46 on this machine within ten
// minutes, 20 of them ours). A gap no longer than the unscanned bound is an
// ordinary poll interval: nothing is scanned and cleanup releases at once.
func TestProcessCloseReleasesPastStrangersAfterAShortGap(t *testing.T) {
	for _, tc := range []struct {
		name string
		uid  int
	}{{"same uid", os.Getuid()}, {"other uid", os.Getuid() + 1}} {
		for _, cause := range []error{syscall.ENOMEM, syscall.EPERM, context.DeadlineExceeded} {
			t.Run(tc.name+"/"+cause.Error(), func(t *testing.T) {
				p := reapedCloseFixture(t, gapCensus(cause, func() []processIdentity {
					return []processIdentity{{pid: 4242, ppid: 1, pgid: 4242, uid: tc.uid, state: "Ss", stamp: bornNow()}}
				}))
				p.limits.UnscannedCensusGap = time.Minute
				if err := p.Close(context.Background()); !errors.Is(err, cause) {
					t.Fatalf("the first close lost the census failure: %v", err)
				}
				if err := p.Close(context.Background()); err != nil {
					t.Fatalf("a launchd child kept a short gap unknown: %v", err)
				}
			})
		}
	}
}

// A gap longer than the bound is scanned, but only for processes of the
// daemon's own user: an Agent cannot create a process owned by somebody else
// without a setuid program, which keeps its real user id ours.
func TestProcessCloseScansALongGapForOurUserOnly(t *testing.T) {
	p := reapedCloseFixture(t, gapCensus(syscall.ENOMEM, func() []processIdentity {
		return []processIdentity{{pid: 4242, ppid: 1, pgid: 4242, uid: os.Getuid() + 1, state: "Ss", stamp: bornNow()}}
	}))
	p.limits.UnscannedCensusGap = 0
	_ = p.Close(context.Background())
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("another user's launchd child kept a long gap unknown: %v", err)
	}
}

// A gap that opened before the first successful census used to have no lower
// bound, so every live launchd child, however old, looked born inside it and
// the gap never settled. It is now bounded by the start of the Agent.
func TestProcessCloseSettlesAGapOpenedBeforeTheFirstCensus(t *testing.T) {
	old := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	p := reapedCloseFixture(t, gapCensus(syscall.ENOMEM, func() []processIdentity {
		return []processIdentity{{pid: 4242, ppid: 1, pgid: 4242, uid: os.Getuid(), state: "Ss", stamp: old}}
	}))
	p.limits.UnscannedCensusGap = 0
	if !p.lastCensus.IsZero() {
		t.Fatal("the fixture already had a census")
	}
	_ = p.Close(context.Background())
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("an hour-old launchd child kept a gap from before the first census unknown: %v", err)
	}
}

// Orphans can only be adopted by init or by a subreaper among the daemon's
// own ancestors, since every descendant of an Agent descends from the daemon.
// Linux used to count every parent as a possible reaper, so any process born
// in the gap kept a busy machine locked.
func TestOrphanReapersAreInitAndTheDaemonsAncestors(t *testing.T) {
	rows := []processIdentity{{pid: 1}, {pid: 50, ppid: 1}, {pid: 60, ppid: 50}, {pid: 70, ppid: 1}, {pid: 71, ppid: 70}}
	subreapers := orphanReaperIn(rows, 60, false)
	for pid, want := range map[int]bool{1: true, 50: true, 60: false, 70: false, 71: false} {
		if subreapers(pid) != want {
			t.Errorf("with subreapers, reaper(%d)=%v, want %v", pid, !want, want)
		}
	}
	initOnly := orphanReaperIn(rows, 60, true)
	for pid, want := range map[int]bool{1: true, 50: false, 70: false} {
		if initOnly(pid) != want {
			t.Errorf("init only, reaper(%d)=%v, want %v", pid, !want, want)
		}
	}
}
