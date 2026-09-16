//go:build darwin

package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
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

// The whole conversation is one script so that it costs one Apple Event round
// trip rather than one per property per session.
const itermList = `
const it = Application("iTerm2");
if (!it.running()) { JSON.stringify({running:false, sessions:[]}); }
else {
  const out = [];
  const wins = it.windows();
  for (let i = 0; i < wins.length; i++) {
    const tabs = wins[i].tabs();
    for (let j = 0; j < tabs.length; j++) {
      const ss = tabs[j].sessions();
      for (let k = 0; k < ss.length; k++) {
        let tty = "", name = "";
        try { tty = String(ss[k].tty() || ""); } catch (e) {}
        try { name = String(ss[k].name() || ""); } catch (e) {}
        out.push({ id: String(ss[k].id()), tty: tty, name: name });
      }
    }
  }
  JSON.stringify({running:true, sessions: out});
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
		Running  bool       `json:"running"`
		Sessions []itermRow `json:"sessions"`
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

// Send types one line into an iTerm2 session.
//
// Apple Events rather than the tty: you cannot write into another process's tty
// on a current macOS, and iTerm2's own `write text` does not bring the window
// forward, which is the whole point.
func (i *ITerm) Send(ctx context.Context, s session.Session, text string) error {
	return i.script(ctx, `
const it = Application("iTerm2");
const id = %q, text = %q;
let done = false;
const wins = it.windows();
for (let a = 0; a < wins.length && !done; a++) {
  const tabs = wins[a].tabs();
  for (let b = 0; b < tabs.length && !done; b++) {
    const ss = tabs[b].sessions();
    for (let c = 0; c < ss.length && !done; c++) {
      if (String(ss[c].id()) === id) { ss[c].writeText(text); done = true; }
    }
  }
}
JSON.stringify({sent: done});
`, s.ID, text)
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

func (i *ITerm) script(ctx context.Context, format string, args ...any) error {
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript")
	cmd.Stdin = strings.NewReader(fmt.Sprintf(format, args...))
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	return cmd.Run()
}
