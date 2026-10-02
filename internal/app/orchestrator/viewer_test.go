package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// viewedLauncher is a machine with iTerm2 open and tmux installed: a child
// opens in a tmux session and an iTerm2 tab attaches to it.
type viewedLauncher struct {
	recordingLauncher
	tabErr  error
	tabLine string
	viewed  string
}

func (l *viewedLauncher) ITermRunning(context.Context) (bool, error) { return true, nil }
func (l *viewedLauncher) TmuxReach(context.Context) int              { return 1 }
func (l *viewedLauncher) PrepareTmuxViewer(_ context.Context, name string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.viewed = name
	return "exec '/usr/bin/tmux' attach -t '=" + name + "'", nil
}
func (l *viewedLauncher) NewITermTab(_ context.Context, line string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.tabLine = line
	if l.tabErr != nil {
		return "", l.tabErr
	}
	return "TAB-VIEWER", nil
}

// A child on a machine with iTerm2 and tmux is the tmux pane — recorded,
// briefed and later closed through tmux — and the iTerm2 tab only attaches to
// its session. A tab that did not open leaves the child started and briefed.
func TestAChildWithITermAndTmuxIsATmuxPaneShownInATab(t *testing.T) {
	for name, tabErr := range map[string]error{
		"tab opened":     nil,
		"tab not opened": errors.New("iTerm2 did not answer in time."),
	} {
		t.Run(name, func(t *testing.T) {
			b, ctx := newTestBroker(t)
			b.Clock = steppingClock(time.Date(2026, 10, 2, 6, 0, 0, 0, time.UTC), 10*time.Second)
			launcher := &viewedLauncher{recordingLauncher: recordingLauncher{pane: "%82"}, tabErr: tabErr}
			b.Launcher = launcher
			keys := &typedKeys{}
			b.Type = keys.Type
			b.Live = func(context.Context) []session.Session {
				return []session.Session{
					{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
					{ID: "%82", Backend: session.BackendTmux, Assistant: session.AssistantCodex},
					// The viewer tab: its tty runs `tmux attach`, no assistant.
					{ID: "TAB-VIEWER", Backend: session.BackendITerm},
				}
			}
			b.Screen = func(_ context.Context, id string) (string, bool) {
				return screen(t, "codex-composer"), id == "%82"
			}
			id := "c9000003-0000-4000-8000-000000000003"
			writeBrief(t, b, id, b.Dir, map[string]any{"assistant": "codex", "permission_mode": "full"})
			out, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret, Offered: true})
			if err != nil {
				t.Fatal(err)
			}
			r := out.Record
			if r.State != StateSpawning || r.ChildBackend != "tmux" || r.ChildTerminalID != "%82" {
				t.Fatalf("state %q backend %q terminal %q (spawn_error %q)",
					r.State, r.ChildBackend, r.ChildTerminalID, r.SpawnError)
			}
			if keys.count("%82") != 1 || keys.count("TAB-VIEWER") != 0 {
				t.Fatalf("briefing typed %d times into the pane and %d into the tab",
					keys.count("%82"), keys.count("TAB-VIEWER"))
			}
			want := ChildSessionName(id)
			if launcher.viewed != want || launcher.tabLine != "exec '/usr/bin/tmux' attach -t '="+want+"'" {
				t.Errorf("viewer for %q typed %q", launcher.viewed, launcher.tabLine)
			}
		})
	}
}
