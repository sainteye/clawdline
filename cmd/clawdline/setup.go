package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/install"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/release"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// `clawdline setup` is everything install.sh does not: it runs from the
// release install.sh unpacked, proves that release is the signed one, checks
// what the daemon needs, and makes it this machine's per-user service. Running
// it again repairs; `--uninstall` takes it all away again. docs/updates.md and
// docs/user/install.md describe the layout it shares with the updater.

const setupUsage = `usage: clawdline setup [options]

Installs the release this binary belongs to as this user's Clawdline service:
checks its signature, checks for tmux and an assistant, points
<root>/current at it, links clawdline into ~/.local/bin, starts the daemon as
a systemd --user unit (Linux) or a LaunchAgent (macOS), installs the app on
macOS with a desktop, and proves the console answers. Running it again repairs.

  --headless          no desktop: no app, no browser; print the sign-in address
  --no-app            macOS: do not install Clawdline Next.app
  --no-autostart      start the service now, but not at login or boot
  --channel <c>       stable (default) or beta: which releases this machine follows
  --port <n>          the daemon's port (default 7727); another port gets its own service name
  --adopt             take over a source deploy (tools/deploy-linux-user.sh) in the same layout
  --no-session-check  do not start and stop one throwaway terminal to prove tmux works
  --uninstall         stop and remove the service, the link and the installed releases
  --purge             with --uninstall: remove the state directory too (devices, sessions, settings)
  --archive <file>    the daemon archive install.sh downloaded (checked against the manifest)
  --manifest-dir <d>  where install.sh put manifest.json and manifest.sig.json

The install root is ~/.local/share/clawdline-next (CLAWDLINE_NEXT_INSTALL_ROOT),
the link directory ~/.local/bin (CLAWDLINE_NEXT_BIN_DIR), the state directory
~/.config/clawdline-next (CLAWDLINE_NEXT_DIR), and apps go to ~/Applications
(CLAWDLINE_NEXT_APPS_DIR).`

type setupOptions struct {
	headless, noApp, noAutostart, adopt bool
	uninstall, purge, noSessionCheck    bool
	channel, archive, manifestDir       string
	port                                int
	channelSet                          bool
}

// setupHost is the machine setup acts on: the parts a test replaces.
type setupHost struct {
	goos, goarch string
	home         string
	uid          int
	username     string
	getenv       func(string) string
	// keys are the release keys this binary trusts.
	keys []ed25519.PublicKey
	// run runs a command and answers its combined output.
	run         func(name string, args ...string) ([]byte, error)
	portAnswers func(port int) bool
	out, errOut io.Writer
}

func realSetupHost() setupHost {
	home, _ := os.UserHomeDir()
	name := os.Getenv("USER")
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	return setupHost{
		goos: runtime.GOOS, goarch: runtime.GOARCH, home: home, uid: os.Getuid(), username: name,
		getenv: os.Getenv, keys: release.TrustedKeys(),
		run: func(name string, args ...string) ([]byte, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		},
		portAnswers: func(port int) bool {
			c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Second)
			if err != nil {
				return false
			}
			c.Close()
			return true
		},
		out: os.Stdout, errOut: os.Stderr,
	}
}

func setupCommand(args []string) {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var o setupOptions
	fs.BoolVar(&o.headless, "headless", false, "")
	fs.BoolVar(&o.noApp, "no-app", false, "")
	fs.BoolVar(&o.noAutostart, "no-autostart", false, "")
	fs.BoolVar(&o.adopt, "adopt", false, "")
	fs.BoolVar(&o.uninstall, "uninstall", false, "")
	fs.BoolVar(&o.purge, "purge", false, "")
	fs.BoolVar(&o.noSessionCheck, "no-session-check", false, "")
	fs.StringVar(&o.channel, "channel", "", "")
	fs.StringVar(&o.archive, "archive", "", "")
	fs.StringVar(&o.manifestDir, "manifest-dir", "", "")
	fs.IntVar(&o.port, "port", 0, "")
	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Println(setupUsage)
		return
	}
	if err != nil || fs.NArg() != 0 {
		if err != nil {
			fmt.Fprintln(os.Stderr, "clawdline setup:", err)
		}
		fmt.Fprintln(os.Stderr, setupUsage)
		os.Exit(2)
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "channel" {
			o.channelSet = true
		}
	})
	if o.channel == "" {
		o.channel = "stable"
	}
	if o.channel != "stable" && o.channel != "beta" {
		fmt.Fprintln(os.Stderr, "clawdline setup: --channel is stable or beta")
		os.Exit(2)
	}
	if o.purge && !o.uninstall {
		fmt.Fprintln(os.Stderr, "clawdline setup: --purge goes with --uninstall")
		os.Exit(2)
	}
	if o.port == 0 {
		p, err := daemonPort()
		if err != nil {
			fail(err)
		}
		o.port = p
	}
	if o.port <= 0 || o.port > 65535 {
		fmt.Fprintln(os.Stderr, "clawdline setup: --port is 1 to 65535")
		os.Exit(2)
	}
	h := realSetupHost()
	if o.uninstall {
		os.Exit(runUninstall(h, o))
	}
	os.Exit(runSetup(h, o))
}

