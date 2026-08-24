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

- `~/.config/clawdline/hook.sh` — a small `sh` bridge, copied out of the app so that
  moving or rebuilding the app cannot break the path.
- Nine matcher groups in `~/.claude/settings.json`, under eight event names.

Everything else in that file is read, changed and written back. A copy of it is kept once, as
`~/.claude/settings.json.before-clawdline`, so "what did it do to my settings" is a question you
can answer with `diff` rather than with a promise.

```jsonc
{
  "hooks": {
    "PreToolUse": [
      { "matcher": "AskUserQuestion",
        "hooks": [ { "type": "command",
                     "command": "'/Users/you/.config/clawdline/hook.sh' PreToolUse ask_user_question",
                     "timeout": 5 } ] }
    ],
    "Notification": [
      { "matcher": "permission_prompt", "hooks": [ /* the same bridge */ ] },
      { "matcher": "idle_prompt",       "hooks": [ /* the same bridge */ ] }
    ]
  }
}
```

To install: **Settings → Claude Code hooks → Install**, or `open "clawdline://hooks?install=1"`
from a script. `install=0` takes them back out, and taking them out leaves nothing behind —
Clawdline goes back to reading the screen, which is what it does when they were never there.

---

## The events and their matchers

| Event | Matcher | What Clawdline does with it |
|---|---|---|
| `SessionStart` | — | Learns the session id, which is also the transcript filename. |
| `UserPromptSubmit` | — | **Looks now, and again in 2.5 seconds.** Claims nothing. |
| `Stop` | — | Marks it not working if a stale spinner remains. |
| `PreToolUse` | `AskUserQuestion` | Authoritatively marks it waiting and keeps the full questions and options from `tool_input`. |
| `PostToolUse` | `AskUserQuestion` | Retracts that waiting state after the answer. |
| `PermissionRequest` | — | Authoritatively marks it waiting for approval. |
| `Notification` | `permission_prompt` | Marks it waiting for approval. |
| `Notification` | `idle_prompt` | **Looks now. Claims nothing.** |
| `SessionEnd` | — | Same as `Stop`, and forgets the tty remembered for the session. |

These are still rare. `PreToolUse` and `PostToolUse` without a matcher would run on every tool
call; `matcher: "AskUserQuestion"` means the bridge runs only for the handful of calls whose
input is an actual question. That input contains the complete `questions` array, including the
labels the terminal may clip to its current width.

`Notification` is also matched rather than handled as one ambiguous event. Claude Code matches
that group against the notification type, so `permission_prompt` and `idle_prompt` arrive as
different meanings. A minute of quiet is never turned into “a question is waiting.” If an idle
notification arrives while an AskUserQuestion note is still open, the script leaves the question
note intact.

**`SubagentStop` is missing because it would be wrong.** A subagent finishing is not the session
finishing, and treating it as one would call a session idle while its main agent was still going.

---

## What is authoritative, and what remains a fallback

Most notes still say only *when* something happened. A prompt submission or an idle notification
asks Clawdline to read the screen; it does not claim what that reading will find.

Two matched lifecycle events are stronger than a drawing:

- `PreToolUse/AskUserQuestion` is Claude Code stating that its question tool is about to run. Its
  `tool_input` is the full question, not an inference. `PostToolUse/AskUserQuestion` closes it.
- `PermissionRequest` is emitted when the permission dialog is about to be shown. The matched
  `Notification/permission_prompt` is the notification equivalent.

Those notes may assert `waiting`. This is the necessary boundary: Claude Code can draw an
AskUserQuestion picker with its caret at column zero, exactly the same shape as a user's echoed
message that begins with a numbered list. No screen-only rule can accept the first without also
accepting the second. The structured hook has information the pixels do not.

The screen is still the complete fallback. Sessions started before installation, disabled hooks,
missing notes, old notes without `tool_input`, and clipped or unreadable hook data all take the
same screen path as before. When both exist, structured hook options win so phone buttons carry
the original labels rather than terminal-width truncations.

What the notes do settle on their own are the things a screen genuinely cannot:

- **A question is waiting or has just been answered.** The matched tool events bracket it and
  carry its content.
- **Approval is waiting.** PermissionRequest says so directly; `idle_prompt` never does.
- **A turn has ended.** Claude Code does not always erase its live line when a fast turn
  finishes, so a capture taken a moment later can find one and call a finished session busy. A
  `Stop` overrides that — for ten seconds, after which the screen wins again.

**Nothing here ever claims that a session is working**, which is a
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

A question recognised on screen still outranks every look-only note. An explicit
AskUserQuestion opening or closing event outranks a stale screen because it is the lifecycle
being drawn, not a guess about that drawing.

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
{"event":"Stop","kind":"stop","tty":"ttys004","at":1787040501,"session":"a2937509-…"}
```

One file per session, overwritten in place, never appended. AskUserQuestion notes may also carry
`tool_input`; that field is accepted only up to 32 KiB, so a surprising tool payload cannot grow
an unbounded file in the hooks directory. An oversized input is omitted whole and the screen
remains the fallback.

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

The unfiltered lifecycle events run a few times per turn. The two tool events run only when their
`AskUserQuestion` matcher succeeds.

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
