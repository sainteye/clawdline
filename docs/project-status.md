# Telling Clawdline about your project

The bar's bottom edge answers "where does this text go". By default it can work most of that out
on its own — the repository name, the branch, how much is uncommitted, all from `git`. The rest
comes from files you write, and there are **seven kinds of them**:

| what it says | where it lives |
|---|---|
| the mark and the colour | `~/.claude/project-icons.json` |
| a deploy or CI run in flight | `~/.claude/statusline-cache/ghrun-<owner>-<repo>.json` |
| anything long running here: a test, a build, an import, an encode | `~/.claude/statusline-cache/run-<path>.json` |
| a backlog | `~/.claude/statusline-cache/backlog-<path>.json` |
| a finite milestone | `~/.claude/statusline-cache/milestone-<path>.json` |
| a health check | `~/.claude/statusline-cache/health-<path>.json` |
| the dev stack: servers, ports, start/stop/status | `.devstack.json`, in the repository |

Six of them are on this page. The seventh, `.devstack.json`, has [a page of its
own](devstack.md) — it is the only one that lives inside the repository, the only one that names
commands rather than reporting a state, and much the largest format. It is listed here anyway,
because this is the page somebody reads to find out what a project can say about itself, and a
list that leaves one out is how a project ends up with six of seven and nobody noticing.

An agent doing the wiring wants [connect.md](connect.md) instead, which walks all seven in order
and ends with how to check its own work. This page is the contract the two of them share.

