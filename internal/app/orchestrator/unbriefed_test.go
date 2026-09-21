package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app/lane"
	"github.com/sainteye/clawdline/internal/domain/session"
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
	id          string
	itermClosed []string
}

func (l *itermLauncher) CloseITermChild(_ context.Context, id string, _ time.Time) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.itermClosed = append(l.itermClosed, id)
	return true, nil
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

// A briefing whose typing failed with an outcome nobody can know — the script
// was killed after the paste, the terminal answered something unreadable — is
// not a fact: the keystrokes may have landed. The dispatch leaves it
// `spawning`, and it is not marked unbriefed.
//
// The error here is a terminal that did not answer, not a lane that timed out:
// a lane is waited for before the first byte, so "the lane timed out" is the
// one failure that proves nothing was typed (the review of e54e338, F2).
func TestATypedBriefingThatErredIsNotSettled(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.Clock = steppingClock(time.Date(2026, 9, 19, 4, 0, 0, 0, time.UTC), 50*time.Second)
	b.Launcher = &openingLauncher{pane: "%74"}
	b.Type = func(context.Context, string, string) error {
		return errors.New("send_failed: iTerm2 did not answer in time")
	}
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
	if out.Record.State != StateSpawning || out.Record.Unbriefed ||
		out.Record.SpawnError != "send_failed: iTerm2 did not answer in time" {
		t.Fatalf("state %q unbriefed %v spawn_error %q", out.Record.State, out.Record.Unbriefed, out.Record.SpawnError)
	}
}

// F1: a typing whose outcome is unknown is not typed again. The line may be
// sitting in the child's composer, or already read; a second one is the child
// told its first sentence twice, or two briefings joined into one message.
func TestABriefingWhoseOutcomeIsUnknownIsNotTypedAgain(t *testing.T) {
	b, ctx := newTestBroker(t)
	// Thirty seconds a reading: the ninety-second wait has room for two tries.
	b.Clock = steppingClock(time.Date(2026, 9, 19, 4, 0, 0, 0, time.UTC), 30*time.Second)
	b.Launcher = &openingLauncher{pane: "%75"}
	var mu sync.Mutex
	tries := 0
	b.Type = func(context.Context, string, string) error {
		mu.Lock()
		defer mu.Unlock()
		tries++
		return terminal.Failure{Message: "iTerm2 did not answer in time."}
	}
	b.Live = func(context.Context) []session.Session {
		return []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
			{ID: "%75", Backend: session.BackendTmux, Assistant: session.AssistantClaude},
		}
	}
	out, err := dispatchOne(t, b, ctx, "c8000006-0000-4000-8000-000000000006")
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if tries != 1 {
		t.Fatalf("the briefing was typed %d times after an attempt whose outcome was unknown, want 1", tries)
	}
	if out.Record.State != StateSpawning || out.Record.Unbriefed {
		t.Fatalf("state %q unbriefed %v", out.Record.State, out.Record.Unbriefed)
	}
}

