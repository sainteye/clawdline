package projects

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A folder in Dropbox where Claude Code was once run is not listed as a
// Project, so nothing reads it without the person choosing it: on 2026-10-15
// such a folder made macOS ask the person, on a reinstall, to let Clawdline
// read Dropbox. recorded is asked directly: on Linux the test's home is under
// /tmp, which the listing's own scratch rule drops whole.
func TestAFolderFoundUnderLibraryIsNotListed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dropbox := filepath.Join(home, "Library", "CloudStorage", "Dropbox", "work")
	code := filepath.Join(home, "code", "app")
	for _, dir := range []string{dropbox, code} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		folder := filepath.Join(home, ".claude", "projects", Slug(dir))
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		row := `{"type":"user","cwd":"` + dir + `","message":{"role":"user","content":"hi"}}` + "\n"
		if err := os.WriteFile(filepath.Join(folder, "a.jsonl"), []byte(row), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p := NewPlaces(func(string) string { return "" }, nil)
	found := map[string]bool{}
	for _, place := range p.recorded(60, 240) {
		found[place.Path] = true
	}
	if !found[code] || found[dropbox] {
		t.Fatalf("found %v; want %s and not %s", found, code, dropbox)
	}
	if !InUserLibrary(dropbox) || InUserLibrary(code) || InUserLibrary(filepath.Join(home, "LibraryNotes")) {
		t.Fatal("InUserLibrary does not tell ~/Library from its neighbours")
	}
}

// Attribute reads count too: the resolver must never be asked to enter a
// protected root merely to calculate which transcript slugs to exclude.
func TestScratchMarksDoNotResolvePersonalData(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	protected := []string{filepath.Join(home, "Documents", "Codex"), SwiftWorktreeRoot()}
	marks := scratchMarksWithResolver(nil, func(path string) string {
		for _, root := range protected {
			if path == root {
				t.Errorf("resolved personal-data root: %s", path)
			}
		}
		return filepath.Clean(path)
	})
	for _, root := range protected {
		found := false
		for _, mark := range marks {
			if mark == Slug(root) {
				found = true
			}
		}
		if !found {
			t.Errorf("lost lexical scratch exclusion for %s", root)
		}
	}
}

func TestRecordedPersonalFoldersDoNotReachTheLabelReader(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"Desktop", "Documents", "Downloads", "Pictures", "Music", "Movies", "Library"} {
		dir := filepath.Join(home, name, "project")
		folder := filepath.Join(home, ".claude", "projects", Slug(dir))
		if err := os.MkdirAll(folder, 0700); err != nil {
			t.Fatal(err)
		}
		row := `{"type":"user","cwd":"` + filepath.ToSlash(dir) + `","message":{"role":"user","content":"hi"}}` + "\n"
		if err := os.WriteFile(filepath.Join(folder, "a.jsonl"), []byte(row), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p := NewPlaces(func(path string) string { t.Errorf("inspected unchosen personal folder: %s", path); return "" }, nil)
	if got := p.recorded(60, 240); len(got) != 0 {
		t.Errorf("offered %d personal folders", len(got))
	}
}

func TestBackgroundPolicyMakesNoCallsInsidePersonalRoots(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"Desktop", "Documents", "Downloads", "Pictures", "Music", "Movies", "Library"} {
		for _, tail := range []string{"", "project", filepath.Join("project", "child")} {
			path := filepath.Join(home, name, tail)
			if backgroundPath(path, home, func(path string) (os.FileInfo, error) {
				t.Fatalf("filesystem called for %s", path)
				return nil, os.ErrPermission
			}) {
				t.Errorf("allowed %s", path)
			}
		}
	}
	if needsExplicitChoice(filepath.Join(home, "MusicNotes"), home) {
		t.Fatal("a neighbouring name was treated as personal data")
	}
}

func TestBackgroundPolicyDoesNotFollowAnAlias(t *testing.T) {
	home := t.TempDir()
	code := filepath.Join(home, "code")
	music := filepath.Join(home, "Music")
	mkdirs(t, code, music)
	alias := filepath.Join(code, "alias")
	if err := os.Symlink(music, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, path := range []string{alias, filepath.Join(alias, "nested")} {
		if backgroundPath(path, home, func(path string) (os.FileInfo, error) {
			if needsExplicitChoice(path, home) {
				t.Fatalf("followed alias into %s", path)
			}
			return os.Lstat(path)
		}) {
			t.Errorf("allowed alias %s", path)
		}
	}
	if !backgroundPath(code, home, os.Lstat) {
		t.Fatal("ordinary code directory was refused")
	}
}

func TestAnExplicitPersonalProjectIsStillListed(t *testing.T) {
	home, _ := managedHome(t)
	music := filepath.Join(home, "Music", "project")
	mkdirs(t, music)
	p := NewPlaces(nil, nil)
	if got := p.List([]string{music}, 40); len(got) != 0 {
		t.Fatalf("live cwd became consent: %v", got)
	}
	p.Registered = func() ([]RegisteredPlace, error) { return []RegisteredPlace{{Path: music, AddedAt: 1}}, nil }
	if got := p.List([]string{music}, 40); len(got) != 1 || got[0].Path != music {
		t.Fatalf("explicit choice was lost: %v", got)
	}
}

func TestMacOSBackgroundPolicyRejectsCaseAliasesAndMountedVolumes(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS protected-path spellings")
	}
	home := t.TempDir()
	for _, path := range []string{filepath.Join(home, "mUsIc", "project"), filepath.Join(home, "lIbRaRy", "project"), "/Volumes/External/project", "/volumes/Network/project"} {
		if backgroundPath(path, home, func(path string) (os.FileInfo, error) {
			t.Fatalf("touched protected spelling: %s", path)
			return nil, os.ErrPermission
		}) {
			t.Errorf("allowed %s", path)
		}
	}
}
