// Package antigravity identifies the standalone CLI without accessing accounts.
package antigravity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const CLIProbeOutputLimit = 64 << 10
const CLIProbeTimeLimit = 3 * time.Second

type AuthState string

const (
	AuthUnknown            AuthState = "unknown"
	AuthRequired           AuthState = "required"
	AuthReady              AuthState = "ready"
	AuthUnsupportedAccount AuthState = "unsupported_account"
)

// Installation distinguishes a missing executable from an incompatible IDE.
// Finding an executable never establishes authentication or interactive readiness.
type Installation struct {
	InstalledKnown bool
	Installed      bool
	Compatible     bool
	Path           string
	Version        string
	AuthState      AuthState
	Reason         string
}

// Runner executes only the two local fingerprint commands, with a caller deadline.
type Runner func(context.Context, string, string) ([]byte, error)

// Resolver keeps one snapshot, invalidated when candidate file metadata changes.
// Its mutex owns the snapshot and the probe. Contending polls return unknown
// rather than queuing or duplicating commands. It owns no goroutine; commands
// inherit the bounded probe context.
type Resolver struct {
	mu           sync.Mutex
	candidates   []string
	run          Runner
	signature    string
	known        bool
	installation Installation
}

func NewResolver(candidates []string, run Runner) *Resolver {
	if run == nil {
		run = runProbe
	}
	return &Resolver{candidates: append([]string(nil), candidates...), run: run}
}

// DefaultCandidates prioritizes the standalone install location over PATH,
// which can contain the unrelated editor launcher. No shell/PATH is modified.
func DefaultCandidates() []string {
	home, _ := os.UserHomeDir()
	paths := []string{filepath.Join(home, ".local", "bin", "agy")}
	if path, err := exec.LookPath("agy"); err == nil {
		paths = append(paths, path)
	}
	return append(paths, "/opt/homebrew/bin/agy", "/usr/local/bin/agy")
}

func safePath(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	for _, r := range path {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// Snapshot performs no vendor/account/network operation. Only a changed local
// candidate executes --version/--help; failed probes are cached too.
func (r *Resolver) Snapshot(parent context.Context) Installation {
	if !r.mu.TryLock() {
		return Installation{AuthState: AuthUnknown, Reason: "provider_cli_busy"}
	}
	defer r.mu.Unlock()
	if parent.Err() != nil {
		return Installation{AuthState: AuthUnknown, Reason: "provider_cli_unreadable"}
	}
	var signature strings.Builder
	for _, path := range r.candidates {
		if !safePath(path) {
			fmt.Fprintf(&signature, "%q:unsafe;", path)
			continue
		}
		st, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(&signature, "%q:%v;", path, err)
			continue
		}
		canonical, _ := filepath.EvalSymlinks(path)
		fmt.Fprintf(&signature, "%q:%q:%d:%d:%d;", path, canonical, st.Size(), st.ModTime().UnixNano(), st.Mode())
	}
	if r.known && signature.String() == r.signature {
		return r.installation
	}
	result := Installation{InstalledKnown: true, AuthState: AuthUnknown, Reason: "provider_cli_missing"}
	ctx, cancel := context.WithTimeout(parent, CLIProbeTimeLimit)
	defer cancel()
	for _, path := range r.candidates {
		if !safePath(path) {
			result.InstalledKnown = false
			result.Reason = "provider_executable_unsafe"
			continue
		}
		absolute, err := filepath.EvalSymlinks(path)
		if err != nil {
			if !os.IsNotExist(err) {
				result.InstalledKnown = false
				result.Reason = "provider_cli_unreadable"
			}
			continue
		}
		if !safePath(absolute) {
			result.InstalledKnown = false
			result.Reason = "provider_executable_unsafe"
			continue
		}
		st, err := os.Stat(absolute)
		if err != nil || !st.Mode().IsRegular() || st.Mode()&0111 == 0 {
			continue
		}
		result.Installed = true
		result.Reason = "provider_cli_incompatible"
		version, err := r.run(ctx, absolute, "--version")
		if err != nil || len(version) > CLIProbeOutputLimit {
			result.Reason = "provider_cli_unreadable"
			continue
		}
		// Pin the protocol actually measured. A future version needs fresh
		// compatibility evidence, not a successful executable lookup.
		if strings.TrimSpace(string(version)) != "1.4.5" {
			continue
		}
		help, err := r.run(ctx, absolute, "--help")
		if err != nil || len(help) > CLIProbeOutputLimit {
			result.Reason = "provider_cli_unreadable"
			continue
		}
		if !standaloneHelp(string(help)) {
			continue
		}
		result = Installation{InstalledKnown: true, Installed: true, Compatible: true, Path: absolute,
			Version: "1.4.5", AuthState: AuthUnknown}
		break
	}
	// Cancellation is transient: a later ordinary caller must be able to retry.
	if ctx.Err() == nil {
		r.signature, r.installation, r.known = signature.String(), result, true
	} else {
		result.Compatible, result.Path, result.Version = false, "", ""
		result.Reason = "provider_cli_unreadable"
	}
	return result
}

func standaloneHelp(help string) bool {
	flags := map[string]bool{}
	for _, line := range strings.Split(help, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 {
			flags[fields[0]] = true
		}
	}
	for _, flag := range []string{"--conversation", "--prompt-interactive", "--model", "--effort", "--mode", "--input-format", "--output-format"} {
		if !flags[flag] {
			return false
		}
	}
	return !flags["--new-window"] && !flags["--install-extension"] &&
		strings.Contains(help, "low|medium|high|xhigh|max") && strings.Contains(help, "accept-edits, auto, plan")
}

// limitedOutput refuses overflow without retaining arbitrary process output.
type limitedOutput struct{ data []byte }

func (w *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > CLIProbeOutputLimit-len(w.data) {
		return 0, errors.New("provider_cli_output_too_large")
	}
	w.data = append(w.data, p...)
	return len(p), nil
}

func runProbe(ctx context.Context, path, flag string) ([]byte, error) {
	if flag != "--version" && flag != "--help" {
		return nil, errors.New("provider_probe_unsupported")
	}
	cmd := exec.CommandContext(ctx, path, flag)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	// The standalone is a single executable. Direct cancellation kills it;
	// WaitDelay also bounds pipe draining if a broken executable spawns a child.
	cmd.WaitDelay = CLIProbeTimeLimit
	output := &limitedOutput{}
	cmd.Stdout, cmd.Stderr = output, io.Discard
	err := cmd.Run()
	return output.data, err
}
