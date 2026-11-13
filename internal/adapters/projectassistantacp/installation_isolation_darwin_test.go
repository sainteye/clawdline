package projectassistantacp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestClaudeIsolationRejectsAnUnprotectedProcess(t *testing.T) {
	node, e := exec.LookPath("node")
	if e != nil {
		t.Skip("Node is required for the local OS control")
	}
	node, e = filepath.EvalSymlinks(node)
	if e != nil {
		t.Fatal(e)
	}
	for _, protected := range []bool{false, true} {
		t.Run(map[bool]string{false: "unprotected", true: "protected"}[protected], func(t *testing.T) {
			root := t.TempDir()
			owned := filepath.Join(root, "owned")
			memory := filepath.Join(owned, ".claude", "memory")
			if e := os.MkdirAll(memory, 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(filepath.Join(memory, "control"), []byte("control"), 0600); e != nil {
				t.Fatal(e)
			}
			owned, e = filepath.EvalSymlinks(owned)
			if e != nil {
				t.Fatal(e)
			}
			home, e := os.UserHomeDir()
			if e != nil {
				t.Fatal(e)
			}
			profile := "(version 1)(allow default)"
			if protected {
				profile = claudeProfile(home, owned, node, "/usr/bin/false", filepath.Join(root, "node_modules"))
			}
			config := Configuration{Executable: "/usr/bin/sandbox-exec", Arguments: []string{"-p", profile, node}, Environment: []string{"HOME=" + home, "PATH=/usr/bin:/bin", "LC_ALL=C"}}
			e = verifyClaudeIsolation(context.Background(), config, owned, filepath.Join(root, "foreign"), testLimits())
			if protected && e != nil {
				t.Fatalf("OS boundary refused: %v", e)
			}
			if !protected && pa.Code(e) != pa.ReadOnlyUnverified {
				t.Fatalf("unprotected process was not refused: %v", e)
			}
		})
	}
}
