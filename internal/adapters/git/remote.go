package git

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ErrNoRemote is the repository having no `origin`.
//
// It is its own answer because "this directory is not a repository", "this
// repository has nobody to push to" and "git would not answer" are three
// different sentences about a project, and the one thing they have in common —
// no deploy row — is the answer they must not collapse into. A workflow status
// file is named after the GitHub remote, so a repository without one has
// nothing that could be found, while a git that failed leaves it unknown
// whether there was anything to find.
var ErrNoRemote = errors.New("that repository has no origin remote")

// remoteTimeout is the whole read's budget. `remote get-url` reads
// `.git/config` and `rev-parse` reads one file, so this is not about how long
// the work takes — it is the ceiling on a git that has stopped answering,
// which is the same ceiling Changes uses per command.
const remoteTimeout = 5 * time.Second

// Remote is the URL of a working directory's `origin`.
//
// The happy path is one process. Only a refusal pays for the second one, which
// is what tells ErrNoRemote from ErrNotRepository: both spellings of the
// question exit non-zero, and `remote get-url` alone cannot say which of the
// two it was.
func (g *Git) Remote(ctx context.Context, cwd string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	url, err := g.readOnly(ctx, cwd, "remote", "get-url", "origin")
	if err == nil {
		// **Trimmed here, once.** `readOnly` hands back what the command
		// printed, newline and all, because its other caller splits the answer
		// into lines and never sees it. A caller of this one compares the
		// whole string: with the newline left on, `…/widgets.git\n` does not
		// end in `.git`, the repository name comes out as `widgets.git`, and
		// the workflow file named after it is looked for under a name nothing
		// writes. That was measured against a real checkout, not reasoned
		// about — the fake git in the first test returned a clean string.
		url = strings.TrimSpace(url)
		if url == "" {
			return "", ErrNoRemote
		}
		return url, nil
	}
	// A timeout, a missing git or an oversized answer says nothing about this
	// directory, so it is carried as itself rather than asked again.
	if errors.Is(err, ErrTimedOut) || errors.Is(err, ErrUnavailable) || errors.Is(err, ErrTooLarge) {
		return "", err
	}
	if _, err := g.readOnly(ctx, cwd, "rev-parse", "--show-toplevel"); err != nil {
		if errors.Is(err, ErrTimedOut) || errors.Is(err, ErrUnavailable) || errors.Is(err, ErrTooLarge) {
			return "", err
		}
		return "", ErrNotRepository
	}
	return "", ErrNoRemote
}
