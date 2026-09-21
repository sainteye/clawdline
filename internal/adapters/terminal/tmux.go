// Package terminal enumerates and drives terminal sessions. tmux is the one
// backend that is the same on every platform this product targets, which is why
// it is here rather than behind a build tag.
package terminal

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

const paneSeparator = "\x01"

// paneSeparatorEscaped is what tmux 3.4 writes where 3.6a writes the byte
// itself: format output goes through vis(3), which renders a control
// character as its four-character octal escape. `-u` below stops 3.6a
// rewriting the byte, but it does not reach this — 3.4 escapes after it has
// expanded the format, whatever the client claims about UTF-8.
const paneSeparatorEscaped = `\001`

// splitPaneFields cuts a format line on whichever spelling of the separator
// this build of tmux used.
//
// A build that escapes the separator is not a build with no panes. Read
// literally, every line loses its fields at once, so Inventory would have
// answered "there are no panes" carrying the full authority of a listing that
// succeeded — the one answer D05 ③ forbids it to arrive at by guessing. Found
// on Ubuntu 24.04 (tmux 3.4) on 2026-09-20; this Mac runs 3.6a and never saw
// it.
func splitPaneFields(line string) []string {
	if strings.Contains(line, paneSeparator) {
		return strings.Split(line, paneSeparator)
	}
	return strings.Split(line, paneSeparatorEscaped)
}

// sendConfirm is how long Send waits for the pane to show the text before it
// gives up without pressing Enter.
var sendConfirm = 6 * time.Second

type Tmux struct {
	Binary string
	// fallbacks is nil in production, where tmuxFallbacks is used. Tests set
	// an explicit list so the Finder-style PATH case does not depend on what
	// package manager is installed on the machine running them.
	fallbacks []string
}

func NewTmux() *Tmux { return &Tmux{Binary: "tmux"} }

func (t *Tmux) Name() string { return "tmux" }

// Inventory lists every pane on this machine's tmux server.
//
// "No server" is a complete empty inventory. Any other failure — a timeout
// above all — has no authority to prove a pane absent, so it sets Complete to
// false and says why. The two are the same list and two different amounts of
// evidence.
//
// "No server" is read from what tmux says, not from its exit status: tmux
// exits 1 for nearly every failure, and a socket directory with the wrong
// permissions ("has unsafe permissions") exits 1 exactly as an absent server
// does. Reading every exit 1 as "no server" made a listing that failed an
// authoritative "there are no panes" — the one answer that lets the broker
// call a live child's tab gone (docs/design-decisions.md D05 ③).
func (t *Tmux) Inventory(ctx context.Context) (session.Inventory, error) {
	inv := session.Inventory{
		ObservedAt: time.Now(),
		Provenance: "tmux",
		Complete:   true,
	}
	found, onPath := t.binary()
	if found == "" {
		inv.Notes = append(inv.Notes, "tmux is not installed")
		return inv, nil
	}
	if !onPath {
		// A package-manager tmux outside this process's PATH may have a live
		// server, but this backend cannot ask it consistently: Capture, Send
		// and Close still run t.Binary. This is therefore an unread source,
		// never authoritative evidence that the server has no panes.
		inv.Complete = false
		inv.Notes = append(inv.Notes, tmuxOutsidePATHReason(found))
		return inv, nil
	}

	format := strings.Join([]string{
		"#{pane_id}", "#{pane_tty}", "#{pane_current_command}",
		"#{session_name}", "#{pane_title}", "#{pane_current_path}",
	}, paneSeparator)

	// `-u`: under LC_ALL=C tmux treats the client as non-UTF-8 and rewrites
	// every control character in format output — the \x01 separator above
	// included — as `_`, so no line had six fields and every pane was
	// dropped (tmux 3.6a). `-u` declares the client UTF-8 and leaves the
	// separator alone; the locale stays C for everything else.
	cmd := exec.CommandContext(ctx, t.Binary, "-u", "list-panes", "-a", "-F", format)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		said := strings.TrimSpace(stderr.String())
		if strings.Contains(err.Error(), "exit status 1") && NoServer(said) {
			// No server running: an authoritative empty answer.
			return inv, nil
		}
		inv.Complete = false
		why := err.Error()
		if said != "" {
			why = said
		}
		inv.Notes = append(inv.Notes, "tmux list-panes failed: "+why)
		return inv, nil
	}

	dropped := 0
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := splitPaneFields(line)
		if len(parts) < 6 {
			dropped++
			continue
		}
		s := session.Session{
			ID:       parts[0],
			Backend:  session.BackendTmux,
			TTY:      strings.TrimPrefix(parts[1], "/dev/"),
			Label:    parts[4],
			CWD:      parts[5],
			State:    session.StateUnknown,
			Evidence: session.EvidenceProcess,
		}
		switch parts[2] {
		case "claude":
			s.Assistant = session.AssistantClaude
		case "codex":
			s.Assistant = session.AssistantCodex
		}
		inv.Sessions = append(inv.Sessions, s)
	}
	if dropped > 0 {
		// Whatever those lines were, they were panes. Handing back the ones
		// that parsed, as a complete listing, is how a pane that is on screen
		// becomes a pane the broker is entitled to call gone.
		inv.Complete = false
		inv.Notes = append(inv.Notes,
			fmt.Sprintf("tmux list-panes: %d line(s) did not carry six fields", dropped))
	}
	return inv, nil
}

