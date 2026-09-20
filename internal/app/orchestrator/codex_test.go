package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/projects"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// A Codex child, from the launch line to the briefing typed into it.
//
// Measured before this: fifty-odd Claude children went out on this daemon and
// every one of them was briefed; the first Codex one, dispatched 2026-09-20,
// came back `spawn_failed`, "the child session did not reach a prompt".
// It had reached one. Two separate things stood in the way and either
// alone was enough, which is why both are tested here rather than one:
//
//  1. Codex asks whether the directory is one to trust before it draws
//     anything, and a child's directory is always new (trustArgs);
//  2. the composer it draws once past that is not framed, and a frame was the
//     only thing the screen was read for (composer.go).

// recordingLauncher is a tmux that keeps the command line it was given.
type recordingLauncher struct {
	fakeLauncher
	pane    string
	command string
}

func (l *recordingLauncher) TmuxReach(context.Context) int { return 2 }
func (l *recordingLauncher) NewTmuxSession(_ context.Context, _, _, command string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.command = command
	return l.pane, nil
}

func (l *recordingLauncher) line() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.command
}

// The whole path, with the child's screen answering as the captured one does.
func TestACodexChildIsLaunchedPastTheTrustDialogAndBriefed(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.Clock = steppingClock(time.Date(2026, 9, 20, 6, 0, 0, 0, time.UTC), 10*time.Second)
	launcher := &recordingLauncher{pane: "%80"}
	b.Launcher = launcher
	keys := &typedKeys{}
	b.Type = keys.Type
	b.Live = func(context.Context) []session.Session {
		return []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
			{ID: "%80", Backend: session.BackendTmux, Assistant: session.AssistantCodex},
		}
	}
	b.Screen = func(_ context.Context, id string) (string, bool) {
		return screen(t, "codex-composer"), id == "%80"
	}
	id := "c9000001-0000-4000-8000-000000000001"
	writeBrief(t, b, id, b.Dir, map[string]any{"assistant": "codex", "permission_mode": "full"})
	out, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret, Offered: true})
	if err != nil {
		t.Fatal(err)
	}
	if out.Record.State != StateSpawning || out.Record.Unbriefed {
		t.Fatalf("a Codex child that drew its composer is %q (unbriefed=%v, spawn_error %q)",
			out.Record.State, out.Record.Unbriefed, out.Record.SpawnError)
	}
	if keys.count("%80") != 1 {
		t.Fatalf("the briefing was typed %d times, want once", keys.count("%80"))
	}
	line := launcher.line()
	for _, want := range []string{
		"codex ",
		"--ask-for-approval never --sandbox workspace-write",
		`-c 'projects={"` + b.Dir + `"={trust_level="trusted"}}'`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("the launch line does not carry %q:\n%s", want, line)
		}
	}
}

// The same child on a machine where the trust dialog still comes up — an older
// Codex, a directory answered "No, quit" before. Nothing is typed, and the
// record says what was on the screen rather than that nothing was.
func TestACodexChildHoldingTheTrustDialogIsNeverTypedInto(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.Clock = steppingClock(time.Date(2026, 9, 20, 6, 0, 0, 0, time.UTC), 50*time.Second)
	b.Launcher = &recordingLauncher{pane: "%81"}
	keys := &typedKeys{}
	b.Type = keys.Type
	b.Live = func(context.Context) []session.Session {
		return []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
			{ID: "%81", Backend: session.BackendTmux, Assistant: session.AssistantCodex},
		}
	}
	b.Screen = func(_ context.Context, id string) (string, bool) {
		return screen(t, "codex-trust"), id == "%81"
	}
	id := "c9000002-0000-4000-8000-000000000002"
	writeBrief(t, b, id, b.Dir, map[string]any{"assistant": "codex", "permission_mode": "full"})
	out, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret, Offered: true})
	if err != nil {
		t.Fatal(err)
	}
	if keys.count("%81") != 0 {
		t.Fatal("the briefing was typed at a dialog; its Return would have answered it")
	}
	if out.Record.State != StateSpawnFailed || !out.Record.Unbriefed {
		t.Fatalf("state %q unbriefed %v", out.Record.State, out.Record.Unbriefed)
	}
	if !strings.Contains(out.Record.SpawnError, "showing a dialog") {
		t.Fatalf("the record does not say what was on the screen: %q", out.Record.SpawnError)
	}
}

// A child that never draws anything is a different sentence from one holding a
// dialog, and both are different from the sentence this used to end with.
// "The child session did not reach a prompt" was what a Codex child with a live
// composer was recorded as, and it named neither what was there nor what was
// looked for.
func TestAChildThatDrewNoInputLineSaysSo(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.Clock = steppingClock(time.Date(2026, 9, 20, 6, 0, 0, 0, time.UTC), 50*time.Second)
	b.Launcher = &recordingLauncher{pane: "%82"}
	b.Type = (&typedKeys{}).Type
	b.Live = func(context.Context) []session.Session {
		return []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
			{ID: "%82", Backend: session.BackendTmux, Assistant: session.AssistantCodex},
		}
	}
	b.Screen = func(_ context.Context, id string) (string, bool) { return "Loading…", id == "%82" }
	id := "c9000003-0000-4000-8000-000000000003"
	writeBrief(t, b, id, b.Dir, map[string]any{"assistant": "codex"})
	out, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret, Offered: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Record.SpawnError, "no input line Codex draws") {
		t.Fatalf("the record does not say what was looked for: %q", out.Record.SpawnError)
	}
}

// The launch line, per assistant. The permission ceiling is spelled the way
// each CLI spells it, and only Codex is told the directory is one to work in —
// Claude Code neither takes that flag nor asks that question.
func TestTheLaunchLineIsSpelledForItsOwnCLI(t *testing.T) {
	const cwd = "/private/tmp/cl-launch-probe"
	const tasks = "/private/tmp/cl-launch-tasks"
	for _, c := range []struct {
		assistant, mode string
		want, notWant   []string
	}{
		{"codex", "full",
			[]string{"--add-dir '" + tasks + "'", "--ask-for-approval never --sandbox workspace-write",
				`-c 'projects={"` + cwd + `"={trust_level="trusted"}}'`},
			[]string{"--permission-mode"}},
		{"codex", "edits",
			[]string{"--ask-for-approval on-request --sandbox workspace-write", "trust_level"},
			[]string{"--permission-mode"}},
		{"claude", "full",
			[]string{"--add-dir '" + tasks + "'", "--permission-mode bypassPermissions"},
			[]string{"trust_level", "--ask-for-approval"}},
	} {
		launch, err := projects.Admit(projects.LaunchRequest{ProjectRoot: cwd, Assistant: c.assistant})
		if err != nil {
			t.Fatal(err)
		}
		line := shellCommand(launch, Record{Assistant: c.assistant, PermissionMode: c.mode}, tasks, cwd)
		for _, want := range c.want {
			if !strings.Contains(line, want) {
				t.Errorf("%s/%s: the launch line does not carry %q:\n%s", c.assistant, c.mode, want, line)
			}
		}
		for _, not := range c.notWant {
			if strings.Contains(line, not) {
				t.Errorf("%s/%s: the launch line carries %q, which is the other CLI's:\n%s", c.assistant, c.mode, not, line)
			}
		}
	}
}
