package projectfiles

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
)

func unifyRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, text := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func symlink(t *testing.T, root, target, rel string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.FromSlash(target), path); err != nil {
		t.Fatal(err)
	}
}

func mustPlan(t *testing.T, root string) contract.ProjectUnifyPlan {
	t.Helper()
	p, err := Plan(root)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func kinds(p contract.ProjectUnifyPlan) []string {
	out := []string{}
	for _, a := range p.Actions {
		out = append(out, string(a.Kind))
	}
	return out
}

func conflictKinds(p contract.ProjectUnifyPlan) []string {
	out := []string{}
	for _, c := range p.Conflicts {
		out = append(out, string(c.Kind)+" "+c.Path)
	}
	return out
}

func skillRow(t *testing.T, p contract.ProjectUnifyPlan, name string) contract.ProjectUnifySkill {
	t.Helper()
	for _, s := range p.Skills {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no skill row %q in %+v", name, p.Skills)
	return contract.ProjectUnifySkill{}
}

func TestUnifyPlanRulesRows(t *testing.T) {
	cases := []struct {
		name      string
		files     map[string]string
		status    contract.ProjectUnifyStatus
		actions   []string
		conflicts []string
		claudeNow []string
		claudeAft []string
		codexAft  []string
	}{
		{"only AGENTS.md", map[string]string{"AGENTS.md": "rule\n"}, "unified", []string{}, []string{},
			[]string{"AGENTS.md"}, []string{"AGENTS.md"}, []string{"AGENTS.md"}},
		{"CLAUDE.md without import", map[string]string{"AGENTS.md": "shared\n", "CLAUDE.md": "claude rule\n"}, "drifting",
			[]string{"rules_add_import"}, []string{"claude_only_lines CLAUDE.md"},
			[]string{"CLAUDE.md"}, []string{"CLAUDE.md", "AGENTS.md"}, []string{"AGENTS.md"}},
		{"CLAUDE.md with import", map[string]string{"AGENTS.md": "shared\n", "CLAUDE.md": "@AGENTS.md\n\nclaude only\n"}, "unified",
			[]string{}, []string{}, []string{"CLAUDE.md", "AGENTS.md"}, []string{"CLAUDE.md", "AGENTS.md"}, []string{"AGENTS.md"}},
		{"rules only in CLAUDE.md", map[string]string{"CLAUDE.md": "only here\n"}, "drifting",
			[]string{"rules_create_agents"}, []string{}, []string{"CLAUDE.md"}, []string{"CLAUDE.md", "AGENTS.md"}, []string{"AGENTS.md"}},
		{"import without AGENTS.md", map[string]string{"CLAUDE.md": "@AGENTS.md\n"}, "drifting",
			[]string{}, []string{"import_without_agents CLAUDE.md"}, []string{"CLAUDE.md"}, []string{"CLAUDE.md"}, []string{}},
		{"neither file", map[string]string{"README.md": "x"}, "unified", []string{}, []string{}, []string{}, []string{}, []string{}},
		{"not UTF-8", map[string]string{"AGENTS.md": "a\n", "CLAUDE.md": "\xff\xfe"}, "unknown",
			[]string{}, []string{"unreadable CLAUDE.md"}, []string{"CLAUDE.md"}, []string{"CLAUDE.md"}, []string{"AGENTS.md"}},
		{"too large", map[string]string{"AGENTS.md": strings.Repeat("a", MaxFileBytes+1)}, "unknown",
			[]string{}, []string{"too_large AGENTS.md"}, []string{}, []string{}, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := mustPlan(t, unifyRepo(t, c.files))
			if p.Status != c.status {
				t.Fatalf("status %s, want %s; %+v", p.Status, c.status, p)
			}
			if got := kinds(p); !reflect.DeepEqual(got, c.actions) {
				t.Fatalf("actions %v, want %v", got, c.actions)
			}
			if got := conflictKinds(p); !reflect.DeepEqual(got, c.conflicts) {
				t.Fatalf("conflicts %v, want %v", got, c.conflicts)
			}
			if !reflect.DeepEqual(p.Rules.Now.Claude, c.claudeNow) || !reflect.DeepEqual(p.Rules.After.Claude, c.claudeAft) ||
				!reflect.DeepEqual(p.Rules.After.Codex, c.codexAft) {
				t.Fatalf("reads now %v after %v / codex %v", p.Rules.Now.Claude, p.Rules.After.Claude, p.Rules.After.Codex)
			}
		})
	}
}

func TestUnifyPlanShowsTheLinesCodexCannotSee(t *testing.T) {
	p := mustPlan(t, unifyRepo(t, map[string]string{"AGENTS.md": "shared\n", "CLAUDE.md": "first\n\n  second\n",
		".claude/CLAUDE.md": "x", "CLAUDE.local.md": "y"}))
	if !reflect.DeepEqual(p.Rules.ClaudeOnlyLines, []string{"first", "  second"}) {
		t.Fatalf("lines %q", p.Rules.ClaudeOnlyLines)
	}
	if !reflect.DeepEqual(p.Rules.ClaudeOnlyFiles, []string{".claude/CLAUDE.md", "CLAUDE.local.md"}) {
		t.Fatalf("files %q", p.Rules.ClaudeOnlyFiles)
	}
	edit := p.Actions[0].Edits[0]
	if edit.Before == nil || *edit.Before != "first\n\n  second\n" || edit.After != "@AGENTS.md\nfirst\n\n  second\n" {
		t.Fatalf("edit %+v", edit)
	}
}

func TestUnifyPlanRulesLinks(t *testing.T) {
	root := unifyRepo(t, map[string]string{"AGENTS.md": "shared\n"})
	symlink(t, root, "AGENTS.md", "CLAUDE.md")
	if p := mustPlan(t, root); p.Status != "unified" || !p.Rules.ClaudeImportsAgents {
		t.Fatalf("CLAUDE.md -> AGENTS.md: %+v", p)
	}
	other := unifyRepo(t, map[string]string{"AGENTS.md": "shared\n", "docs/rules.md": "r"})
	symlink(t, other, "docs/rules.md", "CLAUDE.md")
	p := mustPlan(t, other)
	if p.Status != "drifting" || !reflect.DeepEqual(conflictKinds(p), []string{"rules_link CLAUDE.md"}) || len(p.Actions) != 0 {
		t.Fatalf("CLAUDE.md -> elsewhere: %+v", p)
	}
}

func TestUnifyPlanUnreadableFileIsUnknown(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file without permission")
	}
	root := unifyRepo(t, map[string]string{"AGENTS.md": "a", "CLAUDE.md": "b"})
	path := filepath.Join(root, "CLAUDE.md")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0o644)
	p := mustPlan(t, root)
	if p.Status != "unknown" || len(p.Actions) != 0 {
		t.Fatalf("unreadable CLAUDE.md: %+v", p)
	}
	if _, err := Apply(root, p.Version); !errors.Is(err, ErrPlanUnknown) {
		t.Fatalf("apply on unknown: %v", err)
	}
}

