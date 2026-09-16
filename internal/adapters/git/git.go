// Package git answers questions about a repository by asking git, never by
// reading its files. Every answer is a receipt from the tool that owns the
// truth.
package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Git struct{ Binary string }

func New() *Git { return &Git{Binary: "git"} }

// IsAncestor reports whether a commit is contained in a branch.
//
// This is what "landed" means, and it is the only thing that means it. A branch
// existing, a diff being empty, or a delivery being marked done are all
// compatible with the work never having reached the target. The question is
// ancestry and git answers it directly.
func (g *Git) IsAncestor(ctx context.Context, repo, commit, branch string) (bool, error) {
	if commit == "" || branch == "" {
		return false, fmt.Errorf("a landing needs both a commit and a target branch")
	}
	// Resolve first, so that "that commit does not exist here" is a different
	// answer from "it exists and is not on the branch". Collapsing them would
	// let a typo read as an honest refusal.
	if _, err := g.run(ctx, repo, "rev-parse", "--verify", commit+"^{commit}"); err != nil {
		return false, fmt.Errorf("%s is not a commit in %s", commit, repo)
	}
	if _, err := g.run(ctx, repo, "rev-parse", "--verify", branch); err != nil {
		return false, fmt.Errorf("%s is not a branch in %s", branch, repo)
	}
	cmd := exec.CommandContext(ctx, g.Binary, "-C", repo,
		"merge-base", "--is-ancestor", commit, branch)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Head is the commit a branch currently points at.
func (g *Git) Head(ctx context.Context, repo, ref string) (string, error) {
	return g.run(ctx, repo, "rev-parse", ref)
}

func (g *Git) run(ctx context.Context, repo string, args ...string) (string, error) {
	full := append([]string{"-C", repo}, args...)
	cmd := exec.CommandContext(ctx, g.Binary, full...)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		said := strings.TrimSpace(stderr.String())
		if said == "" {
			said = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], said)
	}
	return strings.TrimSpace(string(out)), nil
}
