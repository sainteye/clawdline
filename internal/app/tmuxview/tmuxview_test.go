package tmuxview

import (
	"context"
	"errors"
	"regexp"
	"testing"
)

// launcher is a terminal that records what it was asked and fails what it is
// told to.
type launcher struct {
	tmuxErr, prepareErr, tabErr error

	sessions []string // name, cwd, command
	prepared []string
	tabs     []string
}

func (l *launcher) ITermRunning(context.Context) (bool, error) { return true, nil }
func (l *launcher) TmuxReach(context.Context) int              { return 1 }
func (l *launcher) NewITermTab(_ context.Context, line string) (string, error) {
	l.tabs = append(l.tabs, line)
	if l.tabErr != nil {
		return "", l.tabErr
	}
	return "TAB-1", nil
}
func (l *launcher) NewTmuxWindow(context.Context, string, string) (string, error) {
	return "", errors.New("a viewer never opens a window")
}
func (l *launcher) NewTmuxSession(_ context.Context, cwd, name, command string) (string, error) {
	l.sessions = append(l.sessions, name, cwd, command)
	if l.tmuxErr != nil {
		return "", l.tmuxErr
	}
	return "%42", nil
}
func (l *launcher) PrepareTmuxViewer(_ context.Context, name string) (string, error) {
	l.prepared = append(l.prepared, name)
	if l.prepareErr != nil {
		return "", l.prepareErr
	}
	return "exec '/usr/bin/tmux' attach -t '=" + name + "'", nil
}
func (l *launcher) CloseTmuxSession(context.Context, string, string) (bool, error) { return false, nil }

func TestOpenRecordsThePaneAndShowsItInATab(t *testing.T) {
	l := &launcher{}
	got, err := Open(context.Background(), l, "/work", "clawdline-task-aaaaaaaa", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if got != (Opened{PaneID: "%42", TabID: "TAB-1"}) {
		t.Errorf("opened %+v", got)
	}
	if want := []string{"clawdline-task-aaaaaaaa", "/work", "claude"}; !equal(l.sessions, want) {
		t.Errorf("tmux was asked %q, want %q", l.sessions, want)
	}
	if want := []string{"exec '/usr/bin/tmux' attach -t '=clawdline-task-aaaaaaaa'"}; !equal(l.tabs, want) {
		t.Errorf("the tab was typed %q, want %q", l.tabs, want)
	}
}

// The work started; a tab that did not open does not make it a failure, and
// nothing is started twice. The answer says how to see it.
func TestOpenWithoutATabStillAnswersThePane(t *testing.T) {
	for name, l := range map[string]*launcher{
		"tab refused":       {tabErr: errors.New("iTerm2 did not answer in time.")},
		"viewer unprepared": {prepareErr: errors.New("tmux is not installed.")},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Open(context.Background(), l, "/work", "clawdline-session-0f0f0f0f", "claude")
			if err != nil {
				t.Fatalf("a started session was answered as a failure: %v", err)
			}
			want := Opened{PaneID: "%42", Attach: "tmux attach -t '=clawdline-session-0f0f0f0f'"}
			if got != want {
				t.Errorf("opened %+v, want %+v", got, want)
			}
			if len(l.sessions) != 3 {
				t.Errorf("tmux was asked for %d sessions, want 1", len(l.sessions)/3)
			}
		})
	}
}

// No tmux session is the launcher's failure as it was, and no tab is opened
// onto nothing.
func TestOpenWithoutATmuxSessionOpensNoTab(t *testing.T) {
	refused := errors.New("tmux would not start a server.")
	l := &launcher{tmuxErr: refused}
	got, err := Open(context.Background(), l, "/work", "clawdline-session-0f0f0f0f", "claude")
	if !errors.Is(err, refused) {
		t.Fatalf("err = %v, want the launcher's", err)
	}
	if got != (Opened{}) || len(l.tabs) != 0 || len(l.prepared) != 0 {
		t.Errorf("opened %+v, tabs %q, prepared %q", got, l.tabs, l.prepared)
	}
}

func TestSessionNameIsFreshAndATmuxTarget(t *testing.T) {
	shape := regexp.MustCompile(`^clawdline-session-[0-9a-f]{8}$`)
	a, b := SessionName("session"), SessionName("session")
	if !shape.MatchString(a) || a == b {
		t.Errorf("names %q and %q", a, b)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
