package install

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNamedDaemonInstallAndLegacyRollbackUseTheirOwnExecutable(t *testing.T) {
	l := NewLayout(t.TempDir(), t.TempDir())
	unpack(t, l, "v2.0.0")
	unpack(t, l, "v2.0.1")
	daemon := filepath.Join(l.ReleaseDir("v2.0.1"), DaemonBinary)
	if err := os.WriteFile(daemon, []byte("new daemon"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"v2.0.1", "v2.0.0", "v2.0.1"} {
		if err := l.SwitchCurrent(version); err != nil {
			t.Fatal(err)
		}
		if err := l.InstallServiceBinary(version); err != nil {
			t.Fatal(err)
		}
		path := l.ServiceExecutable(version)
		if got, _ := l.ServiceRelease(path); got != version {
			t.Fatalf("%s: %s", version, got)
		}
		if version == "v2.0.1" && filepath.Base(path) != DaemonBinary {
			t.Fatal("new install uses CLI name")
		}
		if version == "v2.0.0" && path != l.LegacyServiceBin() {
			t.Fatal("legacy rollback changed identity")
		}
	}
}

func TestNamedDaemonWithCopySupportIsIndependentBeforeLaunch(t *testing.T) {
	l := NewLayout(t.TempDir(), t.TempDir())
	for _, version := range []string{"v2.0.0", "v2.0.1"} {
		unpack(t, l, version)
		if err := os.WriteFile(filepath.Join(l.ReleaseDir(version), DaemonBinary), []byte(version), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(l.ReleaseDir("v2.0.1"), daemonCopySupport), nil, 0644); err != nil {
		t.Fatal(err)
	}

	for _, selection := range []struct {
		version string
		same    bool
	}{
		{"v2.0.1", false},
		{"v2.0.0", true},
		{"v2.0.1", false},
	} {
		if err := l.SwitchCurrent(selection.version); err != nil {
			t.Fatal(err)
		}
		if err := l.InstallServiceBinary(selection.version); err != nil {
			t.Fatal(err)
		}
		service, err := os.Stat(l.ServiceBin)
		if err != nil {
			t.Fatal(err)
		}
		packaged, err := os.Stat(filepath.Join(l.ReleaseDir(selection.version), DaemonBinary))
		if err != nil {
			t.Fatal(err)
		}
		if same := os.SameFile(service, packaged); same != selection.same {
			t.Fatalf("%s: service shares packaged inode = %v, want %v", selection.version, same, selection.same)
		}
		if service.Mode().Perm() != packaged.Mode().Perm() {
			t.Fatalf("%s: service permissions = %v, want %v", selection.version, service.Mode(), packaged.Mode())
		}
		if name, kind := l.ServiceRelease(l.ServiceBin); name != selection.version || kind != KindRelease {
			t.Fatalf("running release = %q (%s), want %s", name, kind, selection.version)
		}
	}
}

func TestServiceExecutableMigrationPreservesTheRestOfThePlist(t *testing.T) {
	old := "/example/service/clawdline"
	named := "/example/service/clawdline-daemon"
	before := []byte(LaunchdPlist("com.example.test", ServiceSpec{Exec: old, Port: 17890, Autostart: true, Home: "/example/home"}))
	after, changed, err := RenameLaunchdExecutable(before, []string{old}, named)
	if err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	if !bytes.Equal(after, []byte(strings.Replace(string(before), old, named, 1))) {
		t.Fatal("migration changed other service settings")
	}
	rollback, changed, err := RenameLaunchdExecutable(after, []string{named}, old)
	if err != nil || !changed || !bytes.Equal(rollback, before) {
		t.Fatalf("rollback: %v %v", changed, err)
	}
	if _, _, err := RenameLaunchdExecutable(before, []string{"/unrelated"}, named); err == nil {
		t.Fatal("overwrote unknown executable")
	}
}
