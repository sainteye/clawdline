// Package grok identifies the official coding CLI without reading credentials.
package grok

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
)

// The shared resolver bounds commands, output, caching and contention. Native
// initialize additionally checks Grok's protocol fingerprint before any prompt.
func NewResolver(candidates []string, run antigravity.Runner) *antigravity.Resolver {
	return antigravity.NewResolverWithFingerprint(candidates, run, func(version, help string) bool {
		return strings.HasPrefix(version, "grok ") && strings.Contains(help, "Grok Build") &&
			strings.Contains(help, "--permission-mode") && strings.Contains(help, "--output-format") &&
			strings.Contains(help, "agent") && !strings.Contains(help, "--install-extension")
	})
}

func DefaultCandidates() []string {
	home, _ := os.UserHomeDir()
	paths := []string{filepath.Join(home, ".grok", "bin", "grok"), filepath.Join(home, ".local", "bin", "grok")}
	if path, err := exec.LookPath("grok"); err == nil {
		paths = append(paths, path)
	}
	return append(paths, "/opt/homebrew/bin/grok", "/usr/local/bin/grok")
}

func AccountDir() string {
	if dir := os.Getenv("GROK_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".grok")
}
