package terminal

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
)

// What this machine's terminals can do, asked without doing any of it
// (docs/broker-design.md #43): reading a session's screen and typing into it,
// each by name, each with the backends that provide it here and, when none
// does, why not.
//
// Every probe is a read. tmux is looked up on the PATH the tmux backend runs
// it from; iTerm2 is asked of Launch Services, which never starts it. Nothing
// is opened, typed or listed.

// backendReach is one backend's answer to "could a session be driven through
// you right now".
type backendReach struct {
	name   string
	state  ports.CapabilityState
	reason string
}

var _ ports.CapabilityHost = Launcher{}

// Capabilities is read_screen and send_keys on this machine.
func (l Launcher) Capabilities(ctx context.Context) ports.Capabilities {
	return terminalCapabilities(runtime.GOOS, l.reaches(ctx))
}

// reaches asks each backend this platform has.
func (l Launcher) reaches(ctx context.Context) []backendReach {
	out := []backendReach{l.tmuxReach()}
	if runtime.GOOS == "darwin" {
		out = append(out, l.itermReach(ctx))
	}
	return out
}

// tmuxReach looks for the binary the tmux backend itself runs, which is
// `tmux` on this process's PATH (Tmux.run). The launcher also tries the places
// package managers put tmux (Launcher.binary), and when only it finds one the
// two disagree: a child's tab would open and the briefing typed into it would
// fail. That is named rather than reported as either answer.
func (l Launcher) tmuxReach() backendReach {
	if l.Tmux == nil {
		return backendReach{name: "tmux", state: ports.CapabilityUnavailable, reason: "tmux is not installed"}
	}
	found, onPath := l.Tmux.binary()
	if onPath {
		return backendReach{name: "tmux", state: ports.CapabilityAvailable}
	}
	if found != "" {
		return backendReach{name: "tmux", state: ports.CapabilityUnavailable,
			reason: tmuxOutsidePATHReason(found)}
	}
	return backendReach{name: "tmux", state: ports.CapabilityUnavailable, reason: "tmux is not installed"}
}

// itermReach is whether iTerm2 is running: its sessions are read and typed
// into through Apple Events, and a closed iTerm2 has no sessions to drive.
func (l Launcher) itermReach(ctx context.Context) backendReach {
	running, err := l.ITermRunning(ctx)
	switch {
	case err != nil:
		return backendReach{name: "iterm", state: ports.CapabilityUnknown,
			reason: "whether iTerm2 is running could not be read: " + err.Error()}
	case running:
		return backendReach{name: "iterm", state: ports.CapabilityAvailable}
	}
	return backendReach{name: "iterm", state: ports.CapabilityUnavailable, reason: "iTerm2 is not running"}
}

// terminalCapabilities is the two answers from the backends' reaches. It reads
// nothing, so every platform's answer can be checked on any one of them.
func terminalCapabilities(goos string, reaches []backendReach) ports.Capabilities {
	one := func(name ports.CapabilityName) ports.Capability {
		var via, unknown, absent []string
		for _, r := range reaches {
			switch r.state {
			case ports.CapabilityAvailable:
				via = append(via, r.name)
			case ports.CapabilityUnknown:
				unknown = append(unknown, r.reason)
			default:
				absent = append(absent, r.reason)
			}
		}
		switch {
		case len(via) > 0:
			// The backends that cannot are said too: a child opens in one
			// backend, and "available through iTerm2" is no answer for a
			// child that would open in tmux (orchestrator capability.go).
			reason := "through " + strings.Join(via, " and ")
			if len(absent)+len(unknown) > 0 {
				reason += "; not otherwise: " + strings.Join(append(absent, unknown...), "; ")
			}
			return ports.Capability{Name: name, State: ports.CapabilityAvailable, Via: via, Reason: reason}
		case len(unknown) > 0:
			return ports.Capability{Name: name, State: ports.CapabilityUnknown,
				Reason: strings.Join(append(unknown, absent...), "; ")}
		}
		// The platform's own absences are said too, so that a reader on Linux
		// knows installing tmux is the whole answer and one on Windows knows
		// it is not.
		switch goos {
		case "darwin":
		case "windows":
			absent = append(absent, "windows has no iTerm2, and a console this daemon owns (ConPTY) is not built yet")
		default:
			absent = append(absent, goos+" has no iTerm2")
		}
		return ports.Capability{Name: name, State: ports.CapabilityUnavailable, Reason: strings.Join(absent, "; ")}
	}
	return ports.Capabilities{one(ports.CapReadScreen), one(ports.CapSendKeys)}
}

