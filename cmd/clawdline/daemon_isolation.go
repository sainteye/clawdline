package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/sainteye/clawdline/internal/config"
)

func isolatedDaemonConfigError() error {
	return errors.New(cliCopy("entry", "main_isolated_daemon_config",
		"Set CLAWDLINE_NEXT_DIR to a separate, existing absolute test directory and CLAWDLINE_NEXT_PORT to an unused port other than 7727."))
}

// Usage, live identity and on-demand history resolve their provider paths
// independently. Set their shared process environment before starting any
// daemon components or provider commands.
func prepareIsolatedDaemon() error {
	stateDir := os.Getenv("CLAWDLINE_NEXT_DIR")
	if !filepath.IsAbs(stateDir) || os.Getenv("CLAWDLINE_NEXT_PORT") == "" {
		return isolatedDaemonConfigError()
	}
	port, err := daemonPort()
	if err != nil {
		return err
	}
	if port == config.DefaultPort {
		return isolatedDaemonConfigError()
	}
	stateDir, err = filepath.EvalSymlinks(stateDir)
	if err != nil {
		return err
	}
	defaultDir := config.DefaultDir()
	if resolved, err := filepath.EvalSymlinks(defaultDir); err == nil {
		defaultDir = resolved
	}
	if rel, err := filepath.Rel(defaultDir, stateDir); err == nil &&
		rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return isolatedDaemonConfigError()
	}
	providerHome, err := os.MkdirTemp(stateDir, "isolated-home-")
	if err != nil {
		return err
	}
	for _, key := range []string{
		"CLAUDE_CONFIG_DIR", "CODEX_HOME", "GEMINI_HOME", "GEMINI_CLI_HOME",
		"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME",
		"APPDATA", "LOCALAPPDATA", "CLAWDLINE_NEXT_INSTALL_ROOT", "CLAWDLINE_NEXT_BIN_DIR", "TMUX",
	} {
		if err := os.Unsetenv(key); err != nil {
			return err
		}
	}
	for key, value := range map[string]string{
		"HOME":        providerHome,
		"USERPROFILE": providerHome,
		"TMUX_TMPDIR": stateDir,
	} {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}