func TestUnifyPlanSkillsRows(t *testing.T) {
	skill := func(text string) string { return "---\nname: x\n---\n" + text }
	root := unifyRepo(t, map[string]string{
		".agents/skills/codex-only/SKILL.md":   skill("a"),
		".claude/skills/claude-only/SKILL.md":  skill("b"),
		".agents/skills/same/SKILL.md":         skill("c"),
		".agents/skills/same/notes/more.md":    "m",
		".claude/skills/same/SKILL.md":         skill("c"),
		".claude/skills/same/notes/more.md":    "m",
		".agents/skills/different/SKILL.md":    skill("d1"),
		".claude/skills/different/SKILL.md":    skill("d2"),
		".agents/skills/linked/SKILL.md":       skill("e"),
		".agents/skills/taken/SKILL.md":        skill("f"),
		".claude/skills/taken":                 "a plain file",
		".agents/skills/not-a-skill/README.md": "no SKILL.md",
		"outside/SKILL.md":                     skill("g"),
	})
	symlink(t, root, "../../.agents/skills/linked", ".claude/skills/linked")
	symlink(t, root, "../../outside", ".claude/skills/elsewhere")
	p := mustPlan(t, root)
	want := map[string]struct {
		place  contract.ProjectUnifySkillPlace
		action contract.ProjectUnifyActionKind
		now    contract.ProjectUnifySeen
		after  contract.ProjectUnifySeen
	}{
		"codex-only":  {"agents", "skill_link", contract.ProjectUnifySeen{Codex: true}, contract.ProjectUnifySeen{Codex: true, Claude: true}},
		"claude-only": {"claude", "skill_move_and_link", contract.ProjectUnifySeen{Claude: true}, contract.ProjectUnifySeen{Codex: true, Claude: true}},
		"same":        {"both_same", "skill_replace_copy_with_link", contract.ProjectUnifySeen{Codex: true, Claude: true}, contract.ProjectUnifySeen{Codex: true, Claude: true}},
		"different":   {"both_different", "", contract.ProjectUnifySeen{Codex: true, Claude: true}, contract.ProjectUnifySeen{Codex: true, Claude: true}},
		"linked":      {"both_same", "", contract.ProjectUnifySeen{Codex: true, Claude: true}, contract.ProjectUnifySeen{Codex: true, Claude: true}},
		"taken":       {"agents", "", contract.ProjectUnifySeen{Codex: true}, contract.ProjectUnifySeen{Codex: true}},
		"elsewhere":   {"claude", "", contract.ProjectUnifySeen{}, contract.ProjectUnifySeen{}},
	}
	if len(p.Skills) != len(want) {
		t.Fatalf("skills %+v", p.Skills)
	}
	for name, w := range want {
		row := skillRow(t, p, name)
		if row.Place != w.place || row.Action != w.action || row.Now != w.now || row.After != w.after {
			t.Errorf("%s: %+v, want %+v", name, row, w)
		}
	}
	if !skillRow(t, p, "linked").Linked {
		t.Error("linked skill not marked linked")
	}
	if got := conflictKinds(p); !reflect.DeepEqual(got, []string{"skill_differs .claude/skills/different",
		"skill_link_elsewhere .claude/skills/elsewhere", "name_taken .claude/skills/taken"}) {
		t.Fatalf("conflicts %v", got)
	}
	if p.Status != "drifting" {
		t.Fatalf("status %s", p.Status)
	}
	for _, a := range p.Actions {
		if strings.HasPrefix(string(a.Kind), "skill_") && a.Kind != "skill_copy" && !strings.HasPrefix(a.LinkTarget, "../../.agents/skills/") {
			t.Errorf("%s link target %q", a.Kind, a.LinkTarget)
		}
	}
}

