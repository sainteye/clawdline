package app

import (
	"context"
	"errors"
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
