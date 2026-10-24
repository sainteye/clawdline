package updater

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
)

func cloudCandidate(t *testing.T) (*fixture, func() bool) {
	t.Helper()
	f := newFixture(t, "v2.0.0")
	f.publish("v2.0.1", commitOf('b'), daemonArchive(t, commitOf('b')), true, nil)
	if err := f.update(Request{}); err != nil {
		t.Fatal(err)
	}
	return f, f.env.CloudAdmission(filepath.Join(f.env.Layout.ReleaseDir("v2.0.1"), "clawdline"), Served{Version: "v2.0.1", Commit: commitOf('b')})
}

func TestCloudAdmissionFollowsRealFinish(t *testing.T) {
	for _, healthy := range []bool{true, false} {
		t.Run(map[bool]string{true: "healthy", false: "wrong_stamp_rollback"}[healthy], func(t *testing.T) {
			f, ready := cloudCandidate(t)
			if ready() {
				t.Fatal("pending admitted")
			}
			f.env.HealthWait = time.Millisecond
			f.env.Health = func(_ context.Context, _ int, _ string, want Served) error {
				if ready() {
					t.Fatal("admitted inside health callback")
				}
				if healthy || want.Version == "v2.0.0" {
					return nil
				}
				return errors.New("injected wrong BUILD stamp")
			}
			if err := f.env.Finish(context.Background()); err != nil {
				t.Fatal(err)
			}
			if ready() != healthy {
				t.Fatalf("ready=%v healthy=%v receipt=%+v", ready(), healthy, f.apply())
			}
			t.Logf("real Finish: healthy=%v current=%s Cloud admitted=%v", healthy, f.current(), ready())
		})
	}
}

func TestCloudAdmissionRequiresReceiptAfterPendingDisappears(t *testing.T) {
	f, ready := cloudCandidate(t)
	if ready() {
		t.Fatal("pending admitted")
	}
	if err := f.env.Layout.SwitchCurrent("v2.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := f.env.clearPending(); err != nil {
		t.Fatal(err)
	}
	if ready() {
		t.Fatal("clearPending/restarting gap admitted")
	}
	if err := os.Remove(f.env.statusPath()); err != nil {
		t.Fatal(err)
	}
	if ready() {
		t.Fatal("missing receipt admitted a witnessed update")
	}
	if err := f.env.record(contract.UpdateApplyStateHealthy, "v2.0.0", "v2.0.1", nil, ""); err != nil {
		t.Fatal(err)
	}
	if !ready() {
		t.Fatal("healthy receipt did not resume")
	}
}

