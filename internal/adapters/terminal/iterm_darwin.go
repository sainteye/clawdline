//go:build darwin

package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// ITerm enumerates iTerm2's own sessions over Apple Events.
//
// Apple Events rather than the pty because you cannot write into another
// process's tty on a current macOS — TIOCSTI is gone — and iTerm2's own
// scripting interface is the supported way in. This is the one adapter that is
// macOS-only by nature: Windows and Linux reach the same port through tmux and
// through ptys this daemon owns.
type ITerm struct {
	// ptys reads which pseudo-terminals belong to iTerm2 from the process
	// table. It is what seals a window this adapter cannot read, and it is a
	// field so a test can hand over a machine of its own — including one where
	// the process table cannot be read at all, which must seal nothing.
	ptys func(context.Context) (itermPTYs, bool)
	list func(context.Context) ([]byte, error)
	now  func() time.Time

	listingMu       sync.Mutex
	listingFailures int
	listingQuiet    time.Time
	backoffFirst    time.Duration
	backoffMax      time.Duration
}

func NewITerm() *ITerm {
	return &ITerm{ptys: systemITermPTYs, list: runITermList, now: time.Now}
}

// SetInventoryBackoff configures how long repeated failed list Apple Events
// are left alone. The composition root supplies already registered bounds;
// without that wiring the adapter preserves its old retry-every-call behavior.
func (i *ITerm) SetInventoryBackoff(first, maximum time.Duration) {
	i.listingMu.Lock()
	defer i.listingMu.Unlock()
	if first <= 0 || maximum < first {
		i.backoffFirst, i.backoffMax = 0, 0
		return
	}
	i.backoffFirst, i.backoffMax = first, maximum
}

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
//
// itermMissing is what a script that looked for one session and did not find
// it says. A walk that skipped a window it could not read has not seen that
// session, which is not the same as its being gone (F7 of the review of
// e54e338): "gone" is said only by a walk that read everything.
const itermEach = `
function itermMissing(walk) {
  if (!walk || !walk.unreadable) return "That session is gone";
  return "That session was not seen: " + walk.unreadable + " window(s) or tab(s) could not be read";
}
function itermEach(it, visit) {
  let unreadable = 0;
  const gaps = [];
  // A gap is recorded against the window, not the tab, so a window with
  // forty tabs it will not list is one thing a person can look at rather
  // than forty. The count is still per region, because it is what decides
  // whether the listing was whole.
  function note(id, why) {
    for (let g = 0; g < gaps.length; g++) {
      if (gaps[g].window === id) { gaps[g].regions++; return; }
    }
    gaps.push({ window: id, why: why, regions: 1 });
  }
  const wins = it.windows();
  for (let a = 0; a < wins.length; a++) {
    let wid = "";
    try { wid = String(wins[a].id()); } catch (e) {}
    let tabs = null;
    try { tabs = wins[a].tabs(); } catch (e) {}
    if (!tabs) { unreadable++; note(wid, "tabs() answered null"); continue; }
    for (let b = 0; b < tabs.length; b++) {
      let ss = null;
      try { ss = tabs[b].sessions(); } catch (e) {}
      if (!ss) { unreadable++; note(wid, "a tab's sessions() answered null"); continue; }
      for (let c = 0; c < ss.length; c++) {
        if (visit(ss[c], wins[a], tabs[b]) === true) {
          return { stopped: true, unreadable: unreadable, gaps: gaps };
        }
      }
    }
  }
  return { stopped: false, unreadable: unreadable, gaps: gaps };
}
`

