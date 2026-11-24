package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Presence is, for every path a delivery base..head changed, where a tree
// stands on that path: Kept still holds the delivery's change, Reverted holds
// the path as it was before the delivery, and Unknown is neither — the lines
// have moved on, or a generated file was regenerated from other sources.
type Presence struct {
	Kept     int
	Reverted int
	Unknown  int
}

// Gone reports whether a tree shows the delivery backed out: at least one of
// its paths is back where it started and none still carries its change. A
// tree in which any part of the delivery is still recognisable is not gone,
// so a partial revert reads as present.
func (p Presence) Gone() bool { return p.Kept == 0 && p.Reverted > 0 }

// DeliveryPresence asks, path by path, whether the changes base..head makes
// are still in the tree of commit at. Ancestry cannot answer this — every
// commit after a merge descends from it, its revert included — and neither
// can a revert's message or `git cherry`, which still finds the reverted
// commits upstream. The trees can.
//
// A path whose blob at `at` equals head's is kept, one equal to base's is
// reverted. For the rest, the path's own patch is checked against a
// temporary index read from at's tree: applying it backwards cleanly means
// the change is still there, forwards cleanly means it is not. No checkout is
// touched and the repository's own index is left alone.
func (g *Git) DeliveryPresence(ctx context.Context, repo, base, head, at string) (Presence, error) {
	var p Presence
	if base == "" || head == "" || at == "" {
		return p, fmt.Errorf("a presence check needs a base, a head and a tree")
	}
	delivered, err := g.changedPaths(ctx, repo, base, head)
	if err != nil || len(delivered) == 0 {
		return p, err
	}
	sinceBase, err := g.changedPaths(ctx, repo, base, at)
	if err != nil {
		return p, err
	}
	sinceHead, err := g.changedPaths(ctx, repo, head, at)
	if err != nil {
		return p, err
	}
	var moved []string
	for path := range delivered {
		_, differsFromBase := sinceBase[path]
		_, differsFromHead := sinceHead[path]
		switch {
		case !differsFromHead:
			p.Kept++
		case !differsFromBase:
			p.Reverted++
		default:
			moved = append(moved, path)
		}
	}
	if len(moved) == 0 {
		return p, nil
	}

	dir, err := os.MkdirTemp("", "clawdline-tree-check-")
	if err != nil {
		return p, err
	}
	defer os.RemoveAll(dir)
	index := filepath.Join(dir, "index")
	read := exec.CommandContext(ctx, g.Binary, "-C", repo, "read-tree", "--end-of-options", at+"^{tree}")
	read.Env = append(os.Environ(), "LC_ALL=C", "GIT_INDEX_FILE="+index)
	if out, err := read.CombinedOutput(); err != nil {
		return p, fmt.Errorf("git read-tree %s: %v: %s", at, err, bytes.TrimSpace(out))
	}
	for _, path := range moved {
		kept, err := g.patchApplies(ctx, repo, index, base, head, path, true)
		if err != nil {
			return p, err
		}
		if kept {
			p.Kept++
			continue
		}
		gone, err := g.patchApplies(ctx, repo, index, base, head, path, false)
		if err != nil {
			return p, err
		}
		if gone {
			p.Reverted++
		} else {
			p.Unknown++
		}
	}
	return p, nil
}

// changedPaths is the set of paths that differ between two commits, a rename
// counted as a deletion and an addition.
func (g *Git) changedPaths(ctx context.Context, repo, from, to string) (map[string]struct{}, error) {
	cmd := exec.CommandContext(ctx, g.Binary, "-C", repo, "diff", "--name-only", "-z", "--no-renames",
		"--no-ext-diff", "--end-of-options", from, to)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff %s %s: %v: %s", from, to, err, bytes.TrimSpace(stderr.Bytes()))
	}
	paths := map[string]struct{}{}
	for _, path := range strings.Split(string(out), "\x00") {
		if path != "" {
			paths[path] = struct{}{}
		}
	}
	return paths, nil
}

// patchApplies checks one path's patch base..head against the index, in
// reverse when reverse is set. The patch goes from diff to apply untouched:
// trimming it, as run does, would cut a final blank context line and corrupt
// the hunk.
func (g *Git) patchApplies(ctx context.Context, repo, index, base, head, path string, reverse bool) (bool, error) {
	diff := exec.CommandContext(ctx, g.Binary, "-C", repo, "diff", "--binary", "--full-index", "--no-color",
		"--no-ext-diff", "--no-renames", "--end-of-options", base, head, "--", ":(literal)"+path)
	diff.Env = append(os.Environ(), "LC_ALL=C")
	var diffErr bytes.Buffer
	diff.Stderr = &diffErr
	patch, err := diff.Output()
	if err != nil {
		return false, fmt.Errorf("git diff %s: %v: %s", path, err, bytes.TrimSpace(diffErr.Bytes()))
	}
	args := []string{"-C", repo, "apply", "--cached", "--check"}
	if reverse {
		args = append(args, "-R")
	}
	apply := exec.CommandContext(ctx, g.Binary, append(args, "-")...)
	apply.Env = append(os.Environ(), "LC_ALL=C", "GIT_INDEX_FILE="+index)
	apply.Stdin = bytes.NewReader(patch)
	var stderr bytes.Buffer
	apply.Stderr = &stderr
	err = apply.Run()
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git apply --check %s: %v: %s", path, err, bytes.TrimSpace(stderr.Bytes()))
}
