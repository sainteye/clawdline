// Package grok identifies the official coding CLI without reading credentials.
package grok

import (
	"context"
	"encoding/json"
	"io"
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

// Renew asks the official CLI to renew an expired access token with its saved
// refresh token. `grok models` signs in silently before listing models (its log
// records "oidc refresh ... PreRequest"), takes the CLI's own auth.json lock and
// sends no prompt. Clawdline neither reads nor writes the refresh token here.
func Renew(ctx context.Context, path string) error {
	command := exec.CommandContext(ctx, path, "models")
	command.Dir = os.TempDir() // No project configuration is discovered.
	command.Stdin = nil
	command.Stdout, command.Stderr = io.Discard, io.Discard
	return command.Run()
}

// ACPIdentity identifies the native server and its noninteractive sign-in offer.
func ACPIdentity(raw []byte) (valid, cachedToken bool) {
	var identity struct {
		Meta struct {
			Shell   bool   `json:"grokShell"`
			Version string `json:"agentVersion"`
		} `json:"_meta"`
		Auth []struct {
			ID string `json:"id"`
		} `json:"authMethods"`
	}
	if json.Unmarshal(raw, &identity) != nil || !identity.Meta.Shell || identity.Meta.Version == "" {
		return false, false
	}
	for _, method := range identity.Auth {
		if method.ID == "cached_token" {
			return true, true
		}
	}
	return true, false
}

// PersonalAccountProblem uses only the native auth result's billing and gate
// metadata. The provider body is never included in a refusal.
func PersonalAccountProblem(raw []byte) string {
	var proof struct {
		Meta struct {
			Mode          string          `json:"auth_mode"`
			BackendBilled *bool           `json:"backend_billed"`
			Gate          json.RawMessage `json:"gate"`
		} `json:"_meta"`
	}
	if json.Unmarshal(raw, &proof) != nil || proof.Meta.Mode != "Oidc" || proof.Meta.BackendBilled == nil || *proof.Meta.BackendBilled {
		return "auth_mode_unsupported"
	}
	if len(proof.Meta.Gate) > 0 && string(proof.Meta.Gate) != "null" {
		return "account_restricted"
	}
	return ""
}
