// Package config resolves where this daemon keeps its state and which ports it
// uses. Everything here is deliberately distinct from the Swift Clawdline's own
// identity so both can be installed and run at the same time: a different config
// directory, a different port, a different token file.
package config

import (
	"os"
	"path/filepath"
	"runtime"
)

const (
	// DefaultPort is this daemon's own port. The Swift app owns 7717.
	DefaultPort = 7727
	// DefaultUpstreamPort is the Swift app, which answers every route this
	// daemon has not taken over yet.
	DefaultUpstreamPort = 7717
	// AppDir is the per-user directory name. Not "clawdline": sharing it would
	// mean two writers over one orchestrator store.
	AppDir = "clawdline-next"
)

type Config struct {
	Port         int
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
		UpstreamPort: DefaultUpstreamPort,
		Dir:          Dir(),
		Host:         host,
	}
}

// Dir is the durable state root. Task exchange, receipts and the event store
// live under it — never under a temporary directory, which the capability
// matrix already records as a Linux blocker for the Swift app.
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
