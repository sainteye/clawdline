package skillmenu

import (
	"os"
	"path/filepath"
	"strings"
)

// commandFiles reads metadata from provider-owned Markdown command files. It
// never evaluates the body, frontmatter shell substitutions or plugin hooks.
// One shared directory-entry budget bounds the entire recursive traversal.
func commandFiles(root string, source Source, namespace string, recursive bool) []Skill {
	var out []Skill
	remaining := maxDirectoryEntries
	var visit func(string, string)
	visit = func(dir, prefix string) {
		if remaining <= 0 {
			return
		}
		f, err := os.Open(dir)
		if err != nil {
			return
		}
		entries, _ := f.ReadDir(remaining)
		f.Close()
		remaining -= len(entries)
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			// Do not follow directory symlinks into unrelated trees or cycles.
			if entry.IsDir() {
				if recursive {
					visit(path, prefix+entry.Name()+":")
				}
				continue
			}
			if entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			name := prefix + strings.TrimSuffix(entry.Name(), ".md")
			if !validCodexCommand(name) {
				continue
			}
			data, ok := readHead(path, maxHeadBytes)
			if !ok {
				continue
			}
			metadata := frontmatter(data)
			if !metadata.userInvocable {
				continue
			}
			if namespace != "" {
				name = namespace + ":" + name
			}
			out = append(out, Skill{Name: name, Description: metadata.description, Source: source, Invocation: "/" + name})
		}
	}
	visit(root, "")
	return out
}

// WithCodexPrompts adds the flat personal prompt directory, including before a
// new conversation has a rollout. Codex resolves arguments when it runs them.
func WithCodexPrompts(reading Reading, home string) Reading {
	root := os.Getenv("CODEX_HOME")
	if root == "" {
		root = filepath.Join(home, ".codex")
	}
	prompts := commandFiles(filepath.Join(root, "prompts"), Personal, "prompts", false)
	reading.Skills = append(reading.Skills, prompts...)
	result := cut(reading.Skills)
	result.Truncated = result.Truncated || reading.Truncated
	return result
}