// setupPlace is where this install lives.
type setupPlace struct {
	layout    install.Layout
	rootNamed bool
	stateDir  string
	stateSet  bool
	suffix    string
}

func resolvePlace(h setupHost, port int) (setupPlace, error) {
	l, err := install.DefaultLayout()
	if err != nil {
		return setupPlace{}, err
	}
	p := setupPlace{layout: l, rootNamed: h.getenv("CLAWDLINE_NEXT_INSTALL_ROOT") != ""}
	p.stateDir = config.Dir()
	p.stateSet = filepath.Clean(p.stateDir) != filepath.Join(h.home, ".config", config.AppDir)
	p.suffix = install.ServiceSuffix(port, l.Root, p.rootNamed)
	return p, nil
}

func (p setupPlace) serviceName(goos string) string {
	if goos == "darwin" {
		return install.LaunchdLabel(p.suffix)
	}
	return install.UnitName(p.suffix)
}

func serviceFilePath(h setupHost, p setupPlace) string {
	if h.goos == "darwin" {
		return filepath.Join(h.home, "Library", "LaunchAgents", install.LaunchdLabel(p.suffix)+".plist")
	}
	cfg := h.getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(h.home, ".config")
	}
	return filepath.Join(cfg, "systemd", "user", install.UnitName(p.suffix))
}

func setupRefuse(h setupHost, err error) int {
	var r *install.Refusal
	var rel *release.Error
	switch {
	case errors.As(err, &r):
		fmt.Fprintf(h.errOut, "clawdline setup: %s\n  (%s)\n", r.Detail, r.Code)
	case errors.As(err, &rel):
		fmt.Fprintf(h.errOut, "clawdline setup: the release was not installed: %s\n  (%s)\n", rel.Detail, rel.Code)
	default:
		fmt.Fprintln(h.errOut, "clawdline setup:", err)
	}
	return 1
}

