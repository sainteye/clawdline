package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

// Every entry here is invented: the store holds a person's lessons, and
// none of theirs belongs in a public repository.
func invented(name string, t Type) Entry {
	return Entry{Name: name, Description: "an invented lesson about " + name, Type: t, Body: "Invented body for " + name + ".\n"}
}

const testKey = "scratch-project-0123abcd"

func TestAnEntryRoundTripsThroughItsFile(t *testing.T) {
	s := New(t.TempDir())
	e := Entry{Name: "quote-in-description", Description: `says "hi": twice`, Type: TypeReference,
		Body: "line one\n\n---\nline after a rule\n"}
	if out, err := s.Add(testKey, e); err != nil || out != Created {
		t.Fatalf("add: %v %v", out, err)
	}
	got, err := s.Get(testKey, e.Name)
	if err != nil || got != e {
		t.Fatalf("got %+v, %v; want %+v", got, err, e)
	}
	for _, d := range []string{`"quoted all the way"`, "> looks folded", "'single'"} {
		odd := Entry{Name: "odd-description", Description: d, Type: TypeUser, Body: "b\n"}
		if _, err := s.Add(testKey, odd); err != nil {
			t.Fatal(err)
		}
		if got, err := s.Get(testKey, odd.Name); err != nil || got != odd {
			t.Fatalf("description %q read back as %q (%v)", d, got.Description, err)
		}
		if _, err := s.Forget(testKey, odd.Name); err != nil {
			t.Fatal(err)
		}
	}
	dir, _ := s.Dir(testKey)
	if st, err := os.Stat(filepath.Join(dir, e.Name+".md")); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("entry file: %v %v", st, err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".tmp-*")); len(left) != 0 {
		t.Fatalf("temporary files left behind: %v", left)
	}
}

func TestWritesAreIdempotentAndRefusalsAreTyped(t *testing.T) {
	s := New(t.TempDir())
	e := invented("first-lesson", TypeFeedback)
	if out, err := s.Add(testKey, e); err != nil || out != Created {
		t.Fatalf("add: %v %v", out, err)
	}
	// A retry after a lost answer: the same entry, without its trailing newline.
	retry := e
	retry.Body = strings.TrimRight(e.Body, "\n")
	if out, err := s.Add(testKey, retry); err != nil || out != Unchanged {
		t.Fatalf("the same add again: %v %v", out, err)
	}
	other := e
	other.Body = "a different body"
	if _, err := s.Add(testKey, other); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("a different entry under the same name: %v", err)
	}
	if got, _ := s.Get(testKey, e.Name); got.Body != e.Body {
		t.Fatalf("a refused add changed the entry: %q", got.Body)
	}
	if out, err := s.Update(testKey, other); err != nil || out != Updated {
		t.Fatalf("update: %v %v", out, err)
	}
	if _, err := s.Update(testKey, invented("never-added", TypeUser)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update unknown: %v", err)
	}
	if _, err := s.Get(testKey, "never-added"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get unknown: %v", err)
	}
	if out, err := s.Forget(testKey, e.Name); err != nil || out != Forgotten {
		t.Fatalf("forget: %v %v", out, err)
	}
	if _, err := s.Forget(testKey, e.Name); !errors.Is(err, ErrNotFound) {
		t.Fatalf("forget twice: %v", err)
	}
	for _, bad := range []Entry{
		{Name: "Has Caps", Description: "d", Type: TypeUser, Body: "b"},
		{Name: "../escape", Description: "d", Type: TypeUser, Body: "b"},
		{Name: "trailing-", Description: "d", Type: TypeUser, Body: "b"},
		{Name: strings.Repeat("a", 65), Description: "d", Type: TypeUser, Body: "b"},
		{Name: "ok", Description: "two\nlines", Type: TypeUser, Body: "b"},
		{Name: "ok", Description: "", Type: TypeUser, Body: "b"},
		{Name: "ok", Description: strings.Repeat("d", DescriptionByteLimit+1), Type: TypeUser, Body: "b"},
		{Name: "ok", Description: "d", Type: "opinion", Body: "b"},
		{Name: "ok", Description: "d", Type: TypeUser, Body: "  \n"},
		{Name: "ok", Description: "d", Type: TypeUser, Body: strings.Repeat("b", EntryBodyLimit+1)},
	} {
		if _, err := s.Add(testKey, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q / %q / %q: %v, want ErrInvalid", bad.Name, bad.Description, bad.Type, err)
		}
	}
	if _, err := s.Add("../outside", e); !errors.Is(err, ErrBadKey) {
		t.Fatalf("a key that is not a repo key: %v", err)
	}
}

