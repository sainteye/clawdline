package http

import (
	"net/http/httptest"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/git"
)

// A missing or refused directory has codes of its own, additive beside
// not_a_repo: the console names not_a_repo and shows any other code through
// its general refusal sentence (web/console/src/legacy/git-bridge.ts).
func TestAGoneOrRefusedDirectoryIsNotReportedAsNotARepo(t *testing.T) {
	for err, want := range map[error]string{
		git.ErrNotRepository: "not_a_repo",
		git.ErrNoDirectory:   "git_no_directory",
		git.ErrNoPermission:  "git_permission_denied",
	} {
		rec := httptest.NewRecorder()
		writeGitRefusal(rec, err)
		if got := codeOf(t, rec); got != want {
			t.Errorf("%v: code %q, want %q", err, got, want)
		}
	}
}
