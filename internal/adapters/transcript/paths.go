package transcript

import (
	"os"
	"path/filepath"
	"strings"
)

// ClaudePath is where Claude Code keeps one conversation's record.
//
// The directory name is the working directory as ProjectSlug spells it. That
// is Claude's scheme, not ours, so it is reproduced as given.
func ClaudePath(home, cwd, conversationID string) string {
	return filepath.Join(ClaudeConfigDir(home), "projects", ProjectSlug(cwd), conversationID+".jsonl")
}

// ClaudeConfigDir is the Claude configuration visible to this daemon. A
// different user's or process's unconfigured directory is never searched.
func ClaudeConfigDir(home string) string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(home, ".claude")
}

// CodexPath finds the rollout file for a thread.
//
// Codex files a session under the date it started and puts the id in the name,
// so there is nothing to compute: the tree is walked until the id is found.
func CodexPath(home, sessionID string) string {
	root := filepath.Join(home, ".codex", "sessions")
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, sessionID+".jsonl") {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}
