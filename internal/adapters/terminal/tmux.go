// Package terminal enumerates and drives terminal sessions. tmux is the one
// backend that is the same on every platform this product targets, which is why
// it is here rather than behind a build tag.
package terminal

import (
	"context"
	"os/exec"
	"strings"
	"time"

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

	cmd := exec.CommandContext(ctx, t.Binary, "list-panes", "-a", "-F", format)
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
