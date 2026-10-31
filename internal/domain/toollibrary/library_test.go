package toollibrary

import (
	"net/url"
	"strings"
	"testing"
)

func TestToolLibrarySourcesAndPinnedPreparation(t *testing.T) {
	c := Recommendations()
	if len(c.Projects) != 23 || len(c.Categories) != 9 {
		t.Fatalf("catalog: %d projects, %d categories", len(c.Projects), len(c.Categories))
	}
	skills := 0
	for _, p := range c.Projects {
		normalized, err := Normalize(p)
		if err != nil {
			t.Fatalf("%s: %v", p.ID, err)
		}
		if normalized.ID != p.ID {
			t.Fatalf("noncanonical id %s", p.ID)
		}
		skills += len(p.Skills)
		for _, sk := range p.Skills {
			out, err := Prepare(p, Preparation{SkillID: sk.ID, Task: "Review this component", Mode: "task", Locale: "en"})
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{p.Commit, sk.Path, sk.SHA256, "Verify setup", "untrusted task data", "does not grant", "task-owned"} {
				if want != "" && !strings.Contains(out.Prompt, want) {
					t.Errorf("%s: prompt lost %q", sk.ID, want)
				}
			}
		}
	}
	if skills != 46 {
		t.Fatalf("skills %d", skills)
	}
	p := c.Projects[0]
	out, err := Prepare(p, Preparation{SkillID: p.Skills[0].ID, Task: "核對範圍", Mode: "project", Locale: "zh-Hant-TW"})
	if err != nil || !strings.Contains(out.Prompt, "安裝") || !strings.Contains(out.Prompt, "付費") {
		t.Fatalf("localized preparation: %v %s", err, out.Prompt)
	}
	if _, err := Prepare(p, Preparation{SkillID: "unknown", Task: "Do it", Mode: "task"}); err == nil {
		t.Fatal("unknown entry accepted")
	}
}

func TestToolLibraryRejectsUnsafeSourcesAndEntries(t *testing.T) {
	userinfo := (&url.URL{Scheme: "https", Host: "github.com", Path: "/a/b", User: url.User("fixture")}).String()
	for _, u := range []string{"http://github.com/owner/repo", "https://github.com.evil.test/a/b", userinfo, "https://github.com/a/b?token=secret", "https://github.com/a/b/tree/main", "https://github.com/a/%2e%2e", "https://127.0.0.1/a/b", "https://github.com/-x/y"} {
		if _, _, _, err := Repository(u); err == nil {
			t.Errorf("accepted %s", u)
		}
	}
	_, repo, url, err := Repository("https://github.com/Owner/Repo.git/")
	if err != nil || repo != "Repo" || url != "https://github.com/Owner/Repo" {
		t.Fatal(repo, url, err)
	}
	for _, path := range []string{"../SKILL.md", "a/../../SKILL.md", "/SKILL.md", "a\\SKILL.md", "a/./SKILL.md", "a/README.md"} {
		if ValidPath(path) {
			t.Errorf("accepted path %s", path)
		}
	}
	p := Recommendations().Projects[0]
	p.Icon.Asset = "javascript:bad"
	p.Icon.URL = "file:///private"
	normalized, err := Normalize(p)
	if err != nil || normalized.Icon.Asset == p.Icon.Asset || normalized.Icon.URL == p.Icon.URL {
		t.Fatalf("untrusted icon was retained: %v", err)
	}
}

func TestToolLibraryCustomSourceMustResolveBeforeSetup(t *testing.T) {
	p := Project{RepoURL: "https://github.com/example/skills", CategoryID: "development"}
	out, err := Prepare(p, Preparation{Task: "Find an appropriate Skill", Mode: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Skill != nil || out.Project.Commit != "" || !strings.Contains(out.Prompt, "resolve and record one before proceeding") {
		t.Fatal("unresolved source presented as ready")
	}
}
