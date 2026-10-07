package orchestrator

import (
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/memory"
)

// A Project's shared memory is filed under the same key its worktrees are:
// the two spellings must never drift apart.
func TestTheMemoryKeyIsTheWorktreeSlug(t *testing.T) {
	root := t.TempDir()
	for _, repo := range []string{
		filepath.Join(root, "Clawdline"),
		filepath.Join(root, "a.repo_with-punctuation & spaces"),
		filepath.Join(root, "a-name-that-is-much-longer-than-thirty-two-bytes-long"),
		filepath.Join(root, "!!!"),
	} {
		if got, want := memory.RepoKey(repo), RepoSlug(repo); got != want {
			t.Errorf("%s: memory key %q, worktree slug %q", repo, got, want)
		}
	}
}
