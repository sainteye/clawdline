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
