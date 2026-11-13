package projectassistantacp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestProcessWriteStallIsBoundedUnknown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix owned process supervisor")
	}
	if inventoryUnavailable() {
		t.Skip("safe startup admission requires process inventory")
	}
	l := testLimits()
	l.FrameBytes = 4 << 20
	p, err := StartProcess(context.Background(), ProcessOptions{Configuration: peerConfiguration(t, "no-read"), Directory: t.TempDir(), Limits: l})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(context.Background())
	began := time.Now()
	err = p.Write(context.Background(), []byte(strings.Repeat("x", l.FrameBytes)))
	if pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatalf("partial stdin=%v", err)
	}
	if time.Since(began) > time.Second {
		t.Fatal("stdin write exceeded bound")
	}
}

func TestProcessStderrIsCapped(t *testing.T) {
	c, p := openTestClient(t, "stderr", func(context.Context, app.ProtocolEvent) error { return nil })
	_ = c
	deadline := time.Now().Add(time.Second)
	for len(p.Stderr()) < p.limits.StderrBytes && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(p.Stderr()) != p.limits.StderrBytes {
		t.Fatalf("stderr bytes=%d", len(p.Stderr()))
	}
}

func TestProcessCleansObservedSetsidDescendant(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix setsid")
	}
	if inventoryUnavailable() {
		t.Skip("sandbox cannot inventory owned descendants; native cleanup acceptance remains pending")
	}
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		t.Skip("python fixture unavailable")
	}
	file := t.TempDir() + "/owned-pid"
	config := peerConfiguration(t, "descendant")
	config.Environment = append(config.Environment, "CLAWDLINE_ACP_PID_FILE="+file)
	p, err := StartProcess(context.Background(), ProcessOptions{Configuration: config, Directory: t.TempDir(), Limits: testLimits()})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(context.Background())
	var pid int
	// /usr/bin/python3 is an Xcode shim; its first start on a cold CI runner
	// has exceeded three seconds. Readiness is not what this test measures.
	deadline := time.Now().Add(time.Second * 30)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(file)
		pid, _ = strconv.Atoi(string(b))
		if pid > 0 {
			break
		}
		time.Sleep(time.Millisecond * 10)
	}
	if pid == 0 {
		t.Fatal("setsid child not ready")
	}
	p.captureOwned()
	p.mu.Lock()
	birth, owned := p.owned[pid]
	p.mu.Unlock()
	if !owned {
		t.Fatal("escaped child not owned")
	}
	defer cleanupFixtureIdentity(t, p, pid, birth)
	if err = p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for fixtureIdentityAlive(t, p, pid, birth) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond * 10)
	}
	if fixtureIdentityAlive(t, p, pid, birth) {
		t.Fatalf("owned setsid child %d survived", pid)
	}
}

// The platform census is implemented by process_unix.go. Keeping the optional
// capability assertion here lets the same fixture source compile on Windows.
func fixtureCensus(p *Process) ([]processIdentity, error) {
	// Test assertions happen after Close has cancelled its cleanup context.
	// Use an independent bounded read-only census, never the expired budget.
	reader := &Process{limits: p.limits}
	if census, ok := any(reader).(interface {
		identities() ([]processIdentity, error)
	}); ok {
		return census.identities()
	}
	return nil, errors.New("platform process census unavailable")
}
func fixtureIdentityAlive(t *testing.T, p *Process, pid int, birth string) bool {
	t.Helper()
	rows, err := fixtureCensus(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.pid == pid && row.stamp == birth && !deadProcess(row) {
			return true
		}
	}
	return false
}
func cleanupFixtureIdentity(t *testing.T, p *Process, pid int, birth string) {
	t.Helper()
	handle, err := os.FindProcess(pid)
	if err != nil {
		t.Error(err)
		return
	}
	defer handle.Release()
	rows, err := fixtureCensus(p)
	if err != nil {
		t.Errorf("fixture ownership unknown, refusing blind cleanup: %v", err)
		return
	}
	for _, row := range rows {
		if row.pid == pid && row.stamp == birth && !deadProcess(row) {
			if err = handle.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Error(err)
			}
			return
		}
	}
}

func TestProcessLimitsMustBeExplicit(t *testing.T) {
	if err := (Limits{}).Validate(); pa.Code(err) != pa.Invalid {
		t.Fatal(err)
	}
}

