package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowseRevisionChangesForSameLengthEditsAndForget(t *testing.T) {
	s := New(t.TempDir())
	entry := invented("synthetic-browse", TypeProject)
	entry.Body = "original"
	if _, err := s.Add(testKey, entry); err != nil {
		t.Fatal(err)
	}
	before, err := s.Browse(testKey, entry.Name)
	if err != nil || strings.TrimSpace(before.Text) != "original" || before.Entries[0].UpdatedAt == "" {
		t.Fatalf("browse %+v %v", before, err)
	}
	entry.Body = "modified"
	if _, err = s.Update(testKey, entry); err != nil {
		t.Fatal(err)
	}
	after, err := s.Browse(testKey, entry.Name)
	if err != nil || before.Revision == after.Revision {
		t.Fatal("same-size edit kept its old revision", err)
	}
	if _, err = s.Browse(testKey, "../../unknown"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unregistered path admitted: %v", err)
	}
	if _, err = s.Forget(testKey, entry.Name); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Browse(testKey, entry.Name); !errors.Is(err, ErrNotFound) {
		t.Fatal("forgotten note returned", err)
	}
}

func TestBrowsePageLoadsOnlySelectedBodiesAndOmitsArchives(t *testing.T) {
	s := New(t.TempDir())
	entry := invented("alpha", TypeProject)
	entry.Body = "original"
	if _, err := s.Add(testKey, entry); err != nil {
		t.Fatal(err)
	}
	dir, err := s.Dir(testKey)
	if err != nil {
		t.Fatal(err)
	}
	oversized := invented("zulu", TypeProject)
	oversized.Body = strings.Repeat("x", entryFileReadLimit)
	archived := Entry{Name: "archived", Description: "Hidden from pages", Type: TypeProject, Body: "Historical note", Storage: ArchivedDuplicate, Replacement: "alpha"}
	if err = os.WriteFile(filepath.Join(dir, "archived.md"), []byte(Format(archived)), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "zulu.md"), []byte(Format(oversized)), 0600); err != nil {
		t.Fatal(err)
	}
	first, total, err := s.BrowsePage(testKey, "", 1, 1)
	if err != nil || total != 2 || len(first.Entries) != 1 || first.Entries[0].Entry.Name != "alpha" ||
		first.Entries[0].Entry.Body != "" || first.Text != "" {
		t.Fatalf("first page %+v, total %d, error %v", first, total, err)
	}
	if _, _, err = s.BrowsePage(testKey, "", 2, 1); err == nil {
		t.Fatalf("invalid on-page entry hidden: %v", err)
	}
	if _, _, err = s.BrowsePage(testKey, "zulu", 1, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("off-page detail admitted: %v", err)
	}
	if err = os.Remove(filepath.Join(dir, "zulu.md")); err != nil {
		t.Fatal(err)
	}
	first, _, err = s.BrowsePage(testKey, "", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	entry.Body = "modified"
	if _, err = s.Update(testKey, entry); err != nil {
		t.Fatal(err)
	}
	updated, _, err := s.BrowsePage(testKey, entry.Name, 1, 1)
	if err != nil || updated.Revision == first.Revision || strings.TrimSpace(updated.Text) != "modified" {
		t.Fatal("same-length change failed to invalidate page detail", err)
	}
}
