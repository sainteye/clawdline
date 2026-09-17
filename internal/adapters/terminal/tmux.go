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

	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

const paneSeparator = "\x01"

type Tmux struct{ Binary string }

func NewTmux() *Tmux { return &Tmux{Binary: "tmux"} }

func (t *Tmux) Name() string { return "tmux" }

// Inventory lists every pane on this machine's tmux server.
//
// "No server" is a complete empty inventory. Any other failure — a timeout
// above all — has no authority to prove a pane absent, so it sets Complete to
// false and says why. The two are the same list and two different amounts of
// evidence.
func (t *Tmux) Inventory(ctx context.Context) (session.Inventory, error) {
	inv := session.Inventory{
		ObservedAt: time.Now(),
		Provenance: "tmux",
		Complete:   true,
	}
	if _, err := exec.LookPath(t.Binary); err != nil {
		inv.Notes = append(inv.Notes, "tmux is not installed")
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
	out, err := cmd.Output()
	if err != nil {
		if strings.Contains(err.Error(), "exit status 1") {
			// No server running: an authoritative empty answer.
			return inv, nil
		}
		inv.Complete = false
		inv.Notes = append(inv.Notes, "tmux list-panes failed: "+err.Error())
		return inv, nil
	}

	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		parts := strings.Split(line, paneSeparator)
		if len(parts) < 6 {
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
	return inv, nil
}

// Capture returns what is currently drawn in a pane. It is read-only: nothing
// is typed, and the pane is not brought forward.
func (t *Tmux) Capture(ctx context.Context, s session.Session) (string, bool) {
	if s.Backend != session.BackendTmux || s.ID == "" {
		return "", false
	}
	cmd := exec.CommandContext(ctx, t.Binary, "capture-pane", "-p", "-t", s.ID)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// Send types one line into a pane and submits it.
//
// The text goes in literally (`-l`) so that a line containing a semicolon, a
// brace or a newline cannot be read by tmux as its own command syntax. Submit
// is a separate key rather than a trailing newline in the same call, because
// the two are different events to the program reading the tty.
func (t *Tmux) Send(ctx context.Context, s session.Session, text string) error {
	if err := t.run(ctx, "send-keys", "-t", s.ID, "-l", text); err != nil {
		return err
	}
	return t.run(ctx, "send-keys", "-t", s.ID, "Enter")
}

// Interrupt stops the current turn without closing the pane.
func (t *Tmux) Interrupt(ctx context.Context, s session.Session) error {
	return t.run(ctx, "send-keys", "-t", s.ID, "C-c")
}

// Close removes the pane.
func (t *Tmux) Close(ctx context.Context, s session.Session) error {
	return t.run(ctx, "kill-pane", "-t", s.ID)
}

// run carries tmux's own sentence out with the failure.
//
// `exit status 1` tells a reader nothing they can act on, and the difference
// between "that pane does not exist" and "no server is running" is the whole
// question when an effect did not land. tmux already says which; this stops
// throwing that away.
func (t *Tmux) run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, t.Binary, args...)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		said := strings.TrimSpace(stderr.String())
		if said == "" {
			return fmt.Errorf("tmux %s failed: %w", args[0], err)
		}
		return fmt.Errorf("tmux %s refused: %s", args[0], said)
	}
	return nil
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
