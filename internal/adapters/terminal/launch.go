package terminal

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/app/ports"
)

// Failure is TerminalFailure: an opening that did not happen, with the
// terminal's own words. Attention is iTerm2 refusing because something on its
// screen — a modal, a permission prompt — wants a person first.
type Failure struct {
	Attention bool
	Message   string
}

func (f Failure) Error() string { return f.Message }

// Launcher is the start route's port over this machine's terminals: tmux on
// every platform, iTerm2 where there is one (launch_darwin.go).
type Launcher struct{ Tmux *Tmux }

var _ ports.Launcher = Launcher{}

func NewLauncher() Launcher { return Launcher{Tmux: NewTmux()} }

// binary is Tmux.binary: an app has no login PATH, so the places package
// managers put tmux are tried after the PATH this process has.
func (l Launcher) binary() string {
	if found, err := exec.LookPath(l.Tmux.Binary); err == nil {
		return found
	}
	for _, p := range []string{"/opt/homebrew/bin/tmux", "/usr/local/bin/tmux", "/usr/bin/tmux", "/opt/local/bin/tmux"} {
		if found, err := exec.LookPath(p); err == nil {
			return found
		}
	}
	return ""
}

// TmuxReach is StartPoints.tmuxReach. A listing that failed is read as a
// server, not as an absent one: only "no server" is permission to start one.
func (l Launcher) TmuxReach(ctx context.Context) int {
	bin := l.binary()
	if bin == "" {
		return 0
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "list-panes", "-a", "-F", "#{pane_id}")
	cmd.Env = append(outsideTmux(cmd.Environ()), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		if strings.TrimSpace(string(out)) != "" {
			return 2
		}
		return 1
	}
	if NoServer(stderr.String()) {
		return 1
	}
	return 2
}

// NewTmuxWindow is Tmux.newWindowResult.
func (l Launcher) NewTmuxWindow(ctx context.Context, cwd, command string) (string, error) {
	return l.openPane(ctx, []string{"new-window", "-P", "-F", "#{pane_id}"}, cwd, command,
		"tmux would not open a window.")
}

// NewTmuxSession is Tmux.newSessionResult.
func (l Launcher) NewTmuxSession(ctx context.Context, cwd, name, command string) (string, error) {
	return l.openPane(ctx, []string{"new-session", "-d", "-s", name, "-P", "-F", "#{pane_id}"}, cwd, command,
		"tmux would not start a server.")
}

// openPane is Tmux.openPane: make the pane with no command, so tmux gives it
// an interactive login shell that reads the person's rc files and finds the
// assistant, then type the line at that shell. A pane started as
// `new-window <command>` runs under the server's environment instead, which
// for a server an app started has no PATH worth reading.
//
// What comes back says the keystrokes were accepted, which is the most tmux
// can say; a shell that flushes pending input shows up afterwards as a pane
// that never reports an assistant.
func (l Launcher) openPane(ctx context.Context, create []string, cwd, command, refused string) (string, error) {
	bin := l.binary()
	if bin == "" {
		return "", Failure{Message: "tmux is not installed."}
	}
	args := append([]string{}, create...)
	if cwd != "" {
		args = append(args, "-c", cwd)
	}
	out, err := runTmux(ctx, bin, args...)
	if err != nil {
		if err.Error() == "" {
			return "", Failure{Message: refused}
		}
		return "", Failure{Message: err.Error()}
	}
	id := strings.TrimSpace(out)
	if !strings.HasPrefix(id, "%") {
		return "", Failure{Message: "tmux returned no new pane id."}
	}
	// A pane this call made and could not start anything in is closed here,
	// by the id tmux just gave it: nobody else holds that id, and a shell left
	// open in a pane nobody recorded is the next spawn's failure (the Swift
	// app's `3e37e8ec`). A pane this call did not make is never touched.
	if _, err := runTmux(ctx, bin, "send-keys", "-t", id, "-l", command); err != nil {
		l.discard(ctx, bin, id)
		return "", Failure{Message: "tmux would not type into the pane it just made."}
	}
	if _, err := runTmux(ctx, bin, "send-keys", "-t", id, "Enter"); err != nil {
		l.discard(ctx, bin, id)
		return "", Failure{Message: "tmux typed the line but Enter did not land."}
	}
	return id, nil
}

// discard kills a pane openPane made and did not hand out. Its failure is
// only logged: the caller is already reporting the failure that led here.
func (l Launcher) discard(ctx context.Context, bin, paneID string) {
	if _, err := runTmux(context.WithoutCancel(ctx), bin, "kill-pane", "-t", paneID); err != nil {
		log.Printf("tmux: the pane %s this spawn made could not be closed: %v", paneID, err)
	}
}

// CloseTmuxSession closes the session called name, and only when paneID is
// one of its panes — the proof that it is the session this daemon opened for
// that pane (docs/design-decisions.md D11). It answers whether it closed
// anything; a pane that is gone, or that now belongs to a session of another
// name, closes nothing and is not an error.
//
// The session is closed by the id tmux reports for that pane, never by name:
// a name is matched by prefix when it is not exact, and the id cannot be.
func (l Launcher) CloseTmuxSession(ctx context.Context, paneID, name string) (bool, error) {
	if !strings.HasPrefix(paneID, "%") || name == "" {
		return false, nil
	}
	bin := l.binary()
	if bin == "" {
		return false, nil
	}
	out, err := runTmux(ctx, bin, "display-message", "-p", "-t", paneID, "#{session_id} #{session_name}")
	if err != nil {
		// "can't find pane" is the pane being gone, which leaves nothing of
		// ours to close; any other failure is reported, and nothing is closed.
		if strings.Contains(err.Error(), "can't find") || NoServer(err.Error()) {
			return false, nil
		}
		return false, err
	}
	// A space, not a tab: under LC_ALL=C tmux rewrites control characters in
	// format output, and a session id never contains a space.
	parts := strings.SplitN(strings.TrimRight(out, "\n"), " ", 2)
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "$") || parts[1] != name {
		return false, nil
	}
	if _, err := runTmux(ctx, bin, "kill-session", "-t", parts[0]); err != nil {
		return false, err
	}
	return true, nil
}

// runTmux is one bounded tmux call, carrying tmux's own sentence on failure.
func runTmux(ctx context.Context, bin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(outsideTmux(cmd.Environ()), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		said := strings.TrimSpace(stderr.String())
		if said == "" {
			return "", fmt.Errorf("tmux %s failed: %v", args[0], err)
		}
		return "", fmt.Errorf("%s", said)
	}
	return string(out), nil
}

// outsideTmux drops what a daemon started inside a tmux pane inherited from
// it. With TMUX set, a bare `new-window` goes to the launching pane's session;
// the Swift app, which no pane launched, gets the session tmux last used, and
// that is where a new session belongs.
func outsideTmux(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_PANE=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
