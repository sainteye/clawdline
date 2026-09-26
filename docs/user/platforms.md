# macOS, Linux and Windows

After this page you know what Clawdline does on your operating system, and you can keep the daemon
running without a terminal open: the native app on macOS, a `systemd --user` service on Linux.

## At a glance

| | macOS | Linux | Windows |
| --- | --- | --- | --- |
| Daemon, console, state | Yes | Yes | Yes |
| List and control sessions you started | tmux and iTerm2 | tmux | No |
| Sessions Clawdline opens (start, dispatch, schedules) | tmux | tmux | No |
| Keep it running | Native app, start at login | `systemd --user` service | By hand (a shortcut in `shell:startup`) |
| Global hotkey, menu bar, notch mascot | Native app | No | No |

Linux was measured on a headless Ubuntu 24.04 server, and Windows on Windows Server 2022. Other
distributions and desktop Linux have not been run.

## macOS: the native app (optional)

The app bundles its own daemon and console, so you do not run `serve` yourself.

```sh
tools/package-macos.sh          # builds dist/Clawdline Next.app
tools/package-macos.sh --dmg    # and a disk image
```

It needs Apple silicon, macOS 13 or newer, and the Xcode command line tools to build
(`swiftc --version`). It is signed ad hoc, not notarized. Copy it to `/Applications` and open it.

What it adds:

- **A menu bar item** (✳). It turns to the accent colour with a dot when a session is waiting for
  you, and shows a count when several are working. Its menu opens the input bar, the console
  window (**主頁／設定中心**), **開機時啟動** (start at login), **設定⋯** (settings) and **結束
  Clawdline** (quit). The app's own menus are in Traditional Chinese.
- **An input bar** on a global hotkey, ⌥Space by default. By default the hotkey is only held while
  iTerm2 or the app is in front; the settings window changes both. No Accessibility permission is
  needed.
- **A mascot in the notch**, on displays that have one. It can be turned off.
- **A settings window** with tabs for general settings, the input bar, dictation, remote access,
  dispatch, and Cloud pairing with a QR code.

**Check:** the app's window shows the same session list as the browser. If a daemon you started by
hand already holds port 7727, the app's own daemon exits and the window shows the one already
there. Quitting the app stops only the daemon it started.

## Linux: a `systemd --user` service

Run the daemon by hand as in [install.md](install.md), or install it as a user service that
survives logout and that the service account can update without root:

```sh
tools/deploy-linux-user.sh --stage-only                   # as the service account
sudo tools/bootstrap-linux-user-service.sh <service-user> # once, as an administrator
```

The bootstrap installs `tools/systemd/clawdline-next.service` into that user's
`~/.config/systemd/user/`, runs `loginctl enable-linger` so it runs without a login, starts it and
checks that the console answers. Releases live under `~/.local/share/clawdline-next/`.

Every later update is one unprivileged command:

```sh
git fetch origin main && git merge --ff-only origin/main
tools/deploy-linux-user.sh
```

It builds from a clean checkout, switches the release atomically, restarts only the daemon — tmux
and the assistants it opened keep running — checks `GET /` and the build stamp, and rolls back if
either fails. It prints `deployed <commit>; daemon pid N; console 200`.

**Check:** `systemctl --user status clawdline-next` is active, and `./bin/clawdline doctor` answers.

On a machine with no desktop, `clawdline open --print` prints the sign-in address; reach it over an
SSH port forward ([remote-access.md](remote-access.md)). Clipboard pictures, the global hotkey and
the notch are unavailable on Linux, and a picture you send is handed to the assistant by its path.

## Windows: not yet for sessions

The daemon, the console, its sign-in and its store run on Windows, and `clawdline skill install` works. There is
no way yet to list or drive sessions: the session list is empty and says it could not look, and
dispatch stops with `no_child_capability`. Chinese text printed by the command line is garbled in a
Windows console. State lives in `%APPDATA%\clawdline-next`.

## Deeper

- [cross-platform.md](../cross-platform.md) — every feature on each platform, and how a platform
  says it cannot do something.
- [linux.md](../linux.md) and [windows.md](../windows.md) — what was run on each, and what it
  answered.