// NoServer is whether tmux's own sentence says there is no server to ask —
// the one failure that is an answer. tmux 3.6a says "no server running on
// <socket>" when the socket is there and nobody listens, and "error connecting
// to <socket> (No such file or directory)" when there is no socket at all.
// Anything else it says is a failure to read.
func NoServer(said string) bool {
	return strings.Contains(said, "no server running") ||
		(strings.Contains(said, "error connecting to") && strings.Contains(said, "No such file or directory"))
}

// Capture returns what is currently drawn in a pane. It is read-only: nothing
// is typed, and the pane is not brought forward.
func (t *Tmux) Capture(ctx context.Context, s session.Session) (string, bool) {
	if s.Backend != session.BackendTmux || s.ID == "" {
		return "", false
	}
	// `-J` joins what the pane wrapped, as the Swift app's reading does: a
	// menu option too long for the pane is one row, not a row and a
	// description.
	cmd := exec.CommandContext(ctx, t.Binary, "capture-pane", "-p", "-J", "-t", s.ID)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// Send types one line into a pane and submits it, by the four steps in
// submit.go: one bracketed paste, a look at the pane until it shows the text
// arriving, then Enter — never before — and another Enter only while the
// input line still shows exactly what it showed.
//
// It used to be `send-keys -l` and then Enter at once. The two are one byte
// stream to the program, which reads it in pieces of at most 1022 bytes, and a
// 1045-byte notice reached a Claude Code root as its last 23 bytes.
func (t *Tmux) Send(ctx context.Context, s session.Session, text string) error {
	return submit(ctx, tmuxInput{call: t.call, target: s.ID}, text, sendConfirm)
}

// Interrupt stops the current turn without closing the pane.
func (t *Tmux) Interrupt(ctx context.Context, s session.Session) error {
	return t.run(ctx, "send-keys", "-t", s.ID, "C-c")
}

// Close removes the pane, once nothing this close may not end is running in
// it.
//
// **It walks the same farewell ladder the iTerm2 close does** (farewell.go),
// for the same reason and one of its own. `kill-pane` puts up no sheet — that
// half is iTerm2's — but it does hang up the tty under whatever was running,
// and an assistant killed mid-sentence loses the tail of the transcript it was
// writing. So the assistant is sent its own quit word first, watched until it
// has left, and the pane is taken after.
func (t *Tmux) Close(ctx context.Context, s session.Session) error {
	if s.ID == "" {
		return Unsent{Why: "there is no tmux pane to close"}
	}
	return t.farewell().say(ctx, s)
}

// farewell is Close's ladder on this backend's own steps.
func (t *Tmux) farewell() farewell {
	return farewell{
		look:   t.sight,
		send:   func(ctx context.Context, s session.Session, line string) error { return t.Send(ctx, s, line) },
		signal: ttySignal,
		close:  func(ctx context.Context, s session.Session) error { return t.run(ctx, "kill-pane", "-t", s.ID) },
		polite: farewellPolite, afterTerm: farewellAfterTerm, afterKill: farewellAfterKill,
		tick: farewellTick, now: time.Now,
	}
}

// sight is one look into a pane: tmux for whether it is still there and which
// tty it is, and then the kernel for what is in front of that tty.
//
// **tmux's own answer is the fallback and not the first evidence.**
// `#{pane_current_command}` names what is in front without saying which
// process it is, which is enough to see an assistant and to see it leave, and
// not enough to aim a signal at. Where the processes behind the tty can be
// read they are, and the ladder gets its escalation rung; where they cannot —
// every platform but macOS today (tty_other.go) — it stops at the polite word
// and says so rather than signalling something it cannot name.
//
// The pane id tmux answers with is checked against the one asked for: tmux
// matches a target by prefix when it is not exact, and a pane that answers
// under another id is not the pane this close was about.
func (t *Tmux) sight(ctx context.Context, s session.Session) (farewellSight, error) {
	// A space, not a tab: under LC_ALL=C tmux rewrites control characters in
	// format output, and neither a pane id nor a tty contains a space.
	out, err := t.call(ctx, "", "display-message", "-p", "-t", s.ID,
		"#{pane_id} #{pane_tty} #{pane_current_command}")
	if err != nil {
		// "can't find pane" and "no server" are the pane being gone, which
		// leaves nothing of ours to close. Any other failure has proved
		// nothing about what is in that pane.
		if strings.Contains(err.Error(), "can't find") || NoServer(err.Error()) {
			return farewellSight{Gone: true}, nil
		}
		return farewellSight{}, err
	}
	fields := strings.Fields(strings.TrimRight(out, "\n"))
	if len(fields) < 3 || fields[0] != s.ID {
		return farewellSight{Gone: true}, nil
	}
	if sight, err := ttySight(fields[1], s); err == nil {
		return sight, nil
	}
	assistant := assistantOfComm(fields[2])
	return farewellSight{Job: assistant != "", Assistant: assistant}, nil
}

// run carries tmux's own sentence out with the failure.
//
// `exit status 1` tells a reader nothing they can act on, and the difference
// between "that pane does not exist" and "no server is running" is the whole
// question when an effect did not land. tmux already says which; this stops
// throwing that away.
func (t *Tmux) run(ctx context.Context, args ...string) error {
	_, err := t.call(ctx, "", args...)
	return err
}

// call is run with stdin, answering what tmux wrote.
func (t *Tmux) call(ctx context.Context, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, t.Binary, args...)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		said := strings.TrimSpace(stderr.String())
		if said == "" {
			return "", fmt.Errorf("tmux %s failed: %w", args[0], err)
		}
		return "", fmt.Errorf("tmux %s refused: %s", args[0], said)
	}
	return string(out), nil
}

// Open starts a detached session and returns its first pane.
//
// Detached because a dispatched task is not something to put in front of
// whoever happens to be at the keyboard. The pane id is read back from tmux
// rather than derived, so the identity in the record is the one tmux will
// answer to later.
func (t *Tmux) Open(ctx context.Context, req ports.OpenRequest) (session.Session, error) {
	args := []string{"new-session", "-d", "-P", "-F", "#{pane_id}"}
	if req.Name != "" {
		args = append(args, "-s", req.Name)
	}
	if req.Cwd != "" {
		args = append(args, "-c", req.Cwd)
	}
	for k, v := range req.Env {
		args = append(args, "-e", k+"="+v)
	}
	if req.Command != "" {
		args = append(args, req.Command)
	}
	cmd := exec.CommandContext(ctx, t.Binary, args...)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		said := strings.TrimSpace(stderr.String())
		if said == "" {
			said = err.Error()
		}
		return session.Session{}, fmt.Errorf("tmux new-session refused: %s", said)
	}
	return session.Session{
		ID:       strings.TrimSpace(string(out)),
		Backend:  session.BackendTmux,
		CWD:      req.Cwd,
		State:    session.StateUnknown,
		Evidence: session.EvidenceProcess,
	}, nil
}
