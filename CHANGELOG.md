# Changelog

Release notes have lived on [the releases page](https://github.com/sainteye/clawdline/releases)
since 0.1.0. This file is where the next one is written before it gets there, and where the
older ones can be found from without leaving the repository.

The entries are prose rather than a list of commits. What belongs in one is **what changed for
somebody using this** — a commit log already exists and is better at being a commit log.

## Unreleased

### Fixed: every reading could stop, minutes after the app started

The change that stopped a subprocess wait from running the app underneath itself did it by waiting
on a thread borrowed from the global pool — which is where the caller usually already is. That is a
deadlock as soon as the pool is full: the waiter holds a thread the block it is waiting for needs.
The one place that reads every terminal runs on that pool and shells out from inside it, so once
it happened nothing was read again. Sessions kept whatever state they were last seen in, a phone or
a browser was sent a snapshot on connect and then nothing at all, and a tab opened or closed after
that never appeared or disappeared.

The wait now happens on a thread of its own, which the pool cannot starve, and the test fills the
pool before asking for one so that it fails if this is ever written that way again.

### Added: a session with a command still running no longer reads as finished

`Bash` with `run_in_background` starts something that outlives the turn that started it — a build,
a dev server, a test suite. Claude Code says so once, on the line where the turn ends: *Cooked for
1h 25m 13s · 1 shell still running*. Then it draws an ordinary prompt and says nothing more about
it, for however long the command takes.

Everything in this app reads that prompt as what it looks like. The session list drew the row with
nothing after its title, the phone drew it the same way, and the fleet count called it quiet — so
the one session that still had work in flight was indistinguishable from the four that were done.
That is the wrong answer in the direction that costs something: you close the laptop on a build.

The row now says `1 shell running`, on the Mac and on the phone, the header can no longer call a
fleet "all quiet" over a build, and the strip above the transcript lists the commands next to the
background agents with the last line each one printed. It is worked out from what Claude Code
already writes down — the output file a command prints into, which gets `[exited with code 0]` under
it when a background one ends, and the line in the transcript that says an id was backgrounded in
the first place. Both are needed: a foreground command that somebody interrupted leaves its file
behind looking exactly like a build still going, which is how the first version of this spent an
afternoon reporting a cancelled `curl` as work in flight. Nothing has to be installed and nothing
has to be restarted. `GET /v1/sessions` carries it as `shells`; see
[`docs/api.md`](docs/api.md#the-session-object).

**And the rows open.** A background command has no conversation to read — it was given its words
when it was started and is not listening for more — so what it has to show is the file it is
printing into, and pressing its row puts that in the transcript's space, re-read while it is open
and stopping when the command does. `GET /v1/sessions/:id/shells/:shellId` is the same thing for
anything else that wants it.

### Changed: reloading the web interface no longer means a second and a half of black

The page is a document, fourteen stylesheets and forty modules, and until now every one of them
was sent `no-store` — so a reload fetched all fifty-five again, and through a tunnel a request
costs the same half-second whether it returns three kilobytes or three hundred. Worse, they did
not arrive together. The browser learns about the stylesheets from the document, about `main.js`
from the document, and about the other thirty-nine modules only after `main.js` has arrived and
been parsed; then the page asked for its own words, which it could not do until all forty modules
had run, and it is deliberately blank until those land. Four round trips, in a line, in front of
a dark rectangle — measured at about 2.5 seconds, which is past the two-second fallback that
gives up and draws the interface in English.

Three changes, and none of them touches what the page does:

**Every stylesheet and module URL now carries the build in its path** — `/app/v1756100000/js/…` —
and is served with a year of `immutable` cache. The stamp is the executable's modification time,
the same one `/v1/health` reports, so a rebuilt Mac serves a document naming *different* URLs and
the old ones are simply never asked for again. There is no version of this that can hand somebody
a stale stylesheet, because the document itself stays `no-store`. A reload now asks for the
document and nothing else.

**The interface's words are written into the document** instead of fetched from `/v1/strings`.
That request was the worst-placed one on the page: last to be sent, first thing the paint waits
for. It is now a line of script in the head, and the fallback fetch stays for the dev server and
for a copy opened off a disk.

**Every module is named in the head**, as `modulepreload`, so all forty are asked for at once
instead of thirty-nine of them a round trip behind `main.js`. The list is read out of the bundle
rather than copied from `main.js`'s imports, so there is still only one manifest.

The file on disk is unchanged by all this — the two slots are HTML comments, and a page served by
`tools/web-serve.py` or opened as `file://` still works exactly as before.

### Fixed: shelling out could let the app re-enter itself

Waiting for a subprocess is supposed to be the most boring thing a program does. On macOS it is
not: `waitUntilExit()` polls the run loop while it waits, so on the main thread every timer and
every queued block runs *inside* the wait. Any function here that shelled out was therefore a
function that could be re-entered halfway through, at a point nobody writing it had to think
about.

That is how one walk of the dispatched-task list came to start inside another one. The outer walk
typed a briefing into a terminal through `osascript`, the timer fired during that wait, and the
second walk carried on from a copy of a task the first was about to advance — which is what
reported a task as failed while the child it opened was doing the work and finished it.

Every wait for a subprocess now happens where a run loop turning costs nothing, and the twelve
places that shell out — for git status, the assistant versions, tmux, the terminal, dev-stack
commands, transcription — go through it. Measured before the change, a one-second wait on the
main thread let a timer fire five times; after it, none.

### Changed: a session waiting for you says so itself, instead of being caught at it

Until now, "this session is waiting for an answer" was something Clawdline worked out by looking:
it captured the terminal and recognised the shape of a menu — numbered options, a caret on one of
them. That works, it works on sessions that were open before this app existed, and it has two
costs. It only knows what it has looked at, and away from the panel it looks once every twenty
seconds. And a question drawn in a shape it does not recognise is a question it never reports.

Claude Code has been writing the answer down the whole time. Every session keeps a small file
about itself under `~/.claude/sessions/`, and the status in it — idle, busy, **waiting** — is
rewritten the moment it stops being true. Clawdline now reads those files. **Nothing to install
and nothing to restart**: unlike the hooks, the files are already there, for every session already
open, whether or not you ever let this app near your `settings.json`.

What changes in front of you. A session that stops for a permission dialog, an MCP server's
question or a sandbox request is marked as waiting straight away, rather than when its dialog
happens to be recognised — including the ones drawn in shapes the screen reader has never been
able to tell apart from ordinary output. A session that has just been given work looks busy in the
two seconds before it draws its first line, instead of looking idle. A spinner Claude Code forgot
to erase after a fast turn no longer keeps a finished session looking busy. And ⌘J finds the right
conversation without matching on tab titles and timestamps, because the file names it.

The screen still has the last word on one thing, deliberately: a menu actually recognised on the
terminal is never overwritten by a session that says it is merely busy. The file can be a beat
behind a dialog that has just been drawn, and of the two ways to be wrong for that beat, only one
hides the row you have to act on.

Everything here degrades to exactly what this app did before, and it does so quietly. An older
Claude Code that writes no such files, a backend that does not carry them, a status word this
version has never heard of, a file left behind by a session whose process is gone and whose
number has since been handed to somebody else — each of those falls back to reading the screen,
and Codex, which writes nothing of the kind, was never anywhere near this path. If you would
rather it did not read them at all, `session_registry` in `~/.config/clawdline/config.json` turns
it off in one word.

### Added: a child can hand work on, one level further

A session dispatched a task and that was the end of the line — the child it opened was refused if
it tried to dispatch anything itself. That floor is now one step lower. A session may have **five**
children out at once, and each of those may have **three** of its own. What *they* open, nothing
opens under.

Both numbers are yours: `orchestrator_max_children` and `orchestrator_max_grandchildren` in
Settings → Remote, or in `~/.config/clawdline/config.json`. Setting the second to zero is the rule
this app had before — a child that tries is refused at the door — and it is a stop on the same
list rather than a switch of its own.

The first number is now counted **per session** rather than per Mac, which is the part worth
knowing if you had raised it: five is what one conversation may have out, not what the machine may.
Over both there is a ceiling nobody sets — one full tree, twenty by default — because the
per-session caps are the ones a caller could sidestep by claiming to be somebody else.

Everything downstream follows the shape. The list on the Mac and on the phone indents twice, so a
grandchild sits under its parent rather than beside it. Closing a session takes both levels with
it, deepest first, including work handed on by a child that has already reported. Cancelling one
task does the same on a smaller scale. And `CHILD.md` now tells each child which level it is on:
one with room under it gets the whole recipe for dispatching, one standing on the floor is told
plainly not to — spelled out rather than pointed at a skill, since half of these sessions are Codex
and Codex has no skills.

### Changed: a child no longer stops at every permission prompt

Dispatched sessions ran in whatever the CLI's default permission mode is, which means they stopped
and asked. **Nobody is watching a child's tab.** A session that stops for approval there does not
stop for a moment — it stops until the task times out, and afterwards it reads as work that
silently did not happen.

`orchestrator_permission` is the new setting, and `full` is the default — arrived at by trying the
narrower ones and watching each of them fail against a real task. A dispatched session's whole job
is running commands and writing files, so every stop short of the last one stops it somewhere:
`ask` on the first thing it does, which is reading its own briefing; `edits` past writing a result
but not past `cat`, `mkdir`, `curl` or `sleep`, which is most of what handing work on consists of.
What it does not widen is who may dispatch — still a `0600` file — or what a child could reach,
since it already has a shell.

It is a ceiling as well as a default. A task can name `permission_mode` and get less than the
setting; asking for more gets the setting instead, because the session doing the asking is not the
one that lives with the consequences. The record and the audit line both say what was actually
used.

**There is no `auto`, and the reason is worth knowing before you go looking for it.** Claude Code
has an `auto` mode and `--permission-mode auto` selects it — on Sonnet and on Opus. On Haiku the
same flag produces `manual`, everything asked, with no error. A word a task fills in has to mean
the same thing to every session that task can name, and one that quietly becomes the *strictest*
setting on the cheapest model is the failure nobody catches.

Two doors no setting here reaches, now written down in
[`docs/dispatch-permissions.md`](docs/dispatch-permissions.md) along with the rest of this: the
trust prompt on a directory this Mac has never run that assistant in, and Claude Code's command
screening, which refuses a `jq -n '{…}'` line on its shape alone and offers no "always allow". The
briefing a child reads was itself telling it to write files in the refused shape; it now says to
use the file tool and a heredoc.

### Fixed: a dispatched task that ended left the work it had handed on running

Closing a session cascaded and cancelling a task cascaded, but a task simply *finishing* did not.
A child that timed out, failed, or reported before its own children were done left grandchildren
running for a session that no longer existed — and on the list, a row with a `Child` chip and
nothing above it.

A `spawn_failed` that never reached briefing now also closes its tab at once, where before every
failed spawn kept one. That was not free: each is a live assistant holding a slot, and the usual
reason a tab fails to reach a prompt is that too many sessions were starting at once — so the
failure fed itself. A `timeout` still keeps its screen, which is the case where something is
written on it.

The window for reaching a prompt is four minutes rather than two. Two was measured against one
session starting; a two-level dispatch starts three at once by definition.

### Added: a task can name its model, and this Mac can say how work should be handed out

`task.json` takes a `model` — `haiku` for a mechanical pass, `opus` for a judgement somebody will
act on — and a `plan`, the whole graph the task is one node of. The plan goes near the top of
every child's briefing, leaves included: a child that knows what its answer feeds writes
something joinable, one that does not writes a report.

`~/.config/clawdline/dispatch-policy.md` is the house rules — which assistant, which model, what
shape the graph should be, and how to dispatch it. Read fresh on every dispatch and copied into
the briefing of every child that may dispatch in turn, which is the audience that needs it: a
root has a person nearby, a dispatching child has nobody. It arrives with opinions in it and
Settings → Remote has a button that opens it; delete the contents and the whole paragraph
disappears from every briefing.

The mechanics in there were each paid for. Stagger dispatches by 30–45 seconds, because every
child is a real assistant cold-starting on this Mac and four of them started together compete
until one misses its window. A `spawn_failed` retry needs a fresh id, since the old one is
terminal. And a child that fell back to doing the work itself has to say so, because the reader
is weighing evidence rather than just reading an answer.

The one string a dispatch now puts on a command line is the model name, and it is a name out of a
closed alphabet rather than a fragment of a command: `[a-z0-9._-]`, at most 64, never opening with
`-`. Nothing that admits is a character a shell reads. The route a paired phone can reach still
passes nothing.

### Fixed: a task could be reported as failed while its child was working

A dispatched task was marked `spawn_failed` with "the task's secret was lost before briefing",
while the child it had opened sat there doing the work and finished it. Both things were true.
The record was walked twice: one walk copied the task while it was still starting up, the other
briefed it and spent the secret, and then the first walk carried on from its copy and found the
secret gone. Nothing had gone wrong with the child; the broker had lost track of it.

A task's state can now only move forward. A copy that was taken before somebody else advanced the
record is refused rather than written, so a briefed task cannot become a starting one again and a
finished task cannot come back to life. Each walk also re-reads a task at the moment it advances
it, rather than trusting the list it started from.

The overlap that made this possible should not be reachable — every caller runs on the same
thread — so this release counts it rather than preventing it: a walk that begins while another is
still running writes a line to the audit log naming both, and a refused write does the same. The
next occurrence should say who the second walker is, which is the one thing the first occurrence
could not.

### Fixed: a slow-starting child could miss its briefing forever

Opening a child and seeing the assistant process was not the same thing as seeing somewhere to
type. A Claude Code session still starting slow MCP servers could already have a readable banner
without a spinner or a menu; that absence was mistaken for an idle prompt, so the briefing was
sent into startup, silently dropped, and marked delivered. The child stayed open at an empty
prompt until its task timed out.

The orchestrator now waits for the assistant's actual composer before typing. Sending bytes to a
terminal is no longer treated as delivery either: the task remains in startup until Claude Code's
transcript or Codex's rollout records that task's first user turn. If the named record still has
no such turn after the receipt window and the empty composer is back, the app retries under a
fixed attempt limit; once a turn is recorded, that receipt closes the retry gate before the child
can execute it twice. Trust prompts are still answered automatically, and a child that never
becomes ready still times out after two minutes.

### Fixed: ending a session from a phone could freeze every page in the house

Ending a session types the assistant's quit word and then takes the tab away. The pause between
the two was a fixed 1.2 seconds — fine when the word lands at an idle prompt, wrong the moment it
does not. A session in the middle of a tool call *queues* `/exit` and keeps working, so the tab
still had a job in it when the close arrived, and iTerm2 does what a terminal should do about
that: it puts up a sheet and asks.

A sheet is modal. The Apple event never came back, `osascript` never exited, and because every
remote request is answered on one queue, one unanswered dialog on the Mac stopped the web page,
the phone and the panel until somebody walked over and clicked a button they could not see.

The pause is now an answer rather than a guess: the session's tty is watched until the process is
actually gone, and only then does the tab go. One that will not leave on the word is asked with a
signal and then told — which is the same ending the sheet was offering, minus the waiting, and
gentler than the tab close it replaces. The ordinary case got quicker too, closing in a few
hundred milliseconds instead of sitting out the second and a bit.

Every round trip to iTerm2 now has a deadline as well, so a dialog this app did not raise cannot
wedge it either. When one is up, whatever asked says so — *iTerm2 is waiting on a dialog — answer
it on the Mac* — instead of the app going quiet.

### Fixed: the app could stop answering when the panel went away

Putting the panel away asks the dictation engine to stop, and stopping it reached for
`AVAudioEngine.inputNode` whether or not anything had ever been recorded. Reading that property
is not free — it builds the input node and allocates render resources against the audio HAL, on
the calling thread, with no timeout — so when the HAL was wedged the main thread went in and did
not come back. The app kept its window and answered nothing: not the bar, not the hotkey, not
HTTP. It now reaches for the node only when this session actually put a tap on it.

### Dictating to a session from a phone

The composer on the page took typing and pictures, which is the wrong shape for what a phone is
actually for here: answering a session in one sentence on the way out of the building. Every phone
can already dictate — and every phone's dictation hands the sentence to whoever wrote the
recogniser, which is the one thing this app spends its whole design not doing.

- **A microphone beside the send button.** Press it, talk, press it again. The recording goes to
  the Mac, the Whisper already installed there reads it, and the words arrive in the box where you
  can edit them. Nothing is sent until you send it.
- **The audio stops at your Mac.** Same binary, same model, same `voice_language` and
  `voice_vocabulary` as the bar's own dictation. The phone is not asked which language it is
  speaking and cannot name one, so this project's own names come out spelled the same on both
  screens.
- **`POST /v1/voice`, behind the switch that already governs sending** — and not because it writes
  anything. A device that may only read has nowhere to put a sentence once it has one, and
  transcribing spends ten-plus seconds of every core this Mac has on demand. Read-level access is
  meant to be cheap to grant; this is the one read-shaped thing here that is not.
- **No live text on a phone, and the page says so rather than pretending.** whisper.cpp cannot
  stream and Apple's recogniser runs on the Mac, so a phone records, waits, and gets the whole
  sentence at once. It counts the seconds while it waits, and adds that the first one after a
  restart loads the model first — twelve seconds of nothing looks exactly like a hang.
- **One at a time, one more in the queue, and the third is told to come back.** Transcription runs
  on a queue of its own, so a dictation cannot hold the event stream and every other page in the
  house for as long as it takes. Recording stops at three minutes on its own.
- **Every way it can refuse says which one it was, in all fourteen languages.** A microphone that
  was refused says where it is switched back on; a page opened over plain `http` says that a
  microphone needs https; a Mac with `whisper-cli` and no model says which of the two is missing,
  rather than "dictation failed".
- **`voice.transcribe` in the audit log** — device, seconds, milliseconds, characters, ok. How long
  the recording was and how long the transcript came out, and not a word of either.

Needs Whisper on the Mac ([docs/whisper.md](docs/whisper.md)) and an https address, which a tunnel
already gives you ([docs/remote.md](docs/remote.md)).

### Following a background agent into its own conversation

The strip that says *three agents are out* was the end of the road: it named them, said what each
had last reached for, and stopped there. What an agent actually did was on disk the whole time —
Claude Code writes each one a transcript beside the session's own — and nothing in the app or on
the page could open it.

- **Every row leads somewhere now.** Click an agent in the composer's strip on the page, or its
  tab above the ⌘J pane on the Mac, and the pane you are already reading swaps to that agent's
  conversation: same blocks, same folds, same reading order, because it is the same kind of
  record. `‹ Session` on the page and `← Session` in the pane come back; so do Escape, ⌘J and a
  phone's back gesture, one step each.
- **The session does not close to show it.** It stays open underneath — the row keeps updating,
  the list keeps its place — and an agent that is still working refreshes while you read it.
- **The strip is a tree, and it moved.** `main` at the root with a filled dot, a ring per agent
  under it, the kind of agent in a column of its own; the row you are reading is the lit one. It
  now sits above the composer rather than inside it, with the live line, so it is still there
  while an agent is on screen — as a line inside the box it vanished at exactly the moment you
  were navigating by it.
- **What it cost, which nothing was showing.** The header above an agent's transcript carries how
  long it ran, the tokens it drew and how many tools it used. The app has read those numbers since
  agents first appeared in the strip and had nowhere to put them.
- **`GET /v1/sessions/:id/agents/:agentId`**, the same shape as `…/transcript` plus the agent's
  own row. The id is checked before it names a file: anything that is not one of Claude Code's is
  a `404`, including anything shaped like a path.
- **Fixed: an agent's transcript read as empty.** Every record in one is marked as a sidechain,
  which is precisely what the session's own reader drops — so a busy agent came back with nothing
  at all. `Transcript.parse` now takes which of the two files it is reading.

### Codex sessions, in the same list

`isClaude` was a boolean for as long as there was only one thing it could be about. Codex draws a
different screen, keeps its record somewhere else and leaves on a different word, so what a session
is running became something it **has** rather than something the app assumes.

- **They are just sessions.** A tty running `codex` is in ⌘K next to the Claude Code ones, says
  what it is doing, takes a prompt, answers a question with a digit, and can be ended. Nothing is
  installed into Codex; it is read off what it already draws and already writes.
- **A row says which only when it matters.** With one assistant on the machine the word would be
  on every row and separate nothing, so it appears the moment the list is holding both.
- **⌘J reads the rollout**, `~/.codex/sessions/YYYY/MM/DD/rollout-….jsonl`, and lays it out as the
  same conversation a transcript becomes. Codex's own vocabulary comes through: `shell` for a
  command, `edit` for a file change, `web.search` for a plugin, `server.tool` for MCP.
- **Which file belongs to which session is a fact rather than a guess.** A Codex process holds its
  rollout open, so `lsof` names it outright. This was not theoretical: two sessions started seconds
  apart in this repository were shown each other's conversation by the version that matched on the
  directory and the clock. Its subagents write rollouts in the same folder within the same second,
  and `thread_source` in the first line is what tells those apart.
- **What it reads, observed rather than assumed.** Codex's live line is `• Working (10s • esc to
  interrupt)` — a bullet and a clock, where the bullet alone proves nothing because Codex prefixes
  everything it says with one. Its dialogs put the caret in **column zero**, which is also where it
  draws the composer's, so the rule the Claude Code reader leans on says nothing here; what
  separates them is that a dialog takes the composer away, so the last caret on the screen decides.
- **`codex exec`, `mcp-server` and the two servers are left out.** Same binary, and not somewhere
  you can type — a row that accepts your sentence and drops it is worse than no row.
- **Start either one.** *Start a session* offers whichever of the two this Mac has a home directory
  for. From a phone the assistant is a name in the path — `POST /v1/places/:id/start/codex` —
  matched against a two-case list; the body on that route is still not read at all.
- **`/quit`, not `/exit`.** Each refuses the other's word, so *End* asks the session which it is.
- Background agents stay a Claude Code row: the count comes from a directory only Claude Code
  writes, so a Codex session with three out looks like one thinking hard.
- `codex_home` in the config, for a Codex that does not live in `~/.codex` — an app launched from
  Finder inherits no login shell and cannot see your `CODEX_HOME`.

### A Codex session can name itself

A Codex row was the directory it was opened in and nothing else, which is fine until three of them
are open in one repository and the list stops distinguishing anything.

- **Off by default, and it says why.** *Settings → Name new Codex sessions*. Each title is a real
  Codex turn against your account, so this is not something to switch on for somebody.
- **One turn, after the first request.** The helper run is ephemeral, uses low reasoning with tools
  disabled, and asks `codex_auto_name_model` — `gpt-5.6-luna` unless you name another.
- **A name you chose is never overwritten.** Only a session that has never been titled is titled.
- **It is the third thing here that can use the network**, and the privacy section says so now
  rather than leaving the count at two.

### The `/` menu, in the bar and on the phone

Typing `/` in the bar used to be typing a character. It now opens the same nine-row surface the
session and stack lists use, filtered as you type, with <kbd>↑</kbd><kbd>↓</kbd> and <kbd>Tab</kbd>
to accept.

- **The list is what that working directory can actually reach** — project skills, personal skills
  and installed plugin skills, in the precedence a typed command would get: a personal skill
  replaces a project one of the same name, and a plugin skill keeps its namespace and so collides
  with neither. Skills switched off in settings are not offered.
- **Read off `SKILL.md`, not asked for.** Claude Code has no read-only way to ask a running session
  for its slash menu; its Agent SDK publishes one while starting a *new* session, and doing that on
  every `/` would be a model-shaped side effect for an autocomplete. The stable local half is enough
  to be useful and honest.
- **Metadata only.** The name and the description; never the body of a `SKILL.md`. Reading a menu
  must not execute the dynamic commands a skill is allowed to contain, and a prompt box that loaded
  skill bodies would be a second skill runtime with a second set of rules to get wrong.
- **A space ends completion.** From there the words are arguments and the ordinary Return-to-send
  path owns them.
- **The catalog is read once per session**, not once per keystroke, and a slow lookup for the tab
  you just left can never paint its skills under the new one's prompt.
- **The phone gets the same list** — `GET /v1/sessions/:id/skills`, `read` capability, metadata
  only, no local path in the reply. A Codex session answers it with an empty list for now.

### A notification says which session, not just which project

A push carried the project and the state — *"clawdline — waiting for you"* — on the reasoning that
the task title is the embarrassing half on a lock screen somebody else can read. That holds until
three sessions are open in one repository, which is the normal way this gets used: every
notification then reads the same and none of them says which tab to go to.

So the task becomes the title and the project moves down beside the state: **"fix the webhook"**
over *"clawdline is waiting for an answer"*. **This is a deliberate reversal of a documented privacy
decision**, and the prose that argued the old way has been rewritten rather than left standing — the
README, `docs/remote.md` and the comment on `WebPush.send` all say what is actually sent now. The
line that has not moved is the one under it: prompt text and transcript contents still never leave
the machine in a notification.

### The agents a session sent away

Everything here is learned by looking at a screen, and that stops working the moment the work moves
somewhere Claude Code does not draw. A session with three agents out searching a codebase painted
exactly the same spinner as one thinking about a sentence.

- **The conversations are already on disk** — `subagents/agent-<id>.meta.json` beside the
  transcript, written at the spawn — so they are read rather than guessed at. There is no record
  saying "started" and none saying "still going": an ending is a `<task-notification>` in the
  parent's transcript, so **running is the absence of one**, and the work is in establishing that
  absence cheaply enough to ask once a second. A session that has never spawned one costs a cached
  lookup and a single failed `stat`.
- The list row gets a count, the strip above the transcript gets what is happening away from it,
  and the phone gets a row per agent.
- **Quiet in all three.** An agent explains why a session is busy and never asks anything of you,
  and the one state allowed to be loud here is a session waiting for an answer.

### Everywhere a project opens

`GET /v1/sessions/:id/links` gathers the health endpoint from the icon registry, the run from the
deploy status, the servers from the project's own status command, and the backlog page. **None of it
is invented** — each is a URL some other tool already wrote into a file this app reads. The
contribution is that they are in one list on a phone, rather than four places on a Mac in another
room. On the Mac it is a Links sheet in the transcript header, in all fourteen languages; sort moved
into the settings sheet, where the rare controls live.

- **A route rather than a field on the session.** Working these out costs a `git` invocation plus a
  handful of file reads, and the session list goes out on the event stream every time anything
  moves — free when a menu is opened, a subprocess per session per second on the stream.
- **Rows are anchors only for `http(s)`**, a whitelist rather than a blacklist: those strings come
  out of a repository's own `devstack.json`, and `javascript:` in an `href` is script on that page
  with that page's cookie.
- **A `file://` row is not a link at all** — a path, a copy button, and a sentence saying it opens
  on the Mac. A link that does nothing when tapped is worse than text that explains itself.
- An untrusted dev stack stays silent rather than being probed.

### The notch, all day

`IslandMode` gains `.resting` as its floor: the character alone, breathing with its eyes shut, ears
the same width as one running session so waking moves the animation and not the shape. A `sleep`
routine is authored for both shipped packs, and a pack without one falls back to its own `idle`,
slowed, with the eyes held shut — which also suppresses `idle`'s random blink, since a sleeper does
not blink.

Because it is on screen all day, the bar is different from the states that last seconds: anything
catching the eye every few seconds is wrong. The breath is a sub-pixel swell over five seconds and
the loop closes exactly. `notch: false` still means nothing in the notch, and a screen with no
camera housing is left alone — the pill under a menu bar is fine for the minute a job runs and quite
another thing parked there all day.

**Drawing all day cost 3.6% of a core, continuously.** Throttling the redraw to 10fps measured no
difference at all and was reverted rather than shipped with a confident comment. The real cost was
building an `NSColor` from a hex string once per pixel cell per frame; memoised, 3.82% → 0.67%, with
the rendered frame byte-identical.

### From the page

- **End a session.** `exit` sent from the page never worked and could not have: it arrives at the
  prompt as a *message*, and once the assistant has gone the tab drops off the list, so the shell
  that could have taken it was unreachable from the moment it became a shell.
  `POST /v1/sessions/:id/end` sends the assistant's own word, waits, then closes — **in that order**,
  because the transcript is appended to right up to the moment the process ends and it is the thing
  you would still want tomorrow. It closes the *session*, not the tab: an iTerm2 tab can be split
  and the panes beside it belong to work nobody asked about; when it was the only one, iTerm2
  removes the tab, which is what the person pressing this expects. tmux gets `kill-pane` for the
  same reason. **No new capability** — a device that may type could already send `/exit` and then
  `exit`. Audited as `session.end`, and every test of it is a refusal, because a suite that
  occasionally ends somebody's session is a suite people stop running.
- **Bring a session's tab to the front**, without the page having to say where it is.
- **Answer a menu from the phone.** `isChoosing` parsed every option in order to count them and then
  returned a `Bool`, so a phone could be told a question was waiting and never told what it was. It
  returns the options now. `POST /key` had existed the whole time; what was missing was seeing what
  you were answering. **The number drawn is the number sent, never the position** — renumbering rows
  to make them tidy is how a button comes to answer a different question than its label.
- **A bare URL is a link.** Written links already worked; an address on its own did not, which is
  most of them. Both are handled in one pass with `[label](href)` first in the pattern, and the
  order is the whole trick — it is consumed whole, so the bare rule never sees the URL inside it.
  Trailing punctuation goes outside the link, and a closing bracket only if the address did not open
  one: `…/Foo_(bar)` keeps its paren, `(https://example.com)` does not.
- **A conversation says whether it is still running.** The Mac has the notch and the footer; a phone
  has neither once the list is a different screen. The live line now sits above the composer while a
  session works — dim and monospace, not another coloured panel competing with the warning that
  sometimes sits beside it.
### Fixed

- **Codex sessions had started disappearing from the list.** Interactive Codex now runs
  `codex app-server --listen stdio://` beside its own UI, and the rule that keeps `codex exec` and
  the servers out of the list was refusing the whole tty on account of the child. Refusal now flows
  **down the process tree** on `ppid` instead of sideways across the tty, so a server descendant no
  longer disqualifies the interactive parent that spawned it — while `codex exec`'s own native child
  is still kept out by its refused ancestor. `codex sandbox` joined the list of subcommands that are
  not somewhere you can type.
- **Each row wears the assistant's product mark**, Claude's coral and OpenAI's green, drawn as SVG
  so an 11-point mark stays sharp. It still appears only when the list is holding both — but when it
  does, the split is visible before the word beside it has been read.
- **A new Codex session no longer borrows the previous one's transcript.** A rollout that predates
  the process holding it is not that process's rollout.
- **A numbered list you typed was read as a menu.** `❯` is both the glyph a dialog marks its
  current row with and the one Claude Code puts in front of the line you type, so a message opening
  with a numbered list echoed back as character-for-character the shape of a menu with its first row
  selected. The session went to *waiting*, the phone raised "this session is waiting for an answer",
  and that notice says sending from here confirms the highlighted option rather than typing — so
  somebody who sends lists, which is most people, was told a question existed and warned off
  answering it, leaving nothing they could do.
- **Every page decided it was out of date the moment the stream connected.** A page identifies a
  build from `build|version|protocol`; `build` had been added to `/v1/health` and not to the `hello`
  event, so the two sources disagreed about which fields exist and the stamps differed *by
  construction*, immediately, on every page. Reloading could not clear it, because the fresh page
  computed the same mismatch a second later. Both send the same fields now.
- **A phone already holding a stale page had no way to learn otherwise.** Serving `no-store` fixed
  every load after the fix and did nothing for a device that already had the old copy — it never
  asks again, so it never finds out. The service worker now claims open tabs and fetches the page
  with `cache: "reload"` rather than letting the HTTP cache answer.
- **A black screen shipped**, from `git add -A` in a worktree shared with other agents: it picked up
  an `index.html` that was midway through having its sort control removed, so the markup was gone
  and the listener binding to it was not. The script died on the first line that touched it.
- **Every rebuild was a coin flip on leaving a crash report behind.** `pkill` asks; it does not
  wait, and the next line deleted the bundle a process on its way out was still reading — AppKit's
  teardown asks CoreFoundation for the bundle identifier, which then reads freed memory. `build.sh`
  waits now, and it stopped printing "relaunched" the instant `open` returned, which said nothing:
  a build that killed the app and failed to restart it used to report success while the person
  watching saw their bar vanish with no reason given.
- **The release script deleted the build before checking it** — it removed the worktree and then
  looked for the app inside it, so the check meant to catch an empty build was the thing
  guaranteeing one. The build lands beside the worktree now.
- **The "this page is older" notice can be dismissed.** It was correct and unclearable, and
  reloading to silence a banner is what somebody halfway through a sentence is trying not to do.
- The sleepy-tuna mascot pack failed its own validator.

## 0.6.0 — 2026-08-19

The release the README had been describing. 0.5.0 was cut by hand two hours before the remote half
landed, so for a day the only build you could download did not contain the thing half the README is
about — which is why [tools/release.sh](tools/release.sh) exists and why nothing is cut by hand any
more: it builds from a clean worktree at HEAD, runs the tests, checks the built app's own version
string, and does not publish anything until every one of those has passed.

### Answer a session from your phone

Your Mac serves a page; your phone opens it on your own domain through your own Cloudflare tunnel.
Every session with its state and its transcript, a box to type into, and Web Push when one starts
waiting for you — signed on your Mac with CryptoKit. **There is no account, no relay and no server
of mine in the path**, which also means it works when your Claude account uses an API key.

It is not a terminal in a browser and deliberately not. It answers one question: which session
wants you, and can you answer it from here.

- **Off until you switch it on**, and typing into a session is a second switch after that.
- Pairing is a six-digit code shown on the Mac; a device gets read, or read-and-send.
- Loopback only. `Host` headers are validated so a hostile page cannot reach it by DNS rebinding,
  and cross-site requests are refused.
- A multiple-choice question can be answered from the phone, by the option's own number — the
  picker discards a typed answer, so `/send` refuses one outright rather than answering the wrong
  thing quietly.
- Start a new session in any project this Mac has worked in — the client sends an opaque id, never
  a path, and the command is the literal `claude`.

### Claude Code can say so itself

Everything here works by looking, and looking has one cost it cannot avoid: it only knows what it
has looked at. With the bar away that is once every twenty seconds — long enough for a permission
dialog to sit there through a whole train of thought.

- **Optional hooks, off until you press a button.** *Settings → Claude Code hooks → Install* puts
  five entries in `~/.claude/settings.json`. After that, the moment a turn starts, ends or needs
  an answer, a two-line note lands in a directory the app is watching, and the reading that would
  have happened twenty seconds later happens in under a second instead. Measured on three
  sessions: 20s → 0.8s.
- **The polling does not change.** Same three round trips a minute; a note moves one of them to a
  moment worth taking it rather than adding one.
- **The screen is still the authority, and that is the design.** `Notification` fires both for a
  permission request and for a session that has merely been quiet for a minute, so a note asks
  for a reading and `SessionState` still decides what is on the screen. **No note asserts that a
  session is working.** Measuring is what settled that: Claude Code draws its live line about 2.1
  seconds after you press Return and then removes it again while the answer streams, so a claim
  short enough to be safe would cover almost none of a turn — and a long one could not be
  retracted, because pressing Esc to cancel fires no hook at all. A nudge looks twice instead,
  immediately and again 2.5 seconds later, which is the same information with nothing claimed.
- **The one thing a note does settle** is something the screen gets wrong rather than misses: a
  live line that was never erased after a fast turn. A `Stop` overrides it for ten seconds.
- **Five events, all rare.** `PreToolUse` is deliberately not among them — it fires hundreds of
  times an hour to say something `UserPromptSubmit` and `Stop` already bracket. `SubagentStop` is
  left out because a subagent finishing is not the session finishing.
- **Your settings file is a guest room.** Everything already in it is read, changed and written
  back, a copy is kept once as `settings.json.before-clawdline`, and removing the hooks leaves
  the file reading as though this had never touched it.
- **⌘J finds the transcript by name.** A hook carries the session id, which is what Claude Code
  names the transcript file after — so the matching by title and start time is only needed when
  there are no hooks.
- `clawdline://hooks?install=1` and `install=0`, for setting a machine up from a script.
- `"hooks": false` in the config ignores the notes without touching anybody's settings file.
- **And the Mac tells you.** Notifications when a session starts waiting, when a turn over two
  minutes ends, and — if you switch it on — when a deploy stops running.

The contract, including what a note is and is not allowed to change, is in
[docs/hooks.md](docs/hooks.md).

### Everywhere else

Fourteen languages, on the Mac and on the page. A transcript pane that folds a finished run of tool
calls to one line. On-device dictation through Whisper. A settings window that looks like it belongs
to this project, and an app icon. A backlog the status line can draw.

### Security

A route that took a directory and a command out of the request body and ran the second in the first
has been removed, along with the code behind it. **It was never in a release** — 0.5.0 predates the
whole remote feature — but it was on `main` for a day, and it is named here rather than left in a
diff. What replaces it has no field a path or a command can be written into.
[docs/remote.md](docs/remote.md) has the threat model in full, including what it does not defend
against.

### Requires

macOS 13+, Apple silicon. iTerm2 directly, every other terminal through tmux. Built against Claude
Code 2.1.235 — see [docs/compatibility.md](docs/compatibility.md). Swift and AppKit, no
dependencies, no package manager. 1205 tests.

## 0.5.0 — 2026-08-18

### The bar knows what every session is doing

Not looking at the terminal worked for one session. With four, you were back to going round the
tabs to find out who had finished — so the thing that made the bar worth having stopped scaling
at exactly the point you started needing it.

- **⌘K names what each session is doing.** A row that is working carries the line Claude Code
  draws for itself, quietly; a session with a question on screen and nobody answering it says
  so, loudly, because that is the only state that costs you something for every second it goes
  unnoticed. Nothing is installed into Claude Code to know this — it is each session's own
  screen, read the same way the ⌘J pane reads it, and a screen that cannot be read leaves the
  row exactly as plain as it was rather than guessing at it.
- **The menu bar ✳ carries it too.** Nothing running and it is the character it always was;
  things running and it carries a count; something waiting for an answer and it says so in the
  accent. It is the one piece of screen this app owns all day and it used to say nothing.
- **One reading serves all of it.** The session list, the strip above the transcript, the menu
  bar and the island are four consumers of one set of terminal round trips — 1.2s while the
  panel is up, once every twenty seconds while it is not, and a single `ps` and nothing else on
  a machine with no Claude Code running.

### The servers a project runs

⌘S lists every project that describes a dev stack, whether or not a session is open in it —
because the project whose servers have quietly fallen over is exactly the one you have no
session in. It reads a `.devstack.json` out of the repository and runs the commands that file
names; **Clawdline never starts a process of its own**, so the servers outlive the app rather
than dying with it on the next quit or update. A row can start, restart and stop a stack, and
show what its processes printed. The format is documented in
[docs/devstack.md](docs/devstack.md), so anything can produce one — process-compose, Overmind,
pm2, Docker Compose, a Makefile with PID files.

A stack whose status command has never been agreed to is drawn as its own thing rather than as
"down": a grey square next to a green one reads as an outage, and the first day that shipped it
sent somebody looking for one that was not happening.

### A character in the notch

Play, and meant to read that way — it tells you nothing the menu bar mark does not. Your mascot
lives in the menu bar band beside the camera housing: it leans out while something is running,
says which session wants you when one does, and dances when a long job finishes. How hard it
appears to be working is how much you have running.

Clicking the character opens the bar; clicking the words goes to that terminal tab. When the
number stands for more than one session, it offers a menu rather than picking for you.

`"notch": false` turns the whole thing off — no window, no observer, nothing drawn.

### Settings, as controls

Menu bar ✳ → **Settings…** has a control for everything worth changing, and every control
applies the moment you move it. The hotkey is recorded by pressing it rather than spelled into a
text field; the pane's font list offers only monospaced faces, because that is a setting you can
only get wrong. `config.json` is still the truth, still hand-editable, and there is a button in
the window that opens it.

### Switching sessions got about five times faster

Measured on a real 29 MB transcript, per press of ↓: **443 ms → 86 ms**, and to roughly nothing
for a session you have already looked at.

- `Transcript.parse` read the whole tail and threw away all but the last four hundred entries.
  It reads backwards now and stops when it has enough — and walks the UTF-8 view rather than
  building an array of every line, which was 140 ms of the 268 on its own.
- `Transcript.locate` was calling `stat` inside a sort comparator, so a project with fifty-six
  transcripts in it spent several hundred of them to order fifty-six names.
- Transcript titles are remembered against each file's size and mtime, so the six megabytes of
  reading that picked one file happens once rather than on every switch.
- Laid-out transcripts are kept, keyed by the same signature that decides whether a repaint is
  needed, and the sessions either side of the selected one are laid out before you ask for them.

**Fixed: switching quickly could paint the wrong session's conversation**, under the next
session's name — nothing checked that the reader you started was still the reader you wanted by
the time it finished.

### The terminal's tab follows the bar

The bar's target and the tab in front of you were free to be two different sessions, and the
moment you closed the panel you were looking at the wrong one. They are now the same session by
construction. Selecting is not the same as activating and only the first one happens, or every
press of Tab would take the keyboard out of the box you are typing into.
`"follow_target": false` restores the old behaviour.

### Fixed

- **tmux found no Claude Code at all.** The pane's process name is the basename of the
  executable, and the current installer symlinks `claude` at
  `~/.local/share/claude/versions/<version>` — so every pane announced itself as a version
  number and every tmux session was listed as an ordinary shell. That is the one path the README
  promises for Terminal.app, Ghostty, Warp and the rest. `ps` reads argv, which still says
  `claude`, so the tty is asked as well as the name.
- **A tmux session never once reported being busy.** Its captures arrive with the colours still
  in them, and a line that begins with a colour code does not begin with the character it looks
  like it begins with.

### For contributors

- **A string that is left in English now fails the build.** The check that catches "copied the
  reference file and translated half of it" used to sample fifteen strings by hand, so a new one
  was by definition not in it — a whole settings window shipped with thirty-two strings that
  nothing looked at. It reflects over every stored string now, and the handful that legitimately
  read the same in two languages are exempted one at a time, per language, with a reason.
- **`/recap` is in the repo.** Four questions at the end of a stretch of work — what changed,
  what it is worth, what has gone out of sync, and is it in version control — with the checks
  that this project in particular keeps forgetting: the fourteen languages, the two READMEs, and
  the test count that three files claim.

## 0.4.0 — 2026-08-17

Dictation that hears two languages in one sentence and does not need an account, images that
arrive as images, and thirteen languages of interface.
→ [Full notes](https://github.com/sainteye/clawdline/releases/tag/v0.4.0)

## 0.3.0 — 2026-08-15

Every terminal, through tmux: Terminal.app, Warp, Tabby, Ghostty, Alacritty and Kitty all work
as long as Claude Code runs inside tmux.
→ [Full notes](https://github.com/sainteye/clawdline/releases/tag/v0.3.0)

## 0.2.0 — 2026-08-15

Mascots became a browsable, swappable format, with a second pack to prove the format was one.
→ [Full notes](https://github.com/sainteye/clawdline/releases/tag/v0.2.0)

## 0.1.0 — 2026-08-15

First public release: a Spotlight-style prompt bar that floats at eye level and sends what you
type straight into a Claude Code session, without bringing the terminal to the front.
→ [Full notes](https://github.com/sainteye/clawdline/releases/tag/v0.1.0)
