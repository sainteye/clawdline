# Clawdline

[繁體中文](README.zh-TW.md)

**A local control plane for Claude Code and Codex. See every session, know which one needs you,
and let agents hand work to each other without losing the delivery record.**

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8.svg)](go.mod)

Clawdline is a Go daemon with a React console. Your agents and code keep running on your machine;
the optional Cloud connection is only an encrypted path to that machine from another device.

## Why Clawdline?

One coding agent is easy to follow. Several agents across several repositories quickly become a
wall of terminal tabs: you forget which project a session belongs to, miss a permission question,
and lose track of whether delegated work was merely finished or actually landed.

Clawdline turns those independent terminals into one observable, controllable system.

### Project-based sessions

Every Claude Code or Codex session is shown with its project, terminal, assistant, and current
work. Sessions started by hand in tmux are included too; Clawdline does not require a wrapper or
install hooks into either agent.

### Session status and attention management

See which sessions are working, waiting for you, idle, finished, or unreadable. Open a session to
read its real transcript, answer a question, send another instruction, interrupt it, or close it.
Unknown evidence stays `unknown` instead of being reported as idle.

<p align="center">
  <img src="docs/assets/sessions-live.gif" width="760" alt="Clawdline updating the state of several Claude Code and Codex sessions.">
</p>

### Use Codex and Claude Code from your phone

The same console works in a browser. With the optional Clawdline Cloud connection, you can check
progress, read transcripts, receive attention notices, and reply from your phone while execution
stays on your own machine. Reading and sending are separate permissions.

<p align="center">
  <img src="docs/assets/fleet-phone.png" width="390" alt="Clawdline on a phone, showing working, waiting, and child sessions across several projects.">
</p>

### Scheduled tasks, executed by an agent

Save a task for Claude Code or Codex and run it once or on a local schedule. Scheduled work uses
the same broker, isolation, timeout, status, and result records as interactive delegation. If the
daemon was offline, it can catch up the latest eligible occurrence without replaying every missed
run.

### Webhooks for complex work

Bind a Cloud webhook to a saved agent task. An incoming event can start a full agent session on
your machine instead of a fixed shell command, while durable claims and receipts prevent the same
delivery from silently opening duplicate work.

### Clawdfather coordinates other sessions

Designate one live session as **Clawdfather**, the machine-wide coordinator. It can inspect the
session fleet, hand bounded work to Claude Code or Codex children, wait for results, move a line of
work to another session, and keep delivery separate from review and landing.

## How it compares

[T3 Code](https://github.com/pingdotgg/t3code) is a full control surface for running agents, with
web, desktop, and mobile apps, support for more agents than Clawdline, and native version-control
workflows. Choose T3 Code when you want a feature-rich, cross-device workspace that feels closer
to a complete development environment. Clawdline focuses on the Claude Code and Codex sessions
you already started in tmux or iTerm2, along with their state, schedules, and durable coordination
records.

[Herdr](https://github.com/herdrdev/herdr) is an agent harness and multiplexer built around real
terminal panes. It detects many coding agents, rolls up workspace state, and jumps quickly to the
pane that needs attention. Choose Herdr for a terminal-first multi-agent workspace with broad
agent support. Clawdline emphasizes web and phone control, brokered dispatch, landing evidence,
scheduled tasks, webhooks, and Clawdfather coordination.

[Orca](https://github.com/stablyai/orca) is a full Agent Development Environment with isolated git
worktrees, a built-in terminal, editor, diff review, browser, GitHub and Linear integrations, and
a mobile companion. Choose Orca when you want to create, compare, and merge several agents' work
inside one application. Clawdline is not an IDE: it is a control plane around existing sessions,
with long-running schedules, webhooks, and agent-to-agent coordination at its core.

Clawdline does not replace your IDE, Claude Code, or Codex. Choose it when the difficult part is no
longer starting an agent, but operating several of them reliably over time.

## Install

Clawdline is pre-1.0 and does not have a release download yet. Build it from source on macOS or
Linux. You need Go 1.25 or newer, Node.js with npm, tmux, and Claude Code or Codex. Windows builds,
but session discovery and control are not supported there yet.

```sh
git clone https://github.com/sainteye/clawdline.git
cd clawdline

(cd web && npm install && npm run build)
go build -o bin/clawdline ./cmd/clawdline
```

Start the daemon:

```sh
CLAWDLINE_NEXT_WEB="$PWD/web/console/dist" ./bin/clawdline serve
```

Then, in another terminal:

```sh
./bin/clawdline doctor
./bin/clawdline open
```

Run Claude Code or Codex inside tmux and the session will appear in the console:

```sh
tmux new -s work
cd /path/to/your/project
claude  # or: codex
```

Before starting an agent, you can also add existing directories explicitly to the “Open a
session” list. This does not invoke a model:

```sh
./bin/clawdline project add /path/to/project /path/to/another-project
./bin/clawdline project list
```

On macOS, you can also build the native shell:

```sh
tools/package-macos.sh          # dist/Clawdline Next.app
tools/package-macos.sh --dmg    # also create a disk image
```

For pairing, Cloud setup, diagnostics, and troubleshooting, continue with the
[getting-started guide](docs/getting-started.md).

[Architecture](docs/architecture.md) · [Remote access](docs/remote.md) ·
[All documentation](docs/README.md) · [MIT License](LICENSE)