func runSetup(h setupHost, o setupOptions) int {
	if !supportedPlatform(h.goos, h.goarch) {
		return setupRefuse(h, &install.Refusal{Code: install.CodeUnsupported,
			Detail: fmt.Sprintf("Clawdline installs on macOS and Linux (amd64 or arm64); this is %s/%s", h.goos, h.goarch)})
	}
	place, err := resolvePlace(h, o.port)
	if err != nil {
		return setupRefuse(h, err)
	}
	exe, err := os.Executable()
	if err != nil {
		return setupRefuse(h, err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	// a. The release: signed, and unpacked exactly as signed.
	m, name, err := verifyRelease(h, place.layout, exe, o)
	if err != nil {
		return setupRefuse(h, err)
	}
	fmt.Fprintf(h.out, "release %s (%s), signature verified\n", m.Version, m.Commit[:12])
	if o.channel == "stable" && m.Channel == "beta" {
		if o.channelSet {
			return setupRefuse(h, &install.Refusal{Code: install.CodeChannelMismatch,
				Detail: m.Version + " is a beta release; install it with --channel beta"})
		}
		o.channel = "beta"
	}

	// b, c. What the daemon needs, and where it was found.
	found, pm := findTools(h)
	report := install.Prereqs{Found: found, PackageManager: pm}.Check(h.goos)
	for _, line := range report.Lines {
		fmt.Fprintln(h.out, line)
	}
	if report.Stop {
		fmt.Fprintln(h.errOut, "clawdline setup: nothing was activated  ("+install.CodeTmuxMissing+")")
		return 1
	}
	shell := h.getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	// e. What is already here, before anything changes.
	unitFile := serviceFilePath(h, place)
	ex := install.Existing{Port: o.port, PortAnswers: h.portAnswers(o.port), ServiceName: place.serviceName(h.goos)}
	if sf, err := install.ReadServiceFile(place.stateDir); err == nil {
		ex.Service = &sf
	}
	if _, err := os.Stat(unitFile); err == nil {
		ex.ServiceInstalled = true
	}
	ex.Current, ex.CurrentKind = place.layout.CurrentRelease()
	if err := ex.Decide(o.adopt); err != nil {
		return setupRefuse(h, err)
	}
	if ex.CurrentKind == install.KindSourceDeploy {
		fmt.Fprintf(h.out, "adopting the source deploy releases/%s; it stays for rollback\n", ex.Current)
	}

	// d. Activate.
	if err := place.layout.SwitchCurrent(name); err != nil {
		return setupRefuse(h, err)
	}
	fmt.Fprintf(h.out, "current -> releases/%s\n", name)
	if err := linkBin(place.layout); err != nil {
		fmt.Fprintf(h.errOut, "warning: %v\n", err)
	} else {
		fmt.Fprintf(h.out, "linked %s\n", place.layout.BinLink)
	}
	binDir := filepath.Dir(place.layout.BinLink)
	if !install.OnPath(h.getenv("PATH"), binDir) {
		fmt.Fprintf(h.out, "%s is not on your PATH; add this line to your shell's profile:\n    export PATH=\"%s:$PATH\"\n", binDir, binDir)
	}

	// f. The service.
	spec := install.ServiceSpec{
		Exec: filepath.Join(place.layout.Current, "clawdline"), Port: o.port,
		Path: install.ServicePath(found, install.ExtraToolDirs(h.home)), Shell: shell, Home: h.home,
		Autostart: !o.noAutostart,
	}
	if place.stateSet {
		spec.StateDir = place.stateDir
	}
	if place.rootNamed {
		spec.InstallRoot = place.layout.Root
	}
	if err := os.MkdirAll(place.stateDir, 0o700); err != nil {
		return setupRefuse(h, err)
	}
	sf := install.ServiceFile{Port: o.port, Root: place.layout.Root, File: unitFile, Channel: o.channel}
	gui := false
	if h.goos == "darwin" {
		spec.LogPath = filepath.Join(place.stateDir, "service.log")
		domain, isGUI := launchdDomain(h)
		gui = isGUI
		sf.Supervisor, sf.Name, sf.Domain = "launchd", install.LaunchdLabel(place.suffix), domain
		if !isGUI {
			fmt.Fprintf(h.out, "no desktop login session (launchctl print gui/%d failed): the service goes into %s and runs while this user is logged in\n", h.uid, domain)
		}
		if err := installLaunchAgent(h, unitFile, sf.Name, domain, spec); err != nil {
			return setupRefuse(h, err)
		}
	} else {
		sf.Supervisor, sf.Name = "systemd", install.UnitName(place.suffix)
		if err := installSystemdUnit(h, unitFile, sf.Name, spec); err != nil {
			return setupRefuse(h, err)
		}
	}
	// g. The app, on a Mac someone sits at.
	desktop := !o.headless && (gui || (h.goos == "linux" && (h.getenv("DISPLAY") != "" || h.getenv("WAYLAND_DISPLAY") != "")))
	if prev, err := install.ReadServiceFile(place.stateDir); err == nil {
		sf.App = prev.App
	}
	if h.goos == "darwin" && desktop && !o.noApp {
		if app, err := installApp(h, place.layout, m); err != nil {
			fmt.Fprintf(h.errOut, "warning: the app was not installed: %v\n", err)
		} else if app != "" {
			sf.App = app
		}
	}
	if err := install.WriteServiceFile(place.stateDir, sf); err != nil {
		return setupRefuse(h, err)
	}
	fmt.Fprintf(h.out, "service %s (%s) written to %s\n", sf.Name, sf.Supervisor, unitFile)

	// h. Prove it.
	if err := proveInstall(h, place, o, m.Commit); err != nil {
		fmt.Fprintf(h.errOut, "the service is installed but did not prove itself; `clawdline doctor` and %s say more\n",
			filepath.Join(place.stateDir, "daemon.log"))
		return setupRefuse(h, err)
	}
	fmt.Fprintf(h.out, "\nClawdline %s is installed and running on http://127.0.0.1:%d\n", m.Version, o.port)
	if o.noAutostart {
		fmt.Fprintln(h.out, "it was started now and will not start by itself at login (--no-autostart)")
	} else {
		fmt.Fprintln(h.out, "it starts by itself at login; `clawdline setup --no-autostart` turns that off")
	}
	fmt.Fprintln(h.out, "uninstall: clawdline setup --uninstall"+install.PortFlag(o.port))
	openConsole(h, place, o, desktop, sf.App)
	return 0
}

func supportedPlatform(goos, goarch string) bool {
	return (goos == "darwin" || goos == "linux") && (goarch == "amd64" || goarch == "arm64")
}

// verifyRelease proves that the release exe belongs to is the one the signed
// manifest names, and answers the manifest and the release's name.
func verifyRelease(h setupHost, l install.Layout, exe string, o setupOptions) (release.Manifest, string, error) {
	dir := filepath.Dir(exe)
	if filepath.Dir(dir) != filepath.Clean(evalOr(l.Releases)) && filepath.Dir(dir) != filepath.Clean(l.Releases) {
		return release.Manifest{}, "", &install.Refusal{Code: install.CodeCurrentNotRelease, Detail: fmt.Sprintf(
			"setup runs from an unpacked release under %s; this binary is %s. Run the installer (install.sh), "+
				"or `clawdline setup` from an installed release", l.Releases, exe)}
	}
	name := filepath.Base(dir)
	mdir := o.manifestDir
	if mdir == "" {
		mdir = dir
	}
	manifest, err := readBounded(filepath.Join(mdir, "manifest.json"), release.MaxManifestBytes())
	if err != nil {
		return release.Manifest{}, "", &install.Refusal{Code: install.CodeSignature, Detail: "no manifest.json beside the release: " + err.Error()}
	}
	sigs, err := readBounded(filepath.Join(mdir, "manifest.sig.json"), release.MaxSignatureBytes())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return release.Manifest{}, "", err
	}
	m, err := release.Open(manifest, sigs, h.keys)
	if err != nil {
		return release.Manifest{}, "", err
	}
	if m.Version != name {
		return release.Manifest{}, "", &install.Refusal{Code: install.CodeSignature, Detail: fmt.Sprintf(
			"the signed manifest is for %s, but this binary runs from releases/%s", m.Version, name)}
	}
	if o.archive != "" {
		a, err := m.Artifact(h.goos, h.goarch, release.KindDaemon)
		if err != nil {
			return release.Manifest{}, "", err
		}
		f, err := os.Open(o.archive)
		if err != nil {
			return release.Manifest{}, "", err
		}
		err = a.Check(f, nil)
		f.Close()
		if err != nil {
			return release.Manifest{}, "", err
		}
		if err := install.VerifyTree(o.archive, dir); err != nil {
			return release.Manifest{}, "", err
		}
	} else {
		fmt.Fprintln(h.out, "no --archive: the manifest's signature is checked, the unpacked files are taken as installed")
	}
	// Kept beside the release, so a later `setup` (a repair) and the updater
	// can verify what is installed without downloading it again.
	if mdir != dir {
		if err := writeAtomic(filepath.Join(dir, "manifest.json"), manifest, 0o644); err != nil {
			return release.Manifest{}, "", err
		}
		if err := writeAtomic(filepath.Join(dir, "manifest.sig.json"), sigs, 0o644); err != nil {
			return release.Manifest{}, "", err
		}
	}
	return m, name, nil
}

func evalOr(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s is longer than %d bytes", path, limit)
	}
	return b, nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// findTools looks for tmux, claude and codex the way the person's own shell
// would find them, and then in the usual places a service PATH misses.
func findTools(h setupHost) (map[string]string, string) {
	found := map[string]string{}
	shell := h.getenv("SHELL")
	if shell == "" || filepath.Base(shell) == "fish" || filepath.Base(shell) == "nu" {
		shell = "/bin/sh"
	}
	script := `for c in tmux claude codex; do p=$(command -v "$c" 2>/dev/null) && printf '%s=%s\n' "$c" "$p"; done`
	if out, err := h.run(shell, "-lc", script); err == nil || len(out) > 0 {
		for _, line := range strings.Split(string(out), "\n") {
			k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
			if ok && filepath.IsAbs(v) && isExecutable(v) {
				found[k] = v
			}
		}
	}
	for _, c := range []string{"tmux", "claude", "codex"} {
		if _, ok := found[c]; ok {
			continue
		}
		for _, d := range install.ExtraToolDirs(h.home) {
			if p := filepath.Join(d, c); isExecutable(p) {
				found[c] = p
				break
			}
		}
		if _, ok := found[c]; !ok {
			if p, err := exec.LookPath(c); err == nil && filepath.IsAbs(p) {
				found[c] = p
			}
		}
	}
	pm := install.DetectPackageManager(h.goos, func(c string) bool {
		if _, err := exec.LookPath(c); err == nil {
			return true
		}
		return h.goos == "darwin" && (isExecutable("/opt/homebrew/bin/brew") || isExecutable("/usr/local/bin/brew"))
	})
	return found, pm
}

func fileExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func isExecutable(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// linkBin points the PATH link at current/clawdline, replacing only a link.
func linkBin(l install.Layout) error {
	target := filepath.Join(l.Current, "clawdline")
	if info, err := os.Lstat(l.BinLink); err == nil && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s is a file setup did not make; it was left as it is, and %s is the installed command", l.BinLink, target)
	}
	if err := os.MkdirAll(filepath.Dir(l.BinLink), 0o755); err != nil {
		return err
	}
	tmp := l.BinLink + ".new"
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, l.BinLink)
}

