package skillmenu

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAntigravityFollowsTheTypedCommandsPrecedence(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(repo, "web", "app")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}

	// Project skills in .agents and .gemini
	skill(t, filepath.Join(repo, ".gemini", "skills"), "deploy", "---\ndescription: root gemini deploy\n---")
	skill(t, filepath.Join(repo, "web", ".agents", "skills"), "deploy", "---\ndescription: web agents deploy\n---")
	skill(t, filepath.Join(repo, ".agents", "skills"), "lint", "---\ndescription: lint it\n---")

	// Personal / global skill
	skill(t, filepath.Join(home, ".gemini", "skills"), "review", "---\ndescription: personal review\n---")
	skill(t, filepath.Join(home, ".gemini", "antigravity-cli", "skills"), "global-audit", "---\ndescription: global audit\n---")

	r := Antigravity(work, home)
	if len(r.Skills) != 4 {
		t.Fatalf("expected 4 skills, got %d: %+v", len(r.Skills), r.Skills)
	}

	// deploy should be web agents deploy (deeper wins)
	foundDeploy := false
	for _, s := range r.Skills {
		if s.Name == "deploy" {
			foundDeploy = true
			if s.Description != "web agents deploy" || s.Source != Project {
				t.Fatalf("wrong deploy skill: %+v", s)
			}
		}
	}
	if !foundDeploy {
		t.Fatal("deploy skill missing")
	}
}
