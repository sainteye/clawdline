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
// drawing it. There is one this daemon can ask — iTerm2 — and two shapes of it
// to ask about, because tmux can be drawn two ways. Under `tmux -CC` iTerm2
// draws each tmux window as a tab of its own and names it by the pane it is
// mirroring; under an ordinary `tmux attach` the whole server is one pty in one
// tab, and what names that tab is the tty tmux says its client is on. Both
// halves of putting somebody in front of one — the tab and the application —
// have to be asked for separately either way, because neither follows from the
// `select-window` here. Ghostty, Terminal.app, Warp and the rest are driven
// exactly this far and no further: the pane is selected, and nothing is raised.
//
// **The selection is what was asked for; coming forward is a courtesy, and it
// leaves this call.** The tail below is up to four round trips with a deadline
// each, two of them Apple Events, and this route is on a path somebody is
// waiting on: a modal must not hold the answer. Nothing is lost by not waiting,
// because the tail's only outcome is discarded either way.
//
// **What does not leave this call is whether anybody can see the answer.** A
// tmux session with no client attached is on no screen anywhere, and selecting
// a pane inside it puts nothing in front of a person. Saying `ok` to that sends
// somebody to look for a window that does not exist, so it is answered here,
// before the reply goes out, and it is answered from tmux's own client list
// rather than guessed.
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
	// Unknown never refuses: a client list tmux would not give, or a pane whose
	// session it would not name, is no evidence that nobody is attached, and
	// the selection above did happen.
	if name := t.sessionName(ctx, s.ID); name != "" {
		if clients, known := t.attachedClients(ctx); known && len(clientsWatching(name, clients)) == 0 {
			return Unwatched{Session: name}
		}
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

// attachedClient is one client tmux says is attached, as far as this cares.
//
// `control` is whether it speaks control mode, which decides which of the two
// shapes of drawn tmux this is — not whether it counts as somebody watching.
// An ordinary `tmux attach` in an iTerm2 tab is a person looking at a screen
// just as much as a `-CC` client is, and reading only the control-mode ones as
// clients is what made this whole tail do nothing on the commonest setup there
// is.
type attachedClient struct {
	tty     string
	session string
	control bool
}

// attachedClients is every client tmux says is attached, and whether tmux
// answered at all.
//
// **The second return is the difference between "nobody" and "no answer".** A
// caller refusing on an empty list must not refuse on a list it never got: a
// tmux that timed out has no authority to prove a session unwatched.
//
// The control-mode flag list measured against tmux 3.6a from a live iTerm2
// control-mode client is `attached,focused,control-mode,wait-exit,pause-after=0,UTF-8`,
// and from an ordinary attach in an iTerm2 tab it is `attached,UTF-8`. It is
// split on commas and compared whole rather than searched as a substring:
// `#{client_flags}` is an open vocabulary that tmux adds to between releases,
// and a future flag that merely contains these letters must not read as this
// one.
//
// A deadline of its own, well under the one every other tmux call gets: this is
// on the far side of somebody pressing a button, and the healthy cost measured
// on tmux 3.6a is `real 0.00`.
func (t *Tmux) attachedClients(ctx context.Context) ([]attachedClient, bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	format := "#{client_tty}" + paneSeparator + "#{client_flags}" + paneSeparator + "#{client_session}"
	cmd := exec.CommandContext(ctx, t.Binary, "-u", "list-clients", "-F", format)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	var rows []attachedClient
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := splitPaneFields(line)
		if len(f) < 2 {
			continue
		}
		row := attachedClient{tty: strings.TrimSpace(f[0])}
		for _, flag := range strings.Split(f[1], ",") {
			if strings.TrimSpace(flag) == "control-mode" {
				row.control = true
				break
			}
		}
		if len(f) > 2 {
			row.session = f[2]
		}
		rows = append(rows, row)
	}
	return rows, true
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

// clientsWatching is the clients attached to one tmux session: who, if
// anybody, has this pane's session on a screen.
//
// **Both facts come from tmux**, which is what makes it answerable at all. A
// client is attached to one tmux session and a pane belongs to one, so a pane
// in the session iTerm2 is drawing is followed there, and a pane in a session
// Ghostty or Terminal.app is attached to is not — even while an iTerm2 client
// exists somewhere else on the same server.
//
// An unknown pane session is nobody. Bringing the wrong application forward
// takes somebody's keyboard away from what they were typing into, which is the
// failure this whole app exists not to commit, and an empty session name
// matching every client is the shape that commits it.
func clientsWatching(paneSession string, clients []attachedClient) []attachedClient {
	if paneSession == "" {
		return nil
	}
	out := make([]attachedClient, 0, len(clients))
	for _, c := range clients {
		if c.session == paneSession {
			out = append(out, c)
		}
	}
	return out
}

// controlModeOnly keeps the clients speaking control mode, which are the ones
// iTerm2 mirrors window by window.
func controlModeOnly(clients []attachedClient) []attachedClient {
	out := make([]attachedClient, 0, len(clients))
	for _, c := range clients {
		if c.control {
			out = append(out, c)
		}
	}
	return out
}
