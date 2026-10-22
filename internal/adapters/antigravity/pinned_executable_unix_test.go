//go:build darwin || linux

package antigravity

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPinnedExecutableRejectsReplacementAndSameStampRewrites(t *testing.T) {
	for _, kind := range []string{"stable", "replacement", "rewrite", "mode", "removed", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(privateRuntimeRoot(t), "helper")
			if err := os.WriteFile(path, []byte("synthetic executable"), 0700); err != nil {
				t.Fatal(err)
			}
			pin, err := NewPinnedExecutable(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			original, err := pin.Fingerprint(context.Background())
			if err != nil || original.Digest == "" {
				t.Fatal(err)
			}
			info, _ := os.Stat(path)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "replacement":
				if err := os.Rename(path, path+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("synthetic executable"), 0700); err != nil {
					t.Fatal(err)
				}
			case "rewrite":
				if err := os.WriteFile(path, []byte("synthetic changedexe"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.Chmod(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "removed":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				cancel()
			}
			got, err := pin.Fingerprint(ctx)
			if kind == "stable" {
				if err != nil || got != original {
					t.Fatal(got, err)
				}
			} else if err == nil {
				t.Fatal("changed/cancelled image accepted", kind)
			}
		})
	}
}

func TestPinnedExecutableRefusesInvalidOrCancelledConstruction(t *testing.T) {
	root := privateRuntimeRoot(t)
	for _, kind := range []string{"relative", "missing", "directory", "nonexec", "symlink", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(root, kind)
			if err := os.WriteFile(path, []byte("synthetic"), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "relative":
				path = "relative"
			case "missing":
				path += ".missing"
			case "directory":
				path = root
			case "nonexec":
				if err := os.Chmod(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				link := path + ".link"
				if err := os.Symlink(path, link); err != nil {
					t.Fatal(err)
				}
				path = link
			case "cancelled":
				cancel()
			}
			if _, err := NewPinnedExecutable(ctx, path); err == nil {
				t.Fatal("invalid pin accepted")
			}
		})
	}
	var absent *PinnedExecutable
	if _, err := absent.Fingerprint(context.Background()); err == nil {
		t.Fatal("nil pin accepted")
	}
}
