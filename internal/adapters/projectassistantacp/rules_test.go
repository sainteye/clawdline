package projectassistantacp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The standing rules Always allow and Never leave in a Project's
// .claude/settings.local.json are listed, and revoking one rewrites only that
// list, keeping every other setting and the file's mode.
func TestClaudeLocalRulesListAndRevokeOnlyThatRule(t *testing.T) {
	dir := t.TempDir()
	rules := ClaudeLocalRules{}
	if got, err := rules.List(dir); err != nil || len(got) != 0 {
		t.Fatalf("no file: %v %v", got, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".claude", "settings.local.json")
	original := `{"env":{"A":"1"},"permissions":{"allow":["Bash(git status:*)","Read(**)"],"deny":["WebFetch(domain:example.com)"],"defaultMode":"default"}}`
	if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	got, err := rules.List(dir)
	want := []pa.PermissionRule{{Behavior: "allow", Rule: "Bash(git status:*)"}, {Behavior: "allow", Rule: "Read(**)"}, {Behavior: "deny", Rule: "WebFetch(domain:example.com)"}}
	if err != nil || len(got) != 3 || got[0] != want[0] || got[2] != want[2] {
		t.Fatalf("list: %+v %v", got, err)
	}
	if err = rules.Remove(dir, pa.PermissionRule{Behavior: "allow", Rule: "Bash(git status:*)"}); err != nil {
		t.Fatal(err)
	}
	var after struct {
		Env         map[string]string `json:"env"`
		Permissions struct {
			Allow       []string `json:"allow"`
			Deny        []string `json:"deny"`
			DefaultMode string   `json:"defaultMode"`
		} `json:"permissions"`
	}
	data, _ := os.ReadFile(path)
	if err = json.Unmarshal(data, &after); err != nil {
		t.Fatal(err)
	}
	if after.Env["A"] != "1" || after.Permissions.DefaultMode != "default" || len(after.Permissions.Allow) != 1 || after.Permissions.Allow[0] != "Read(**)" || len(after.Permissions.Deny) != 1 {
		t.Fatalf("other settings changed: %s", data)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	if err = rules.Remove(dir, pa.PermissionRule{Behavior: "allow", Rule: "Bash(git status:*)"}); pa.Code(err) != pa.NotFound {
		t.Fatalf("revoking a revoked rule: %v", err)
	}
	if err = rules.Remove(dir, pa.PermissionRule{Behavior: "maybe", Rule: "x"}); pa.Code(err) != pa.Invalid {
		t.Fatalf("an unknown behavior: %v", err)
	}
}
