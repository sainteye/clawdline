package projects

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPlaceRegistryPersistsDirectoriesWithoutProviderHistory(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	for _, path := range []string{first, second} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	registry := OpenPlaceRegistry(state)
	registry.SetLimit(2)
	at := time.Unix(1_790_000_000, 0)
	if _, err := registry.Add([]string{first, second, first}, at); err != nil {
		t.Fatal(err)
	}

	reopened := OpenPlaceRegistry(state)
	rows, err := reopened.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want two: %#v", len(rows), rows)
	}
	for _, row := range rows {
		if row.AddedAt != at.Unix() {
			t.Errorf("%s added at %d, want %d", row.Path, row.AddedAt, at.Unix())
		}
	}
	st, err := os.Stat(registry.Path())
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("registry mode %o, want 600", st.Mode().Perm())
	}
}

func TestPlaceRegistryRefusesInvalidOrExcessDirectories(t *testing.T) {
	registry := OpenPlaceRegistry(filepath.Join(t.TempDir(), "state"))
	registry.SetLimit(1)
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := registry.Add([]string{missing}, time.Now()); !errors.Is(err, ErrNotDirectory) {
		t.Fatalf("missing directory error = %v", err)
	}
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	for _, path := range []string{first, second} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := registry.Add([]string{first}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Add([]string{second}, time.Now()); !errors.Is(err, ErrPlaceRegistryFull) {
		t.Fatalf("full registry error = %v", err)
	}
	rows, err := registry.List()
	if err != nil || len(rows) != 1 || rows[0].Path != resolvedPath(first) {
		t.Fatalf("failed add changed registry: rows=%#v err=%v", rows, err)
	}
}

func TestPlaceRegistryRemoveDoesNotNeedDirectoryToExist(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	dir := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	registry := OpenPlaceRegistry(state)
	if _, err := registry.Add([]string{dir}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	rows, err := registry.Remove([]string{dir})
	if err != nil || len(rows) != 0 {
		t.Fatalf("remove: rows=%#v err=%v", rows, err)
	}
	rows, err = OpenPlaceRegistry(state).List()
	if err != nil || len(rows) != 0 {
		t.Fatalf("reopen after remove: rows=%#v err=%v", rows, err)
	}
}
