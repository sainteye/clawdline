package git

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// What the broker needs to ask a repository: where it is, whether a branch
// exists, whether it has been merged, whether a checkout is dirty, and how many
// commits a delivery branch carries.
//
// Every answer comes from git itself, and every one of them distinguishes "no"
// from "I could not ask". That distinction is the whole point: an inventory
// that turns an unreadable repository into `droppable` deletes somebody's work,
// so unknown is carried as unknown all the way to the wire, where it reads as
// `null` and keeps the row visible.

// Toplevel resolves a directory to the repository that owns it — the **main**
// worktree, not a linked one.
//
// A task working in a linked worktree must be arbitrated against the tasks in
// the repository it came from, and `--show-toplevel` inside a linked worktree
// answers with the linked checkout. `--git-common-dir` is the one that is
// shared, and its parent is the main worktree.
func (g *Git) Toplevel(ctx context.Context, dir string) (string, error) {
	common, err := g.runIn(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	common = filepath.Clean(common)
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common), nil
	}
	// A bare repository, or a `.git` file layout this does not recognise: fall
	// back to the checkout's own top level rather than inventing a parent.
	top, err := g.runIn(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return filepath.Clean(top), nil
}

// AddWorktree creates an isolated checkout on a new branch at base.
//
// `--no-track` because the branch is a delivery, not a follower: a branch that
// tracks the base will report itself behind the moment anybody else lands, and
// "behind" is not a fact about this task.
func (g *Git) AddWorktree(ctx context.Context, repo, path, branch, base string) error {
	_, err := g.run(ctx, repo, "worktree", "add", "--no-track", "-b", branch, path, base)
	return err
}

// RemoveWorktree takes a checkout away, leaving the branch.
func (g *Git) RemoveWorktree(ctx context.Context, repo, path string) error {
	_, err := g.run(ctx, repo, "worktree", "remove", "--force", path)
	return err
}

// BranchExists asks whether a local branch is there. The third answer — the
// listing failed — is `false, false`, and no caller may read that as absence.
func (g *Git) BranchExists(ctx context.Context, repo, branch string) (exists bool, known bool) {
	out, err := g.run(ctx, repo, "for-each-ref", "--format=%(refname:short)", "refs/heads/"+branch)
	if err != nil {
		return false, false
	}
	return strings.TrimSpace(out) != "", true
}

// Merged is whether every commit on branch is already on target.
func (g *Git) Merged(ctx context.Context, repo, branch, target string) (merged bool, known bool) {
	ok, err := g.IsAncestor(ctx, repo, "refs/heads/"+branch, target)
	if err != nil {
		return false, false
	}
	return ok, true
}

// BranchesContaining names every local branch whose history holds commit,
// without the `refs/heads/` prefix. An error is "could not ask", never "no
// branch has it".
func (g *Git) BranchesContaining(ctx context.Context, repo, commit string) ([]string, error) {
	if commit == "" || strings.HasPrefix(commit, "-") {
		return nil, fmt.Errorf("%q is not a commit to ask about", commit)
	}
	out, err := g.run(ctx, repo, "for-each-ref", "--format=%(refname)", "--contains", commit, "refs/heads/")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if name := strings.TrimPrefix(strings.TrimSpace(line), "refs/heads/"); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// Commits counts what a delivery branch carries over the commit it started
// from.
func (g *Git) Commits(ctx context.Context, repo, base, branch string) (int, bool) {
	if base == "" || branch == "" {
		return 0, false
	}
	out, err := g.run(ctx, repo, "rev-list", "--count", base+".."+branch)
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, false
	}
	return n, true
}

// Dirty reports uncommitted changes in a checkout, tracked or not.
func (g *Git) Dirty(ctx context.Context, worktree string) (dirty bool, known bool) {
	out, err := g.runIn(ctx, worktree, "status", "--porcelain")
	if err != nil {
		return false, false
	}
	return strings.TrimSpace(out) != "", true
}

// ResolveCommit turns a caller's text into the commit git says it names.
//
// `--end-of-options` is not decoration: without it a ref beginning with `-` is
// read by git as a flag, and the caller chose that string.
func (g *Git) ResolveCommit(ctx context.Context, repo, rev string) (string, error) {
	return g.run(ctx, repo, "rev-parse", "--verify", "--end-of-options", rev+"^{commit}")
}

// ValidBranchName is `git check-ref-format --branch`, asked of git rather than
// guessed at with a regular expression.
func (g *Git) ValidBranchName(ctx context.Context, name string) bool {
	if name == "" || strings.HasPrefix(name, "-") {
		return false
	}
	cmd := exec.CommandContext(ctx, g.Binary, "check-ref-format", "--branch", name)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	return cmd.Run() == nil
}

// runIn is run with the working directory set rather than `-C`, for the two
// questions that are about a directory rather than about a repository.
func (g *Git) runIn(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, g.Binary, args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
