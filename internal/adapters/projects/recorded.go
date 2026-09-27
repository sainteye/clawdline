package projects

import (
	"os"
	"path/filepath"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
)

// Recorded is whether the assistant's own record of conversation id is on
// disk for place: Claude's transcript in the place's project folder, or
// Codex's rollout file anywhere under its sessions tree.
//
// It is what a resume of an archived conversation is admitted on. The past
// list (ClaudePast, CodexPast) stops at the newest few hundred conversations
// of a directory, and a conversation archived months ago in a busy one has
// fallen off it while its transcript is still there to resume.
func Recorded(place Place, assistant, id string) bool {
	id, ok := SessionName(id)
	if !ok || place.Path == "" {
		return false
	}
	h := home()
	if h == "" {
		return false
	}
	switch assistant {
	case "", AssistantClaude:
		st, err := os.Stat(filepath.Join(h, ".claude", "projects", Slug(place.Path), id+".jsonl"))
		return err == nil && st.Mode().IsRegular()
	case AssistantCodex:
		return transcript.CodexPath(h, id) != ""
	}
	return false
}