Clawdline **only reads** them. Something else has to keep them current, and that something can be
a cron job, a git hook, a shell one-liner, the test script itself, or
[claude-bestiary](https://github.com/sainteye/claude-bestiary), which is the implementation these
formats came from. This page is the contract; anything that writes these files works.

Working examples of every file are in [`examples/`](examples/), and the test suite parses them,
so what is written here cannot quietly stop matching what the app reads.

---

## The mark and the colour

`~/.claude/project-icons.json`

```jsonc
{
  "projects": {
    "/Users/you/code/atrium": {
      "label": "atrium",
      "art": {                                  // hand-drawn
        "accent": "#5CBBA1",                    // tints the project's name in the bar
        "bg": "#2F6B5E",                        // what "." means
        "palette": { "W": "#EEF6F4" },
        "rows": [".WWWWW.", ".W...W.", ".W.W.W.", ".W...W."]
      }
    },
    "/Users/you/code/cairn": {                  // or generated
      "label": "cairn", "hue": 3, "tone": 1, "shape": 35
    }
  }
}
```

Two ways to have an icon:

- **`art`** — draw it. Rows of characters; `.`, a space or `·` is the background; every other
  character comes from `palette`. Rows need not be the same length, and an unrecognised character
  falls back to the background rather than leaving a hole.
- **`hue` / `tone` / `shape`** — let it be generated. `hue` 0–15 around the colour wheel, `tone`
  0 or 1, `shape` 0–97 (three ear pixels, three leg pixels, and which row the eyes sit in). Cheap
  to assign automatically, and distinct enough to tell a dozen projects apart.

A row is found by the **longest** registered path containing the session's directory, so
`/code/atrium` covers `/code/atrium/frontend`, and `/code/atrium/backend` can still have its own.

With no file at all, the colour and the shape are derived from the path. That is stable across
launches, which is the only property that matters when the point is recognising a project.

**Do not write this file from more than one program.** It is usually a symlink into a checkout,
and writing through a symlink replaces it with a plain copy — after which the repository's version
is no longer the one in use.

---

## Two readers, one set of files — which is which

These files are read by **two different programs**, and the page you are on documents only one
of them. That distinction is the first thing to get straight, because it decides which fields you
need:

| | reads | documented in |
|---|---|---|
| **Clawdline** — the bar | `state`, `label`, `url`, `started_at`, `typical_seconds`, and from `run-` also `phase`, `updated_at`, `stale_after` and `log` | this page |
| **[claude-bestiary](https://github.com/sainteye/claude-bestiary)** — the terminal status line | those, **plus** `producer`, `steps`, `sha`, `head_in_run` | [its own docs](https://github.com/sainteye/claude-bestiary/blob/main/docs/producers.md) |

**Write for the one you want to see it in.** A file with only the fields on this page is complete
for Clawdline and will simply show less on the status line; a file written for the status line is
a superset and Clawdline ignores what it does not know. Neither reader fails on a field it has
never heard of.

The one that catches people is `producer`. Clawdline does not read it at all — but the status line
uses it to decide whether *your* local deploy or *its* GitHub poller owns the file, and a local
producer that omits it gets quietly overwritten a few seconds later. If you are writing a deploy
script, read the other page before you write this file.

`run-<path>.json` is the exception to the whole of this section, and the exception is a claim about
readers rather than a report on them. **Any reader that adopts this format is expected to read it
out of these fields and to apply the staleness ceiling below**, because that ceiling is a reader's
job rather than a producer's: a producer killed with `kill -9` cannot retract its own row, so a
reader that skips the ceiling shows a run that finished an hour ago and no producer can tell it
otherwise. A rule only one reader honours is a rule the other reader's users do not have. One file,
one set of rules, wherever it is being drawn — which is a contract this page is asking for, not a
description of how many programs have implemented it today.

---

## What is happening right now

`~/.claude/statusline-cache/`, and five of the seven live here: a deploy, a long local operation, a
backlog, a milestone, a health check. Each file is optional; absent means the bar has one less thing
to show.

**Two rules hold across every file on this page**, and the sections below rely on them rather than
restating them:

- **A malformed value is an absent one.** Each format names the few fields it cannot do without —
  `state` and `updated_at` for a `running` local run, `total` for a backlog, `total` and `complete`
  for a milestone — and everything else is optional, so a format that grows a key does not break a
  reader that has not heard of it. A field carrying the wrong type is treated exactly as if it were
  not written: it falls back to that field's documented default where the format has one, and where
  it has none, the row is malformed and is not drawn. There is no per-field exception to that, and
  a reader that invents one is the reason two readers disagree about the same file.
- **A value nobody recognises means "say nothing".** Not a cross, not a red dot, not an error the
  user gets told about — a row that silently is not there. A state you have not heard of is not a
  failure, and the mark drawn for one is always wrong: this is why `ghrun-` has a `none` at all, and
  it is the rule for every file here rather than a quirk of the file it was first written for.

### A deploy in flight — `ghrun-<owner>-<repo>.json`

```jsonc
{
  "state": "running",                 // running | ok | fail
  "label": "deploy",
  "started_at": 1786925931,           // unix seconds
  "typical_seconds": 800,             // how long this workflow usually takes
  "url": "https://github.com/you/atrium/actions/runs/31981652530"
}
```

`running` draws a bar from elapsed against typical, plus `12m 20s/13m 20s`; `ok` and `fail` draw a
tick or a cross. The whole chip is a link to `url` — a run you cannot open is a number you have to
go and look up somewhere else, which is most of the reason nobody looks.

There is a fourth value, and a producer will emit it constantly: **`none` means there is nothing to
say** — no workflow, no run on this branch yet, `gh` not installed, the workflow disabled. It draws
nothing at all. A `why` field beside it carries which of those it was, for a person reading the
file rather than for the bar:

```json
{ "state": "none", "why": "no-runs", "updated_at": 1787056878 }
```

**A reader that has not heard of `none` draws a cross for a project that simply has no CI**, which
is a red mark that is always wrong. That is where the second shared rule above came from, and it is
why it is written as a rule for all of these files rather than as an allowance this one file makes:
any state a reader does not recognise means "say nothing".

`<owner>-<repo>` comes from the repository's `origin` remote — **of the repository you want to
see it in**, which is not always the one that deployed.

A deploy script in one repository that publishes another is ordinary (`./deploy web` in one place
putting a different repository on the wire), and the filename cannot express that. It is keyed to
where somebody will be *sitting* when they want to know, so: write the file for the repository you
work in. If both are places you work, write both — the file is a few hundred bytes and two copies
of the truth is better than one copy in the wrong place.

### A long local operation in flight — `run-<path>.json`

The deploy above happens on somebody else's machines. This one is **the progress protocol for work
happening here**: one small file, written by whatever is taking the time, so that something started
in one tab is a bar you can watch from another.

**A test run and a build are the worked example, not the definition.** `label` and `phase` are free
text and nothing on either side of this file knows what they mean, so a lint pass, a data import, a
schema migration, a long encode, a model download and a deploy script of your own all fit it exactly
as `./test.sh` does. If it takes long enough that somebody would wonder how much is left, it belongs
here.

**That includes the assistant in the next tab.** An agent about to run something that takes four
minutes has, until it writes this file, no way to say so except by printing into a terminal nobody
is watching — and what the person is left with is the one question this file exists to answer: *how
much longer?* If you are an assistant reading this page and what you are about to start is slow,
this is the tool for showing the person where it has got to.

**The short way, and the one to reach for first.** This repository ships a helper,
`Resources/clawdline-progress.sh`, which writes the file correctly in two shapes:

```sh
# wrap the slow thing — nothing inside it changes
clawdline-progress run --label lint --typical 120 -- ./scripts/lint.sh

# or source it into a script that has phases worth naming
. "$CLAWDLINE_PROGRESS"
progress_start --label test --typical 288
progress_phase compiling
```

The wrapper form is the whole of what somebody who just wants a progress bar has to know: the
command it runs is not edited and knows nothing about any of this, and the state the row lands on is
that command's own exit status rather than a line somebody remembered to write. The sourced form is
for a script that already knows what its own phases are called; `$CLAWDLINE_PROGRESS` is wherever
your copy of the helper is, and if nothing on the machine sets it, set it yourself — sourcing needs
a path to that file and nothing else.

**Why a helper at all, when the format below is ten lines of JSON.** Because the JSON is the easy
half. The hard half is the shell around it, and it fails in the direction that looks like success:
two separate agents, each holding this whole contract, got the traps wrong on their first attempt on
this machine's `bash 3.2.57`. Measured today against `/bin/bash` 3.2.57(1)-release, one script each:

- **An `EXIT` trap on its own reads a killed run as a clean one.** A script whose only handler is
  `trap 'finish "$?"' EXIT`, sent `TERM`, runs that handler with `$?` set to `0` — so the last thing
  it writes is `ok`, and the bar reports a tick for a run somebody killed.
- **`set -e` does not fire `ERR` inside a function.** A command failing inside one ends the script
  without the `ERR` trap running at all unless `set -E` is on too, so a producer that hangs its
  `fail` on `ERR` writes nothing and leaves `running` behind for the ceiling to clear up.
- **A signal handler that returns lets the run declare success.** `trap 'finish fail' TERM` with no
  `exit` in it: the handler runs, the script carries on from where the signal interrupted it,
  reaches its own `exit 0`, and the *last* write is `ok`.

That is a fact about shell rather than about those two agents, and it is the argument for the
helper. **The format stays open regardless** — anything may write this file, and that openness is
the whole reason these formats are written down at all — but a producer written by hand has to get
all three of those right, and nothing tells it when it has not.

```jsonc
{
  "state": "running",                        // required — running | ok | fail | none
  "label": "test",                           // free text — `lint`, `import`; drawn as written
  "phase": "compiling",                      // optional free text, drawn in place of the percentage
  "started_at": 1757040000,                  // unix seconds
  "typical_seconds": 288,                    // optional — measured; leave it out if you have not
  "updated_at": 1757040100,                  // required while running — keep it moving
  "stale_after": 900,                        // optional seconds; 900 when absent, 0 means "expire now"
  "log": "/tmp/clawdline-tests-8d.log",      // optional, a path for a person to open
  "holder": "clawdline-8d",                  // optional, which session started it
  "tree": "/Users/you/code/atrium"           // optional, which checkout it is running in
}
```

`running` draws a bar from elapsed against typical, the same one a deploy draws; where a deploy
shows how far through it is, this one shows `phase` verbatim when the producer sets one. `ok` and
`fail` draw a tick or a cross.

**`typical_seconds` is optional, and a producer that has not measured itself should leave it out
rather than guess.** It is the only thing the bar is drawn against, so an invented number is a bar
that is confidently wrong — full four minutes into a twenty-minute job, then pinned at the end for
the rest of it — and no reader can tell an invented number from a measured one. Leaving it out costs
the bar and nothing else: the row still names what is running, `phase` still says where it has got
to, and the bar stays empty instead of lying. This repository does it both ways on purpose:
`./test.sh` writes `288`, which is one measured green run recorded in
[`suite-runtime.md`](suite-runtime.md), and **`./build.sh` has never been timed and writes no
`typical_seconds` at all** — a decision, not an oversight, and one to copy rather than to fix. What
an absent one looks like is worth seeing before you choose it: the Mac footer draws the empty bar
and an elapsed clock with nothing to count against — `1m 4s/0s` — and the browser page draws the
empty bar with `…` where the percentage would be.

`none`, and every state a reader does not recognise, draws nothing at all — the second shared rule
above, and this file is not an exception to it.

**`state` is required always. `updated_at` is required while that state is `running`, and only
then. Every other field is optional.** `updated_at` is what the ceiling below is measured against, so
a `running` row that does not carry a usable number there is malformed rather than merely thin, and
**is not drawn at all** — there is nothing to hold it to a liveness ceiling with, and the ceiling is
the whole reason this format exists rather than reusing `ghrun-`. Falling back to `started_at` looks
kinder and is not: it makes "required" mean nothing, and it draws a run whose producer died before it
ever said it was alive.

**A finished verdict runs the other way, and that is why `updated_at` is required of a `running`
row and of nothing else.** `ok` and `fail` are not alive and do not decay, so they are drawn however
old they are and whether or not `updated_at` is there: a verdict is not a claim about something still
moving, so there is nothing to hold it to a ceiling with. "The last run of this tree failed" stays
true until the next run overwrites the file — and every run rewrites it, so a verdict only survives
while there has been nothing newer to say. The deploy chip
beside it already behaves this way, and two neighbouring chips with two expiry rules is a thing no
reader will ever get right.

Write to a temporary name and rename it into place, as with every other file on this page.

`label` and `phase` are the producer's own words and are drawn **verbatim, in every language** —
`test`, `build`, `compiling`, `編譯中`. Nothing here is translated, so a producer that writes its
own vocabulary gets that vocabulary in the bar. A row with no `label` is drawn as `run`, and an empty
`phase` is the same as no `phase` at all.

**Optional does not always mean free.** `started_at` is optional the way the required list above
says — a row without it is still drawn — but it is what the bar is measured from, so a `running` row
that omits it draws a *full* bar on the Mac and an unknown one on the browser page. Write it. It is
one call to `date +%s` and it is the difference between a bar and a decoration.

Working example: [`examples/run--Users-you-code-atrium.json`](examples/run--Users-you-code-atrium.json).

**Three things about it are deliberately not the way `ghrun-` does them.** Each is a decision, and
each is the kind of decision the next reader assumes was an accident and then "fixes":

- **It is keyed by the working directory, not by the git remote.** `<path>` is the project's
  directory with `/` turned into `-`, exactly as `backlog-`, `milestone-` and `health-` are. A
  deploy is a fact about a repository, so `ghrun-<owner>-<repo>` is right for it; a local run is a
  fact about **one checkout**. This machine routinely has several worktrees of one repository
  compiling at once, and keyed by the remote they would be one row overwriting itself, each run
  erasing the last. Two worktrees are two rows.
- **There is no `producer` field, and one should not be added.** `ghrun-` needs it because it has
  two writers competing for the same file — a local deploy script and claude-bestiary's GitHub
  poller — and the field is how they arbitrate. Nothing polls for local runs: the only writer is
  the script that is running, and it is writing about itself. A field for arbitrating between
  writers that do not exist is a field somebody will one day feel obliged to honour.
- **The staleness ceiling is in the reader.** A run killed with `kill -9` writes nothing on the way
  out, and there is no poller to tidy up after it, so a `running` row would otherwise spin in the
  bar forever — which is exactly what claude-bestiary's own `docs/producers.md` says about
  `ghrun-`. So **a reader ignores a `running` row whose `updated_at` is older than `stale_after`**,
  900 seconds when that field is absent or carries something that is not a number. The instant equal
  to the ceiling is still fresh and one second past it is not; `0` is a value rather than a missing
  field, so a producer that writes `0` gets a row that expires the moment it is written, and a
  negative one the same. Putting the ceiling in the reader rather than asking every producer to
  clean up after itself means every reader gets it, including the ones nobody has written yet.

That last rule is what makes `updated_at` the one field beyond `state` a producer must not leave
out: it is what staleness is measured against, and a `running` row that never says when it was last
touched cannot be told from one whose process died an hour ago — so it is not drawn. Write it at
every phase boundary, and more often than `stale_after` for any phase that runs longer than that.

### A backlog — `backlog-<path>.json`

```jsonc
{
  "total": 44,
  "lanes": { "now": 2, "scheduled": 6, "waiting": 17, "drop": 19 },
  "artifact": "/Users/you/code/atrium/backend/artifacts/backlog.html"
}
```

`≡44` with `now 2` after it in the accent colour, linking to `artifact` — a local file is fine.
Only `now` is highlighted: a backlog's enemy is not being long, it is being unread.

### A finite milestone — `milestone-<path>.json`

```jsonc
{
  "total": 8,
  "complete": 3,
  "waiting_on_user": 2,
  "updated_at": 1788105600,
  "artifact": "/Users/you/code/atrium/backend/artifacts/launch-milestone.html"
}
```

Milestones and backlogs answer different questions. A backlog is an unbounded inventory; a
milestone is a finite definition of done. `waiting_on_user` stays separate from unfinished agent
work so a credential, spend approval or product decision is visible instead of looking like slow
implementation.

**Both artifacts — this one and the backlog's — must be a regular HTML file inside the project's
directory.** Clawdline serves them back through an authenticated, same-origin route with scripts
disabled, so the Links sheet works from a paired phone without exposing the Mac's filesystem path.
The status file is a pointer, not an authorization mechanism: paths outside the project and symlink
escapes are refused.

### A health check — `health-<path>.json`

**Read this first: you probably do not have to write this file.**
[claude-bestiary](https://github.com/sainteye/claude-bestiary) polls and writes it for you. Put a
`health` block in that project's entry in `~/.claude/project-icons.json` — the registry above —
and its status line refreshes the check in the background whenever the file goes stale. Nothing to
schedule, no cron entry, no hook. The format below is for a machine that does not run it.

```jsonc
"health": {
  "url": "https://example.com/health",            // what to poll
  "label": "prod",                                // what to call it on the line
  "site": "https://example.com/",                 // where a click should go instead
  "expect": { "status": "ok", "database": true }, // what a healthy answer says
  "version_key": "commit"                         // the field carrying the deployed revision
}
```

`expect` is what separates healthy from merely answering: a 200 that says `{"database": false}` is
not health, and a check that reads only the status code reports that everything is fine during the
outage. **One trap worth naming**: with `expect` set, an endpoint that answers something that is
not JSON is judged `sick` — which is usually right, and is a permanently red light if you pointed
it at an HTML page by mistake.

`version_key` is what gives you `● prod ↑3`: the live service is three commits behind the code you
are holding. If the project's health endpoint does not report its own build, adding that field is
one line and it answers "is what I am looking at deployed".

What lands in the file:

```jsonc
{ "state": "ok", "label": "example.com", "ms": 1077, "http": 200,
  "version": "dd138a4dea27", "checked_at": 1787056356 }
```

Only `state` and `label` are read for the dot; the rest is there for anything else that wants it,
and a failure carries `detail` and `bad` as well — what went wrong, and which of the `expect` keys
did not hold.

**`state` has more than two values, and the difference between two of them matters.**

| state | meaning |
|---|---|
| `ok` | answering, and `expect` holds |
| `sick` | **the service is broken** — unreachable, a bad status, or `expect` did not hold |
| `offline` | **this machine has no network.** The service is probably fine |
| `unknown` | not checked yet |

`sick` and `offline` draw alike and mean opposite things: one is a thing to go and fix, the other
is a thing to ignore until the café's wifi comes back. Anything outside this vocabulary draws
nothing rather than a red dot — the second shared rule above, which this file follows for the same
reason `ghrun-` does: a project whose check has never run is not a project that is down.

`<path>` — in this file name and in `run-`, `backlog-` and `milestone-` — is the project's
directory with `/` turned into `-`, the same shape Claude Code uses for its own project folders:
`/Users/you/code/atrium` → `-Users-you-code-atrium`. The prefix ends in a dash and the path begins
with one, so the real file name has two: `health--Users-you-code-atrium.json`.

---

## Writing them

Nothing here is Clawdline-specific — these are small JSON files with a name and a shape. A
sketch, in whatever language you like:

```bash
# after a push, record the run that just started
gh run list --limit 1 --json databaseId,status,url,workflowName \
  | jq '{state: "running", label: .[0].workflowName, url: .[0].url,
         started_at: now|floor, typical_seconds: 800}' \
  > ~/.claude/statusline-cache/ghrun-you-atrium.json
```

Write to a temporary file and rename it into place. A reader that catches a half-written file
shows nothing for one refresh, which is harmless — but only because the file is either the old
one or the new one, never half of each.

`run-<path>.json` is the one whose producer is not a cron job or a hook but **the thing that is
taking the time**, and it is the one with a helper: `clawdline-progress run --label lint --typical
120 -- ./scripts/lint.sh` wraps a command that knows nothing about any of this, and
`. "$CLAWDLINE_PROGRESS"` puts `progress_start` and `progress_phase` inside a script that does.
Writing it by hand is three lines at the top of the script, one at each phase boundary, and traps on
`EXIT`, `INT` and `TERM` that each `exit` rather than return — so an interrupted run writes `fail`
instead of a tick, and instead of leaving `running` behind for the staleness rule to clear up
fifteen minutes later. [connect.md](connect.md#4-the-long-local-run) has both, and the sketch in
full.

If you would rather not write any of this yourself,
[claude-bestiary](https://github.com/sainteye/claude-bestiary) already does: it keeps these files
current for its own terminal status line, and Clawdline reads the same ones. Installing it turns
all of the above on at once.