func TestAFullProjectRefusesOneMore(t *testing.T) {
	s := New(t.TempDir())
	for i := 0; i < EntryCountLimit; i++ {
		if _, err := s.Add(testKey, invented(fmt.Sprintf("lesson-%03d", i), TypeProject)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Add(testKey, invented("one-too-many", TypeProject)); !errors.Is(err, ErrFull) {
		t.Fatalf("one past the limit: %v", err)
	}
	// An update of an entry already there is not one more.
	changed := invented("lesson-000", TypeProject)
	changed.Body = "changed"
	if _, err := s.Update(testKey, changed); err != nil {
		t.Fatalf("an update in a full store: %v", err)
	}
}

func TestTheIndexIsGroupedByTypeAndWrittenAfterEveryWrite(t *testing.T) {
	s := New(t.TempDir())
	for _, e := range []Entry{invented("zeta", TypeReference), invented("alpha", TypeProject),
		invented("beta", TypeUser), invented("gamma", TypeFeedback), invented("aardvark", TypeUser)} {
		if _, err := s.Add(testKey, e); err != nil {
			t.Fatal(err)
		}
	}
	text, n, cut, err := s.Index(testKey)
	if err != nil || n != 5 || cut {
		t.Fatalf("index: %d %v %v", n, cut, err)
	}
	want := indexHeading + "\n## user\n\n- aardvark — an invented lesson about aardvark\n- beta — an invented lesson about beta\n" +
		"\n## feedback\n\n- gamma — an invented lesson about gamma\n" +
		"\n## project\n\n- alpha — an invented lesson about alpha\n" +
		"\n## reference\n\n- zeta — an invented lesson about zeta\n"
	if text != want {
		t.Fatalf("index:\n%s\nwant:\n%s", text, want)
	}
	dir, _ := s.Dir(testKey)
	if onDisk, err := os.ReadFile(filepath.Join(dir, IndexFile)); err != nil || string(onDisk) != want {
		t.Fatalf("INDEX.md: %q %v", onDisk, err)
	}
	for _, name := range []string{"zeta", "alpha", "beta", "gamma", "aardvark"} {
		if _, err := s.Forget(testKey, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, IndexFile)); !os.IsNotExist(err) {
		t.Fatalf("an empty store kept its index: %v", err)
	}
}

func TestAnIndexPastItsBoundIsCutAtALineAndSaysSo(t *testing.T) {
	s := New(t.TempDir())
	for i := 0; i < 200; i++ {
		e := invented(fmt.Sprintf("lesson-%03d", i), TypeFeedback)
		e.Description = strings.Repeat("d", 80)
		if _, err := s.Add(testKey, e); err != nil {
			t.Fatal(err)
		}
	}
	text, n, cut, err := s.Index(testKey)
	if err != nil || n != 200 || !cut {
		t.Fatalf("index: %d %v %v", n, cut, err)
	}
	if len(text) > IndexByteLimit {
		t.Fatalf("the index is %d bytes, past %d", len(text), IndexByteLimit)
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	last := lines[len(lines)-1]
	listed := strings.Count(text, "\n- lesson-")
	if want := fmt.Sprintf("… %d more resident entries are not shown", 200-listed); !strings.HasPrefix(last, want) {
		t.Fatalf("the last line is %q, want it to start %q", last, want)
	}
	for _, l := range lines[:len(lines)-1] {
		if strings.HasPrefix(l, "- ") && !strings.HasSuffix(l, strings.Repeat("d", 80)) {
			t.Fatalf("a line was cut: %q", l)
		}
	}
}

// Every write is serialized: concurrent adds of different names all land and
// the index lists every one of them.
func TestConcurrentWritesAllLandInTheIndex(t *testing.T) {
	s := New(t.TempDir())
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Add(testKey, invented(fmt.Sprintf("parallel-%02d", i), TypeProject))
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	dir, _ := s.Dir(testKey)
	onDisk, err := os.ReadFile(filepath.Join(dir, IndexFile))
	if err != nil || strings.Count(string(onDisk), "\n- parallel-") != 32 {
		t.Fatalf("INDEX.md lists %d of 32: %v", strings.Count(string(onDisk), "\n- parallel-"), err)
	}
}

// Unknown is not zero: a store that cannot be listed is an error, and a
// launch is told nothing rather than told the Project has no memory.
func TestAStoreThatCannotBeReadIsAnErrorNotAnEmptyList(t *testing.T) {
	state := t.TempDir()
	s := New(state)
	dir, _ := s.Dir(testKey)
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := s.List(testKey); err == nil {
		t.Fatalf("listed %v from a store that is a file", got)
	}
	if _, _, _, err := s.Index(testKey); err == nil {
		t.Fatal("indexed a store that is a file")
	}
}

// The capacity row reads the fullest Project; no store is a known zero and a
// store root it cannot read is an error, not a zero.
func TestMostEntriesIsTheFullestProject(t *testing.T) {
	state := t.TempDir()
	s := New(state)
	if n, err := s.MostEntries(); err != nil || n != 0 {
		t.Fatalf("no store: %d %v", n, err)
	}
	for i := range 3 {
		if _, err := s.Add(testKey, invented(fmt.Sprintf("lesson-%d", i), TypeProject)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Add("another-project-89abcdef", invented("only-one", TypeUser)); err != nil {
		t.Fatal(err)
	}
	if n, err := s.MostEntries(); err != nil || n != 3 {
		t.Fatalf("two Projects: %d %v, want 3", n, err)
	}
	blocked := New(t.TempDir())
	if err := os.WriteFile(blocked.root, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err := blocked.MostEntries(); err == nil {
		t.Fatalf("read %d from a store root that is a file", n)
	}
}

func TestAStoreWithNoDirectoryHasNoEntries(t *testing.T) {
	got, err := New(t.TempDir()).List(testKey)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
}

// The store follows the repository: the main checkout and a linked worktree
// of it are one key, the broker's worktree slug.
func TestALinkedWorktreeSharesItsRepositorysMemory(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "My Repo")
	admin := filepath.Join(repo, ".git", "worktrees", "child")
	linked := filepath.Join(root, "worktrees", "child")
	for _, d := range []string{admin, linked} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(admin, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked, ".git"), []byte("gitdir: "+admin+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	main, ok1 := KeyFor(repo)
	child, ok2 := KeyFor(filepath.Join(linked))
	if !ok1 || !ok2 || main != child || main != RepoKey(repo) || !strings.HasPrefix(main, "my-repo-") {
		t.Fatalf("main %q, linked %q", main, child)
	}
}

// What a launch is given: the instruction, then the index; "" for none.
func TestLaunchTextIsTheInstructionAndTheIndexOrNothing(t *testing.T) {
	state := t.TempDir()
	repo, _ := filepath.EvalSymlinks(t.TempDir())
	s := New(state)
	if got, err := s.LaunchText(repo); got != "" || err != nil {
		t.Fatalf("no entries: %q %v", got, err)
	}
	key, _ := KeyFor(repo)
	if _, err := s.Add(key, invented("a-lesson", TypeProject)); err != nil {
		t.Fatal(err)
	}
	got, err := s.LaunchText(repo)
	if err != nil || !strings.HasPrefix(got, LaunchInstruction+"\n\n"+indexHeading) ||
		!strings.Contains(got, "- a-lesson — an invented lesson about a-lesson") {
		t.Fatalf("launch text %q %v", got, err)
	}
	if LaunchTextLimit > projects.MemoryLaunchLimit {
		t.Fatalf("a full index (%d bytes) is past what a launch carries (%d)", LaunchTextLimit, projects.MemoryLaunchLimit)
	}
}
