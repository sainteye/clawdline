package terminal

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// Reveal brings a pane to the front within tmux.
//
// Whether the terminal *window* comes forward is up to whichever emulator is
// drawing it, and not something to chase. There is one emulator where a piece
// of it can be chased and only one: under `tmux -CC` iTerm2 draws each tmux
// window as a tab of its own, and both halves of putting somebody in front of
// one — the tab and the application — have to be asked for separately, because
// neither follows from the `select-window` here. Ghostty, Terminal.app, Warp
// and the rest are driven exactly this far and no further: the pane is
// selected, and nothing is raised. That is what the Swift app does and it is
// what this does.
//
// **The selection is what was asked for; coming forward is a courtesy, and it
// leaves this call.** The tail below is up to four round trips with a deadline
// each, two of them Apple Events, and this route is on a path somebody is
// waiting on: a modal must not hold the answer. Nothing is lost by not waiting,
// because the tail's only outcome is discarded either way.
func (t *Tmux) Reveal(ctx context.Context, s session.Session, activate bool) error {
	if !IsPaneID(s.ID) {
		return errUnsupported("show a session that is not a tmux pane")
	}
	if err := t.run(ctx, "select-pane", "-t", s.ID); err != nil {
		return err
	}
	if err := t.run(ctx, "select-window", "-t", s.ID); err != nil {
		return err
	}
	go t.follow(s.ID, activate)
	return nil
}

// follow is the courtesy half, on its own goroutine and its own deadline.
//
// It is `revealFollow` on a platform that has an emulator to ask and nothing at
// all on one that does not, which is why it is behind a build tag rather than
// behind a runtime check: there is no iTerm2 to ask on Linux or Windows, and a
// no-op there is the honest answer rather than a failure.
func (t *Tmux) follow(paneID string, activate bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	revealFollow(ctx, t, paneID, activate)
}

// controlModeClient is one client tmux says is attached, as far as this cares.
type controlModeClient struct {
	tty     string
	session string
}

// controlModeClients is which clients are speaking control mode, or nothing.
//
// The flag list measured against tmux 3.6a from a live iTerm2 control-mode
// client is `attached,focused,control-mode,wait-exit,pause-after=0,UTF-8`. It
// is split on commas and compared whole rather than searched as a substring:
// `#{client_flags}` is an open vocabulary that tmux adds to between releases,
// and a future flag that merely contains these letters must not read as this
// one.
//
// A deadline of its own, well under the one every other tmux call gets: this is
// on the far side of somebody pressing a button, and the healthy cost measured
// on tmux 3.6a is `real 0.00`.
func (t *Tmux) controlModeClients(ctx context.Context) []controlModeClient {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	format := "#{client_tty}" + paneSeparator + "#{client_flags}" + paneSeparator + "#{client_session}"
	cmd := exec.CommandContext(ctx, t.Binary, "-u", "list-clients", "-F", format)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var rows []controlModeClient
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		f := splitPaneFields(line)
		if len(f) < 2 {
			continue
		}
		control := false
		for _, flag := range strings.Split(f[1], ",") {
			if strings.TrimSpace(flag) == "control-mode" {
				control = true
				break
			}
		}
		if !control {
			continue
		}
		row := controlModeClient{tty: strings.TrimSpace(f[0])}
		if len(f) > 2 {
			row.session = f[2]
		}
		rows = append(rows, row)
	}
	return rows
}

// sessionName is which tmux session a pane belongs to, or "" when tmux would
// not say. One `display-message` rather than a second `list-panes -a`: the
// answer wanted is one field about one pane.
func (t *Tmux) sessionName(ctx context.Context, paneID string) string {
	if !IsPaneID(paneID) {
		return ""
	}
	cmd := exec.CommandContext(ctx, t.Binary, "-u", "display-message", "-p", "-t", paneID, "#{session_name}")
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// shouldActivateITerm is whether selecting this pane should also bring iTerm2
// to the front.
//
// **Both facts come from tmux**, which is what makes it answerable at all. A
// control-mode client is attached to one tmux session and a pane belongs to
// one, so a pane in the session iTerm2 is drawing gets the application brought
// forward, and a pane in a session Ghostty or Terminal.app is attached to does
// not — even while an iTerm2 `-CC` client exists somewhere else on the same
// server.
//
// An unknown pane session is a no. Bringing the wrong application forward takes
// somebody's keyboard away from what they were typing into, which is the
// failure this whole app exists not to commit.
func shouldActivateITerm(paneSession string, clients []controlModeClient) bool {
	if paneSession == "" {
		return false
	}
	for _, c := range clients {
		if c.session == paneSession {
			return true
		}
	}
	return false
}
