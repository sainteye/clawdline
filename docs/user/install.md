# Install and first run

After this page Clawdline runs on your machine as a service that starts at login, the console
is open in your browser, and one Claude Code or Codex session shows in its list. Each step ends
with a check.

## Before you start

| You need | Check |
| --- | --- |
| macOS 13+ (Apple silicon or Intel) or Linux (x86-64 or ARM64) | `uname -sm` |
| *(optional)* tmux 3.0 or later — a release carries its own when you have none | `tmux -V` |
| Claude Code or Codex | `claude --version` or `codex --version` |
| *(optional)* a Claude Code status line that writes `~/.claude/statusline-cache/rate-limits.json` | `ls ~/.claude/statusline-cache/rate-limits.json` |

You do not need Go, Node.js, Xcode, an account or `sudo`. The installer checks the assistants
itself and prints the exact command to install a missing one.

You do not need tmux either. Every release carries tmux 3.6a. When your machine has tmux 3.0 or
later, that one is used, because it is where your own sessions are. When it has none, or an older
one, the daemon runs the carried tmux on a server of its own
(`~/.config/clawdline-next/tmux/sessions.sock`), so it never meets another tmux. Setup says which
it chose: `✓ tmux found`, or `✓ tmux: this release carries tmux 3.6a`.

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
  line that adds it to your shell's profile file (`~/.zshrc`, `~/.bashrc`, …), and every
  command it prints uses the full path until then;
- starts the daemon as a per-user service that comes back after logout and reboot: a
  `systemd --user` unit on Linux, a LaunchAgent on macOS. On Linux it also turns on lingering,
  so the service starts at boot and outlives your last logout; where your system reserves that
  for an administrator, it says so and prints the one `sudo loginctl enable-linger` line to ask
  for;
- on a Mac with a desktop, also installs **Clawdline Next.app** into `~/Applications`;
- checks that the console answers, that it is the release just installed, and that the daemon
  can open and close a tmux session — then opens the console in your browser.

Each check prints one ✓ line; `--verbose` adds the paths and versions behind it. The install ends
with a **Next:** block: how to sign in (on a machine with no desktop, the address and the
`ssh -L` line that reaches it from your laptop), how to start an assistant inside tmux, and how to
turn autostart off or remove Clawdline, each with `--port` when you chose another port.

"Next" in `Clawdline Next.app`, `clawdline-next.service` and `~/.config/clawdline-next` is this
generation's internal name; the product is Clawdline.

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
| `--verbose` | Also print the paths and versions behind each check |
| `--adopt` | Take over an installation made from a source checkout (below) |

**Check:** the installer ends with the console open (or its address printed), and

```sh
clawdline doctor          # version, port, state directory
```

answers with the version you installed.

## 2. Open the console

The installer opens it for you. To open it again, or on another browser:

```sh
clawdline open           # sign this browser in
clawdline open --print   # print the address instead (a machine with no desktop)
```

A browser signed in this way can read every session and type into it.

`open` creates a device for your browser and signs it in. The key travels in the address's
fragment, which the browser never sends to a server. Treat a printed address like a password
until it has been used. On a machine with no desktop, reach it over an SSH port forward
([remote-access.md](remote-access.md)).

**Check:** the console loads and shows the session list. The examples in these pages show Traditional Chinese labels with their English meaning; the console supports other interface languages.

## 3. Put a session in the list

The daemon finds sessions running in tmux (and iTerm2 on a Mac). Start one:

```sh
tmux new -s work
cd ~/code/my-app
claude            # or: codex
```

If setup said it uses the tmux the release carries, type `clawdline tmux` where this says `tmux`:
`clawdline tmux new -s work`, and later `clawdline tmux attach -t work`. That reaches the server
the daemon lists. `clawdline tmux which` shows which tmux that is.

**Check:** within a few seconds the session is a row in the console with its project and state.
Nothing was installed into Claude Code or Codex: the daemon reads what they already write under
`~/.claude` and `~/.codex`, and reads their screens through tmux.

To list a directory among the places to start a session before you ever ran an agent there:

```sh
clawdline project add ~/code/my-app ~/code/another-app
```

## Updates

The daemon checks for a new release every few hours. When there is one, the console's Settings
page and `clawdline update` say so; **立即更新** there, or `clawdline update --apply`, installs it.
Every release is signature-checked before it is installed, and one that does not come up healthy
is rolled back by itself. Automatic updates are off until you turn on **自動更新** on the Settings
page or run `clawdline setting set update_auto_apply true`. [updates.md](../updates.md) has the
whole of it.

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
where you were. To delete it later, once the `clawdline` command is gone, run
`rm -rf ~/.config/clawdline-next`, or the installer with `--uninstall --purge`, which removes it
even when nothing else is left. Uninstall also removes the service's systemd drop-in directory
(`~/.config/systemd/user/clawdline-next.service.d/`, where `systemctl --user edit` puts overrides). Terminals Clawdline opened keep running after either; while one is still open,
`--purge` leaves the `tmux` folder they run from, and says how to reach them. If you ran `clawdline skill install`, run `clawdline skill uninstall` first
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
