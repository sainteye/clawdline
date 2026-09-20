package terminal

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// Closing a session a person asked to close.
//
// **Two closes were written on this daemon and only one of them learned.**
// `CloseITermChild` (iterm_close_darwin.go) ends a finished child's job before
// it closes its tab, because iTerm2 puts a sheet on the person's screen when a
// tab with a job in it is closed, holds the Apple Event past its ten-second
// limit, and leaves the tab open until somebody answers. The close a person
// presses went straight to the backend — `found.close()` on iTerm2,
// `kill-pane` on tmux — so it produced exactly that sheet, and the assistant
// it was meant to take away was still running behind it.
//
// This is the ladder both of them now walk, in this order:
//
//  1. **The assistant's own quit word**, typed at its prompt, so it leaves the
//     way it would if somebody typed it: the transcript it is appending to is
//     flushed rather than cut off by a hung-up tty. Claude Code leaves on
//     `/exit` and Codex on `/quit`, and each refuses the other's — a refused
//     word would leave the session open with the terminal closing under it.
//  2. **The wait, and then the escalation.** A session at its prompt is gone
//     inside a second; a session in the middle of a tool call has queued the
//     word and will not read it until the tool returns, which more waiting
//     does not fix. `SIGTERM` is how a program is asked to leave and both
//     assistants flush on the way out; `SIGKILL` is the last rung. Neither is
//     harsher than what the old close did anyway, which was to hang up the tty
//     underneath them.
//  3. **The close, and only once the terminal is provably empty.** Elapsed
//     time is never evidence of absence: the terminal is read again, and a tab
//     whose foreground is back at its shell is the only one that is closed.
//
// **It is not `CloseITermChild` with a different caller, and the difference is
// the premise.** That one is closing a tab this daemon opened for a task that
// has ended, so "every process in front started before the task ended" is a
// sound proof that what it is signalling is the child's. A person's close has
// no such moment: the tab may be one they opened themselves, and what is in it
// now may have nothing to do with the row they pressed. So the proof here is
// the reading the close is acting on — see `ours`, which is the whole of what
// may be signalled and what may not.

// QuitLine is the line that ends a session, typed at its prompt.
//
// Claude Code takes `/exit` and Codex takes `/quit`. The second answer says
// whether this assistant has one at all: a row that is not an assistant is
// never typed into, because nothing here knows what a word would mean to
// whatever is running in that terminal.
func QuitLine(a session.Assistant) (string, bool) {
	switch a {
	case session.AssistantClaude:
		return "/exit", true
	case session.AssistantCodex:
		return "/quit", true
	}
	return "", false
}

// assistantOfComm is the assistant a process name belongs to, as the process
// table's own classification spells it (internal/adapters/process): the base
// name of the program, never a line that merely mentions one.
//
// The kernel's short name is all there is here — sixteen characters, no
// arguments — so an assistant started through a wrapper reads as the wrapper.
// That is why it is never the only evidence: a row the inventory already
// classified carries its pid, and `ours` prefers that.
func assistantOfComm(comm string) session.Assistant {
	switch comm {
	case "claude":
		return session.AssistantClaude
	case "codex":
		return session.AssistantCodex
	}
	return ""
}

