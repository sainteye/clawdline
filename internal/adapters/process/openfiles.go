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
// **The question is how many conversations are open, not how many files.** A
// Codex session running sub-agents holds one rollout per thread open at once,
// all of them its own, and on this Mac that is the ordinary case: of the four
// Codex sessions running on 2026-09-20, two held two rollouts each and a third
// held six. Counting files answered `ambiguous` for all three and left them
// with no name, which is the bug this rule replaces.
//
// **What tells them apart is a positive field, never the shape of the set.**
// The head of every rollout is a `session_meta` record carrying `session_id`,
// the conversation the thread belongs to, beside `id`, the thread itself. A
// thread that started the conversation has them equal; a sub-thread has
// `session_id` pointing at the conversation and `id` at itself. So the rollouts
// one process holds open fold to the set of `session_id`s among them: one
// conversation binds, two do not.
//
// Reading the head rather than excluding the rollouts that carry a
// `parent_thread_id` is deliberate, and not only because absence is the weaker
// evidence. Of the 414 sub-thread rollouts on this Mac, 36 name another
// sub-thread as their parent rather than the conversation, so a rule built on
// parentage has to walk a tree the open files may not contain all of; and where
// the conversation's own rollout is not among the open ones — a sub-agent still
// writing after its parent thread closed its descriptor — the parentage rule
// answers `no_record` while `session_id` still names the session correctly.
// The same field also corrects an answer the old rule got wrong rather than
// merely refused: one open rollout that happens to be a sub-thread used to bind
// the sub-thread's id, taken from the file's name.
//
// **Two conversations remain ambiguous and bind nothing.** Codex's own
// app-server holds every thread it is serving open at once, and a session named
// after whichever of those sorted first would be a session wearing another's
// name — which costs far more than a row with no name on it. What is new is
// that the answer says what it counted (session.OpenTranscripts), so the next
// reader does not have to open the same files to find out why.
func codexConversation(paths []string, read bool, head RolloutHead) (string, session.Binding, string) {
	if !read {
		return "", session.BindingUnreadable, session.UnreadableTranscriptsDetail
	}
	counted := session.OpenTranscripts{}
	conversations := map[string]bool{}
	for _, p := range paths {
		name, ok := codexRolloutID(p)
		if !ok {
			continue
		}
		counted.Open++
		// The file's name carries the thread, which is the conversation only
		// when the thread started it. An unreadable head therefore falls back
		// to the name, which can only ever refuse — a sub-thread's own id
		// disagrees with its conversation and the answer stays ambiguous —
		// and never to naming one session after another.
		conversation := name
		if conv, thread, readable := headOf(head, p); !readable {
			counted.Unread++
		} else if conv != "" {
			conversation = conv
			if thread != "" && thread != conv {
				counted.SubThreads++
			}
		}
		conversations[conversation] = true
	}
	counted.Conversations = len(conversations)
	if counted.Open == 0 {
		return "", session.BindingNoRecord, session.NoTranscriptDetail
	}
	if counted.Conversations == 1 {
		for id := range conversations {
			return id, session.BindingOpenFile, ""
		}
	}
	return "", session.BindingAmbiguous, counted.Detail()
}

// headOf reads one rollout's head, or answers that it could not be read. A
// scan built without a reader is one that cannot read any of them.
func headOf(head RolloutHead, path string) (string, string, bool) {
	if head == nil {
		return "", "", false
	}
	return head(path)
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
