package store

import (
	"context"
	"errors"
	"github.com/sainteye/clawdline/internal/domain/toollibrary"
	"testing"
)

func TestToolLibraryPersistsAndRejectsLostUpdates(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	projects := toollibrary.Recommendations().Projects
	state, err := s.ChangeToolProject(ctx, 0, "", &projects[0], 1)
	if err != nil || state.Version != 1 || len(state.Saved) != 1 {
		t.Fatal(state, err)
	}
	if _, err = s.ChangeToolProject(ctx, 0, "", &projects[1], 1); !errors.Is(err, ErrToolLibraryVersion) {
		t.Fatalf("stale write: %v", err)
	}
	if _, err = s.ChangeToolProject(ctx, 1, "", &projects[1], 1); !errors.Is(err, ErrToolLibraryFull) {
		t.Fatalf("capacity: %v", err)
	}
	if _, err = s.ChangeToolProject(ctx, 1, "", &projects[0], 1); err != nil {
		t.Fatal("updating existing at capacity", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	state, err = s.ToolLibrary(ctx)
	if err != nil || state.Version != 2 || len(state.Saved) != 1 || state.Saved[0].Commit != projects[0].Commit {
		t.Fatal(state, err)
	}
	state, err = s.ChangeToolProject(ctx, 2, projects[0].ID, nil, 1)
	if err != nil || len(state.Saved) != 0 || state.Version != 3 {
		t.Fatal(state, err)
	}
}
