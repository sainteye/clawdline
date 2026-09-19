# Clawdline

**A local control plane for Claude Code and Codex: every live session in one list, work handed
from one session to another with its claims checked first, and a record of what was delivered
that outlives the chat it happened in.**

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8.svg)](go.mod)

[Where it stands](#where-it-stands) · [Install](#install-and-run) ·
[From a browser or a phone](#from-a-browser-or-a-phone) · [Free and Cloud](#free-and-cloud) ·
[Architecture](docs/architecture.md) · [Getting started](docs/getting-started.md) ·
[All documents](docs/README.md)

This is the second generation of Clawdline. The first was a macOS-only Swift app. This one keeps
the product and changes what it stands on:

- **The core is one Go binary**, which is both the daemon and its command line. It reads the
  sessions, runs the broker, keeps one SQLite store and serves one HTTP + SSE API.
- **The interface is a React web console**, served by that daemon. The same page is the window on
  the Mac and the page in a browser.
- **A thin native shell per platform** does only what a web page cannot: a window, a menu bar
  item, a global hotkey, the notch. Today there is a macOS shell. On Linux you run the daemon
  and use a browser. Windows is not supported yet.

Execution never leaves your machine. Clawdline Cloud is an optional, encrypted way to reach it from
a phone or another machine. It is off until you turn it on.

## Where it stands

Pre-1.0. The binary reports itself as `0.0.1-p0`. Everything below can be checked against a
checkout, and the last column says how.

| | State | How to check |
| --- | --- | --- |
| Daemon and console on macOS | Works | `curl -s http://127.0.0.1:7727/v1/health` |
| Sessions in tmux and iTerm2 on macOS | Works | start `claude` or `codex` inside tmux; it appears in the list |
| Linux | The daemon has run in a Linux container and answered `/v1/health`. Sessions go through tmux on the same code path as macOS, but have not been exercised on Linux | `clawdline doctor` prints the resolved state directory |
| The macOS app | Works when built from source. Apple silicon, macOS 13+, signed ad hoc | `tools/package-macos.sh` |
| Handing work to another session (the broker) | Works. `serialize`, `attach_session` and `reasoning_effort` are refused by name, not ignored | `POST /v1/orchestrator/tasks` with one of them returns `bad_task` |
| Board, backlog and a session's own to-do list | Works. A new install starts empty | `GET /v1/work/board` |
| A browser on the same machine | Works | `clawdline open` |
| A phone, without an account | **Works, over a tunnel you run.** The pairing page, the gate and a launcher for your own `cloudflared` are all in. A clean browser was driven through it end to end; no public tunnel has been raised from this repository | [below](#from-a-browser-or-a-phone) |
| A phone, through Clawdline Cloud | **Preview.** The machine side is written and was driven end to end against a local copy of the service, not yet against the production service | [Free and Cloud](#free-and-cloud) |
| Windows | The daemon cross-compiles and has not been run on Windows. It could not list or drive sessions there yet: no tmux, no ConPTY backend, no process inventory | `internal/adapters/process/ps_windows.go` |
| Interface language | The console ships one catalog, Traditional Chinese. The command line is English | `web/console/public/strings/` |

**It still expects to be run beside the Swift app unless you tell it otherwise.** It was built to
run next to the app it replaces, so by default it uses its own port (7727), its own state
directory (`clawdline-next`) and its own credentials, and it forwards any route it does not own to
the Swift app on 7717. To run it on its own, set the three variables in
[Install and run](#install-and-run). The macOS app sets them for you.

## What it does

### Every session, in one list

Every Claude Code and Codex session running in tmux, and in iTerm2 on a Mac, is a row: which
assistant it is, which project, and whether it is working, waiting for an answer, or idle. A
state that could not be read is reported as `unknown`, never as idle. Guessing "idle" would be a
confident wrong answer about somebody's work.

Open a row and the transcript is read from the assistant's own record under `~/.claude` or
`~/.codex`, not scraped from the screen. From the same page you can send text, answer the question
a session is stuck on, interrupt it, close it, start a new session in a directory this machine has
already worked in, or resume an earlier conversation.

**Nothing is installed into Claude Code or Codex.** No hooks, no MCP server, no wrapper around the
`claude` or `codex` command, no edits to their settings. Those directories are only read, which is
why a session you started by hand an hour ago is in the list too.

### Handing work to another session

A session can hand a bounded piece of work to a new session, of either assistant. It writes the
task down as `task.json` and posts `POST /v1/orchestrator/tasks`. The daemon opens a terminal for
the child, adds its briefing (`CHILD.md`), types one first line carrying a secret only that task
holds, and collects the answer from the `result.json` the child writes. The session that asked is
told when the child finishes.

- **Declared write paths are checked before anything opens.** A task lists the paths it will
  write in `claims`. If a live task under a *different* root has claimed an overlapping path, the
  dispatch is refused with `workspace_busy`. The refusal names the task holding the path, its
  title, and every conflicting path. Two tasks under the same root only get a `claims_overlap`
  warning, because that root drew the plan.
- **A child's work can be isolated.** `"isolation": "worktree"` gives it its own git worktree and
  branch, so its delivery is a branch head rather than edits in a shared tree.
- **Delivered is not landed.** A task that returned `success` has delivered an answer. Whether
  that answer reached the target branch is a separate landing record, proved from git ancestry.
- **Every task has a clock.** `timeout_minutes` is 1 to 240. A root may have
  `orchestrator_max_children` children out at once (default 5), and the whole machine four times
  that.
- **Side effects happen once.** Typing into a terminal, opening a tab and making a checkout are
  recorded as intent in the same transaction as the fact that owes them, and run after it commits.
  If the daemon dies in the middle of typing, it records the effect as `unknown` and does not
  type it twice.

The same broker also carries messages between sessions, handoffs of a whole line of work to a new
session, a machine-wide coordinator role with leases, and waits. [Architecture →](docs/architecture.md#the-broker)

### Board, backlog, and a session's own to-do list

Three structures, kept apart on purpose, because they have different readers:

| | For | Who creates an item | Who closes it |
| --- | --- | --- | --- |
| **Board** | A person: what is happening, and what is waiting on me | a person, or a proposal a person accepted | evidence (the work landed), or a person |
| **Backlog** | Planning: work that is committed to but not started | a person | only a person, or by moving it to the board |
| **Session to-do** | A session: what it still owes | the broker, from facts it already records. Today that is each dispatch: the root owes collecting and landing the result | the broker, when the matching fact arrives |

The session to-do list does not appear on the board. It is on the session's own panel, and a
session can read it through the API. Nobody has to tend it: nothing depends on an assistant
remembering to call a route.

### Schedules

Task templates that dispatch on local wall-clock time. An occurrence missed while the daemon was
down runs once when it comes back, not once per missed occurrence. A schedule's interval counts
from its last run or from when this daemon first saw it, whichever is later, so a schedule restored
from a backup does not fire the moment the clock ticks.
[docs/schedules.md](docs/schedules.md) (Traditional Chinese).

### Also

- **Usage** by model, assistant, project and day, computed from the transcripts.
- **Pictures**: attach, paste or drop an image into a session. On a Mac it reaches the assistant
  as a pasted image; elsewhere as a file path.
- **Dictation** from the console, read back by `whisper-cli` on your own machine, if you have it
  and a model file installed. Without them, the refusal says which one is missing.
- **Web Push** to a paired browser (VAPID, RFC 8291): a child's `notify`, a completion notice that
  could not be delivered, a failed schedule, and a board proposal. A push when a session starts
  waiting is not wired yet.
- **The macOS app**: a window onto the console, a menu bar item, an input bar on a global hotkey
  you choose, launch at login, and a mascot in the notch.

## Coming from the Swift app

This repository used to hold the Swift app. What changes for you:

- **The two can be installed at the same time.** This one has its own bundle id, state directory,
  port and credentials. Nothing in `~/.config/clawdline` is written.
- **Nothing is migrated for you.** New tasks, board items and settings start in this app's own
  store. The old task records and board cards are not converted. Schedules are the exception you
  can carry over yourself: `POST /v1/orchestrator/schedule-imports` takes the Swift app's schedule
  files byte for byte. It is off until you set `schedule_imports_enabled` in the config, because
  an import can name any project directory.
- **While both are installed, this daemon reads a few of the Swift app's files, read-only,** so
  that its screens match: session titles, old task records and board cards, the usage ledger,
  saved pictures, and the dispatch policy. It never reads the Swift app's secrets, tokens or
  keys. That read lives in one adapter, `internal/adapters/swiftstore`, so it can be removed in
  one piece.
- **Not in this generation yet:** a bundled tunnel binary (you install `cloudflared` yourself),
  Claude Code hook installation, snippets, the skills menu, the dev-server list, the project
  timeline, and interface languages other than Traditional Chinese.

## Install and run

There is no release download yet. You build it from source. The full walk-through, including
what to do when something does not come up, is [docs/getting-started.md](docs/getting-started.md).

You need Go 1.25 or newer, Node.js with npm, tmux, and Claude Code or Codex.

```sh
git clone https://github.com/sainteye/clawdline.git
cd clawdline

(cd web && npm install && npm run build)        # the console, into web/console/dist
go build -o bin/clawdline ./cmd/clawdline       # the daemon and CLI

CLAWDLINE_NEXT_STANDALONE=1 \
CLAWDLINE_NEXT_OWN_SESSIONS=1 \
CLAWDLINE_NEXT_WEB="$PWD/web/console/dist" \
  ./bin/clawdline serve                         # listens on 127.0.0.1:7727
```

In a second terminal:

```sh
./bin/clawdline doctor      # version, port and state directory as this binary resolves them
./bin/clawdline open        # opens the console in your browser, signed in
```

`STANDALONE` answers an unported route with `501 not_implemented` instead of forwarding it to the
Swift app. `OWN_SESSIONS` makes this daemon answer `/v1/sessions` itself. `WEB` tells it where
the console's files are. Without `STANDALONE`, a machine with no Swift app answers `502
upstream_unreachable`.

**On a Mac**, `tools/package-macos.sh` builds `dist/Clawdline Next.app` (add `--dmg` for a disk
image). It needs Xcode's command line tools for `swiftc`, as well as Go and npm. The app starts the
daemon bundled inside it, with the three variables set, and signs its own window in.

## From a browser or a phone

**A browser on this machine.** `clawdline open` creates a device for that browser and opens the
console signed in. That device can read. `clawdline open --send` also lets it type into sessions.

**A phone, without an account.** Start a tunnel with `clawdline tunnel`, which runs the
`cloudflared` you installed and always passes its own `--config`, so it can never pick up another
tunnel's configuration. It refuses to start while no device has been paired, or while remote is
off. Open the address it prints on the phone and you meet the door: ask to pair, and
`clawdline pair --watch` prints six digits on this machine only. Type them and the console opens.
A newly paired phone can read; typing is a second permission.

**A phone, through Clawdline Cloud.** The other way, with an account and no tunnel of your own:
the next section.

What stands in front of every request, in the order it meets them:

- **Loopback only by default.** The daemon binds `127.0.0.1`. `CLAWDLINE_NEXT_HOST` changes that,
  and the daemon logs a warning when it does.
- **The `Host` header is checked first.** Only `127.0.0.1`, `localhost`, `::1`, your configured
  `remote_hostname` and `*.trycloudflare.com` are answered. DNS rebinding cannot change `Host`.
- **Cross-site requests are refused.** A change must be JSON from this page's own origin. A
  change carried by the cookie must also have `Origin` and `Sec-Fetch-Site: same-origin`.
- **Every route needs a credential, including from loopback.** The exceptions are the console's
  static files, `/v1/health`, `/v1/strings` and the sign-in routes. Once a tunnel exists, a request
  from the other side of the world also arrives from `127.0.0.1`.
- **Tokens are stored as SHA-256 hashes** and compared in constant time.
- **Reading and typing are separate permissions.** A newly paired device can only read. Typing
  into a session is code execution, because the assistant runs a shell.
- **Pairing needs your screen.** The six-digit code is shown on this machine and never sent back
  to the device that asked. It expires after two minutes. Five wrong guesses in 24 hours, or three
  pairing requests in ten minutes, and it stops accepting until the window passes.

## Free and Cloud

| | Free (this repository) | Clawdline Cloud |
| --- | --- | --- |
| What it is | Everything on one machine: daemon, console, native shell, broker, board, schedules | An optional hosted service at [clawdline.com](https://clawdline.com/) that relays between your machine and your other devices |
| Account | None | A Clawdline account |
| Reach | This machine's browser | A phone or another computer, through app.clawdline.com |
| Machines | One | Several, under one account |
| Where the work runs | Your machine | Your machine. Cloud never runs anything |
| What a service can read | There is no service | Signed ciphertext only (AES-256-GCM, Ed25519). The content key stays on hardware you own |

The machine's half of the Cloud protocol is in this repository, under the same license. The hosted
service, meaning the relay, the account API and the hosted console, is not.

Cloud has two switches, both off by default. `clawdline cloud on` lets the machine connect.
`clawdline cloud commands on` lets a paired viewer act on it, not just read, and each request
re-reads that switch. A viewer also needs the matching capability on its own device record. A
broken Cloud setting never stops the daemon. `/v1/cloud/status` says why the line is down.

Where Cloud stands: the machine side of pairing, the encrypted line and most commands are done,
and have been exercised against a local copy of the service. The commands
`agent`, `shell`, `skills`, `timeline`, `snippets` and `schedule` answer `unknown_command`, and
`dispatch` is refused. More than one machine on one account has not been tested with this daemon.
Turning it on is in [docs/getting-started.md](docs/getting-started.md#turn-on-clawdline-cloud-optional).

## Platforms

| | macOS | Linux | Windows |
| --- | --- | --- | --- |
| Daemon and console | Yes | Run in a container | Builds; not run yet |
| Sessions you started yourself | tmux, iTerm2 | tmux (not exercised yet) | Not yet |
| Sessions the daemon opens | tmux, iTerm2 | tmux (not exercised yet) | Not yet |
| Native shell | Yes | No: use a browser | Not written (WebView2 planned) |

The binary is pure Go (`CGO_ENABLED=0`), so the macOS, Linux and Windows builds for amd64 and
arm64 all come from one machine.

On Linux, run your assistants in tmux. The daemon does not push keys into a terminal it cannot
drive through tmux: it does not use `TIOCSTI`, and it does not read `/dev/input` for a hotkey.
Either would give it the power to read or type anything you do.
[docs/cross-platform.md](docs/cross-platform.md) (Traditional Chinese) has every feature,
platform by platform.

## Documentation

[docs/README.md](docs/README.md) lists every document and says which are public design notes and
which are internal records of the move from the Swift app. Start with:

- [docs/getting-started.md](docs/getting-started.md): from clone to a running console, a browser,
  and Cloud
- [docs/architecture.md](docs/architecture.md): the whole system on one page

Most design notes are written in Traditional Chinese. The English pages above point into them.

## Contributing

```sh
go test ./...                        # the Go tests
(cd web && npm run check)            # TypeScript, no emit
go run ./tools/contract-gen -check   # the generated Go and TypeScript match api/v1
```

There is no CI yet. The API is defined in `api/v1/*.schema.json`. Change the schema, then run
`go run ./tools/contract-gen`. Do not edit the generated files by hand. Commit messages and code
comments are in English.

## Credits

The mascot is fan art of the pixel character that appears in Claude Code, known in the community as
**Clawd**. This project is not affiliated with, endorsed by, or connected to Anthropic. Claude and
Claude Code are trademarks of Anthropic.

Putting live agent activity in the MacBook's camera housing is
[CLI Island](https://github.com/bistin/cc-island) by [bistin](https://github.com/bistin), which got
there first; the implementation here is its own and works differently, but the idea is borrowed with
thanks. The shape of the notch itself comes from
[DynamicNotchKit](https://github.com/MrKai77/DynamicNotchKit) by way of
[boring.notch](https://github.com/TheBoredTeam/boring.notch).

## License

[MIT](LICENSE)
