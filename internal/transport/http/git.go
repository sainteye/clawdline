package http

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/git"
	"github.com/sainteye/clawdline/internal/contract"
)

// gitPath recognises GET /v1/sessions/{id}/git and returns the id, decoded —
// one segment of the path the mux dispatched by, which is what makes a tmux
// pane's `%19` a name here rather than a separator or a control character.
func gitPath(r *http.Request) (string, bool) {
	return sessionVerbIs(r, "git", http.MethodGet)
}

// gitDiffPath recognises the one nested read below /git. Split first and
// decode only the session id, under the same gate as the two-segment route.
func gitDiffPath(r *http.Request) (string, bool) {
	if r.Method != http.MethodGet {
		return "", false
	}
	rest, ok := strings.CutPrefix(routePath(r), "/v1/sessions/")
	if !ok {
		return "", false
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] != "git" || parts[2] != "diff" {
		return "", false
	}
	return decodeSegment(parts[0]), true
}

// sessionGitRoute answers the Git panel: the branch line and the changed files
// of the repository the open session is sitting in.
//
// It is a reading, not a subscription. The Swift route makes the same three
// lock-free invocations when the panel opens and keeps nothing afterwards,
// because a status and two diffs are cheap once and three subprocesses per
// session per event-stream beat are not.
//
// **Read level.** Nothing is typed into the session and nothing is written to
// the repository, so this asks the gate for no more than the list does. The
// two rows beside it in the same menu — commit and push — are sends, and they
// go through `/send` with the confirmation sheet in front of them, exactly as
// they do in the app being replicated.
func (s *Server) sessionGitRoute(w http.ResponseWriter, r *http.Request, id string) {
	if !s.ownsSessions() {
		s.forwardUpstream(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	item, err := s.actions().Find(ctx, id)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	// No working directory is not a repository that could not be read; it is
	// this daemon not knowing where the session is. `Targets.workingDirectory`
	// answers the same question there, and the same 404 follows from it.
	if strings.TrimSpace(item.CWD) == "" {
		writeRefusal(w, http.StatusNotFound, "not_found", "No session named that")
		return
	}
	snapshot, err := git.New().Changes(ctx, item.CWD)
	if err != nil {
		writeGitRefusal(w, err)
		return
	}
	writeJSON(w, contract.GitReply{Git: wireGitSnapshot(snapshot)})
}

// sessionGitDiffRoute answers only for a path in a fresh status reading. That
// makes a read-only paired device able to inspect a change without turning the
// route into a general file reader.
func (s *Server) sessionGitDiffRoute(w http.ResponseWriter, r *http.Request, id string) {
	if !s.ownsSessions() {
		s.forwardUpstream(w, r)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "A Git diff needs a changed path")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	item, err := s.actions().Find(ctx, id)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	if strings.TrimSpace(item.CWD) == "" {
		writeRefusal(w, http.StatusNotFound, "not_found", "No session named that")
		return
	}
	diff, err := git.New().FileDiff(ctx, item.CWD, path)
	if err != nil {
		if errors.Is(err, git.ErrFileNotChanged) {
			writeRefusal(w, http.StatusNotFound, "git_file_not_changed", "That path is not a changed file in this repository")
			return
		}
		writeGitRefusal(w, err)
		return
	}
	patches := make([]contract.GitPatch, 0, len(diff.Patches))
	for _, patch := range diff.Patches {
		patches = append(patches, contract.GitPatch{
			Scope:       contract.GitPatchScope(patch.Scope),
			UnifiedDiff: patch.UnifiedDiff,
		})
	}
	writeJSON(w, contract.GitDiffReply{Diff: contract.GitFileDiff{
		Path: diff.Path, Kind: contract.GitFileKind(diff.Kind), Patches: patches,
	}})
}

// writeGitRefusal gives each way the read can end its own status and sentence,
// in the Swift route's words. The panel tells `not_a_repo` from the rest —
// "this is not a repository" is a fact about the session, and everything else
// is a fact about this read — so the two must not arrive as one code.
func writeGitRefusal(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, git.ErrNotRepository):
		writeRefusal(w, http.StatusNotFound, "not_a_repo",
			"That session is not inside a Git repository")
	case errors.Is(err, git.ErrTimedOut):
		writeRefusal(w, http.StatusGatewayTimeout, "git_timeout",
			"That repository did not answer inside the Git read deadline")
	case errors.Is(err, git.ErrTooLarge):
		// Not a failure of git and not a fact about the session: this daemon
		// declined to hold that much of somebody's repository in memory.
		writeRefusal(w, http.StatusInternalServerError, "git_too_large",
			"That repository answered with more than this reads")
	case errors.Is(err, git.ErrUnavailable):
		// Not 404: this machine said nothing about that directory.
		writeRefusal(w, http.StatusNotImplemented, "git_unavailable",
			"There is no git on this machine to read that repository with")
	default:
		writeRefusal(w, http.StatusInternalServerError, "git_failed",
			"Could not read that repository")
	}
}

// wireGitSnapshot carries the reading across in the Swift payload's shape.
//
// `from`, `additions` and `deletions` keep their keys and carry null: a file
// that is in no diff has no measurement, and a zero would read as one.
func wireGitSnapshot(in git.Status) contract.GitSnapshot {
	files := make([]contract.GitFile, 0, len(in.Files))
	for _, f := range in.Files {
		file := contract.GitFile{
			Path:      f.Path,
			Staged:    f.Staged,
			Unstaged:  f.Unstaged,
			Kind:      contract.GitFileKind(f.Kind),
			Additions: wideCount(f.Additions),
			Deletions: wideCount(f.Deletions),
		}
		if f.From != "" {
			from := f.From
			file.From = &from
		}
		files = append(files, file)
	}
	return contract.GitSnapshot{
		Branch: in.Branch,
		Head:   in.Head,
		Ahead:  int64(in.Ahead),
		Behind: int64(in.Behind),
		Clean:  len(in.Files) == 0,
		Files:  files,
	}
}

func wideCount(n *int) *int64 {
	if n == nil {
		return nil
	}
	wide := int64(*n)
	return &wide
}
