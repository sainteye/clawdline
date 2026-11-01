// Package install is where an installed Clawdline lives on disk, and what kind
// of installation the running binary belongs to. The installer
// (`clawdline setup`), the updater and the Linux source deploy
// (tools/deploy-linux-user.sh) share this one layout, so a machine has one
// `current`, one service, and one place to roll back to (docs/updates.md).
//
//	<root>/releases/v0.10.0/{clawdline,dist/}       a release
//	<root>/releases/<40-hex commit>/{clawdline,dist/} a source deploy
//	<root>/current -> releases/<one of them>
//	<root>/staging/                                  downloads being checked
//	~/.local/bin/clawdline -> <root>/current/clawdline
//
// <root> is ~/.local/share/clawdline-next ($XDG_DATA_HOME/clawdline-next), or
// CLAWDLINE_NEXT_INSTALL_ROOT, which tests and `setup --prefix` use so an
// install under test never touches the one the person runs.
package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/release"
)

// DaemonBinary distinguishes the macOS background service from the public CLI.
const DaemonBinary = "clawdline-daemon"

// Layout is one installation's paths.
type Layout struct {
	Root     string
	Releases string
	Current  string
	Staging  string
	// ServiceBin is a real, stable path for the macOS LaunchAgent. It is a
	// hard link to the selected release, never a symlink through current: TCC
	// records an unbundled executable by its resolved path.
	ServiceBin string
	// BinLink is the `clawdline` on PATH.
	BinLink string
}

// NewLayout is the layout under root, with the PATH link in binDir.
func NewLayout(root, binDir string) Layout {
	return Layout{
		Root:       root,
		Releases:   filepath.Join(root, "releases"),
		Current:    filepath.Join(root, "current"),
		Staging:    filepath.Join(root, "staging"),
		ServiceBin: filepath.Join(root, "service", DaemonBinary),
		BinLink:    filepath.Join(binDir, "clawdline"),
	}
}

// DefaultLayout is this user's installation.
func DefaultLayout() (Layout, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Layout{}, err
	}
	root := os.Getenv("CLAWDLINE_NEXT_INSTALL_ROOT")
	if root == "" {
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
		root = filepath.Join(data, "clawdline-next")
	}
	bin := os.Getenv("CLAWDLINE_NEXT_BIN_DIR")
	if bin == "" {
		bin = filepath.Join(home, ".local", "bin")
	}
	return NewLayout(root, bin), nil
}

// ReleaseDir is where version is unpacked.
func (l Layout) ReleaseDir(version string) string { return filepath.Join(l.Releases, version) }

// Kind is what kind of installation something is.
type Kind string

const (
	// KindRelease: `current` is a signed release, vX.Y.Z; the updater owns it.
	KindRelease Kind = "release"
	// KindSourceDeploy: `current` is a commit deployed from a source checkout;
	// it keeps following the hosted console's BUILD.json.
	KindSourceDeploy Kind = "source_deploy"
	// KindSourceCheckout: the binary runs from somewhere outside the layout,
	// a checkout's bin/ or a dev-built app bundle.
	KindSourceCheckout Kind = "source_checkout"
	// KindNone: nothing is installed in the layout.
	KindNone Kind = "none"
)

var commitName = regexp.MustCompile(`^[0-9a-f]{40}$`)

// kindOfRelease names a releases/ entry: a version or a commit.
func kindOfRelease(name string) Kind {
	if _, err := release.ParseVersion(name); err == nil {
		return KindRelease
	}
	if commitName.MatchString(name) {
		return KindSourceDeploy
	}
	return KindNone
}

// CurrentRelease is the releases/ entry `current` points at, and its kind.
// KindNone when there is no `current` or it points elsewhere.
func (l Layout) CurrentRelease() (string, Kind) {
	target, err := os.Readlink(l.Current)
	if err != nil {
		return "", KindNone
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(l.Root, target)
	}
	if filepath.Dir(filepath.Clean(target)) != filepath.Clean(l.Releases) {
		return "", KindNone
	}
	name := filepath.Base(target)
	return name, kindOfRelease(name)
}

// KindOf is the kind of installation the executable at exe belongs to: a
// release or source deploy when it runs from this layout's releases (directly
// or through `current`), a source checkout otherwise.
func (l Layout) KindOf(exe string) Kind {
	if name, kind := l.ServiceRelease(exe); name != "" {
		return kind
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	releases := l.Releases
	if resolved, err := filepath.EvalSymlinks(releases); err == nil {
		releases = resolved
	}
	rel, err := filepath.Rel(releases, exe)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return KindSourceCheckout
	}
	first := strings.SplitN(rel, string(filepath.Separator), 2)[0]
	if k := kindOfRelease(first); k != KindNone {
		return k
	}
	return KindSourceCheckout
}

// ServiceRelease identifies the release whose binary is hard-linked at the
// stable service path. The inode comparison keeps rollback truthful even when
// current has switched but the supervisor has not installed the next link.
func (l Layout) ServiceRelease(exe string) (string, Kind) {
	if filepath.Clean(exe) != filepath.Clean(l.ServiceBin) && filepath.Clean(exe) != filepath.Clean(l.LegacyServiceBin()) {
		return "", KindNone
	}
	service, err := os.Stat(exe)
	if err != nil {
		return "", KindNone
	}
	current, _ := l.CurrentRelease()
	if current != "" {
		if l.serviceMatches(service, current, exe) {
			return current, kindOfRelease(current)
		}
	}
	entries, err := os.ReadDir(l.Releases)
	if err != nil {
		return "", KindNone
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == current || kindOfRelease(name) == KindNone {
			continue
		}
		if l.serviceMatches(service, name, exe) {
			return name, kindOfRelease(name)
		}
	}
	return "", KindNone
}

