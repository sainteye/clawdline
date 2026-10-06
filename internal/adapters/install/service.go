package install

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// The per-user service `setup` installs: one systemd --user unit on Linux, one
// LaunchAgent on macOS. Both run `<root>/current/clawdline serve`, so a switch
// of `current` followed by a restart is an update, and neither carries any
// CLAWDLINE_NEXT_* the caller happened to have: only what setup resolved.

const (
	// DefaultPort is the daemon's port when nothing names another
	// (internal/config DefaultPort).
	DefaultPort = 7727
	// UnitBase and LabelBase are the real install's names.
	UnitBase  = "clawdline-next"
	LabelBase = "com.sainteye.clawdline-next"
)

// ServiceSuffix keeps a test install from colliding with the real one: empty
// for the real install (default port, default root), otherwise the port when
// it is not the default and a short hash of the root when the root was named.
func ServiceSuffix(port int, root string, rootNamed bool) string {
	var parts []string
	if port != DefaultPort {
		parts = append(parts, strconv.Itoa(port))
	}
	if rootNamed {
		sum := sha256.Sum256([]byte(filepath.Clean(root)))
		parts = append(parts, hex.EncodeToString(sum[:4]))
	}
	if len(parts) == 0 {
		return ""
	}
	return "-" + strings.Join(parts, "-")
}

// UnitName is the systemd unit for suffix.
func UnitName(suffix string) string { return UnitBase + suffix + ".service" }

// LaunchdLabel is the LaunchAgent label for suffix.
func LaunchdLabel(suffix string) string {
	if suffix == "" {
		return LabelBase
	}
	return LabelBase + "." + strings.TrimPrefix(suffix, "-")
}

// ServiceSpec is everything a generated unit or plist says.
type ServiceSpec struct {
	// Exec is the daemon binary: <root>/current/clawdline.
	Exec string
	Port int
	// Path and Shell are what was captured from the person's login shell.
	Path  string
	Shell string
	Home  string
	// StateDir and InstallRoot are written only when they are not the
	// defaults, so the real install's unit names neither.
	StateDir    string
	InstallRoot string
	// Autostart: start at login / boot. Off, the service is started now and
	// only by hand afterwards.
	Autostart bool
	// LogPath is where launchd sends what the daemon prints before its own
	// log is open. Unused by systemd, which has the journal.
	LogPath string
}

// env is the service's environment, in a stable order.
func (s ServiceSpec) env() [][2]string {
	out := [][2]string{
		{"HOME", s.Home},
		{"PATH", s.Path},
		{"SHELL", s.Shell},
		{"CLAWDLINE_NEXT_PORT", strconv.Itoa(s.Port)},
		{"CLAWDLINE_NEXT_STANDALONE", "1"},
		{"CLAWDLINE_NEXT_OWN_SESSIONS", "1"},
	}
	if s.StateDir != "" {
		out = append(out, [2]string{"CLAWDLINE_NEXT_DIR", s.StateDir})
	}
	if s.InstallRoot != "" {
		out = append(out, [2]string{"CLAWDLINE_NEXT_INSTALL_ROOT", s.InstallRoot})
	}
	return out
}

// systemdQuote quotes a value for an Environment= line.
func systemdQuote(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "$", "$$")
	return `"` + r.Replace(v) + `"`
}

// SystemdUnit is the unit text. It keeps the shape of
// tools/systemd/clawdline-next.service, KillMode=process above all: tmux and
// the assistants it owns are in the unit's cgroup, and only the daemon is
// stopped on a restart.
func SystemdUnit(s ServiceSpec) string {
	var b strings.Builder
	b.WriteString("# Written by `clawdline setup`; running setup again rewrites it.\n")
	b.WriteString("[Unit]\nDescription=Clawdline\nAfter=network-online.target\nWants=network-online.target\n\n")
	b.WriteString("[Service]\nType=simple\n")
	for _, kv := range s.env() {
		fmt.Fprintf(&b, "Environment=%s=%s\n", kv[0], systemdQuote(kv[1]))
	}
	fmt.Fprintf(&b, "ExecStart=%s serve\n", systemdExecQuote(s.Exec))
	b.WriteString("Restart=on-failure\nRestartSec=3\n")
	b.WriteString("# tmux and the assistants it owns remain alive while only the daemon restarts.\n")
	b.WriteString("KillMode=process\nUMask=0077\n\n")
	b.WriteString("[Install]\nWantedBy=default.target\n")
	return b.String()
}

func systemdExecQuote(path string) string {
	if strings.ContainsAny(path, " \t\"'\\%$") {
		return systemdQuote(path)
	}
	return path
}

func plistEscape(v string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(v)
}

// LaunchdPlist is the LaunchAgent text. AbandonProcessGroup keeps the tmux
// server the daemon starts alive when launchd stops the daemon, the
// counterpart of KillMode=process; ThrottleInterval 30 keeps a daemon that
// cannot bind its port (another app holds it) from restarting every ten
// seconds.
//
// With Autostart it is RunAtLoad and KeepAlive. Without it, KeepAlive is only
// for a crash: a KeepAlive of true, or one keyed on SuccessfulExit, starts the
// job whenever it is loaded, which is every login.
func LaunchdPlist(label string, s ServiceSpec) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- Written by clawdline setup; running setup again rewrites it. -->
<plist version="1.0">
<dict>
`)
	fmt.Fprintf(&b, "\t<key>Label</key>\n\t<string>%s</string>\n", plistEscape(label))
	fmt.Fprintf(&b, "\t<key>ProgramArguments</key>\n\t<array>\n\t\t<string>%s</string>\n\t\t<string>serve</string>\n\t</array>\n", plistEscape(s.Exec))
	b.WriteString("\t<key>EnvironmentVariables</key>\n\t<dict>\n")
	for _, kv := range s.env() {
		fmt.Fprintf(&b, "\t\t<key>%s</key>\n\t\t<string>%s</string>\n", kv[0], plistEscape(kv[1]))
	}
	b.WriteString("\t</dict>\n")
	if s.Autostart {
		b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n\t<key>KeepAlive</key>\n\t<true/>\n")
	} else {
		b.WriteString("\t<key>RunAtLoad</key>\n\t<false/>\n\t<key>KeepAlive</key>\n\t<dict>\n\t\t<key>Crashed</key>\n\t\t<true/>\n\t</dict>\n")
	}
	b.WriteString("\t<key>ThrottleInterval</key>\n\t<integer>30</integer>\n")
	b.WriteString("\t<key>AbandonProcessGroup</key>\n\t<true/>\n")
	b.WriteString("\t<key>ProcessType</key>\n\t<string>Interactive</string>\n")
	if s.LogPath != "" {
		fmt.Fprintf(&b, "\t<key>StandardOutPath</key>\n\t<string>%s</string>\n", plistEscape(s.LogPath))
		fmt.Fprintf(&b, "\t<key>StandardErrorPath</key>\n\t<string>%s</string>\n", plistEscape(s.LogPath))
	}
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}
