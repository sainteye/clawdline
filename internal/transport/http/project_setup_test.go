package http

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/icon"
)

func TestProjectSetupKeepsConfigurationSeparateFromCurrentDeployActivity(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "projects", "shop")
	statusDir := filepath.Join(home, "status")
	swiftDir := filepath.Join(home, "swift")
	for _, dir := range []string{project, statusDir, swiftDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("CLAWDLINE_SWIFT_DIR", swiftDir)
	if err := os.WriteFile(filepath.Join(swiftDir, "config.json"), []byte(`{"status_dir":"`+statusDir+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	before := projectSetup(project, "github.com/team/shop", icon.SourceGenerated)
	if before.Deploy != contract.ProjectDeploySetupMissing || before.DeployActivity != contract.ProjectDeployActivityUnknown {
		t.Fatalf("before producer = %#v", before)
	}
	if err := os.WriteFile(filepath.Join(statusDir, "ghrun-team-shop.json"), []byte(`{"state":"none","why":"no-runs","updated_at":1790000000}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".devstack.json"), []byte(`{"processes":[{"name":"web","port":4173},{"name":"api","url":"http://127.0.0.1:8080"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	after := projectSetup(project, "github.com/team/shop", icon.SourceOverride)
	if after.Icon != contract.ProjectIconSetupOverride || after.Sync != contract.ProjectSyncSetupReady {
		t.Fatalf("identity = %#v", after)
	}
	if after.Deploy != contract.ProjectDeploySetupReady || after.DeployActivity != contract.ProjectDeployActivityIdle {
		t.Fatalf("configured idle deploy = %#v", after)
	}
	if after.Servers != contract.ProjectServerSetupReady || after.ServerCount != 2 {
		t.Fatalf("servers = %#v", after)
	}
}

func TestProjectSetupNamesInapplicableAndBrokenEvidence(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("CLAWDLINE_SWIFT_DIR", filepath.Join(home, "absent-swift"))
	if err := os.WriteFile(filepath.Join(project, ".devstack.json"), []byte(`not json`), 0o600); err != nil {
		t.Fatal(err)
	}

	setup := projectSetup(project, "git.example.com/team/shop", icon.SourceRegistry)
	if setup.Deploy != contract.ProjectDeploySetupNotApplicable {
		t.Errorf("non-GitHub deploy = %q", setup.Deploy)
	}
	if setup.Servers != contract.ProjectServerSetupAttention {
		t.Errorf("broken stack = %q", setup.Servers)
	}
	if setup.Sync != contract.ProjectSyncSetupReady {
		t.Errorf("remote identity = %q", setup.Sync)
	}
}

// The readiness card's unify row is the unify plan's own status: shared,
// drifting with how many items, or unknown when the plan could not read
// something — never shared by default.
func TestProjectSetupCarriesTheUnifyStatusOfThePlan(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAWDLINE_SWIFT_DIR", filepath.Join(home, "absent-swift"))
	write := func(dir, name, text string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	unified := filepath.Join(home, "unified")
	write(unified, "AGENTS.md", "# Rules\n")
	if got := projectSetup(unified, "", icon.SourceGenerated); got.Unify != contract.ProjectUnifyStatusUnified || got.UnifyCount != 0 {
		t.Errorf("AGENTS.md alone: unify %q count %d", got.Unify, got.UnifyCount)
	}

	// CLAUDE.md without the import is one action (add the import); a skill
	// only Claude has is another.
	drifting := filepath.Join(home, "drifting")
	write(drifting, "AGENTS.md", "# Rules\n")
	write(drifting, "CLAUDE.md", "")
	write(filepath.Join(drifting, ".claude", "skills", "review"), "SKILL.md", "---\nname: review\n---\n")
	if got := projectSetup(drifting, "", icon.SourceGenerated); got.Unify != contract.ProjectUnifyStatusDrifting || got.UnifyCount != 2 {
		t.Errorf("an import and a skill to share: unify %q count %d", got.Unify, got.UnifyCount)
	}

	tooLarge := filepath.Join(home, "too-large")
	write(tooLarge, "AGENTS.md", strings.Repeat("x", 129<<10))
	if got := projectSetup(tooLarge, "", icon.SourceGenerated); got.Unify != contract.ProjectUnifyStatusUnknown || got.UnifyCount != 0 {
		t.Errorf("an AGENTS.md past the read limit: unify %q count %d", got.Unify, got.UnifyCount)
	}
	if got := projectSetup(filepath.Join(home, "gone"), "", icon.SourceGenerated); got.Unify != contract.ProjectUnifyStatusUnknown {
		t.Errorf("a place no longer on disk: unify %q", got.Unify)
	}
}
