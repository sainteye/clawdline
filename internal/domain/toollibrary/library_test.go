package toollibrary

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestToolLibrarySourcesAndPinnedPreparation(t *testing.T) {
	c := Recommendations()
	if len(c.Projects) != 37 || len(c.Categories) != 14 {
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
	if skills != 60 {
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

func TestToolLibraryPreparesLibrariesWithoutInventingSkills(t *testing.T) {
	libraries := 0
	for _, p := range Recommendations().Projects {
		if p.IntegrationKind != "library" {
			continue
		}
		libraries++
		if !commit.MatchString(p.Commit) || len(p.Skills) != 0 || len(p.Requirements) == 0 || p.License == nil {
			t.Fatalf("incomplete library source: %s", p.ID)
		}
		for _, locale := range []string{"en", "zh-Hant-TW"} {
			out, err := Prepare(p, Preparation{Task: "Add an accessible animation", Mode: "project", Locale: locale})
			if err != nil || out.Skill != nil || out.Project.IntegrationKind != "library" {
				t.Fatalf("%s library preparation: %v", p.ID, err)
			}
			for _, want := range []string{p.Commit, *p.License, p.Requirements[0]["en"], p.Requirements[0]["zh-Hant"]} {
				if !strings.Contains(out.Prompt, want) {
					t.Fatalf("%s lost prerequisite or source: %q", p.ID, want)
				}
			}
			if locale == "en" && (!strings.Contains(out.Prompt, "no Skill entry required") || !strings.Contains(out.Prompt, "actual browser behavior") || strings.Contains(out.Prompt, "discover the real SKILL.md entries")) {
				t.Fatalf("%s was given the Skill discovery flow", p.ID)
			}
			if locale != "en" && (!strings.Contains(out.Prompt, "無須 Skill 入口") || !strings.Contains(out.Prompt, "實際瀏覽器行為")) {
				t.Fatalf("%s lost localized library flow", p.ID)
			}
		}
		p.Skills = Recommendations().Projects[0].Skills[:1]
		if _, err := Normalize(p); err == nil {
			t.Fatalf("%s library accepted a fabricated Skill entry", p.ID)
		}
	}
	if libraries != 3 {
		t.Fatalf("libraries %d", libraries)
	}
}

func TestToolLibraryPreparesEvaluationFrameworkWithoutInventingSkills(t *testing.T) {
	var p Project
	for _, candidate := range Recommendations().Projects {
		if candidate.ID == "google-research--harness2" {
			p = candidate
		}
	}
	if p.IntegrationKind != "framework" || len(p.Skills) != 0 || !commit.MatchString(p.Commit) || len(p.Requirements) == 0 || p.License == nil {
		t.Fatal("Harness2 must be a pinned evaluation framework with prerequisites and a license")
	}
	for _, mode := range []string{"task", "project"} {
		for _, locale := range []string{"en", "zh-Hant-TW"} {
			out, err := Prepare(p, Preparation{Task: "Compare two workflows", Mode: mode, Locale: locale})
			if err != nil || out.Skill != nil || out.Project.IntegrationKind != "framework" || out.Mode != mode {
				t.Fatalf("framework preparation: %v", err)
			}
			for _, want := range []string{p.Commit, *p.License, p.Requirements[0]["en"], p.Requirements[0]["zh-Hant"]} {
				if !strings.Contains(out.Prompt, want) {
					t.Fatalf("framework lost source or prerequisites: %q", want)
				}
			}
			if locale == "en" {
				for _, want := range []string{"No Skill entry is required", "independent acceptance", "baseline", "request or cost cap", "sandbox", "credential-free", "actual or bounded cost"} {
					if !strings.Contains(out.Prompt, want) {
						t.Fatalf("framework preparation lost %q", want)
					}
				}
				if strings.Contains(out.Prompt, "discover the real SKILL.md entries") || strings.Contains(out.Prompt, "Integrate the selected effect") {
					t.Fatal("framework was given Skill or web-library setup")
				}
			} else if !strings.Contains(out.Prompt, "無須 Skill 入口") || !strings.Contains(out.Prompt, "獨立驗收") || !strings.Contains(out.Prompt, "費用上限") {
				t.Fatal("framework lost localized setup")
			}
		}
	}
	p.Skills = Recommendations().Projects[0].Skills[:1]
	if _, err := Normalize(p); err == nil {
		t.Fatal("evaluation framework accepted a fabricated Skill")
	}
}

// Categories are grouped by what the person wants to do; each source added
// with the 2026-11-08 regrouping keeps its pinned entry and license evidence.
func TestToolLibraryGroupsSourcesByIntentWithPinnedEvidence(t *testing.T) {
	var raw struct {
		Categories []Category `json:"categories"`
		Projects   []struct {
			ID         string `json:"id"`
			CategoryID string `json:"category_id"`
			Category   Names  `json:"category"`
			Tier       string `json:"recommendation_tier"`
			Commit     string `json:"source_commit"`
			License    string `json:"license"`
			Icon       Icon   `json:"icon"`
			Evidence   []struct {
				Path, SHA256, License string
				SourceURL             string `json:"source_url"`
			} `json:"license_evidence"`
			Skills []struct {
				ID, Path, Mode, SHA256 string
				SourceURL              string `json:"source_url"`
				Bytes                  int    `json:"source_bytes"`
			} `json:"skills"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(catalogBytes, &raw); err != nil {
		t.Fatal(err)
	}
	names := map[string]Names{}
	for _, c := range raw.Categories {
		names[c.ID] = c.Name
	}
	for id, en := range map[string]string{"design": "Interface design direction", "design-review": "Interface review & polish", "design-system": "Design systems & components", "creative": "Creative visuals & 3D", "diagrams": "Diagrams, slides & documentation"} {
		if names[id]["en"] != en || names[id]["zh-Hant"] == "" {
			t.Errorf("category %s: %v", id, names[id])
		}
	}
	want := map[string]struct{ category, tier string }{
		"jakubkrehel--skills":              {"design-review", "start_here"},
		"emilkowalski--skills":             {"animation", "start_here"},
		"dammyjay93--interface-design":     {"design", "explore"},
		"wondelai--skills":                 {"design-review", "explore"},
		"dominikmartn--hue":                {"design-system", "explore"},
		"google-labs-code--stitch-skills":  {"design-system", "explore"},
		"shadcn-ui--ui":                    {"design-system", "task_specific"},
		"lottiefiles--motion-design-skill": {"animation", "explore"},
		"greensock--gsap-skills":           {"animation", "task_specific"},
		"minimax-ai--skills":               {"creative", "task_specific"},
		"mengto--skills":                   {"creative", "task_specific"},
		"zarazhangrui--frontend-slides":    {"diagrams", "explore"},
	}
	found := map[string]bool{}
	for _, p := range raw.Projects {
		if p.ID == "greensock--gsap" {
			t.Error("the GSAP library card was replaced by the official GSAP skills")
		}
		if n := names[p.CategoryID]; n == nil || p.Category["en"] != n["en"] || p.Category["zh-Hant"] != n["zh-Hant"] {
			t.Errorf("%s embeds a stale category name %v", p.ID, p.Category)
		}
		w, ok := want[p.ID]
		if !ok {
			continue
		}
		found[p.ID] = true
		if p.CategoryID != w.category || p.Tier != w.tier {
			t.Errorf("%s: %s/%s, want %s/%s", p.ID, p.CategoryID, p.Tier, w.category, w.tier)
		}
		if !commit.MatchString(p.Commit) || p.License == "" || len(p.Evidence) == 0 || len(p.Skills) == 0 || !strings.HasPrefix(p.Icon.Asset, "/tool-library/") {
			t.Errorf("%s lacks a pin, license, entry or icon", p.ID)
		}
		for _, e := range p.Evidence {
			if !digest.MatchString(e.SHA256) || !strings.Contains(e.SourceURL, "/blob/"+p.Commit+"/"+e.Path) || e.License != p.License {
				t.Errorf("%s license evidence %+v", p.ID, e)
			}
		}
		for _, sk := range p.Skills {
			if !digest.MatchString(sk.SHA256) || sk.Bytes <= 0 || !ValidPath(sk.Path) || !strings.HasSuffix(sk.SourceURL, "/blob/"+p.Commit+"/"+sk.Path) || (sk.Mode != "instruction" && sk.Mode != "runtime") {
				t.Errorf("%s entry %+v", p.ID, sk)
			}
		}
	}
	if len(found) != len(want) {
		t.Fatalf("found %d of %d regrouped sources", len(found), len(want))
	}
	var slides Project
	for _, p := range Recommendations().Projects {
		if p.ID == "zarazhangrui--frontend-slides" {
			slides = p
		}
	}
	out, err := Prepare(slides, Preparation{SkillID: slides.Skills[0].ID, Task: "Build a deck", Mode: "task", Locale: "en"})
	if err != nil || !strings.Contains(out.Prompt, "Vercel") || !strings.Contains(out.Prompt, "explicit approval") {
		t.Fatalf("Frontend Slides preparation lost its publishing limit: %v", err)
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
