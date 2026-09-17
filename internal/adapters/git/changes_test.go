package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The porcelain v2 rows these tests pin are the ones where an indexing mistake
// is invisible: it does not crash, it names the wrong file or reports a count
// nobody measured. Each case below is a sentence about the format, and the
// fixtures are what `git` actually writes.

func TestParseStatusReadsTheBranchLine(t *testing.T) {
	got := ParseStatus(strings.Join([]string{
		"# branch.oid 45b8906547fe622fabc35d06f6fcd82258db6129",
		"# branch.head clawdline/task/2fc3e801",
		"# branch.ab +3 -11",
	}, "\n"))
	if got.Head != "45b8906547fe622fabc35d06f6fcd82258db6129" {
		t.Errorf("head = %q", got.Head)
	}
	if got.Branch != "clawdline/task/2fc3e801" {
		t.Errorf("branch = %q", got.Branch)
	}
	if got.Ahead != 3 || got.Behind != 11 {
		t.Errorf("ahead/behind = %d/%d, want 3/11", got.Ahead, got.Behind)
	}
}

// `(initial)` is not a commit id. Shown as one it would be eight characters of
// the word "initial" in the branch line of a repository that has no commits.
func TestParseStatusLeavesAnInitialRepositoryWithoutAHead(t *testing.T) {
	got := ParseStatus("# branch.oid (initial)\n# branch.head main\n")
	if got.Head != "" {
		t.Errorf("head = %q, want empty", got.Head)
	}
	if got.Branch != "main" {
		t.Errorf("branch = %q", got.Branch)
	}
}

func TestParseStatusReadsTheFileRows(t *testing.T) {
	got := ParseStatus(strings.Join([]string{
		"1 M. N... 100644 100644 100644 aaaa bbbb internal/app/actions.go",
		"1 .M N... 100644 100644 100644 cccc dddd web/console/src/session/Detail.tsx",
		"1 MM N... 100644 100644 100644 eeee ffff docs/plan.md",
		"1 A. N... 000000 100644 100644 0000 1111 internal/transport/http/git.go",
		"1 .D N... 100644 100644 000000 2222 3333 gone.txt",
		"u UU N... 100644 100644 100644 100644 4444 5555 6666 both.txt",
		"? untracked with spaces.txt",
	}, "\n"))
	want := []File{
		{Path: "internal/app/actions.go", Staged: true, Kind: KindModified},
		{Path: "web/console/src/session/Detail.tsx", Unstaged: true, Kind: KindModified},
		{Path: "docs/plan.md", Staged: true, Unstaged: true, Kind: KindModified},
		{Path: "internal/transport/http/git.go", Staged: true, Kind: KindAdded},
		{Path: "gone.txt", Unstaged: true, Kind: KindDeleted},
		{Path: "both.txt", Staged: true, Unstaged: true, Kind: KindConflict},
		{Path: "untracked with spaces.txt", Unstaged: true, Kind: KindUntracked},
	}
	if len(got.Files) != len(want) {
		t.Fatalf("%d files, want %d: %+v", len(got.Files), len(want), got.Files)
	}
	for i, w := range want {
		if got.Files[i] != w {
			t.Errorf("file %d = %+v, want %+v", i, got.Files[i], w)
		}
	}
}

// A rename's last field is `new path<TAB>old path`, and both halves may hold
// spaces. Splitting the row on spaces past that field names a directory.
func TestParseStatusSplitsARenameOnItsTab(t *testing.T) {
	got := ParseStatus("2 R. N... 100644 100644 100644 aaaa bbbb R100 new name.txt\told name.txt")
	if len(got.Files) != 1 {
		t.Fatalf("%d files, want 1: %+v", len(got.Files), got.Files)
	}
	f := got.Files[0]
	if f.Path != "new name.txt" || f.From != "old name.txt" {
		t.Errorf("path/from = %q/%q", f.Path, f.From)
	}
	if f.Kind != KindRenamed || !f.Staged || f.Unstaged {
		t.Errorf("kind/staged/unstaged = %v/%v/%v", f.Kind, f.Staged, f.Unstaged)
	}
}

