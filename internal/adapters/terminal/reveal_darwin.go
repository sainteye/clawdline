//go:build darwin

package terminal

import (
	"context"
	"strings"
)

// revealFollow is the courtesy half of a tmux reveal on the one platform where
// part of it can be asked for.
//
// **There are two shapes of drawn tmux and they are found by different facts.**
// Which one this is comes from tmux's own client list, and the cheap question
// is asked first so that no Apple Event is spent on a pane nothing is
// mirroring.
//
//  1. **Control mode (`tmux -CC`).** iTerm2 draws each tmux window as a tab of
//     its own, so the tab is found by the pane it is mirroring and there is no
//     tty to match — a mirrored row comes back from `list` with none. tmux says
//     something is speaking control mode over a pty; it does not say that
//     something is iTerm2, and `tmux -C attach` from a script carries the
//     identical flag. So before an *application* is raised, the client's tty
//     has to be found among the rows iTerm2 says it is holding, and a listing
//     iTerm2 would not give is not a yes.
//
//  2. **An ordinary `tmux attach`.** The whole server is one pty in one tab,
//     and that pty is the tty tmux names as its client's. Here the match *is*
//     the lock, and a stronger one than the control-mode path can make: iTerm2
//     is asked which of its own sessions holds that tty, and only a row it
//     names is selected. This is the commonest setup there is — a `tmux attach`
//     typed into an iTerm2 tab — and it used to fall out of this function at
//     the first line, because only control-mode clients were read as clients at
//     all.
//
// `activate: false` stops after the tab, and no fallback anywhere: raising
// iTerm2 is the one thing not activating is a promise not to do.
func revealFollow(ctx context.Context, t *Tmux, paneID string, activate bool) {
	number, ok := tmuxMirrorPaneNumber(paneID)
	if !ok {
		return
	}
	clients, known := t.attachedClients(ctx)
	if !known || len(clients) == 0 {
		return
	}
	watching := clientsWatching(t.sessionName(ctx, paneID), clients)
	if len(watching) == 0 {
		return
	}
	if mirrors := controlModeOnly(watching); len(mirrors) > 0 {
		revealMirroredPane(ctx, number, mirrors, activate)
		return
	}
	// One tab holds the whole server, so the first tty iTerm2 owns is the
	// answer; the rest are other emulators attached to the same tmux session.
	for _, c := range watching {
		tty := bareTTY(c.tty)
		if tty == "" {
			continue
		}
		if err := itermRevealTTY(ctx, tty, activate); err == nil {
			return
		}
	}
}

// revealMirroredPane is the `tmux -CC` half: the tab is named by the pane.
//
// An application that will not come forward is worth no failure of its own:
// this is a courtesy on a background goroutine and its outcome is discarded
// either way. The fallback is deliberate — a window in front of the wrong tab
// beats nothing happening at all, and it is only reached when iTerm2 says it is
// drawing no tab for this pane.
func revealMirroredPane(ctx context.Context, paneNumber string, mirrors []attachedClient, activate bool) {
	if !activate {
		// `revealtmux` names a row iTerm2 itself says is drawing this pane,
		// which is a stronger statement than the client list can make, so the
		// second lock is not needed where nothing is raised.
		_ = itermRevealTmuxPane(ctx, paneNumber, false)
		return
	}
	ttys, listed := itermListedRowTTYs(ctx)
	if !listed {
		return
	}
	if len(drawnByITerm2(mirrors, ttys)) == 0 {
		return
	}
	if err := itermRevealTmuxPane(ctx, paneNumber, true); err != nil {
		_ = itermActivate(ctx)
	}
}

// drawnByITerm2 keeps the clients whose tty is one iTerm2 says it is holding.
// The gateway is an ordinary iTerm2 session with a real tty and it is in the
// very list being attributed, which is what makes this answerable.
//
// An empty `#{client_tty}` is no match rather than a wildcard: a client
// attached through a fifo has no tty at all, and an empty string matching
// everything is the shape that makes a guard say yes to what it was written to
// refuse.
func drawnByITerm2(clients []attachedClient, rowTTYs map[string]bool) []attachedClient {
	out := make([]attachedClient, 0, len(clients))
	for _, c := range clients {
		tty := bareTTY(c.tty)
		if tty == "" || !rowTTYs[tty] {
			continue
		}
		out = append(out, c)
	}
	return out
}

// bareTTY is a pty as both sides spell it once the directory is off:
// `/dev/ttys047` and `ttys047` are the same terminal, and tmux gives the first
// while iTerm2 is asked for the second.
func bareTTY(tty string) string {
	return strings.TrimPrefix(strings.TrimSpace(tty), "/dev/")
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
