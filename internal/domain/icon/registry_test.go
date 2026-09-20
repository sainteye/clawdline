package icon

import (
	"os"
	"path/filepath"
	"testing"
)

func registryAt(t *testing.T, body string) *Registry {
	t.Helper()
	path := filepath.Join(t.TempDir(), "project-icons.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Registry{path: path}
}

// The row that wins is the longest registered path containing the directory,
// and the label is that row's own — never the tail of the directory, which in a
// subdirectory is the name of the subdirectory.
func TestMatchAnswersTheLongestRegisteredPath(t *testing.T) {
	r := registryAt(t, `{"projects":{
		"/w/shop":{"label":"Shop"},
		"/w/shop/backend":{"label":"Shop backend"}
	}}`)
	path, label, ok := r.Match("/w/shop/frontend/src")
	if !ok || path != "/w/shop" || label != "Shop" {
		t.Errorf("%q %q %v", path, label, ok)
	}
	path, label, ok = r.Match("/w/shop/backend/api")
	if !ok || path != "/w/shop/backend" || label != "Shop backend" {
		t.Errorf("%q %q %v", path, label, ok)
	}
	if _, _, ok := r.Match("/w/other"); ok {
		t.Error("an unregistered directory matched")
	}
	if _, _, ok := r.Match(""); ok {
		t.Error("an empty directory matched")
	}
}

// A row that names a directory through a symbolic link, asked by a caller whose
// own path has been resolved. Matching one side only is the answer that is not
// "no row" but the wrong no: without the row's own spelling going through the
// same function, this session sees an empty list of snippets.
func TestMatchSpelledComparesBothSidesInOneSpelling(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(filepath.Join(real, "inner"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("this platform would not make a symbolic link: %v", err)
	}
	r := registryAt(t, `{"projects":{"`+link+`":{"label":"Linked"}}}`)
	resolvedRoot, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	spell := func(p string) string {
		if out, err := filepath.EvalSymlinks(filepath.Clean(p)); err == nil {
			return out
		}
		return filepath.Clean(p)
	}
	if _, _, ok := r.Match(filepath.Join(resolvedRoot, "inner")); ok {
		t.Fatal("the plain comparison matched; this test then proves nothing")
	}
	path, label, ok := r.MatchSpelled(filepath.Join(resolvedRoot, "inner"), spell)
	if !ok || path != resolvedRoot || label != "Linked" {
		t.Errorf("%q %q %v, want %q", path, label, ok, resolvedRoot)
	}
}

// Two rows that spell to one directory collapse onto one key, and which of them
// answers is decided rather than left to whichever the map hands over first.
func TestMatchSpelledIsTheSameAnswerTwice(t *testing.T) {
	r := registryAt(t, `{"projects":{
		"/w/shop":{"label":"first"},
		"/w/shop/":{"label":"second"}
	}}`)
	spell := filepath.Clean
	first, label, ok := r.MatchSpelled("/w/shop/src", spell)
	if !ok {
		t.Fatal("no row matched")
	}
	for i := 0; i < 20; i++ {
		path, again, ok := r.MatchSpelled("/w/shop/src", spell)
		if !ok || path != first || again != label {
			t.Fatalf("pass %d answered %q %q, not %q %q", i, path, again, first, label)
		}
	}
}

// A registry row with no label of its own says so, rather than inventing one:
// what to call an unnamed project differs by caller, and Label's own answer for
// one is still empty.
func TestMatchLeavesAnUnnamedRowUnnamed(t *testing.T) {
	r := registryAt(t, `{"projects":{"/w/shop":{}}}`)
	path, label, ok := r.Match("/w/shop")
	if !ok || path != "/w/shop" || label != "" {
		t.Errorf("%q %q %v", path, label, ok)
	}
	if got := r.Label("/w/shop"); got != "" {
		t.Errorf("Label answered %q", got)
	}
}
