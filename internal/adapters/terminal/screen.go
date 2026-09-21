package terminal

import (
	"context"
	"os/exec"
	"strconv"
	"strings"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// IsPaneID is whether this is a pane id tmux handed out, and therefore a word
// that may be written into a command target or a shell line.
//
// Closed on purpose. Everything asked about here came back out of `list-panes`
// seconds earlier, so there is no shape to be generous about, and both callers
// — `-t` on a pipe-pane and the FIFO name that is this daemon's ownership
// record — are places where a stray argument would be read as something else.
func IsPaneID(id string) bool {
	if len(id) < 2 || id[0] != '%' {
		return false
	}
	for _, r := range id[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Screen is the visible screen and what scrolled off the top of it.
//
// `-e` keeps the escape sequences, which is the only way any of this arrives
// with colour, and it is the difference between this and Capture: the reading
// the classifiers use has its colour stripped again on the way in, and the
// panel is the one caller that draws it. `-J` joins what the pane wrapped.
//
// On an assistant pane the scrollback argument is inert — a live Claude Code
// pane measures `alternate_on=1, history_size=0`, so `-S -200` returns the
// visible rows and nothing above them — and on an ordinary shell pane it is
// two hundred lines of history. Nothing here promises which.
func (t *Tmux) Screen(ctx context.Context, s session.Session, lines int) (string, bool) {
	if s.Backend != session.BackendTmux || !IsPaneID(s.ID) {
		return "", false
	}
	if lines < 0 {
		lines = 0
	}
	cmd := exec.CommandContext(ctx, t.Binary, "capture-pane", "-p", "-e", "-J",
		"-S", "-"+strconv.Itoa(lines), "-t", s.ID)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// Pipe attaches a command to everything a pane writes.
//
// **This is machine state that outlives the process that asked for it.** tmux
// keeps the pipe until somebody takes it away, the pane dies, or the command on
// the far end does — so every caller owes a matching Unpipe, and the FIFO the
// signal writes into is what makes the third of those a safety net rather than
// a leak: close the reading end and the far end dies on the pane's first write.
func (t *Tmux) Pipe(ctx context.Context, paneID, command string) bool {
	if !IsPaneID(paneID) || command == "" {
		return false
	}
	return t.run(ctx, "pipe-pane", "-t", paneID, command) == nil
}

// Unpipe takes the pipe off that pane. `pipe-pane` with no command is tmux's
// own way of saying it, and it is idempotent: a pane with nothing attached
// answers ok.
func (t *Tmux) Unpipe(ctx context.Context, paneID string) bool {
	if !IsPaneID(paneID) {
		return false
	}
	return t.run(ctx, "pipe-pane", "-t", paneID) == nil
}

// PipedPanes is which panes tmux says have a pipe attached, keyed by pane id.
//
// One invocation for the whole server. A pane missing from the answer is a pane
// tmux does not have, which is not the same as a pane with no pipe — so the
// caller gets a map and decides, rather than a set that has already collapsed
// the two. A line whose second field is neither `0` nor `1` is dropped rather
// than guessed at: somebody acts on this, and "unreadable" must not arrive
// spelled the same way as "not piped".
func (t *Tmux) PipedPanes(ctx context.Context) map[string]bool {
	cmd := exec.CommandContext(ctx, t.Binary, "-u", "list-panes", "-a", "-F",
		"#{pane_id}"+paneSeparator+"#{pane_pipe}")
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return map[string]bool{}
	}
	state := map[string]bool{}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		fields := splitPaneFields(line)
		if len(fields) != 2 || !IsPaneID(fields[0]) {
			continue
		}
		switch strings.TrimSpace(fields[1]) {
		case "1":
			state[fields[0]] = true
		case "0":
			state[fields[0]] = false
		}
	}
	return state
}

var (
	_ ports.PipeHost = (*Tmux)(nil)
)
