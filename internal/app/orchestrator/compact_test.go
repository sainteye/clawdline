package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// When the Claude sessions this broker opens compact (compact.go): the
// machine's `claude_auto_compact_window`, a task.json's override, and what is
// recorded of either.

func windowOf(n int64) func() int64 { return func() int64 { return n } }

// dispatchWith sends one brief through the whole dispatch, with the child's
// tab answering as assistant's composer does, and answers the record and the
// line its tab was opened with.
func dispatchWith(t *testing.T, b *Broker, id, assistant string, extra map[string]any) (Record, string) {
	t.Helper()
	b.Clock = steppingClock(time.Date(2026, 9, 25, 6, 0, 0, 0, time.UTC), 10*time.Second)
	launcher := &recordingLauncher{pane: "%90"}
	b.Launcher = launcher
	b.Type = (&typedKeys{}).Type
	live := session.AssistantClaude
	if assistant == "codex" {
		live = session.AssistantCodex
	}
	b.Live = func(context.Context) []session.Session {
		return []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
			{ID: "%90", Backend: session.BackendTmux, Assistant: live},
		}
	}
	b.Screen = func(_ context.Context, pane string) (string, bool) {
		return screen(t, assistant+"-composer"), pane == "%90"
	}
	brief := map[string]any{"assistant": assistant, "permission_mode": "full"}
	for k, v := range extra {
		brief[k] = v
	}
	writeBrief(t, b, id, b.Dir, brief)
	out, err := b.Dispatch(context.Background(), DispatchRequest{TaskID: id, Secret: w1Secret, Offered: true})
	if err != nil {
		t.Fatal(err)
	}
	return out.Record, launcher.line()
}

func windowIs(w *int64, want int64) bool { return w != nil && *w == want }

// The launch line with the setting and without it. Off — the default — the
// line is exactly what it was before this setting existed: nothing is set and
// nothing is unset that was not unset already.
func TestAClaudeChildIsLaunchedWithTheMachinesWindow(t *testing.T) {
	b, _ := newTestBroker(t)
	r, line := dispatchWith(t, b, "c2800001-0000-4000-8000-000000000001", "claude", nil)
	if strings.Contains(line, AutoCompactEnv) {
		t.Fatalf("no setting, and the line still sets the window:\n%s", line)
	}
	if !windowIs(r.AutoCompactWindow, 0) {
		t.Fatalf("no setting is recorded as %v, want 0 (none applied)", r.AutoCompactWindow)
	}

	b, _ = newTestBroker(t)
	b.AutoCompactWindow = windowOf(120_000)
	r, line = dispatchWith(t, b, "c2800002-0000-4000-8000-000000000002", "claude", nil)
	if !strings.HasPrefix(line, "env -u CLAUDECODE ") || !strings.Contains(line, " "+AutoCompactEnv+"=120000 claude ") {
		t.Fatalf("the window is not in the child's environment:\n%s", line)
	}
	if !windowIs(r.AutoCompactWindow, 120_000) || r.AutoCompactRequested != nil {
		t.Fatalf("recorded window %v requested %v, want 120000 and nothing requested",
			r.AutoCompactWindow, r.AutoCompactRequested)
	}
}

// A task.json's `auto_compact_window` wins over the setting for its one task:
// a number is that window, null is none whatever the setting says.
func TestATaskOverridesTheWindowForItself(t *testing.T) {
	b, _ := newTestBroker(t)
	b.AutoCompactWindow = windowOf(120_000)
	r, line := dispatchWith(t, b, "c2800003-0000-4000-8000-000000000003", "claude",
		map[string]any{"auto_compact_window": 60_000})
	if !strings.Contains(line, AutoCompactEnv+"=60000 ") || strings.Contains(line, "120000") {
		t.Fatalf("the task's window did not replace the setting's:\n%s", line)
	}
	if !windowIs(r.AutoCompactWindow, 60_000) || !windowIs(r.AutoCompactRequested, 60_000) {
		t.Fatalf("recorded %v requested %v", r.AutoCompactWindow, r.AutoCompactRequested)
	}

	b, _ = newTestBroker(t)
	b.AutoCompactWindow = windowOf(120_000)
	id := "c2800004-0000-4000-8000-000000000004"
	r, line = dispatchWith(t, b, id, "claude", map[string]any{"auto_compact_window": nil})
	if strings.Contains(line, AutoCompactEnv) {
		t.Fatalf("null asked for none and the line sets one:\n%s", line)
	}
	if !windowIs(r.AutoCompactWindow, 0) || !windowIs(r.AutoCompactRequested, 0) {
		t.Fatalf("null is recorded as %v requested %v, want 0 and 0", r.AutoCompactWindow, r.AutoCompactRequested)
	}
	// The file a respawn copies still says null: the retried half of a
	// comparison runs with the same window.
	body, err := os.ReadFile(filepath.Join(b.Tasks.Path(id), "task.json"))
	if err != nil {
		t.Fatal(err)
	}
	var brief map[string]json.RawMessage
	if err := json.Unmarshal(body, &brief); err != nil {
		t.Fatal(err)
	}
	if got, ok := brief["auto_compact_window"]; !ok || string(got) != "null" {
		t.Fatalf("task.json's auto_compact_window after dispatch is %q (present %v), want null", got, ok)
	}
}