// Ask each window for its sessions and three columns of properties. JXA sends
// one Apple Event per column for the whole window; asking id, tty and name on each
// session made a window with many tabs cost three round trips per session.
// A malformed window falls back to the old walk only for that window, so one
// bad tab cannot make the other windows disappear.
const itermList = `
const it = Application("iTerm2");
if (!it.running()) { JSON.stringify({running:false, sessions:[]}); }
else {
  const out = [];
  let unreadable = 0;
  const gaps = [];
  function note(win, why) {
    unreadable++;
    let wid = "";
    try { wid = String(win.id()); } catch (e) {}
    for (let g = 0; g < gaps.length; g++) {
      if (gaps[g].window === wid) { gaps[g].regions++; return; }
    }
    gaps.push({ window: wid, why: why, regions: 1 });
  }
  function append(s) {
    let tty = "", name = "";
    try { tty = String(s.tty() || ""); } catch (e) {}
    try { name = String(s.name() || ""); } catch (e) {}
    out.push({ id: String(s.id()), tty: tty, name: name });
  }
  const wins = it.windows();
  for (let a = 0; a < wins.length; a++) {
    const win = wins[a];
    let tabs = null;
    try { tabs = win.tabs(); } catch (e) {}
    if (!tabs) { note(win, "tabs() answered null"); continue; }

    let groups, ids, ttys, names;
    try {
      groups = win.tabs.sessions();
      ids = win.tabs.sessions.id();
      ttys = win.tabs.sessions.tty();
      names = win.tabs.sessions.name();
    } catch (e) {}
    let whole = Array.isArray(groups) && Array.isArray(ids) && Array.isArray(ttys) && Array.isArray(names) &&
      groups.length === tabs.length && ids.length === tabs.length &&
      ttys.length === tabs.length && names.length === tabs.length;
    if (whole) {
      for (let b = 0; b < tabs.length; b++) {
        if (!Array.isArray(groups[b]) || !Array.isArray(ids[b]) ||
            !Array.isArray(ttys[b]) || !Array.isArray(names[b]) ||
            ids[b].length !== groups[b].length || ids[b].length !== ttys[b].length ||
            ids[b].length !== names[b].length || ids[b].some(id => !id)) {
          whole = false;
          break;
        }
      }
    }
    if (whole) {
      for (let b = 0; b < tabs.length; b++) {
        for (let c = 0; c < ids[b].length; c++) {
          out.push({ id: String(ids[b][c] || ""), tty: String(ttys[b][c] || ""),
            name: String(names[b][c] || "") });
        }
      }
      continue;
    }
    for (let b = 0; b < tabs.length; b++) {
      let sessions = null;
      try { sessions = tabs[b].sessions(); } catch (e) {}
      if (!sessions) { note(win, "a tab's sessions() answered null"); continue; }
      for (let c = 0; c < sessions.length; c++) append(sessions[c]);
    }
  }
  JSON.stringify({running:true, sessions: out, unreadable: unreadable, gaps: gaps});
}
`

type itermRow struct {
	ID   string `json:"id"`
	TTY  string `json:"tty"`
	Name string `json:"name"`
}

