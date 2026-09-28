package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// A Feature Root whose first screen is a dialog is left for a person to
// answer, as a child is (dialog.go).
//
// Measured on 2026-09-27: two Board items assigned to a new Codex Session
// both opened on Codex's own "Update available! … 1. Update now 2. Skip"
// menu. The briefing rightly typed nothing at it — but it then waited out its
// 150 seconds, recorded the assignment `failed` ("the child is showing a
// dialog") and the Board item went back to Created. The tab stayed open with
// the question on it, and nothing watched it any more: answering it started a
// Codex that had never been told what it was for.
//
// So the wait stops on a dialog (two readings in a row, as a child's does) and
// the Root Assignment becomes `awaiting_dialog`. The beat then watches the tab:
//
//   - a composer is up — the person answered and the session went on: the
//     line is typed, once, and the Root Assignment is briefed;
//   - the assistant is gone from the tab, or the tab is gone: failed, saying
//     so;
//   - still the dialog: nothing. There is no clock: the Board item says it is
//     waiting on the person, and closing the tab is the way to give up.
//
// Unlike a child's, the line carries no secret — it names the brief file — so
// a daemon that restarts during the wait still briefs the root afterwards.
// Either ending is reported to RootAssignmentSettled.

const (
	rootDialogLeftFailure = "brief_failed: the first screen was a dialog, and it was answered by leaving: " +
		"the assistant is no longer running in the tab, so the assignment was never typed"
	rootDialogClosedFailure = "brief_failed: the first screen was a dialog, and its tab was closed before " +
		"anybody answered it, so the assignment was never typed"
)

// waitComposerOrDialog is waitComposer that stops, with errShowingDialog, as
// soon as dialogReadings readings in a row showed a dialog: a question only a
// person may answer is not something waiting longer will change.
func (b *Broker) waitComposerOrDialog(ctx context.Context, terminalID, assistant string, within time.Duration) error {
	deadline := b.now().Add(within)
	var last error
	dialogs := 0
	for b.now().Before(deadline) {
		if s, ok := b.sessionByTerminal(ctx, terminalID); ok && s.IsAssistant() {
			ready, why := b.composerReady(ctx, terminalID, assistant)
			if ready {
				return nil
			}
			if errors.Is(why, errShowingDialog) {
				if dialogs++; dialogs >= dialogReadings {
					return errShowingDialog
				}
			} else {
				dialogs = 0
			}
			if why != nil {
				last = why
			}
		} else {
			dialogs = 0
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if last == nil {
		last = errors.New("the session did not reach a prompt")
	}
	return last
}

// tendRootDialogs is the beat's look at every Feature Root left at a dialog.
// It answers how many it settled.
func (b *Broker) tendRootDialogs(ctx context.Context, rd reading) int {
	rows, err := b.Store.ListOpenedIn(ctx, store.TableRootAssignments, AssignmentAwaitingDialog, assignmentListLimit)
	if err != nil {
		return 0
	}
	settled := 0
	for _, o := range rows {
		var a RootAssignment
		if json.Unmarshal(o.Record, &a) != nil || a.State != AssignmentAwaitingDialog {
			continue
		}
		if out, done := b.tendRootDialog(ctx, rd, a); done {
			settled++
			if b.RootAssignmentSettled != nil {
				b.RootAssignmentSettled(ctx, out)
			}
		}
	}
	return settled
}

func (b *Broker) tendRootDialog(ctx context.Context, rd reading, a RootAssignment) (RootAssignment, bool) {
	if a.Executor == nil {
		return b.failAssignment(ctx, a.ID, "brief_failed: the Root Assignment names no terminal to brief"), true
	}
	s, present := rd.session(a.Executor.TerminalID)
	switch {
	case !present && rd.sourceComplete(a.Executor.Backend):
		return b.failAssignment(ctx, a.ID, rootDialogClosedFailure), true
	case !present:
		// A source that could not answer says nothing about the tab.
		return a, false
	case !s.IsAssistant():
		return b.failAssignment(ctx, a.ID, rootDialogLeftFailure), true
	}
	if ready, _ := b.composerReady(ctx, a.Executor.TerminalID, a.Assistant); !ready {
		return a, false
	}
	out, err := b.briefAssignment(ctx, a)
	switch {
	case err != nil && nothingTyped(err):
		// Nothing reached the tab: the next beat tries again.
		return a, false
	case err != nil:
		return b.failAssignment(ctx, a.ID, "brief_failed: "+err.Error()), true
	}
	return out, true
}
