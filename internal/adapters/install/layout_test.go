package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func unpack(t *testing.T, l Layout, name string) {
	t.Helper()
	dir := l.ReleaseDir(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clawdline"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestCurrentNamesTheReleaseAndItsKind(t *testing.T) {
	l := NewLayout(t.TempDir(), t.TempDir())
	if _, k := l.CurrentRelease(); k != KindNone {
		t.Fatalf("empty layout: %s", k)
	}
	commit := strings.Repeat("ab", 20)
	unpack(t, l, "v0.10.0")
	unpack(t, l, commit)
	if err := l.SwitchCurrent("v0.10.0"); err != nil {
		t.Fatal(err)
	}
	if n, k := l.CurrentRelease(); n != "v0.10.0" || k != KindRelease {
		t.Fatalf("release: %s %s", n, k)
	}
	if err := l.SwitchCurrent(commit); err != nil {
		t.Fatal(err)
	}
	if n, k := l.CurrentRelease(); n != commit || k != KindSourceDeploy {
		t.Fatalf("source deploy: %s %s", n, k)
	}
	if err := l.SwitchCurrent("v0.11.0"); err == nil {
		t.Fatal("switched to a release that is not unpacked")
	}
	if err := l.SwitchCurrent("../elsewhere"); err == nil {
		t.Fatal("switched outside releases/")
	}
	if n, _ := l.CurrentRelease(); n != commit {
		t.Fatalf("a refused switch moved current: %s", n)
	}
}

func TestTheRunningBinarySaysWhichInstallationItBelongsTo(t *testing.T) {
	l := NewLayout(t.TempDir(), t.TempDir())
	unpack(t, l, "v0.10.0")
	commit := strings.Repeat("cd", 20)
	unpack(t, l, commit)
	if err := l.SwitchCurrent("v0.10.0"); err != nil {
		t.Fatal(err)
	}
	if k := l.KindOf(filepath.Join(l.Current, "clawdline")); k != KindRelease {
		t.Fatalf("through current: %s", k)
	}
	if k := l.KindOf(filepath.Join(l.ReleaseDir(commit), "clawdline")); k != KindSourceDeploy {
		t.Fatalf("a deployed commit: %s", k)
	}
	elsewhere := filepath.Join(t.TempDir(), "bin", "clawdline")
	if k := l.KindOf(elsewhere); k != KindSourceCheckout {
		t.Fatalf("a checkout's bin: %s", k)
	}
}

func TestStableServicePathIdentifiesTheActualBinaryAcrossSwitches(t *testing.T) {
	l := NewLayout(t.TempDir(), t.TempDir())
	unpack(t, l, "v0.10.0")
	unpack(t, l, "v0.11.0")
	for _, version := range []string{"v0.10.0", "v0.11.0"} {
		dist := filepath.Join(l.ReleaseDir(version), "dist")
		if err := os.MkdirAll(dist, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte(version), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.SwitchCurrent("v0.10.0"); err != nil {
		t.Fatal(err)
	}
	if err := l.InstallServiceBinary("v0.10.0"); err != nil {
		t.Fatal(err)
	}
	if got, _ := l.ServiceRelease(l.ServiceExecutable("v0.10.0")); got != "v0.10.0" {
		t.Fatalf("initial service: %s", got)
	}
	console := filepath.Join(filepath.Dir(l.ServiceBin), "dist")
	if target, err := os.Readlink(console); err != nil || target != "../current/dist" {
		t.Fatalf("service console link: %q, %v", target, err)
	}
	if body, err := os.ReadFile(filepath.Join(console, "index.html")); err != nil || string(body) != "v0.10.0" {
		t.Fatalf("initial service console: %q, %v", body, err)
	}
	if err := l.SwitchCurrent("v0.11.0"); err != nil {
		t.Fatal(err)
	}
	if got, _ := l.ServiceRelease(l.ServiceExecutable("v0.10.0")); got != "v0.10.0" {
		t.Fatalf("current switched before service binary: %s", got)
	}
	if err := l.InstallServiceBinary("v0.11.0"); err != nil {
		t.Fatal(err)
	}
	if got, _ := l.ServiceRelease(l.ServiceExecutable("v0.11.0")); got != "v0.11.0" {
		t.Fatalf("new service: %s", got)
	}
	if body, err := os.ReadFile(filepath.Join(console, "index.html")); err != nil || string(body) != "v0.11.0" {
		t.Fatalf("updated service console: %q, %v", body, err)
	}
}

func TestSameAppShellRequiresARealMatchingMarker(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	marker := filepath.Join("Contents", "Resources", "shell-revision")
	for _, dir := range []string{a, b} {
		if err := os.MkdirAll(filepath.Join(dir, "Contents", "Resources"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if SameAppShell(a, b) {
		t.Fatal("unmarked app was treated as unchanged")
	}
	for _, dir := range []string{a, b} {
		if err := os.WriteFile(filepath.Join(dir, marker), []byte(strings.Repeat("a", 64)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !SameAppShell(a, b) {
		t.Fatal("same native shell would replace the app")
	}
	if err := os.WriteFile(filepath.Join(b, marker), []byte(strings.Repeat("b", 64)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if SameAppShell(a, b) {
		t.Fatal("changed native shell would be skipped")
	}
}

func TestTheServiceFileRoundTripsAndRefusesAnUnknownSupervisor(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadServiceFile(dir); !os.IsNotExist(err) {
		t.Fatalf("absent: %v", err)
	}
	want := ServiceFile{Supervisor: "launchd", Name: "com.sainteye.clawdline-next", Domain: "gui/501", Port: 7727, Root: "/x"}
	if err := WriteServiceFile(dir, want); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadServiceFile(dir); err != nil || got != want {
		t.Fatalf("%+v %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, ServiceFileName), []byte(`{"supervisor":"cron"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadServiceFile(dir); err == nil {
		t.Fatal("an unknown supervisor was read")
	}
	if err := RemoveServiceFile(dir); err != nil {
		t.Fatal(err)
	}
	if err := RemoveServiceFile(dir); err != nil {
		t.Fatalf("removing twice: %v", err)
	}
}
