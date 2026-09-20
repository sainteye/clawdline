package process

import (
	"context"
	"path"
	"strings"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// OpenFiles lists the files each pid holds open. `ok` is false when the table
// itself could not be read.
//
// It is the one place a running Codex says which conversation it is having.
// Claude Code writes `~/.claude/sessions/<pid>.json` and so can be asked about
// a pid directly; Codex writes nothing that carries a pid at all. What it does
// do is hold its own rollout open for as long as the session lives, so the
// kernel answers the question — not a correlation between a file's timestamp
// and a process's start, which on a machine that opens several sessions a
// minute would pair the wrong two often enough to matter.
//
// The false answer is kept apart from an empty one on purpose. A process that
// holds nothing open is early; a table that could not be read has said nothing
// at all, and only the second is this machine's own fault to fix (DG-7).
type OpenFiles func(ctx context.Context, pids []int) (map[int][]string, bool)

// codexRollout is the prefix and suffix of a Codex transcript's file name,
// which is where the conversation id is: `rollout-<timestamp>-<id>.jsonl`,
// filed under `~/.codex/sessions/<year>/<month>/<day>/`.
const (
	codexRolloutPrefix = "rollout-"
	codexRolloutSuffix = ".jsonl"
	// codexIDLength is the length of the thread id in that name. It is fixed
	// because the timestamp before it also contains dashes, so the id cannot
	// be found by splitting: it is the last 36 characters before `.jsonl`.
	codexIDLength = 36
)

// codexConversation reads the conversation id out of the transcripts a process
// holds open, and says how it got there — or which kind of nothing it found.
//
// Two open rollouts are not a tie to be broken. Codex's own app-server holds
// every thread it is serving open at once, and a session named after whichever
// of those sorted first would be a session wearing another's name, which costs
// far more than a row with no name on it.
func codexConversation(paths []string, read bool) (string, session.Binding) {
	if !read {
		return "", session.BindingUnreadable
	}
	found := ""
	for _, p := range paths {
		id, ok := codexRolloutID(p)
		if !ok {
			continue
		}
		if found != "" && found != id {
			return "", session.BindingAmbiguous
		}
		found = id
	}
	if found == "" {
		return "", session.BindingNoRecord
	}
	return found, session.BindingOpenFile
}

// codexRolloutID is the thread id in a rollout's file name, if that is what
// this path is.
func codexRolloutID(p string) (string, bool) {
	name := path.Base(p)
	if !strings.HasPrefix(name, codexRolloutPrefix) || !strings.HasSuffix(name, codexRolloutSuffix) {
		return "", false
	}
	stem := strings.TrimSuffix(name, codexRolloutSuffix)
	if len(stem) < codexRolloutLen {
		return "", false
	}
	id := stem[len(stem)-codexIDLength:]
	if stem[len(stem)-codexIDLength-1] != '-' {
		return "", false
	}
	for _, r := range id {
		if r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' {
			continue
		}
		return "", false
	}
	return id, true
}

// codexRolloutLen is the shortest name that can carry an id: the prefix, a
// separator and the id itself.
const codexRolloutLen = len(codexRolloutPrefix) + 1 + codexIDLength
