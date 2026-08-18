# Hooks: letting Claude Code speak first

Clawdline works by looking. It reads the screen a session is drawing — the spinner line, the
shape of a menu waiting for an answer — and that is deliberately the whole mechanism: it works on
sessions you started before you installed this, it needs nothing added to anybody's
configuration, and it cannot break what it is reading.

Looking has one cost it cannot avoid. **It only knows what it has looked at.** With the bar on
screen that is every 1.2 seconds. With the bar away it is every twenty, because a reading is a
round trip to every terminal you have open and that is a thing worth doing rarely when nothing
has happened.

Twenty seconds is a long time to leave a permission dialog sitting there.

Claude Code will tell you instead, if you ask it to. This page is what that costs and what it
buys.

---

## What it buys

Measured on a machine with three sessions open, the bar closed:

| | without hooks | with hooks |
|---|---|---|
| A permission dialog appears → the menu bar says so | up to 20s | **under a second** |
| A long job finishes → the mascot dances | up to 20s | **under a second** |
| You press Return → the row says "working" | up to 20s | **~2.5s** |
| ⌘J finds the right transcript | matched by title and start time | **named outright** |
| Terminal round trips per minute, idle | 3 | 3 |

The last row is the point of the design. **The polling does not change.** A hook does not add a
reading; it moves one that was going to happen anyway to the moment it was worth taking.

---

## What it installs

Two things, both of which you can read:

- `~/.config/clawdline/hook.sh` — about sixty lines of `sh`, copied out of the app so that
  moving or rebuilding the app cannot break the path.
- Five entries in `~/.claude/settings.json`, under `hooks`.

Everything else in that file is read, changed and written back. A copy of it is kept once, as
`~/.claude/settings.json.before-clawdline`, so "what did it do to my settings" is a question you
can answer with `diff` rather than with a promise.

```jsonc
{
  "hooks": {
    "Stop": [
      { "hooks": [ { "type": "command",
                     "command": "'/Users/you/.config/clawdline/hook.sh' Stop",
                     "timeout": 5 } ] }
    ]
    // …and the same for SessionStart, UserPromptSubmit, Notification, SessionEnd
  }
}
```

To install: **Settings → Claude Code hooks → Install**, or `open "clawdline://hooks?install=1"`
from a script. `install=0` takes them back out, and taking them out leaves nothing behind —
Clawdline goes back to reading the screen, which is what it does when they were never there.

---

## The five events, and the five that are missing

| Event | What Clawdline does with it |
|---|---|
| `SessionStart` | Learns the session's id, which is also the name of its transcript file. |
| `UserPromptSubmit` | **Looks now, and looks again in 2.5 seconds.** Claims nothing. |
| `Stop` | Marks it not working, even if the spinner line was left on the screen. |
| `Notification` | **Looks now.** Nothing else — see below. |
| `SessionEnd` | Same as `Stop`, and forgets what it remembered about the session. |

Five, all of them rare, and that is the design rather than an oversight.

**`PreToolUse` and `PostToolUse` are missing on purpose.** They fire hundreds of times an hour and
would put this script on the critical path of every tool call your agent makes — to say something
`UserPromptSubmit` and `Stop` already bracket between them. There is no state in the middle of a
turn that those two do not already cover.

**`SubagentStop` is missing because it would be wrong.** A subagent finishing is not the session
finishing, and treating it as one would call a session idle while its main agent was still going.

---

## Why the screen is still the authority

A note says *when* something happened. It never says what is on the screen, and the difference
matters most for the one state this whole feature exists for.

`Notification` fires when Claude Code asks for permission. It also fires when a session has
merely been quiet for a minute. Those are the same event and they are not remotely the same
thing to somebody reading the list — one of them costs you every second it goes unnoticed and
the other costs nothing at all. Nothing in the payload separates them reliably.

So `Notification` does not set a state. It asks for a reading, the reading happens about forty
milliseconds later, and `SessionState` decides — from the shape on the screen, the way it always
has. You get the speed of being told and the accuracy of looking.

What the notes do settle on their own is the pair of things a screen genuinely cannot:

- **A turn has ended.** Claude Code does not always erase its live line when a fast turn
  finishes, so a capture taken a moment later can find one and call a finished session busy. A
  `Stop` overrides that — for ten seconds, after which the screen wins again.

And that is the only one. **Nothing here ever claims that a session is working**, which is a
narrowing that came out of measuring rather than out of caution:

| measured on a real session | |
|---|---|
| Live line appears after Return | **~2.1s** |
| It was on screen for | **0.64s**, then gone while the answer streamed |

So a claim short enough to be safe would cover almost none of a turn, and one long enough to
cover a turn could not be retracted — **pressing Esc to cancel fires no hook at all**. Herdr wired
Claude Code's hooks into its state, shipped the long version, and had to take it out again; their
issue #249 is this exact bug, and their `HOOK_REMOVALS` list is the tombstone.

What replaces it is cheaper and cannot be wrong: **a nudge looks twice** — once immediately, and
again 2.5 seconds later, by which time the live line is up. Same information, nothing asserted.

A question on the screen outranks every note there is.

---

## How a note finds its session

Claude Code tells a hook the session id, the transcript path and the working directory. It does
not tell it which terminal tab you are looking at, and that is the thing Clawdline needs.

The script works it out from the process tree. Claude Code starts its hooks in a session of their
own with no controlling terminal — `ps` says `??` for the hook itself — so the script walks up
its parents until it finds a process that has one. That process is Claude Code, and the tty it is
sitting on is the same string iTerm2 reports for its tab and tmux reports for its pane. It is the
one name both ends already agree on, which is why the note is filed under it:

```
~/.config/clawdline/hooks/ttys004.json
{"event":"Stop","tty":"ttys004","at":1787040501,"session":"a2937509-…"}
```

One file per session, overwritten in place, never appended. Nothing queues, so an app that was
not running has missed nothing it could still have acted on.

If no tty can be found, the script writes nothing and exits. A note nobody can match to a session
on screen is worse than no note: it would make the wiring look like it was working while telling
you nothing. Screen reading covers that case, as it covers every case.

---

## Two rules the script follows

Both are about never making Claude Code worse than it was without this.

- **Nothing on stdout, ever.** A hook's stdout is read back as instructions, so a stray `echo` in
  there is a sentence typed into your session.
- **Always exit 0.** A non-zero exit from a hook is a decision about the work in progress — `2`
  blocks it outright. No failure inside this is worth stopping your turn over.

It costs about eleven milliseconds, five times a turn.

---

## If something looks wrong

The settings window says which of three states you are in, and the third is the one that matters:

- **Not installed** — every reading comes from the screen.
- **Installed** — the entries are in `settings.json`, but no session has ever reported.
- **Installed, and sessions are reporting** — a note has arrived within the day.

Sitting on the middle one means the wiring is written down but nothing is running it. Usually
that is a Claude Code that has not been restarted since you pressed Install; occasionally it is a
`settings.json` that something else manages and rewrites.

To rule the notes out of a reading without touching anybody's settings file, put `"hooks": false`
in `~/.config/clawdline/config.json`. The entries stay where they are and Clawdline stops
believing them.
