package http

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/coordinator"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestMachineWorkspaceHasInstructionsAndRejectsSymlinkedFiles(t *testing.T) {
	state := t.TempDir()
	if err := ensureMachineWorkspace(state); err != nil {
		t.Fatal(err)
	}
	workspace := projects.MachineWorkspace(state)
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		body, err := os.ReadFile(filepath.Join(workspace, name))
		if err != nil || !strings.Contains(string(body), "Do not edit source code in any Project, including Clawdline") ||
			!strings.Contains(string(body), "create a Board item in the target Project first") {
			t.Fatalf("%s instructions: %v", name, err)
		}
	}
	if err := ensureMachineWorkspace(state); err != nil {
		t.Fatalf("reopening the workspace changed it: %v", err)
	}
	if err := os.Remove(filepath.Join(workspace, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(workspace, "AGENTS.md"), filepath.Join(workspace, "CLAUDE.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := ensureMachineWorkspace(state); err == nil {
		t.Fatal("a symlinked instruction file was trusted")
	}
}

func TestMachineStartRefusesIncompleteInventoryBeforeOpening(t *testing.T) {
	state := t.TempDir()
	for _, seen := range []app.Seen{
		{Inventory: session.Inventory{Complete: false}},
		{Inventory: session.Inventory{Complete: false}, Sessions: map[string]session.Session{}},
	} {
		if got := machineStartBlock(app.State{Seen: seen}, state); got != "coordinator_liveness_unknown" {
			t.Fatalf("incomplete reading returned %q; would allow a second unregistered Session", got)
		}
	}
	if got := machineStartBlock(app.State{Seen: app.Seen{Inventory: session.Inventory{Complete: true}}}, state); got != "" {
		t.Fatalf("complete empty reading returned %q", got)
	}
}

func TestMachineSessionRowKeepsPendingRegistrationVisible(t *testing.T) {
	state := t.TempDir()
	s := &Server{cfg: config.Config{Dir: state}, icons: icon.NewRegistry()}
	item := session.Session{ID: "machine-terminal", CWD: projects.MachineWorkspace(state),
		Assistant: session.AssistantCodex, Backend: session.BackendTmux}
	row := s.sessionRow(rowInput{item: item}).SessionRow
	if !row.MachineScope || row.Coordinator != nil {
		t.Fatalf("unregistered machine row = %+v", row)
	}
	item.CWD = t.TempDir()
	if s.sessionRow(rowInput{item: item}).MachineScope {
		t.Fatal("an ordinary Project row was labelled a machine Session")
	}
}

func TestOnlyTheBoundMachineSessionMayCreateProjectWork(t *testing.T) {
	sess := session.Session{ID: "terminal-a", ConversationID: "conversation-a"}
	state := app.State{Record: &coordinator.Record{Identity: coordinator.Identity{
		TerminalID: sess.ID, ConversationID: sess.ConversationID}}, Liveness: coordinator.Online}
	if !machineMayCreateItem(state, sess) {
		t.Fatal("the registered steward was refused")
	}
	for _, changed := range []app.State{
		{Liveness: coordinator.Online},
		{Record: state.Record, Liveness: coordinator.Unknown},
		{Record: state.Record, Liveness: coordinator.Offline},
	} {
		if machineMayCreateItem(changed, sess) {
			t.Fatalf("an unregistered or unverified role was accepted: %+v", changed)
		}
	}
	sess.ConversationID = "another-conversation"
	if machineMayCreateItem(state, sess) {
		t.Fatal("a different conversation impersonated the machine steward")
	}
}

func TestMachineWorkspaceRejectsPublicDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows permissions do not use Unix mode bits")
	}
	state := t.TempDir()
	workspace := projects.MachineWorkspace(state)
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureMachineWorkspace(state); err == nil {
		t.Fatal("a public machine workspace was accepted")
	}
}
