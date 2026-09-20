//go:build darwin

package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
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

// itermCloseLooks and itermCloseLookGap are how a close that was not answered
// is looked for afterwards: up to three looks, two seconds apart, each bounded
// as a listing is (findITermSession).
const (
	itermCloseLooks   = 3
	itermCloseLookGap = 2 * time.Second
)

// closeITermByID is the one close of an iTerm2 session by its id, for the
// broker (CloseITermChild) and for a person's close (ITerm.Close) alike. It
// answers nil when the session closed, Unsent when it was not there to close,
// and Unconfirmed when iTerm2 did not answer and a look afterwards could not
// find it gone.
//
// **A close that ran out of time is not a failed close.** Measured on this Mac
// (docs/switch-blockers.md): the Apple Event was killed at its limit, and the
// tab it named closed afterwards — iTerm2 had asked a person whether to close
// a tab with a job running, and the close landed when they answered. So such a
// close is neither counted done — the tab may still be there — nor asked
// again, which would put a second question on the same screen. It is looked
// for instead, by the same id, and only a walk that read every window says it
// is gone: a session not seen past a window that will not list is not a
// session closed. A child's tab is not closed this way with its job running
// at all (CloseITermChild).
//
// Ten seconds, as every effect here: a close with nothing running in the tab
// answered in 0.15 s, and one that ran past ten was waiting for a person —
// which no longer limit cures.
func closeITermByID(ctx context.Context, id string) error {
	err := itermCall(ctx, itermCloseScript, 10*time.Second, id)
	var unsent Unsent
	if err == nil || errors.As(err, &unsent) {
		return err
	}
	return settleUnansweredClose(ctx, id, err, lookITermSession, itermCloseLooks, itermCloseLookGap)
}

// sighting is what one look for one iTerm2 session found.
type sighting int

const (
	// sightingUnknown is a look that failed, or a walk that skipped a window
	// it could not read and did not find the session: not seen, not gone.
	sightingUnknown sighting = iota
	sightingThere
	// sightingGone is a walk that read every window and did not find it, or
	// an iTerm2 that is not running.
	sightingGone
)

// settleUnansweredClose looks again for a session whose close was not
// answered, and answers nil once a look finds it gone. Anything else is
// Unconfirmed, saying what the last look saw.
// **Why the asked-for error's own kind is carried through.** iTerm2 answers a
// close it is holding a sheet behind with a timeout, and that timeout is the
// only sign this daemon gets that there is a question on somebody's screen. A
// settlement that flattened it into "not answered" threw away the one thing a
// person could act on, so `Attention` travels with the answer.
func settleUnansweredClose(ctx context.Context, id string, asked error,
	look func(context.Context, string) sighting, looks int, gap time.Duration) error {
	var failure Failure
	attention := errors.As(asked, &failure) && failure.Attention
	last := sightingUnknown
	for i := 0; i < looks; i++ {
		if i > 0 && !pause(ctx, gap) {
			break
		}
		if last = look(ctx, id); last == sightingGone {
			log.Printf("iterm: the close of %s was not answered (%v), and the session is gone", id, asked)
			return nil
		}
	}
	if last == sightingThere {
		return Unconfirmed{Attention: attention,
			Why: asked.Error() + " The session was still open when it was looked for again, " +
				"and a close iTerm2 has not answered can still land."}
	}
	return Unconfirmed{Attention: attention,
		Why: asked.Error() + " Whether the session closed could not be seen afterwards."}
}

// itermFindScript looks for one session by id and changes nothing. It answers
// the session's tty when it finds it.
const itermFindScript = itermEach + `
function run(argv) {
  const id = String(argv[0] || "");
  const it = Application("iTerm2");
  if (!it.running()) return JSON.stringify({ running: false, found: false, unreadable: 0 });
  let tty = "";
  const walk = itermEach(it, function (s) {
    if (String(s.id()) !== id) return false;
    try { tty = String(s.tty() || ""); } catch (e) { tty = ""; }
    return true;
  });
  return JSON.stringify({ running: true, found: walk.stopped, unreadable: walk.unreadable, tty: tty });
}
`

// lookITermSession is findITermSession without the tty.
func lookITermSession(ctx context.Context, id string) sighting {
	seen, _ := findITermSession(ctx, id)
	return seen
}

// findITermSession runs itermFindScript. It is a reading and takes no turn
// among the effects (appleEvents), as the listing does not: a look that
// waited behind the close it is looking for would be no look at all.
func findITermSession(ctx context.Context, id string) (sighting, string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-", id)
	cmd.Stdin = strings.NewReader(itermFindScript)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return sightingUnknown, ""
	}
	return readSighting(out)
}

// readSighting is what itermFindScript's answer says, and the tty of a
// session it found.
func readSighting(out []byte) (sighting, string) {
	var answer struct {
		Running    *bool  `json:"running"`
		Found      bool   `json:"found"`
		Unreadable int    `json:"unreadable"`
		TTY        string `json:"tty"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &answer) != nil || answer.Running == nil {
		return sightingUnknown, ""
	}
	switch {
	case !*answer.Running:
		return sightingGone, ""
	case answer.Found:
		return sightingThere, answer.TTY
	case answer.Unreadable == 0:
		return sightingGone, ""
	}
	return sightingUnknown, ""
}
