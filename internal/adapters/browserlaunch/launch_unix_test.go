//go:build darwin || linux

package browserlaunch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A shell script stands in for Chromium, so no browser starts: it records the
// arguments it was given and exits.
func TestLaunchStartsChromiumOnClawdlinesOwnProfile(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")
	fake := filepath.Join(t.TempDir(), "chromium")
	script := "#!/bin/sh\nfor a in \"$@\"; do echo \"$a\"; done > '" + argsFile + ".tmp' && mv '" + argsFile + ".tmp' '" + argsFile + "'\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	stable := filepath.Join(dir, "bin", "clawdline")
	if err := os.MkdirAll(filepath.Dir(stable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	started, err := Launch(Options{Dir: dir, Port: 7791, Browser: fake})
	if err != nil {
		t.Fatal(err)
	}
	if started.PID <= 0 || started.Browser != fake || started.Profile != ProfileDir(dir) || !ValidExtensionID(started.ExtensionID) {
		t.Fatalf("started %+v", started)
	}
	var got []byte
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if got, err = os.ReadFile(argsFile); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("the stand-in Chromium never ran: %v", err)
	}
	ext, _ := filepath.EvalSymlinks(ExtensionDir(dir))
	for _, want := range []string{"--user-data-dir=" + ProfileDir(dir), "--load-extension=" + ext, "--disable-extensions-except=" + ext, "--use-mock-keychain", "--disable-backgrounding-occluded-windows", "--disable-renderer-backgrounding", "--disable-background-timer-throttling", "about:blank"} {
		if !strings.Contains(string(got), want+"\n") {
			t.Errorf("argument %q missing from:\n%s", want, got)
		}
	}
	if UnpackedExtensionID(ext) != started.ExtensionID {
		t.Errorf("id %s is not the extension directory's", started.ExtensionID)
	}
	launcher, _ := os.ReadFile(filepath.Join(dir, "browser-host.sh"))
	if !strings.Contains(string(launcher), "CLAWDLINE_NEXT_PORT=7791 exec '"+stable+"' browser-host") {
		t.Errorf("launcher %q does not run the stable binary", launcher)
	}
	if _, err := os.Stat(filepath.Join(ProfileDir(dir), "NativeMessagingHosts", HostName+".json")); err != nil {
		t.Errorf("host manifest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ExtensionDir(dir), "manifest.json")); err != nil {
		t.Errorf("extension: %v", err)
	}
	if Running(dir) {
		t.Error("a profile no Chromium holds reads as running")
	}
	if err := os.Symlink("host-1", filepath.Join(ProfileDir(dir), "SingletonLock")); err != nil {
		t.Fatal(err)
	}
	if !Running(dir) {
		t.Error("a held profile reads as not running")
	}
}

func TestAChromiumThatCannotStartIsAnError(t *testing.T) {
	if _, err := Launch(Options{Dir: t.TempDir(), Port: 7791, Browser: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("a missing binary started")
	}
}

func TestEachHostLauncherExecutesWithItsOwnModeAndProfile(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(t.TempDir(), "fake clawdline")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nprintf '%s\\n' \"$CLAWDLINE_BROWSER_MODE\" \"$CLAWDLINE_BROWSER_PROFILE\" \"$CLAWDLINE_NEXT_DIR\" \"$CLAWDLINE_NEXT_PORT\" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"isolated", "user_chrome"} {
		profile := filepath.Join(t.TempDir(), "person's Chrome")
		host, err := InstallHostForMode(dir, 7791, exe, strings.Repeat("a", 32), profile, mode)
		if err != nil {
			t.Fatal(err)
		}
		got, err := exec.Command(host.Launcher).Output()
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		want := strings.Join([]string{mode, profile, dir, "7791", "browser-host", ""}, "\n")
		if string(got) != want {
			t.Fatalf("%s host environment: %q, want %q", mode, got, want)
		}
	}
}
