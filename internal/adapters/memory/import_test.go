package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A Claude Code auto-memory directory, invented: an index, a sub-index, two
// entries in the two frontmatter shapes Claude writes, and one whose name
// this store would refuse.
func claudeMemoryFixture(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"MEMORY.md":         "- [A](a.md) — hook\n",
		"index-topic.md":    "Sub-index, no frontmatter.\n",
		"nested-type.md":    "---\nname: nested-type\ndescription: an invented lesson\nmetadata:\n  type: feedback\n---\n\nInvented body.\n",
		"top-level-type.md": "---\nname: top-level-type\ndescription: >\n  folded over\n  two lines\ntype: project\n---\nAnother invented body.\n",
		"Bad Name.md":       "---\nname: Bad Name\ndescription: d\ntype: user\n---\nb\n",
		"not-markdown.txt":  "ignored",
		"missing-type.md":   "---\nname: missing-type\ndescription: d\n---\nb\n",
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestReadClaudeTakesEntriesAndSaysWhyItSkipsTheRest(t *testing.T) {
	dir := claudeMemoryFixture(t)
	before := snapshot(t, dir)
	plan, err := ReadClaude(dir)
	if err != nil {
		t.Fatal(err)
	}
	entries, skipped := plan.Entries, plan.Skipped
	if len(entries) != 2 || entries[0].Name != "nested-type" || entries[0].Type != TypeFeedback ||
		entries[1].Name != "top-level-type" || entries[1].Type != TypeProject ||
		entries[1].Description != "folded over two lines" || entries[1].Body != "Another invented body.\n" {
		t.Fatalf("entries %+v", entries)
	}
	why := map[string]string{}
	for _, s := range skipped {
		why[s.File] = s.Reason
	}
	if len(skipped) != 4 || !strings.Contains(why["MEMORY.md"], "the index") ||
		!strings.Contains(why["index-topic.md"], "not an entry") || !strings.Contains(why["Bad Name.md"], "name") ||
		!strings.Contains(why["missing-type.md"], "type") {
		t.Fatalf("skipped %+v", skipped)
	}
	if after := snapshot(t, dir); after != before {
		t.Fatalf("reading the source changed it:\n%s\n%s", before, after)
	}
}

func TestReadClaudeOfAMissingDirectoryIsNotAnEmptyAnswer(t *testing.T) {
	if _, err := ReadClaude(filepath.Join(t.TempDir(), "absent")); !os.IsNotExist(err) {
		t.Fatalf("%v", err)
	}
}

func TestClaudeSourcesAreUnderClaudesProjectsFolder(t *testing.T) {
	got := ClaudeSources("/home/someone", "/work/a_b.c", "/work/a_b.c", "")
	if len(got) != 1 || got[0] != filepath.Join("/home/someone", ".claude", "projects", "-work-a-b-c", "memory") {
		t.Fatalf("%v", got)
	}
}

// snapshot is every file's name, size, mode, mtime and bytes.
func snapshot(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		data := []byte{}
		if info.Mode().IsRegular() {
			data, _ = os.ReadFile(path)
		}
		b.WriteString(path + " " + info.Mode().String() + " " + info.ModTime().Format(time.RFC3339Nano) + " " + string(data) + "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}