func TestOwnedDescendantCaptureIsBoundedAndKeepsKnownOwnership(t *testing.T) {
	l := testLimits()
	l.OwnedDescendants = 2
	p := &Process{cmd: &exec.Cmd{Process: &os.Process{Pid: 10}}, limits: l, owned: map[int]string{}}
	rows := []processIdentity{{pid: 10, ppid: 1, stamp: "root"}, {pid: 11, ppid: 10, stamp: "child"}, {pid: 12, ppid: 11, stamp: "escaped"}}
	if err := p.captureRows(rows); pa.Code(err) != pa.CapacityFull {
		t.Fatal(err)
	}
	if len(p.owned) != 2 || p.owned[10] != "root" || p.owned[11] != "child" {
		t.Fatalf("lost known ownership: %+v", p.owned)
	}
	// Reuse/death cannot manufacture parentage for another process.
	if err := p.captureRows([]processIdentity{{pid: 11, ppid: 1, stamp: "reused"}}); err != nil {
		t.Fatal(err)
	}
	if len(p.owned) != 0 {
		t.Fatalf("reused identity retained: %+v", p.owned)
	}
}

func TestProcessCensusCaptureNeverAcceptsTruncation(t *testing.T) {
	capture := &censusCapture{budget: 4}
	if _, err := capture.Write([]byte("five!")); err == nil {
		t.Fatal("truncated census accepted")
	}
	if capture.buf.Len() != 0 {
		t.Fatal("partial census retained as complete")
	}
}

func TestProcessObservesFastExitBeforeWaitCanReap(t *testing.T) {
	if runtime.GOOS == "windows" || inventoryUnavailable() {
		t.Skip("requires Unix process inventory")
	}
	var seen *Process
	p, err := StartProcess(context.Background(), ProcessOptions{Configuration: peerConfiguration(t, "no-read"), Directory: t.TempDir(), Limits: testLimits(), beforeInitialCapture: func(child *Process) {
		seen = child
		if err := child.cmd.Process.Kill(); err != nil {
			t.Error(err)
			return
		}
		deadline := time.Now().Add(time.Second)
		for processAlive(child.cmd.Process.Pid) && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if processAlive(child.cmd.Process.Pid) {
			t.Error("controlled child did not exit before capture")
		}
		select {
		case <-child.done:
			t.Error("Wait reaped child before initial observation")
		default:
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	observed := p.rootObserved
	p.mu.Unlock()
	if p != seen || !observed {
		t.Fatal("startup returned without ownership receipt")
	}
	if err = p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.done:
	default:
		t.Fatal("fast-exited child not reaped after receipt")
	}
}

func TestProcessDeniedInitialInventoryRemainsUnknownAndReapsDirectChild(t *testing.T) {
	if runtime.GOOS == "windows" || !inventoryUnavailable() {
		t.Skip("requires an environment that denies process inventory")
	}
	var child *Process
	p, err := StartProcess(context.Background(), ProcessOptions{Configuration: peerConfiguration(t, "no-read"), Directory: t.TempDir(), Limits: testLimits(), beforeInitialCapture: func(p *Process) { child = p }})
	if p != nil || pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatalf("unverified startup admitted: %v %v", p, err)
	}
	if child == nil {
		t.Fatal("fixture did not start")
	}
	select {
	case <-child.done:
	default:
		t.Fatal("unverified direct child not reaped")
	}
}

func TestProcessRootPIDReuseNeverAdoptsForeignRootOrChildren(t *testing.T) {
	l := testLimits()
	l.OwnedDescendants = 8
	p := &Process{cmd: &exec.Cmd{Process: &os.Process{Pid: 10}}, limits: l, owned: map[int]string{}}
	first := []processIdentity{{pid: 10, ppid: 1, stamp: "first-root"}, {pid: 11, ppid: 10, stamp: "owned-child"}, {pid: 12, ppid: 11, stamp: "owned-setsid"}}
	if err := p.captureRows(first); err != nil {
		t.Fatal(err)
	}
	reused := []processIdentity{{pid: 10, ppid: 1, stamp: "foreign-root"}, {pid: 11, ppid: 1, stamp: "owned-child"}, {pid: 12, ppid: 11, stamp: "owned-setsid"}, {pid: 13, ppid: 10, stamp: "foreign-child"}}
	if err := p.captureRows(reused); err != nil {
		t.Fatal(err)
	}
	if p.rootStamp != "first-root" {
		t.Fatal("initial root identity changed")
	}
	if _, ok := p.owned[10]; ok {
		t.Fatal("reused foreign root adopted")
	}
	if _, ok := p.owned[13]; ok {
		t.Fatal("foreign child adopted through reused root")
	}
	if len(p.owned) != 2 || p.owned[11] != "owned-child" || p.owned[12] != "owned-setsid" {
		t.Fatalf("lost escaped owned descendants: %+v", p.owned)
	}
}

func TestProcessLiveGroupPermissionFailureNeverLooksStopped(t *testing.T) {
	permissionErr := &os.SyscallError{Syscall: "kill", Err: os.ErrPermission}
	live := []processIdentity{{pid: 10, pgid: 10, state: "Z", stamp: "root"}, {pid: 11, pgid: 10, state: "S", stamp: "child"}}
	err := confirmStoppedGroup(live, 10, permissionErr)
	if pa.Code(err) != pa.ExecutionEffectUnknown || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("live group signal failure hidden: %v", err)
	}
	var detail *ProcessSignalFailure
	if !errors.As(err, &detail) || detail.Phase != "signal_group_live" {
		t.Fatalf("missing phase: %v", err)
	}
	zombies := []processIdentity{{pid: 10, pgid: 10, state: "Z", stamp: "root"}, {pid: 11, pgid: 10, state: "Z+", stamp: "child"}}
	if err := confirmStoppedGroup(zombies, 10, permissionErr); err != nil {
		t.Fatalf("proven stopped group rejected: %v", err)
	}
	unknown := []processIdentity{{pid: 10, pgid: 10, state: "", stamp: "unknown"}}
	if err := confirmStoppedGroup(unknown, 10, permissionErr); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatal("unknown state treated as stopped")
	}
}

