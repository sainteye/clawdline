# The screen and the file: what a reader sees while a question waits

Clawdline reads a session back from Claude Code's own transcript file rather than from a picture
of a terminal. That is the right default and it is most of why the pane has real message
boundaries, tables and folded tool runs at all. This page is about the one hour a day where the
file is the wrong source, what is done about it, and what that fix does not claim.

## The hole

Claude Code appends an assistant message to its `.jsonl` as soon as the message is complete. That
is not a guess: a `Bash` call that ran for twenty-one seconds had its `tool_use` record on disk
six seconds in, while the command was still running.

`AskUserQuestion` is the exception. The whole turn that asks a question — the thinking, **the
prose explaining the choice**, and the call itself — is written only after the person has
answered. Every one of the last twelve questions in this project's transcripts has that shape:
`thinking`, `text` and `tool_use` share one `msg_…` id, are written as three consecutive records,
and all three land together once the answer is in.

Measured on 2026-09-01, session `e54c45b7`:

| | time | what it held |
| --- | --- | --- |
| the Mac's screen | 13:28:38 | a full analysis — five files over the size guardrail, a correction to a stale document, a timing warning — and a four-option picker |
| the transcript file | stopped at 13:27:17 | the seventeenth `Bash` result, and nothing after it |
| a phone, at 13:29 | — | the seventeen tool calls, then the question card |

Ten minutes later the file had still not moved. The reader had a question in front of them and
none of the reasoning that made it answerable.

**The question card arrives by a different road**, which is why this reads as a hole rather than
as ordinary lag: a hook note and the screen parse in `SessionState` produce the card immediately,
while the sentences behind it wait on a file that is deliberately not being written. "Answer it
and then you can read why" is not an instruction anybody can follow.

## What is done about it

`Sources/ScreenTail.swift` reconstructs the missing words from the captures the app is already
taking, and `RemoteServer.unsyncedRow` offers them as one more entry — only while the session is
stopped on a question, and only until the file catches up.

**Sampling is mostly free.** `Targets.reading(of:hookWaiting:)` already fetches one screen per
session per beat — 1.2 s while the Mac's panel is open, 20 s otherwise — in a single Apple event
for all of them. `ScreenTail.observe` is handed the capture that has already been paid for.
Captures are taken on **every** beat, not only for waiting sessions: by the time a session is
known to be waiting, the sentences explaining the question have already scrolled past, and only
the captures taken while they were being written still hold them.

**But 20 s is not a cadence you can reconstruct from**, and a phone reading from another room
does not open the Mac's panel. Overlap is the whole mechanism; twenty seconds of a streaming
answer is several screens, consecutive captures share no lines, and the reconstruction correctly
refuses to guess across the break — so the reader gets the end of an answer with its beginning
missing. `Sources/ScreenFollow.swift` closes that: **one extra capture per second for the session
whose transcript is actually being read**, and nothing at all when nobody is reading. Attention
is taken from the transcript request itself, so there is no new client contract and a closed page
stops costing anything within 25 seconds. The fleet-wide rate is untouched, which is the point —
a rate rise for everybody would pay for nine screens nobody is looking at in order to fix the one
somebody is.

**One capture is not enough, and that does not stop it.** iTerm2 exposes the visible screen and
no more — `text` and `contents` are the same sixty rows, and there is no scrollback API. But
prose arrives by streaming, over tens of seconds, so successive captures overlap, and overlapping
captures can be reconciled back into the document they are a window onto.

**The live rows are the clock, not the conversation.** `Running 1 shell command · 3s…`,
`✻ Thinking…`, the token counter, and a running tool's own preview row `…(3s · 3 lines)` — these
are redrawn in place several times a second, and with them in, consecutive captures never match:
measured, 58 of 59 frames failed to align. Dropping them is what makes the rest of the screen
append-only, which is the property the whole reconciliation rests on.

**And a redraw that gets through is still not new text.** Two defences, because the first one is
a list of shapes and lists are never complete. Alignment falls back to comparing the two captures
with every number replaced, so a row whose only change was its counter lines up with the row it
replaced. And the frame is placed by **where it starts**, not where its matched run ends —
appending from the end of the match re-appends exactly the rewritten rows the match stepped over.
Missing that put forty near-identical copies of one line in front of a reader.

