// Package browserlaunch starts isolated Chromium and registers native hosts
// for the person's explicitly selected browser profile (docs/browser.md).
// The ordinary launch path uses Clawdline's own profile. Registering the
// person's usual Chrome is a separate, explicit install-host operation.
package browserlaunch

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/sainteye/clawdline/extension"
)

// HostName is the native messaging host's name, which the extension connects to.
const HostName = "com.clawdline.browser"

// InstallCommand downloads a Chromium this package finds on its own.
const InstallCommand = "npx playwright install chromium"

// ErrChromiumMissing is a launch with no Chromium named and none found.
var ErrChromiumMissing = errors.New("no Chromium found; install one with `" + InstallCommand + "`, or name it with --browser or CLAWDLINE_CHROMIUM")

// ProfileDir is the Chromium user-data directory that keeps the sign-ins made
// in Clawdline's own Chromium.
func ProfileDir(dir string) string { return filepath.Join(dir, "chrome-profile") }

// An unpacked extension keeps its installed worker across browser restarts.
// A distinct payload needs a distinct identity, even if its manifest version
// was not changed. Old directories stay intact while a browser may use them.
var extensionRevision = sync.OnceValue(func() string {
	digest, err := extensionDigest(extension.Chrome)
	if err != nil {
		panic("the embedded browser extension cannot be read: " + err.Error())
	}
	return digest
})

// ExtensionDir is where this exact carried extension is written before launch.
func ExtensionDir(dir string) string {
	return filepath.Join(dir, "chrome-extension-"+extensionRevision())
}

func extensionDigest(files fs.FS) (string, error) {
	hash := sha256.New()
	err := fs.WalkDir(files, "chrome", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(files, path)
		if err != nil {
			return err
		}
		hash.Write([]byte(path))
		hash.Write([]byte{0})
		hash.Write(data)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// FindChromium is the Chromium to start: explicit, else CLAWDLINE_CHROMIUM,
// else the newest Chromium Playwright downloaded, else "".
func FindChromium(explicit string, getenv func(string) string) string {
	if explicit != "" {
		return explicit
	}
	if v := getenv("CLAWDLINE_CHROMIUM"); v != "" {
		return v
	}
	return PlaywrightChromium()
}

// PlaywrightChromium is the newest Chromium Playwright downloaded, or "".
func PlaywrightChromium() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	var cache string
	switch runtime.GOOS {
	case "darwin":
		cache = filepath.Join(home, "Library", "Caches", "ms-playwright")
	case "linux":
		cache = filepath.Join(home, ".cache", "ms-playwright")
	default:
		return ""
	}
	return playwrightChromium(cache, runtime.GOOS)
}

func playwrightChromium(cache, platform string) string {
	layouts := []string{"chrome-linux/chrome", "chrome-linux64/chrome"}
	if platform == "darwin" {
		layouts = []string{"chrome-mac/Chromium.app/Contents/MacOS/Chromium", "chrome-mac-arm64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing", "chrome-mac-x64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing"}
	}
	installed, _ := filepath.Glob(filepath.Join(cache, "chromium-*"))
	newest, found := -1, ""
	for _, directory := range installed {
		revision, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(directory), "chromium-"))
		if err != nil || revision <= newest {
			continue
		}
		for _, layout := range layouts {
			file := filepath.Join(directory, filepath.FromSlash(layout))
			if info, err := os.Stat(file); err == nil && info.Mode().IsRegular() {
				newest, found = revision, file
				break
			}
		}
	}
	return found
}

// Running reports whether a Chromium holds the profile now. Chromium keeps a
// SingletonLock link in its user-data directory while it runs and removes it
// when it quits; a crash can leave one behind, so this is a hint for the
// person's guide, not a fact a command relies on.
func Running(dir string) bool {
	_, err := os.Lstat(filepath.Join(ProfileDir(dir), "SingletonLock"))
	return err == nil
}

