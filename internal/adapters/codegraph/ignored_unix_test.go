//go:build unix

package codegraph

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A shell script stands in for the program a hostile clone would name.
func TestIgnoreListingRunsNothingTheRepositoryNames(t *testing.T) {
	root := gitRepo(t)
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "monitor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "config", "core.fsmonitor", script).CombinedOutput(); err != nil {
		t.Fatalf("git config: %v %s", err, out)
	}
	write(t, root, "main.go", "package main\n")
	if _, err := gitIgnored(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the repository's core.fsmonitor program ran during an inventory check")
	}
}
