# Telling Clawdline about your project

The bar's bottom edge answers "where does this text go". By default it can work most of that out
on its own — the repository name, the branch, how much is uncommitted, all from `git`. The rest —
a mark and a colour, a deploy in flight, a backlog, a health check — comes from files you write.

Clawdline **only reads** them. Something else has to keep them current, and that something can be
a cron job, a git hook, a shell one-liner, or
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
| **Clawdline** — the bar | `state`, `label`, `url`, `started_at`, `typical_seconds` | this page |
| **[claude-bestiary](https://github.com/sainteye/claude-bestiary)** — the terminal status line | those, **plus** `producer`, `steps`, `sha`, `head_in_run` | [its own docs](https://github.com/sainteye/claude-bestiary/blob/main/docs/producers.md) |

**Write for the one you want to see it in.** A file with only the fields on this page is complete
for Clawdline and will simply show less on the status line; a file written for the status line is
a superset and Clawdline ignores what it does not know. Neither reader fails on a field it has
never heard of.

The one that catches people is `producer`. Clawdline does not read it at all — but the status line
uses it to decide whether *your* local deploy or *its* GitHub poller owns the file, and a local
producer that omits it gets quietly overwritten a few seconds later. If you are writing a deploy
script, read the other page before you write this file.

---

## What is happening right now

`~/.claude/statusline-cache/`. Each file is optional; absent means the bar has one less thing to
show. Every field is optional on the way in too, so a format that grows a key does not break a
reader that has not heard of it.

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

**A consumer that has not heard of `none` will draw a cross for a project that simply has no CI**,
which is a red mark that is always wrong. Treat any state you do not recognise as "say nothing"
rather than as failure.

`<owner>-<repo>` comes from the repository's `origin` remote — **of the repository you want to
see it in**, which is not always the one that deployed.

A deploy script in one repository that publishes another is ordinary (`./deploy web` in one place
putting a different repository on the wire), and the filename cannot express that. It is keyed to
where somebody will be *sitting* when they want to know, so: write the file for the repository you
work in. If both are places you work, write both — the file is a few hundred bytes and two copies
of the truth is better than one copy in the wrong place.

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

The artifact must be a regular HTML file inside the project's directory. Clawdline serves it back
through an authenticated, same-origin route with scripts disabled, so the Links sheet works from a
paired phone without exposing the Mac's filesystem path. The status file is a pointer, not an
authorization mechanism: paths outside the project and symlink escapes are refused.

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
is a thing to ignore until the café's wifi comes back. Anything unrecognised should draw nothing
rather than a red dot.

`<path>` is the project's directory with `/` turned into `-`, the same shape Claude Code uses for
its own project folders: `/Users/you/code/atrium` → `-Users-you-code-atrium`.

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

If you would rather not write any of this yourself,
[claude-bestiary](https://github.com/sainteye/claude-bestiary) already does: it keeps these files
current for its own terminal status line, and Clawdline reads the same ones. Installing it turns
all of the above on at once.
