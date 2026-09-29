package projects

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMachineWorkspaceNeverBecomesAProject(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	workspace := MachineWorkspace(state)
	child := filepath.Join(workspace, "reports")
	sibling := filepath.Join(state, "machine-workspace-other")
	for _, dir := range []string{child, sibling} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if !IsMachineWorkspace(state, workspace) || !IsMachineWorkspace(state, child) || IsMachineWorkspace(state, sibling) {
		t.Fatal("the machine directory boundary is not exact")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(workspace, alias); err == nil && !IsMachineWorkspace(state, alias) {
		t.Fatal("a symlink alias became a Project")
	}

	places := NewPlaces(nil, nil)
	places.MachineStateDir = state
	places.Fixture = []string{workspace, child, sibling}
	rows := places.List(nil, 10)
	if len(rows) != 1 || rows[0].Path != sibling {
		t.Fatalf("the start list exposed the machine workspace: %#v", rows)
	}

	registry := OpenPlaceRegistry(state)
	if _, err := registry.Add([]string{workspace}, time.Now()); !errors.Is(err, ErrMachineWorkspace) {
		t.Fatalf("registration of the machine workspace: %v", err)
	}
	if _, err := registry.Add([]string{sibling}, time.Now()); err != nil {
		t.Fatalf("ordinary directory lost registration: %v", err)
	}
}
