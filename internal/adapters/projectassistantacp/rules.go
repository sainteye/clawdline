package projectassistantacp

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// ClaudeLocalRules reads and revokes the standing rules Claude Code keeps for
// a Project after Always allow or Never: its adapter writes them to the
// Project's .claude/settings.local.json under permissions.allow and
// permissions.deny (claude-agent-acp permissions/effects.js, destination
// localSettings). Removing a rule rewrites only that list; every other key in
// the file is kept.
type ClaudeLocalRules struct{}

func claudeLocalSettings(dir string) string {
	return filepath.Join(dir, ".claude", "settings.local.json")
}

func readClaudeLocal(dir string) (map[string]json.RawMessage, os.FileMode, error) {
	path := claudeLocalSettings(dir)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return nil, 0, refusal(pa.Invalid, "rules_file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, refusal(pa.Invalid, "rules_file")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, capacity.MaximumBotRulesFileBytes+1))
	if err != nil {
		return nil, 0, refusal(pa.Invalid, "rules_file")
	}
	if len(data) > capacity.MaximumBotRulesFileBytes {
		return nil, 0, refusal(pa.CapacityFull, "rules_file")
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(data, &top) != nil {
		return nil, 0, refusal(pa.Invalid, "rules_file")
	}
	return top, info.Mode().Perm(), nil
}

func claudeRuleLists(top map[string]json.RawMessage) (map[string]json.RawMessage, map[string][]string, error) {
	permissions := map[string]json.RawMessage{}
	lists := map[string][]string{}
	if raw, ok := top["permissions"]; ok {
		if json.Unmarshal(raw, &permissions) != nil {
			return nil, nil, refusal(pa.Invalid, "rules_file")
		}
	}
	for _, behavior := range []string{"allow", "deny"} {
		if raw, ok := permissions[behavior]; ok {
			var rules []string
			if json.Unmarshal(raw, &rules) != nil {
				return nil, nil, refusal(pa.Invalid, "rules_file")
			}
			lists[behavior] = rules
		}
	}
	return permissions, lists, nil
}

// List returns the Project's standing rules, allow before deny, in the file's order.
func (ClaudeLocalRules) List(dir string) ([]pa.PermissionRule, error) {
	top, _, err := readClaudeLocal(dir)
	if err != nil || top == nil {
		return []pa.PermissionRule{}, err
	}
	_, lists, err := claudeRuleLists(top)
	if err != nil {
		return nil, err
	}
	out := []pa.PermissionRule{}
	for _, behavior := range []string{"allow", "deny"} {
		for _, rule := range lists[behavior] {
			out = append(out, pa.PermissionRule{Behavior: behavior, Rule: rule})
		}
	}
	return out, nil
}

// Remove revokes one rule, writing the file atomically with its mode kept.
func (ClaudeLocalRules) Remove(dir string, rule pa.PermissionRule) error {
	if err := pa.ValidateRule(rule); err != nil {
		return err
	}
	top, mode, err := readClaudeLocal(dir)
	if err != nil {
		return err
	}
	if top == nil {
		return refusal(pa.NotFound, "rule")
	}
	permissions, lists, err := claudeRuleLists(top)
	if err != nil {
		return err
	}
	kept, found := []string{}, false
	for _, r := range lists[rule.Behavior] {
		if r == rule.Rule && !found {
			found = true
			continue
		}
		kept = append(kept, r)
	}
	if !found {
		return refusal(pa.NotFound, "rule")
	}
	permissions[rule.Behavior], _ = json.Marshal(kept)
	top["permissions"], _ = json.Marshal(permissions)
	data, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return refusal(pa.Invalid, "rules_file")
	}
	path := claudeLocalSettings(dir)
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings.local-*.json")
	if err != nil {
		return refusal(pa.Invalid, "rules_file")
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(append(data, '\n')); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(name, mode)
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		return refusal(pa.Invalid, "rules_file")
	}
	return nil
}