func TestProcessOwnedUnreapedZombieGroupIsAlreadyStopped(t *testing.T) {
	if runtime.GOOS == "windows" || inventoryUnavailable() {
		t.Skip("requires Unix process groups and inventory")
	}
	config := peerConfiguration(t, "no-read")
	cmd := exec.Command(config.Executable, config.Arguments...)
	cmd.Env = config.Environment
	cmd.Dir = t.TempDir()
	if err := configureProcess(cmd); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Wait is intentionally last: the direct handle cannot be PID-reused
	// while its child remains unreaped, including during deferred cleanup.
	defer cmd.Wait()
	defer cmd.Process.Kill()
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	p := &Process{cmd: cmd, limits: testLimits(), owned: map[int]string{}}
	deadline := time.Now().Add(time.Second)
	var zombie bool
	for time.Now().Before(deadline) {
		rows, err := fixtureCensus(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.pid == cmd.Process.Pid && deadProcess(row) {
				zombie = true
			}
		}
		if zombie {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !zombie {
		t.Fatal("owned root did not become an unreaped zombie")
	}
	p.captureOwned()
	if err := p.signalOwned(true); err != nil {
		t.Fatalf("already stopped owned group: %v", err)
	}
}

func TestProcessCompletedReceiptWinsWhenDeadlineAlsoReady(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	close(done)
	for i := 0; i < 100; i++ {
		if !waitOwnedDone(ctx, done) {
			t.Fatal("reaped receipt lost to ready deadline")
		}
	}
}

func TestProcessCleanupUsesOneRemainingBudgetAndFallsBackToOwnedHandle(t *testing.T) {
	if runtime.GOOS == "windows" || inventoryUnavailable() {
		t.Skip("requires Unix ownership census")
	}
	p, err := StartProcess(context.Background(), ProcessOptions{Configuration: peerConfiguration(t, "no-read"), Directory: t.TempDir(), Limits: testLimits()})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(context.Background())
	p.mu.Lock()
	p.censusOverride = func(ctx context.Context) ([]processIdentity, error) { <-ctx.Done(); return nil, ctx.Err() }
	p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*100)
	defer cancel()
	began := time.Now()
	err = p.Close(ctx)
	elapsed := time.Since(began)
	if pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatalf("unknown inventory hidden: %v", err)
	}
	if elapsed >= p.limits.CleanupTimeout {
		t.Fatalf("per-operation budgets exceeded remaining deadline: %s", elapsed)
	}
	select {
	case <-p.done:
	case <-time.After(p.limits.CleanupTimeout):
		t.Fatal("fallback failed to reap direct owned process")
	}
}
