//go:build darwin

package terminal

import (
	"context"
	"strings"
)

// revealFollow is the courtesy half of a tmux reveal on the one platform where
// part of it can be asked for.
//
// Two locks, and the order is the point.
//
//  1. **tmux's own client list.** A control-mode client is attached to one tmux
//     session and the pane belongs to one, so a pane in the session iTerm2 is
//     drawing may be followed and a pane in a session Ghostty or Terminal.app
//     is attached to may not — even while an iTerm2 `-CC` client exists
//     elsewhere on the same server. The cheap question first, so no Apple Event
//     is spent on a pane nothing is mirroring.
//  2. **iTerm2's own listing.** tmux says something is speaking control mode
//     over a pty; it does not say that something is iTerm2. `tmux -C attach`
//     from a script carries the identical flag. So before an *application* is
//     raised, the client's tty has to be found among the rows iTerm2 says it is
//     holding, and a listing iTerm2 would not give is not a yes.
//
// `activate: false` stops after the tab: `revealtmux` names a row iTerm2 itself
// says is drawing this pane, which is a stronger statement than the client list
// can make, so the second lock is not needed where nothing is raised. And no
// fallback there either — raising iTerm2 is the one thing not activating is a
// promise not to do.
func revealFollow(ctx context.Context, t *Tmux, paneID string, activate bool) {
	number, ok := tmuxMirrorPaneNumber(paneID)
	if !ok {
		return
	}
	clients := t.controlModeClients(ctx)
	if len(clients) == 0 {
		return
	}
	paneSession := t.sessionName(ctx, paneID)
	if !shouldActivateITerm(paneSession, clients) {
		return
	}
	if !activate {
		_ = itermRevealTmuxPane(ctx, number, false)
		return
	}
	ttys, listed := itermListedRowTTYs(ctx)
	if !listed {
		return
	}
	if !shouldActivateITerm(paneSession, drawnByITerm2(clients, ttys)) {
		return
	}
	// An application that will not come forward is worth no failure of its own:
	// this is a courtesy on a background goroutine and its outcome is discarded
	// either way. The fallback is deliberate — a window in front of the wrong
	// tab beats nothing happening at all, and it is only reached when iTerm2
	// says it is drawing no tab for this pane.
	if err := itermRevealTmuxPane(ctx, number, true); err != nil {
		_ = itermActivate(ctx)
	}
}

// drawnByITerm2 keeps the control-mode clients whose tty is one iTerm2 says it
// is holding. The gateway is an ordinary iTerm2 session with a real tty and it
// is in the very list being attributed, which is what makes this answerable.
//
// An empty `#{client_tty}` is no match rather than a wildcard: a client
// attached through a fifo has no tty at all, and an empty string matching
// everything is the shape that makes a guard say yes to what it was written to
// refuse.
func drawnByITerm2(clients []controlModeClient, rowTTYs map[string]bool) []controlModeClient {
	out := make([]controlModeClient, 0, len(clients))
	for _, c := range clients {
		tty := strings.TrimPrefix(c.tty, "/dev/")
		if tty == "" || !rowTTYs[tty] {
			continue
		}
		out = append(out, c)
	}
	return out
}

// tmuxMirrorPaneNumber is `%65` as iTerm2 spells it: `65`, and nothing at all
// for anything that is not a pane id.
//
// The refusal is the point. The number is compared against a session variable
// with `!==`, so a `%` left on the front or an empty string would match no row
// and be indistinguishable from *iTerm2 is not drawing this pane* — the answer
// that sends the caller on to raise the application blind.
func tmuxMirrorPaneNumber(paneID string) (string, bool) {
	if !IsPaneID(paneID) {
		return "", false
	}
	return paneID[1:], true
}
