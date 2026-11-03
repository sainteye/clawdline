package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
		DefaultReasoningEffort: func(assistant string) string { return "xhigh" },
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
	if started.Model != "gpt-6-sol" || !strings.Contains(term.command, " codex --model gpt-6-sol --config model_reasoning_effort=xhigh") {
		t.Fatalf("codex default: %+v / %s", started, term.command)
	}
	started, err = s.Start(ctx, place, "codex", "gpt-5.6-luna", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if started.Model != "gpt-5.6-luna" || !strings.Contains(term.command, "--model gpt-5.6-luna") || strings.Contains(term.command, "gpt-6-sol") || !strings.Contains(term.command, "model_reasoning_effort=xhigh") {
		t.Fatalf("explicit override: %+v / %s", started, term.command)
	}
	started, err = s.Resume(ctx, place, conversation, "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if started.Model != "" || strings.Contains(term.command, "--model") || strings.Contains(term.command, "model_reasoning_effort") {
		t.Fatalf("resume changed model: %+v / %s", started, term.command)
	}
}

func TestResumeVerifiesTheSelectionWithoutRelistingHistory(t *testing.T) {
	place := projects.Place{Path: t.TempDir()}
	id := "0f1e2d3c-0000-4000-8000-000000000005"
	term := &personaTerminal{}
	failure := StartRefusal{Status: 503, Code: "codex_unavailable", Message: "retry"}
	s := Starter{
		Terminal: func() projects.TerminalChoice { return projects.TerminalTmux }, Launcher: term,
		Past: func(context.Context, projects.Place, string) []projects.Past {
			t.Fatal("resume relisted history")
			return nil
		},
		VerifyPast: func(_ context.Context, p projects.Place, assistant, selected string) (bool, error) {
			if p.Path != place.Path || assistant != "codex" || selected != id {
				t.Fatal("wrong selection")
			}
			return false, failure
		},
	}
	_, err := s.Resume(context.Background(), place, id, "codex", "")
	var refused StartRefusal
	if !errors.As(err, &refused) || refused.Code != "codex_unavailable" || term.command != "" {
		t.Fatalf("err=%v command=%q", err, term.command)
	}
	s.VerifyPast = func(context.Context, projects.Place, string, string) (bool, error) { return true, nil }
	if _, err = s.Resume(context.Background(), place, id, "codex", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(term.command, "codex resume "+id) {
		t.Fatalf("wrong command %q", term.command)
	}
}

func TestSelectedPersonalProjectStillStartsAndValidatesAtUse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "Documents", "selected-project")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	place := projects.Place{ID: projects.PlaceID(path), Path: path}
	terminal := &personaTerminal{}
	starter := Starter{Places: func(context.Context) []projects.Place { return []projects.Place{place} }, Terminal: func() projects.TerminalChoice { return projects.TerminalTmux }, Launcher: terminal}
	chosen, ok := starter.Place(context.Background(), place.ID)
	if !ok {
		t.Fatal("selected choice was unavailable")
	}
	if _, err := starter.Start(context.Background(), chosen, "claude", "", "", ""); err != nil {
		t.Fatalf("explicit Start was blocked: %v", err)
	}
	if terminal.command == "" {
		t.Fatal("selected Project never reached launcher")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := starter.Start(context.Background(), chosen, "claude", "", "", ""); err == nil {
		t.Fatal("selected missing path was not validated at use")
	}
}
