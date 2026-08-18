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
healthy, how far a deploy has got, and how much is in its backlog.

**It reads files. It never calls you.** There is no SDK, no package, no webhook to register, and
nothing to add to the project's dependencies. Every integration below is "write a small JSON file
where Clawdline looks". A project with none of them still works; it simply has less to say.

Two pages are the authority, and where this page and they disagree, they win:

- **[docs/devstack.md](devstack.md)** — the servers a project declares
- **[docs/project-status.md](project-status.md)** — the mark, the health check, the deploy, the backlog

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

Several filenames below contain the project's path with `/` turned into `-`. For
`/Users/you/code/thing` that is `-Users-you-code-thing`, so the file is
`health--Users-you-code-thing.json`. **The doubled dash is not a typo**: the prefix ends in one
and the path begins with one.

---

## The shortcut: is claude-bestiary installed?

Three of the four things below — the health check, the deploy, the mark — are small files that
have to be *kept current*, and keeping them current is most of the work.
[claude-bestiary](https://github.com/sainteye/claude-bestiary) already does it. It writes these
files for its own terminal status line, and Clawdline reads the same ones, so installing it
connects three of four with nothing left for you to maintain.

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

`state` is `ok` or anything else, and anything else is red. `label` is what to show — usually the
domain.

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

## 4. The mark and the colour

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
3. If `.devstack.json` names a `status` command, **run it**, show what it printed, and confirm
   every process state is one of the six.
4. **Run every other command the file names**, and show it worked: `up` from stopped, then
   `status` again to show the row goes green; `restart` on one process; `logs` returning
   something. A command in that file is a button somebody will press, and an untested one is a
   button that fails in front of them.
5. For anything that is supposed to stay current, **run its update path once** and show that the
   file's modification time moved.

Then tell the user, in plain terms: what now appears in their bar, what they still have to do
themselves (a cron entry, a workflow change, granting trust the first time they press ⌘S), and
anything you deliberately skipped and why.

**If any part of the two contract pages left you guessing, say which part.** That is a defect in
the documentation and the maintainers want to hear about it.
