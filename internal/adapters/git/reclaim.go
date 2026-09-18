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

// What reclaiming a checkout needs to ask git (docs/design-decisions.md D13,
// cutover B4): which checkouts a repository says it has, what a checkout
// holds beyond its commit — tracked changes and untracked files alike — and a
// way to keep exactly that somewhere a removal cannot reach.
//
// Every question here is asked with `core.fsmonitor` emptied, as the change
// reader asks (changes.go): a repository is somebody else's configuration,
// and a sweep that runs the program a repository names is a sweep with the
// repository's permissions, not this daemon's.

// WorktreeEntry is one checkout as `git worktree list --porcelain` names it.
type WorktreeEntry struct {
	Path     string
	Head     string
	Branch   string // refs/heads/…, empty when detached
	Detached bool
	Locked   bool
	Prunable bool
}

// Worktrees lists every checkout the repository knows. An error is "could not
// ask", never "there are none".
func (g *Git) Worktrees(ctx context.Context, repo string) ([]WorktreeEntry, error) {
	out, err := g.safe(ctx, repo, nil, nil, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var list []WorktreeEntry
	var cur *WorktreeEntry
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "worktree "):
			if cur != nil {
				list = append(list, *cur)
			}
			cur = &WorktreeEntry{Path: filepath.Clean(strings.TrimPrefix(line, "worktree "))}
		case cur == nil:
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(line, "branch ")
		case line == "detached":
			cur.Detached = true
		case line == "locked" || strings.HasPrefix(line, "locked "):
			cur.Locked = true
		case line == "prunable" || strings.HasPrefix(line, "prunable "):
			cur.Prunable = true
		}
	}
	if cur != nil {
		list = append(list, *cur)
	}
	return list, nil
}

// Snapshot is what a checkout holds, as git objects: the commit it is on, that
// commit's tree, and the tree of everything in the checkout that is not
// ignored — tracked edits, deletions and untracked files alike.
//
// Tree == HeadTree is the only "clean" this package gives: `status --porcelain`
// is a report for a person, and a tree id is the thing a later reader can
// compare with and a patch can be checked against.
type Snapshot struct {
	Head     string
	HeadTree string
	Tree     string
}

// Dirty is whether the checkout holds anything its commit does not.
func (s Snapshot) Dirty() bool { return s.Tree != s.HeadTree }

// SnapshotCheckout reads a checkout's whole non-ignored state into a tree,
// through a temporary index so the checkout's own index is never touched.
func (g *Git) SnapshotCheckout(ctx context.Context, checkout string) (Snapshot, error) {
	head, err := g.safeString(ctx, checkout, nil, nil, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return Snapshot{}, err
	}
	headTree, err := g.safeString(ctx, checkout, nil, nil, "rev-parse", "--verify", head+"^{tree}")
	if err != nil {
		return Snapshot{}, err
	}
	dir, err := os.MkdirTemp("", "clawdline-snapshot-")
	if err != nil {
		return Snapshot{}, err
	}
	defer os.RemoveAll(dir)
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(dir, "index")}
	if _, err := g.safe(ctx, checkout, env, nil, "read-tree", head); err != nil {
		return Snapshot{}, err
	}
	if _, err := g.safe(ctx, checkout, env, nil, "add", "--all", "--", "."); err != nil {
		return Snapshot{}, err
	}
	tree, err := g.safeString(ctx, checkout, env, nil, "write-tree")
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Head: head, HeadTree: headTree, Tree: tree}, nil
}

// Tree is the tree a commit names.
func (g *Git) Tree(ctx context.Context, repo, commit string) (string, error) {
	return g.safeString(ctx, repo, nil, nil, "rev-parse", "--verify", "--end-of-options", commit+"^{tree}")
}

// reclaimIdentity is who a preservation commit says made it. Fixed, so the
// commit does not depend on — or fail for want of — a person's git identity.
var reclaimIdentity = []string{
	"GIT_AUTHOR_NAME=Clawdline", "GIT_AUTHOR_EMAIL=clawdline@localhost",
	"GIT_COMMITTER_NAME=Clawdline", "GIT_COMMITTER_EMAIL=clawdline@localhost",
}

// CommitTree makes a commit of tree on top of parent, touching no ref.
func (g *Git) CommitTree(ctx context.Context, repo, tree, parent, message string) (string, error) {
	return g.safeString(ctx, repo, reclaimIdentity, nil, "commit-tree", tree, "-p", parent, "-m", message)
}

// ErrRefExists is a CreateRef that found the ref already there.
var ErrRefExists = errors.New("ref already exists")

// CreateRef points a new ref at commit, refusing when the ref already exists:
// a preservation ref is written once and never moved.
func (g *Git) CreateRef(ctx context.Context, repo, ref, commit string) error {
	if _, err := g.safeString(ctx, repo, nil, nil, "rev-parse", "--verify", "--quiet", ref); err == nil {
		return ErrRefExists
	}
	_, err := g.safe(ctx, repo, nil, []byte("create "+ref+" "+commit+"\n"), "update-ref", "--stdin")
	return err
}