// pause waits d, or until ctx is done, and answers whether it waited it out.
// Every wait in this package is one of these: a close that lost its caller
// stops waiting rather than finishing a ladder nobody is listening to.
func pause(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// escalation is a rung past the polite word.
type escalation int

const (
	escalateTerm escalation = iota
	escalateKill
)

func (e escalation) String() string {
	if e == escalateKill {
		return "SIGKILL"
	}
	return "SIGTERM"
}

// farewellSight is one look into a session: what holds its terminal now.
//
// Each backend fills it from the best evidence it has, and says in its own
// comment what `Job` means there. Zero — no `Gone`, no `Job` — is a terminal
// with nothing in front of it, which is the one state that may simply be
// closed.
type farewellSight struct {
	// Gone is a session the backend no longer has. Nothing is left to close.
	Gone bool
	// Job is something holding the terminal's foreground: anything but a
	// shell at its prompt.
	Job bool
	// Assistant is what that something is, when it could be told. Empty means
	// either that it is not an assistant or that this machine could not say —
	// and neither is permission to signal it.
	Assistant session.Assistant
	// PID and Start are the kernel's identity for the job's leader: a pid
	// alone is not one, because a pid can be reused between two rungs of this
	// ladder. Zero when the processes behind the terminal could not be read.
	PID   int
	Start time.Time
	// Group is the process group a signal is sent to, so an assistant's own
	// helpers leave with it rather than being left behind on the tty. Zero
	// when there is nothing this machine may signal.
	Group int
}

// terminalName is how this sighting's process is named in the log: the pid is
// the only identity a reader can look up afterwards.
func (s farewellSight) terminalName() string { return fmt.Sprintf("pid %d", s.PID) }

// farewell is the ladder, with every step it takes on the machine passed in,
// so the order and the refusals can be tested without a terminal.
type farewell struct {
	// look reads the session now. It is asked again at every rung: what was
	// true when the word was typed is not evidence a second later.
	look func(ctx context.Context, s session.Session) (farewellSight, error)
	// send types one line into the session and submits it.
	send func(ctx context.Context, s session.Session, line string) error
	// signal asks a process group to leave. Nil is a machine that cannot —
	// the ladder then stops at the polite word rather than pretending.
	signal func(group int, step escalation) error
	// close takes the session away.
	close func(ctx context.Context, s session.Session) error

	polite, afterTerm, afterKill, tick time.Duration
	now                                func() time.Time
}

// farewellPolite is how long the quit word gets before anything harsher.
//
// Three seconds, and short on purpose: a session at its prompt reads it inside
// one, and a session in the middle of a tool call has not read it at all and
// will not for as long as the tool takes. Waiting longer is only waiting, and
// this runs while somebody is looking at a card that says "closing".
const (
	farewellPolite    = 3 * time.Second
	farewellAfterTerm = 2 * time.Second
	farewellAfterKill = 1 * time.Second
	farewellTick      = 150 * time.Millisecond
)

// ours is whether this close may end what is in front of the terminal, and why
// not when it may not.
//
// **Everything this ladder is allowed to do to a process is decided here**, so
// the cases it refuses are listed rather than implied:
//
//   - The row is not an assistant. Nothing here knows how to ask a command to
//     leave, and a close that signalled one would be ending work somebody
//     started, not a session they asked to close.
//   - What is in front is not an assistant, or is a different one. The session
//     the reading named has already ended and the terminal has been reused;
//     what is in it now was never what this close was about.
//   - It is not the process the reading named. A pid that does not match is
//     plainly a different process, and it is newer than anything this close
//     knows about — the reading it is acting on is the only account it has of
//     what that terminal held.
//   - The processes behind the terminal could not be read (`PID` zero). The
//     word may still be typed, because typing is what a person would do; a
//     signal may not, because there is nothing to aim it at.
//
// A refusal here never closes and never signals: the terminal is left exactly
// as it was found.
func ours(s session.Session, sight farewellSight) (session.Assistant, string) {
	if s.Assistant == "" {
		return "", "that session is not an assistant"
	}
	if sight.Assistant == "" {
		return "", "what is running in it now is not an assistant"
	}
	if sight.Assistant != s.Assistant {
		return "", fmt.Sprintf("a %s is running in it now, and this close was asked about a %s",
			sight.Assistant, s.Assistant)
	}
	if s.PID != 0 && sight.PID != 0 && sight.PID != s.PID {
		return "", fmt.Sprintf("%s (%d) is not the process this close was asked about (%d), so it is newer "+
			"than anything this reading knows", sight.Assistant, sight.PID, s.PID)
	}
	return sight.Assistant, ""
}

// say walks the ladder. Nil means the session was closed, or was already gone.
//
// Every refusal before the close leaves the terminal as it was, and says which
// rung it stopped at: an assistant that would not take its word, one that would
// not leave, a terminal whose contents could not be read, something else in
// front of it. A caller that can only say "it did not close" sends a person to
// the terminal to find out why, which is the round trip this daemon exists to
// remove.
func (f farewell) say(ctx context.Context, s session.Session) error {
	sight, err := f.look(ctx, s)
	if err != nil {
		return Unreadable{Why: "What is running in that terminal could not be read (" + err.Error() +
			"), so it was left as it was."}
	}
	if sight.Gone {
		return Unsent{Why: "That session is gone"}
	}
	// Nothing is in front of the terminal: the ordinary tab, and the fast path.
	// A close here asks nobody anything and lands in a fraction of a second.
	if !sight.Job {
		return f.close(ctx, s)
	}
	assistant, why := ours(s, sight)
	if why != "" {
		// Not this close's to end. The close is still asked for — taking a tab
		// away is what a person meant — but nothing is typed and nothing is
		// signalled, and whatever the terminal says about it is reported as it
		// is, including a question it put on the screen.
		return f.close(ctx, s)
	}
	line, ok := QuitLine(assistant)
	if !ok {
		return f.close(ctx, s)
	}
	sendErr := f.send(ctx, s, line)
	if sendErr != nil && !f.canSignal(sight) {
		return QuitRefused{Why: "The " + string(assistant) + " in that terminal would not take " + line +
			" (" + sendErr.Error() + "), and this machine has no way to ask the process itself, " +
			"so the terminal was left open."}
	}
	return f.waitAndClose(ctx, s, sight, assistant, line, sendErr)
}

// canSignal is whether there is a process this close may aim a signal at.
func (f farewell) canSignal(sight farewellSight) bool {
	return f.signal != nil && sight.Group != 0 && sight.PID != 0
}

// waitAndClose is the wait, the escalation and the close.
//
// The terminal is read again at every turn of the loop, and the identity is
// re-checked before each signal: once a rung has been walked, the next one
// belongs only to the same kernel process. A different pid is plainly
// different; the same pid with a different start instant is pid reuse and is
// just as different. Either way this stops, because a process that arrived
// after the word was typed never inherits the old one's history.
func (f farewell) waitAndClose(ctx context.Context, s session.Session, asked farewellSight,
	assistant session.Assistant, line string, sendErr error) error {
	started := f.now()
	termed, killed := false, false
	for {
		cur, err := f.look(ctx, s)
		if err != nil {
			return Unreadable{Why: "What is running in that terminal could not be read after " + line +
				" (" + err.Error() + "), so it was left open."}
		}
		if cur.Gone {
			// The session took itself away — a tmux pane whose shell was the
			// assistant, an iTerm2 tab closed by its own profile.
			return nil
		}
		if !cur.Job {
			// The foreground is back at its shell. This is the state the whole
			// ladder exists to reach, and the only one a close is asked for in.
			return f.close(ctx, s)
		}
		if _, why := ours(s, cur); why != "" {
			return Occupied{Why: "Something else came to the front of that terminal as the " +
				string(assistant) + " left (" + why + "), so it was left open."}
		}
		elapsed := f.now().Sub(started)
		switch {
		case elapsed < f.polite:
		case !termed:
			if err := f.escalate(cur, asked, escalateTerm, assistant, line, sendErr); err != nil {
				return err
			}
			termed, asked = true, cur
		case elapsed < f.polite+f.afterTerm:
		case !killed:
			if err := f.escalate(cur, asked, escalateKill, assistant, line, sendErr); err != nil {
				return err
			}
			killed, asked = true, cur
		case elapsed < f.polite+f.afterTerm+f.afterKill:
		default:
			return StillRunning{Why: "The " + string(assistant) + " in that terminal did not leave after " +
				line + " and after being signalled, so it was left open."}
		}
		if !pause(ctx, f.tick) {
			return StillRunning{Why: "Waiting for the " + string(assistant) +
				" to leave that terminal was cut short, so it was left open."}
		}
	}
}

// escalate sends one signal, once the process is still the one the previous
// rung was walked against.
func (f farewell) escalate(cur, previous farewellSight, step escalation,
	assistant session.Assistant, line string, sendErr error) error {
	if !f.canSignal(cur) {
		why := "The " + string(assistant) + " in that terminal did not leave after " + line
		if sendErr != nil {
			why = "The " + string(assistant) + " in that terminal would not take " + line +
				" (" + sendErr.Error() + ")"
		}
		return StillRunning{Why: why + ", and this machine cannot read which process it is, " +
			"so nothing was signalled and the terminal was left open."}
	}
	if previous.PID != 0 && (cur.PID != previous.PID || !cur.Start.Equal(previous.Start)) {
		return Occupied{Why: "The process in that terminal changed before " + step.String() +
			", so nothing was signalled and it was left open."}
	}
	// **The rung is in the log, because from outside a signal looks like the
	// word working.** A close that ended an assistant politely and one that
	// had to signal it both answer "closed", and only this says which — which
	// is what tells a reader whether the polite rung is doing anything at all.
	log.Printf("terminal: the %s in %s did not leave after %s; sending %s to its group (%d)",
		assistant, cur.terminalName(), line, step, cur.Group)
	if err := f.signal(cur.Group, step); err != nil {
		return StillRunning{Why: "The " + string(assistant) + " in that terminal could not be sent " +
			step.String() + " (" + err.Error() + "), so it was left open."}
	}
	return nil
}
