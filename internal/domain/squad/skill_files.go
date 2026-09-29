package squad

import (
	"encoding/base64"
	"path"
	"strings"
)

// ValidSkillFiles checks a browser-imported folder before it can be stored or
// published. The existing squad body limit covers SKILL.md and every file.
func ValidSkillFiles(content string, files []SkillFile) bool {
	total := len(content)
	seen := map[string]bool{"skill.md": true}
	for _, file := range files {
		name := file.Path
		if name == "" || strings.ContainsAny(name, "\\:\x00\r\n") ||
			strings.HasPrefix(name, "/") || path.Clean(name) != name ||
			strings.HasPrefix(name, "../") || name == ".." || strings.Contains(name, "/../") {
			return false
		}
		for _, part := range strings.Split(name, "/") {
			if part == "." || part == ".." || part == "" {
				return false
			}
		}
		folded := strings.ToLower(name)
		if seen[folded] {
			return false
		}
		seen[folded] = true
		bytes, err := base64.StdEncoding.Strict().DecodeString(file.ContentBase64)
		if err != nil || base64.StdEncoding.EncodeToString(bytes) != file.ContentBase64 {
			return false
		}
		total += len(bytes)
		if total > MaxSquadBodyBytes {
			return false
		}
	}
	return total <= MaxSquadBodyBytes
}
