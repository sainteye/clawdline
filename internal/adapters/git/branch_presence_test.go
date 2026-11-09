package git

import (
	"context"
	"path/filepath"
	"testing"
)

func TestBranchPresenceMatchesIndividualGitReads(t *testing.T) {
	repo, run := mergedRepository(t)
	run("branch", "topic/leaf")
	run("branch", "nearby")
	branches := []string{"main", "merged", "unmerged", "missing", "topic", "topic/leaf", "near", "", "main", "m*", "topic/"}
	g := New()
	presence, err := g.ReadBranchPresence(context.Background(), repo, branches)
	if err != nil {
		t.Fatal(err)
	}
	for _, branch := range branches {
		got, included := presence[branch]
		if branch == "m*" || branch == "topic/" {
			if included {
				t.Fatalf("legacy pattern %q did not defer to an individual read", branch)
			}
			continue
		}
		want, known := g.BranchExists(context.Background(), repo, branch)
		if !included || !known || got != want {
			t.Fatalf("%q: batch=%v included=%v individual=%v known=%v", branch, got, included, want, known)
		}
	}
	if len(presence) != 8 {
		t.Fatalf("unexpected branch results: %d", len(presence))
	}
}

func TestBranchPresenceFailureCannotAnswerAbsent(t *testing.T) {
	repo, _ := mergedRepository(t)
	g := New()
	for _, dir := range []string{filepath.Join(repo, "missing"), t.TempDir()} {
		if got, err := g.ReadBranchPresence(context.Background(), dir, []string{"main"}); err == nil || len(got) != 0 {
			t.Fatalf("failed repository answered known branches: %v %v", got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := g.ReadBranchPresence(ctx, repo, []string{"main"}); err == nil || len(got) != 0 {
		t.Fatal("cancelled batch answered known branches")
	}
	if got, err := (&Git{Binary: filepath.Join(repo, "missing-git")}).ReadBranchPresence(context.Background(), repo, []string{"main"}); err == nil || len(got) != 0 {
		t.Fatal("unavailable Git answered known branches")
	}
	// No requested ordinary branch must not accidentally list every ref.
	if got, err := g.ReadBranchPresence(ctx, repo, []string{"m*", "topic/"}); err != nil || len(got) != 0 {
		t.Fatal("legacy-only patterns ran an unrelated batch")
	}
}
