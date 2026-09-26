package orchestrator

import (
	"context"
	"errors"

	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// A child whose first screen is a dialog is left for a person to answer.
//
// Measured on 2026-09-26: a schedule dispatched with permission_mode full
// opened Claude Code with --permission-mode bypassPermissions on a machine
// that had never accepted that mode, and its first screen was
//
//	WARNING: Claude Code running in Bypass Permissions mode
//	…
//	❯ No, exit
//	  Yes, I accept
//
// The briefing rightly typed nothing at it (composer.go) — but it then waited
// out its 90 seconds, the task ended spawn_failed and its tab was closed. The
// dialog was on screen for a minute and a half, in a tab nobody was looking
// at, and then it was gone: the person pressed Run again and again and never
// once saw the question they were the only one allowed to answer.
//
// So a dialog is handed to the person instead of waited out. The briefing
// stops on it (two readings in a row, so a frame drawn mid-start is not
// taken for one), the task stays `spawning` with AwaitingDialogSince set, and
// the tab stays open. It is then an ordinary session standing on a question:
// the console draws its buttons (app.Inventory reads a dialog's menu without
// the registry's gate, because nothing but a dialog has a caret with no
// composer under it) and the waiting push tells the person, with the time the
// task has left (docs/push.md "有人在等你回答").
//
// The beat then watches the tab (tendDialog):
//
//   - a composer is up — the person answered and the session went on: the
//     briefing is typed, once, and the task goes on as any briefed child;
//   - the assistant is gone from the tab, or the tab is gone — the person
//     answered by leaving, or closed it: spawn_failed, saying so;
//   - still the dialog: nothing. The task's own timeout ends the wait.
//
// The secret stays in this process's memory for the wait, as it does for the
// ninety seconds of an ordinary briefing, and never at rest. A daemon that
// restarts during the wait has lost it, and the task ends as unbriefed: it
// cannot be briefed by anybody, and respawning it is the way on.

// errShowingDialog is composerReady finding a dialog where the composer
// should be.
var errShowingDialog = errors.New("the child is showing a dialog; the briefing would have answered it")

// dialogReadings is how many readings in a row must show a dialog before the
// briefing stops and leaves it for a person. Two readings are two seconds
// apart (brief).
const dialogReadings = 2

// awaitingDialog is brief stopping on a dialog without having typed anything.
type awaitingDialog struct{}

func (awaitingDialog) Error() string {
	return "the child is showing a dialog, left in its tab for a person to answer"
}

// heldSecret is the plaintext secret this process still holds for id.
func (b *Broker) heldSecret(id string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.secrets[id]
	return s, ok
}

// takeSecret is heldSecret that also lets it go, so one secret is typed at
// most once however many beats see the composer.
func (b *Broker) takeSecret(id string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.secrets[id]
	delete(b.secrets, id)
	return s, ok
}

const (
	dialogLostVerdict = "The child's first screen was a dialog, and this daemon restarted while it waited for a " +
		"person to answer it. The secret the briefing carries is never kept at rest, so nothing can brief " +
		"this child now. Dispatch it again."
	dialogLeftVerdict = "The child's first screen was a dialog, and it was answered by leaving: the assistant " +
		"is no longer running in the tab, so the briefing was never typed."
	dialogClosedVerdict = "The child's first screen was a dialog, and its tab was closed before anybody " +
		"answered it, so the briefing was never typed."
)

// tendDialog is the beat's look at a child left at a dialog. It answers true
// when the task became terminal.
func (b *Broker) tendDialog(ctx context.Context, rd reading, r Record, p *Pulse) bool {
	if _, held := b.heldSecret(r.ID); !held {
		return b.endDialog(ctx, r, dialogLostVerdict, true, p)
	}
	s, present := rd.session(r.ChildTerminalID)
	switch {
	case !present && rd.sourceComplete(r.ChildBackend):
		return b.endDialog(ctx, r, dialogClosedVerdict, false, p)
	case !present:
		// A source that could not answer says nothing about the tab.
		return false
	case !s.IsAssistant():
		return b.endDialog(ctx, r, dialogLeftVerdict, true, p)
	}
	if ready, _ := b.composerReady(ctx, r.ChildTerminalID, r.Assistant); !ready {
		return false
	}
	secret, held := b.takeSecret(r.ID)
	if !held {
		return false
	}
	err := b.typeLine(ctx, r.ChildTerminalID, FirstLine(r, secret, b.Language))
	if err != nil && nothingTyped(err) {
		// Nothing left this process: the next beat tries again.
		b.rememberSecret(r.ID, secret)
		return false
	}
	// Typed, or possibly typed: either way never again, and the task is an
	// ordinary briefed child from here — a failure that may have landed is
	// left to the receipt and the stall watch, as brief leaves it.
	_, _ = b.mutate(ctx, r.ID, "task.briefed_after_dialog", func(t *Record) error {
		if t.State != StateSpawning || t.AwaitingDialogSince.IsZero() {
			return errUnchanged
		}
		t.AwaitingDialogSince = time.Time{}
		if err != nil {
			t.SpawnError = err.Error()
		}
		return nil
	})
	return false
}

// endDialog settles a child left at a dialog as spawn_failed, closing its tab
// when there is one to close.
func (b *Broker) endDialog(ctx context.Context, r Record, why string, present bool, p *Pulse) bool {
	_, ids, err := b.settle(ctx, r.ID, StateSpawnFailed, why, nil, b.closeChild(r, present)...)
	if err != nil {
		return false
	}
	p.SpawnFail++
	for _, res := range b.runRecorded(ctx, ids) {
		if res.state == store.EffectDone && res.outcome == "closed" {
			p.Closed++
		}
	}
	return true
}

// leftAtDialog is whether r is a child left at a dialog.
func (r Record) leftAtDialog() bool {
	return r.State == StateSpawning && !r.AwaitingDialogSince.IsZero()
}
