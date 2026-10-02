// Package tmuxview opens a session the way projects.PlanITermTmux says: the
// program in a new detached tmux session, and an iTerm2 tab that only shows it.
//
// iTerm2's Apple Events stall in episodes of minutes (eleven between
// 2026-09-23 and 2026-10-02 in one daemon's log): a screen read is killed at
// its limit, a send answers "iTerm2 did not answer in time.", and a session
// that lives in an iTerm2 tab can be neither read nor typed into until it
// passes. A tmux capture of the same screen took about 4 ms with no lock and
// no Apple Event. So the session this opens is the tmux pane — every reading,
// line, key, interrupt and close goes through tmux — and the one Apple Event
// left is the one that opens the tab
// (docs/interface.md, "An iTerm2 tab that only shows a tmux session").
//
// One helper for every caller that opens a session — the start route, a squad
// launch, a child, a hand-over — so the four cannot drift apart on what a
// viewer is.
package tmuxview

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/ports"
)

// Backend is the session backend a viewed session is recorded under.
const Backend = "tmux"

// Opened is a session started in tmux.
type Opened struct {
	// PaneID is the tmux pane the program runs in: the session's id.
	PaneID string
	// TabID is the iTerm2 session id of the viewer tab, empty when no tab
	// opened.
	TabID string
	// Attach is what a person types to see the session, filled exactly when
	// no tab opened to show it.
	Attach string
}

// Open starts command in a new detached tmux session called name, in cwd, on
// the person's default tmux server, and opens an iTerm2 tab attached to that
// session alone.
//
// What it answers, by case:
//   - tmux session and tab: the pane, and the tab's id.
//   - tmux session, no tab: still the pane — the work started and must not
//     be started again — with Attach saying how to see it. Why the tab did
//     not open is logged.
//   - no tmux session: the launcher's error, and no tab is opened.
func Open(ctx context.Context, l ports.Launcher, cwd, name, command string) (Opened, error) {
	pane, err := l.NewTmuxSession(ctx, cwd, name, command)
	if err != nil {
		return Opened{}, err
	}
	out := Opened{PaneID: pane}
	line, err := l.PrepareTmuxViewer(ctx, name)
	if err == nil {
		out.TabID, err = l.NewITermTab(ctx, line)
	}
	if err != nil {
		out.TabID = ""
		out.Attach = projects.TmuxAttachSessionCommand(name)
		log.Printf("tmuxview: tmux session %s (pane %s) started, but no iTerm2 tab shows it: %v", name, pane, err)
	}
	return out, nil
}

// SessionName is a new tmux session name for a session nobody else names:
// `clawdline-<kind>-` and eight random hex. A name tmux already has is refused
// by `new-session`, never reused, so a collision is a failed open, not a
// session shared with somebody else's.
func SessionName(kind string) string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on the platforms this builds for; if it
		// did, the fixed name is still refused by tmux rather than shared.
		return "clawdline-" + kind
	}
	return "clawdline-" + kind + "-" + hex.EncodeToString(b[:])
}
