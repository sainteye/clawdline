# Install and first run

After this page Clawdline runs on your machine as a service that starts at login, the console
is open in your browser, and one Claude Code or Codex session shows in its list. Each step ends
with a check.

## Before you start

| You need | Check |
| --- | --- |
| macOS 13+ (Apple silicon or Intel) or Linux (x86-64 or ARM64) | `uname -sm` |
| tmux | `tmux -V` |
| Claude Code or Codex | `claude --version` or `codex --version` |
| *(optional)* a Claude Code status line that writes `~/.claude/statusline-cache/rate-limits.json` | `ls ~/.claude/statusline-cache/rate-limits.json` |

You do not need Go, Node.js, Xcode, an account or `sudo`. The installer checks tmux and the
assistants itself and prints the exact command to install anything that is missing, for the
package manager your machine has (Homebrew, apt, dnf, pacman, zypper or apk).

The last row is the only one you can skip and still lose something visible: Claude Code hands its
`5h`/`7d` plan percentages only to the stdin of `statusLine.command`, so without one the Status
Line's right edge reads `方案額度 未知`. [usage.md](usage.md) says what to configure.

## 1. Install

```sh
curl -fsSL https://raw.githubusercontent.com/sainteye/clawdline/main/install.sh | sh
```

It downloads the latest release for your system, checks it against the release's checksums and
signature, and runs `clawdline setup` from it, which:

- puts the release in `~/.local/share/clawdline-next/releases/<version>/` and links
  `~/.local/bin/clawdline` to it — if `~/.local/bin` is not on your `PATH`, it prints the one
  line to add to your shell profile;
- starts the daemon as a per-user service that comes back after logout and reboot: a
  `systemd --user` unit on Linux, a LaunchAgent on macOS. On Linux it also turns on lingering,
  so the service starts at boot and outlives your last logout; where your system reserves that
  for an administrator, it says so and prints the one `sudo loginctl enable-linger` line to ask
  for;
- on a Mac with a desktop, also installs **Clawdline Next.app** into `~/Applications`;
- checks that the console answers, that it is the release just installed, and that the daemon
  can open and close a tmux session — then opens the console in your browser.

Options go after `sh -s --`:

```sh
curl -fsSL https://raw.githubusercontent.com/sainteye/clawdline/main/install.sh | sh -s -- --headless
```

| Option | Does |
| --- | --- |
| `--headless` | No desktop: no app, no browser; prints the sign-in address instead |
| `--no-app` | macOS: the service only, no app |
| `--no-autostart` | Start now, but not at login or boot |
| `--channel beta` | Follow pre-releases too |
| `--version vX.Y.Z` | Install that release instead of the latest |
| `--port N` | Listen on another port than 7727 (it gets its own service name) |
| `--adopt` | Take over an installation made from a source checkout (below) |

**Check:** the installer ends with the console open (or its address printed), and

```sh
clawdline doctor          # version, port, state directory
```

answers with the version you installed.

## 2. Open the console

The installer opens it for you. To open it again, or on another browser:

```sh
clawdline open           # this browser can read every session
clawdline open --send    # this browser can also type into sessions
clawdline open --print   # print the address instead (a machine with no desktop)
```

`open` creates a device for your browser and signs it in. The key travels in the address's
fragment, which the browser never sends to a server. Treat a printed address like a password
until it has been used. On a machine with no desktop, reach it over an SSH port forward
([remote-access.md](remote-access.md)).

**Check:** the console loads and shows the session list. Its interface is in Traditional Chinese,
the only language it ships so far; the other pages here give each label with its meaning.

## 3. Put a session in the list

The daemon finds sessions running in tmux (and iTerm2 on a Mac). Start one:

```sh
tmux new -s work
cd ~/code/my-app
claude            # or: codex
```

**Check:** within a few seconds the session is a row in the console with its project and state.
Nothing was installed into Claude Code or Codex: the daemon reads what they already write under
`~/.claude` and `~/.codex`, and reads their screens through tmux.

To list a directory among the places to start a session before you ever ran an agent there:

```sh
clawdline project add ~/code/my-app ~/code/another-app
```

## Updates

The daemon checks for a new release every few hours. When there is one, the console's Settings
page and `clawdline update` say so; **update** there, or `clawdline update --apply`, installs it.
Every release is signature-checked before it is installed, and one that does not come up healthy
is rolled back by itself. [updates.md](../updates.md) has the whole of it.

## When something already runs

- **A daemon already answers on the port** (a `clawdline serve` you started, or an app built
  from a checkout): the installer changes nothing and says how to stop it, or use `--port`.
- **A Linux service deployed from a source checkout** (`tools/deploy-linux-user.sh`): the
  installer leaves it alone unless you pass `--adopt`. After adopting, the machine follows
  releases; deploying from the checkout again makes it follow commits again.

## Stop or remove it

```sh
curl -fsSL https://raw.githubusercontent.com/sainteye/clawdline/main/install.sh | sh -s -- --uninstall
# or, with it installed:
clawdline setup --uninstall           # service, link, app and releases
clawdline setup --uninstall --purge   # and the state: devices, sessions, settings
```

Without `--purge` your state in `~/.config/clawdline-next` stays, so installing again picks up
where you were. If you ran `clawdline skill install`, run `clawdline skill uninstall` first
([clawdfather-and-dispatch.md](clawdfather-and-dispatch.md)).

## Build from source instead

For working on Clawdline itself — you need Go 1.25+, Node.js with npm, and tmux:

```sh
git clone https://github.com/sainteye/clawdline.git && cd clawdline
(cd web && npm install && npm run build)
go build -o bin/clawdline ./cmd/clawdline
CLAWDLINE_NEXT_WEB="$PWD/web/console/dist" ./bin/clawdline serve
```

[getting-started.md](../getting-started.md) has every environment variable and the reasoning
behind them; [platforms.md](platforms.md) says how a checkout keeps running as a service.
