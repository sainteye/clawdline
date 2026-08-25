# A conversation that moved to the background

Written for whoever touches `SessionRegistry`, `Transcript` or the picker parser and wonders why
there is suddenly a second file involved in answering "what is this tab doing".

A Claude Code session can stop being the thing its terminal tab is running and become something
the tab is only *watching*. When that happens the tab keeps its process, keeps its pid, keeps its
window — and stops writing the file this app had been reading it from. Nothing announces it. The
tab looks exactly the same.

Everything below was measured on a real machine. Where something was not measured it says so,
because three claims in this investigation were true by reasoning and false by experiment, and all
three would have shipped as code.

## What it looks like when it goes wrong

Two symptoms, both measured on the same session on 25 Aug 2026:

- **The transcript stops mid-sentence.** The phone showed a conversation whose last message was
  timed 17:05:29 while the terminal was at 17:31 and still going. `/v1/sessions/:id/transcript`
  returned exactly the four entries visible at the bottom of the phone, twenty-six minutes stale.
- **A finished session never goes quiet.** At 17:45 the live session had been sitting at an empty
  prompt for three minutes and its own file said `idle`; the tab's file still said `busy`, and
  every surface in the app — menu bar, notch, panel, phone — said `working`.

The second one is the expensive one. A session that finishes and never says so is the signal
somebody uses to decide whether to walk back to the computer.

## What is actually happening

**Pressing <kbd>←</kbd> to look at the background sessions is what moves the conversation you were
in to the background.** That is not a guess: open that list on any session and Claude Code writes
the reason across the middle of it —

```
Your conversation moved to the background — enter opens it · esc returns to it · ctrl+c twice quits
```

So this is not a rare state reached by an unusual keystroke. It is what happens whenever somebody
glances at their agents.

From that moment:

- The tab's `~/.claude/sessions/<pid>.json` gains a `parkedJobId` and **is never written again**.
  Its `sessionId` stays at the conversation that has moved on; its `status` freezes at whatever it
  was at that instant. Measured: one file unchanged byte-for-byte across forty minutes while the
  conversation it names changed state three times.
- The conversation carries on in a session whose file says `kind: "bg"` and whose `jobId` equals
  that `parkedJobId`. It has its own pid, its own `sessionId`, its own transcript, and a `status`
  that is kept current.
- The tab still shows that conversation, and typing into the tab still reaches it — confirmed by a
  `UserPromptSubmit` hook note carrying the *background* session's id from the parked tab's tty.

One detail worth having before debugging anything here: a background session writes its file only
when its status *class* changes. Fifteen samples over 140 seconds produced no write at all while
it was busy throughout. A file that has not moved in twenty minutes is not evidence of a stuck
session.

## Why nothing noticed

`entry(for:in:)` has three gates — the process is alive, its start time matches the file, the file
states the protocol version this build knows. **A parked tab passes all three.** The process really
is alive, it really is the process the file is about, and the file really is well-formed. It is
simply describing a conversation that left.

So the failure had no shape to catch. It was a valid file, fully trusted, permanently wrong.

## What it does now

`Entry` reads three more fields — `kind`, `jobId`, `parkedJobId` — and an entry carrying a
`parkedJobId` is replaced by the `kind: "bg"` entry whose `jobId` matches it, held to the same
process check as everything else here.

**When that live entry cannot be found, this answers with nothing rather than with the frozen
file.** That is the point of the change and not a fallback. Nothing loses to the hook note and to
the tab title, and both of those are about the conversation that is running: the hook note carries
the background session's id, and the live transcript carries the tab's own title while the frozen
one carries a title from before the rename. A stopped clock beats both and cannot be argued with.

## Finding the live file, and the two paths that were rejected

**Walking the process tree does not work.** The daemon is a single per-user process shared by
every session that needs one — one daemon was observed serving three background sessions from
three different origins — and a background session hangs off a `bg-pty-host` under it rather than
off the daemon itself. From a tab you would have to walk three layers to a process that may belong
to somebody else's tab entirely.

**`claude agents --json` does list them**, with `id` (the job), `sessionId`, `kind` and `status`,
and it is the official way to ask. Two things ruled it out. It costs 210–630 ms per call against
0.426 ms for reading the directory — and this path is walked every 1.2 seconds — and it does not
print `parkedJobId`, so the tab-to-job mapping still has to come from the tab's own file.

**So the directory is read by key.** With `parkedJobId` in hand there is nothing to guess at: the
one file whose `jobId` matches is the answer, and when no such file exists the answer is nothing.
That is a different act from the listing this file has always argued against, which was guessing
*which* file belongs to a session by title and timestamp.

## Three claims that came from reasoning and did not survive

Kept here because the pattern repeated three times in one afternoon, from three different readers,
and each one would have become code.

1. **"`merge`'s `.idle` case is missing a guard the `.busy` case has."** The shapes really are
   asymmetric. But the comment on the `.busy` line says what it is for — keeping the live line's
   own words rather than replacing them with an empty string — and the `.idle` case says in its
   own comment that it is the stale-spinner fix. Copying the guard across would have deleted a
   working feature and turned a test that pins it. The asymmetry is the design.

2. **"The list of background sessions will be read as a picker, and the phone will show buttons
   whose real action is `esc to cancel`."** It is a flush-left list with a marker on every row and
   a composer underneath, so the shape argument was reasonable. It is also wrong: `option(_:)`
   counts a row only if it begins with digits, a full stop and a space, and no row on that screen
   carries a number. Measured by capturing the real screen from a throwaway session and putting it
   through the parser three ways — gate shut, gate open, and a window wide enough to reach every
   row. Nothing came back a menu. The capture is now a test.

3. **"`parkedJobId` is never cleared, so a tab is bound to a dead job forever."** Two reviews rested
   a blocking finding on this. It came from one observation that could not distinguish "never
   cleared" from "nobody unparked it". Claude Code ships a function that writes `parkedJobId:
   undefined`, so a clearing path exists; which flow calls it is still unknown.

## What is still open

- **An answer typed on a phone goes to the tab, and the background session has no tty.** While the
  tab is showing the conversation this works, and it was working before this change too — the same
  signal reached the phone through the hook note. While the tab is showing the list of background
  sessions, the composer under that list says *describe a task for a new session*, so a message
  sent from a phone would start one instead of answering anything. Routing input to the background
  session itself means talking to its `messagingSocketPath`, which is undocumented and may not
  accept keystrokes at all.
- **Unpark is not covered.** No experiment reproduced a return to the foreground, so nothing here
  knows what the tab should do when the job it is following ends. The safe direction is already
  taken — an entry that cannot be found produces nothing — but there is no test for the case where
  it is found and stale.
- **Two files claiming the same job**, a zombie process passing `kill(pid, 0)`, and a negative
  `ps` result cached for twenty seconds are all reachable and all unhandled. They are recorded
  rather than fixed because none of them was observed.

## Where the evidence lives

`fix: a conversation that moved to the background left its tab reading a dead file` carries the
change and the tests that pin it, including the one that fails if a frozen file is ever preferred
to nothing. `test: the list of background sessions is nearly a dialog, and this says it is not
one` carries the captured screen. [Versions](compatibility.md) records the three fields this now
depends on, because a Claude Code that never parks writes none of them and none of this applies to
it.
