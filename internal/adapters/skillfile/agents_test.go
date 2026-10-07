package skillfile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The stub goes to Claude Code's skill directory and to the user-scope
// `.agents/skills` one Codex reads, each with its own record; uninstall puts
// back what was at each, a link at one and nothing at the other.
func TestBothTargetsInstallAndRestoreIndependently(t *testing.T) {
	root := t.TempDir()
	p := Paths{Home: filepath.Join(root, "home"), StateDir: filepath.Join(root, "state")}
	targets := Targets(p)
	if len(targets) != 2 || targets[0].stub() != StubPath(p.Home) || targets[1].stub() != AgentsStubPath(p.Home) ||
		targets[0].record() == targets[1].record() || targets[0].backup() == targets[1].backup() {
		t.Fatalf("targets %+v", targets)
	}
	if targets[0].record() != filepath.Join(p.StateDir, RecordDir, RecordFile) {
		t.Fatalf("Claude Code's record moved: %s", targets[0].record())
	}
	// Claude Code's stub is a link into somebody's checkout; Codex's is absent.
	if err := os.MkdirAll(filepath.Dir(StubPath(p.Home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/elsewhere/SKILL.md", StubPath(p.Home)); err != nil {
		t.Fatal(err)
	}
	stub := []byte("---\nname: clawdline\n---\nstub\n")
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	for _, target := range targets {
		if done, err := Install(target, stub, now); err != nil || done.Outcome != Installed {
			t.Fatalf("install %s: %+v %v", target.stub(), done, err)
		}
		if got, err := os.ReadFile(target.stub()); err != nil || !bytes.Equal(got, stub) {
			t.Fatalf("%s: %q %v", target.stub(), got, err)
		}
	}
	for _, target := range targets {
		if _, err := Uninstall(target); err != nil {
			t.Fatalf("uninstall %s: %v", target.stub(), err)
		}
	}
	if link, err := os.Readlink(StubPath(p.Home)); err != nil || link != "/elsewhere/SKILL.md" {
		t.Fatalf("Claude Code's link was not put back: %q %v", link, err)
	}
	if _, err := os.Lstat(AgentsStubPath(p.Home)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Codex's stub is still there: %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(AgentsStubPath(p.Home))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the clawdline skill directory the install made is still there: %v", err)
	}
}
