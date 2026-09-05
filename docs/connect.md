# Connecting a project to Clawdline

**This page is written for an agent.** If somebody has pasted
`https://github.com/sainteye/clawdline` at you and asked you to connect their project, this is
the whole job. You do not need to install anything, import anything, or run Clawdline. Read this
page, create a few small files in and around their repository, verify them, and report what you
did.

If you are a person: you can do all of this by hand, and the two contract pages linked below are
the reference. But the reason this page exists is so you do not have to.

---

## What you are connecting to

Clawdline is a macOS bar that manages several Claude Code sessions. It shows, for the project a
session is working in: its name and mark, which of its servers are up, whether its site is
healthy, how far a deploy has got, how far the slow thing running in the next tab has got, how much
is in its backlog, and how much of a milestone is left.

**It reads files. It never calls you.** There is no SDK, no package, no webhook to register, and
nothing to add to the project's dependencies. Every integration below is "write a small JSON file
where Clawdline looks". A project with none of them still works; it simply has less to say.

**There are seven kinds and this page walks all seven**, in the order worth doing them:

| # | what it says | where it lives |
|---|---|---|
| [1](#1-the-servers--devstackjson) | the dev stack: servers, ports, start/stop/status | `.devstack.json`, in the repository |
| [2](#2-the-health-check) | whether what this project deploys is answering | `~/.claude/statusline-cache/health-<path>.json` |
| [3](#3-the-deploy-or-ci-run) | a deploy or CI run in flight | `~/.claude/statusline-cache/ghrun-<owner>-<repo>.json` |
| [4](#4-the-long-local-run) | anything long running locally: a test, a build, an import, an encode | `~/.claude/statusline-cache/run-<path>.json` |
| [5](#5-the-backlog) | a backlog | `~/.claude/statusline-cache/backlog-<path>.json` |
| [6](#6-the-finite-milestone) | a finite milestone | `~/.claude/statusline-cache/milestone-<path>.json` |
| [7](#7-the-mark-and-the-colour) | the mark and the colour | `~/.claude/project-icons.json` |

You will not write all seven for most projects, and you should not pretend otherwise. What you owe
the user is the list: which ones you wired, which ones this project has nothing to say through, and
which you skipped and why. The last section of this page is how you check the ones you did.

Two pages are the authority, and where this page and they disagree, they win:

- **[docs/devstack.md](devstack.md)** — the servers a project declares
- **[docs/project-status.md](project-status.md)** — the other six: the mark, the health check, the
  deploy, the local run, the backlog and the milestone

---

## Before you start

Work out these three things about the project you are in, because everything below needs them:

```bash
pwd                                    # the absolute path of the repository root
git remote get-url origin              # for <owner>/<repo>
ls ~/.claude/statusline-cache/ 2>/dev/null   # what already exists for this project
```

The last one matters, and the next section is about why: something may already be writing these
files, and **a second writer for the same file is a race** whose loser is whichever ran first.

Four of the filenames below — `health-`, `run-`, `backlog-`, `milestone-` — contain the project's
path with `/` turned into `-`. For `/Users/you/code/thing` that is `-Users-you-code-thing`, so the
file is `health--Users-you-code-thing.json`. **The doubled dash is not a typo**: the prefix ends in
one and the path begins with one. `pwd | tr / -` prints it, and that is worth using rather than
typing, because a name that is wrong by one character is a file nothing ever reads and nothing ever
complains about.

`ghrun-` is the one that is keyed differently — by `<owner>-<repo>` from the `origin` remote,
because a deploy is a fact about a repository rather than about one checkout. Section 4 says why
the local run goes the other way.

---

## The shortcut: is claude-bestiary installed?

Four of the seven below — the health check (2), the deploy (3), the backlog (5) and the mark (7) —
are small files that have to be *kept current*, and keeping them current is most of the work.
[claude-bestiary](https://github.com/sainteye/claude-bestiary) already does it. It writes these
files for its own terminal status line, and Clawdline reads the same ones, so installing it
connects four of the seven with nothing left for you to maintain.

**The backlog is the one that surprises people**, because the count looks like something only this
project could know — and bestiary does not know it either. When the file goes stale it shells out to
a counter **inside your repository** and writes down whatever that prints, so what it is offering is
the scheduling, not the counting. The paths it probes for are the convention worth adopting:
`tools/backlog.py --json`, or `backend/scripts/build_backlog_artifact.py`. Put this project's
counter at one of those and the backlog re-counts itself whenever its source file changes, with no
cron entry, no hook, and one writer for the JSON rather than two. Put it anywhere else and the
backlog is yours to keep current, which is section 5.

**The other three are yours either way.** `.devstack.json` (1) states this project's own
commands; the milestone (6) counts work only this project can define; and the long local run
(4) is written by the thing that is taking the time, because nothing polls for a job that started
thirty seconds ago in the next tab. Check what is actually in the cache directory before you decide
who owns a file; a second writer for one file is a race whose loser is whichever ran first.

**Check first, then recommend it:**

```bash
ls -l ~/.claude/project-icons.json ~/.claude/statusline-cache/ 2>/dev/null
```

- **Files already there** — it is installed and running. Do not write those files yourself; you
  would be a second writer racing the first. Your job is then `.devstack.json` and whatever
  bestiary does not cover, and telling the user which is which.
- **Nothing there** — say so in your report and recommend installing it, with the link. Then do
  the work below by hand anyway, so the user gets something today rather than a suggestion.

Its own repository has a page like this one, written for you.

---

## 1. The servers — `.devstack.json`

The one that changes a user's day. It puts the project's processes in the bar, with their ports as
links, and lets them be started and restarted from there.

Put it at the repository root. **What you write decides what the user can do**, so decide that
first rather than picking a tier:

| the file names | the user gets |
|---|---|
| `processes` with ports | a live row: which are up, ports as links. **Read only.** |
| …and `status` | the same row, but the project's own answer — health probes, exit codes, the error from a crashed process |
| …and `up` / `down` / `restart` | **buttons.** Start, stop and restart from the bar, without finding the tab |
| …and `logs` | the log of a process that failed, in the panel, next to the red mark |

**Stopping before `up` and `restart` leaves a panel nobody can act on** — it tells the reader that
two of three services are down and gives them nowhere to press. That is worse than it sounds,
because the row looks finished. Go all the way unless something makes it impossible, and if you
stop short, **say in your report which of these the user did not get and why**.

Start with the shape:

```jsonc
{
  "version": 1,
  "name": "thing",
  "status":  "make stack-status",
  "up":      "make stack-up",
  "down":    "make stack-down",
  "restart": "make stack-restart P={process}",
  "logs":    "make stack-logs P={process} N={lines}",

  "processes": [
    { "name": "api", "port": 8002 },
    { "name": "web", "port": 3001 }
  ]
}
```

`processes` alone already gets a live row — Clawdline connects to each port itself, which executes
nothing and needs no trust. Keep it even when `status` exists: it is what still works before the
user has granted trust, and for a project they never trust at all.

### If the project has no start command, writing one is part of this job

Most projects do not have `make stack-up`. They have a `README` that says to open three terminals
and run three things, or a `docker compose up`, or a script somebody wrote once. **Turning that
into one command the project genuinely has is the work**, not a reason to skip the field.

What "do not invent a command" means, precisely:

- **Never name a command that does not exist or does not work.** A button that fails when pressed
  is worse than no button, and the user granted trust on the strength of that name.
- **Do write one, and prove it.** A `Makefile` target, a `scripts/stack-up.sh` — whatever fits how
  this project already does things. Then run it, run `status`, and show that the row goes green.
- **Ask before anything destructive.** `down` that drops a database volume, `restart` that
  redeploys — those are the user's call, not yours.

`{process}` and `{lines}` are substituted by Clawdline, so `restart` and `logs` can act on one
service rather than all of them. A `restart` that ignores `{process}` and restarts everything is
allowed and is much less useful — the whole reason somebody reaches for it is that one thing died.

### Three things about `status` that are easy to get wrong

All of them are in `devstack.md`:

- **The process states are a closed vocabulary**: `healthy`, `running`, `starting`, `completed`,
  `exited`, `stopped`. Do not invent one. The *top-level* `state` is a different vocabulary —
  `running`, `partial`, `stopped`, `unknown` — and you may omit it entirely.
- **`completed` is for a one-shot that finished on purpose** — a build, a `docker compose up -d`.
  Without it every successful build draws a red mark, and a red mark that is usually wrong is one
  nobody reads on the day it is right.
- **Put the failed process's last few log lines in its `error` field.** It travels with the state
  because the reader always wants it next, and that one field is the difference between an agent
  that reports a red mark and one that fixes it.

---

## 2. The health check

Whether the thing this project deploys is answering, as a dot with a label.

`~/.claude/statusline-cache/health-<path>.json`:

```json
{ "state": "ok", "label": "example.com" }
```

`ok` is the green dot, and a recognised state that is not `ok` is red —
[project-status.md](project-status.md) has the vocabulary. A value **no reader recognises** draws
nothing rather than a red dot, which is the rule every file on this page follows: a project whose
check has never run is not a project that is down. `label` is what to show — usually the domain.

**Do not build a cron entry for this before checking whether one is needed.**
[claude-bestiary](https://github.com/sainteye/claude-bestiary) polls and writes this file for you:
put a `health` block in that project's entry in `~/.claude/project-icons.json` and its status line
refreshes the check on its own. Writing this file by hand is for machines that do not run it.

If you do have to keep it current yourself, say what you wired up and prove it moves. A health
check frozen on yesterday is worse than none: it reports that everything is fine, forever,
including during the outage.

---

## 3. The deploy or CI run

`~/.claude/statusline-cache/ghrun-<owner>-<repo>.json`, from the `origin` remote:

```jsonc
{
  "state": "running",            // running | ok | fail
  "label": "deploy",
  "started_at": 1786925931,      // unix seconds
  "typical_seconds": 800,        // how long this workflow usually takes
  "url": "https://github.com/you/thing/actions/runs/31981652530"
}
```

While `running`, elapsed against typical draws a progress bar. **Always fill in `url`** — a run
you cannot open is a number you have to go and look up somewhere else, which is most of the reason
nobody looks.

The natural place to write it is a step in the workflow itself, or a git hook after a push.

---

## 4. The long local run

Section 3 is a run on somebody else's machines. This one is **the progress protocol for work
happening here**: whatever is taking the time writes one small file, and the bar draws how far it
has got, so a job started in one tab is visible while the user works in another.

**A test or a build is the worked example, not the definition.** `label` and `phase` are free text,
so a lint pass, a data import, a schema migration, a long encode, a model download and the project's
own deploy script all fit this file exactly as `./test.sh` does. The question to ask about a project
is not "does it have a test suite" but **"what does this project do that takes minutes?"** — that is
what goes here, and there is usually more than one.

**And it is a tool you can use yourself.** If you are about to run something that takes four minutes,
this file is how the person watching finds out how far it has got without reading your terminal.
Wire it for the project, then use it: a wrapped command is one line and it answers the question the
user would otherwise have to ask you.

`~/.claude/statusline-cache/run-<path>.json`, keyed by the working directory:

```jsonc
{
  "state": "running",       // required — running | ok | fail | none
  "label": "test",          // free text, drawn exactly as written, never translated
  "phase": "compiling",     // optional, drawn where a percentage would be
  "started_at": 1757040000, // unix seconds
  "typical_seconds": 288,   // optional — time one honest run; leave it out rather than guess
  "updated_at": 1757040100  // required while running; a `running` row without it is drawn by nobody
}
```

**Keyed by the working directory, not by the `origin` remote**, and that is the difference from
section 3 rather than an inconsistency: a machine that has three worktrees of one repository
compiling at once gets three rows, where a remote-keyed name would give one row that each run
overwrites.

### Where it is written from

**From the command the project already runs**, not from something the user has to remember to type
first. A row that only appears when somebody remembered a wrapper reports on the runs they
remembered, which is not the same thing as what the project is doing.

This is the one kind on the page where connecting the project means **editing a file the project
already has** rather than adding one beside it. Say so before you do it, keep the change small, and
make sure a failure to write the status file cannot fail the run — nobody's suite should go red
because a status file could not be written.

### The short way: `clawdline-progress`

This repository ships a helper, `Resources/clawdline-progress.sh`, and it is what to reach for
first:

```sh
# inside the project's own script, target or task — the command it wraps is not edited
clawdline-progress run --label lint --typical 120 -- ./scripts/lint.sh

# or sourced into a script that already knows what its own phases are called
. "$CLAWDLINE_PROGRESS"
progress_start --label test --typical 288
progress_phase compiling
```

The wrapper form is one line in front of a command that stays exactly as it was, and the state the
row lands on is that command's own exit status rather than a line the control flow has to reach.
Put it where the project's command already lives — the `test` target, the `lint` script, the
migration entry point — so that it runs whenever that command does. Typed at a shell for a one-off
job it works just as well; what it must not turn into is something a person has to remember before
every run. `$CLAWDLINE_PROGRESS` is wherever this machine's copy of the helper is; if nothing sets
it, set it yourself — sourcing needs a path to that file and nothing else.

**Why there is a helper at all, when the JSON above is six fields.** Because the JSON is the easy
half and the shell around it is the hard one, and it fails in the direction that looks like success.
Two separate agents, each holding this whole page, got the traps wrong on their first attempt on
this machine's `bash 3.2.57`. Measured against `/bin/bash` 3.2.57(1)-release, one script each:

- an **`EXIT` trap on its own reads a killed run as a clean one** — sent `TERM`, that handler runs
  with `$?` set to `0`, so the last thing written is `ok` and the bar shows a tick for a run
  somebody killed;
- **`set -e` does not fire `ERR` inside a function** unless `set -E` is on too, so a producer that
  hangs its `fail` on `ERR` writes nothing at all and leaves `running` behind;
- **a signal handler that returns lets the run declare success** — the handler writes `fail`, the
  script carries on from where the signal interrupted it, reaches its own `exit 0`, and the *last*
  write is `ok`.

### The long way, written by hand

Nothing stops a project writing the file itself, and plenty should — the format is open and that
openness is the point. What it has to get right is everything in the sketch below.

```bash
run=~/.claude/statusline-cache/run-$(pwd | tr / -).json
started=$(date +%s)
finished=0
mkdir -p "$(dirname "$run")" 2>/dev/null || :   # absent on a machine with nothing else writing there

say() {   # say <state> [phase]
  { printf '{"state":"%s","label":"test","phase":"%s","started_at":%d,"typical_seconds":288,"updated_at":%d}\n' \
      "$1" "$2" "$started" "$(date +%s)" > "$run.tmp" && mv "$run.tmp" "$run"; } 2>/dev/null
  return 0   # a status file must never be able to fail the run it is reporting on
}

finish() {   # finish <exit status> — the last write, and only ever one of them
  [ "$finished" = 0 ] || return 0
  finished=1
  case "${1:-1}" in 0) say ok ;; *) say fail ;; esac
  return 0
}

trap 'finish "$?"' EXIT           # every way out, a deliberate `exit 1` included
trap 'finish 130; exit 130' INT   # the `exit` is the point of these two: a handler
trap 'finish 143; exit 143' TERM  # that returns carries on and then declares success

status=0
say running compiling
make build || status=$?                    # … or whatever this project compiles with

if [ "$status" -eq 0 ]; then
  say running "running tests"
  make test || status=$?                   # … or whatever it tests with
fi

exit "$status"    # no `say ok` anywhere: the EXIT trap decides which of the two it was
```

Five things in that sketch are the whole point of it:

- **Nothing writes `ok` by hand.** The tick is a *consequence* of the run's exit status, never a
  line the control flow reaches on its way past. A sketch ending in `say ok` reports a red suite as
  a tick on every machine whose script does not set `-e`, and freezes at `running` until the
  staleness ceiling clears it on every machine whose script does — and the first is the worse of the
  two, because it is the one that looks like it is working. The `|| status=$?` is what survives both
  settings: a command on the left of `||` does not trip `set -e`, and its status is still there to
  be read.
- **The traps, and `EXIT` as the one carrying most of it.** `INT` and `TERM` cover `Ctrl-C` and a
  `kill`; `KILL` cannot be trapped, which is what the `updated_at` bullet is for. `ERR` alone is not
  enough, and that is measured rather than assumed — on macOS's own bash 3.2, under `set -e`, a
  command failing *inside a function* ends the script without firing `ERR` at all unless `set -E` is
  on too, and every deliberate `exit 1` misses `ERR` by construction. `EXIT` sees all of them, and
  this repository's own `test.sh` uses the same construction if you want a worked example longer
  than a sketch. **Look for an existing `EXIT` trap before you add one** — a shell keeps exactly
  one, a second `trap … EXIT` silently replaces the first, and whatever cleanup it was doing goes
  with it. If the script already has one, call `finish` from inside that handler rather than
  installing a second.
- **`updated_at` on every write.** A reader ignores a `running` row whose `updated_at` is older
  than `stale_after` — 900 seconds when that field is absent — because a run killed with `kill -9`
  writes nothing on the way out and nothing else ever retracts it. That ceiling is the only thing
  standing between the user and a progress bar that runs forever, which is why the field is
  **required while the row says `running`**: a row without a usable number there is malformed, and
  is drawn by nobody rather than being drawn from `started_at` instead. A finished `ok` or `fail`
  does not decay and does not need it. If a phase of this project's build takes longer than fifteen
  minutes, either write from inside it or set `stale_after` to something that fits, but do not leave
  the row unable to say it is still alive.
- **`.tmp` then `mv`.** As with every file on this page.
- **The `mkdir -p` and the `return 0`.** `~/.claude/statusline-cache/` does not exist on a machine
  where nothing else has ever written there, so without the `mkdir` this whole section quietly does
  nothing on exactly the machines it was needed on. And under `set -e` — which a test script very
  often has — a full disk or an unwritable home would otherwise take the suite down with it, and
  the user would be debugging their tests over a status chip. Redirect the group's stderr as well:
  a failed redirection prints from the shell rather than from the command, so silencing only the
  `printf` still leaves three lines of noise in the middle of somebody's test output. The chip is
  allowed to be absent; the run is not allowed to be wrong about itself, or noisy about it.

**`typical_seconds` is optional, and an unmeasured project leaves it out.** The bar is drawn against
that number and nothing else, so a guess draws a bar that is confidently wrong and no reader can tell
a guess from a measurement. Time one honest run and write that; if you have not timed it, leave the
field out and the row still says what is running and which phase it is in, with an empty bar rather
than a false one. This repository does both on purpose — `./test.sh` writes a measured `288`, and
`./build.sh`, which nobody has ever timed, writes no `typical_seconds` at all.

`none`, and every state a reader does not recognise, draws nothing at all. Do not invent states:
an unrecognised one is not an error the user gets told about, it is a row that silently disappears.

There is deliberately **no `producer` field** here, unlike `ghrun-`. That field exists to arbitrate
between two writers of one file; this file has one writer, which is the script itself.

---

## 5. The backlog

`~/.claude/statusline-cache/backlog-<path>.json`:

```jsonc
{
  "total": 44,
  "lanes": { "now": 2, "scheduled": 6, "waiting": 17, "drop": 19 },
  "artifact": "/Users/you/code/thing/artifacts/backlog.html"
}
```

`≡44` in the bar with `now 2` beside it in the project's accent colour, and the chip opens
`artifact`. Only the `now` lane is highlighted, which is the design: a backlog's enemy is not being
long, it is being unread.

`total` is the one field that must be there and must be a number — a row without it is dropped
whole rather than drawn as zero. The other lane names are free.

**Do not hand-write the counts.** A backlog file typed once is a number that was true on the
afternoon somebody wired this up, and it will still be on the screen next spring. If this project
keeps its backlog in a file — a YAML list, a `TODO.md`, GitHub issues — write the few lines that
count it and say in your report what runs them. **Put those few lines at `tools/backlog.py` and give
them a `--json` flag**: that is the path claude-bestiary probes for, so the answer to "what runs
them" becomes "whatever is already installed, whenever the source file changes" and nothing has to
be scheduled. Anywhere else works and is then yours to keep current. If this project does not keep a
backlog anywhere, this is a kind it has nothing to say through: skip it and say so.

`artifact` is optional and is what the chip opens. It must be a regular `.html` file **inside the
project's directory**: on the Mac the chip opens it directly, and for the browser page and a paired
phone Clawdline serves it back through an authenticated same-origin route with scripts disabled,
which refuses paths outside the project and symlink escapes. A path elsewhere works on the Mac and
is a link to nothing from the phone, which is the worst of the two failures because the person who
wired it never sees it.

---

## 6. The finite milestone

`~/.claude/statusline-cache/milestone-<path>.json`:

```jsonc
{
  "total": 8,
  "complete": 3,
  "waiting_on_user": 2,
  "artifact": "/Users/you/code/thing/artifacts/launch-milestone.html"
}
```

A backlog and a milestone answer different questions, which is why they are two files rather than
one with a flag: a backlog is an unbounded inventory, a milestone is a **finite definition of
done**. "Ship the beta" is a milestone. "Everything we might do" is not.

`total` and `complete` must both be present, both be numbers, and satisfy `0 ≤ complete ≤ total`. A
row that fails any of that is dropped whole — deliberately, because a milestone drawn wrong is
worse than one not drawn, and it is the reason to compute these two rather than maintain them.

`waiting_on_user` is a separate count on purpose, and it is the field worth wiring properly. Work
stopped on a credential, a spend approval or a product decision is not slow implementation, and
folded into "incomplete" it looks exactly like it. Kept separate it reads *2 waiting on you*, which
is a thing the user can act on in a minute.

**The milestone is a row in the Links sheet, not a chip in the Mac's bar** — it shows as
`milestone 3/8` on the browser page and on a paired phone, and the Mac shows the backlog rather
than this. Say that when you report, or the user goes looking in the bar for something that was
never going to be there.

`artifact` follows the same rule as the backlog's: a regular `.html` file inside the project.

---

## 7. The mark and the colour

Each project gets a pixel mark that appears next to it everywhere in the bar. It lives in
`~/.claude/project-icons.json`, and the format is in `project-status.md`.

**Read this before you write that file.** It is very often a symlink into a checkout, and writing
through a symlink replaces it with an ordinary copy — after which the repository's version is no
longer the one in use, silently. Check first:

```bash
ls -l ~/.claude/project-icons.json
```

If it is a link, edit the file it points at. If the machine has a `project-icon` skill or
`~/.claude/project-icon.py`, use that instead of writing JSON yourself; it already knows.

---

## Writing any of these files

**Write to a temporary name and rename it into place.** A reader that catches a half-written file
shows nothing for one refresh, which is harmless — but only because the file is either the old one
or the new one and never half of each.

---

## Verify, and report what you actually did

Do not report success without evidence. For each file you created:

1. Print its contents.
2. Confirm it parses: `python3 -m json.tool < the-file`.
3. **Confirm the name is the name Clawdline looks for.** For the four path-keyed files, print
   `ls -l ~/.claude/statusline-cache/<prefix>-$(pwd | tr / -).json` and show that it exists —
   naming it by hand and getting one character wrong produces a file that is read by nothing and
   complained about by nobody. For `ghrun-`, the key is `<owner>-<repo>` from `git remote get-url
   origin`, so print that too.
4. If `.devstack.json` names a `status` command, **run it**, show what it printed, and confirm
   every process state is one of the six.
5. **Run every other command the file names**, and show it worked: `up` from stopped, then
   `status` again to show the row goes green; `restart` on one process; `logs` returning
   something. A command in that file is a button somebody will press, and an untested one is a
   button that fails in front of them.
6. For anything that is supposed to stay current, **run its update path once** and show that the
   file's modification time moved. That covers the health check's poller, the deploy's workflow
   step or hook, and whatever counts the backlog and the milestone — a status file nothing updates
   reports that everything is fine, forever, including during the outage.
7. **For the run file, watch a real run move.** This is the step that is not "did I write a
   file" — a file that parses proves nothing, because the failure this wiring actually has is a row
   that stops moving or lands on the wrong state. Start whatever this project has that takes
   minutes — its tests, its build, an import, an encode, whichever you wired — and while it is still
   going:

   ```bash
   run=~/.claude/statusline-cache/run-$(pwd | tr / -).json
   python3 - "$run" <<'PY'
   import json, sys, time
   row = json.load(open(sys.argv[1]))
   age = time.time() - row.get("updated_at", 0)
   print(row["state"], row.get("phase", ""), "updated", round(age), "s ago,",
         "ceiling", row.get("stale_after", 900), "s")
   PY
   ```

   Read it twice, a few seconds apart, and show that `updated_at` **moved**. A `running` row whose
   age is already past its ceiling is a row every reader ignores, and one that never moves is what
   a dead producer leaves behind. If the producer names phases, let it cross one and show `phase`
   changing too — that is the difference between a row that is alive and a row that is merely
   recent. Then show all three endings, because they are three different paths through the wiring
   and a good day only exercises one of them:

   - let a run finish and show the state landing on `ok`;
   - **make one run fail on purpose** — break an assertion, or put `exit 1` where the tests are —
     and show the file landing on `fail`. This is the check that catches the mistake everybody
     makes: a producer that writes the tick unconditionally is green on a good day, green on a bad
     day, and wrong only when it matters;
   - interrupt one with `Ctrl-C` and show it landing on `fail` rather than staying `running`.

   The last two are the half of this wiring that only shows up on the bad day, which is the half
   nobody checks unless a list tells them to.
8. **Say which of the seven you did not write, and which of those were nothing to say rather than
   nothing done.** A project with no deployed service has no health check, and that is a complete
   answer; a project whose backlog you could not find a source for is an open item. They read
   identically in a report that only lists what was created.

Then tell the user, in plain terms: what now appears in their bar, what appears only on the browser
page and a paired phone (the milestone, and anything whose artifact they will open from there),
what they still have to do themselves (a cron entry, a workflow change, granting trust the first
time they press ⌘S), and anything you deliberately skipped and why.

**If any part of the two contract pages left you guessing, say which part.** That is a defect in
the documentation and the maintainers want to hear about it.