func (i *ITerm) Inventory(ctx context.Context) (session.Inventory, error) {
	now := i.now()
	inv := session.Inventory{
		ObservedAt: now,
		Provenance: "iterm",
		Complete:   true,
	}
	i.listingMu.Lock()
	quiet, failures := i.listingQuiet, i.listingFailures
	i.listingMu.Unlock()
	if !quiet.IsZero() && now.Before(quiet) {
		inv.Complete = false
		inv.Notes = append(inv.Notes, fmt.Sprintf(
			"iTerm2 listing is degraded after %d failed Apple Event(s); retry after %s",
			failures, quiet.Format(time.RFC3339)))
		inv.Gaps = append(inv.Gaps, session.Gap{Source: "iterm", Scope: "listing",
			Detail: "iTerm2 listing is in failure backoff"})
		return inv, nil
	}
	select {
	case appleEvents <- struct{}{}:
		defer func() { <-appleEvents }()
	default:
		inv.Complete = false
		inv.Gaps = append(inv.Gaps, session.Gap{Source: "iterm", Scope: "listing",
			Detail: "iTerm2 listing deferred while a terminal action is running"})
		return inv, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	out, err := i.list(ctx)
	if err != nil {
		// A stopped iTerm2 is an observed absence and would be an authoritative
		// empty answer. A failed Apple Event is not: it says nothing about what
		// is open, so the reading is incomplete and says why. Collapsing the two
		// would let a broken bridge quietly delete every iTerm row.
		inv.Complete = false
		inv.Notes = append(inv.Notes, "iTerm2 apple event failed: "+err.Error())
		i.failedListing(now)
		return inv, nil
	}

	var answer struct {
		Running    bool       `json:"running"`
		Sessions   []itermRow `json:"sessions"`
		Unreadable int        `json:"unreadable"`
		Gaps       []itermGap `json:"gaps"`
	}
	if err := json.Unmarshal(out, &answer); err != nil {
		inv.Complete = false
		inv.Notes = append(inv.Notes, "iTerm2 answer was unreadable: "+err.Error())
		i.failedListing(now)
		return inv, nil
	}
	i.succeededListing()
	if !answer.Running {
		inv.Notes = append(inv.Notes, "iTerm2 is not running")
		return inv, nil
	}
	// The rows it did list are published — each one is a session iTerm2
	// named — but the listing is not all there is, so it proves no absence.
	//
	// Which is true of the windows it could not read, and of nothing else. So
	// each one is named here, and then offered to the process table: a window
	// that is hiding no pty is hiding no session this adapter would ever
	// publish, and a listing whose every gap is sealed that way answers for
	// itself again (sealITermGaps).
	if answer.Unreadable > 0 {
		inv.Complete = false
		inv.Notes = append(inv.Notes, fmt.Sprintf("iTerm2 would not list the tabs of %d window(s) or tab(s)",
			answer.Unreadable))
		inv.Gaps = itermGaps(answer.Gaps, answer.Unreadable)
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
	if len(inv.Gaps) > 0 {
		inv = sealITermGaps(ctx, inv, i.ptys)
	}
	return inv, nil
}

func runITermList(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript")
	cmd.Stdin = strings.NewReader(itermList)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	return cmd.Output()
}

func (i *ITerm) failedListing(now time.Time) {
	i.listingMu.Lock()
	defer i.listingMu.Unlock()
	if i.backoffFirst <= 0 {
		return
	}
	i.listingFailures++
	wait := i.backoffFirst
	for n := 1; n < i.listingFailures && wait < i.backoffMax; n++ {
		wait *= 2
		if wait >= i.backoffMax {
			wait = i.backoffMax
			break
		}
	}
	i.listingQuiet = now.Add(wait)
}

func (i *ITerm) succeededListing() {
	i.listingMu.Lock()
	i.listingFailures = 0
	i.listingQuiet = time.Time{}
	i.listingMu.Unlock()
}

// itermGap is one window the walk could not read, as the script names it.
type itermGap struct {
	Window  string `json:"window"`
	Why     string `json:"why"`
	Regions int    `json:"regions"`
}

// itermGaps turns the script's answer into the reading's own gaps.
//
// A script that answered a count and no names — an older build of this walk,
// or one whose windows would not even give their ids — still leaves a gap, and
// an unnamed gap can never be sealed: a region nothing can point at is a
// region nothing can account for.
func itermGaps(gaps []itermGap, unreadable int) []session.Gap {
	out := make([]session.Gap, 0, len(gaps))
	named := 0
	for _, g := range gaps {
		if g.Window == "" {
			continue
		}
		named += g.Regions
		detail := fmt.Sprintf("iTerm2 window %s would not list its tabs (%s)", g.Window, g.Why)
		if g.Regions > 1 {
			detail = fmt.Sprintf("iTerm2 window %s would not list %d of its regions (%s)",
				g.Window, g.Regions, g.Why)
		}
		out = append(out, session.Gap{Source: "iterm", Scope: "window", ID: g.Window, Detail: detail})
	}
	if named < unreadable {
		out = append(out, session.Gap{
			Source: "iterm", Scope: "listing",
			Detail: fmt.Sprintf("iTerm2 left %d window(s) or tab(s) unread and would not name them",
				unreadable-named),
		})
	}
	return out
}

// itermSendScript is submit.go's four steps in iTerm2, with the same rule
// (inputRuleJS): the text as one bracketed paste; the session's screen read
// until it shows the text arriving; then a Return of its own, and never
// before; then another Return only while a framed composer still shows exactly
// what was confirmed.
//
// It pastes bracketed whatever the program asked for, because iTerm2's
// scripting does not say whether it did; tmux does, and brackets only then.
//
// The first version of this called `writeText`, which is not in iTerm2's
// scripting dictionary (the command is `write` with a `text` parameter), sent
// no Return at all, and discarded what the script answered, so a session that
// was not found read as a line delivered. The next pasted, waited 60 ms and
// pressed Return whether or not the program had read the paste. The briefing
// is typed through here.
var itermSendScript = itermEach + inputRuleJS + fmt.Sprintf(`
function run(argv) {
  const id = String(argv[0] || ""), text = argv[1] === undefined ? "" : String(argv[1]);
  const it = Application("iTerm2");
  if (!it.running()) return JSON.stringify({ ok: false, error: "iTerm2 is not running" });
  const ESC = String.fromCharCode(27), CR = String.fromCharCode(13);
  const LOOKS = %d, WINDOW_MS = %d, PAUSES = %s, NUDGES = %s;
  function screenOf(s) {
    try { return String(s.text() || ""); } catch (e) { return ""; }
  }
  let found = null;
  const walk = itermEach(it, function (s) {
    if (String(s.id()) !== id) return false;
    found = s;
    return true;
  });
  if (!found) return JSON.stringify({ ok: false, error: itermMissing(walk) });
  const before = screenOf(found);
  found.write({ text: ESC + "[200~" + text + ESC + "[201~", newline: false });
  const until = Date.now() + WINDOW_MS;
  let confirmed = null;
  for (let look = 0; look < LOOKS; look++) {
    const screen = screenOf(found);
    if (showsText(before, screen, text)) { confirmed = screen; break; }
    if (Date.now() >= until) break;
    delay(PAUSES[Math.min(look, PAUSES.length - 1)]);
  }
  if (confirmed === null) {
    return JSON.stringify({ ok: false, typed: true, error: "the text was typed, but the terminal did not " +
      "show it arriving in its input line within %s, so Enter was not pressed" });
  }
  found.write({ text: CR, newline: false });
  for (let n = 0; n < NUDGES.length; n++) {
    delay(NUDGES[n]);
    const now = screenOf(found);
    if (!stillHolds(confirmed, now)) break;
    found.write({ text: CR, newline: false });
    confirmed = now;
  }
  return JSON.stringify({ ok: true });
}
`, looksWithin(sendConfirm), sendConfirm.Milliseconds(), seconds(submitPauses), seconds(nudgePauses), sendConfirm)

// itermSendLimit bounds the whole send script: the look for the paste
// (sendConfirm), the looks after the Return, and the Apple Events between.
var itermSendLimit = sendConfirm + 6*time.Second

// Send types one line into an iTerm2 session and submits it.
//
// Apple Events rather than the tty: you cannot write into another process's tty
// on a current macOS, and iTerm2's own `write` does not bring the window
// forward, which is the whole point.
func (i *ITerm) Send(ctx context.Context, s session.Session, text string) error {
	return itermCall(ctx, itermSendScript, itermSendLimit, s.ID, text)
}

// Open is not implemented for the iTerm backend yet.
func (i *ITerm) Open(ctx context.Context, req ports.OpenRequest) (session.Session, error) {
	return session.Session{}, errUnsupported("open a session")
}

// Interrupt stops the current turn with one Escape, as Tmux.Interrupt does
// and for the same reason, typed through the same key script a menu answer
// uses.
func (i *ITerm) Interrupt(ctx context.Context, s session.Session) error {
	return i.Keystroke(ctx, s, keyEscape)
}

// Close closes one iTerm2 session — the session, never its tab or window
// (itermCloseScript) — found by its id.
//
// **It walks the farewell ladder first** (farewell.go): an assistant running
// in that tab is sent its own quit word, watched until the kernel says its
// foreground group is gone, and only then is the tab closed. Closing straight
// away is what put "Close tab #N? This tab is running claude." on the person's
// screen and left both the tab and the session where they were — the bug this
// answers — and it is also how a transcript being appended to is cut off.
//
// It is bounded like every effect here, and its failures are typed: a session
// that was not found, or not seen, is Unsent, and closed nothing; an Apple
// Event that timed out (-1712, which a close on this Mac has answered) or was
// killed at the limit is looked for again, and is Unconfirmed unless it is
// then gone (closeITermByID) — it may still close after its limit.
func (i *ITerm) Close(ctx context.Context, s session.Session) error {
	if s.ID == "" {
		return Unsent{Why: "there is no iTerm2 session id to close"}
	}
	return i.farewell().say(ctx, s)
}

// farewell is Close's ladder on this backend's own steps.
func (i *ITerm) farewell() farewell {
	return farewell{
		look:   itermSight,
		send:   func(ctx context.Context, s session.Session, line string) error { return i.Send(ctx, s, line) },
		signal: ttySignal,
		close:  func(ctx context.Context, s session.Session) error { return closeITermByID(ctx, s.ID) },
		polite: farewellPolite, afterTerm: farewellAfterTerm, afterKill: farewellAfterKill,
		tick: farewellTick, now: time.Now,
	}
}

// itermSight is one look into an iTerm2 session: iTerm2 for whether the tab is
// still there and which tty it is, the kernel for what is in front of it.
//
// A session iTerm2 did not list past a window it could not read is neither
// there nor gone, and that is an error rather than an absence: a close that
// read "not seen" as "already closed" would report a tab taken away that is
// still on the screen.
func itermSight(ctx context.Context, s session.Session) (farewellSight, error) {
	seen, tty := findITermSession(ctx, s.ID)
	switch {
	case seen == sightingGone:
		return farewellSight{Gone: true}, nil
	case seen != sightingThere || tty == "":
		return farewellSight{}, errors.New("iTerm2 did not list that session, and did not list everything either")
	}
	return ttySight(tty, s)
}

// appleEvents serialises every iTerm2 Apple Event that has an effect —
// writing into a session, a keystroke, opening a tab, selecting one — across
// this whole process (docs/design-decisions.md D22). The caller's lane already
// keeps one writer per session; this is the adapter's own half, because two
// scripts that each ask iTerm2 for "the current window" and then act on it can
// interleave inside iTerm2 whatever sessions they were aimed at. A listing
// takes this lane only when it is free, so it cannot compete with an effect;
// screen captures stay outside it and are bounded by the held-screen reader.
var appleEvents = make(chan struct{}, 1)

// effect waits only as long as the caller can still use the lane. A mutex
// taken after the request expired used to launch an osascript already doomed
// to be killed, adding another Apple Event to a jammed application.
func effect(ctx context.Context) (func(), error) {
	if ctx.Err() != nil {
		return nil, EffectBusy{Why: "iTerm2 was busy before this action began"}
	}
	select {
	case appleEvents <- struct{}{}:
		if ctx.Err() != nil {
			<-appleEvents
			return nil, EffectBusy{Why: "iTerm2 was busy before this action began"}
		}
		return func() { <-appleEvents }, nil
	case <-ctx.Done():
		return nil, EffectBusy{Why: "iTerm2 was busy before this action began"}
	}
}

// itermCall runs one effect script with its arguments — never inside the
// script text, so no quoting rule has to be right about them — and reads its
// `{ok, error}` answer. Its script budget starts after the lane is acquired;
// waiting behind a bounded listing cannot consume the time needed to send.
// A script that answered "not done" is a failure, never a quiet success.
func itermCall(ctx context.Context, script string, limit time.Duration, args ...string) error {
	release, err := effect(ctx)
	if err != nil {
		return err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", append([]string{"-l", "JavaScript", "-"}, args...)...)
	cmd.Stdin = strings.NewReader(script)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return osascriptFailure(ctx, stderr.String(), err, "iTerm2 did not do what it was asked.")
	}
	return itermAnswer(out)
}

// osascriptFailure is a failed osascript run as it is reported.
//
// **osascript's own words are read here and go no further.** They carry the
// script's position, the interpreter's path and an Apple Event error number,
// and the app being replicated never sends any of it: its iTerm2 failures carry
// the script's `error` field or a fixed sentence
// (`ITerm.terminalFailure(_:fallback:)`). So the reader of this machine's log
// gets the diagnosis, and the refusal a phone shows — and the spawn_error a
// record keeps — gets the sentence (F8 of the review of e54e338).
//
// It is never Unsent. A script that was killed or threw may have written
// before it stopped, and only the script's own answer can say it did not.
func osascriptFailure(ctx context.Context, stderr string, err error, sentence string) Failure {
	said := strings.TrimSpace(stderr)
	// -1712 is errAETimeout and -1743 is a refused automation permission:
	// both are something on the Mac's screen waiting for a person.
	attention := strings.Contains(said, "-1712") || strings.Contains(said, "-1743") ||
		ctx.Err() == context.DeadlineExceeded
	if said != "" {
		log.Printf("iterm: osascript refused: %s", said)
	} else {
		log.Printf("iterm: osascript failed: %v", err)
	}
	if ctx.Err() == context.DeadlineExceeded {
		sentence = "iTerm2 did not answer in time."
	}
	return Failure{Attention: attention, Message: sentence}
}

// itermAnswer reads what an effect script said it did.
//
// "Not done" is Unsent: every effect script looks its session up, and answers
// not done, before it writes anything — a caller may type the same line again
// without delivering it twice. The one exception says so: the send script's
// "typed, and not submitted" (`typed: true`) is Unsubmitted, because its text
// is in the session. An answer that is not JSON came from a script that ran,
// and is only a Failure.
func itermAnswer(out []byte) error {
	var answer struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Typed bool   `json:"typed"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &answer) != nil {
		return Failure{Message: "iTerm2 answered something that is not JSON."}
	}
	if !answer.OK {
		if answer.Error == "" {
			answer.Error = "iTerm2 refused."
		}
		if answer.Typed {
			return Unsubmitted{Why: answer.Error}
		}
		return Unsent{Why: answer.Error}
	}
	return nil
}
