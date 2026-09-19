//go:build darwin

package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// ITermRunning is whether iTerm2 is open, asked of Launch Services rather than
// of iTerm2: an Apple Event to an application that is not running starts it,
// and this question must never be the thing that opens a terminal.
func (l Launcher) ITermRunning(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/lsappinfo", "find", "bundleid=com.googlecode.iterm2").Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// itermNewTab is iterm.js's `newtab`: a tab in the current window, or a new
// window when there is none, and one line typed into its shell. Nothing here
// calls activate(): the person who asked is not the person at this screen.
const itermNewTab = `
function run(argv) {
  const line = String(argv[0] || "");
  if (!line) return JSON.stringify({ ok: false, error: "Nothing to run" });
  const it = Application("iTerm2");
  if (!it.running()) return JSON.stringify({ ok: false, error: "iTerm2 is not running" });
  let w, t;
  try {
    w = it.currentWindow();
    t = w.createTabWithDefaultProfile();
  } catch (e) {
    try {
      w = it.createWindowWithDefaultProfile();
      t = w.currentTab();
    } catch (e2) {
      return JSON.stringify({ ok: false, error: "Could not open a tab: " + e2.message });
    }
  }
  let s;
  try { s = t.currentSession(); } catch (e) {
    return JSON.stringify({ ok: false, error: "The new tab has no session" });
  }
  try { s.write({ text: line, newline: true }); } catch (e) {
    return JSON.stringify({ ok: false, error: "Could not start it: " + e.message });
  }
  let tty = "";
  for (let i = 0; i < 20 && !tty; i++) {
    try { tty = String(s.tty() || ""); } catch (e) { tty = ""; }
    if (!tty) delay(0.05);
  }
  let id = "";
  try { id = String(s.id() || ""); } catch (e) { id = ""; }
  return JSON.stringify({ ok: true, id: id, tty: tty });
}
`

// NewITermTab runs itermNewTab. The line is an argument, never part of the
// script text, so nothing in it is read as JavaScript. The id is returned as
// iTerm2 spells it, which is how this daemon's inventory lists the tab.
func (l Launcher) NewITermTab(ctx context.Context, line string) (string, error) {
	defer effect()()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-", line)
	cmd.Stdin = strings.NewReader(itermNewTab)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", osascriptFailure(ctx, stderr.String(), err, "iTerm2 would not open a tab.")
	}
	var answer struct {
		OK    bool   `json:"ok"`
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &answer) != nil {
		return "", Failure{Message: "iTerm2 answered something that is not JSON."}
	}
	if !answer.OK || answer.ID == "" {
		if answer.Error == "" {
			answer.Error = "iTerm2 would not open a tab."
		}
		return "", Failure{Message: answer.Error}
	}
	return answer.ID, nil
}

// itermCloseScript is iterm.js's `close`: one session, found by the id iTerm2
// gave back when it was opened.
//
// It closes the *session*, not its tab or window, as iterm.js does: a tab can
// be split, and `tab.close()` would take the panes beside it, which belong to
// work nobody asked about. A session that was alone in its tab takes the tab
// with it.
const itermCloseScript = itermEach + `
function run(argv) {
  const id = String(argv[0] || "");
  const it = Application("iTerm2");
  if (!it.running()) return JSON.stringify({ ok: false, error: "iTerm2 is not running" });
  let found = null;
  const walk = itermEach(it, function (s) {
    if (String(s.id()) !== id) return false;
    found = s;
    return true;
  });
  if (!found) return JSON.stringify({ ok: false, error: itermMissing(walk) });
  found.close();
  return JSON.stringify({ ok: true });
}
`

// CloseITermSession closes the iTerm2 session with this id and answers whether
// it closed anything. The id is the proof of ownership: it is the one iTerm2
// answered when this daemon opened the tab (NewITermTab), and iTerm2 never
// gives it to another session. A session that is gone — or not seen, past a
// window that would not list — closes nothing and is not an error; anything
// else, including a script that failed part way, is.
func (l Launcher) CloseITermSession(ctx context.Context, id string) (bool, error) {
	if id == "" {
		return false, nil
	}
	err := itermCall(ctx, itermCloseScript, 10*time.Second, id)
	var unsent Unsent
	if errors.As(err, &unsent) {
		return false, nil
	}
	return err == nil, err
}
