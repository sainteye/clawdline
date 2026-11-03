package updater

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/install"
	"github.com/sainteye/clawdline/internal/contract"
)

func TestNamedDaemonBootstrapFailureCanRestoreTheLegacyService(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	l := f.env.Layout
	root := l.ReleaseDir("v0.11.0")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "clawdline"), []byte("new CLI"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, install.DaemonBinary), []byte("new"), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(f.env.StateDir, "service.plist")
	before := install.LaunchdPlist("com.example.test", install.ServiceSpec{Exec: l.LegacyServiceBin(), Port: 17890, Autostart: true})
	if err := os.WriteFile(file, []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	service := install.ServiceFile{Supervisor: "launchd", Name: "com.example.test", Domain: "gui/501", File: file}
	var calls []string
	bootstraps := 0
	candidateContext, cancelCandidate := context.WithCancel(context.Background())
	defer cancelCandidate()
	f.env.Run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		line := strings.Join(append([]string{name}, args...), " ")
		calls = append(calls, line)
		if len(args) > 0 && args[0] == "bootstrap" {
			bootstraps++
			if bootstraps == 1 {
				cancelCandidate()
				return nil, errors.New("candidate bootstrap failed")
			}
		}
		if len(args) > 0 && args[0] == "bootout" && bootstraps == 1 {
			return nil, errors.New("job already unloaded")
		}
		return nil, nil
	}
	if err := l.SwitchCurrent("v0.11.0"); err != nil {
		t.Fatal(err)
	}
	if err := l.InstallServiceBinary("v0.11.0"); err != nil {
		t.Fatal(err)
	}
	if err := f.env.restartService(candidateContext, service); err == nil {
		t.Fatal("lost candidate failure")
	}
	changed, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(changed), l.ServiceBin) {
		t.Fatalf("candidate plist %s %v", changed, err)
	}
	if err := l.SwitchCurrent("v0.10.0"); err != nil {
		t.Fatal(err)
	}
	if err := l.InstallServiceBinary("v0.10.0"); err != nil {
		t.Fatal(err)
	}
	if err := f.env.restartService(context.Background(), service); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(file)
	if err != nil || string(restored) != before || bootstraps != 2 {
		t.Fatalf("rollback plist=%s err=%v calls=%v", restored, err, calls)
	}
}