// launchdDomain is gui/<uid> when this user has a desktop login session, else
// user/<uid>, which exists for a user logged in only over SSH.
func launchdDomain(h setupHost) (string, bool) {
	gui := fmt.Sprintf("gui/%d", h.uid)
	if _, err := h.run("launchctl", "print", gui); err == nil {
		return gui, true
	}
	return fmt.Sprintf("user/%d", h.uid), false
}

func installLaunchAgent(h setupHost, plist, label, domain string, spec install.ServiceSpec) error {
	if err := writeAtomic(plist, []byte(install.LaunchdPlist(label, spec)), 0o644); err != nil {
		return err
	}
	// A loaded job keeps the plist it was loaded with: out first, then in,
	// which is also the restart that makes a repair take effect.
	_, _ = h.run("launchctl", "bootout", domain+"/"+label)
	var last error
	for attempt := 0; attempt < 10; attempt++ {
		out, err := h.run("launchctl", "bootstrap", domain, plist)
		if err == nil {
			last = nil
			break
		}
		last = fmt.Errorf("launchctl bootstrap %s %s: %v: %s", domain, plist, err, strings.TrimSpace(string(out)))
		time.Sleep(500 * time.Millisecond)
	}
	if last != nil {
		return &install.Refusal{Code: install.CodeServiceFailed, Detail: last.Error()}
	}
	if !spec.Autostart {
		if out, err := h.run("launchctl", "kickstart", domain+"/"+label); err != nil {
			return &install.Refusal{Code: install.CodeServiceFailed, Detail: fmt.Sprintf("launchctl kickstart: %v: %s", err, out)}
		}
	}
	return nil
}

