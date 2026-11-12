package browserlaunch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Chromium names an unpacked extension by the SHA-256 of its path, each hex
// digit spelled a-p; TestBrowserBridgeEndToEnd shows Chromium accepting the
// host manifest written with this id. The expected values are that rule
// worked by hand for two short paths.
func TestUnpackedExtensionIDSpellsTheHashInAToP(t *testing.T) {
	for path, want := range map[string]string{
		"/tmp/ext":              "lcfjooiecahccmjaipimfaidcnaihadb",
		"/opt/clawdline/chrome": "",
	} {
		got := UnpackedExtensionID(path)
		if want != "" && got != want {
			t.Errorf("%s: id = %s, want %s", path, got, want)
		}
		if len(got) != 32 || strings.Trim(got, "abcdefghijklmnop") != "" {
			t.Errorf("%s: id %q is not 32 letters a-p", path, got)
		}
	}
}

func TestTheEmbeddedExtensionIsWrittenWhole(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "chrome-extension")
	if err := WriteExtension(dir); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"manifest.json", "worker.js", "page.js", "popup.html", "popup.js", "_locales/en/messages.json", "_locales/zh_TW/messages.json", "icons/icon-128.png"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}

func TestNoChromiumIsRefusedByName(t *testing.T) {
	_, err := Launch(Options{Dir: t.TempDir(), Port: 7727})
	if !errors.Is(err, ErrChromiumMissing) || !strings.Contains(err.Error(), InstallCommand) {
		t.Fatalf("err = %v", err)
	}
}

func TestAChromiumNamedWinsOverTheEnvironment(t *testing.T) {
	env := func(k string) string {
		if k == "CLAWDLINE_CHROMIUM" {
			return "/from/env"
		}
		return ""
	}
	if got := FindChromium("/named", env); got != "/named" {
		t.Fatalf("named: %s", got)
	}
	if got := FindChromium("", env); got != "/from/env" {
		t.Fatalf("env: %s", got)
	}
}

func TestTheNewestInstalledBrowserIsFoundInBothPlaywrightLayouts(t *testing.T) {
	for _, fixture := range []struct{ platform, old, current string }{
		{"darwin", "chrome-mac/Chromium.app/Contents/MacOS/Chromium", "chrome-mac-arm64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing"},
		{"darwin", "chrome-mac/Chromium.app/Contents/MacOS/Chromium", "chrome-mac-x64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing"},
		{"linux", "chrome-linux/chrome", "chrome-linux64/chrome"},
	} {
		t.Run(fixture.platform+fixture.current, func(t *testing.T) {
			cache := t.TempDir()
			write := func(revision, name string) string {
				file := filepath.Join(cache, "chromium-"+revision, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte("browser"), 0o755); err != nil {
					t.Fatal(err)
				}
				return file
			}
			old := write("999", fixture.old)
			if got := playwrightChromium(cache, fixture.platform); got != old {
				t.Fatalf("existing layout: %s, want %s", got, old)
			}
			current := write("1254", fixture.current)
			if got := playwrightChromium(cache, fixture.platform); got != current {
				t.Fatalf("newest download: %s, want %s", got, current)
			}
		})
	}
}

