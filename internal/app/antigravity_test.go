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