// userBus makes `systemctl --user` reachable from a shell that did not set
// XDG_RUNTIME_DIR (su, sudo -u), and says when there is no user manager at all.
func userBus(h setupHost) error {
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		dir := fmt.Sprintf("/run/user/%d", h.uid)
		if _, err := os.Stat(dir); err == nil {
			_ = os.Setenv("XDG_RUNTIME_DIR", dir)
		}
	}
	if out, err := h.run("systemctl", "--user", "show-environment"); err != nil {
		return &install.Refusal{Code: install.CodeNoUserManager, Detail: fmt.Sprintf(
			"this user has no systemd user manager to run the service (systemctl --user: %s). Log in as %s directly "+
				"(not through su), or have an administrator run: sudo loginctl enable-linger %s — then run the installer again",
			strings.TrimSpace(string(out)), h.username, h.username)}
	}
	return nil
}

func installSystemdUnit(h setupHost, unit, name string, spec install.ServiceSpec) error {
	if err := userBus(h); err != nil {
		return err
	}
	if err := writeAtomic(unit, []byte(install.SystemdUnit(spec)), 0o644); err != nil {
		return err
	}
	steps := [][]string{{"--user", "daemon-reload"}}
	if spec.Autostart {
		steps = append(steps, []string{"--user", "enable", name})
	} else {
		steps = append(steps, []string{"--user", "disable", name})
	}
	steps = append(steps, []string{"--user", "restart", name})
	for _, s := range steps {
		if out, err := h.run("systemctl", s...); err != nil && s[1] != "disable" {
			return &install.Refusal{Code: install.CodeServiceFailed, Detail: fmt.Sprintf("systemctl %s: %v: %s",
				strings.Join(s, " "), err, strings.TrimSpace(string(out)))}
		}
	}
	if spec.Autostart {
		linger(h)
	}
	return nil
}