// F2: a typing refused before its first byte is not a typing. The lane that
// never came free and the terminal that answered "not found" before writing
// anything leave the child exactly as unbriefed as never trying did — so the
// dispatch settles it as spawn_failed with that reason, rather than leaving it
// `spawning` until a timeout that cannot be respawned.
func TestABriefingRefusedBeforeAnyByteIsSettledAsNeverBriefed(t *testing.T) {
	for name, refusal := range map[string]error{
		"busy":   fmt.Errorf("busy: %w", lane.Busy{Key: "tmux:%76", Limit: 4, Waited: true}),
		"unsent": fmt.Errorf("send_failed: %w", terminal.Unsent{Why: "That session is gone"}),
	} {
		t.Run(name, func(t *testing.T) {
			b, ctx := newTestBroker(t)
			b.Clock = steppingClock(time.Date(2026, 9, 19, 4, 0, 0, 0, time.UTC), 50*time.Second)
			launcher := &openingLauncher{pane: "%76"}
			b.Launcher = launcher
			b.Type = func(context.Context, string, string) error { return refusal }
			b.Live = func(context.Context) []session.Session {
				return []session.Session{
					{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
					{ID: "%76", Backend: session.BackendTmux, Assistant: session.AssistantClaude},
				}
			}
			out, err := dispatchOne(t, b, ctx, "c8000007-0000-4000-8000-000000000007")
			if err != nil {
				t.Fatal(err)
			}
			r := out.Record
			if r.State != StateSpawnFailed || !r.Unbriefed {
				t.Fatalf("a briefing refused before its first byte left %q unbriefed=%v (spawn_error %q)",
					r.State, r.Unbriefed, r.SpawnError)
			}
			if !strings.Contains(r.Verdict, refusal.Error()) {
				t.Fatalf("verdict %q does not carry the refusal %q", r.Verdict, refusal.Error())
			}
			if len(launcher.closed) != 1 {
				t.Fatalf("closed %v, want the pane this dispatch made", launcher.closed)
			}
		})
	}
}

// F3: a screen reader that failed this once is not a machine with no screen
// reader. The child may be showing its workspace-trust dialog, whose
// highlighted answer is "No, exit"; typing the briefing and its Return at it
// is the accident composer.go records. Not ready, and asked again.
func TestAChildWhoseScreenCouldNotBeReadIsNotTypedAt(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.Clock = steppingClock(time.Date(2026, 9, 19, 4, 0, 0, 0, time.UTC), 50*time.Second)
	b.Launcher = &openingLauncher{pane: "%77"}
	keys := &typedKeys{}
	b.Type = keys.Type
	b.Screen = func(context.Context, string) (string, bool) { return "", false }
	b.Live = func(context.Context) []session.Session {
		return []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
			{ID: "%77", Backend: session.BackendTmux, Assistant: session.AssistantClaude},
		}
	}
	out, err := dispatchOne(t, b, ctx, "c8000008-0000-4000-8000-000000000008")
	if err != nil {
		t.Fatal(err)
	}
	if n := keys.count("%77"); n != 0 {
		t.Fatalf("the briefing was typed %d time(s) at a screen nobody could read", n)
	}
	r := out.Record
	if r.State != StateSpawnFailed || !r.Unbriefed || !strings.Contains(r.SpawnError, "screen") {
		t.Fatalf("state %q unbriefed %v spawn_error %q", r.State, r.Unbriefed, r.SpawnError)
	}
}

// F5: an iTerm2 tab this dispatch opened and could not brief is closed with
// the settlement, by the session id iTerm2 gave back — as a tmux one is. The
// verdict tells the root to dispatch again; a tab left behind each time is an
// idle assistant per retry, on the machine that was already too slow.
func TestAnUnbriefedITermChildIsClosed(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.Clock = steppingClock(time.Date(2026, 9, 19, 4, 0, 0, 0, time.UTC), 50*time.Second)
	const guid = "4F1C0000-0000-4000-8000-00000000A009"
	launcher := &itermLauncher{id: guid}
	b.Launcher = launcher
	b.Terminal = func() projects.TerminalChoice { return projects.TerminalITerm }
	b.Type = (&typedKeys{}).Type
	out, err := dispatchOne(t, b, ctx, "c8000009-0000-4000-8000-000000000009")
	if err != nil {
		t.Fatal(err)
	}
	if out.Record.State != StateSpawnFailed {
		t.Fatalf("state %q", out.Record.State)
	}
	if len(launcher.itermClosed) != 1 || launcher.itermClosed[0] != guid {
		t.Fatalf("closed %v, want the iTerm2 session this dispatch opened", launcher.itermClosed)
	}
	if len(launcher.closed) != 0 {
		t.Fatalf("a tmux close was asked for an iTerm2 tab: %v", launcher.closed)
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
