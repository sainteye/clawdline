# Architecture

One Go binary is both the daemon and its command line. Work always runs on your own machine. Every
client, whether the native window, a browser tab or the Cloud console, reads the same HTTP + SSE
API, which is defined once, as JSON Schema, before any code is written against it.

```text
  native shell, one per platform         a browser            Clawdline Cloud (optional)
  macOS: Swift + WKWebView               on this machine      app.clawdline.com, via the relay
          |                                   |                        |
          +------------------+----------------+                        |
                             |                                         |
                  React console (web/console)                          |
                             |                                         |
                             |  /v1 over HTTP + SSE                    |  signed, encrypted
                             |  (api/v1/*.schema.json)                 |  envelopes over WSS
                             v                                         v
  +-----------------------------------------------------------------------------------+
  |  clawdline serve, on 127.0.0.1:7727                                               |
  |                                                                                   |
  |  transport/http    the gate, the routes, the event stream                         |
  |  transport/cloud   the line to the relay, pairing, publishing session rows        |
  |  app               the broker, schedules, the three structures, Cloud commands    |
  |  domain            pure rules: sessions, tasks, work, auth, capacity, Cloud crypto|
  |  adapters          terminals, processes, transcripts, the SQLite store, push, keys|
  +-----------------------------------------------------------------------------------+
          |                      |                          |
     tmux, iTerm2        ~/.claude, ~/.codex          one SQLite file
     (drive sessions)    (read transcripts, never     in the state directory
                          write)
```

The design notes linked from this page are written in Traditional Chinese. This page is the
English map into them. [docs/README.md](README.md) lists them all.

## What does not move

The product has ten rules that any change must keep ([plan.md](plan.md) §1):

1. **Coordinate the sessions you already have.** Never take over your keyboard.
2. **Coordination stays on your machine.** Cloud carries ciphertext and holds no task state.
3. **The content key stays on your hardware.** AES-GCM and Ed25519, because they are the
   intersection that needs no extra dependency in Go and in a browser.
4. **Several machines is a Cloud feature.** The open-source product is one machine.
5. **Only online and offline are modelled.** Sleep is not.
6. **Delivered is not reviewed, and reviewed is not landed.**
7. **Receipts are durable and typed**: accepted, executed, delivered, observed, acknowledged.
8. **Claims declare write paths first**, and an overlap is refused before work starts.
9. **Claude and Codex are peers.** Either can dispatch the other.
10. **No store-and-forward.** A machine that is offline says so, loudly.

Ten design rules sit under those, each one paid for by a failure: a loop that stops must say so,
everything that accumulates has a written limit, unknown is never read as absent, and so on
([design-guidelines.md](design-guidelines.md)). The implementation decisions that apply them are in
[design-decisions.md](design-decisions.md), which wins wherever an older analysis disagrees.

## Contract first

The API is `api/v1/*.schema.json`. `go run ./tools/contract-gen` generates the Go types
(`internal/contract/zz_generated.go`) and the TypeScript types (`web/contract/src/generated.ts`)
from it. `-check` fails if either output is out of date. Route bodies are generated types on both
sides, so a field is changed in one place, the schema, and both the daemon and the console follow.

A refusal has one shape everywhere: `{"error": {"code", "message", "request_id", …}}`. Anything
extra, such as the task holding a claimed path, goes inside `error`. The code is the part a
program reads. A refusal names what it is refusing: a missing platform capability by its name, an
unsupported task field by the field.

A few shapes are not in the contract yet, and each is marked where it is used: the broker's
inventory body (a map), `/v1/strings` (its keys are the catalog), and `/v1/cloud/status`.

## Layers

| Package | Holds |
| --- | --- |
| `internal/domain/*` | Rules without I/O: session identity and state, tasks, work items, device auth, the capacity register, Cloud envelopes and crypto |
| `internal/app/*` | Use cases: the broker (`app/orchestrator`), schedules, work, Cloud commands (`app/cloudops`), and the ports they need (`app/ports`) |
| `internal/adapters/*` | One implementation per port, split by platform in file names (`*_darwin.go`, `*_windows.go`) |
| `internal/transport/http`, `internal/transport/cloud` | The ways in: routes, the gate, SSE; the Cloud line |
| `cmd/clawdline` | The binary: `serve`, and the commands that talk to a running daemon |