// linger lets the service start at boot and outlive the last logout. Without
// polkit's permission it cannot be done without an administrator, and the
// exact command is said.
func linger(h setupHost) {
	out, _ := h.run("loginctl", "show-user", h.username, "-p", "Linger", "--value")
	if strings.TrimSpace(string(out)) == "yes" {
		fmt.Fprintln(h.out, "linger is on: the service starts at boot and keeps running after you log out")
		return
	}
	if _, err := h.run("loginctl", "enable-linger", h.username); err == nil {
		fmt.Fprintln(h.out, "turned linger on: the service starts at boot and keeps running after you log out")
		return
	}
	fmt.Fprintf(h.out, "the service runs while %s is logged in. To start it at boot and keep it running after logout, an administrator runs:\n    sudo loginctl enable-linger %s\n", h.username, h.username)
}

const appName = "Clawdline Next.app"

func appsDir(h setupHost) string {
	if d := h.getenv("CLAWDLINE_NEXT_APPS_DIR"); d != "" {
		return d
	}
	return filepath.Join(h.home, "Applications")
}

// installApp puts the release's app bundle in ~/Applications, staged beside
// it and renamed into place; a running app is never modified. It answers the
// installed path, or "" when the release carries no app for this Mac.
func installApp(h setupHost, l install.Layout, m release.Manifest) (string, error) {
	if h.goarch != "arm64" {
		fmt.Fprintln(h.out, "the app is built for Apple silicon; this machine gets the daemon and the browser console")
		return "", nil
	}
	a, err := m.Artifact("darwin", "arm64", release.KindApp)
	if err != nil {
		fmt.Fprintf(h.out, "release %s carries no app; the console opens in the browser\n", m.Version)
		return "", nil
	}
	if err := os.MkdirAll(l.Staging, 0o755); err != nil {
		return "", err
	}
	archive := filepath.Join(l.Staging, a.Name)
	if err := downloadArtifact(a, archive); err != nil {
		return "", err
	}
	defer os.Remove(archive)
	return placeApp(h, archive, filepath.Join(appsDir(h), appName), l.Staging, m.Version)
}

