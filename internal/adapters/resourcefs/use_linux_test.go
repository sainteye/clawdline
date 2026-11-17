//go:build linux

package resourcefs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxProcessUseIncludesExecutableAndClosedDescriptorMappings(t *testing.T) {
	proc := t.TempDir()
	pid := filepath.Join(proc, "123")
	for _, dir := range []string{pid, filepath.Join(pid, "fd")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	managed := filepath.Join(t.TempDir(), "resource  with spaces")
	if err := os.Mkdir(managed, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(pid, "cwd")); err != nil {
		t.Fatal(err)
	}
	maps := filepath.Join(pid, "maps")
	if err := os.WriteFile(maps, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(pid, "exe")
	if err := os.Symlink(filepath.Join(managed, "server"), exe); err != nil {
		t.Fatal(err)
	}
	if got := useProcessPresence(context.Background(), managed, proc); got != "present" {
		t.Fatal("live executable was missed", got)
	}
	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	row := "01234000-01235000 r--p 00000000 08:01 42   " + filepath.Join(managed, "mapped file") + " (deleted)\n"
	if err := os.WriteFile(maps, []byte(row), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := useProcessPresence(context.Background(), managed, proc); got != "present" {
		t.Fatal("mapped file with closed FD was missed", got)
	}
	if err := os.WriteFile(maps, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := useProcessPresence(context.Background(), managed, proc); got != "absent" {
		t.Fatal("complete empty reading was not recognized", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := useProcessPresence(ctx, managed, proc); got != "unknown" {
		t.Fatal("cancelled reading authorized removal", got)
	}
}
