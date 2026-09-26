# Clawdline

[繁體中文](README.zh-TW.md)

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8.svg)](go.mod)

**A local control plane for Claude Code and Codex. See every session, know which one needs you,
and let agents hand work to each other without losing the delivery record.**

Clawdline is a Go daemon with a React console. It watches the Claude Code and Codex sessions you
already run in tmux (and iTerm2 on a Mac) — no wrapper, no hooks — and lets you read, answer, start
and stop them from a browser or your phone. Your agents and code stay on your machine; the optional
Clawdline Cloud connection is only an end-to-end encrypted path to it from another device.

## What it does

<p align="center">
  <img src="docs/assets/sessions-live.gif" width="760" alt="Clawdline updating the state of several Claude Code and Codex sessions.">
</p>

- **Sessions and attention.** Every session with its project, assistant and state: working, waiting
  for you, idle, or unreadable — never guessed as idle. Open one to read the real transcript, answer
  its question, send text or a marked-up picture, dictate, stop the current turn, or close it
  safely.
- **From your phone.** The same console in any browser. Reach it over an SSH forward, your own
  cloudflared tunnel, or Clawdline Cloud, with reading and acting as separate permissions, and get
  a push notification when a question has waited ten minutes.
- **Schedules and webhooks.** Save a task for Claude Code or Codex and run it on the local clock,
  with catch-up, a timeout and a failure notice. On Cloud Pro, a webhook starts the same task from
  any event.
- **Board and Verify.** Put work on a project's Board, assign it to a session, and let the agent
  move it through implementation, verification, merge and deployment with evidence. Keep changes
  that can only be judged later on a list of things waiting to be verified.
- **Clawdfather and dispatch.** Sessions dispatch bounded work to child sessions, get the result
  back, and hand a line of work to a fresh session. Clawdline records when delivered work actually
  lands. One session can hold the machine-wide coordinator role, **Clawdfather**.
- **Projects across machines.** Give a second machine the same project names, icons and untracked
  skills as the first, matched by git origin.

<p align="center">
  <img src="docs/assets/fleet-phone.png" width="390" alt="Clawdline on a phone, showing working, waiting, and child sessions across several projects.">
</p>

## How it compares

[T3 Code](https://github.com/pingdotgg/t3code) is a full control surface for running agents, with
web, desktop and mobile apps, more agents and native version-control workflows.
[Herdr](https://github.com/herdrdev/herdr) is a terminal-first agent harness and multiplexer with
broad agent support. [Orca](https://github.com/stablyai/orca) is a full Agent Development
Environment with worktrees, an editor, diff review and integrations.

Clawdline is not an IDE and does not replace Claude Code or Codex. It is a control plane around the
sessions you already start, with web and phone control, brokered dispatch, landing evidence,
schedules, webhooks and Clawdfather coordination at its core. Choose it when the hard part is no
longer starting an agent, but operating several of them reliably over time.

## Install

Build from source on macOS or Linux. You need Go 1.25 or newer, Node.js with npm, tmux, and Claude
Code or Codex.

```sh
git clone https://github.com/sainteye/clawdline.git
cd clawdline
(cd web && npm install && npm run build)
go build -o bin/clawdline ./cmd/clawdline

CLAWDLINE_NEXT_WEB="$PWD/web/console/dist" ./bin/clawdline serve
```

In another terminal:

```sh
./bin/clawdline doctor      # version, port, state directory
./bin/clawdline open        # sign this browser in; --send lets it type into sessions
```

Then run `claude` or `codex` inside tmux, and the session appears in the list. Each step, with a
check that it worked, is in [Install and first run](docs/user/install.md).

## Some notes

- Clawdline is **pre-1.0** and has no release download yet. Expect things to change.
- The console's interface is in **Traditional Chinese** for now; it is the only language catalog
  it ships.
- **Windows** builds and runs the daemon and console, but cannot list or control sessions yet.
  Linux runs headless under a `systemd --user` service; macOS has an optional native app.
- Everything on your own machine is free and needs no account. **Clawdline Cloud** (several
  machines, remote pairing, schedule webhooks) is optional, off by default, and in preview.

## Documentation

Getting going

- [Install and first run](docs/user/install.md)
- [macOS, Linux and Windows](docs/user/platforms.md) — the native app, a `systemd --user` service,
  what Windows can do
- [Troubleshooting](docs/user/troubleshooting.md)

Everyday use

- [Watch, answer, start, stop and close sessions](docs/user/sessions.md)
- [Keyboard shortcuts](docs/user/keyboard-shortcuts.md)
- [Notifications](docs/user/notifications.md)
- [Remote access from a phone or another machine](docs/user/remote-access.md) — SSH, your own
  tunnel, Clawdline Cloud

Running work

- [Scheduled tasks and webhooks](docs/user/schedules.md)
- [Board, session to-dos, Now and Verify](docs/user/board.md)
- [Clawdfather, dispatch, landing and handoff](docs/user/clawdfather-and-dispatch.md)
- [Projects, and bringing them to another machine](docs/user/projects.md)
- [Token use, assistant quotas and capacity](docs/user/usage.md)

Building on it? Start at [docs/architecture.md](docs/architecture.md), [AGENTS.md](AGENTS.md), and
[docs/README.md](docs/README.md) for every design note.

## License

[MIT](LICENSE)