func TestParseNumstatKeepsABinaryFileUncounted(t *testing.T) {
	got := ParseNumstat("12\t4\tREADME.md\n-\t-\tlogo.png\n")
	if a := got["README.md"].additions; a == nil || *a != 12 {
		t.Errorf("README additions = %v, want 12", a)
	}
	if got["logo.png"].additions != nil || got["logo.png"].deletions != nil {
		t.Errorf("logo.png counted: %+v", got["logo.png"])
	}
}

// numstat abbreviates a rename; status names the destination. Left unexpanded
// the two answers never join and a renamed file loses its counts.
func TestParseNumstatExpandsARenamedPath(t *testing.T) {
	got := ParseNumstat(strings.Join([]string{
		"1\t1\tdocs/{old => new}.md",
		"2\t0\told/top.md => new/top.md",
	}, "\n"))
	if _, ok := got["docs/new.md"]; !ok {
		t.Errorf("braced rename did not expand: %v", keys(got))
	}
	if _, ok := got["new/top.md"]; !ok {
		t.Errorf("plain rename did not expand: %v", keys(got))
	}
}

func TestAssembleSumsBothSidesOfAPartiallyStagedFile(t *testing.T) {
	got := Assemble(
		"1 MM N... 100644 100644 100644 aaaa bbbb notes.md\n",
		"1\t2\tnotes.md\n",
		"30\t4\tnotes.md\n",
	)
	if len(got.Files) != 1 {
		t.Fatalf("%d files", len(got.Files))
	}
	if a := got.Files[0].Additions; a == nil || *a != 31 {
		t.Errorf("additions = %v, want 31", a)
	}
	if d := got.Files[0].Deletions; d == nil || *d != 6 {
		t.Errorf("deletions = %v, want 6", d)
	}
}

// The case an int64 cannot carry. An untracked file is in neither diff, so it
// has no measurement — and a zero here would draw "+0 −0" on a file nobody has
// measured a line of.
func TestAssembleLeavesAnUnmeasuredFileNull(t *testing.T) {
	got := Assemble("? new.txt\n", "", "")
	if len(got.Files) != 1 {
		t.Fatalf("%d files", len(got.Files))
	}
	if got.Files[0].Additions != nil || got.Files[0].Deletions != nil {
		t.Errorf("counted an untracked file: %+v", got.Files[0])
	}
}

// Once one side of a pair is unknowable, neither is reported: half a
// measurement read as a whole one is worse than no measurement.
func TestAssembleDropsBothCountsWhenOneSideIsBinary(t *testing.T) {
	got := Assemble(
		"1 MM N... 100644 100644 100644 aaaa bbbb logo.png\n",
		"-\t-\tlogo.png\n",
		"3\t1\tlogo.png\n",
	)
	if got.Files[0].Additions != nil || got.Files[0].Deletions != nil {
		t.Errorf("reported half a measurement: %+v", got.Files[0])
	}
}

/* ---- against a real repository ------------------------------------------- */

// The parsers above are pinned against fixtures; this is the one test that
// proves the invocation itself — the flags, the working directory, and that a
// directory with no repository in it is told apart from one that would not
// answer. It builds its own repository in a temporary directory and touches
// nothing else on the machine.
func TestChangesReadsARepositoryItMade(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL="+filepath.Join(dir, "nonexistent-gitconfig"),
			"GIT_CONFIG_SYSTEM="+filepath.Join(dir, "nonexistent-gitconfig"),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	run("init", "-q", "-b", "trunk")
	write("kept.txt", "one\ntwo\n")
	run("add", "kept.txt")
	run("commit", "-qm", "first")
	write("kept.txt", "one\ntwo\nthree\n")
	write("fresh.txt", "new\n")

	got, err := New().Changes(context.Background(), dir)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if got.Branch != "trunk" {
		t.Errorf("branch = %q, want trunk", got.Branch)
	}
	if len(got.Head) != 40 {
		t.Errorf("head = %q, want a commit id", got.Head)
	}
	byPath := map[string]File{}
	for _, f := range got.Files {
		byPath[f.Path] = f
	}
	kept, ok := byPath["kept.txt"]
	if !ok {
		t.Fatalf("kept.txt missing: %+v", got.Files)
	}
	if kept.Kind != KindModified || !kept.Unstaged {
		t.Errorf("kept.txt = %+v", kept)
	}
	if a := kept.Additions; a == nil || *a != 1 {
		t.Errorf("kept.txt additions = %v, want 1", a)
	}
	fresh, ok := byPath["fresh.txt"]
	if !ok {
		t.Fatalf("fresh.txt missing: %+v", got.Files)
	}
	if fresh.Kind != KindUntracked || fresh.Additions != nil {
		t.Errorf("fresh.txt = %+v", fresh)
	}
	// No index.lock left behind: this route is opened by somebody pressing a
	// menu item in a checkout another process may be building in.
	if _, err := os.Stat(filepath.Join(dir, ".git", "index.lock")); !os.IsNotExist(err) {
		t.Errorf("index.lock after a read: %v", err)
	}
}

func TestChangesRefusesADirectoryWithNoRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	// A temporary directory can sit inside somebody's repository; this one is
	// given its own ceiling so the answer is about it and not about its parent.
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", dir)
	inner := filepath.Join(dir, "plain")
	if err := os.Mkdir(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := New().Changes(context.Background(), inner)
	if !errors.Is(err, ErrNotRepository) {
		t.Fatalf("err = %v, want ErrNotRepository", err)
	}
}

func keys(m map[string]numstat) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// **A reading of somebody's repository runs git, and nothing that repository
// asked to have run.**
//
// `core.fsmonitor` names a program and `git status` runs it — twice per read,
// measured here on git 2.38.1 — and `.git/config` is a file anything working in
// that directory can write. The Git panel is opened by a device that may only
// read, on a working directory that on this machine is often a checkout an
// agent made an hour ago (reviewer task 2315c043, F4). So the settings that
// name a program are turned off on the command line, where they outrank the
// repository's own config.
//
// The marker is what the planted program would leave behind. The test asserts
// there is none, and that the reading still answers.
func TestChangesRunsNothingTheRepositoryNames(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "it-ran")
	hostile := filepath.Join(dir, "hostile.sh")
	if err := os.WriteFile(hostile, []byte("#!/bin/sh\necho ran >> \""+marker+"\"\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(dir, "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL="+filepath.Join(dir, "nonexistent-gitconfig"),
			"GIT_CONFIG_SYSTEM="+filepath.Join(dir, "nonexistent-gitconfig"),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "trunk")
	if err := os.WriteFile(filepath.Join(repo, "kept.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "kept.txt")
	run("commit", "-qm", "first")
	if err := os.WriteFile(filepath.Join(repo, "kept.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Everything this repository can say that names a program.
	run("config", "core.fsmonitor", hostile)
	run("config", "diff.external", hostile+" external")
	run("config", "diff.evil.textconv", hostile+" textconv")
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("kept.txt diff=evil\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := New().Changes(context.Background(), repo)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		body, _ := os.ReadFile(marker)
		t.Fatalf("the repository's own program was run by a reading of it: %s", body)
	}
	if len(got.Files) == 0 {
		t.Errorf("the reading answered nothing: %+v", got)
	}
}

// The sink under one command's stdout: what fits is kept, and the moment there
// is more than that the reading says so instead of parsing half a line as a
// whole one. `git status --porcelain=v2` in a home directory is hundreds of
// megabytes, and `cmd.Output()` grew a buffer for all of it (F5).
func TestCappedRefusesMoreThanItHolds(t *testing.T) {
	c := &capped{limit: 8}
	if n, err := c.Write([]byte("12345")); n != 5 || err != nil {
		t.Fatalf("Write(5) = %d, %v", n, err)
	}
	if c.over {
		t.Fatal("five bytes did not fit in eight")
	}
	if _, err := c.Write([]byte("67890")); err == nil {
		t.Fatal("the ninth byte was accepted")
	}
	if !c.over {
		t.Fatal("over was not recorded")
	}
	// A writer that keeps being written to does not panic or grow.
	if _, err := c.Write([]byte("more")); err != nil {
		t.Fatalf("a second write after the limit: %v", err)
	}
	if c.String() != "12345" {
		t.Fatalf("kept %q", c.String())
	}
}
