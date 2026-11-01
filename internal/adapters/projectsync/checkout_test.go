package projectsync

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestOriginDoesNotCallAMissingDirectoryANonRepository(t *testing.T) {
	_, err := Origin(context.Background(), filepath.Join(t.TempDir(), "absent"))
	if err == nil || errors.Is(err, ErrNotRepository) || errors.Is(err, ErrNoRemote) {
		t.Fatalf("an unreadable directory must retain its error: %v", err)
	}
}

// A clone that could never start says why. A machine without git prints
// nothing on git's standard error, and the line the mirror's page showed was
// "git clone: " — a sentence with its reason missing. The machine this was
// found on (2026-10-11) had no git at all.
func TestACloneThatCouldNotStartSaysWhy(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := Clone(context.Background(), "https://git.example.test/acme/shop.git", filepath.Join(t.TempDir(), "shop"))
	if err == nil {
		t.Fatal("a clone on a machine without git is not an error")
	}
	if strings.TrimSpace(strings.TrimPrefix(err.Error(), "git clone:")) == "" {
		t.Fatalf("the reason is missing: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "git") {
		t.Fatalf("the reason does not name git: %q", err.Error())
	}
}
