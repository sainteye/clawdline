package orchestrator

import "testing"

// The screen that cost a child its life, captured from the pane it happened in
// (tmux capture-pane, 2026-09-18). The broker saw an assistant on a tty, typed
// the briefing, and the Return at the end of it answered this dialog — which
// was highlighting "No, exit".
const trustDialog = `G_TOKEN -u CLAUDE_CODE_BRIDGE_SESSION_ID -u CLAUDE_EFFORT -u AI_AGENT claude --a
dd-dir '/tmp/.clawdline/0ca0b8c0/work/next-dir/tasks' --permission-mode bypassPermissions

────────────────────────────────────────────────────────────────────────────────
 Accessing workspace:

 /private/tmp/.clawdline/0ca0b8c0/work/next-dir/worktrees/demo-205be400/428264f6

 Quick safety check: Is this a project you created or one you trust?

 Claude Code'll be able to read, edit, and execute files here.

 Security guide

 ❯ No, exit
   Yes, I trust this folder

 Enter to confirm · Esc to cancel`

// The composer of a running Claude session, captured the same way. The caret is
// the same glyph; what is different is the frame drawn under it.
const claudeComposer = `  ctrl+x ctrl+s to send now

────────────────────────────────────────────────────────────────────────────────
❯ Press up to edit queued messages
────────────────────────────────────────────────────────────────────────────────
  ▀ ▄ ▀ 943eb744-2cb4-43ef-8431-a240471526ef
  ▀▀▀▀▀ ~/Library/Application Support/Clawdline/worktrees/clawdline-…  ctx 23%`

func TestADialogIsNeverTypedInto(t *testing.T) {
	if ComposerReady(trustDialog) {
		t.Error("the workspace-trust dialog was read as a composer; the briefing would answer it")
	}
	if !Choosing(trustDialog) {
		t.Error("the workspace-trust dialog was not recognised as something waiting to be answered")
	}
}

func TestAFramedCaretIsAComposer(t *testing.T) {
	if !ComposerReady(claudeComposer) {
		t.Error("a drawn composer was not recognised, so no child would ever be briefed")
	}
	if Choosing(claudeComposer) {
		t.Error("a drawn composer was read as a chooser")
	}
}

// A screen with no caret at all is a session still starting up. Neither answer
// may be yes: typing at it drops the bytes, and calling it a chooser would stop
// the briefing that is about to become possible.
func TestAStartingSessionIsNeitherReadyNorChoosing(t *testing.T) {
	screen := "Loading…\n\n  ▀ ▄ ▀ starting"
	if ComposerReady(screen) {
		t.Error("a starting session was read as ready")
	}
	if Choosing(screen) {
		t.Error("a starting session was read as a chooser")
	}
}

// Four minutes of silence from a child.
//
// The first version of this clock read silence as failure, and task
// 69c21384 — a child that had been briefed, was alive, and had simply been
// told not to send heartbeat notes — was recorded as `spawn_failed` while its
// tab sat there working. Silence is not evidence; the tab is.
func TestSilenceFromALiveChildIsNotASpawnFailure(t *testing.T) {
	for _, c := range []struct {
		name            string
		alive, choosing bool
		want            State
		decided         bool
	}{
		{"the tab is gone", false, false, StateSpawnFailed, true},
		{"the tab is holding a dialog", true, true, StateSpawnFailed, true},
		{"the tab is alive and quiet", true, false, "", false},
	} {
		state, why, decided := spawnVerdict(c.alive, c.choosing)
		if decided != c.decided || state != c.want {
			t.Errorf("%s: got %q/%v, want %q/%v", c.name, state, decided, c.want, c.decided)
		}
		if decided && why == "" {
			t.Errorf("%s: settled a task without saying why", c.name)
		}
	}
}
