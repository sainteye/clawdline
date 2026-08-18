# The dev stack a project describes

Most projects have a handful of long-running things that make up "the app running locally" — an
API, a front end, a worker, a tunnel. They are started from a terminal tab, and that tab then
belongs to them for the rest of the day. Restarting one of them means finding the tab, stopping
all of them, and starting all of them again.

Clawdline can show you what those processes are doing, and start, stop and restart them, in the
same place you already type. It does that by reading one file out of your repository.

**Clawdline never starts a process of its own.** It reads `.devstack.json` and runs the commands
that file names. That single rule is why this works at all:

- **The servers outlive the app.** Anything Clawdline spawned would die with Clawdline — on quit,
  on update, on crash. A dev stack whose life is tied to a text field is worse than one tied to a
  terminal tab; at least the tab is visible while it dies.
- **It works for projects nobody here has seen.** process-compose, Overmind, pm2, Docker Compose,
  systemd, a Makefile with PID files — they all reduce to "a command that prints state" and "a
  command that restarts something". Nothing in Clawdline knows which one you use.

This page is the contract. Anything that writes such a file works, exactly as with
[project-status.md](project-status.md).

---

## The file

`.devstack.json`, at your repository root. Clawdline looks for it from the session's directory
upwards, so a monorepo can put one beside each deployable instead of one at the top.

```jsonc
{
  "version": 1,
  "name": "cairn",

  // Reading state
  "status": "make stack-status",          // prints the state document, below

  // Acting. {process} and {lines} are substituted.
  "up":      "make stack-up",
  "down":    "make stack-down",
  "restart": "make stack-restart P={process}",
  "logs":    "make stack-logs P={process} N={lines}",
  "attach":  "make stack-attach",         // opens a full view for a human

  // Tier 0: the processes this project has and the ports they listen on. Clawdline probes
  // these itself, so a file with nothing but these gets a live row — see below.
  "processes": [
    { "name": "api", "port": 8002 },
    { "name": "web", "port": 3001 },
    { "name": "tunnel", "url": "https://example.dev" }
  ]
}
```

**`processes` and `status` can both be present, and they answer different questions.** The
declared list is what the project *has*; a `status` command is what it is *doing right now*. When
a trusted `status` runs, its answer is used. Until then — and for a project that was never
trusted — the declared ports are probed directly, which executes nothing. So the sensible file
has both: the list is what still works before anybody has granted trust.

**Every field except `name` is optional, and `name` falls back to the directory.** A file with a
`up` and nothing else gets you a button. A file with `{}` in it is valid and does nothing. An
unknown field is ignored rather than fatal — a reader that throws the whole file away because one
key moved is worse than one that shows the parts it still recognises.

### Substitution, not concatenation

`{process}` is replaced where you put it, because `make stack-restart P=api` and
`overmind restart api` do not put the process name in the same place. The value is shell-quoted.

Restarting everything omits the process, and then **the whole whitespace-delimited word
containing the placeholder is dropped** — `make stack-restart P={process}` becomes
`make stack-restart`, not `make stack-restart P=`.

### What commands must guarantee

> **A command finishes only once the thing it changed is actually up.**

This is the contract's central demand and the reason a restart can report success or failure
instead of "sent". A `restart` that returns the moment it has signalled something leaves the
caller — you, or Claude Code — believing a broken deploy succeeded, and the next thing either of
you does is built on that belief.

When it fails, exit non-zero and **print the failing process's log to stderr**. Whoever is looking
at a red mark always wants that next, and it is what lets an agent go straight from "it is broken"
to fixing it.

---

## The state document

What `status` prints on stdout:

```jsonc
{
  "state": "partial",              // running · partial · stopped
  "updated_at": 1786949616,
  "processes": [
    { "name": "api",       "state": "healthy",   "port": 8002, "pid": 7970 },
    { "name": "web",       "state": "healthy",   "port": 3001, "pid": 8243 },
    { "name": "tunnel",    "state": "healthy",   "url": "https://example.dev" },
    { "name": "build-web", "state": "exited",    "exit_code": 1,
      "error": "Error: Cannot find module 'next/font/google'" }
  ]
}
```

`name` and `state` are required on a process. Everything else — `port`, `url`, `pid`,
`exit_code`, `error` — is optional, and a missing field means one less thing on screen, not a
parse failure.

**The top-level `state` is a different vocabulary from the per-process one**, which is easy to
miss because they share a key name. It is one of:

| top-level state | meaning |
|---|---|
| `running` | everything the project declares is up |
| `partial` | some of it is |
| `stopped` | none of it is |
| `unknown` | nobody has been able to look |

`partial` and `unknown` exist only here; `healthy`, `starting` and the rest exist only on a
process. **You may omit the top-level `state` entirely** — it is derived from the processes, and
deriving it is the better answer, because a writer that reports `running` while one of its own
processes says `exited` has told two different stories in one document. Send it only if you know
something the process list does not.

