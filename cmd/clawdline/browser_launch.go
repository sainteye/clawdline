package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/sainteye/clawdline/extension"
	"github.com/sainteye/clawdline/internal/config"
)

// browserLaunch is `clawdline browser launch`: Clawdline's own Chromium, with
// the extension already loaded and its native messaging host registered in
// its own profile, so nothing is installed into the person's Chrome. The
// profile lives in the daemon's directory and keeps its sign-ins.
func browserLaunch(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("browser launch", flag.ContinueOnError)
	fs.SetOutput(errOut)
	bin := fs.String("browser", os.Getenv("CLAWDLINE_CHROMIUM"), "Chromium binary (default: CLAWDLINE_CHROMIUM, then Playwright's Chromium)")
	if fs.Parse(args) != nil {
		return 2
	}
	if *bin == "" {
		*bin = playwrightChromium()
	}
	if *bin == "" {
		fmt.Fprintln(errOut, "clawdline browser launch: no Chromium found; install one with `npx playwright install chromium`, or name it with --browser or CLAWDLINE_CHROMIUM")
		return 1
	}
	dir := config.Dir()
	extDir := filepath.Join(dir, "chrome-extension")
	if err := writeExtension(extDir); err != nil {
		fmt.Fprintln(errOut, "clawdline browser launch:", err)
		return 1
	}
	real, err := filepath.EvalSymlinks(extDir)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	profile := filepath.Join(dir, "chrome-profile")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	id := unpackedExtensionID(real)
	var hostOut, hostErr discard
	if code := browserInstallHost([]string{"--extension-id", id, "--chrome-dir", profile}, &hostOut, &hostErr); code != 0 {
		fmt.Fprintln(errOut, "clawdline browser launch: the native messaging host could not be registered:", string(hostErr))
		return 1
	}
	cmd := exec.Command(*bin, "--user-data-dir="+profile, "--load-extension="+real, "--disable-extensions-except="+real,
		"--no-first-run", "--no-default-browser-check", "--use-mock-keychain", "--window-size=1280,860", "about:blank")
	detach(cmd)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(errOut, "clawdline browser launch:", err)
		return 1
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	_ = json.NewEncoder(out).Encode(map[string]any{"ok": true, "pid": pid, "browser": *bin, "profile": profile, "extension_id": id})
	return 0
}

type discard []byte

func (d *discard) Write(p []byte) (int, error) { *d = append(*d, p...); return len(p), nil }

// writeExtension replaces dir with the extension carried in this binary, so
// the loaded extension is always the one this build expects.
func writeExtension(dir string) error {
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

// unpackedExtensionID is the id Chromium gives an unpacked extension: the
// first 128 bits of SHA-256 of its absolute path, each hex digit as a-p.
func unpackedExtensionID(path string) string {
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

// playwrightChromium is the newest Chromium Playwright downloaded, or "".
func playwrightChromium() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	var pattern string
	switch runtime.GOOS {
	case "darwin":
		pattern = filepath.Join(home, "Library", "Caches", "ms-playwright", "chromium-*", "chrome-mac", "Chromium.app", "Contents", "MacOS", "Chromium")
	case "linux":
		pattern = filepath.Join(home, ".cache", "ms-playwright", "chromium-*", "chrome-linux", "chrome")
	default:
		return ""
	}
	found, _ := filepath.Glob(pattern)
	sort.Strings(found)
	if len(found) == 0 {
		return ""
	}
	return found[len(found)-1]
}
