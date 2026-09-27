package http

import (
	"os"
	"path/filepath"
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
