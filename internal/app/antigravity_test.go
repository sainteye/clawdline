package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

func TestAntigravityStarterRequiresPreparedContextBeforeOpen(t *testing.T) {
	term := &personaTerminal{}
	s := Starter{Terminal: func() projects.TerminalChoice { return projects.TerminalTmux }, Launcher: term,
		Language: func(string) string { return "zh-Hant-TW" }, Memory: func(string) string { return "synthetic memory index" }}
	place := projects.Place{Path: t.TempDir()}
	if _, err := s.Start(context.Background(), place, projects.AssistantAntigravity, "", "", ""); err == nil || term.command != "" {
		t.Fatal("unprepared launch", err, term.command)
	}
	s.PrepareAntigravity = func(_ context.Context, r projects.LaunchRequest) (projects.LaunchRequest, []string, error) {
		c, err := projects.PrepareAntigravityContext("", "", r.Memory, r.Language)
		r.AntigravityContext = &c
		r.Executable = "/validated/standalone"
		r.PermissionMode = "plan"
		return r, []string{"CLAWDLINE_TEST_LAUNCH='synthetic'"}, err
	}
	if _, err := s.Start(context.Background(), place, projects.AssistantAntigravity, "", "", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(term.command, "'/validated/standalone' --mode plan") || strings.Contains(term.command, "claude") || strings.Contains(term.command, "--append-system-prompt") {
		t.Fatal(term.command)
	}
	term.command = ""
	s.PrepareAntigravity = func(_ context.Context, r projects.LaunchRequest) (projects.LaunchRequest, []string, error) {
		return r, nil, projects.ErrSquadContextUnavailable
	}
	if _, err := s.Start(context.Background(), place, projects.AssistantAntigravity, "", "", ""); !errors.Is(err, projects.ErrSquadContextUnavailable) || term.command != "" {
		t.Fatal(err, term.command)
	}
}

func TestAntigravityNewLaunchUsesItsOwnEffortAndResumeKeepsTheOriginal(t *testing.T) {
	term := &personaTerminal{}
	place := projects.Place{Path: t.TempDir()}
	reads := 0
	var requests []projects.LaunchRequest
	s := Starter{
		Terminal: func() projects.TerminalChoice { return projects.TerminalTmux }, Launcher: term,
		DefaultReasoningEffort: func(assistant string) string {
			reads++
			if assistant == projects.AssistantAntigravity {
				return "medium"
			}
			return "xhigh"
		},
		PrepareAntigravity: func(_ context.Context, r projects.LaunchRequest) (projects.LaunchRequest, []string, error) {
			requests = append(requests, r)
			c, err := projects.PrepareAntigravityContext("", "", r.Memory, r.Language)
			r.AntigravityContext = &c
			r.Executable = "/validated/standalone"
			r.PermissionMode = "plan"
			return r, nil, err
		},
	}
	if _, err := s.Start(context.Background(), place, projects.AssistantAntigravity, "", "", ""); err != nil {
		t.Fatal(err)
	}
	if reads != 1 || requests[0].ReasoningEffort != "medium" || !strings.Contains(term.command, "--effort medium") || strings.Contains(term.command, "xhigh") {
		t.Fatalf("new launch did not select native effort: reads=%d request=%+v line=%s", reads, requests[0], term.command)
	}
	const id = "0f1e2d3c-0000-4000-8000-000000000006"
	if _, err := s.Start(context.Background(), place, projects.AssistantAntigravity, "", id, ""); err != nil {
		t.Fatal(err)
	}
	if reads != 1 || len(requests) != 2 || requests[1].ReasoningEffort != "" || strings.Contains(term.command, "--effort") || !strings.Contains(term.command, id) {
		t.Fatalf("resume reread a mutable effort: reads=%d request=%+v line=%s", reads, requests[1], term.command)
	}
}
