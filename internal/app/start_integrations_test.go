package app

import (
	"context"
	"errors"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/workflowlaunch"
	"github.com/sainteye/clawdline/skills"
	"regexp"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

func TestNewSessionsReadMachineIntegrationsAndResumesDoNot(t *testing.T) {
	place := projects.Place{Path: t.TempDir()}
	for _, assistant := range []string{projects.AssistantClaude, projects.AssistantCodex} {
		terminal := &personaTerminal{}
		calls := 0
		enabled := true
		starter := Starter{Launcher: terminal, Terminal: func() projects.TerminalChoice { return projects.TerminalTmux },
			IntegrationInstructions: func(_ context.Context, path string) (string, error) {
				calls++
				if path != place.Path {
					t.Fatalf("wrong project passed: %q", path)
				}
				if enabled {
					return "MACHINE-INTEGRATION", nil
				}
				return "", nil
			}}
		if _, err := starter.Start(context.Background(), place, assistant, "", "", ""); err != nil || !strings.Contains(terminal.command, "MACHINE-INTEGRATION") {
			t.Fatalf("new %s did not receive opt-in: %v %s", assistant, err, terminal.command)
		}
		enabled = false
		if _, err := starter.Start(context.Background(), place, assistant, "", "", ""); err != nil || strings.Contains(terminal.command, "MACHINE-INTEGRATION") {
			t.Fatalf("new %s kept disabled integration: %v %s", assistant, err, terminal.command)
		}
		if _, err := starter.Start(context.Background(), place, assistant, "", "0f1e2d3c-0000-4000-8000-000000000002", ""); err != nil || calls != 2 {
			t.Fatalf("resume consulted a changed machine setting: calls=%d err=%v", calls, err)
		}
		terminal.command = ""
		starter.IntegrationInstructions = func(context.Context, string) (string, error) {
			return "", errors.New("unreadable integration settings")
		}
		if _, err := starter.Start(context.Background(), place, assistant, "", "", ""); err == nil || terminal.command != "" {
			t.Fatalf("unreadable opt-in still launched %s: %v", assistant, err)
		}
	}
}

func TestNewSessionRecordsItsWorkflowAndResumeKeepsTheOriginal(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := t.TempDir()
	terminal := &personaTerminal{}
	service := workflowlaunch.Service{Store: st, Dir: dir, Resolve: skills.ResolveWorkflow}
	starter := Starter{Launcher: terminal, Terminal: func() projects.TerminalChoice { return projects.TerminalTmux }, PrepareWorkflow: service.Prepare}
	place := projects.Place{Path: t.TempDir()}
	result, err := starter.Start(ctx, place, "codex", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`clawdline-workflow:([A-Za-z0-9_-]{22})`).FindStringSubmatch(terminal.command)
	if len(match) != 2 {
		t.Fatalf("no workflow launch marker: %s", terminal.command)
	}
	launch, found, err := st.WorkflowLaunchByID(ctx, match[1])
	if err != nil || !found || launch.TerminalID != result.ID || launch.Snapshot.ProfileID != "clawdline" {
		t.Fatalf("launch not recorded after opening: %+v %v", launch, err)
	}
	if err := st.BindWorkflowConversation(ctx, launch.ID, result.ID, "0f1e2d3c-0000-4000-8000-000000000002"); err != nil {
		t.Fatal(err)
	}
	if _, err := nextconfig.Open(dir).Set(map[string]any{"workflow_profile": "poteto"}); err != nil {
		t.Fatal(err)
	}
	_, err = starter.Start(ctx, place, "codex", "", "0f1e2d3c-0000-4000-8000-000000000002", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(terminal.command, "Poteto workflow") {
		t.Fatal("resume received the new machine workflow")
	}
	match = regexp.MustCompile(`clawdline-workflow:([A-Za-z0-9_-]{22})`).FindStringSubmatch(terminal.command)
	if len(match) != 2 {
		t.Fatal("resume missing original-snapshot launch identity")
	}
	resumed, found, err := st.WorkflowLaunchByID(ctx, match[1])
	if err != nil || !found || resumed.SnapshotID != launch.SnapshotID {
		t.Fatalf("resume snapshot changed: %+v %v", resumed, err)
	}
}