The rule that holds is the first one in [plan.md](plan.md) §3: **`domain` imports no adapter, no
transport and no app package**. One domain package, `domain/icon`, still reads a file itself. The second rule there, that transport does not
import adapters, is not true today: `internal/transport/http` is also where the daemon is wired
together, and the broker in `internal/app/orchestrator` uses the store and terminal adapters
directly. No guard test enforces either rule yet.

A port is one thing the platform does: `TerminalHost` (list, open, send, interrupt, close,
reveal, read the screen), `ProcessHost`, `ScreenHost`, `KeyHost` and others
(`internal/app/ports/ports.go`). A platform capability is answered by name before anything is
tried, so a machine that cannot open a child session refuses the dispatch and says which
capability is missing, rather than recording a task that then fails to spawn
(`internal/app/ports/capabilities.go`).

## Sessions

The daemon lists tmux panes, and iTerm2 sessions on macOS, and matches each terminal to the
`claude` or `codex` process running in it through the process table. A session's state comes from
evidence: what its screen shows and what its own records say. With no evidence, the state is
`unknown`. Transcripts are read from the assistant's own files under `~/.claude` and `~/.codex`.
Nothing is written there.

Provider-native subagents are part of that same read-only inventory, not broker tasks. Claude Code
puts the description written by the spawning turn in an immutable sidecar; Codex identifies an
open subagent through process evidence and puts a distinct nickname and start time in the
rollout's `session_meta` head. The daemon reads those names without reading the conversation below
the head. Claude's running state remains an inference from a recent transcript without a
completion notice; Codex's is direct evidence that the parent process still holds the rollout
open. In the console they are folded into **Session to-dos**, with only their running count on the
closed row. Broker children stay in the main session list, where each already has its own session
row, and are not drawn a second time as provider subagents.

`GET /v1/sessions` answers one inventory. `GET /v1/events` streams whole snapshots, named
`sessions` and `orchestrator`, plus `screen` frames for a session being watched. Whole snapshots,
not patches, because a view built from two sources that update at different times shows a state
that never existed ([plan.md](plan.md) §6).

## What another program writes, and this one only reads

Two of this daemon's readings come from files a *different* program leaves on the machine, and
both are drawn in the console as though they were the daemon's own. Naming them here so that an
empty cell is debugged in the right place:

| What is drawn | The file | Who writes it |
|---|---|---|
| The `.deploy` cell and a session's deploy/CI links | `~/.claude/statusline-cache/ghrun-<owner>-<repo>.json` | Whatever `statusLine.command` names in `~/.claude/settings.json`. Claude Code runs it to draw its own status line; these files are its side effect |
| A project's health and run rows | `~/.claude/statusline-cache/health-*.json`, `run-*.json` | The same tool |