// placeApp unpacks archive and renames its bundle to target.
func placeApp(h setupHost, archive, target, staging, version string) (string, error) {
	unpacked := filepath.Join(staging, "app-"+version)
	_ = os.RemoveAll(unpacked)
	if err := install.Extract(archive, unpacked); err != nil {
		return "", err
	}
	defer os.RemoveAll(unpacked)
	bundle := filepath.Join(unpacked, appName)
	if _, err := os.Stat(filepath.Join(bundle, "Contents", "MacOS")); err != nil {
		return "", fmt.Errorf("the app archive holds no %s", appName)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	staged := filepath.Join(filepath.Dir(target), "."+appName+".new")
	_ = os.RemoveAll(staged)
	if err := os.Rename(bundle, staged); err != nil {
		return "", err
	}
	if appRunning(h, target) {
		fmt.Fprintf(h.out, "%s is running, so it was not replaced: the new one is staged as %s. Quit the app and run `clawdline setup` again\n", target, staged)
		return target, nil
	}
	old := target + ".old"
	_ = os.RemoveAll(old)
	if _, err := os.Lstat(target); err == nil {
		if err := os.Rename(target, old); err != nil {
			return "", err
		}
	}
	if err := os.Rename(staged, target); err != nil {
		_ = os.Rename(old, target)
		return "", err
	}
	_ = os.RemoveAll(old)
	fmt.Fprintf(h.out, "installed %s\n", target)
	return target, nil
}

func appRunning(h setupHost, bundle string) bool {
	out, err := h.run("pgrep", "-f", filepath.Join(bundle, "Contents", "MacOS")+"/")
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func downloadArtifact(a release.Artifact, path string) error {
	res, err := (&http.Client{Timeout: 10 * time.Minute}).Get(a.URL)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", a.URL, res.Status)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	err = a.Check(res.Body, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
	}
	return err
}

// setupHealthSeconds is how long setup waits for the service's console.
const setupHealthSeconds = 60

// proveInstall is the console answering, the served build being the signed
// commit, and — unless asked not to — one throwaway terminal started and
// closed through the daemon, which proves tmux runs under the service's PATH.
func proveInstall(h setupHost, p setupPlace, o setupOptions, commit string) error {
	base := "http://127.0.0.1:" + strconv.Itoa(o.port)
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(setupHealthSeconds * time.Second)
	var last string
	var token string
	for {
		last = ""
		if res, err := client.Get(base + "/"); err != nil {
			last = "GET / failed: " + err.Error()
		} else {
			res.Body.Close()
			if res.StatusCode != http.StatusOK {
				last = "GET / answered " + res.Status
			}
		}
		if last == "" {
			t, err := localToken(config.Config{Dir: p.stateDir})
			if err != nil {
				last = err.Error()
			} else {
				token = t
				stamp, err := servedStamp(client, base, token)
				switch {
				case err != nil:
					last = "BUILD.json: " + err.Error()
				case stamp != commit:
					last = fmt.Sprintf("the console serves build %q, the release is %s — another daemon may hold the port", stamp, commit)
				}
			}
		}
		if last == "" {
			break
		}
		if time.Now().After(deadline) {
			return &install.Refusal{Code: install.CodeHealthFailed, Detail: fmt.Sprintf("after %ds: %s", setupHealthSeconds, last)}
		}
		time.Sleep(time.Second)
	}
	fmt.Fprintf(h.out, "console: GET / 200, BUILD.json names %s\n", commit[:12])
	if o.noSessionCheck {
		return nil
	}
	if err := sessionCheck(base, token, p.stateDir); err != nil {
		return &install.Refusal{Code: install.CodeSessionCheck, Detail: err.Error()}
	}
	fmt.Fprintln(h.out, "session check: started and closed one tmux terminal through the daemon")
	return nil
}

func servedStamp(c *http.Client, base, token string) (string, error) {
	req, _ := http.NewRequest(http.MethodGet, base+"/BUILD.json", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", errors.New(res.Status)
	}
	var b struct {
		Stamp string `json:"stamp"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&b); err != nil {
		return "", err
	}
	return b.Stamp, nil
}

// setupCheckDir is the throwaway project the session check opens a terminal
// in: inside the state directory, never somebody's work.
const setupCheckDir = "setup-check"

func sessionCheck(base, token, stateDir string) error {
	dir := filepath.Join(stateDir, setupCheckDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	registry := projects.OpenPlaceRegistry(stateDir, foreignDirs()...)
	resolved, _ := capacity.Resolve(capacity.Register(), os.Getenv(capacity.OverrideEnv))
	registry.SetLimit(capacity.Limit(resolved, capacity.PlacesRegistered))
	if _, err := registry.Add([]string{dir}, time.Now()); err != nil {
		return fmt.Errorf("could not register the throwaway project: %w", err)
	}
	defer func() {
		_, _ = registry.Remove([]string{dir})
		_ = os.RemoveAll(dir)
	}()
	call := func(method, path string, body, out any) error {
		var r io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			r = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, base+path, r)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
		if err != nil {
			return fmt.Errorf("%s %s: %w", method, path, err)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("%s %s answered %s: %s", method, path, res.Status, strings.TrimSpace(string(data)))
		}
		if out != nil {
			return json.Unmarshal(data, out)
		}
		return nil
	}
	want := evalOr(dir)
	var places contract.StartPlaceList
	var id string
	for attempt := 0; attempt < 10 && id == ""; attempt++ {
		if err := call(http.MethodGet, "/v1/places", nil, &places); err != nil {
			return err
		}
		for _, p := range places.Places {
			if evalOr(p.Path) == want {
				id = p.ID
			}
		}
		if id == "" {
			time.Sleep(500 * time.Millisecond)
		}
	}
	if id == "" {
		return fmt.Errorf("the daemon does not list the throwaway project %s it was given; a state directory under a "+
			"temporary directory (/tmp) is never listed as a project, so there pass --no-session-check", dir)
	}
	var t contract.Terminal
	if err := call(http.MethodPost, "/v1/terminals", contract.TerminalOpenRequest{ProjectID: id, Cols: 80, Rows: 24}, &t); err != nil {
		return fmt.Errorf("could not start a terminal (is tmux on the service's PATH?): %w", err)
	}
	var c contract.TerminalControl
	if err := call(http.MethodPost, "/v1/terminals/"+t.ID+"/control",
		contract.TerminalControlRequest{Action: "takeover", Client: "setup-check"}, &c); err != nil {
		return err
	}
	return call(http.MethodDelete, "/v1/terminals/"+t.ID, contract.TerminalCloseRequest{Client: "setup-check", Epoch: c.Epoch}, nil)
}

// openConsole ends setup with the console: the app or a browser on a desktop,
// the sign-in address and the SSH forward otherwise.
func openConsole(h setupHost, p setupPlace, o setupOptions, desktop bool, app string) {
	exe := filepath.Join(p.layout.Current, "clawdline")
	env := append(os.Environ(), "CLAWDLINE_NEXT_PORT="+strconv.Itoa(o.port), "CLAWDLINE_NEXT_DIR="+p.stateDir)
	if desktop {
		if app != "" {
			if _, err := h.run("open", app); err == nil {
				fmt.Fprintf(h.out, "opened %s\n", app)
				return
			}
		}
		cmd := exec.Command(exe, "open")
		cmd.Env, cmd.Stdout, cmd.Stderr = env, h.out, h.errOut
		if cmd.Run() == nil {
			return
		}
	}
	cmd := exec.Command(exe, "open", "--print")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintln(h.out, "sign in later with: clawdline open --print")
		return
	}
	fmt.Fprintf(h.out, "sign in by opening this address in a browser on this machine (it carries a key; do not share it):\n    %s\n", strings.TrimSpace(string(out)))
	fmt.Fprintf(h.out, "from another computer, forward the port first:\n    ssh -L %d:127.0.0.1:%d <this machine>\n", o.port, o.port)
}

// runUninstall stops and removes what setup installed. The state directory —
// devices, sessions, settings — stays unless --purge.
func runUninstall(h setupHost, o setupOptions) int {
	place, err := resolvePlace(h, o.port)
	if err != nil {
		return setupRefuse(h, err)
	}
	removed, kept := uninstall(h, place)
	for _, r := range removed {
		fmt.Fprintln(h.out, "removed "+r)
	}
	if o.purge {
		if err := os.RemoveAll(place.stateDir); err == nil {
			fmt.Fprintln(h.out, "removed "+place.stateDir+" (--purge)")
		}
	} else if _, err := os.Stat(place.stateDir); err == nil {
		kept = append(kept, place.stateDir+" (devices, sessions and settings; --purge removes it)")
	}
	if len(removed) == 0 {
		fmt.Fprintln(h.out, "nothing of this install was found to remove")
	}
	for _, k := range kept {
		fmt.Fprintln(h.out, "kept "+k)
	}
	return 0
}

func uninstall(h setupHost, p setupPlace) (removed, kept []string) {
	sf, err := install.ReadServiceFile(p.stateDir)
	if err != nil {
		sf = install.ServiceFile{Name: p.serviceName(h.goos), File: serviceFilePath(h, p)}
		if h.goos == "darwin" {
			sf.Supervisor = "launchd"
			sf.Domain, _ = launchdDomain(h)
		} else {
			sf.Supervisor = "systemd"
		}
	}
	if sf.File == "" {
		sf.File = serviceFilePath(h, p)
	}
	// Only a service this install has a record or a file of is stopped:
	// never a label or unit somebody else loaded under a similar name.
	known := err == nil || fileExists(sf.File)
	switch {
	case !known:
	case sf.Supervisor == "launchd":
		if _, err := h.run("launchctl", "bootout", sf.Domain+"/"+sf.Name); err == nil {
			removed = append(removed, "service "+sf.Name+" (stopped)")
		}
	case sf.Supervisor == "systemd":
		_ = userBus(h)
		if _, err := h.run("systemctl", "--user", "disable", "--now", sf.Name); err == nil {
			removed = append(removed, "service "+sf.Name+" (stopped and disabled)")
		}
	}
	if err := os.Remove(sf.File); err == nil {
		removed = append(removed, sf.File)
	}
	if known && sf.Supervisor == "systemd" {
		_, _ = h.run("systemctl", "--user", "daemon-reload")
	}
	if _, err := os.Stat(filepath.Join(p.stateDir, install.ServiceFileName)); err == nil {
		if install.RemoveServiceFile(p.stateDir) == nil {
			removed = append(removed, filepath.Join(p.stateDir, install.ServiceFileName))
		}
	}
	if target, err := os.Readlink(p.layout.BinLink); err == nil && strings.HasPrefix(target, p.layout.Root) {
		if os.Remove(p.layout.BinLink) == nil {
			removed = append(removed, p.layout.BinLink)
		}
	}
	if sf.App != "" {
		if appRunning(h, sf.App) {
			kept = append(kept, sf.App+" (it is running; quit it and delete it)")
		} else if os.RemoveAll(sf.App) == nil {
			removed = append(removed, sf.App)
		}
	}
	if _, err := os.Stat(p.layout.Releases); err == nil {
		if os.RemoveAll(p.layout.Root) == nil {
			removed = append(removed, p.layout.Root)
		}
	}
	// A restart never stops the terminals the daemon started, and neither
	// does this: they are the person's sessions.
	if sock := filepath.Join(p.stateDir, "tmux", "term.sock"); fileExists(sock) {
		if out, err := h.run("tmux", "-S", sock, "ls"); err == nil && strings.TrimSpace(string(out)) != "" {
			kept = append(kept, "the terminals the daemon started that are still open; they end when you exit them (tmux -S "+sock+" ls)")
		}
	}
	return removed, kept
}
