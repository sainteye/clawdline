package orchestrator

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/projects"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// docs/switch-blockers.md: a child this daemon opened and never briefed.
//
// Measured before the change: an iTerm2 tab opened under the session id iTerm2
// gave back, this daemon's reading listed the same tab under its tty, the
// briefing waited ninety seconds for an id nobody listed and gave up without a
// keystroke — and the task sat in `spawning` until its own timeout, then ended
// as `timeout` with a sentence that named nothing that had gone wrong.

// itermLauncher is an iTerm2 that opens every tab under one session id.
type itermLauncher struct {
	fakeLauncher
	id string
}

func (l *itermLauncher) ITermRunning(context.Context) (bool, error) { return true, nil }
func (l *itermLauncher) NewITermTab(context.Context, string) (string, error) {
	return l.id, nil
}

// steppingClock moves on by step every time it is read, so the briefing's
// ninety-second wait is over in one real two-second pause.
func steppingClock(start time.Time, step time.Duration) func() time.Time {
	var mu sync.Mutex
	now := start
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(step)
		return now
	}
}

// The tab is open and its assistant is running, but the id the terminal gave
// back is not the id the reading lists it under. Nothing is typed, and the
// dispatch answers spawn_failed at once, with the reason in the record — not
// `spawning` until the task's own timeout.
func TestASpawnWhoseIDTheReadingDoesNotListIsSettledAsNeverBriefed(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.Clock = steppingClock(time.Date(2026, 9, 19, 4, 0, 0, 0, time.UTC), 50*time.Second)
	const guid = "4F1C0000-0000-4000-8000-00000000A001"
	b.Launcher = &itermLauncher{id: guid}
	b.Terminal = func() projects.TerminalChoice { return projects.TerminalITerm }
	keys := &typedKeys{}
	b.Type = keys.Type
	b.Live = func(context.Context) []session.Session {
		return []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
			// The child, as a reading whose iTerm2 half failed lists it: by tty.
			{ID: "ttys031", Backend: session.BackendITerm, TTY: "ttys031", Assistant: session.AssistantClaude},
		}
	}
	id := "c8000001-0000-4000-8000-000000000001"
	out, err := dispatchOne(t, b, ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	r := out.Record
	if r.State != StateSpawnFailed {
		t.Fatalf("a child that was never briefed is %q, want spawn_failed now (spawn_error %q)", r.State, r.SpawnError)
	}
	if keys.count(guid) != 0 || keys.count("ttys031") != 0 {
		t.Fatal("something was typed")
	}
	if !r.Unbriefed || !strings.Contains(r.SpawnError, guid) || !strings.Contains(r.SpawnError, "never listed") {
		t.Fatalf("the record does not say why: unbriefed=%v spawn_error=%q", r.Unbriefed, r.SpawnError)
	}
	if !strings.Contains(r.Verdict, "never typed") || !strings.Contains(r.Verdict, r.SpawnError) {
		t.Fatalf("verdict %q does not carry the briefing's reason", r.Verdict)
	}
	if r.ChildTerminalID != guid || r.ChildBackend != "iterm" || r.SpawnedAt.IsZero() {
		t.Fatalf("the tab it opened is not recorded: %+v", r)
	}
	if stored, _, _ := b.Record(ctx, id); stored.State != StateSpawnFailed {
		t.Fatalf("stored state %q", stored.State)
	}
}

// A tmux pane this dispatch made and could not brief is closed with the
// settlement, by the pane id tmux gave back.
func TestAnUnbriefedTmuxChildIsClosed(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.Clock = steppingClock(time.Date(2026, 9, 19, 4, 0, 0, 0, time.UTC), 50*time.Second)
	launcher := &openingLauncher{pane: "%73"}
	b.Launcher = launcher
	b.Type = (&typedKeys{}).Type
	id := "c8000002-0000-4000-8000-000000000002"
	out, err := dispatchOne(t, b, ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if out.Record.State != StateSpawnFailed {
		t.Fatalf("state %q", out.Record.State)
	}
	if len(launcher.closed) != 1 || launcher.closed[0] != [2]string{"%73", ChildSessionName(id)} {
		t.Fatalf("closed %v, want the pane this dispatch made", launcher.closed)
	}
}

// A briefing that was typed and errored is not a fact: the keystrokes may have
// landed. The dispatch leaves it `spawning`, and it is not marked unbriefed.
func TestATypedBriefingThatErredIsNotSettled(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.Clock = steppingClock(time.Date(2026, 9, 19, 4, 0, 0, 0, time.UTC), 50*time.Second)
	b.Launcher = &openingLauncher{pane: "%74"}
	b.Type = func(context.Context, string, string) error { return errors.New("the lane timed out") }
	b.Live = func(context.Context) []session.Session {
		return []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
			{ID: "%74", Backend: session.BackendTmux, Assistant: session.AssistantClaude},
		}
	}
	out, err := dispatchOne(t, b, ctx, "c8000003-0000-4000-8000-000000000003")
	if err != nil {
		t.Fatal(err)
	}
	if out.Record.State != StateSpawning || out.Record.Unbriefed || out.Record.SpawnError != "the lane timed out" {
		t.Fatalf("state %q unbriefed %v spawn_error %q", out.Record.State, out.Record.Unbriefed, out.Record.SpawnError)
	}
}

// The beat is the backstop for a dispatch that recorded Unbriefed and did not
// get to settle: four minutes on, it decides on that fact alone — on a Mac
// whose iTerm2 listing never completes, where no reading could decide it.
func TestTheSpawnClockSettlesAnUnbriefedChildWithoutAReading(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 19, 4, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	b.Launcher = &fakeLauncher{}
	b.Reading = func(context.Context) session.Inventory {
		return session.Inventory{Sources: map[string]bool{"iterm": false, "ps": true, "tmux": true},
			Sessions: []session.Session{{ID: "ttys031", Backend: session.BackendITerm, TTY: "ttys031",
				Assistant: session.AssistantClaude}}}
	}
	store := func(id string, unbriefed bool) {
		r := Record{Protocol: Protocol, ID: id, Assistant: "claude", State: StateSpawning, CreatedAt: now,
			Claims: []string{}, TimeoutMinutes: 240, ChildTerminalID: "4F1C0000-0000-4000-8000-00000000A00" + id[len(id)-1:],
			ChildBackend: "iterm", SpawnedAt: now.Add(-5 * time.Minute), Unbriefed: unbriefed,
			SpawnError: "the child session did not reach a prompt"}
		if err := b.save(ctx, r, HashSecret("s"), "task.spawned"); err != nil {
			t.Fatal(err)
		}
	}
	never, typed := "c8000004-0000-4000-8000-000000000004", "c8000005-0000-4000-8000-000000000005"
	store(never, true)
	store(typed, false)
	p := b.Pass(ctx)
	if r, _, _ := b.Record(ctx, never); r.State != StateSpawnFailed || !strings.Contains(r.Verdict, "never typed") {
		t.Fatalf("the unbriefed child is %q (%q), pulse %+v", r.State, r.Verdict, p)
	}
	if r, _, _ := b.Record(ctx, typed); r.State != StateSpawning {
		t.Fatalf("a child whose briefing was typed was decided %q on an incomplete reading", r.State)
	}
}
