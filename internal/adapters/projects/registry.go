package projects

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// RegistryPaths is ProjectIcon.knownPaths: every directory
// ~/.claude/project-icons.json names. Only the keys are read.
func RegistryPaths() []string {
	data, err := os.ReadFile(filepath.Join(home(), ".claude", "project-icons.json"))
	if err != nil {
		return nil
	}
	var root struct {
		Projects map[string]json.RawMessage `json:"projects"`
	}
	if json.Unmarshal(data, &root) != nil {
		return nil
	}
	out := make([]string, 0, len(root.Projects))
	for path := range root.Projects {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}
