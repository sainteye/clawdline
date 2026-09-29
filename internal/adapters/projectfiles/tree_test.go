package projectfiles

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectTreeListsFoldersAndReadsOnlyProjectText(t *testing.T) {
	place := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(place, "src", "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(place, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"src/nested/readme.txt": "A text file\n", ".env": "local text\n",
		"src/binary": "\x00\x01", ".git/config": "metadata",
	} {
		if err := os.WriteFile(filepath.Join(place, filepath.FromSlash(name)), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(place, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	root, err := ListTree(place, "")
	if err != nil {
		t.Fatal(err)
	}
	if root.Directory != "" || root.Truncated || len(root.Entries) != 3 {
		t.Fatalf("unexpected root listing: %+v", root)
	}
	if root.Entries[0].Path != "src" || root.Entries[0].Kind != "directory" {
		t.Fatalf("folder not first: %+v", root.Entries)
	}
	for _, entry := range root.Entries {
		if entry.Name == ".git" {
			t.Fatal("Git metadata was listed")
		}
	}
	listed, err := ListTree(place, "src/nested")
	if err != nil || len(listed.Entries) != 1 || listed.Entries[0].Path != "src/nested/readme.txt" {
		t.Fatalf("nested listing: %+v, %v", listed, err)
	}
	content, err := ReadTree(place, listed.Entries[0].Path)
	if err != nil || content.Text != "A text file\n" || content.Version == "" {
		t.Fatalf("nested read: %+v, %v", content, err)
	}
	for _, name := range []string{"../secret.txt", "src/../secret.txt", ".git/config", "linked/secret.txt", "/etc/passwd", "src//nested/readme.txt", "src\\nested\\readme.txt"} {
		if _, err := ReadTree(place, name); err == nil {
			t.Errorf("unsafe path %q was read", name)
		}
	}
	if _, err := ReadTree(place, "src/binary"); !errors.Is(err, ErrText) {
		t.Fatalf("binary read: %v", err)
	}
	if _, err := ListTree(place, "linked"); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("linked directory: %v", err)
	}
}

func TestProjectTreeBoundsRejectBeforeOpening(t *testing.T) {
	place := t.TempDir()
	if _, err := ReadTree(place, strings.Repeat("x", MaxTreePathBytes+1)); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("path length: %v", err)
	}
	if _, err := ListTree(place, strings.Repeat("x/", MaxTreeDepth)+"x"); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("tree depth: %v", err)
	}
	if err := os.WriteFile(filepath.Join(place, "large.txt"), []byte(strings.Repeat("x", MaxFileBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTree(place, "large.txt"); !errors.Is(err, ErrLarge) {
		t.Fatalf("large text: %v", err)
	}
}
