package projects

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveScopeDistinguishesRepositoryAndPlace(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "main")
	linked := filepath.Join(root, "linked")
	plain := filepath.Join(root, "plain")
	for _, path := range []string{filepath.Join(repo, ".git", "worktrees", "linked"), linked, plain} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	gitDir := filepath.Join(repo, ".git", "worktrees", "linked")
	if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../..\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked, ".git"), []byte("gitdir: "+gitDir+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mainScope, ok := ResolveScope(repo)
	if !ok || mainScope.Kind != ScopeRepository || mainScope.Key != repo || mainScope.ID != ProjectID(repo) {
		t.Fatalf("main scope = %+v, %t", mainScope, ok)
	}
	linkedScope, ok := ResolveScope(linked)
	if !ok || linkedScope != mainScope {
		t.Fatalf("linked scope = %+v, %t; want %+v", linkedScope, ok, mainScope)
	}
	placeScope, ok := ResolveScope(plain)
	if !ok || placeScope.Kind != ScopePlace || placeScope.Key != plain || placeScope.ID != "place:"+PlaceID(plain) {
		t.Fatalf("plain scope = %+v, %t", placeScope, ok)
	}
	if placeScope.ID == mainScope.ID {
		t.Fatal("repository and place scopes have the same id")
	}
	if _, ok := ResolveScope("relative/place"); ok {
		t.Fatal("relative place was accepted")
	}
}
