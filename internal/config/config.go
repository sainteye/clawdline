// Package config resolves where this daemon keeps its state and which ports it
// uses. Everything here is deliberately distinct from the retired Swift
// Clawdline's own identity — a different config directory, a different port, a
// different token file — so that installing this one never wrote over that
// one's state, and so that a machine which still has both installed keeps them
// apart.
package config

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	// DefaultPort is this daemon's own port.
	DefaultPort = 7727
	// NoUpstream is the default, and it means there is nobody behind this
	// daemon: a route it has not taken over answers `501 not_implemented` and
	// names itself, rather than being forwarded to a port.
	//
	// It used to be 7717, the Swift app's port, from when this daemon ran in
	// front of that app and borrowed what it had not ported yet. The Swift app
	// was stopped on 2026-09-19 and removed from launch-at-login, so nothing
	// answers 7717 on this machine any more: with that default, an unported
	// route answered `502 upstream_unreachable` instead of saying its own
	// name. Forwarding is now something an operator asks for out loud.
	NoUpstream = 0
	// RetiredUpstreamPort is that port, kept as a name rather than as a
	// default so the number in the logs and in docs/cutover.md still has
	// something to point at. Nothing in this daemon reaches for it.
	RetiredUpstreamPort = 7717
	// UpstreamPortEnv names the port to forward unowned routes to. Unset, or
	// not a port, means NoUpstream. It exists for the one case that is left:
	// running this daemon in front of another that does answer.
	UpstreamPortEnv = "CLAWDLINE_NEXT_UPSTREAM_PORT"
	// AppDir is the per-user directory name. Not "clawdline": sharing it would
	// mean two writers over one orchestrator store.
	AppDir = "clawdline-next"
)

type Config struct {
	Port int
	// UpstreamPort is the port unowned routes are forwarded to, or NoUpstream
	// when there is none. Read it through Upstream, which answers the question
	// callers actually have.
	UpstreamPort int
	Dir          string
	WebRoot      string
	// Host is what the server binds to. Loopback by default, because a daemon
	// that owns terminals and credentials should not become reachable by
	// accident. Binding wider is a decision somebody makes out loud.
	Host string
}

// Load resolves the configuration from the environment, falling back to the
// platform's ordinary location.
func Load() Config {
	host := "127.0.0.1"
	if v := os.Getenv("CLAWDLINE_NEXT_HOST"); v != "" {
		host = v
	}
	return Config{
		Port:         DefaultPort,
		UpstreamPort: upstreamPort(),
		Dir:          Dir(),
		Host:         host,
	}
}

// Upstream is the port to forward a route this daemon does not own to, and
// whether there is one at all. No upstream is the ordinary case and is not a
// failure: it is what makes an unowned route answer by name.
func (c Config) Upstream() (int, bool) {
	if c.UpstreamPort <= 0 {
		return 0, false
	}
	return c.UpstreamPort, true
}

// upstreamPort reads UpstreamPortEnv. A value that is not a port is said once
// in the log and leaves the default standing, because a misspelt port must not
// look like one that worked — the same rule the legacy-store switch follows.
func upstreamPort() int {
	v := strings.TrimSpace(os.Getenv(UpstreamPortEnv))
	if v == "" {
		return NoUpstream
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 || n > 65535 {
		log.Printf("config: %s=%q is not a port; nothing is forwarded and an unowned route answers by name", UpstreamPortEnv, v)
		return NoUpstream
	}
	return n
}

// Dir is the durable state root. Task exchange, receipts and the event store
// live under it — never under a temporary directory, which the capability
// matrix recorded as a Linux blocker for the Swift app.
func Dir() string {
	if v := os.Getenv("CLAWDLINE_NEXT_DIR"); v != "" {
		return v
	}
	switch runtime.GOOS {
	case "windows":
		if v := os.Getenv("APPDATA"); v != "" {
			return filepath.Join(v, AppDir)
		}
	case "darwin", "linux":
		if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
			return filepath.Join(v, AppDir)
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return AppDir
	}
	return filepath.Join(home, ".config", AppDir)
}
