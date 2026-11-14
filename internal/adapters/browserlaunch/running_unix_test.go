//go:build !windows

package browserlaunch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunningPIDReadsTheProfileLock(t *testing.T) {
	dir := t.TempDir()
	if RunningPID(dir) != 0 {
		t.Fatal("no profile, yet a pid")
	}
	if err := os.MkdirAll(ProfileDir(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	for target, want := range map[string]int{"my-host.local-4242": 4242, "host-": 0, "4242": 0, "host-12a": 0} {
		lock := filepath.Join(ProfileDir(dir), "SingletonLock")
		_ = os.Remove(lock)
		if err := os.Symlink(target, lock); err != nil {
			t.Fatal(err)
		}
		if got := RunningPID(dir); got != want {
			t.Errorf("%q: pid %d, want %d", target, got, want)
		}
	}
}
