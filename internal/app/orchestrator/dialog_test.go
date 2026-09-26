package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// A child whose first screen is a dialog is left for a person (dialog.go).

// bypassScreen is the first screen a scheduled child opened with
// --permission-mode bypassPermissions drew on 2026-09-26: a dialog, no
// composer, "No, exit" under the highlight.
const bypassScreen = stallRule + "\n  WARNING: Claude Code running in Bypass Permissions mode\n\n" +
	"  By proceeding, you accept all responsibility for actions taken while running\n" +
	"  in Bypass Permissions mode.\n\n" +
	"  ❯ No, exit\n    Yes, I accept\n\n  Enter to confirm · Esc to cancel\n"

const dialogSecret = "d1a10900d1a10900d1a10900d1a10900"

// newDialogRig is the stall rig's child, left at a dialog with its secret
// still held, the way Dispatch leaves one.
func newDialogRig(t *testing.T, id string) *stallRig {
	t.Helper()
	x := newStallRig(t, id)
	x.show(bypassScreen)
	if _, err := x.b.mutate(x.ctx, id, "task.spawned", func(r *Record) error {
		r.AwaitingDialogSince = x.now
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	x.b.rememberSecret(id, dialogSecret)
	return x
}

// The briefing stops on a dialog without typing and without failing: the tab
// is kept for a person, and the record says since when.
func TestABriefingLeavesADialogForAPerson(t *testing.T) {
	b, ctx := newTestBroker(t)
	keys := &typedKeys{}
	b.Type = keys.Type
	b.Launcher = &openingLauncher{pane: "%71"}
	b.Live = func(context.Context) []session.Session {
		return []session.Session{{ID: "%71", Assistant: session.AssistantClaude}}
	}
	b.Screen = func(context.Context, string) (string, bool) { return bypassScreen, true }
	r := Record{Protocol: Protocol, ID: "d1a10001-0000-4000-8000-000000000001", Assistant: "claude",
		Title: "daily", Claims: []string{}, ScheduleID: "5c000000-0000-4000-8000-000000000004"}

	started := time.Now()
	got := b.spawn(ctx, r, t.TempDir(), dialogSecret, func() {})
	if took := time.Since(started); took > 30*time.Second {
		t.Fatalf("the briefing waited %v on a dialog before leaving it", took)
	}
	if got.State != StateSpawning || got.AwaitingDialogSince.IsZero() || got.Unbriefed || got.SpawnError != "" {
		t.Fatalf("state %q since %v unbriefed %v error %q: want spawning, left at the dialog",
			got.State, got.AwaitingDialogSince, got.Unbriefed, got.SpawnError)
	}
	if n := keys.count("%71"); n != 0 {
		t.Fatalf("typed %d lines at a dialog", n)
	}
}

// The spawn clock does not judge a child left at a dialog, and neither does
// the stall watch: nothing is typed and nothing ends while the dialog is up.
// Once the person answers and a composer is drawn, the briefing — secret and
// all — is typed exactly once.
func TestADialogAnsweredInTheTabIsBriefedOnce(t *testing.T) {
	x := newDialogRig(t, "d1a10002-0000-4000-8000-000000000002")

	x.run(readyLimit + stallIdleLimit + time.Minute)
	if r := x.record(); r.State != StateSpawning || r.AwaitingDialogSince.IsZero() {
		t.Fatalf("after %v at a dialog: %q, since %v; want still spawning and waiting",
			readyLimit+stallIdleLimit, r.State, r.AwaitingDialogSince)
	}
	if got := x.nudges(); len(got) != 0 {
		t.Fatalf("typed %q at a dialog", got)
	}

	x.show(stalledScreen)
	x.run(time.Minute)
	got := x.nudges()
	if len(got) != 1 || !strings.Contains(got[0], "TASK_SECRET="+dialogSecret) {
		t.Fatalf("once the composer was up, typed %q; want the briefing once", got)
	}
	if r := x.record(); r.State != StateSpawning || !r.AwaitingDialogSince.IsZero() {
		t.Fatalf("after the briefing: %q, since %v; want spawning and no longer waiting", r.State, r.AwaitingDialogSince)
	}
	if _, held := x.b.heldSecret(x.id); held {
		t.Fatal("the secret is still held after it was typed")
	}
	if n := x.events("task.briefed_after_dialog"); n != 1 {
		t.Fatalf("task.briefed_after_dialog events = %d, want 1", n)
	}
}

// Answered by leaving — the assistant is gone from the tab — the task ends,
// saying so, with nothing typed.
func TestADialogAnsweredByLeavingEndsTheTask(t *testing.T) {
	x := newDialogRig(t, "d1a10003-0000-4000-8000-000000000003")
	x.b.Reading = func(context.Context) session.Inventory {
		return session.Inventory{Complete: true, Sources: map[string]bool{"tmux": true},
			Sessions: []session.Session{
				{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
				{ID: x.pane, Backend: session.BackendTmux},
			}}
	}
	x.run(time.Minute)
	r := x.record()
	if r.State != StateSpawnFailed || r.Verdict != dialogLeftVerdict {
		t.Fatalf("%q: %q; want spawn_failed, left", r.State, r.Verdict)
	}
	if got := x.nudges(); len(got) != 0 {
		t.Fatalf("typed %q", got)
	}
}

// A daemon that restarted during the wait holds no secret, so nobody can
// brief the child: it ends as unbriefed, never typed at.
func TestADialogOutlivingItsSecretEndsTheTask(t *testing.T) {
	x := newDialogRig(t, "d1a10004-0000-4000-8000-000000000004")
	x.b.forgetSecret(x.id)
	x.show(stalledScreen)
	x.run(time.Minute)
	r := x.record()
	if r.State != StateSpawnFailed || r.Verdict != dialogLostVerdict {
		t.Fatalf("%q: %q; want spawn_failed, secret lost", r.State, r.Verdict)
	}
	if got := x.nudges(); len(got) != 0 {
		t.Fatalf("typed %q with no secret", got)
	}
}

// Nobody answers: the task's own timeout ends the wait.
func TestADialogNobodyAnswersEndsAtTheTimeout(t *testing.T) {
	x := newDialogRig(t, "d1a10005-0000-4000-8000-000000000005")
	x.run(241 * time.Minute)
	if r := x.record(); r.State != StateTimeout {
		t.Fatalf("%q after the timeout at a dialog, want timeout", r.State)
	}
	if got := x.nudges(); len(got) != 0 {
		t.Fatalf("typed %q at a dialog", got)
	}
}
