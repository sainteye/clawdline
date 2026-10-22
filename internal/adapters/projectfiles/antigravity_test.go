package projectfiles

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAntigravitySharedCandidatesKeepOneIdentityAndEditor(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	put(t, filepath.Join(root, ".git", "HEAD"), "synthetic")
	put(t, filepath.Join(root, "AGENTS.md"), "shared")
	put(t, filepath.Join(root, "nested", "AGENTS.md"), "nested")
	put(t, filepath.Join(root, ".agents", "skills", "review", "SKILL.md"), "shared skill")
	put(t, filepath.Join(root, "GEMINI.md"), "native")
	put(t, filepath.Join(root, ".agent", "skills", "legacy", "SKILL.md"), "native skill")
	put(t, filepath.Join(home, ".gemini", "antigravity-cli", "skills", "global", "SKILL.md"), "global")
	list, err := List(filepath.Join(root, "nested"), home)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"AGENTS.md", "nested/AGENTS.md", ".agents/skills/review/SKILL.md"} {
		f := listed(t, list, name)
		if strings.Join(f.Assistants, ",") != "codex,antigravity" || f.Assistant != "codex" || !f.Editable {
			t.Fatalf("shared: %+v", f)
		}
		count := 0
		for _, other := range list.Files {
			if other.ID == f.ID {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("duplicate ID: %d", count)
		}
		before, err := Read(filepath.Join(root, "nested"), home, f.ID)
		if err != nil {
			t.Fatal(err)
		}
		after, err := Save(filepath.Join(root, "nested"), home, f.ID, before.Version, "edited once")
		if err != nil {
			t.Fatal(err)
		}
		if after.File.ID != f.ID || after.Version == before.Version {
			t.Fatal("identity or digest")
		}
	}
	for _, name := range []string{"GEMINI.md", ".agent/skills/legacy/SKILL.md", ".gemini/antigravity-cli/skills/global/SKILL.md"} {
		f := listed(t, list, name)
		if f.Editable || f.Assistant != "antigravity" {
			t.Fatalf("native: %+v", f)
		}
		c, err := Read(filepath.Join(root, "nested"), home, f.ID)
		if err != nil {
			t.Fatal(err)
		}
		if c.File.Editable {
			t.Fatal("read enabled native editor")
		}
		if _, err := Save(filepath.Join(root, "nested"), home, f.ID, c.Version, "overwrite"); !errors.Is(err, ErrReadOnly) {
			t.Fatalf("write: %v", err)
		}
	}
}

func TestAntigravityUnifyReportsKnownEmptyFalseAndNativeReadOnly(t *testing.T) {
	root := t.TempDir()
	p, err := Plan(root)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(p)
	if !strings.Contains(string(wire), `"antigravity":[]`) || !strings.Contains(string(wire), `"native_only":[]`) {
		t.Fatalf("known empty: %s", wire)
	}
	put(t, filepath.Join(root, "CLAUDE.md"), "rules")
	put(t, filepath.Join(root, ".claude", "skills", "review", "SKILL.md"), "skill")
	put(t, filepath.Join(root, ".agent", "skills", "legacy", "SKILL.md"), "legacy")
	put(t, filepath.Join(root, ".agents", "rules", "native.md"), "---\ntrigger: manual\n---\nnative")
	p, err = Plan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.NativeOnly) != 2 || len(p.Skills) != 1 || p.Skills[0].Now.Antigravity == nil || *p.Skills[0].Now.Antigravity || !*p.Skills[0].After.Antigravity {
		t.Fatalf("plan: %+v", p)
	}
	wire, _ = json.Marshal(p)
	if !strings.Contains(string(wire), `"antigravity":false`) {
		t.Fatalf("known false: %s", wire)
	}
	out, err := Apply(root, p.Version)
	if err != nil {
		t.Fatal(err)
	}
	if out.Plan.Status != "unified" {
		t.Fatalf("after: %+v", out.Plan)
	}
	if b, _ := os.ReadFile(filepath.Join(root, ".agent", "skills", "legacy", "SKILL.md")); string(b) != "legacy" {
		t.Fatal("native skill changed")
	}
	if len(out.Plan.Rules.Now.Antigravity) != 1 {
		t.Fatal("shared rules absent")
	}
}

func TestAntigravityNativeChangesFenceApplyAndNeverFollowRuleLinks(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "CLAUDE.md"), "shared")
	put(t, filepath.Join(root, "GEMINI.md"), "native v1")
	p, err := Plan(root)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, "GEMINI.md"), "native v2")
	if _, err := Apply(root, p.Version); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("native changed: %v", err)
	}
	outside := t.TempDir()
	put(t, filepath.Join(outside, "private.md"), "outside synthetic")
	if err := os.MkdirAll(filepath.Join(root, ".agents"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".agents", "rules")); err != nil {
		t.Skip(err)
	}
	list, err := List(root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range list.Files {
		if strings.Contains(file.Location, "private.md") {
			t.Fatal("followed native directory link")
		}
	}
	p, err = Plan(root)
	if err != nil || p.Status != "unknown" {
		t.Fatalf("unsafe native directory: %v %s", err, p.Status)
	}
}
