package projects

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Recording that the person trusts a project folder, the way Claude Code
// records it when "Yes, I trust this folder" is chosen.
//
// Claude Code asks, on the first screen of an interactive session, whether a
// folder it has never been told about is to be trusted, and the highlighted
// answer is "No, exit". Nothing may press a key at that dialog (composer.go in
// the orchestrator), and unlike Codex (trustArgs) Claude Code has no flag that
// answers it for one run. So a Board item's "open a new Claude Code Session"
// on a project nobody had ever opened Claude Code in sat on that dialog for
// the briefing's whole 90 seconds and ended `assignment_failed` — measured on
// 2026-09-26 against Claude Code 2.1.283 in a project the person had
// registered and never trusted.
//
// The person decided (2026-09-26) that pressing that button for a project
// they registered is the answer to the question, so the answer is written
// where Claude Code keeps it: `projects[<dir>].hasTrustDialogAccepted` in
// `~/.claude.json` (or `$CLAUDE_CONFIG_DIR/.claude.json`). Only the project
// folder itself is written — never a disposable worktree, which is the reason
// Codex's answer is kept in memory instead.
//
// The file is Claude Code's, and running sessions rewrite it. Every write of
// theirs holds `<file>.lock`, a directory made with mkdir (proper-lockfile), so
// this takes the same lock, reads, changes the one key, and renames a whole new
// file into place. A lock that stays held is not broken: the dialog then shows
// as it did before, which is a refusal the person can read, not lost settings.

// ClaudeConfigPath is where Claude Code keeps its global config for this
// user: `$CLAUDE_CONFIG_DIR/.claude.json` when that is set, else
// `~/.claude.json`.
func ClaudeConfigPath() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, ".claude.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude.json"), nil
}

// ErrClaudeConfigMissing is a machine where Claude Code has never run: there
// is no config to add a folder to, and writing one would stand in for
// Claude Code's own first-run setup.
var ErrClaudeConfigMissing = errors.New("Claude Code has no config file on this machine yet")

// claudeLockWaits is how long, in total, the lock is waited for.
var claudeLockWaits = []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond,
	400 * time.Millisecond, 800 * time.Millisecond}

// TrustClaudeProject records dir as trusted in the Claude Code config at
// config. It answers whether it wrote anything: a folder already trusted —
// itself or through a folder above it, which is how Claude Code reads it —
// is left alone.
func TrustClaudeProject(config, dir string) (bool, error) {
	if !filepath.IsAbs(dir) {
		return false, fmt.Errorf("the project folder %q is not an absolute path", dir)
	}
	key := claudeProjectKey(dir)
	if _, err := os.Stat(config); errors.Is(err, os.ErrNotExist) {
		return false, ErrClaudeConfigMissing
	}
	if trusted, err := claudeTrusts(config, key); err != nil || trusted {
		return false, err
	}
	lock := config + ".lock"
	held := false
	for _, wait := range append(claudeLockWaits, 0) {
		if err := os.Mkdir(lock, 0o700); err == nil {
			held = true
			break
		} else if !errors.Is(err, os.ErrExist) {
			return false, err
		}
		if wait == 0 {
			break
		}
		time.Sleep(wait)
	}
	if !held {
		return false, fmt.Errorf("Claude Code's config lock %s stayed held, so the folder was not recorded as trusted", lock)
	}
	defer os.Remove(lock)

	// Read again under the lock: whatever a session wrote while this waited is
	// what the new file is made from.
	info, err := os.Stat(config)
	if err != nil {
		return false, err
	}
	body, err := os.ReadFile(config)
	if err != nil {
		return false, err
	}
	var whole map[string]json.RawMessage
	if err := json.Unmarshal(body, &whole); err != nil {
		return false, fmt.Errorf("Claude Code's config %s is not a JSON object: %w", config, err)
	}
	projects := map[string]map[string]json.RawMessage{}
	if raw, ok := whole["projects"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if err := json.Unmarshal(raw, &projects); err != nil {
			return false, fmt.Errorf("Claude Code's config %s has projects that are not objects: %w", config, err)
		}
	}
	if trustedIn(projects, key) {
		return false, nil
	}
	entry := projects[key]
	if entry == nil {
		entry = map[string]json.RawMessage{}
	}
	entry["hasTrustDialogAccepted"] = json.RawMessage("true")
	projects[key] = entry
	if whole["projects"], err = json.Marshal(projects); err != nil {
		return false, err
	}
	out, err := json.MarshalIndent(whole, "", "  ")
	if err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(config), ".claude.json.clawdline-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tmp.Name(), config); err != nil {
		return false, err
	}
	return true, nil
}

// claudeTrusts reads config without the lock, so a folder already trusted
// never takes it.
func claudeTrusts(config, key string) (bool, error) {
	body, err := os.ReadFile(config)
	if err != nil {
		return false, err
	}
	var whole struct {
		Projects map[string]map[string]json.RawMessage `json:"projects"`
	}
	if err := json.Unmarshal(body, &whole); err != nil {
		return false, fmt.Errorf("Claude Code's config %s cannot be read: %w", config, err)
	}
	return trustedIn(whole.Projects, key), nil
}

// trustedIn walks from key up to the root, as Claude Code does: a folder under
// a trusted folder is trusted.
func trustedIn(projects map[string]map[string]json.RawMessage, key string) bool {
	for at := key; ; {
		if raw, ok := projects[at]["hasTrustDialogAccepted"]; ok && bytes.Equal(bytes.TrimSpace(raw), []byte("true")) {
			return true
		}
		up := claudeProjectKey(filepath.Dir(filepath.FromSlash(at)))
		if up == at {
			return false
		}
		at = up
	}
}

// claudeProjectKey is the folder as Claude Code names it in `projects`: the
// real path, with forward slashes on Windows.
func claudeProjectKey(dir string) string {
	dir = filepath.Clean(dir)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	if runtime.GOOS == "windows" {
		return filepath.ToSlash(dir)
	}
	return dir
}
