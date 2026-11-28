package resourcefs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func TestSharedGoTrimKeepsRecentEntriesUnknownDataAndOwnership(t *testing.T) {
	root, r := ownedResource(t)
	r.Owner.Kind, r.Class = "shared_go", taskresource.Cache
	path, err := root.GoCachePath(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(path, "ab"), 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(path, "ab", fmt.Sprintf("%064x-d", 1))
	recent := filepath.Join(path, "ab", fmt.Sprintf("%064x-d", 2))
	unknown := filepath.Join(path, "keep-unknown")
	for _, p := range []string{old, recent, unknown} {
		if err := os.WriteFile(p, []byte("cache bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	then := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, then, then); err != nil {
		t.Fatal(err)
	}
	before, err := root.Snapshot(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(old)
	if err != nil {
		t.Fatal(err)
	}
	a, known := allocatedBytes(info)
	budget := before.Logical - info.Size()
	if known {
		budget = before.Allocated - a
	}
	removed, err := root.TrimGoCache(context.Background(), r, before.Digest, budget)
	if err != nil || removed.Logical != info.Size() {
		t.Fatal(removed, err)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old entry retained", err)
	}
	for _, p := range []string{recent, unknown, filepath.Join(root.Path(), r.ID, ".owner")} {
		if _, err := os.Stat(p); err != nil {
			t.Fatal("protected or recent bytes removed", p, err)
		}
	}
	if _, err := root.TrimGoCache(context.Background(), r, before.Digest, 0); !errors.Is(err, ErrChanged) {
		t.Fatal("changed plan accepted", err)
	}
}

func TestGoTrimRefusesTaskCachesAndEscapedEntries(t *testing.T) {
	root, r := ownedResource(t)
	if _, err := root.GoCachePath(r); !errors.Is(err, ErrUnsafe) {
		t.Fatal(err)
	}
	r.Owner.Kind, r.Class = "shared_go", taskresource.Cache
	path, err := root.GoCachePath(r)
	if err != nil {
		t.Fatal(err)
	}
	foreign := t.TempDir()
	if err := os.Symlink(foreign, filepath.Join(path, "ab")); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Snapshot(context.Background(), r); !errors.Is(err, ErrUnsafe) {
		t.Fatal("linked cache accepted", err)
	}
}

func TestSharedGoTrimCanReclaimReadOnlyModulesUnderPressure(t *testing.T) {
	root, r := ownedResource(t)
	r.Owner.Kind, r.Class = "shared_go", taskresource.Cache
	build, err := root.GoCachePath(r)
	if err != nil {
		t.Fatal(err)
	}
	mod := filepath.Join(filepath.Dir(build), "go-mod")
	version := filepath.Join(mod, "example.invalid", "module@v1.0.0")
	if err := os.MkdirAll(version, 0o700); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(mod, "keep-unknown")
	for _, path := range []string{filepath.Join(version, "go.mod"), unknown} {
		if err := os.WriteFile(path, []byte("module fixture"), 0o400); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(version, 0o500); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	before, err := root.Snapshot(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := root.TrimGoCache(ctx, r, before.Digest, 0)
	if err != nil || removed.Logical != int64(len("module fixture")) {
		t.Fatal(removed, err)
	}
	if _, err := os.Stat(version); !os.IsNotExist(err) {
		t.Fatal("read-only module was retained", err)
	}
	if raw, err := os.ReadFile(unknown); err != nil || string(raw) != "module fixture" {
		t.Fatal("unknown file was changed", err)
	}
	if _, err := root.GoCachePath(r); err != nil {
		t.Fatal("shared cache could not be reused", err)
	}
}