func TestCloudAdmissionRejectsUnreadableAndBootGuardRollback(t *testing.T) {
	for _, fault := range []string{"pending", "pending_directory", "status", "status_directory", "deadline", "attempts", "rollback", "stamp"} {
		t.Run(fault, func(t *testing.T) {
			f, ready := cloudCandidate(t)
			p, _, _ := f.env.ReadPending()
			switch fault {
			case "pending":
				if err := os.WriteFile(f.env.pendingPath(), []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			case "status":
				if err := os.WriteFile(f.env.statusPath(), []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			case "pending_directory", "status_directory":
				path := f.env.pendingPath()
				if fault == "status_directory" {
					path = f.env.statusPath()
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "deadline":
				p.Deadline = f.clock.Add(-time.Second)
			case "attempts":
				p.Attempts = BootAttemptsLimit
			case "rollback":
				p.Phase = phaseRollback
				p.Reason = &contract.UpdateApplyError{Code: CodeHealthTimeout, Detail: "injected health failure"}
			case "stamp":
				p.ToCommit = commitOf('c')
			}
			if fault != "pending" && fault != "pending_directory" && fault != "status" && fault != "status_directory" {
				if err := f.env.writePending(p); err != nil {
					t.Fatal(err)
				}
			}
			if ready() {
				t.Fatal("fault admitted")
			}
			if fault == "pending" || fault == "pending_directory" {
				if err := f.env.Finish(context.Background()); err != nil {
					t.Fatal(err)
				}
				if ready() {
					t.Fatal("renamed unreadable pending admitted")
				}
			} else if fault == "status" || fault == "status_directory" {
				if err := f.env.clearPending(); err != nil {
					t.Fatal(err)
				}
				if ready() {
					t.Fatal("unreadable receipt admitted")
				}
			} else if fault == "stamp" {
				f.healthy = commitOf('c')
				if err := f.env.Finish(context.Background()); err != nil {
					t.Fatal(err)
				}
				if ready() {
					t.Fatal("receipt accepted a different installed stamp")
				}
			} else {
				exit, _, err := f.env.BootGuard(filepath.Join(f.env.Layout.ReleaseDir(p.To), "clawdline"))
				if err != nil || !exit {
					t.Fatalf("BootGuard: exit=%v %v", exit, err)
				}
				if ready() {
					t.Fatal("surviving candidate admitted after BootGuard rollback")
				}
			}
		})
	}
}

func TestCloudAdmissionNormalBootAndCapturedIdentity(t *testing.T) {
	f := newFixture(t, "v2.0.0")
	ready := f.env.CloudAdmission(f.exe, Served{Version: "v2.0.0", Commit: commitOf('a')})
	if !ready() {
		t.Fatal("ordinary release blocked")
	}
	if !f.env.CloudAdmission(filepath.Join(t.TempDir(), "source-binary"), Served{})() {
		t.Fatal("ordinary source blocked")
	}
	f.installRelease("v2.0.1", commitOf('b'))
	if err := f.env.Layout.SwitchCurrent("v2.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := f.env.record(contract.UpdateApplyStateHealthy, "v2.0.0", "v2.0.1", nil, ""); err != nil {
		t.Fatal(err)
	}
	if ready() {
		t.Fatal("current symlink reidentified surviving old executable")
	}
	if f.env.CloudAdmission(f.exe, Served{Version: "v2.0.0", Commit: commitOf('a')})() {
		t.Fatal("already replaced executable path hid the process build identity")
	}
	if f.env.CloudAdmission(f.env.Layout.ServiceBin, Served{Version: "v2.0.1", Commit: commitOf('b')})() {
		t.Fatal("unidentifiable service binary was treated as a source checkout")
	}
}

func TestCloudAdmissionSurvivingServiceBinaryCannotBecomeTheRollback(t *testing.T) {
	f := newFixture(t, "v2.0.0")
	f.installRelease("v2.0.1", commitOf('b'))
	if err := f.env.Layout.SwitchCurrent("v2.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := f.env.Layout.InstallServiceBinary("v2.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := f.env.record(contract.UpdateApplyStateHealthy, "v2.0.0", "v2.0.1", nil, ""); err != nil {
		t.Fatal(err)
	}
	running := Served{Version: "v2.0.1", Commit: commitOf('b')}
	ready := f.env.CloudAdmission(f.env.Layout.ServiceBin, running)
	if !ready() {
		t.Fatal("accepted service binary refused")
	}
	if err := f.env.Layout.SwitchCurrent("v2.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := f.env.Layout.InstallServiceBinary("v2.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := f.env.record(contract.UpdateApplyStateRolledBack, "v2.0.0", "v2.0.1", &contract.UpdateApplyError{Code: CodeBootGuard}, ""); err != nil {
		t.Fatal(err)
	}
	if ready() {
		t.Fatal("captured candidate became the rollback service")
	}
	if f.env.CloudAdmission(f.env.Layout.ServiceBin, running)() {
		t.Fatal("replacement before admission capture hid the candidate process")
	}
	if !f.env.CloudAdmission(f.env.Layout.ServiceBin, Served{Version: "v2.0.0", Commit: commitOf('a')})() {
		t.Fatal("genuine rolled-back binary cannot start Cloud")
	}
}
