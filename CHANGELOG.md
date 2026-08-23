# Changelog

Release notes have lived on [the releases page](https://github.com/sainteye/clawdline/releases)
since 0.1.0. This file is where the next one is written before it gets there, and where the
older ones can be found from without leaving the repository.

The entries are prose rather than a list of commits. What belongs in one is **what changed for
somebody using this** — a commit log already exists and is better at being a commit log.

## Unreleased

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

The contract, including what a note is and is not allowed to change, is in
[docs/hooks.md](docs/hooks.md).

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
