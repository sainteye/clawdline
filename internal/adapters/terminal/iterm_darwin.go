//go:build darwin

package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// ITerm enumerates iTerm2's own sessions over Apple Events.
//
// Apple Events rather than the pty because you cannot write into another
// process's tty on a current macOS — TIOCSTI is gone — and iTerm2's own
// scripting interface is the supported way in. This is the one adapter that is
// macOS-only by nature: Windows and Linux reach the same port through tmux and
// through ptys this daemon owns.
type ITerm struct{}

func NewITerm() *ITerm { return &ITerm{} }

func (i *ITerm) Name() string { return "iterm" }

// itermEach is the one walk every iTerm2 script takes over the sessions, and
// it is prepended to each of them.
//
// **A window can answer null for its tabs.** Measured on this Mac: a visible,
// titled window whose `tabs()`, `currentTab()` and `currentSession()` are all
// null. Every script here used to read `tabs.length` straight off it, so one
// such window threw a TypeError out of the listing, the typing, the keys and
// the reveal alike — the listing was never complete, every iTerm2 row reached
// the inventory as a bare tty from the process table, and the session id a new
// tab is opened under was listed nowhere (docs/switch-blockers.md). So a window
// or tab that will not list is skipped and counted: a caller looking for one
// session goes on to the next window, and the listing says it did not see
// everything.
//
// visit answers true to stop the walk.
const itermEach = `
function itermEach(it, visit) {
  let unreadable = 0;
  const wins = it.windows();
  for (let a = 0; a < wins.length; a++) {
    let tabs = null;
    try { tabs = wins[a].tabs(); } catch (e) {}
    if (!tabs) { unreadable++; continue; }
    for (let b = 0; b < tabs.length; b++) {
      let ss = null;
      try { ss = tabs[b].sessions(); } catch (e) {}
      if (!ss) { unreadable++; continue; }
      for (let c = 0; c < ss.length; c++) {
        if (visit(ss[c], wins[a], tabs[b]) === true) return { stopped: true, unreadable: unreadable };
      }
    }
  }
  return { stopped: false, unreadable: unreadable };
}
`

// The whole conversation is one script so that it costs one Apple Event round
// trip rather than one per property per session.
const itermList = itermEach + `
const it = Application("iTerm2");
if (!it.running()) { JSON.stringify({running:false, sessions:[]}); }
else {
  const out = [];
  const walk = itermEach(it, function (s) {
    let tty = "", name = "";
    try { tty = String(s.tty() || ""); } catch (e) {}
    try { name = String(s.name() || ""); } catch (e) {}
    out.push({ id: String(s.id()), tty: tty, name: name });
  });
  JSON.stringify({running:true, sessions: out, unreadable: walk.unreadable});
}
`

type itermRow struct {
	ID   string `json:"id"`
	TTY  string `json:"tty"`
	Name string `json:"name"`
}

func (i *ITerm) Inventory(ctx context.Context) (session.Inventory, error) {
	inv := session.Inventory{
		ObservedAt: time.Now(),
		Provenance: "iterm",
		Complete:   true,
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript")
	cmd.Stdin = strings.NewReader(itermList)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		// A stopped iTerm2 is an observed absence and would be an authoritative
		// empty answer. A failed Apple Event is not: it says nothing about what
		// is open, so the reading is incomplete and says why. Collapsing the two
		// would let a broken bridge quietly delete every iTerm row.
		inv.Complete = false
		inv.Notes = append(inv.Notes, "iTerm2 apple event failed: "+err.Error())
		return inv, nil
	}

	var answer struct {
		Running    bool       `json:"running"`
		Sessions   []itermRow `json:"sessions"`
		Unreadable int        `json:"unreadable"`
	}
	if err := json.Unmarshal(out, &answer); err != nil {
		inv.Complete = false
		inv.Notes = append(inv.Notes, "iTerm2 answer was unreadable: "+err.Error())
		return inv, nil
	}
	if !answer.Running {
		inv.Notes = append(inv.Notes, "iTerm2 is not running")
		return inv, nil
	}
	// The rows it did list are published — each one is a session iTerm2
	// named — but the listing is not all there is, so it proves no absence.
	if answer.Unreadable > 0 {
		inv.Complete = false
		inv.Notes = append(inv.Notes, fmt.Sprintf("iTerm2 would not list the tabs of %d window(s) or tab(s)",
			answer.Unreadable))
	}

	ptyless := 0
	for _, row := range answer.Sessions {
		tty := strings.TrimPrefix(row.TTY, "/dev/")
		if tty == "" {
			// Under `tmux -CC` an iTerm row is a mirror with no pty, while the
			// pane it mirrors is already published by the tmux adapter with its
			// real tty. Publishing the mirror too would put one session on the
			// list twice under two identities.
			ptyless++
			continue
		}
		inv.Sessions = append(inv.Sessions, session.Session{
			ID:       row.ID,
			Backend:  session.BackendITerm,
			TTY:      tty,
			Label:    row.Name,
			State:    session.StateUnknown,
			Evidence: session.EvidenceProcess,
		})
	}
	if ptyless > 0 {
		inv.Notes = append(inv.Notes,
			"iTerm2 reported ptyless mirror rows, left to the tmux adapter")
	}
	return inv, nil
}

