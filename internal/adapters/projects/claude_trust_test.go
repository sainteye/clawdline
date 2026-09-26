package projects

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func readClaudeConfig(t *testing.T, path string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// A project nobody opened Claude Code in is recorded as trusted, and nothing
// else in the file — another project's settings, the account, the file's
// mode — is changed on the way.
func TestAProjectIsRecordedAsTrustedAndNothingElseChanges(t *testing.T) {
	home := t.TempDir()
	config := filepath.Join(home, ".claude.json")
	project := filepath.Join(home, "projects", "dual")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(home, "projects", "other")
	before, _ := json.Marshal(map[string]any{"numStartups": 7, "oauthAccount": map[string]any{"emailAddress": "x"},
		"projects": map[string]any{claudeProjectKey(other): map[string]any{"allowedTools": []string{"Bash"}, "hasTrustDialogAccepted": false}}})
	if err := os.WriteFile(config, before, 0o600); err != nil {
		t.Fatal(err)
	}
	wrote, err := TrustClaudeProject(config, project)
	if err != nil || !wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	got := readClaudeConfig(t, config)
	if got["numStartups"] != float64(7) || got["oauthAccount"].(map[string]any)["emailAddress"] != "x" {
		t.Fatalf("the rest of the file changed: %v", got)
	}
	projects := got["projects"].(map[string]any)
	if projects[claudeProjectKey(project)].(map[string]any)["hasTrustDialogAccepted"] != true {
		t.Fatalf("the project is not trusted: %v", projects)
	}
	kept := projects[claudeProjectKey(other)].(map[string]any)
	if kept["hasTrustDialogAccepted"] != false || kept["allowedTools"].([]any)[0] != "Bash" {
		t.Fatalf("another project changed: %v", kept)
	}
	info, _ := os.Stat(config)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	if _, err := os.Stat(config + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the lock was left behind: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(home, ".claude.json.clawdline-*")); len(left) != 0 {
		t.Fatalf("a temporary file was left behind: %v", left)
	}
}

// A folder under a trusted one is trusted already, which is how Claude Code
// reads it; the file is not rewritten for it.
func TestAFolderUnderATrustedOneIsLeftAlone(t *testing.T) {
	home := t.TempDir()
	config := filepath.Join(home, ".claude.json")
	project := filepath.Join(home, "projects", "dual")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"projects": map[string]any{claudeProjectKey(home): map[string]any{"hasTrustDialogAccepted": true}}})
	before := string(body)
	if err := os.WriteFile(config, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if wrote, err := TrustClaudeProject(config, project); err != nil || wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	if body, _ := os.ReadFile(config); string(body) != before {
		t.Fatalf("rewritten: %s", body)
	}
}

// A machine where Claude Code never ran has no file, and none is made:
// that would stand in for Claude Code's own first run.
func TestNoConfigIsNotCreated(t *testing.T) {
	config := filepath.Join(t.TempDir(), ".claude.json")
	if _, err := TrustClaudeProject(config, t.TempDir()); !errors.Is(err, ErrClaudeConfigMissing) {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(config); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a config was created")
	}
}

// A lock a running Claude Code holds is waited for and never broken: the file
// is left as it is and the lock stays the holder's.
func TestAHeldLockIsNotBroken(t *testing.T) {
	saved := claudeLockWaits
	claudeLockWaits = []time.Duration{time.Millisecond}
	defer func() { claudeLockWaits = saved }()
	home := t.TempDir()
	config := filepath.Join(home, ".claude.json")
	if err := os.WriteFile(config, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(config+".lock", 0o700); err != nil {
		t.Fatal(err)
	}
	if wrote, err := TrustClaudeProject(config, t.TempDir()); err == nil || wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	if body, _ := os.ReadFile(config); string(body) != `{}` {
		t.Fatalf("written under somebody else's lock: %s", body)
	}
	if _, err := os.Stat(config + ".lock"); err != nil {
		t.Fatalf("the holder's lock was removed: %v", err)
	}
}
