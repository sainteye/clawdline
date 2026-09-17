package documents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The cases here are the ones a hostile name would try, and each is written so
// that it can fail on the code it is about: every refusal is checked against a
// fixture where the file it names really exists, so a test that passes because
// the target was missing anyway is not a pass this file can produce.

// fixture builds `<dir>/artifacts` with a document in it and a secret beside
// it, outside the root.
func fixture(t *testing.T) (dir, root string) {
	t.Helper()
	dir = t.TempDir()
	root = filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "notes.md"), "# notes")
	write(t, filepath.Join(root, "plan.txt"), "plan")
	write(t, filepath.Join(root, "sub", "deep.markdown"), "deep")
	write(t, filepath.Join(root, ".hidden.md"), "hidden")
	write(t, filepath.Join(root, "program.html"), "<script>")
	write(t, filepath.Join(dir, "secret.md"), "not yours")
	return dir, ProjectRoot(dir, false)
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFileServesWhatIsInTheRoot(t *testing.T) {
	_, root := fixture(t)
	for _, name := range []string{"notes.md", "plan.txt", "sub/deep.markdown"} {
		located, err := File(root, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if located.Path != name {
			t.Fatalf("%s: came back as %q", name, located.Path)
		}
	}
}

func TestFileRefusesEverythingOutsideTheRoot(t *testing.T) {
	dir, root := fixture(t)
	// The file each of these is reaching for exists, so a refusal is a refusal
	// rather than an absence.
	if _, err := os.Stat(filepath.Join(dir, "secret.md")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"../secret.md",
		"sub/../../secret.md",
		"./notes.md",
		"/etc/hosts",
		"",
		"sub//deep.markdown",
		".hidden.md",
		"program.html",
		"a/b/c/d/e/f/g.md",
		strings.Repeat("a", 520) + ".md",
		"notes.md\x00.txt",
	} {
		if _, err := File(root, name); err != NotFound {
			t.Fatalf("%q was not refused: %v", name, err)
		}
	}
}

// A symlink inside the root that points out of it is the case a comparison of
// spellings cannot see and a comparison of resolved paths can.
func TestFileRefusesASymlinkOutOfTheRoot(t *testing.T) {
	dir, root := fixture(t)
	if err := os.Symlink(filepath.Join(dir, "secret.md"), filepath.Join(root, "escape.md")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if _, err := File(root, "escape.md"); err != NotFound {
		t.Fatalf("a symlink out of the root was served: %v", err)
	}
	// And the walk does not offer it either, which is the property that makes
	// the listing safe to turn into links.
	for _, row := range Walk(context.Background(), root) {
		if row.Path == "escape.md" {
			t.Fatal("the listing offered a document the read refuses")
		}
	}
}

// A symlinked root is followed when this machine has asked for that, which is
// the app being replicated's own behaviour — a person saying "my documents live
// over there" — and everything under what it resolves to is then inside.
func TestProjectRootFollowsItsSymlinkWhenAsked(t *testing.T) {
	dir := t.TempDir()
	elsewhere := filepath.Join(dir, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(elsewhere, "notes.md"), "# over there")
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(home, "artifacts")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	root := ProjectRoot(home, false)
	if root == "" {
		t.Fatal("a symlinked project root was not followed")
	}
	if _, err := File(root, "notes.md"); err != nil {
		t.Fatalf("notes.md under the followed root: %v", err)
	}
	// And the machine that would rather lose the symlink says so, once.
	if root := ProjectRoot(home, true); root != "" {
		t.Fatalf("a contained project root left the session's directory: %q", root)
	}
}

// The floor under a followed root: a symlink may say where this project's
// documents are and may not say "everything above you". `ln -s ~ artifacts`
// in a session's working directory is the finding this pins (reviewer task
// 2315c043, F11) — one line an agent working in that directory could write,
// after which every document under a home directory is readable by a device
// that may only read.
func TestProjectRootRefusesARootThatHoldsTheAsker(t *testing.T) {
	dir := t.TempDir()
	inside := filepath.Join(dir, "deep", "session")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir, filepath.Join(inside, "artifacts")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if root := ProjectRoot(inside, false); root != "" {
		t.Fatalf("a project root above the session was followed: %q", root)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return
	}
	elsewhere := t.TempDir()
	if err := os.Symlink(home, filepath.Join(elsewhere, "artifacts")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if root := ProjectRoot(elsewhere, false); root != "" {
		t.Fatalf("a project root at the home directory was followed: %q", root)
	}
}

// A task's root is the one place the symlink is not followed, because the party
// that could put it there is the party this boundary bounds.
func TestTaskRootRefusesASymlinkOutOfTheTaskDirectory(t *testing.T) {
	dir := t.TempDir()
	elsewhere := filepath.Join(dir, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	task := filepath.Join(dir, "task")
	if err := os.MkdirAll(task, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(task, "artifacts")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if root := TaskRoot(task); root != "" {
		t.Fatalf("a task root left its task directory: %q", root)
	}
	// The same directory, asked for as a project, is followed — which is the
	// whole of the difference between the two calls.
	if root := ProjectRoot(task, false); root == "" {
		t.Fatal("the project root stopped following its symlink")
	}
}

// `…/artifacts-elsewhere` must not pass as being inside `…/artifacts`; the
// trailing separator is the whole of that test.
func TestIsInsideNeedsASeparator(t *testing.T) {
	if IsInside("/a/artifacts-elsewhere/x.md", "/a/artifacts") {
		t.Fatal("a sibling directory passed as being inside")
	}
	if !IsInside("/a/artifacts/x.md", "/a/artifacts") {
		t.Fatal("a file inside its root was called outside")
	}
	if IsInside("/a/artifacts", "/a/artifacts") {
		t.Fatal("a root is not inside itself")
	}
}

func TestFileRefusesSomethingTooLarge(t *testing.T) {
	_, root := fixture(t)
	write(t, filepath.Join(root, "huge.md"), strings.Repeat("x", MaximumBytes+1))
	if _, err := File(root, "huge.md"); err != TooLarge {
		t.Fatalf("an oversized document was not refused with its own code: %v", err)
	}
	if TooLarge.Status() != 413 || NotFound.Status() != 404 {
		t.Fatal("the two refusals lost their statuses")
	}
}

// The listing is a subset of what the read will serve. That direction is what
// makes a row safe to turn into a link, and it is checked rather than assumed.
func TestWalkOffersOnlyWhatTheReadServes(t *testing.T) {
	_, root := fixture(t)
	found := Walk(context.Background(), root)
	if len(found) != 3 {
		t.Fatalf("expected the three readable documents, got %d: %v", len(found), found)
	}
	for _, row := range found {
		if _, err := File(root, row.Path); err != nil {
			t.Fatalf("the listing offered %q, which the read refuses: %v", row.Path, err)
		}
	}
	// Newest first, and by path when two share a moment.
	for i := 1; i < len(found); i++ {
		if found[i-1].Modified < found[i].Modified {
			t.Fatal("the listing is not newest first")
		}
	}
}

func TestMediaTypeIsOneOfTheTwoThePageAccepts(t *testing.T) {
	if MediaType("a/b.txt") != "text/plain; charset=utf-8" {
		t.Fatal("txt is not plain text")
	}
	for _, name := range []string{"a.md", "a.MARKDOWN", "a/b.Md"} {
		if MediaType(name) != "text/markdown; charset=utf-8" {
			t.Fatalf("%s is not markdown", name)
		}
	}
}

func TestEscapedKeepsAnOrdinaryNameOrdinary(t *testing.T) {
	if got := Escaped("2026-09-16-notes.md"); got != "2026-09-16-notes.md" {
		t.Fatalf("an ordinary name was escaped: %q", got)
	}
	if got := Escaped("a b/c#d?e.md"); got != "a%20b/c%23d%3Fe.md" {
		t.Fatalf("a hostile name was not escaped: %q", got)
	}
}

func TestResolvedPathIsAFixedPoint(t *testing.T) {
	// The listing depends on this: it resolves the root once and every entry
	// once more, and against an alternating call no entry would begin with its
	// own root.
	dir := t.TempDir()
	once := ResolvedPath(dir)
	if twice := ResolvedPath(once); twice != once {
		t.Fatalf("resolving twice moved: %q -> %q", once, twice)
	}
}