// Codex is never given the window: not from the setting, and a task that
// names one for a Codex child is refused by name rather than run without it.
func TestCodexIsNeverGivenTheWindow(t *testing.T) {
	b, _ := newTestBroker(t)
	b.AutoCompactWindow = windowOf(120_000)
	r, line := dispatchWith(t, b, "c2800005-0000-4000-8000-000000000005", "codex", nil)
	if strings.Contains(line, AutoCompactEnv) || strings.Contains(line, "120000") {
		t.Fatalf("a Codex child's line carries the window:\n%s", line)
	}
	if r.AutoCompactWindow != nil {
		t.Fatalf("a Codex child is recorded with window %d; the question does not apply", *r.AutoCompactWindow)
	}

	// The line itself refuses too, whatever a record says.
	launch, err := projects.Admit(projects.LaunchRequest{ProjectRoot: "/work/app", Assistant: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	forced := int64(90_000)
	if got := shellCommand(launch, Record{Assistant: "codex", AutoCompactWindow: &forced}, "/tmp/tasks", "/work/app"); strings.Contains(got, AutoCompactEnv) {
		t.Fatalf("a Codex line built from a record with a window carries it:\n%s", got)
	}

	b, _ = newTestBroker(t)
	id := "c2800006-0000-4000-8000-000000000006"
	writeBrief(t, b, id, b.Dir, map[string]any{"assistant": "codex", "auto_compact_window": 90_000})
	_, err = b.ReadDraft(id)
	if refusalCode(err) != "bad_task" || refusalMessage(err) != "auto_compact_window is only valid when assistant is claude" {
		t.Fatalf("a Codex task naming a window: %q / %q", refusalCode(err), refusalMessage(err))
	}
	writeBrief(t, b, id, b.Dir, map[string]any{"assistant": "codex", "auto_compact_window": nil})
	if _, err := b.ReadDraft(id); err != nil {
		t.Fatalf("null on a Codex task is already true of it and is refused: %v", err)
	}
}

// The bounds, on a task.json. The settings file takes the same range, and a
// test in internal/transport/http holds the two to one another.
func TestTheWindowIsBounded(t *testing.T) {
	lo, hi := AutoCompactBounds()
	if lo != 50_000 || hi != 1_000_000 {
		t.Fatalf("bounds %d…%d", lo, hi)
	}
	b, _ := newTestBroker(t)
	id := "c2800007-0000-4000-8000-000000000007"
	for _, c := range []struct {
		value any
		ok    bool
	}{
		{lo, true}, {hi, true}, {lo - 1, false}, {hi + 1, false}, {0, false},
		{-5, false}, {60_000.5, false}, {"60000", false}, {true, false},
	} {
		writeBrief(t, b, id, b.Dir, map[string]any{"auto_compact_window": c.value})
		r, err := b.ReadDraft(id)
		switch {
		case c.ok && err != nil:
			t.Errorf("%v refused: %v", c.value, err)
		case c.ok && !windowIs(r.AutoCompactRequested, toInt(c.value)):
			t.Errorf("%v admitted as %v", c.value, r.AutoCompactRequested)
		case !c.ok && (refusalCode(err) != "bad_task" || !strings.Contains(refusalMessage(err), "50000 to 1000000")):
			t.Errorf("%v: %q / %q", c.value, refusalCode(err), refusalMessage(err))
		}
	}
	// A hand-edited settings file with a value out of range is read as none,
	// not clamped into a window nobody chose.
	b.AutoCompactWindow = windowOf(10)
	if w := b.autoCompactFor("claude", nil); !windowIs(w, 0) {
		t.Fatalf("an out-of-range setting launches with %v", w)
	}
}

func toInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	}
	return -1
}

// A Root Assignment or a handoff's receiver is a Claude session this broker
// opens too: it carries the machine's window, and its record says which.
func TestASessionTheBrokerOpensCarriesTheWindow(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.AutoCompactWindow = windowOf(200_000)
	launcher := &recordingLauncher{pane: "%91"}
	b.Launcher = launcher
	project := t.TempDir()
	opened, err := b.openSession(ctx, project, "clawdline-root-test", "claude", "", []string{b.Dir})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(launcher.line(), " "+AutoCompactEnv+"=200000 claude ") {
		t.Fatalf("the opened session's line does not carry the window:\n%s", launcher.line())
	}
	if !windowIs(opened.AutoCompactWindow, 200_000) {
		t.Fatalf("the opened session records %v", opened.AutoCompactWindow)
	}

	opened, err = b.openSession(ctx, project, "clawdline-root-test2", "codex", "", []string{b.Dir})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(launcher.line(), AutoCompactEnv) || opened.AutoCompactWindow != nil {
		t.Fatalf("a Codex session was given the window (%v):\n%s", opened.AutoCompactWindow, launcher.line())
	}

	b.AutoCompactWindow = nil
	opened, err = b.openSession(ctx, project, "clawdline-root-test3", "claude", "", []string{b.Dir})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(launcher.line(), AutoCompactEnv) || !windowIs(opened.AutoCompactWindow, 0) {
		t.Fatalf("with no setting, recorded %v and line:\n%s", opened.AutoCompactWindow, launcher.line())
	}
}