// The `terminal` capability: an ordinary shell a person types into, on the
// tmux server this daemon owns (internal/adapters/terminal/owned).
//
// **3.0 is the floor**, from tmux's own CHANGES: `send-keys -H`, which every
// keystroke goes through (keys.go), is listed under "CHANGES FROM 2.9 TO
// 3.0" ("New -H flag to send-keys to send literal keys"), and
// `resize-window`, the only way a terminal is sized, under "CHANGES FROM 2.8
// TO 2.9". The later cursor formats (`cursor_shape`, `cursor_blinking`) are
// not needed: a tmux without them draws the default cursor.
const (
	TmuxMinimumMajor = 3
	TmuxMinimumMinor = 0
)

// The codes a `terminal` capability that is not available carries.
const (
	CodeTmuxNotInstalled = "tmux_not_installed"
	CodeTmuxTooOld       = "tmux_too_old"
	CodeNoBackend        = "no_backend"
	CodeTmuxUnread       = "tmux_version_unread"
)

// ParseTmuxVersion reads `tmux -V`: `tmux 3.6a`, `tmux 3.4`, `tmux next-3.7`,
// `tmux openbsd-7.5`. A build that names no number (`tmux master`) is not
// read, which is not the same as too old.
func ParseTmuxVersion(out string) (major, minor int, ok bool) {
	word := strings.TrimSpace(out)
	if i := strings.LastIndexByte(word, ' '); i >= 0 {
		word = word[i+1:]
	}
	if i := strings.LastIndexByte(word, '-'); i >= 0 {
		if strings.HasPrefix(word, "openbsd-") {
			// OpenBSD's base tmux is numbered after the OS, not tmux, and has
			// had `send-keys -H` since long before 7.0.
			return TmuxMinimumMajor, TmuxMinimumMinor, true
		}
		word = word[i+1:]
	}
	num := func(s string) (int, string, bool) {
		n, i := 0, 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			n = n*10 + int(s[i]-'0')
			i++
		}
		return n, s[i:], i > 0
	}
	major, rest, ok := num(word)
	if !ok || !strings.HasPrefix(rest, ".") {
		return 0, 0, false
	}
	minor, _, ok = num(rest[1:])
	return major, minor, ok
}

// TmuxNewEnough is whether a version is at or above the floor.
func TmuxNewEnough(major, minor int) bool {
	return major > TmuxMinimumMajor || (major == TmuxMinimumMajor && minor >= TmuxMinimumMinor)
}

// ownedTerminalCapability decides the answer from what was read, so every
// platform's answer can be checked on any one of them. `found` is tmux on the
// PATH; `version` is what `tmux -V` printed, and `versionErr` its failure.
func ownedTerminalCapability(goos string, found bool, version string, versionErr error) ports.Capability {
	c := ports.Capability{Name: ports.CapTerminal}
	switch {
	case goos == "windows":
		c.State, c.Code = ports.CapabilityUnavailable, CodeNoBackend
		c.Reason = "windows has no tmux, and a console this daemon owns (ConPTY) is not built yet"
	case !found:
		c.State, c.Code = ports.CapabilityUnavailable, CodeTmuxNotInstalled
		c.Reason = "tmux is not installed; " + tmuxInstallHint(goos)
	case versionErr != nil:
		c.State, c.Code = ports.CapabilityUnknown, CodeTmuxUnread
		c.Reason = "tmux's version could not be read: " + versionErr.Error()
	default:
		major, minor, ok := ParseTmuxVersion(version)
		switch {
		case !ok:
			c.State, c.Code = ports.CapabilityUnknown, CodeTmuxUnread
			c.Reason = fmt.Sprintf("tmux's version could not be read from %q", strings.TrimSpace(version))
		case !TmuxNewEnough(major, minor):
			c.State, c.Code = ports.CapabilityUnavailable, CodeTmuxTooOld
			c.Reason = fmt.Sprintf("%s is older than tmux %d.%d; %s", strings.TrimSpace(version),
				TmuxMinimumMajor, TmuxMinimumMinor, tmuxInstallHint(goos))
		default:
			c.State, c.Via = ports.CapabilityAvailable, []string{"tmux"}
			c.Reason = "through a tmux server of this daemon's own (" + strings.TrimSpace(version) + ")"
		}
	}
	return c
}

func tmuxInstallHint(goos string) string {
	if goos == "darwin" {
		return "install it with `brew install tmux`"
	}
	return "install tmux 3.0 or later from this system's package manager"
}

// OwnedTerminalCapability is the `terminal` capability on this machine: a
// PATH lookup and one `tmux -V`, which starts no server.
func OwnedTerminalCapability(ctx context.Context) ports.Capability {
	if runtime.GOOS == "windows" {
		return ownedTerminalCapability(runtime.GOOS, false, "", nil)
	}
	path, _ := FindTmux()
	if path == "" {
		return ownedTerminalCapability(runtime.GOOS, false, "", nil)
	}
	version, err := TmuxVersion(ctx, path)
	return ownedTerminalCapability(runtime.GOOS, true, version, err)
}

// TmuxVersion is what `tmux -V` prints, asked with a two-second ceiling.
func TmuxVersion(ctx context.Context, binary string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "-V").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
