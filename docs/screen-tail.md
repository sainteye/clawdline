# Reading a session's words off its screen: a road tried, measured, and closed

**Status: removed on 2026-09-02.** The code is gone; this page is what it cost to find out, kept
because the next person to notice the underlying problem will have the same idea, and because the
numbers in it are the argument for the road that replaced it.

## The problem, which has not gone away

Claude Code appends an assistant message to its `.jsonl` as soon as the message is complete — a
`Bash` call that ran for twenty-one seconds had its `tool_use` record on disk six seconds in.
`AskUserQuestion` is the exception: the whole turn that asks a question — the thinking, **the
prose explaining the choice**, and the call itself — is written only after somebody answers. Every
one of twelve questions sampled had that shape: `thinking`, `text` and `tool_use` share one
`msg_…` id and land together once the answer is in.

Measured 2026-09-01 on session `e54c45b7`: the Mac's screen held a full analysis and a four-option
picker at 13:28:38 while the transcript file still ended at 13:27:17, and it still did ten minutes
later. The question card reaches a phone by another road — a hook note and the screen parse in
`SessionState` — so the reader had the question on time and none of the reasoning that made it
answerable. **A question you cannot see the reasoning for is a question you cannot answer.**

## What was built, and what it measured

Successive screen captures overlap, so they can be reconciled back into the document they are a
window onto. That part worked, and worked well:

| | result |
| --- | --- |
| 110 consecutive captures of a live session | every frame placed, **0 gaps** |
| lines lost against what any capture had shown | **0** |
| duplicated long lines, once the alignment was right | **0** (80 before) |

The reconciliation is not the reason this was removed. Everything else is.

## Every wall it hit

- **iTerm2 exposes the visible screen and no more.** `text` and `contents` are the same sixty
  rows; there is no scrollback API. Anything printed before the app started capturing is
  unrecoverable, so a reader opening a session that finished a long answer an hour ago sees its
  last screenful and no more. **This is the wall the whole approach dies on**, and it is the one
  tmux does not have (`Tmux.capture(scrollback:)`).
- **A terminal is not an append-only log.** Claude Code rewrites a running tool's row once a
  second (`…(3s · 3 lines)`), and an appending reconciliation stacked one copy per second — forty
  near-identical lines reached a reader. Fixed twice over: align with the numbers removed, and
  place each frame by where it *starts* rather than where its matched run ends.
- **The wrap is a guess.** A hard-wrapped paragraph has to be rejoined, and the only signal is
  whether a line ran to the width the screen was drawn at (counting CJK as two columns). Without
  it a list of five files arrives as one sentence; with it, a paragraph that happens to fill the
  width exactly still joins to the next.
- **A transcript entry is Markdown; a screen is what Markdown renders to.** Suppressing what the
  file already held needed a comparison that ignores backticks, asterisks and brackets — one pair
  of backticks put a whole answer on the page a second time.
- **Not everything on screen is speech.** Claude Code draws prose at two columns and a command's
  arguments and output at five; without that test a commit message typed into a heredoc came back
  as though the assistant had said it. Its opening banner and the release slogan under it did the
  same on a session that had said nothing at all.
- **And the walk had to stop at the tools.** Once a tool's own row scrolls off the top, a reading
  that steps over the leftover output keeps going through everything above it and returns whatever
  sits at the start of the document — which is how a reader ended up looking at the startup banner
  underneath four turns of conversation, while the screen was updating perfectly well the whole
  time.

Each of those was found by a person looking at a phone, not by a test. That is the real signal:
**the shapes on a terminal are a moving target with no contract**, and every release can add
another one. A rule that reads them is a rule that is one release away from being silently wrong.

## What replaces it

tmux. `Tmux.capture(scrollback:)` answers with history rather than with a window, which removes
the wall this road died on: nothing has to be reconstructed from overlapping captures, so the
redraw and alignment rules stop needing to exist.

**It does not remove the rest, and that is worth saying plainly.** Scrollback answers *can I get
the text*. It says nothing about *which of these lines is somebody speaking*. A tmux history still
holds the startup banner and its slogan, tool arguments drawn at five columns, the picker's own
question, and Markdown rendered rather than written — so the last four walls above are still
walls, and they are precisely the ones that were found by a person looking at a phone rather than
by a test. What changes is that they can be met with the whole conversation in hand instead of
sixty rows of it.

## What was kept

`richText` in `Resources/web/app/js/view/markdown.js` learned Markdown's hard break — two trailing
spaces. It was written for this feature, because a screen reading has only the terminal's own line
breaks to work with, but it is ordinary correct Markdown and worth having on its own.
