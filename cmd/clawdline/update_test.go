package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
)

func TestUpdateExitCodes(t *testing.T) {
	for state, want := range map[contract.UpdateState]int{
		contract.UpdateStateCurrent:         0,
		contract.UpdateStateAhead:           0,
		contract.UpdateStateUpdateAvailable: 10,
		contract.UpdateStateDiffers:         10,
		contract.UpdateStateUnknown:         3,
	} {
		if got := updateExit(state); got != want {
			t.Errorf("%s: exit %d, want %d", state, got, want)
		}
	}
}

type recordRunner struct{ calls []string }

func (r *recordRunner) run(dir string, stdout, _ io.Writer, name string, args ...string) error {
	r.calls = append(r.calls, strings.Join(append([]string{name}, args...), " "))
	if name == "git" && len(args) > 0 && args[0] == "rev-parse" {
		// The test's own module root has tools/deploy-linux-user.sh.
		stdout.Write([]byte("../..\n"))
	}
	return nil
}

func TestApplyRefusesWhenNothingIsNewer(t *testing.T) {
	r := &recordRunner{}
	var out, errb bytes.Buffer
	st := contract.UpdateStatus{State: contract.UpdateStateCurrent, Latest: contract.BuildStamp{Stamp: "4b7c3f8d"}}
	if code := applyUpdate(&out, &errb, st, false, "linux", ".", r); code == 0 || len(r.calls) != 0 {
		t.Fatalf("exit %d, ran %v", code, r.calls)
	}
}

func TestApplyOnLinuxFetchesChecksAncestryAndDeploysTheStamp(t *testing.T) {
	r := &recordRunner{}
	var out, errb bytes.Buffer
	st := contract.UpdateStatus{State: contract.UpdateStateUpdateAvailable, Latest: contract.BuildStamp{Stamp: "4b7c3f8d"}}
	if code := applyUpdate(&out, &errb, st, false, "linux", ".", r); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	got := strings.Join(r.calls, "\n")
	for _, want := range []string{"git fetch origin main", "git merge-base --is-ancestor 4b7c3f8d origin/main", "deploy-linux-user.sh --rev 4b7c3f8d"} {
		if !strings.Contains(got, want) {
			t.Errorf("did not run %q; ran:\n%s", want, got)
		}
	}
}

func TestApplyOnMacOSPrintsTheCommandsAndFails(t *testing.T) {
	r := &recordRunner{}
	var out, errb bytes.Buffer
	st := contract.UpdateStatus{State: contract.UpdateStateUpdateAvailable, Latest: contract.BuildStamp{Stamp: "4b7c3f8d"}}
	if code := applyUpdate(&out, &errb, st, false, "darwin", ".", r); code == 0 || len(r.calls) != 0 {
		t.Fatalf("exit %d, ran %v", code, r.calls)
	}
	if !strings.Contains(out.String(), "tools/package-macos.sh") || !strings.Contains(out.String(), "4b7c3f8d") {
		t.Fatalf("%s", out.String())
	}
}
