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
	home, e := os.UserHomeDir()
	if e != nil {
		t.Fatal(e)
	}
	for _, withHistory := range []bool{false, true} {
		for _, protected := range []bool{false, true} {
			name := map[bool]string{false: "unprotected", true: "protected"}[protected] + map[bool]string{false: "", true: "/history"}[withHistory]
			t.Run(name, func(t *testing.T) {
				state, e := filepath.EvalSymlinks(t.TempDir())
				if e != nil {
					t.Fatal(e)
				}
				owned := filepath.Join(state, "project-assistants", "runtime", "owned")
				memory := filepath.Join(owned, ".claude", "memory")
				if e := os.MkdirAll(memory, 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(filepath.Join(memory, "control"), []byte("control"), 0600); e != nil {
					t.Fatal(e)
				}
				history := ""
				if withHistory {
					history = filepath.Join(state, "project-assistants", "native", "scope", "epoch", "config")
					if e := os.MkdirAll(history, 0700); e != nil {
						t.Fatal(e)
					}
				}
				profile := "(version 1)(allow default)"
				if protected {
					profile = claudeProfile(claudeBoundary{Home: home, State: state, Owned: owned, Node: node, CLI: "/usr/bin/false", Modules: filepath.Join(state, "node_modules"), History: history})
				}
				config := Configuration{Executable: "/usr/bin/sandbox-exec", Arguments: []string{"-p", profile, node}, Environment: []string{"HOME=" + home, "USER=" + os.Getenv("USER"), "PATH=/usr/bin:/bin", "LC_ALL=C"}}
				e = verifyClaudeIsolation(context.Background(), config, owned, filepath.Join(state, "foreign"), history, testLimits())
				if protected && e != nil {
					t.Fatalf("OS boundary refused: %v", e)
				}
				if !protected && pa.Code(e) != pa.ReadOnlyUnverified {
					t.Fatalf("unprotected process was not refused: %v", e)
				}
				if withHistory {
					for _, gone := range []string{filepath.Join(history, "memory"), filepath.Join(history, "projects", probeProject), filepath.Join(history, ".credentials.json"), filepath.Join(history, ".claude.json")} {
						if _, e := os.Lstat(gone); !os.IsNotExist(e) {
							t.Fatalf("probe residue %s: %v", gone, e)
						}
					}
					siblings, _ := os.ReadDir(filepath.Dir(filepath.Dir(history)))
					if len(siblings) != 1 {
						t.Fatalf("foreign control was not removed: %v", siblings)
					}
				}
			})
		}
	}
}

// A profile that grants the whole native directory would let one Bot read
// another Bot's or an older epoch's transcript; the probe must refuse it.
func TestClaudeIsolationRejectsABroadHistoryGrant(t *testing.T) {
	node, e := exec.LookPath("node")
	if e != nil {
		t.Skip("Node is required for the local OS control")
	}
	node, _ = filepath.EvalSymlinks(node)
	home, _ := os.UserHomeDir()
	state, _ := filepath.EvalSymlinks(t.TempDir())
	owned := filepath.Join(state, "project-assistants", "runtime", "owned")
	history := filepath.Join(state, "project-assistants", "native", "scope", "epoch", "config")
	for _, d := range []string{owned, history} {
		if e := os.MkdirAll(d, 0700); e != nil {
			t.Fatal(e)
		}
	}
	broad := claudeProfile(claudeBoundary{Home: home, State: state, Owned: owned, Node: node, CLI: "/usr/bin/false", Modules: filepath.Join(state, "node_modules"), History: filepath.Join(state, "project-assistants", "native")})
	config := Configuration{Executable: "/usr/bin/sandbox-exec", Arguments: []string{"-p", broad, node}, Environment: []string{"HOME=" + home, "USER=" + os.Getenv("USER"), "PATH=/usr/bin:/bin", "LC_ALL=C"}}
	if e := verifyClaudeIsolation(context.Background(), config, owned, filepath.Join(state, "foreign"), history, testLimits()); pa.Code(e) != pa.ReadOnlyUnverified {
		t.Fatalf("broad history grant was admitted: %v", e)
	}
}
