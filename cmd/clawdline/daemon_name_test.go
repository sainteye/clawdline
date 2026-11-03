package main

import (
	"github.com/sainteye/clawdline/internal/adapters/install"
	"os"
	"path/filepath"
	"testing"
)

func TestServeFindsTheNamedDaemonAcrossAnOlderUpdater(t *testing.T) {
	l := install.NewLayout(t.TempDir(), t.TempDir())
	for _, version := range []string{"v2.0.0", "v2.0.1"} {
		root := l.ReleaseDir(version)
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		for _, binary := range []string{"clawdline", install.DaemonBinary} {
			if err := os.WriteFile(filepath.Join(root, binary), []byte(version+binary), 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := l.SwitchCurrent("v2.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(l.LegacyServiceBin()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(l.ReleaseDir("v2.0.0"), "clawdline"), l.LegacyServiceBin()); err != nil {
		t.Fatal(err)
	}
	// The loaded legacy launch command can still be the previous CLI while
	// current already names the target. It must not put the daemon back.
	if err := l.SwitchCurrent("v2.0.1"); err != nil {
		t.Fatal(err)
	}
	path, err := namedDaemonPath(l.LegacyServiceBin(), l)
	if err != nil || path != l.ServiceBin {
		t.Fatalf("target=%q err=%v", path, err)
	}
	if version, _ := l.ServiceRelease(path); version != "v2.0.1" {
		t.Fatalf("started %s", version)
	}
	if path, err = namedDaemonPath(l.ServiceBin, l); err != nil || path != "" {
		t.Fatalf("daemon recursively delegates: %q %v", path, err)
	}
}

func TestServeKeepsLegacyAndSourceOnlyLaunchesWorking(t *testing.T) {
	l := install.NewLayout(t.TempDir(), t.TempDir())
	root := l.ReleaseDir("v1.0.0")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(root, "clawdline")
	if err := os.WriteFile(cli, []byte("old"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := l.SwitchCurrent("v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := l.InstallServiceBinary("v1.0.0"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{cli, l.LegacyServiceBin(), filepath.Join(t.TempDir(), "clawdline")} {
		if target, err := namedDaemonPath(path, l); err != nil || target != "" {
			t.Fatalf("%s: %s %v", path, target, err)
		}
	}
}
