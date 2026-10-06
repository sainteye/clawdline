//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// install.sh --uninstall --purge after an uninstall took the binary still
// removes the state directory and says so; without --purge it leaves it.
func TestInstallScriptPurgesStateWithNoBinaryLeft(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, ".config", "clawdline-next")
	if err := os.MkdirAll(filepath.Join(state, "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(state, "local-token"), []byte("t"), 0o600)
	run := func(args ...string) string {
		cmd := exec.Command("sh", append([]string{"../../install.sh"}, args...)...)
		cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("install.sh %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	out := run("--uninstall")
	if _, err := os.Stat(state); err != nil {
		t.Fatalf("--uninstall without --purge removed the state directory:\n%s", out)
	}
	if !strings.Contains(out, "Clawdline is not installed at ") {
		t.Errorf("--uninstall with nothing installed:\n%s", out)
	}

	out = run("--uninstall", "--purge")
	if _, err := os.Stat(state); err == nil {
		t.Fatalf("--uninstall --purge with no binary kept %s:\n%s", state, out)
	}
	if !strings.Contains(out, "removed "+state+" (--purge)") {
		t.Errorf("does not say what it removed:\n%s", out)
	}

	out = run("--uninstall", "--purge")
	if !strings.Contains(out, "nothing to purge") {
		t.Errorf("a second purge:\n%s", out)
	}
}
