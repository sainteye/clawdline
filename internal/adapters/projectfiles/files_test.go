package projectfiles

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, name, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func listed(t *testing.T, list Listing, location string) File {
	t.Helper()
	for _, f := range list.Files {
		if f.Location == location {
			return f
		}
	}
	t.Fatalf("missing %s in %#v", location, list.Files)
	return File{}
}

func TestProjectFilesInventorySeparatesNestedAndGlobalCandidates(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	selected := filepath.Join(repo, "package")
	home := filepath.Join(base, "home")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(repo, "AGENTS.md"), "root rules")
	put(t, filepath.Join(selected, "CLAUDE.md"), "local rules")
	put(t, filepath.Join(repo, ".agents", "skills", "one", "SKILL.md"), "project skill")
	put(t, filepath.Join(selected, ".claude", "skills", "two", "SKILL.md"), "nested skill")
	put(t, filepath.Join(home, ".codex", "AGENTS.md"), "global rules")
	put(t, filepath.Join(home, ".claude", "skills", "three", "SKILL.md"), "global skill")
	put(t, filepath.Join(repo, "sibling", "AGENTS.md"), "not selected")
	list, err := List(selected, home)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		name, source, assistant, status string
		editable                        bool
	}{
		{"AGENTS.md", "project", "codex", "ready", true},
		{"package/CLAUDE.md", "project", "claude", "ready", true},
		{".agents/skills/one/SKILL.md", "project", "codex", "ready", true},
		{"package/.claude/skills/two/SKILL.md", "project", "claude", "ready", true},
		{".codex/AGENTS.md", "global", "codex", "ready", false},
		{".claude/skills/three/SKILL.md", "global", "claude", "ready", false},
		{"package/AGENTS.md", "project", "codex", "missing", false},
	} {
		f := listed(t, list, check.name)
		if f.Source != check.source || f.Assistant != check.assistant || f.Status != check.status || f.Editable != check.editable {
			t.Errorf("%s: %+v", check.name, f)
		}
	}
	for _, f := range list.Files {
		if strings.Contains(f.Location, "sibling") {
			t.Fatal("sibling project listed")
		}
	}
	global := listed(t, list, ".codex/AGENTS.md")
	if _, err := Save(selected, home, global.ID, strings.Repeat("0", 64), "edit"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("global write: %v", err)
	}
}

func TestProjectFilesSaveRequiresCurrentVersionAndKeepsText(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	home := filepath.Join(base, "home")
	put(t, filepath.Join(repo, "AGENTS.md"), "first")
	list, err := List(repo, home)
	if err != nil {
		t.Fatal(err)
	}
	f := listed(t, list, "AGENTS.md")
	first, err := Read(repo, home, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := Save(repo, home, f.ID, first.Version, "second")
	if err != nil || saved.Text != "second" || saved.Version == first.Version {
		t.Fatalf("save: %+v %v", saved, err)
	}
	if _, err := Save(repo, home, f.ID, first.Version, "stale"); !errors.Is(err, ErrChanged) {
		t.Fatalf("stale save: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(repo, "AGENTS.md")); string(got) != "second" {
		t.Fatalf("file: %q", got)
	}
	if _, err := Read(repo, home, "unknown"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("unknown id: %v", err)
	}
	put(t, filepath.Join(repo, "CLAUDE.md"), string([]byte{0xff, 0xfe}))
	list, _ = List(repo, home)
	if _, err := Read(repo, home, listed(t, list, "CLAUDE.md").ID); !errors.Is(err, ErrText) {
		t.Fatalf("binary: %v", err)
	}
}

func TestProjectFilesRejectInRootSymlinkSwapBeforeOpening(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	home := filepath.Join(base, "home")
	good := filepath.Join(repo, ".agents", "skills", "good")
	other := filepath.Join(repo, ".agents", "skills", "other")
	put(t, filepath.Join(good, "SKILL.md"), "chosen")
	put(t, filepath.Join(other, "SKILL.md"), "sibling")
	list, err := List(repo, home)
	if err != nil {
		t.Fatal(err)
	}
	f := listed(t, list, ".agents/skills/good/SKILL.md")
	tripped := false
	testAfterDirectoryLstat = func(name string) {
		if name != "good" || tripped {
			return
		}
		tripped = true
		if err := os.Rename(good, good+"-held"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("other", good); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
	}
	defer func() { testAfterDirectoryLstat = nil }()
	if _, err := read(f); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("swapped read: %v", err)
	}
	if !tripped {
		t.Fatal("swap hook did not run")
	}
	if got, _ := os.ReadFile(filepath.Join(other, "SKILL.md")); string(got) != "sibling" {
		t.Fatal("sibling changed")
	}
	if _, err := Save(repo, home, f.ID, strings.Repeat("0", 64), "attack"); err == nil {
		t.Fatal("swapped write accepted")
	}
}