// RefCommit is the commit a ref names, or an error when it names none.
func (g *Git) RefCommit(ctx context.Context, repo, ref string) (string, error) {
	return g.safeString(ctx, repo, nil, nil, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
}

// BinaryDiff is the patch from one commit to another, in the form `git apply`
// reads back byte for byte: binary hunks, full object ids, no external diff
// program and no text conversion.
func (g *Git) BinaryDiff(ctx context.Context, repo, from, to string) ([]byte, error) {
	return g.safe(ctx, repo, nil, nil, "diff", "--binary", "--full-index", "--no-ext-diff", "--no-textconv",
		"--no-color", from, to, "--")
}

// ApplyToTree applies a patch to a commit's tree in a temporary index and
// answers the tree that results. It is how a saved patch is proved: applied to
// the commit it was taken from, it must give exactly the tree it was taken to.
func (g *Git) ApplyToTree(ctx context.Context, repo, base string, patch []byte) (string, error) {
	baseTree, err := g.Tree(ctx, repo, base)
	if err != nil {
		return "", err
	}
	if len(patch) == 0 {
		return baseTree, nil
	}
	dir, err := os.MkdirTemp("", "clawdline-apply-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(dir, "index")}
	if _, err := g.safe(ctx, repo, env, nil, "read-tree", baseTree); err != nil {
		return "", err
	}
	if _, err := g.safe(ctx, repo, env, patch, "apply", "--cached", "--binary", "-"); err != nil {
		return "", err
	}
	return g.safeString(ctx, repo, env, nil, "write-tree")
}

// PruneWorktrees drops the bookkeeping of checkouts whose directory is gone.
// It is asked only after a removal this package made, so what it drops is the
// entry that removal left.
func (g *Git) PruneWorktrees(ctx context.Context, repo string) error {
	_, err := g.safe(ctx, repo, nil, nil, "worktree", "prune")
	return err
}

// Filtered answers whether the repository names a content filter anywhere
// git would apply one — a checked-in or untracked `.gitattributes` in the
// checkout, the repository's `info/attributes`, or the `core.attributesFile`
// it configures. A filter runs a program the repository chose, and it decides
// what "clean" means: a notebook whose outputs a filter strips reads as
// unchanged while its outputs are the only copy. So a checkout with one is
// not read at all; the answer is only whether one is named.
func (g *Git) Filtered(ctx context.Context, checkout string) (bool, error) {
	named := func(body []byte) bool { return bytes.Contains(body, []byte("filter=")) }
	found := false
	err := filepath.WalkDir(checkout, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == ".gitattributes" {
			body, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if named(body) {
				found = true
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil || found {
		return found, err
	}
	common, err := g.safeString(ctx, checkout, nil, nil, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return false, err
	}
	if body, err := os.ReadFile(filepath.Join(common, "info", "attributes")); err == nil && named(body) {
		return true, nil
	}
	if file, err := g.safeString(ctx, checkout, nil, nil, "config", "--path", "--get", "core.attributesFile"); err == nil && file != "" {
		if body, err := os.ReadFile(file); err == nil && named(body) {
			return true, nil
		}
	}
	return false, nil
}

// NestedRepository names the first directory inside a checkout that is a
// repository of its own — a submodule or a clone somebody made there. Its
// own work is not in the checkout's tree (a snapshot records a gitlink, the
// other repository's commit, and nothing it has not committed), and
// `worktree remove --force` takes it anyway, so a checkout holding one is
// never the snapshot's to vouch for.
func NestedRepository(checkout string) (string, error) {
	found := ""
	err := filepath.WalkDir(checkout, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == ".git" && filepath.Dir(p) != filepath.Clean(checkout) {
			found = filepath.Dir(p)
			return filepath.SkipAll
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		return nil
	})
	return found, err
}

// safe runs git in dir with core.fsmonitor emptied and hooks pointed at
// nothing, extra environment and an optional stdin, and answers stdout.
// stderr is kept for the error.
func (g *Git) safe(ctx context.Context, dir string, env []string, stdin []byte, args ...string) ([]byte, error) {
	full := append([]string{"-c", "core.fsmonitor=", "-c", "core.hooksPath=/dev/null"}, args...)
	cmd := exec.CommandContext(ctx, g.Binary, full...)
	cmd.Dir = dir
	cmd.Env = append(append(cmd.Environ(), "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0"), env...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		said := strings.TrimSpace(stderr.String())
		if said == "" {
			said = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", args[0], said)
	}
	return out, nil
}

func (g *Git) safeString(ctx context.Context, dir string, env []string, stdin []byte, args ...string) (string, error) {
	out, err := g.safe(ctx, dir, env, stdin, args...)
	return strings.TrimSpace(string(out)), err
}