// itermSendScript is iterm.js's `send`: the text as one bracketed paste, then
// a Return of its own, then a look at the composer — and another Return only
// while the paste is demonstrably still sitting there.
//
// The first version of this called `writeText`, which is not in iTerm2's
// scripting dictionary (the command is `write` with a `text` parameter), sent
// no Return at all, and discarded what the script answered, so a session that
// was not found read as a line delivered. The briefing is typed through here.
const itermSendScript = itermEach + `
function run(argv) {
  const id = String(argv[0] || ""), text = argv[1] === undefined ? "" : String(argv[1]);
  const it = Application("iTerm2");
  if (!it.running()) return JSON.stringify({ ok: false, error: "iTerm2 is not running" });
  const ESC = String.fromCharCode(27), CR = String.fromCharCode(13);
  // The tail of what was pasted, whitespace removed so a wrapped line still
  // matches. A briefing ends in a 64-character secret, so it is unique.
  const needle = text.replace(/\s+/g, "").slice(-24);
  function stillInComposer(s) {
    if (!needle) return false;
    let screen = "";
    try { screen = String(s.text() || ""); } catch (e) { screen = ""; }
    const lines = screen.replace(/\s+$/, "").split("\n");
    let mark = -1;
    for (let i = lines.length - 1; i >= 0 && i >= lines.length - 12; i--) {
      const head = lines[i].replace(/^[\s\u2502\u2503|]+/, "").charAt(0);
      if (head === ">" || head === "\u203a") { mark = i; break; }
    }
    if (mark < 0) return false;
    return lines.slice(mark).join("").replace(/\s+/g, "").indexOf(needle) >= 0;
  }
  let found = null;
  itermEach(it, function (s) {
    if (String(s.id()) !== id) return false;
    found = s;
    return true;
  });
  if (!found) return JSON.stringify({ ok: false, error: "That session is gone" });
  found.write({ text: ESC + "[200~" + text + ESC + "[201~", newline: false });
  delay(0.06);
  found.write({ text: CR, newline: false });
  for (let attempt = 0; attempt < 3; attempt++) {
    delay(attempt === 0 ? 0.25 : 0.4);
    if (!stillInComposer(found)) break;
    found.write({ text: CR, newline: false });
  }
  return JSON.stringify({ ok: true });
}
`

// Send types one line into an iTerm2 session and submits it.
//
// Apple Events rather than the tty: you cannot write into another process's tty
// on a current macOS, and iTerm2's own `write` does not bring the window
// forward, which is the whole point.
func (i *ITerm) Send(ctx context.Context, s session.Session, text string) error {
	return itermCall(ctx, itermSendScript, 10*time.Second, s.ID, text)
}

// Open is not implemented for the iTerm backend yet.
func (i *ITerm) Open(ctx context.Context, req ports.OpenRequest) (session.Session, error) {
	return session.Session{}, errUnsupported("open a session")
}

// Interrupt and Close are not implemented for the iTerm backend yet. They
// answer with a refusal rather than doing nothing quietly, because a caller
// that believes a turn was stopped is worse off than one told it was not.
func (i *ITerm) Interrupt(ctx context.Context, s session.Session) error {
	return errUnsupported("interrupt")
}

func (i *ITerm) Close(ctx context.Context, s session.Session) error {
	return errUnsupported("close")
}

// appleEvents serialises every iTerm2 Apple Event that has an effect —
// writing into a session, a keystroke, opening a tab, selecting one — across
// this whole process (docs/design-decisions.md D22). The caller's lane already
// keeps one writer per session; this is the adapter's own half, because two
// scripts that each ask iTerm2 for "the current window" and then act on it can
// interleave inside iTerm2 whatever sessions they were aimed at. Reading the
// session list and a screen stay outside it: they change nothing, and a
// twenty-second tab opening must not stall the list every page is drawn from.
var appleEvents sync.Mutex

// effect takes appleEvents and answers its release, for `defer effect()()`.
func effect() func() {
	appleEvents.Lock()
	return appleEvents.Unlock
}

// itermCall runs one effect script with its arguments — never inside the
// script text, so no quoting rule has to be right about them — and reads its
// `{ok, error}` answer. A script that answered "not done" is a failure, never
// a quiet success.
func itermCall(ctx context.Context, script string, limit time.Duration, args ...string) error {
	defer effect()()
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", append([]string{"-l", "JavaScript", "-"}, args...)...)
	cmd.Stdin = strings.NewReader(script)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		said := strings.TrimSpace(stderr.String())
		if said == "" {
			said = "iTerm2 did not answer: " + err.Error()
		}
		return Failure{Attention: strings.Contains(said, "-1712") || strings.Contains(said, "-1743"), Message: said}
	}
	return itermAnswer(out)
}

// itermAnswer reads what an effect script said it did.
func itermAnswer(out []byte) error {
	var answer struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &answer) != nil {
		return Failure{Message: "iTerm2 answered something that is not JSON."}
	}
	if !answer.OK {
		if answer.Error == "" {
			answer.Error = "iTerm2 refused."
		}
		return Failure{Message: answer.Error}
	}
	return nil
}