On the machine this was written on that tool is
[claude-bestiary](https://github.com/sainteye/claude-bestiary), by the same person — a status line
that draws each project a creature and polls git, the deploy and the service health it needs to
draw them. Those polls are what lands in that directory, which is how Clawdline shows a deploy row
without ever reaching the network. Another machine will have a different tool there, or none.

**Clawdline never writes them**, a missing file is a legitimate answer, and `state: "none"` means
that tool found nothing worth reporting — not that the repository has no runs. So an empty deploy
cell used to have two causes wearing one face: there is no run, or nobody is writing these files
any more.

**It now says which.** The workflow file carries a `why` beside a state it has nothing to show
for, and until 2026-09-21 no line of this repository had read that key — a person asked twice why
their GitHub row was blank while `{"state":"none","why":"stale-fail"}` sat in the file explaining
itself. `deployQuiet` on `GET /v1/sessions/{id}/info` and `/links` now carries which kind of
silence it was (`no_file`, `unreadable`, `state_not_drawn`, `no_address`), the producer's own
`state` and `why` untranslated, and the file's own `updated_at` — which is how a poller that
stopped is told apart from a project between runs. The dot is unchanged: a state this daemon does
not know still draws nothing, because a red mark that is always wrong is worse than no mark. No
guard goes red when that tool stops writing, and nothing here can make one:
`internal/adapters/projectlinks/status.go` says so beside the code that reads them, and the
console's sentence for each kind is in `web/console/src/overlays/links-note.ts`.

That `why` vocabulary is the producer's and is not closed, so neither Go nor the wire maps it:
the console turns the words it knows into sentences and says an unknown one as the word it was
given. A reader that kept only what it recognised would be silent again the first time that tool
learned a new one.

## The broker

The broker is everything under `/v1/orchestrator/*`: dispatching a child session, collecting its
result, recording what landed, and the coordination around that. It lives in
`internal/app/orchestrator`. [broker.md](broker.md) lists its routes and where it deliberately
differs from the Swift app it replaces.

- **Dispatch.** The caller writes `task.json` into the task directory and posts the task id, a
  secret and the inventory generation it read. Posting without a current generation is refused as
  `stale_inventory`, and the refusal carries the current inventory, so the retry is one round
  trip. Claims are checked against every live task before a terminal opens. The daemon writes
  `CHILD.md` and types one line into the child's composer, and only once it can see a composer:
  a dialog is never typed into.
- **Collection.** A beat, every five seconds by default, reads each live task's `result.json`,
  applies timeouts, and notices a child whose terminal disappeared.
- **Completion.** A finished task is announced to the root session that asked, with a notice id
  that retries keep, until the root acknowledges it. After repeated failures the notice becomes a
  dead letter and a person is told.
- **Landing.** A delivery is `landed` only when git ancestry proves the target branch contains it.
- **Coordination.** A machine-wide coordinator role, leases that expire, waits, messages between
  sessions, handoffs of a whole line of work, root assignments, task graphs, and reclaiming
  worktrees that are proved to be landed.

The theory under the coordination routes, one `Obligation` model for waits, landings, handoffs
and dead letters, is in [coordination.md](coordination.md). The full design analysis is in
[broker-design.md](broker-design.md).

## The store

One SQLite file, `clawdline.sqlite3` in the state directory, through `modernc.org/sqlite`, which is
pure Go. There is no cgo anywhere, which is what lets one machine build every platform.

- **One write right.** Every write is one `BEGIN IMMEDIATE` transaction: the read that decides,
  the decision and the write happen inside it. SQLite's lock keeps out every other writer, whether
  that is another goroutine, the CLI or a second daemon (`internal/adapters/store/tx.go`).
- **One fact, one transaction.** Something that can be recomputed is not stored.
- **Effects after facts.** Typing into a terminal, opening a tab or making a checkout is recorded
  in an outbox, in the same commit as the fact that owes it, and runs afterwards. An effect that
  was started and cannot tell whether it happened, such as bytes typed into somebody's terminal,
  is recorded `unknown` and not repeated (`internal/adapters/store/outbox.go`).
- **One spelling of idempotency.** Request receipts are keyed on `(scope, actor, key)`. The same
  key with the same request replays the original answer, and the same key with a different
  request is refused (`internal/adapters/store/receipts.go`).
- **Everything that accumulates has a limit**, a rule for what happens when it is full, and someone
  who finds out. That is the capacity register (`internal/domain/capacity`, `GET /v1/capacity`,
  [limits.md](limits.md)). `clawdline doctor capacity --drill <row>` fills one row on purpose, in a
  throwaway directory, so you can see the warning that fires.

## Three structures

Board, backlog and a session's own to-do list are three tables in the same SQLite file, with one
`work` identity that an item keeps as it moves between them, and an append-only log of every move.
Because they share the broker's database, "the task landed", "the to-do is done" and "the board
item is done" can be one transaction instead of two stores that must agree.

| | Routes | Created by | Closed by |
| --- | --- | --- | --- |
| Board | `/v1/work/board`, `/v1/work/items/…` | a person, or a proposal a person accepted | evidence, or a person |
| Backlog | `/v1/work/backlog` | a person | a person, or a move to the board |
| Session to-do | `/v1/sessions/<id>/todos`, `/v1/orchestrator/sessions/<id>/todos` | the broker | the broker |

Proposals and decisions (`/v1/work/proposals`, `/v1/work/decisions`) are where a person joins in.
The display states on a card ("queued", "in review", "landed") are derived from evidence each time
they are read, never written back. [board-redesign.md](board-redesign.md) has the reasoning and the
lifecycle of each structure.

## Schedules

A schedule is a task template with a local wall-clock time and a catch-up window. The clock ticks
once a minute and asks only about the most recent occurrence. An interval counts from the later of
the last run and the moment this daemon first saw the schedule, which is stamped when it is read.
The rule exists because the first version fired every restored or copied schedule on the first
tick. [schedules.md](schedules.md) has the rules; [plan.md](plan.md) §3.2 has the incidents behind
them.

## Cloud

Off by default, and a failure in it never stops the daemon.

- `internal/domain/cloud`: the envelope, canonical JSON (a safe-integer subset of RFC 8785),
  AES-256-GCM with a 12-byte nonce, Ed25519 signatures, HKDF for pairing, and the replay window.
  Checked byte for byte against known-answer vectors.
- `internal/transport/cloud`: the line (`wss://…/v1/connect?role=machine`, backoff, an outbound
  spool), the machine's half of pairing, and publishing session rows for the hosted console.
- `internal/adapters/cloud`, `internal/adapters/cloudkeys`: the account client, the device roster,
  the devices this machine pinned itself, and key files (mode 0600).
- `internal/app/cloudops`: the commands a viewer can send. **A Cloud command is answered by the
  same route handler, behind the same gate, as a local request**, so there is one set of
  permission checks, not two.

Trust in a viewer is decided on this machine: a device it revoked is always refused, and a device
it pinned during pairing comes before anything the account's roster says. Two switches:
`cloud_enabled` for the line, `cloud_commands` for whether a viewer may act, re-read on every
request. [cloud-wire.md](cloud-wire.md) is the wire specification and the record of what was
measured. [remote.md](remote.md) draws the line between the free product and Cloud.

## The shell and the web

The rule: **every screen is built in the web console, and a native shell keeps only what a web
page cannot do**: its own window and its level, a global hotkey, the terminal's tab, the
clipboard, notifications and the notch. All data and every action go through the daemon's routes.
A shell never touches session data itself.

- **Web to shell**: `window.webkit.messageHandlers.<name>.postMessage({kind: …})` in WebKit
  (WKWebView, and WebKitGTK uses the same name), and `chrome.webview.postMessage` in WebView2.
- **Shell to web**: a `CustomEvent` on `window`, because a message handler has no return value.
- **Signing in**: the shell reads the daemon's local token file (mode 0600) and puts it in the
  webview's cookie store before the first load, so the token never passes through page script or
  an address bar.

The macOS shell starts the daemon bundled inside the app, never one found on `PATH`, so a shell
never talks to a daemon from a different build. [shell-bridge.md](shell-bridge.md) lists every
message, screen by screen, and ends with the minimum a Linux or Windows shell has to implement.

The web side is three npm workspaces:

| | Holds | Imports |
| --- | --- | --- |
| `web/contract` | the generated types | nothing |
| `web/core` | the API client, refusals, the fleet store and its ordering rules | no DOM, no React, nothing bundler-specific |
| `web/console` | the React DOM screens | both of the above |

`core` asks its host for two things: `fetch`, and a stream transport. That split exists so a
future React Native app can reuse everything below the screens ([plan.md](plan.md) §3.1).

## Platforms

The daemon is portable. What it reaches out to touch is not: terminals, the clipboard, the
keyboard, secret storage and the desktop. Each of those is a port with a per-platform adapter,
and a platform that cannot do something says so by name instead of pretending.
[cross-platform.md](cross-platform.md) goes feature by feature and records what each platform
shows when it cannot.
