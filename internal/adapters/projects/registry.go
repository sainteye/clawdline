package projects

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// RegistryRow is ProjectIcon.row(forCwd:): the registry's whole row for a
// working directory, not only its mark.
//
// The row that wins is the **longest** registered path containing the
// directory, not an exact match: a session sits in `shop/frontend` while the
// entry naming the project is `shop` — and `shop/backend` may have a row of
// its own, which should win for anything inside it.
//
// It is the whole row because a project registers more there than a colour: a
// health check's endpoint and label live in it, which is the half of that
// reading that is a fact about the project rather than about this minute.
func RegistryRow(cwd string) map[string]any {
	if cwd == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(home(), ".claude", "project-icons.json"))
	if err != nil {
		return nil
	}
	var root struct {
		Projects map[string]map[string]any `json:"projects"`
	}
	if json.Unmarshal(data, &root) != nil {
		return nil
	}
	var best map[string]any
	longest := -1
	for path, row := range root.Projects {
		if cwd != path && !strings.HasPrefix(cwd, path+"/") {
			continue
		}
		if len(path) > longest {
			best, longest = row, len(path)
		}
	}
	return best
}
