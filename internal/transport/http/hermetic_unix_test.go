//go:build !windows

package http

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

// testHomeOwnerGone is true only when the kernel says no process has pid.
// A process owned by somebody else (EPERM) still counts as present.
func testHomeOwnerGone(pid int) bool {
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func TestAbandonedTestHomesAreRemovedOnlyWhenTheirOwnerIsGone(t *testing.T) {
	exited := exec.Command("true")
	if err := exited.Run(); err != nil {
		t.Fatal(err)
	}
	gone := exited.Process.Pid
	if !testHomeOwnerGone(gone) {
		t.Skip("the exited helper's PID was reused already")
	}
	parent := t.TempDir()
	dead := filepath.Join(parent, strconv.Itoa(gone)+"-1")
	live := filepath.Join(parent, strconv.Itoa(os.Getppid())+"-2")
	self := filepath.Join(parent, strconv.Itoa(os.Getpid())+"-3")
	foreign := filepath.Join(parent, "not-a-pid")
	for _, dir := range []string{dead, live, self, foreign} {
		if err := os.MkdirAll(filepath.Join(dir, ".config"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if n := sweepAbandonedTestHomes(parent, os.Getpid()); n != 1 {
		t.Fatalf("removed %d homes, want 1", n)
	}
	if _, err := os.Stat(dead); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("home of exited PID %d survived: %v", gone, err)
	}
	for _, kept := range []string{live, self, foreign} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("%s was removed: %v", kept, err)
		}
	}
}
