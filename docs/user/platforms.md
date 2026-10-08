# macOS, Linux and Windows

After this page you know what Clawdline does on your operating system and how it keeps running
without a terminal open. [install.md](install.md) sets all of this up with one command; this page
says what it set up, and how a source checkout does the same.

## At a glance

| | macOS | Linux | Windows |
| --- | --- | --- | --- |
| Daemon, console, state | Yes | Yes | Yes |
| List and control sessions you started | tmux and iTerm2 | tmux | No |
| Sessions Clawdline opens (start, dispatch, schedules) | tmux | tmux | No |
| Keep it running | LaunchAgent (installer) | `systemd --user` service (installer) | By hand (a shortcut in `shell:startup`) |
| Installer, signed updates with rollback | Yes | Yes | No |
| Menu bar, notch mascot | Native app | No | No |

Linux was measured on a headless Ubuntu 24.04 server, and the installer and updates were also run on Debian 12 (bookworm) in a container with systemd as its init, logged in over SSH; Windows was measured on Windows Server 2022. Other distributions and desktop Linux have not been run.

## macOS: the service and the app

The installer runs the daemon as a LaunchAgent, so it starts at login whether or not the app is
open, and puts **Clawdline Next.app** in `~/Applications`. The app then shows that service's
console and does not start a daemon of its own; its **開機時啟動** item turns the LaunchAgent's
start-at-login on and off. Quitting the app leaves the service running.

The app is signed ad hoc, not notarized: the installer downloads it with `curl`, which macOS does
not quarantine, so it opens without a Gatekeeper prompt. It needs macOS 13 or newer; the release
app is built for Apple silicon, and an Intel Mac gets the service and console without the app.

### Building the app from a checkout

```sh
tools/package-macos.sh          # builds dist/Clawdline Next.app
tools/package-macos.sh --dmg    # and a disk image
tools/package-macos.sh --no-restart   # never quit a running app
```

The new bundle is built and signed in `dist/.staging.<pid>/` first, so a failed build leaves the
app you have as it was. If the app is running from this checkout's `dist/`, the script quits it
(waiting up to 120 seconds), puts the new bundle in its place, opens it again and waits for its
daemon to answer. It does this because replacing a running app's files makes macOS refuse its
control of iTerm2 (error -1743) until it restarts. With `--no-restart` a running app is left
alone: the script exits with an error and prints where the new bundle is. Building needs the Xcode
command line tools (`swiftc --version`). An app built this way, without the installer's
`service.json`, starts its own daemon as before.

What it adds:

- **A menu bar item** (✳). It turns to the accent colour with a dot when a session is waiting for
  you, and shows a count when several are working. Its menu opens the console
  window (**主頁／設定中心**), **開機時啟動** (start at login), **設定⋯** (settings) and **結束
  Clawdline** (quit). The app's own menus are in Traditional Chinese.
- **A mascot in the notch**, on displays that have one. It can be turned off.
- **A settings window** with tabs for general settings, remote access,
  dispatch, and Cloud pairing with a QR code.

**Check:** the app's window shows the same session list as the browser. An app that starts its own
daemon (built from a checkout) finds a daemon already holding port 7727, lets its own exit and
shows the one already there; quitting it stops only the daemon it started.

The browser settings page uses tabs and two columns. A paired browser connected directly to the
local daemon also has a **This machine** tab for the machine settings from the native settings
window. Changing those settings requires Send access for that browser.
The hosted Cloud browser has **Browser**, **Work**, and **Status** tabs; it only offers the settings
its remote connection can change.

## Linux: a `systemd --user` service

The installer writes `~/.config/systemd/user/clawdline-next.service`, starts it, and turns on
lingering so it runs at boot and after logout ([install.md](install.md)). It needs no `sudo`
unless your system reserves lingering for an administrator, in which case it prints the one line
to ask for. Run it from a real login (SSH or a desktop session), not through `su` or `sudo -u`:
those have no user service manager, and the installer stops and says so.

**Check:** `systemctl --user status clawdline-next` is active, and `clawdline doctor` answers.

### Deploying from a checkout

A machine that follows commits instead of releases installs from the checkout:

```sh
tools/deploy-linux-user.sh --stage-only                   # as the service account
sudo tools/bootstrap-linux-user-service.sh <service-user> # once, as an administrator
```

The bootstrap installs `tools/systemd/clawdline-next.service`, runs `loginctl enable-linger`,
starts it and checks that the console answers; if the machine still runs the earlier system-wide
`clawdline-next.service`, it stops and disables that one. Every later update is one unprivileged
command:

```sh
git fetch origin main && git merge --ff-only origin/main
tools/deploy-linux-user.sh
```

It builds from a clean checkout, switches the release atomically, restarts only the daemon — tmux
and the assistants it opened keep running — checks `GET /` and the build stamp, and rolls back if
either fails. It prints `deployed <commit>; daemon pid N; console 200`. Such a machine is not
offered releases; `clawdline setup --adopt` moves it onto them.

On a machine with no desktop, `clawdline open --print` prints the sign-in address; reach it over an
SSH port forward ([remote-access.md](remote-access.md)). Clipboard pictures and the notch are
unavailable on Linux, and a picture you send is handed to the assistant by its path.

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