// WriteExtension replaces dir with the extension carried in this binary, so
// the loaded extension is always the one this build expects.
func WriteExtension(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return fs.WalkDir(extension.Chrome, "chrome", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("chrome", path)
		target := filepath.Join(dir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := extension.Chrome.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// UnpackedExtensionID is the id Chromium gives an unpacked extension: the
// first 128 bits of SHA-256 of its absolute path, each hex digit as a-p.
func UnpackedExtensionID(path string) string {
	sum := sha256.Sum256([]byte(path))
	hexed := hex.EncodeToString(sum[:16])
	out := make([]byte, len(hexed))
	for i, c := range hexed {
		v := c - '0'
		if c >= 'a' {
			v = c - 'a' + 10
		}
		out[i] = byte('a' + v)
	}
	return string(out)
}

// ValidExtensionID is 32 letters a-p, the only spelling Chromium gives an id.
func ValidExtensionID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if c < 'a' || c > 'p' {
			return false
		}
	}
	return true
}

// LauncherBinary is the clawdline the host launcher runs. The daemon's own
// executable lives in a versioned release directory that an update replaces,
// so the stable `<dir>/bin/clawdline` is preferred whenever it exists.
func LauncherBinary(dir string) (string, error) {
	name := "clawdline"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	stable := filepath.Join(dir, "bin", name)
	if info, err := os.Stat(stable); err == nil && !info.IsDir() {
		return stable, nil
	}
	return os.Executable()
}

// Host is what InstallHost wrote.
type Host struct {
	Manifest string `json:"manifest"`
	Launcher string `json:"launcher"`
}

// InstallHost writes Chrome's native messaging manifest for the extension into
// a Chrome user-data directory, and a launcher (`<dir>/browser-host.sh`) that
// pins the daemon's directory and port and runs exe's `browser-host`.
func InstallHost(dir string, port int, exe, extensionID, chromeDir string) (Host, error) {
	return InstallHostForMode(dir, port, exe, extensionID, chromeDir, "isolated")
}

// ValidHostMode excludes native, which has no Clawdline native messaging host.
func ValidHostMode(mode string) bool { return mode == "isolated" || mode == "user_chrome" }

// UserChromeDir computes Chrome's ordinary user-data directory without reading
// or creating it. Only an explicit install-host command uses the result.
func UserChromeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return userChromeDir(runtime.GOOS, home, os.Getenv)
}

func userChromeDir(platform, home string, getenv func(string) string) (string, error) {
	switch platform {
	case "darwin":
		if home != "" {
			return filepath.Join(home, "Library", "Application Support", "Google", "Chrome"), nil
		}
	case "windows":
		if local := getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "Google", "Chrome", "User Data"), nil
		}
	case "linux":
		if config := getenv("XDG_CONFIG_HOME"); config != "" {
			return filepath.Join(config, "google-chrome"), nil
		}
		if home != "" {
			return filepath.Join(home, ".config", "google-chrome"), nil
		}
	}
	return "", errors.New("Chrome's user-data directory is unavailable; name it with --chrome-dir")
}

