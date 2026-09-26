package terminal

import (
	"context"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// ProcessCloser ends a session only the process table saw (ports.ProcessCloser).
//
// Such a row is an assistant on a tty that no backend here lists — Terminal,
// an editor's terminal, a tmux server on a socket of its own. There is no tab
// or pane to take away and no way to type the quit word into it, so the close
// is the farewell ladder's signal rungs alone: SIGTERM, which both assistants
// take as a request to flush and leave, then SIGKILL, each only while the
// process in front of that tty is still the one the reading named. Whatever
// window the tty belongs to is left to its owner: it was never this daemon's.
type ProcessCloser struct{}

var _ ports.ProcessCloser = ProcessCloser{}

// CloseProcess asks the session's process to leave, and answers once it has.
func (ProcessCloser) CloseProcess(ctx context.Context, s session.Session) error {
	return processFarewell().leave(ctx, s)
}

// processFarewell is the ladder with nothing to type and nothing to close:
// no polite rung, because there is no word to wait for.
func processFarewell() farewell {
	return farewell{
		look:   func(_ context.Context, s session.Session) (farewellSight, error) { return processSight(s.TTY, s) },
		signal: ttySignal,
		close:  func(context.Context, session.Session) error { return nil },
		polite: 0, afterTerm: farewellAfterTerm, afterKill: farewellAfterKill,
		tick: farewellTick, now: time.Now,
	}
}

// leave is say without the word: the look, the refusals, and the signals.
func (f farewell) leave(ctx context.Context, s session.Session) error {
	if s.TTY == "" {
		return Unsent{Why: "that session names no terminal, so there is no process to ask"}
	}
	sight, err := f.look(ctx, s)
	if err != nil {
		return Unreadable{Why: "What is running on " + s.TTY + " could not be read (" + err.Error() +
			"), so it was left as it was."}
	}
	if sight.Gone {
		return Unsent{Why: "That session is gone"}
	}
	assistant, why := ours(s, sight)
	if why != "" {
		return Occupied{Why: "What holds " + s.TTY + " now is not this close's to end (" + why +
			"), so it was left as it was."}
	}
	if !f.canSignal(sight) {
		return StillRunning{Why: "The " + string(assistant) + " on " + s.TTY +
			" is in a terminal this machine cannot type into, and it cannot be signalled here either, " +
			"so it was left running."}
	}
	return f.waitAndClose(ctx, s, sight, assistant, "being asked to close", nil)
}
