package projectsync

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/icon"
)

func TestRepoSpellsTheRepositoryNotTheURL(t *testing.T) {
	same := []string{
		"git@github.com:Owner/Name.git",
		"https://github.com/owner/name",
		"https://user:fake-password@github.com/owner/name.git/",
		"ssh://git@github.com:22/owner/name.git",
		"github.com:owner/name",
	}
	for _, remote := range same {
		got, err := Repo(remote)
		if err != nil || got != "github.com/owner/name" {
			t.Errorf("Repo(%q) = %q, %v; want github.com/owner/name", remote, got, err)
		}
	}
	for _, remote := range []string{"/srv/git/name.git", "file:///srv/git/name", "../name", "C:/repos/name", "", "https://github.com/"} {
		if got, err := Repo(remote); err == nil {
			t.Errorf("Repo(%q) = %q; a remote only this machine can reach must not be an identity", remote, got)
		}
	}
}

func TestOnlyProjectLocalPlacesAreCarried(t *testing.T) {
	for _, p := range []string{"CLAUDE.local.md", ".claude/skills/a/SKILL.md", ".claude/commands/x.md", ".claude/agents/r.md"} {
		if !Allowed(p) {
			t.Errorf("%q should be carried", p)
		}
	}
	for _, p := range []string{".claude/settings.local.json", ".claude/skills/../../etc/passwd", "/etc/passwd",
		".claude/skills", ".claude/skills/./a", "src/main.go", ".claude/skills/.git/config", ".claude\\skills\\a", ""} {
		if Allowed(p) {
			t.Errorf("%q must not be carried", p)
		}
	}
}

func grid(c string) icon.Grid {
	return icon.Grid{Accent: c, Cells: [][]*string{{&c}}}
}

func TestPlanKeepsWhatWasEditedHere(t *testing.T) {
	e := Entry{Files: []File{{Path: "CLAUDE.local.md", SHA256: "new"}, {Path: ".claude/skills/a/SKILL.md", SHA256: "a2"},
		{Path: ".claude/skills/b/SKILL.md", SHA256: "b1"}, {Path: ".claude/commands/t.md", SHA256: "t1"}}}
	owned := map[string]string{"CLAUDE.local.md": "old", ".claude/skills/a/SKILL.md": "a1", ".claude/agents/gone.md": "g1", ".claude/agents/edited.md": "e1"}
	here := map[string]Here{
		"CLAUDE.local.md":           {SHA256: "old"},    // the mirror's own write: replace
		".claude/skills/a/SKILL.md": {SHA256: "mine"},   // edited here: keep
		".claude/skills/b/SKILL.md": {SHA256: "b1"},     // already the same
		".claude/commands/t.md":     {Tracked: true},    // the repository's
		".claude/agents/gone.md":    {SHA256: "g1"},     // dropped at source, untouched here: delete
		".claude/agents/edited.md":  {SHA256: "edited"}, // dropped at source, edited here: keep
	}
	s := Plan(e, owned, here)
	if !reflect.DeepEqual(s.Write, []string{"CLAUDE.local.md"}) {
		t.Errorf("write %v", s.Write)
	}
	if !reflect.DeepEqual(s.Delete, []string{".claude/agents/gone.md"}) {
		t.Errorf("delete %v", s.Delete)
	}
	want := []Kept{{".claude/agents/edited.md", KeepLocalEdit}, {".claude/commands/t.md", KeepTracked}, {".claude/skills/a/SKILL.md", KeepLocalEdit}}
	if !reflect.DeepEqual(s.Kept, want) {
		t.Errorf("kept %v, want %v", s.Kept, want)
	}
	if !reflect.DeepEqual(s.Owned, map[string]string{"CLAUDE.local.md": "new", ".claude/skills/b/SKILL.md": "b1"}) {
		t.Errorf("owned %v", s.Owned)
	}
}

func TestCheckRefusesAnEntryThatLiesAboutItself(t *testing.T) {
	good := Entry{Repo: "github.com/o/n", CloneURL: "git@github.com:o/n.git", Icon: grid("#112233"),
		Files: []File{{Path: "CLAUDE.local.md", Content: []byte("hi"), Size: 2, SHA256: Sum([]byte("hi"))}}}
	if err := Check(good, true); err != nil {
		t.Fatal(err)
	}
	bad := good
	bad.CloneURL = "git@github.com:other/n.git"
	if Check(bad, true) == nil {
		t.Error("a clone URL for another repository was accepted")
	}
	bad = good
	bad.Files = []File{{Path: "CLAUDE.local.md", Content: []byte("hi!"), Size: 2, SHA256: Sum([]byte("hi"))}}
	if Check(bad, true) == nil {
		t.Error("content that does not match its hash was accepted")
	}
	bad = good
	bad.Files = []File{{Path: ".claude/settings.local.json", Content: []byte("{}"), Size: 2, SHA256: Sum([]byte("{}"))}}
	if Check(bad, true) == nil {
		t.Error("a permission file was accepted")
	}
}

func TestMirrorPersistsAndLooksUpByContainingPath(t *testing.T) {
	dir := t.TempDir()
	m, err := OpenMirror(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Put(Record{Repo: "github.com/o/shop", Path: "/w/shop", Label: "Shop", Icon: grid("#010203")}); err != nil {
		t.Fatal(err)
	}
	again, err := OpenMirror(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := again.Lookup("/w/shop/frontend"); !ok || r.Label != "Shop" {
		t.Fatalf("a subdirectory did not find its project: %v %v", r, ok)
	}
	if _, ok := again.Lookup("/w/shopping"); ok {
		t.Fatal("a sibling with a longer name matched")
	}
	if !again.Mirrored("/w/shop") || again.Mirrored("/w/shop/frontend") {
		t.Fatal("Mirrored is about checkout roots only")
	}
	if removed, err := again.Remove("github.com/o/shop"); err != nil || !removed {
		t.Fatalf("remove: %v %v", removed, err)
	}
	if info, err := os.Stat(filepath.Join(dir, MirrorFile)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the mirror file should be private: %v %v", info, err)
	}
}

func TestAnUnreadableMirrorRefusesToOpen(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, MirrorFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenMirror(dir); err == nil {
		t.Fatal("a corrupt mirror opened as an empty one")
	}
}

func TestAnIdentityCannotNameALocalPathOrADotDirectory(t *testing.T) {
	// git reads `a/b:c` as a local path, because a slash comes before the colon.
	for _, remote := range []string{"github.com/owner:name", "https://github.com/x/.git.git", "git@github.com:x/.ssh", "https://github.com/x/~"} {
		if got, err := Repo(remote); err == nil {
			t.Errorf("Repo(%q) = %q; that clone could land on a local path or a dot directory", remote, got)
		}
	}
	if ValidRepo("github.com/x/.git") {
		t.Error("a repository named .git is valid")
	}
}

func TestADotFileInsideACarriedPlaceIsNotCarried(t *testing.T) {
	for _, p := range []string{".claude/skills/x/.env", ".claude/commands/.secret/x.md"} {
		if Allowed(p) {
			t.Errorf("%q must not be carried: an ignored dot file is where a secret sits", p)
		}
	}
}
