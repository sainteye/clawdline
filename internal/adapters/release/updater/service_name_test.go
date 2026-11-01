package updater

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/install"
)

func TestNamedDaemonBootstrapFailureCanRestoreTheLegacyService(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	l := f.env.Layout
	root := l.ReleaseDir("v0.11.0")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "clawdline"), []byte("new CLI"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, install.DaemonBinary), []byte("new"), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(f.env.StateDir, "service.plist")
	before := install.LaunchdPlist("com.example.test", install.ServiceSpec{Exec: l.LegacyServiceBin(), Port: 17890, Autostart: true})
	if err := os.WriteFile(file, []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	service := install.ServiceFile{Supervisor: "launchd", Name: "com.example.test", Domain: "gui/501", File: file}
	var calls []string
	bootstraps := 0
	f.env.Run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		line := strings.Join(append([]string{name}, args...), " ")
		calls = append(calls, line)
		if len(args) > 0 && args[0] == "bootstrap" {
			bootstraps++
			if bootstraps == 1 {
				return nil, errors.New("candidate bootstrap failed")
			}
		}
		if len(args) > 0 && args[0] == "bootout" && bootstraps == 1 {
			return nil, errors.New("job already unloaded")
		}
		return nil, nil
	}
	if err := l.SwitchCurrent("v0.11.0"); err != nil {
		t.Fatal(err)
	}
	if err := l.InstallServiceBinary("v0.11.0"); err != nil {
		t.Fatal(err)
	}
	if err := f.env.restartService(context.Background(), service); err == nil {
		t.Fatal("lost candidate failure")
	}
	changed, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(changed), l.ServiceBin) {
		t.Fatalf("candidate plist %s %v", changed, err)
	}
	if err := l.SwitchCurrent("v0.10.0"); err != nil {
		t.Fatal(err)
	}
	if err := l.InstallServiceBinary("v0.10.0"); err != nil {
		t.Fatal(err)
	}
	if err := f.env.restartService(context.Background(), service); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(file)
	if err != nil || string(restored) != before || bootstraps != 2 {
		t.Fatalf("rollback plist=%s err=%v calls=%v", restored, err, calls)
	}
}
