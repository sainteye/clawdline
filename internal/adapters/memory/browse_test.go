package memory

import (
	"errors"
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
