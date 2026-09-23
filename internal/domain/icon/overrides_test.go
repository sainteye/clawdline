package icon

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func TestCopiedIconSurvivesRestartAndDifferentPaths(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRegistryWithOverrides(dir)
	if err != nil {
		t.Fatal(err)
	}
	r.path = "" // Synthetic fixtures never read the person's registry.
	target := filepath.Join(dir, "remote-project")
	source := creatureFromSeed(StableHash("/source/project"))
	before := r.For(target)
	if err := r.Save(target, source, before); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(target, source, before); err != nil {
		t.Fatalf("lost-reply retry: %v", err)
	}
	reopened, err := NewRegistryWithOverrides(dir)
	if err != nil {
		t.Fatal(err)
	}
	reopened.path = ""
	for _, path := range []string{target, filepath.Join(target, "frontend")} {
		if !reflect.DeepEqual(reopened.For(path), source) {
			t.Fatal("copied icon did not survive restart or reach child directory")
		}
	}
	if reflect.DeepEqual(reopened.For(target+"-other"), source) {
		t.Fatal("prefix sibling inherited icon")
	}
	other := creatureFromSeed(17)
	if err := reopened.Save(target, other, before); !errors.Is(err, ErrIconChanged) {
		t.Fatalf("stale overwrite: %v", err)
	}
	if err := reopened.Save(target, other, source); err != nil {
		t.Fatal(err)
	}
	if got := reopened.SavedCount(); got != 1 {
		t.Fatalf("saved %d icons", got)
	}
}

func TestCopiedIconRejectsUnsafeAndOversizedGrids(t *testing.T) {
	good := creatureFromSeed(9)
	if err := Validate(good); err != nil {
		t.Fatal(err)
	}
	unsafe := "url(https://example.invalid/pixel)"
	for _, grid := range []Grid{
		{}, {Accent: unsafe, Cells: good.Cells},
		{Accent: good.Accent, Cells: [][]*string{{&unsafe}}},
		{Accent: good.Accent, Cells: [][]*string{{nil}, {nil, nil}}},
		{Accent: good.Accent, Cells: make([][]*string, MaxIconSide+1)},
		{Accent: good.Accent, Cells: [][]*string{make([]*string, MaxIconSide+1)}},
	} {
		if Validate(grid) == nil {
			t.Fatal("accepted invalid grid")
		}
	}
}

func TestUnreadableRegistryDoesNotDisappear(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "project-icon-overrides.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistryWithOverrides(dir); err == nil {
		t.Fatal("corrupt overrides accepted")
	}
}

func TestFullIconRegistryKeepsExistingMarks(t *testing.T) {
	r, err := NewRegistryWithOverrides(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r.path = ""
	for i := 0; i < MaxIconOverrides; i++ {
		r.overrides[filepath.Join("/fixture", strconv.Itoa(i))] = creatureFromSeed(int64(i))
	}
	target := filepath.Join(t.TempDir(), "project")
	if err := r.Save(target, creatureFromSeed(4), r.For(target)); !errors.Is(err, ErrIconCapacity) {
		t.Fatalf("capacity: %v", err)
	}
}

func TestCopyOfMatchingFallbackStillPinsTheMark(t *testing.T) {
	r, err := NewRegistryWithOverrides(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r.path = ""
	target := filepath.Join(t.TempDir(), "project")
	mark := r.For(target)
	if err := r.Save(target, mark, mark); err != nil {
		t.Fatal(err)
	}
	if r.SavedCount() != 1 {
		t.Fatal("copy of an identical fallback was not persisted")
	}
}

func TestCustomArtworkCopiesWithoutEditingTheSourceRegistry(t *testing.T) {
	dir := t.TempDir()
	source := NewRegistry()
	source.path = filepath.Join(dir, "legacy.json")
	sourcePath := filepath.Join(dir, "source")
	legacy, err := json.Marshal(map[string]any{"projects": map[string]any{sourcePath: map[string]any{
		"label": "Synthetic project", "art": map[string]any{"accent": "#ABCDEF", "palette": map[string]string{"x": "#ABCDEF"}, "rows": []string{"x.x", ".x."}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(source.path, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	receiver, err := NewRegistryWithOverrides(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	receiver.path = ""
	target := filepath.Join(dir, "receiver")
	mark := source.For(sourcePath)
	if err = receiver.Save(target, mark, receiver.For(target)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(receiver.For(target), mark) {
		t.Fatal("art changed across copy")
	}
	after, err := os.ReadFile(source.path)
	if err != nil || string(after) != string(legacy) {
		t.Fatal("source registry changed")
	}
}