// LegacyServiceBin remains available for rollback to releases predating the name.
func (l Layout) LegacyServiceBin() string { return filepath.Join(l.Root, "service", "clawdline") }

// ServiceExecutable selects the name actually shipped by a release. A legacy
// release keeps its original path and signing identity when rolled back.
func (l Layout) ServiceExecutable(name string) string {
	if info, err := os.Lstat(filepath.Join(l.ReleaseDir(name), DaemonBinary)); err == nil && info.Mode().IsRegular() {
		return l.ServiceBin
	}
	return l.LegacyServiceBin()
}

func (l Layout) serviceMatches(service os.FileInfo, name, exe string) bool {
	binary := "clawdline"
	if filepath.Clean(exe) == filepath.Clean(l.ServiceBin) {
		binary = DaemonBinary
	}
	candidate, err := os.Stat(filepath.Join(l.ReleaseDir(name), binary))
	return err == nil && os.SameFile(service, candidate)
}

// InstallServiceBinary atomically selects the macOS daemon at a fixed real
// path. The release remains untouched and available for rollback. Hard links
// require the service and releases to be on the same filesystem, which they
// are under one installation root.
func (l Layout) InstallServiceBinary(name string) error {
	if kindOfRelease(name) == KindNone {
		return fmt.Errorf("%q is not a release or commit name", name)
	}
	named := filepath.Join(l.ReleaseDir(name), DaemonBinary)
	if info, err := os.Lstat(named); err == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0) {
		return fmt.Errorf("the named background service is not an executable regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.ServiceBin), 0o755); err != nil {
		return err
	}
	// The daemon discovers its console beside its executable. The stable
	// service path needs the current release's dist/ there as well, including
	// when an older updater switches to this release before it restarts.
	console := filepath.Join(filepath.Dir(l.ServiceBin), "dist")
	const consoleTarget = "../current/dist"
	if err := os.Symlink(consoleTarget, console); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	target, err := os.Readlink(console)
	if err != nil {
		return fmt.Errorf("service console path %s: %w", console, err)
	}
	if target != consoleTarget {
		return fmt.Errorf("service console path %s links to %q, want %q", console, target, consoleTarget)
	}
	destination := l.ServiceExecutable(name)
	source := "clawdline"
	if destination == l.ServiceBin {
		source = DaemonBinary
	}
	tmp := destination + ".new"
	_ = os.Remove(tmp)
	if err := os.Link(filepath.Join(l.ReleaseDir(name), source), tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, destination); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// SwitchCurrent points `current` at releases/<name> in one rename, so a
// reader sees the old target or the new one and never neither.
func (l Layout) SwitchCurrent(name string) error {
	if kindOfRelease(name) == KindNone {
		return fmt.Errorf("%q is not a release or commit name", name)
	}
	if _, err := os.Stat(filepath.Join(l.ReleaseDir(name), "clawdline")); err != nil {
		return fmt.Errorf("release %s is not unpacked: %w", name, err)
	}
	tmp := l.Current + ".new"
	_ = os.Remove(tmp)
	if err := os.Symlink(filepath.Join("releases", name), tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, l.Current); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ServiceFile is what `setup` writes into the state directory when it
// installs the daemon as a per-user service. The macOS app reads it to know
// it must not start its bundled daemon (service mode), and the updater reads
// it to know what to restart.
type ServiceFile struct {
	// Supervisor is "systemd" or "launchd".
	Supervisor string `json:"supervisor"`
	// Name is the systemd unit or the launchd label.
	Name string `json:"name"`
	// Domain is the launchd domain the job was bootstrapped into
	// (gui/<uid> or user/<uid>); empty for systemd.
	Domain string `json:"domain,omitempty"`
	Port   int    `json:"port"`
	// Root is the layout root the service runs from.
	Root string `json:"root"`
	// File is the unit or plist setup wrote, so the uninstaller and the
	// app's start-at-login switch need not compose its path.
	File string `json:"file,omitempty"`
	// Channel is the release channel the person chose (stable or beta).
	Channel string `json:"channel,omitempty"`
	// App is the app bundle setup installed, when it installed one; the
	// uninstaller removes only that one.
	App string `json:"app,omitempty"`
}

// ServiceFileName is its name in the state directory.
const ServiceFileName = "service.json"

// ReadServiceFile reads stateDir/service.json; os.ErrNotExist when the daemon
// is not installed as a service.
func ReadServiceFile(stateDir string) (ServiceFile, error) {
	b, err := os.ReadFile(filepath.Join(stateDir, ServiceFileName))
	if err != nil {
		return ServiceFile{}, err
	}
	var s ServiceFile
	if err := json.Unmarshal(b, &s); err != nil {
		return ServiceFile{}, fmt.Errorf("%s: %w", ServiceFileName, err)
	}
	if s.Supervisor != "systemd" && s.Supervisor != "launchd" {
		return ServiceFile{}, fmt.Errorf("%s names unknown supervisor %q", ServiceFileName, s.Supervisor)
	}
	return s, nil
}

// WriteServiceFile writes it atomically.
func WriteServiceFile(stateDir string, s ServiceFile) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(stateDir, ServiceFileName+".tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(stateDir, ServiceFileName))
}

// RemoveServiceFile is the uninstaller's: the app goes back to running its
// own daemon.
func RemoveServiceFile(stateDir string) error {
	err := os.Remove(filepath.Join(stateDir, ServiceFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
