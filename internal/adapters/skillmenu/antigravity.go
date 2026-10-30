package skillmenu

import (
	"path/filepath"
)

// Antigravity is the file-backed skills available from one Gemini working directory.
//
// It looks for skills in the repository/working directory (.gemini/skills, .agents/skills,
// .agent/skills) and personal/global directories (~/.gemini/skills,
// ~/.gemini/antigravity-cli/skills, ~/.agents/skills, ~/.agent/skills).
func Antigravity(cwd, home string) Reading {
	work := filepath.Clean(cwd)
	homeDir := filepath.Clean(home)
	root, inRepo := repositoryRoot(work)

	effective := map[string]Skill{}
	// Project skills: deeper directories win over ancestor directories.
	for _, dir := range projectDirectories(work, root, inRepo) {
		for _, sub := range []string{
			filepath.Join(dir, ".agent", "skills"),
			filepath.Join(dir, ".agents", "skills"),
			filepath.Join(dir, ".gemini", "skills"),
		} {
			for _, s := range skillsIn(sub, Project, "") {
				effective[s.Name] = s
			}
		}
	}
	// Personal / global skills:
	for _, sub := range []string{
		filepath.Join(homeDir, ".agent", "skills"),
		filepath.Join(homeDir, ".agents", "skills"),
		filepath.Join(homeDir, ".gemini", "skills"),
		filepath.Join(homeDir, ".gemini", "antigravity-cli", "skills"),
	} {
		for _, s := range skillsIn(sub, Personal, "") {
			effective[s.Name] = s
		}
	}

	out := make([]Skill, 0, len(effective))
	for _, s := range effective {
		out = append(out, s)
	}
	return cut(out)
}