func TestUnifyPlanWholeSkillsDirectoryLink(t *testing.T) {
	root := unifyRepo(t, map[string]string{".agents/skills/one/SKILL.md": "x"})
	symlink(t, root, "../.agents/skills", ".claude/skills")
	p := mustPlan(t, root)
	if p.Status != "unified" || !skillRow(t, p, "one").Linked {
		t.Fatalf("whole directory link: %+v", p)
	}
	other := unifyRepo(t, map[string]string{".agents/skills/one/SKILL.md": "x", "elsewhere/one/SKILL.md": "y"})
	symlink(t, other, "../elsewhere", ".claude/skills")
	if got := conflictKinds(mustPlan(t, other)); !reflect.DeepEqual(got, []string{"skills_directory_link .claude/skills"}) {
		t.Fatalf("directory linked elsewhere: %v", got)
	}
}

func TestUnifyPlanVersionFollowsEveryInput(t *testing.T) {
	root := unifyRepo(t, map[string]string{"AGENTS.md": "a", ".agents/skills/s/SKILL.md": "x"})
	first := mustPlan(t, root)
	if again := mustPlan(t, root); again.Version != first.Version {
		t.Fatal("the same disk gave two versions")
	}
	if err := os.WriteFile(filepath.Join(root, ".agents/skills/s/SKILL.md"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if mustPlan(t, root).Version == first.Version {
		t.Fatal("a skill edit kept the version")
	}
}

func TestUnifyApplyRefusesAChangedPlan(t *testing.T) {
	root := unifyRepo(t, map[string]string{"AGENTS.md": "a", "CLAUDE.md": "b"})
	p := mustPlan(t, root)
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(root, p.Version); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("apply after an edit: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md")); string(got) != "edited" {
		t.Fatalf("CLAUDE.md %q", got)
	}
}

func TestUnifyApplyEveryActionKind(t *testing.T) {
	root := unifyRepo(t, map[string]string{
		"CLAUDE.md":                           "@AGENTS.md\nclaude rule\n",
		".agents/skills/codex-only/SKILL.md":  "a",
		".claude/skills/claude-only/SKILL.md": "b",
		".claude/skills/claude-only/run.sh":   "#!/bin/sh\n",
		".agents/skills/same/SKILL.md":        "c",
		".claude/skills/same/SKILL.md":        "c",
	})
	if err := os.Chmod(filepath.Join(root, ".claude/skills/claude-only/run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := mustPlan(t, root)
	if got := kinds(p); !reflect.DeepEqual(got, []string{"rules_create_agents", "skill_move_and_link", "skill_link", "skill_replace_copy_with_link"}) {
		t.Fatalf("actions %v", got)
	}
	out, err := Apply(root, p.Version)
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome != "applied" || len(out.Ran) != 4 || out.Plan.Status != "unified" {
		t.Fatalf("applied %+v", out)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "AGENTS.md")); string(got) != "claude rule\n" {
		t.Fatalf("AGENTS.md %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md")); string(got) != "@AGENTS.md\n" {
		t.Fatalf("CLAUDE.md %q", got)
	}
	for _, name := range []string{"codex-only", "claude-only", "same"} {
		link := filepath.Join(root, ".claude/skills", name)
		target, err := os.Readlink(link)
		if err != nil || target != filepath.FromSlash("../../.agents/skills/"+name) {
			t.Fatalf("%s link %q %v", name, target, err)
		}
		resolved, err := filepath.EvalSymlinks(link)
		real, _ := filepath.EvalSymlinks(filepath.Join(root, ".agents/skills", name))
		if err != nil || resolved != real {
			t.Fatalf("%s resolves to %q, want %q", name, resolved, real)
		}
	}
	if info, err := os.Stat(filepath.Join(root, ".agents/skills/claude-only/run.sh")); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("moved script %v %v", info, err)
	}
	entries, _ := os.ReadDir(filepath.Join(root, ".claude/skills"))
	if len(entries) != 3 {
		t.Fatalf("leftovers in .claude/skills: %v", entries)
	}
	again, err := Apply(root, out.Plan.Version)
	if err != nil || len(again.Ran) != 0 || again.Plan.Status != "unified" {
		t.Fatalf("second apply %+v %v", again, err)
	}
}

func TestUnifyApplyAddsTheImportAndKeepsTheRest(t *testing.T) {
	root := unifyRepo(t, map[string]string{"AGENTS.md": "shared\n", "CLAUDE.md": "claude rule\n"})
	out, err := Apply(root, mustPlan(t, root).Version)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md")); string(got) != "@AGENTS.md\nclaude rule\n" {
		t.Fatalf("CLAUDE.md %q", got)
	}
	if out.Plan.Status != "unified" || !reflect.DeepEqual(out.Plan.Rules.ClaudeOnlyLines, []string{"claude rule"}) {
		t.Fatalf("after %+v", out.Plan)
	}
}