`unknown` is the one that has to exist and cannot be derived. A project that declares no ports and
whose `status` command has not been trusted yet must not be reported as `stopped`: it is very
likely running, and an indicator that says "down" about a live site is worse than no indicator at
all, because the next real outage looks identical to it.

### The six process states

A small, closed vocabulary, so an implementer does not have to guess:

| state | meaning |
|---|---|
| `healthy` | up, and its readiness probe passes |
| `running` | up, with no probe configured — the honest answer when nobody knows |
| `starting` | up, probe not passing yet |
| `completed` | a one-shot that finished **on purpose** (a build, `docker compose up -d`) |
| `exited` | finished when it should not have — non-zero exit, or a daemon that died |
| `stopped` | not running |

`completed` earns its place: without it, every successful build draws a red mark, and a red mark
that is usually wrong is one nobody reads on the day it is right.

### `error` travels with the state

Put the last few lines of the failed process's log in `error`. It is carried here rather than left
for a follow-up `logs` call because the reader always wants it next — and because that one field
is the difference between an agent that reports a red mark and one that fixes it.

---

## Three ways to adopt this

You do not need a supervisor, or any new software, to be visible.

The tiers below are about how much machinery you already have. **What a reader of the panel can
actually do is decided by which keys are in the file**, which is a different question and the one
worth answering first:

| the file names | the panel offers |
|---|---|
| `processes` with ports | which are up, ports as links. **Nothing to press.** |
| …and `status` | the project's own answer — readiness, exit codes, the error from a crash |
| …and `up` / `down` / `restart` | **buttons** |
| …and `logs` | the failed process's log, next to its red mark |

A file with `status` and no `up` is a legitimate thing to write and a frustrating thing to arrive
at by accident: the row reports that two of three services are down and offers nowhere to press.
If that is what you meant, good. If you stopped there because the project had no start command
lying around, **write one** — that is a `Makefile` target, not a change to this format.

### Tier 0 — declare ports, run nothing

```jsonc
{ "version": 1, "name": "myapp",
  "up": "make dev",
  "processes": [ { "name": "api", "port": 8002 },
                 { "name": "web", "port": 3001 } ] }
```

No `status` command. Clawdline connects to each declared port itself and reports `running` or
`stopped`. Coarser than a readiness probe, and it is the truth for the overwhelming majority of
dev stacks. **Four lines, no dependency, works today.**

A process may carry a `url` here too, and should when it is one — a tunnel, a preview host. Ports
and addresses are drawn as links, so checking that a site is really up is a click rather than a
number read off a row and typed into a browser.

### Tier 1 — a `status` command

Add `"status"`. It prints the state document, and now you can say anything you like: health,
uptime, exit codes, the error that killed a process. A shell script is a perfectly good
implementation; so is a fifteen-line adapter that translates your supervisor's own output.

### Tier 2 — something already keeps state current

When a supervisor is already running, spawning a subprocess to ask it is the expensive part. Point
`status` at a file a daemon keeps fresh, or write that file to
`~/.claude/statusline-cache/stack-<path>.json` — the same convention the other status files use,
with `/` turned into `-`.

Tier 0 exists so that this format is not an integration for people who already run a supervisor.
Without that rung, adoption starts with "first install process-compose", and a contract with an
installation step in front of it is not a contract.

---

## Trust

`.devstack.json` names commands and Clawdline runs them. Cloning a repository must not be enough
to make that happen. This is the same exposure that made editors grow a "do you trust this
workspace" prompt, and it gets the same answer here.

The split that keeps it usable:

- **Reading declared ports needs no trust.** Connecting to a TCP port executes nothing, so an
  untrusted project still shows real state through Tier 0.
- **Running any of its commands is gated — including `status`.**

Trust is recorded per repository path together with a fingerprint of the file's bytes, in
`~/.config/clawdline/trusted-stacks.json`. **Editing the file revokes it**, because the edit is
exactly where a command would be added.

A project that declares no ports and has not been trusted reports `unknown`, not `stopped`. It is
very likely running, and an indicator that says "down" about a live site is worse than no
indicator at all — the next real outage looks identical to it.

---

## Writing one

Nothing here is Clawdline-specific. A sketch, for a project with no supervisor at all:

```bash
#!/bin/sh
# stack-status — Tier 1 with nothing but lsof and a here-doc.
up () { lsof -ti tcp:"$1" >/dev/null 2>&1 && echo running || echo stopped; }
cat <<EOF
{"processes": [
  {"name": "api", "state": "$(up 8002)", "port": 8002},
  {"name": "web", "state": "$(up 3001)", "port": 3001}]}
EOF
```

**Setting a project up?** [devstack-adopting.md](devstack-adopting.md) is the working guide — the
adapter to copy, and ten things that cost real downtime to learn. Read it before writing files.

Working examples of every file on this page are in [`examples/`](examples/) —
[`devstack.json`](examples/devstack.json) (Tier 1),
[`devstack-tier0.json`](examples/devstack-tier0.json),
[`devstack-state.json`](examples/devstack-state.json) — and the test suite parses them with the
same code the app uses, so what is written here cannot quietly stop matching what it reads.
