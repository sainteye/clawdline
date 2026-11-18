//go:build !windows

package orchestrator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Full tasks must be able to request an automatically reviewed browser launch.
// Never suppresses the request before the reviewer can decide whether it is
// authorized. The same launch must retain the filesystem and network profile.
func TestCodexFullTaskCanRequestReviewedBrowserLaunch(t *testing.T) {
	for _, metadata := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "profile"}[metadata], func(t *testing.T) {
			repo := t.TempDir()
			if metadata {
				if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			args := permissionArgs(Record{Assistant: "codex", PermissionMode: "full", Repository: repo}, t.TempDir(), repo, "")
			// Evaluate the shell quoting used by the actual launch, without a
			// model call or a replacement codex executable on the person's PATH.
			cmd := exec.Command("sh", "-c", "printf '%s\\n' "+strings.Join(args, " "))
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatal(err)
			}
			argv := strings.Split(strings.TrimSpace(string(output)), "\n")
			contains := func(want string) bool {
				for _, arg := range argv {
					if arg == want {
						return true
					}
				}
				return false
			}
			if !contains("on-request") || !contains(`approvals_reviewer="auto_review"`) || contains("never") {
				t.Fatalf("authorized browser launch cannot reach automatic review: %q", argv)
			}
			for _, arg := range argv {
				if strings.Contains(arg, "danger-full-access") || strings.Contains(arg, "bypass-approvals") {
					t.Fatalf("browser support removed confinement: %q", argv)
				}
			}
			if metadata {
				if !contains(`permissions.clawdline-workspace.extends=":workspace"`) || !contains("permissions.clawdline-workspace.network.enabled=true") {
					t.Fatalf("review replaced the workspace profile: %q", argv)
				}
			} else if !contains("workspace-write") || !contains("sandbox_workspace_write.network_access=true") {
				t.Fatalf("review replaced the legacy workspace/network policy: %q", argv)
			}
		})
	}
}
