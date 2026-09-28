package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

func TestLinuxWithoutTmuxIsNeverToldToOpenITermOnAMac(t *testing.T) {
	for _, choice := range []projects.TerminalChoice{projects.TerminalAuto, projects.TerminalITerm} {
		got := unavailableTerminal(choice, "linux")
		if got.Code != "terminal_unsupported" || strings.Contains(got.Message, "not running") {
			t.Errorf("%s: %+v", choice, got)
		}
	}

	mac := unavailableTerminal(projects.TerminalAuto, "darwin")
	if mac.Code != "terminal_closed" || mac.App != "iTerm2" || !strings.Contains(mac.Message, "not running") {
		t.Fatalf("darwin: %+v", mac)
	}
}

// A Codex started or resumed from the console is told not to ask about
// updating before it draws its composer, as the broker's children are
// (projects.UpdateCheckArgs): on 2026-09-28 one opened on "Update available!"
// and a line sent to it from the console was pasted into that menu. Claude
// Code has no such question and is given nothing.
func TestACodexStartedFromTheConsoleSkipsItsUpdateCheck(t *testing.T) {
	place := projects.Place{Path: t.TempDir()}
	term := &personaTerminal{}
	conversation := "0f1e2d3c-0000-4000-8000-000000000003"
	s := Starter{
		Terminal: func() projects.TerminalChoice { return projects.TerminalTmux },
		Launcher: term,
		Past: func(context.Context, projects.Place, string) []projects.Past {
			return []projects.Past{{ID: conversation}}
		},
	}
	ctx := context.Background()
	const flag = "-c check_for_update_on_startup=false"

	if _, err := s.Start(ctx, place, "codex", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(term.command, " codex "+flag) {
		t.Fatalf("codex start: %s", term.command)
	}
	if _, err := s.Resume(ctx, place, conversation, "codex", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(term.command, " codex resume "+conversation+" "+flag) {
		t.Fatalf("codex resume: %s", term.command)
	}
	if _, err := s.Start(ctx, place, "claude", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(term.command, "check_for_update") {
		t.Fatalf("claude start: %s", term.command)
	}
}

func TestANewSessionUsesTheMachinesDefaultModel(t *testing.T) {
	place := projects.Place{Path: t.TempDir()}
	term := &personaTerminal{}
	conversation := "0f1e2d3c-0000-4000-8000-000000000004"
	s := Starter{
		Terminal: func() projects.TerminalChoice { return projects.TerminalTmux },
		Launcher: term,
		Past: func(context.Context, projects.Place, string) []projects.Past {
			return []projects.Past{{ID: conversation}}
		},
		DefaultModel: func(assistant string) string {
			if assistant == "codex" {
				return "gpt-6-sol"
			}
			return "claude-opus-5-5"
		},
	}
	ctx := context.Background()

	started, err := s.Start(ctx, place, "codex", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if started.Model != "gpt-6-sol" || !strings.Contains(term.command, " codex --model gpt-6-sol ") {
		t.Fatalf("codex default: %+v / %s", started, term.command)
	}
	started, err = s.Start(ctx, place, "codex", "gpt-5.6-luna", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if started.Model != "gpt-5.6-luna" || !strings.Contains(term.command, "--model gpt-5.6-luna") || strings.Contains(term.command, "gpt-6-sol") {
		t.Fatalf("explicit override: %+v / %s", started, term.command)
	}
	started, err = s.Resume(ctx, place, conversation, "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if started.Model != "" || strings.Contains(term.command, "--model") {
		t.Fatalf("resume changed model: %+v / %s", started, term.command)
	}
}