// The daemon runs from a versioned release directory an update replaces; the
// launcher Chrome keeps must name the stable binary when there is one.
func TestTheLauncherNamesTheStableBinaryWhenItExists(t *testing.T) {
	dir := t.TempDir()
	self, _ := os.Executable()
	if got, err := LauncherBinary(dir); err != nil || got != self {
		t.Fatalf("without bin/: %s %v, want %s", got, err, self)
	}
	stable := filepath.Join(dir, "bin", "clawdline")
	if err := os.MkdirAll(filepath.Dir(stable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := LauncherBinary(dir); err != nil || got != stable {
		t.Fatalf("with bin/: %s %v, want %s", got, err, stable)
	}
}

func TestTheHostManifestAllowsOnlyTheExtension(t *testing.T) {
	dir, chrome := t.TempDir(), t.TempDir()
	id := UnpackedExtensionID("/tmp/ext")
	host, err := InstallHost(dir, 7790, "/opt/it's/clawdline", id, chrome)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Name    string   `json:"name"`
		Path    string   `json:"path"`
		Allowed []string `json:"allowed_origins"`
	}
	data, _ := os.ReadFile(host.Manifest)
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != HostName || m.Path != host.Launcher || len(m.Allowed) != 1 || m.Allowed[0] != "chrome-extension://"+id+"/" {
		t.Fatalf("manifest %s", data)
	}
	script, _ := os.ReadFile(host.Launcher)
	want := "CLAWDLINE_NEXT_PORT=7790 exec '/opt/it'\\''s/clawdline' browser-host\n"
	if !strings.HasSuffix(string(script), want) || !strings.Contains(string(script), "CLAWDLINE_NEXT_DIR='"+dir+"'") {
		t.Fatalf("launcher %q", script)
	}
	if _, err := InstallHost(dir, 7790, "/x", "not-an-id", chrome); err == nil {
		t.Fatal("a bad id was accepted")
	}
	if _, err := InstallHost(dir, 7790, "/x", id, "relative"); err == nil {
		t.Fatal("a relative Chrome directory was accepted")
	}
}

func TestHostModesKeepLaunchersAndOriginAllowlistsSeparate(t *testing.T) {
	dir := t.TempDir()
	isolated := filepath.Join(t.TempDir(), "isolated")
	ordinary := filepath.Join(t.TempDir(), "ordinary")
	a, err := InstallHost(dir, 7791, "/opt/clawdline", strings.Repeat("a", 32), isolated)
	if err != nil {
		t.Fatal(err)
	}
	firstScript, _ := os.ReadFile(a.Launcher)
	firstManifest, _ := os.ReadFile(a.Manifest)
	b, err := InstallHostForMode(dir, 7791, "/opt/clawdline", strings.Repeat("b", 32), ordinary, "user_chrome")
	if err != nil {
		t.Fatal(err)
	}
	if a.Launcher == b.Launcher || filepath.Base(a.Launcher) != "browser-host.sh" || filepath.Base(b.Launcher) != "browser-host-user-chrome.sh" {
		t.Fatalf("launchers: %+v %+v", a, b)
	}
	afterScript, _ := os.ReadFile(a.Launcher)
	afterManifest, _ := os.ReadFile(a.Manifest)
	if string(firstScript) != string(afterScript) || string(firstManifest) != string(afterManifest) {
		t.Fatal("user Chrome installation changed the isolated host")
	}
	var manifest struct {
		Path    string   `json:"path"`
		Allowed []string `json:"allowed_origins"`
	}
	raw, _ := os.ReadFile(b.Manifest)
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Path != b.Launcher || len(manifest.Allowed) != 1 || manifest.Allowed[0] != "chrome-extension://"+strings.Repeat("b", 32)+"/" {
		t.Fatalf("user manifest: %s", raw)
	}
	if _, err := InstallHostForMode(dir, 7791, "/opt/clawdline", strings.Repeat("b", 32), isolated, "user_chrome"); err == nil {
		t.Fatal("one mode overwrote the other mode's profile registration")
	}
	afterScript, _ = os.ReadFile(a.Launcher)
	afterManifest, _ = os.ReadFile(a.Manifest)
	if string(firstScript) != string(afterScript) || string(firstManifest) != string(afterManifest) {
		t.Fatal("refused cross-mode installation changed existing files")
	}
}

func TestInvalidHostModeHasNoFilesystemSideEffects(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "state")
	profile := filepath.Join(root, "profile")
	for _, mode := range []string{"native", "", "other"} {
		if _, err := InstallHostForMode(dir, 7791, "/opt/clawdline", strings.Repeat("a", 32), profile, mode); err == nil {
			t.Fatalf("accepted mode %q", mode)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid mode wrote files: %v %v", entries, err)
	}
}

func TestUserChromeDirectoryUsesOnlyTheNamedPlatformDefaults(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	local := filepath.Join(t.TempDir(), "local")
	xdg := filepath.Join(t.TempDir(), "xdg")
	for _, tc := range []struct {
		platform string
		env      map[string]string
		want     string
	}{
		{"darwin", nil, filepath.Join(home, "Library", "Application Support", "Google", "Chrome")},
		{"windows", map[string]string{"LOCALAPPDATA": local}, filepath.Join(local, "Google", "Chrome", "User Data")},
		{"linux", map[string]string{"XDG_CONFIG_HOME": xdg}, filepath.Join(xdg, "google-chrome")},
		{"linux", nil, filepath.Join(home, ".config", "google-chrome")},
	} {
		got, err := userChromeDir(tc.platform, home, func(k string) string { return tc.env[k] })
		if err != nil || got != tc.want {
			t.Errorf("%s: %s %v", tc.platform, got, err)
		}
		if _, err := os.Stat(got); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("computing a directory touched it: %s %v", got, err)
		}
	}
	if _, err := userChromeDir("windows", home, func(string) string { return "" }); err == nil {
		t.Fatal("invented a Windows profile without LOCALAPPDATA")
	}
	if _, err := userChromeDir("other", home, func(string) string { return "" }); err == nil {
		t.Fatal("invented an unsupported platform's profile")
	}
}