// Both releases ship the named daemon, but an older updater left launchd using
// the legacy CLI. A failed migration unloads the job without changing the
// rollback's executable name. The rollback must bootstrap it, not kickstart it.
func TestNamedDaemonRollbackReloadsAnUnloadedJobWithTheSameExecutable(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	l := f.env.Layout
	for _, v := range []string{"v0.10.0", "v0.11.0"} {
		f.installRelease(v, commitOf('a'))
		if err := os.WriteFile(filepath.Join(l.ReleaseDir(v), install.DaemonBinary), []byte(v), 0700); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(f.env.StateDir, "service.plist")
	if err := os.WriteFile(file, []byte(install.LaunchdPlist("com.example.test", install.ServiceSpec{Exec: l.ServiceBin, Port: 17890, Autostart: true})), 0600); err != nil {
		t.Fatal(err)
	}
	loaded := false
	f.env.Run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch args[0] {
		case "print", "kickstart":
			if !loaded {
				return nil, errors.New("service not found")
			}
		case "bootstrap":
			loaded = true
		}
		return nil, nil
	}
	svc := install.ServiceFile{Supervisor: "launchd", Name: "com.example.test", Domain: "gui/501", File: file}
	if err := f.env.restartService(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	if !loaded {
		t.Fatal("rollback did not reload the unloaded service")
	}
}

func TestNamedDaemonMigrationWaitsForLaunchdToReleaseTheOldJob(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	l := f.env.Layout
	if err := os.WriteFile(filepath.Join(l.ReleaseDir("v0.10.0"), install.DaemonBinary), []byte("daemon"), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(f.env.StateDir, "service.plist")
	if err := os.WriteFile(file, []byte(install.LaunchdPlist("com.example.test", install.ServiceSpec{Exec: l.LegacyServiceBin(), Port: 17890, Autostart: true})), 0600); err != nil {
		t.Fatal(err)
	}
	bootstraps := 0
	f.env.Run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "bootstrap" {
			bootstraps++
			if bootstraps == 1 {
				return nil, errors.New("Bootstrap failed: 5: Input/output error")
			}
		}
		return nil, nil
	}
	svc := install.ServiceFile{Supervisor: "launchd", Name: "com.example.test", Domain: "gui/501", File: file}
	if err := f.env.restartService(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	if bootstraps != 2 {
		t.Fatalf("bootstrap attempts = %d, want 2", bootstraps)
	}
}

func TestUnhealthyRollbackKeepsItsRecoveryPending(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	f.env.HealthWait = time.Millisecond
	f.installRelease("v0.11.0", commitOf('b'))
	p := Pending{From: "v0.10.0", To: "v0.11.0", FromCommit: commitOf('a'), ToCommit: commitOf('b'), Phase: phaseRollback, Reason: &contract.UpdateApplyError{Code: CodeSupervisorFailed, Detail: "candidate bootstrap failed"}}
	if err := f.env.writePending(p); err != nil {
		t.Fatal(err)
	}
	if err := f.env.Finish(context.Background()); err == nil {
		t.Fatal("unhealthy rollback was reported settled")
	}
	if _, ok, err := f.env.ReadPending(); err != nil || !ok {
		t.Fatalf("recovery lost: pending=%v err=%v", ok, err)
	}
	if got := f.apply(); got.State == contract.UpdateApplyStateRolledBack {
		t.Fatalf("unhealthy rollback reported success: %+v", got)
	}
	f.healthy = commitOf('a')
	if err := f.env.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.apply(); got.State != contract.UpdateApplyStateRolledBack {
		t.Fatalf("healthy rollback not settled: %+v", got)
	}
}

func TestBootstrapFailureStopsAtTheCallerDeadline(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	calls := 0
	f.env.Run = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		calls++
		return nil, errors.New("persistent launchd failure")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	err := f.env.bootstrapService(ctx, install.ServiceFile{Domain: "gui/501", File: "fixture.plist"})
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "persistent launchd failure") || calls == 0 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestExhaustedRollbackReportsFailureAndStopsRetrying(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	f.env.HealthWait = time.Millisecond
	f.installRelease("v0.11.0", commitOf('b'))
	p := Pending{From: "v0.10.0", To: "v0.11.0", FromCommit: commitOf('a'), ToCommit: commitOf('b'), Phase: phaseRollback, SupervisorRuns: 2 * SupervisorRunsLimit}
	if err := f.env.writePending(p); err != nil {
		t.Fatal(err)
	}
	if err := f.env.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := f.env.ReadPending(); err != nil || ok {
		t.Fatalf("exhausted recovery still pending=%v err=%v", ok, err)
	}
	if got := f.apply(); got.State != contract.UpdateApplyStateFailed || got.Error == nil {
		t.Fatalf("exhausted recovery: %+v", got)
	}
	if failed, err := f.env.HasFailed(p.To); err != nil || !failed {
		t.Fatalf("failed candidate not recorded: %v %v", failed, err)
	}
}

func TestBootGuardRollbackWinsOverLateCandidateHealth(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	f.installRelease("v0.11.0", commitOf('b'))
	p := Pending{From: "v0.10.0", To: "v0.11.0", FromCommit: commitOf('a'), ToCommit: commitOf('b'), Phase: phaseSwitch, Deadline: f.clock.Add(time.Hour)}
	if err := f.env.writePending(p); err != nil {
		t.Fatal(err)
	}
	f.env.Health = func(_ context.Context, _ int, _ string, want Served) error {
		if want.Commit == commitOf('b') {
			pending, _, _ := f.env.ReadPending()
			pending.Phase = phaseRollback
			pending.Reason = &contract.UpdateApplyError{Code: CodeBootGuard, Detail: "guard rejected candidate during health response"}
			if err := f.env.writePending(pending); err != nil {
				t.Fatal(err)
			}
		}
		return nil
	}
	if err := f.env.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.apply(); got.State != contract.UpdateApplyStateRolledBack || got.Error.Code != CodeBootGuard || f.current() != p.From {
		t.Fatalf("late health overrode recovery: %+v current=%s", got, f.current())
	}
}
