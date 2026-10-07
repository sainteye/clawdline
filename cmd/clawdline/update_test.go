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

// A failed check is one line in words with its code last, and a rollback
// says that nothing is left to do and that auto-update will not retry it.
func TestUpdateSaysFailuresOnceAndInWords(t *testing.T) {
	var b strings.Builder
	printUpdate(&b, contract.UpdateStatus{State: contract.UpdateStateUnknown, InstallKind: contract.UpdateInstallKindRelease,
		Error:  "manifest_signature_invalid: the signature does not match the manifest",
		Reason: "the release manifest could not be read: manifest_signature_invalid: the signature does not match the manifest"})
	out := b.String()
	if !strings.Contains(out, "state    unknown — the release could not be verified; nothing was changed. Try again later. (manifest_signature_invalid)\n") {
		t.Errorf("the state line:\n%s", out)
	}
	if n := strings.Count(out, "signature"); n != 1 {
		t.Errorf("the error is said %d times:\n%s", n, out)
	}

	b.Reset()
	printUpdate(&b, contract.UpdateStatus{State: contract.UpdateStateUpdateAvailable, InstallKind: contract.UpdateInstallKindRelease,
		Latest: contract.BuildStamp{Version: "v0.10.1"},
		Apply: &contract.UpdateApply{State: contract.UpdateApplyStateRolledBack, From: "v0.10.0", To: "v0.10.1",
			Error: &contract.UpdateApplyError{Code: "health_timeout", Detail: "dial tcp: connection refused"}}})
	out = b.String()
	for _, want := range []string{"v0.10.1 did not start, so Clawdline went back to v0.10.0. (health_timeout)",
		"automatic updates will not try v0.10.1 again"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "dial tcp") {
		t.Errorf("the Go error is shown:\n%s", out)
	}

	if got := rolledBackSentence("v0.10.1", "v0.10.0", "clawdline update --json"); got !=
		"v0.10.1 did not start, so Clawdline went back to v0.10.0, which is running now. Nothing else to do; automatic updates will skip this version. Details: clawdline update --json" {
		t.Errorf("rollback: %s", got)
	}
}
