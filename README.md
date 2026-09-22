# Clawdline

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

[Claude Squad](https://github.com/smtg-ai/claude-squad) is a strong choice when you want a
terminal-first launcher for parallel agents in isolated git worktrees. Clawdline also observes
sessions you started yourself and adds browser/phone control, scheduled and webhook-triggered
work, and durable coordination records.

[Agent Deck](https://github.com/rkunnamp/agent-deck) supports a wider range of terminal agents and
is a good fit when you want one TUI for many tools. Clawdline deliberately goes deeper on Claude
Code and Codex: their transcripts, attention states, cross-session dispatch, landing evidence,
and remote operation.

[Crystal](https://github.com/stravu/crystal) focuses on desktop workflows where each task gets a
git worktree and changes are reviewed and merged. Clawdline is a daemon and control plane: it can
manage existing sessions, expose them to a web console, and coordinate work that is not limited
to a single desktop review flow.

Clawdline is not an IDE and it does not replace Claude Code or Codex. Choose it when the difficult
part is no longer starting an agent, but operating several of them reliably over time.

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

On macOS, you can also build the native shell:

```sh
tools/package-macos.sh          # dist/Clawdline Next.app
tools/package-macos.sh --dmg    # also create a disk image
```

For pairing, Cloud setup, diagnostics, and troubleshooting, continue with the
[getting-started guide](docs/getting-started.md).

[Architecture](docs/architecture.md) · [Remote access](docs/remote.md) ·
[All documentation](docs/README.md) · [MIT License](LICENSE)