func TestUnifyNeverOverwritesANameThatAppeared(t *testing.T) {
	root := unifyRepo(t, map[string]string{".agents/skills/s/SKILL.md": "a", "CLAUDE.md": "rule\n"})
	st, err := planAt(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude/skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude/skills/s"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("theirs"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, a := range st.plan.Actions {
		if err := st.perform(a); !errors.Is(err, ErrNameTaken) {
			t.Fatalf("%s over an existing name: %v", a.Kind, err)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(root, ".claude/skills/s")); string(got) != "mine" {
		t.Fatalf("skill name overwritten: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "AGENTS.md")); string(got) != "theirs" {
		t.Fatalf("AGENTS.md overwritten: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md")); string(got) != "rule\n" {
		t.Fatalf("CLAUDE.md changed though AGENTS.md was refused: %q", got)
	}
}

func TestUnifyMoveRefusesASkillEditedAfterThePlan(t *testing.T) {
	root := unifyRepo(t, map[string]string{".claude/skills/s/SKILL.md": "a"})
	st, err := planAt(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude/skills/s/extra.md"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.perform(st.plan.Actions[0]); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("move of an edited skill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agents/skills/s")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("moved anyway: %v", err)
	}
}

func TestUnifyApplyStopsAndSaysWhatRan(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes without permission")
	}
	root := unifyRepo(t, map[string]string{"AGENTS.md": "a", "CLAUDE.md": "b", ".agents/skills/s/SKILL.md": "x", ".claude/keep": ""})
	claudeDir := filepath.Join(root, ".claude")
	if err := os.Chmod(claudeDir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(claudeDir, 0o755)
	out, err := Apply(root, mustPlan(t, root).Version)
	if err == nil || out.Outcome != "stopped" || len(out.Ran) != 1 || out.Ran[0].Kind != "rules_add_import" ||
		out.Failed == nil || out.Failed.Kind != "skill_link" {
		t.Fatalf("stopped apply %+v %v", out, err)
	}
	if out.Plan.Status != "drifting" || !reflect.DeepEqual(kinds(out.Plan), []string{"skill_link"}) {
		t.Fatalf("plan after the stop %+v", out.Plan)
	}
}

func TestUnifyCopiesWhereLinksAreUnavailable(t *testing.T) {
	linksAvailable = false
	defer func() { linksAvailable = true }()
	root := unifyRepo(t, map[string]string{".agents/skills/a/SKILL.md": "a", ".claude/skills/c/SKILL.md": "c", ".claude/skills/c/sub/x.md": "x"})
	p := mustPlan(t, root)
	if p.LinksAvailable || !reflect.DeepEqual(kinds(p), []string{"skill_copy", "skill_copy"}) || !skillRow(t, p, "a").Copy {
		t.Fatalf("plan without links %+v", p)
	}
	out, err := Apply(root, p.Version)
	if err != nil || out.Plan.Status != "unified" {
		t.Fatalf("copy apply %+v %v", out, err)
	}
	for _, rel := range []string{".claude/skills/a/SKILL.md", ".agents/skills/c/sub/x.md"} {
		info, err := os.Lstat(filepath.Join(root, rel))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("%s: %v %v", rel, info, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".claude/skills/a/SKILL.md"), []byte("edited copy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := mustPlan(t, root); got.Status != "drifting" || skillRow(t, got, "a").Place != "both_different" {
		t.Fatalf("an edited copy is not drift: %+v", got)
	}
}