// InstallHostForMode pins the host to one browser mode and profile. Separate
// launcher paths keep installing one mode from changing the other's process.
func InstallHostForMode(dir string, port int, exe, extensionID, chromeDir, mode string) (Host, error) {
	if !ValidHostMode(mode) {
		return Host{}, errors.New("use isolated or user_chrome for the browser host")
	}
	if !ValidExtensionID(extensionID) || chromeDir == "" || !filepath.IsAbs(chromeDir) {
		return Host{}, errors.New("the extension id must be 32 letters a-p and the Chrome directory an absolute path")
	}
	if port <= 0 || port > 65535 || exe == "" {
		return Host{}, errors.New("the daemon's port and the clawdline binary are required")
	}
	name, otherName := "browser-host.sh", "browser-host-user-chrome.sh"
	if mode == "user_chrome" {
		name, otherName = otherName, name
	}
	launcher := filepath.Join(dir, name)
	hosts := filepath.Join(chromeDir, "NativeMessagingHosts")
	path := filepath.Join(hosts, HostName+".json")
	// A user-data directory can register this host name only once. Refuse an
	// accidental cross-mode target before touching either launcher or manifest.
	if data, err := os.ReadFile(path); err == nil {
		var previous struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(data, &previous) == nil && filepath.Base(previous.Path) == otherName {
			return Host{}, errors.New("that Chrome directory already has the other browser mode's host")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Host{}, err
	}
	var script bytes.Buffer
	fmt.Fprintf(&script, "#!/bin/sh\nCLAWDLINE_BROWSER_MODE=%s CLAWDLINE_BROWSER_PROFILE=%s CLAWDLINE_NEXT_DIR=%s CLAWDLINE_NEXT_PORT=%d exec %s browser-host\n", shellQuote(mode), shellQuote(chromeDir), shellQuote(dir), port, shellQuote(exe))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Host{}, err
	}
	if err := os.WriteFile(launcher, script.Bytes(), 0o700); err != nil {
		return Host{}, err
	}
	if err := os.MkdirAll(hosts, 0o755); err != nil {
		return Host{}, err
	}
	manifest, _ := json.MarshalIndent(map[string]any{
		"name": HostName, "description": "Clawdline browser bridge", "path": launcher,
		"type": "stdio", "allowed_origins": []string{"chrome-extension://" + extensionID + "/"},
	}, "", "  ")
	if err := os.WriteFile(path, append(manifest, '\n'), 0o644); err != nil {
		return Host{}, err
	}
	return Host{Manifest: path, Launcher: launcher}, nil
}

// Options names one launch.
type Options struct {
	Dir     string // the daemon's state directory (CLAWDLINE_NEXT_DIR)
	Port    int    // the daemon's port, pinned in the host launcher
	Browser string // the Chromium binary; "" is refused with ErrChromiumMissing
	Exe     string // the clawdline the host launcher runs; "" is LauncherBinary(Dir)
}

// Started is a Chromium that was started.
type Started struct {
	PID         int    `json:"pid"`
	Browser     string `json:"browser"`
	Profile     string `json:"profile"`
	ExtensionID string `json:"extension_id"`
}

// Launch writes the extension, registers the host in Clawdline's profile and
// starts Chromium on that profile, detached, without waiting for it. When a
// Chromium already holds the profile, the new process hands its arguments to
// that one, which opens another window, and exits.
func Launch(o Options) (Started, error) {
	if o.Browser == "" {
		return Started{}, ErrChromiumMissing
	}
	extDir := ExtensionDir(o.Dir)
	if err := WriteExtension(extDir); err != nil {
		return Started{}, err
	}
	real, err := filepath.EvalSymlinks(extDir)
	if err != nil {
		return Started{}, err
	}
	profile := ProfileDir(o.Dir)
	if err := os.MkdirAll(profile, 0o700); err != nil {
		return Started{}, err
	}
	exe := o.Exe
	if exe == "" {
		if exe, err = LauncherBinary(o.Dir); err != nil {
			return Started{}, err
		}
	}
	id := UnpackedExtensionID(real)
	if _, err := InstallHost(o.Dir, o.Port, exe, id, profile); err != nil {
		return Started{}, fmt.Errorf("the native messaging host could not be registered: %w", err)
	}
	cmd := exec.Command(o.Browser, "--user-data-dir="+profile, "--load-extension="+real, "--disable-extensions-except="+real,
		"--no-first-run", "--no-default-browser-check", "--use-mock-keychain", "--window-size=1280,860", "about:blank")
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return Started{}, err
	}
	pid := cmd.Process.Pid
	// Reaped when it quits, so a daemon that started it keeps no zombie.
	go func() { _ = cmd.Wait() }()
	return Started{PID: pid, Browser: o.Browser, Profile: profile, ExtensionID: id}, nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
