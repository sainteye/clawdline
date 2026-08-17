# Telling Clawdline about your project

The bar's bottom edge answers "where does this text go". By default it can work most of that out
on its own — the repository name, the branch, how much is uncommitted, all from `git`. The rest —
a mark and a colour, a deploy in flight, a backlog, a health check — comes from files you write.

Clawdline **only reads** them. Something else has to keep them current, and that something can be
a cron job, a git hook, a shell one-liner, or
[claude-tools](https://github.com/sainteye/claude-tools), which is the implementation these
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

`running` draws a bar from elapsed against typical, plus `12m 20s/13m 20s`; anything else draws a
tick or a cross. The whole chip is a link to `url` — a run you cannot open is a number you have to
go and look up somewhere else, which is most of the reason nobody looks.

`<owner>-<repo>` comes from the repository's `origin` remote.

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

### A health check — `health-<path>.json`

```jsonc
{ "state": "ok", "label": "example.com" }
```

A green or red dot with its label.

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
[claude-tools](https://github.com/sainteye/claude-tools) already does: it keeps these files
current for its own terminal status line, and Clawdline reads the same ones. Installing it turns
all of the above on at once.