Measured over sixty consecutive captures of a live session:

| strategy | frames that failed to align | lines lost | duplicated long lines |
| --- | --- | --- | --- |
| append what is new after the overlap | **0** | **0** | **0** |
| let each capture overwrite the span it covers | 0 | 16 | 5 |

(The appended reading's duplicates were 80 before the frame was placed by its origin rather than
by the end of its matched run. Same 110 captures, same everything else.)

Both place every frame. They differ in what they are *for*. Overwrite mirrors the screen exactly,
including Claude Code folding a finished tool block down to `Read 1 file, ran 2 shell commands` —
which is why sixteen lines that had genuinely been on screen disappear from it. **Prose is never
folded**, so for the one thing this is for, appending keeps everything and loses nothing. The
duplicated lines are all tool furniture, and `trailingProse` stops at the first tool row anyway.

**It will not guess across a gap.** When a capture cannot be placed against the document — the
sampler was on the 20-second cadence, or the output arrived faster than the beat — the break is
recorded and prose is only ever taken from after the last break. Text spliced at the wrong point
reads as perfectly ordinary and is wrong, which is worse than text that stops early.

**It steps aside, paragraph by paragraph.** Suppressing the whole row was wrong in both
directions: it said nothing when the file held only the first half of what the screen showed, and
it said everything twice when the comparison missed — which it did on the first real screen it
met, because **a transcript entry is Markdown and a screen is what Markdown renders to**. A pair
of backticks was enough to put a whole answer on the page a second time. So the comparison keeps
only letters and digits, and it runs per paragraph: what the file already has is dropped, what is
left is what the reader is missing.

On the wire the row carries `provisional: true`. The web pane dims it and marks its edge rather
than labelling it: the reader wants the sentences, not a lecture about where they came from.

## What it does not claim

- **It is a reading of a screen.** Hard-wrapped at the terminal's width and reassembled by
  heuristic. Two rules do the work, and both were learned from a real screen rather than a
  fixture. **A line is joined to the next only when it ran to the edge** — the width the screen
  was drawn at, measured as the widest line on it, counting CJK as two columns. Without that
  test a list of five files came back as one sentence. And **the seam gets a space only between
  two ASCII word characters**, because a wrap between two English words ate the space that was
  there and a wrap between two CJK characters ate nothing.
- **A table survives as its own lines.** Box-drawing rows are stepped over rather than treated as
  a boundary. They were briefly treated as the picker's furniture, and against the first real
  waiting screen that threw away the entire analysis above the table — nothing was offered at
  all. Only the question header and `❯` mean picker now.
- **It starts when you start watching.** No scrollback API means nothing before the first capture
  can ever be recovered. The division is clean and worth stating plainly: **the file is the
  history, the screen is the present.**
- **iTerm2 only.** tmux panes are captured the same way and do have scrollback
  (`Tmux.capture(scrollback:)`, currently asked for the visible pane only). Ghostty cannot be read
  at all, so none of this exists there.
- **Only what the screen is actually ahead on.** The row is offered whenever the screen holds
  words the file does not — a question's turn is the extreme case, unwritten until it is
  answered, but a long answer being typed out is the same shape. It is never a second copy: any
  parsed entry already containing those words suppresses it.

## Where this goes next

The same reconciliation is the first piece of a live mirror — a phone showing what the Mac's
screen shows, at the sampling cadence, with no transcript file involved. Three things stand
between here and there, and none of them is the algorithm:

1. **Cadence has to follow attention.** ~~Not solved.~~ `ScreenFollow` does this for one
   session at a time, keyed on who is reading a transcript. A mirror wants the same idea with a
   tighter beat and a second reading — the overwrite one — alongside the appended transcript.
2. **The two readings are different products.** Overwrite is the honest mirror; append is the
   honest transcript. A live view wants the first with the second sedimenting behind it.
3. **Nothing pushes it.** Today the provisional row changes only when a client asks again. See
   `B-SCREEN-TAIL-IS-NOT-PUSHED` in [`backlog.yaml`](backlog.yaml).
