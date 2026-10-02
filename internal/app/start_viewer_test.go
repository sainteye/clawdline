package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

// viewerTerminal is a Mac with iTerm2 open and tmux installed.
type viewerTerminal struct {
	tabErr   error
	session  string // the name tmux was asked for
	command  string
	tabLines []string
}

func (f *viewerTerminal) ITermRunning(context.Context) (bool, error) { return true, nil }
func (f *viewerTerminal) TmuxReach(context.Context) int              { return int(projects.TmuxInstalled) }
func (f *viewerTerminal) NewITermTab(_ context.Context, line string) (string, error) {
	f.tabLines = append(f.tabLines, line)
	if f.tabErr != nil {
		return "", f.tabErr
	}
	return "TAB-1", nil
}
func (f *viewerTerminal) NewTmuxWindow(context.Context, string, string) (string, error) {
	return "", errors.New("a viewed session never opens a window")
}
func (f *viewerTerminal) NewTmuxSession(_ context.Context, _, name, command string) (string, error) {
	f.session, f.command = name, command
	return "%90", nil
}
func (f *viewerTerminal) PrepareTmuxViewer(_ context.Context, name string) (string, error) {
	return "exec '/usr/bin/tmux' attach -t '=" + name + "'", nil
}
func (f *viewerTerminal) CloseTmuxSession(context.Context, string, string) (bool, error) {
	return true, nil
}

func TestAStartWithITermAndTmuxIsATmuxPaneShownInATab(t *testing.T) {
	place := projects.Place{Path: t.TempDir()}
	for name, c := range map[string]struct {
		tabErr     error
		wantAttach bool
	}{
		"tab opened":     {},
		"tab not opened": {tabErr: errors.New("iTerm2 did not answer in time."), wantAttach: true},
	} {
		t.Run(name, func(t *testing.T) {
			term := &viewerTerminal{tabErr: c.tabErr}
			s := Starter{Terminal: func() projects.TerminalChoice { return projects.TerminalAuto }, Launcher: term}
			got, err := s.Start(context.Background(), place, projects.AssistantClaude, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if got.ID != "%90" || got.Backend != "tmux" {
				t.Errorf("started %+v, want the tmux pane", got)
			}
			if !strings.HasPrefix(term.session, "clawdline-session-") || !strings.Contains(term.command, "claude") {
				t.Errorf("tmux session %q running %q", term.session, term.command)
			}
			want := "exec '/usr/bin/tmux' attach -t '=" + term.session + "'"
			if len(term.tabLines) != 1 || term.tabLines[0] != want {
				t.Errorf("tab typed %q, want %q", term.tabLines, want)
			}
			if c.wantAttach != (got.Attach == projects.TmuxAttachSessionCommand(term.session)) || !c.wantAttach && got.Attach != "" {
				t.Errorf("attach %q", got.Attach)
			}
		})
	}
}

// `iterm_native` is the tab it always was: the program in the iTerm2 tab.
func TestITermNativeStartsInTheTab(t *testing.T) {
	place := projects.Place{Path: t.TempDir()}
	term := &viewerTerminal{}
	s := Starter{Terminal: func() projects.TerminalChoice { return projects.TerminalITermNative }, Launcher: term}
	got, err := s.Start(context.Background(), place, projects.AssistantClaude, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "TAB-1" || got.Backend != "iterm" || term.session != "" {
		t.Errorf("started %+v, tmux session %q", got, term.session)
	}
	if len(term.tabLines) != 1 || !strings.Contains(term.tabLines[0], "claude") {
		t.Errorf("tab typed %q", term.tabLines)
	}
}
