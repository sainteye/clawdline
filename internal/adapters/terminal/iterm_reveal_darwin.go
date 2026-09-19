//go:build darwin

package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// itermRevealScript is iterm.js's `reveal`, `revealtmux`, `activate` and the
// tty half of `list`. The ids are arguments, never part of the script text.
//
// **`revealtmux` asks a `variable`, not a property.** iTerm2's session class
// carries no tmux property, which is where the old reasoning stopped; the same
// scripting dictionary has a `variable` command and the tmux facts live there.
// Measured against iTerm2 and tmux 3.6a over eight mirrored windows:
// `session.tmuxRole` is `client` on a mirrored window, `gateway` on the row
// `tmux -CC` was typed into and nothing at all on an ordinary session, and
// `session.tmuxWindowPane` is the tmux pane id without its `%`. The name says
// window and the value is a pane: splitting a tmux window settled it — window
// `@85` with panes `%85` and `%86` came back as one iTerm2 tab holding two
// sessions reporting `85` and `86`.
//
// **The role is checked as well as the pane, and that is a lock rather than a
// nicety.** Selecting the wrong tab takes somebody's keyboard away from what
// they were typing into just as surely as raising the wrong application does.
const itermRevealScript = itermEach + `
function run(argv) {
  const cmd = String(argv[0] || "");
  const it = Application("iTerm2");
  if (!it.running()) return JSON.stringify({ ok: false, error: "iTerm2 is not running" });
  if (cmd === "activate") {
    try { it.activate(); } catch (e) {
      return JSON.stringify({ ok: false, error: "Could not bring iTerm2 forward: " + e.message });
    }
    return JSON.stringify({ ok: true });
  }
  if (cmd === "list") {
    const out = [];
    itermEach(it, function (s) {
      let tty = "";
      try { tty = String(s.tty() || ""); } catch (e) {}
      if (tty) out.push(tty);
    });
    return JSON.stringify({ ok: true, ttys: out });
  }
  const activate = String(argv[2] === undefined ? "1" : argv[2]) === "1";
  function variableOf(s, name) {
    try {
      const v = s.variable({ named: name });
      return v === null || v === undefined ? "" : String(v);
    } catch (e) { return ""; }
  }
  const want = String(argv[1] || "");
  if (cmd !== "reveal" && cmd !== "revealtmux") {
    return JSON.stringify({ ok: false, error: "unknown command" });
  }
  const hit = itermEach(it, function (s, win, tab) {
    if (cmd === "reveal") {
      if (String(s.id()) !== want) return false;
    } else {
      if (variableOf(s, "session.tmuxWindowPane") !== want) return false;
      if (variableOf(s, "session.tmuxRole") !== "client") return false;
    }
    try { win.select(); } catch (e) {}
    try { tab.select(); } catch (e) {}
    try { s.select(); } catch (e) {}
    return true;
  }).stopped;
  if (!hit) {
    return JSON.stringify({ ok: false, error: cmd === "reveal"
      ? "That session is gone"
      : "iTerm2 is drawing no tmux window for pane %" + want });
  }
  if (activate) { try { it.activate(); } catch (e) {} }
  return JSON.stringify({ ok: true });
}
`

func itermReveal(ctx context.Context, args ...string) (map[string]any, error) {
	defer effect()()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript",
		append([]string{"-l", "JavaScript", "-"}, args...)...)
	cmd.Stdin = strings.NewReader(itermRevealScript)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		said := strings.TrimSpace(stderr.String())
		if said == "" {
			said = "iTerm2 did not answer: " + err.Error()
		}
		return nil, Failure{
			Attention: strings.Contains(said, "-1712") || strings.Contains(said, "-1743"),
			Message:   said,
		}
	}
	var answer map[string]any
	if json.Unmarshal(bytes.TrimSpace(out), &answer) != nil {
		return nil, Failure{Message: "iTerm2 answered something that is not JSON."}
	}
	if ok, _ := answer["ok"].(bool); !ok {
		said, _ := answer["error"].(string)
		if said == "" {
			said = "iTerm2 refused."
		}
		return nil, Failure{Message: said}
	}
	return answer, nil
}

// Reveal selects a session's window and tab.
//
// `activate: false` stops short of bringing iTerm2 forward, which is what a
// walk through a list wants: the tab underneath follows the target being
// pointed at and the keyboard stays in the box being typed into.
func (i *ITerm) Reveal(ctx context.Context, s session.Session, activate bool) error {
	if s.Backend != session.BackendITerm || s.ID == "" {
		return errUnsupported("show that session on this Mac")
	}
	flag := "0"
	if activate {
		flag = "1"
	}
	_, err := itermReveal(ctx, "reveal", s.ID, flag)
	return err
}

// Screen is the visible screen. iTerm2's scripting interface exposes no
// scrollback at all, so the line ceiling is inert here and this is Capture:
// there is no more to ask for, and the payload says how many rows came back
// rather than how many were wanted.
func (i *ITerm) Screen(ctx context.Context, s session.Session, lines int) (string, bool) {
	return i.Capture(ctx, s)
}

// itermRevealTmuxPane selects the iTerm2 tab drawing one tmux pane under
// `tmux -CC`. The argument is tmux's `#{pane_id}` with its `%` stripped,
// because the mapping is asked of iTerm2 rather than matched on a tty: a
// mirrored tmux window comes back from `list` with no tty at all.
func itermRevealTmuxPane(ctx context.Context, paneNumber string, activate bool) error {
	flag := "0"
	if activate {
		flag = "1"
	}
	_, err := itermReveal(ctx, "revealtmux", paneNumber, flag)
	return err
}

// itermActivate brings iTerm2 forward without selecting anything in it.
//
// The half of a reveal that has no session id to work from. It does not stand
// on its own under `tmux -CC`: iTerm2 does not move its selected tab when
// tmux's active window changes — measured twice, including with iTerm2 already
// frontmost — so on its own this lands the window in front of whatever tab was
// last looked at. It stays for the case where the mirroring row cannot be
// found, where that is still better than nothing happening at all.
func itermActivate(ctx context.Context) error {
	_, err := itermReveal(ctx, "activate")
	return err
}

// itermListedRowTTYs is the ptys one `list` read says iTerm2 is holding, bare
// (`ttys006`) and without the empty ones, which are the pty-less mirror rows.
//
// The second return is *iTerm2 answered*. A caller must read a false as "not
// proved" rather than as an empty set: the one that asks stops there instead of
// bringing forward an application it could not confirm.
func itermListedRowTTYs(ctx context.Context) (map[string]bool, bool) {
	answer, err := itermReveal(ctx, "list")
	if err != nil {
		return nil, false
	}
	raw, ok := answer["ttys"].([]any)
	if !ok {
		return nil, false
	}
	out := map[string]bool{}
	for _, item := range raw {
		tty, _ := item.(string)
		tty = strings.TrimPrefix(strings.TrimSpace(tty), "/dev/")
		if tty != "" {
			out[tty] = true
		}
	}
	return out, true
}
