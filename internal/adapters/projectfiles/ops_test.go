package projectfiles

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func opsRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAnOpsMarkdownFileIsCreatedOnceAndNeverReplaced(t *testing.T) {
	root := opsRoot(t)
	made, err := CreateTreeText(root, "clawdline-ops", "Runbook.md", "# Runbook\n")
	if err != nil || made.Path != "clawdline-ops/Runbook.md" || made.Text != "# Runbook\n" {
		t.Fatalf("create: %+v %v", made, err)
	}
	if _, err := CreateTreeText(root, "clawdline-ops", "Runbook.md", "other"); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("same name: %v", err)
	}
	// Another writer takes the name between the check and the link.
	testBeforeCreateLink = func(name string) {
		_ = os.WriteFile(filepath.Join(root, "clawdline-ops", name), []byte("theirs"), 0o644)
	}
	defer func() { testBeforeCreateLink = nil }()
	if _, err := CreateTreeText(root, "clawdline-ops", "Race.md", "mine"); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("race: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "clawdline-ops", "Race.md")); string(got) != "theirs" {
		t.Fatalf("the other writer's file was replaced: %q", got)
	}
}

func TestAnOpsFolderThatIsALinkIsRefused(t *testing.T) {
	root := opsRoot(t)
	elsewhere := opsRoot(t)
	if err := os.Symlink(elsewhere, filepath.Join(root, "clawdline-ops")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if _, err := CreateTreeText(root, "clawdline-ops", "x.md", "x"); err == nil {
		t.Fatal("wrote through a linked folder")
	}
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
		t.Fatalf("wrote outside the Project: %v", entries)
	}
}

func TestRenameKeepsTheFileAndRefusesWhatItCannotDoSafely(t *testing.T) {
	root := opsRoot(t)
	write := func(rel, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/plan.md", "plan")
	write("docs/taken.md", "taken")
	write("AGENTS.md", "rules")
	write(".claude/skills/x/SKILL.md", "skill")
	recorded := ""
	record := func(to string) error { recorded = to; return nil }

	to, err := RenameTree(root, "docs/plan.md", "roadmap.md", record)
	if err != nil || to != "docs/roadmap.md" || recorded != to {
		t.Fatalf("rename: %q %v (recorded %q)", to, err, recorded)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "docs/roadmap.md")); string(got) != "plan" {
		t.Fatalf("renamed content: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(root, "docs/plan.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old name left: %v", err)
	}
	for _, tc := range []struct {
		from, to string
		want     error
	}{
		{"docs/roadmap.md", "taken.md", ErrNameTaken},
		{"docs/roadmap.md", "ROADMAP.md", ErrRenameCaseOnly},
		{"AGENTS.md", "rules.md", ErrRenameInstruction},
		{".claude/skills/x/SKILL.md", "other.md", ErrRenameInstruction},
		{"docs/roadmap.md", "CLAUDE.md", ErrRenameInstruction},
	} {
		if _, err := RenameTree(root, tc.from, tc.to, record); !errors.Is(err, tc.want) {
			t.Errorf("%s -> %s: %v, want %v", tc.from, tc.to, err, tc.want)
		}
	}
	if err := os.Symlink(filepath.Join(root, "docs/roadmap.md"), filepath.Join(root, "docs/link.md")); err == nil {
		if _, err := RenameTree(root, "docs/link.md", "moved.md", record); !errors.Is(err, ErrUnsafe) {
			t.Errorf("symlink rename: %v", err)
		}
	}
	// A failed record removes the new link and keeps the old name.
	if _, err := RenameTree(root, "docs/roadmap.md", "later.md", func(string) error { return errors.New("store down") }); err == nil {
		t.Fatal("a failed record was ignored")
	}
	if _, err := os.Lstat(filepath.Join(root, "docs/later.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new name left after a failed record: %v", err)
	}
	// The target appears between the check and the link.
	testBeforeRenameLink = func(path string) { _ = os.WriteFile(path, []byte("theirs"), 0o644) }
	defer func() { testBeforeRenameLink = nil }()
	if _, err := RenameTree(root, "docs/roadmap.md", "race.md", record); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("rename race: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "docs/race.md")); string(got) != "theirs" {
		t.Fatalf("rename replaced the other file: %q", got)
	}
}

func TestARenameTheDaemonStoppedInIsSettled(t *testing.T) {
	root := opsRoot(t)
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(root, "a.md"), filepath.Join(root, "b.md")); err != nil {
		t.Skip("hard links unavailable:", err)
	}
	// The record never moved: the new link is removed.
	if got, err := SettleRename(root, "a.md", "b.md", "a.md"); err != nil || got != "a.md" {
		t.Fatalf("undo: %q %v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(root, "b.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new link left: %v", err)
	}
	// The record moved: the old name is removed.
	if err := os.Link(filepath.Join(root, "a.md"), filepath.Join(root, "b.md")); err != nil {
		t.Fatal(err)
	}
	if got, err := SettleRename(root, "a.md", "b.md", "b.md"); err != nil || got != "b.md" {
		t.Fatalf("finish: %q %v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(root, "a.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old name left: %v", err)
	}
}
