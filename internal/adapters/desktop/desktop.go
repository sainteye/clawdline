// Package desktop answers the desktop half of this machine's platform
// capabilities (docs/cross-platform.md §3.4, §5): the clipboard a send lends
// a picture to, a global hotkey, the notch island and starting at login.
//
// On macOS the last three belong to the native shell (shell/darwin), not to
// this daemon: it registers the hotkey, draws the island and holds the login
// item. Nothing on Linux or Windows does any of them yet, and each says so by
// name — with what this machine is, and what to do instead — rather than
// taking a setting nothing will ever act on.
//
// Every answer is decided from the operating system, the environment and the
// clipboard adapter's own word, so it reads nothing on disk and can be asked
// for every platform on any one of them.
package desktop

import (
	"context"
	"os"
	"runtime"

	"github.com/sainteye/clawdline/internal/app/ports"
)

// Host is this machine's desktop.
type Host struct {
	// GOOS is the operating system being answered for.
	GOOS string
	// Getenv reads the session's environment: a Wayland session and a
	// machine with no display at all get different sentences.
	Getenv func(string) string
	// Clipboard is whether the clipboard adapter can lend a picture — the
	// same answer the send path acts on (artifacts.Pasteboard.Available).
	Clipboard bool
}

var _ ports.CapabilityHost = Host{}

// New is this machine: its operating system, its environment, and the
// clipboard adapter's answer.
func New(clipboard bool) Host {
	return Host{GOOS: runtime.GOOS, Getenv: os.Getenv, Clipboard: clipboard}
}

// Capabilities is clipboard, global_hotkey, notch and launch_at_login.
func (h Host) Capabilities(ctx context.Context) ports.Capabilities {
	return ports.Capabilities{h.clipboard(), h.hotkey(), h.notch(), h.launchAtLogin()}
}

func (h Host) env(key string) string {
	if h.Getenv == nil {
		return ""
	}
	return h.Getenv(key)
}

func available(name ports.CapabilityName, via, reason string) ports.Capability {
	return ports.Capability{Name: name, State: ports.CapabilityAvailable, Via: []string{via}, Reason: reason}
}

func unavailable(name ports.CapabilityName, reason string) ports.Capability {
	return ports.Capability{Name: name, State: ports.CapabilityUnavailable, Reason: reason}
}

// clipboard is the picture a send puts on the clipboard. Where there is none a
// send still delivers the picture — as a path — so this is a degraded send,
// never a failed one (docs/cross-platform.md §4.3).
func (h Host) clipboard() ports.Capability {
	if h.Clipboard {
		return available(ports.CapClipboard, "pasteboard", "a send lends the picture to the pasteboard and gives back what was there")
	}
	return unavailable(ports.CapClipboard,
		"there is no clipboard on "+h.GOOS+" that Clawdline can lend a picture to; a send hands the assistant the picture's path instead")
}

// hotkey is StartPoints' global key. The macOS shell registers it with Carbon,
// which needs no permission to read other keys; the reason every other
// platform's answer is careful about the same thing is docs/cross-platform.md
// §2.2 — a program that opens a window must not be able to read every key.
func (h Host) hotkey() ports.Capability {
	const instead = "; bind a shortcut in your desktop's own settings to `clawdline open` instead"
	switch h.GOOS {
	case "darwin":
		return available(ports.CapGlobalHotkey, "shell", "the macOS shell registers it while it runs")
	case "windows":
		return unavailable(ports.CapGlobalHotkey,
			"RegisterHotKey needs a native shell with a message loop, and there is no Windows shell yet"+instead)
	}
	switch {
	case h.env("WAYLAND_DISPLAY") != "" || h.env("XDG_SESSION_TYPE") == "wayland":
		return unavailable(ports.CapGlobalHotkey,
			"this desktop is Wayland, which does not let a program take a key wherever the focus is"+instead)
	case h.env("DISPLAY") != "":
		return unavailable(ports.CapGlobalHotkey,
			"X11 could grab a key, but nothing on "+h.GOOS+" registers one yet"+instead)
	}
	return unavailable(ports.CapGlobalHotkey, "this session has no desktop (neither WAYLAND_DISPLAY nor DISPLAY is set)")
}

// notch is the island the macOS shell draws. Whether this Mac has a notch is
// its display's to say (NSScreen.safeAreaInsets), which the shell reads and
// this daemon cannot: on macOS the answer is unknown, not yes.
func (h Host) notch() ports.Capability {
	if h.GOOS == "darwin" {
		return ports.Capability{Name: ports.CapNotch, State: ports.CapabilityUnknown, Via: []string{"shell"},
			Reason: "the macOS shell draws it on a display that reports a notch; this daemon cannot read the display"}
	}
	return unavailable(ports.CapNotch,
		"there is no notch on "+h.GOOS+"; what the island shows — how many sessions are working and which one waits "+
			"for you — is on the console's session list")
}

// launchAtLogin is the login item. The macOS shell holds it (SMAppService,
// off until somebody turns it on); elsewhere it is docs/cross-platform.md
// §4.7. The daemon has no settings switch for it; the tracked Linux bootstrap
// installs the user unit and linger once so later releases need no root.
func (h Host) launchAtLogin() ports.Capability {
	switch h.GOOS {
	case "darwin":
		return available(ports.CapLaunchAtLogin, "shell", "the macOS shell's menu turns it on and off (SMAppService)")
	case "windows":
		return unavailable(ports.CapLaunchAtLogin,
			"nothing on windows registers a login item yet; a shortcut to `clawdline serve` in shell:startup does it by hand")
	}
	return unavailable(ports.CapLaunchAtLogin,
		"the daemon has no launch-at-login switch on "+h.GOOS+"; `tools/bootstrap-linux-user-service.sh` installs "+
			"its `systemd --user` unit and runs `loginctl enable-linger` once")
}
