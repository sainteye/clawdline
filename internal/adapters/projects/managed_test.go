package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// managedHome is a made-up home with this daemon's state directory in it. The
// temporary directory is moved out from under the test's tree, because it is
// a scratch root and would otherwise keep every directory here off the list.
func managedHome(t *testing.T) (home, own string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if base == "/tmp" || strings.HasPrefix(base, "/tmp/") || strings.HasPrefix(base, "/private/tmp/") {
		t.Skip("the temporary directory is under /tmp, which is never a place")
	}
	t.Setenv("TMPDIR", filepath.Join(base, "scratch"))
	home = filepath.Join(base, "home")
	t.Setenv("HOME", home)
	return home, filepath.Join(home, ".config", "clawdline-next", "worktrees")
}

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

const (
	testSlug = "repo-11111111"
	testTask = "c6000002-0000-4000-8000-000000000002"
)

// A directory is a broker's checkout when it lies at or below a managed root,
// compared a whole component at a time — not when it is named like one.
func TestTheManagedRootsAreJudgedByWhereAPathIs(t *testing.T) {
	home, own := managedHome(t)
	swift := SwiftWorktreeRoot()
	cases := []struct {
		name    string
		path    string
		durable bool
	}{
		{"this daemon's checkout", filepath.Join(own, testSlug, testTask), false},
		{"a directory inside this daemon's checkout", filepath.Join(own, testSlug, testTask, "internal"), false},
		{"the Swift app's checkout", filepath.Join(swift, testSlug, testTask), false},
		{"a project in a directory named worktrees", filepath.Join(home, "code", "worktrees", "foodblogs"), true},
		{"a project shaped like a checkout, elsewhere", filepath.Join(home, "src", "worktrees", testSlug, testTask), true},
		{"a sibling whose name starts with the root's", filepath.Join(own+"-old", testSlug, testTask), true},
	}
	for _, c := range cases {
		mkdirs(t, c.path)
	}
	durable := durableJudge(ManagedWorktreeRoots(own))
	for _, c := range cases {
		if got := durable(c.path); got != c.durable {
			t.Errorf("%s: durable %v, want %v (%s)", c.name, got, c.durable, c.path)
		}
	}

	// Without this daemon's root only the Swift app's is known, which is the
	// reading that let this daemon's checkouts onto the list.
	if !durableJudge(ManagedWorktreeRoots(""))(cases[0].path) {
		t.Error("the Swift app's root alone already keeps this daemon's checkout off; the case tests nothing")
	}
}

// The worktree lifecycle calls a checkout under either broker's root managed,
// and ties it to the task that recorded it; one outside both is foreign.
func TestTheLifecycleOwnsCheckoutsUnderEitherRoot(t *testing.T) {
	home, own := managedHome(t)
	fresh := filepath.Join(own, testSlug, testTask)
	swift := filepath.Join(SwiftWorktreeRoot(), testSlug, testTask)
	lookalike := filepath.Join(home, "src", "worktrees", testSlug, testTask)
	mkdirs(t, fresh, swift, lookalike)

	l := NewLifecycle(Ports{ManagedWorktreeRoots: ManagedWorktreeRoots(own)})
	branch := worktreeBranch(testTask)
	for _, c := range []struct {
		name, path, kind string
	}{
		{"this daemon's checkout", fresh, "task"},
		{"the Swift app's checkout", swift, "task"},
		{"a checkout-shaped directory elsewhere", lookalike, "foreign"},
	} {
		tasks := TaskEvidence{Authoritative: true, Tasks: []Task{{ID: testTask, State: "success",
			Worktree: &TaskWorktree{Path: c.path, Branch: branch}}}}
		got := l.resolveOwner(registered{path: c.path, branch: branch}, false, tasks)
		if got.kind != c.kind {
			t.Errorf("%s: owner %s (%s), want %s", c.name, got.kind, got.evidence, c.kind)
		}
	}
}
