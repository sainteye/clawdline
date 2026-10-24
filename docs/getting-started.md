# Getting started from source

For the installed release and your first Session, start with the [user walkthrough](user/first-session.md). This page is for developers building from a clone.

From a fresh clone to the console in your browser, a session in the list, and, if you want it,
Clawdline Cloud. Every step ends with a way to check that it worked.

Paths below use `~/code/clawdline` for the checkout and `~/code/my-app` for a project you work in.
Use your own.

## 0. What you need

| | Why | Check |
| --- | --- | --- |
| Go 1.25 or newer | builds the daemon | `go version` |
| Node.js with npm | builds the console. No version is pinned yet | `npm --version` |
| tmux | the way the daemon sees and drives sessions on macOS and Linux | `tmux -V` |
| Claude Code or Codex | the sessions themselves | `claude --version` or `codex --version` |
| Xcode command line tools, macOS only | builds the native app, and only if you want it | `swiftc --version` |

iTerm2 is optional on a Mac: its sessions are listed too. Windows is not supported yet. The daemon
builds for it but has no way to list or drive sessions there ([user/platforms.md](user/platforms.md)).

## 1. Clone and build

```sh
git clone https://github.com/sainteye/clawdline.git ~/code/clawdline
cd ~/code/clawdline

(cd web && npm install && npm run build)
go build -o bin/clawdline ./cmd/clawdline
```

The first command builds the console into `web/console/dist`. The second builds one binary, which
is both the daemon and the command line.

**Check:** `ls web/console/dist/index.html` exists, and `./bin/clawdline version` prints a
version.

## 2. Start the daemon

```sh
CLAWDLINE_NEXT_WEB="$PWD/web/console/dist" ./bin/clawdline serve
```

One variable: where the console's built files are. Without it the daemon still starts — the API,
the broker and the Cloud line do not need it — but `/` answers `501 no_web_root`, and the line under
`listening` in its log says `console: NONE`.

Two more you may want:

| Variable | Default |
| --- | --- |
| `CLAWDLINE_NEXT_PORT` | `7727` |
| `CLAWDLINE_NEXT_DIR`, the state directory | `$XDG_CONFIG_HOME/clawdline-next`, else `~/.config/clawdline-next`. On Windows, `%APPDATA%\clawdline-next` |

The daemon binds `127.0.0.1` only. Its log goes to `logs/daemon.log` in the state directory, and
the first line it prints says so. Binding is the first thing it does: if something already holds
the port, it writes who — the pid, its command, since when, and whether that one is a Clawdline
daemon serving a console — into that log and to stderr, and exits with status 3 having written
nothing else.

**Check**, from a second terminal:

```sh
curl -s http://127.0.0.1:7727/v1/health     # {"at":…,"ok":true,"served_by":"clawdline-go"}
./bin/clawdline doctor                      # version, port, state directory, store counts
```

`served_by` is how you tell this daemon from anything else answering on the port. If you run the
command line with a different `CLAWDLINE_NEXT_PORT` or `CLAWDLINE_NEXT_DIR` than the daemon, give
it the same values: that is how it finds the daemon and its local token.

**Health says the daemon is alive, not that it shows a console.** A daemon started without
`CLAWDLINE_NEXT_WEB` is green on `/v1/health` and has no page to show, and a restart checked with
health alone will look fine. Check a restart by what it was for:

```sh
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:7727/   # 200 is the console
grep -A1 'listening on' logs/daemon.log | tail -2                # console: served from …
```

`/v1/diagnostics` (this machine's own token) carries the same answer as `console.state`: `served`,
`none` or `broken`. Health leaves it out on purpose: a paired phone reads the same health, its
console is app.clawdline.com's, and a missing page on this address stops nothing it uses.

## 3. Open the console

```sh
./bin/clawdline open
```

This creates a device for your browser, opens the console, and signs it in. The key travels in
the address's fragment, which a browser never sends to a server and never logs.

That browser can read every session and type into it. (`--send` is still accepted and changes
nothing.)

`--print` prints the address instead of opening it. Treat that address like a password until it
has been used.

**Check:** the console loads and shows the session list. The interface text is Traditional
Chinese in these examples; the console offers multiple interface languages.

## 4. Put a session in the list

The daemon lists sessions that run in tmux, and in iTerm2 on a Mac. Start one:

```sh
tmux new -s work
cd ~/code/my-app
claude            # or: codex
```

**Check:** within a few seconds the session is a row in the console, with its project and its
state. Open the row to read the transcript, and type into the box to send it text.

Nothing was installed into Claude Code or Codex to make this work. The daemon reads what the
assistants already write under `~/.claude` and `~/.codex`, and reads their screens through tmux.

## 5. The macOS app (optional)

```sh
tools/package-macos.sh          # dist/Clawdline Next.app
tools/package-macos.sh --dmg    # and a disk image
```

If the app is already running from that `dist/`, the script quits it, puts the new build in its
place and opens it again, because replacing a running app's files makes macOS refuse its control
of iTerm2 until it restarts. Pass `--no-restart` to have it stop instead and leave the new build
in `dist/.staging.<pid>/`.

Apple silicon and macOS 13 or newer. The app is signed ad hoc, not notarized. It carries its own
copy of the daemon and the console, starts that daemon with the three variables above, and signs
its window in with the daemon's local token. You do not run `serve` yourself. If a daemon you
started by hand already holds port 7727, the app's daemon exits and the window shows the one
already there.

The app adds a menu bar item, an input bar on a global hotkey you choose in its settings, launch
at login, and a mascot in the notch.

**Check:** the app's window shows the same session list as the browser.

## 6. Reach it from a phone

Three ways, compared in [user/remote-access.md](user/remote-access.md): an SSH port forward,
Tailscale Serve on your own tailnet, or Clawdline Cloud, below. Clawdline no longer runs a
Cloudflare tunnel of its own; an old setting for one starts nothing, and the **Remote** tab says
so once. Over Serve a device pairs with a six-digit code, and like every paired device it can
then read and send. When a device asks to pair, the code appears on this machine, in the macOS app
or through `./bin/clawdline pair --watch`, and is never sent back to the device that asked.

### Through Tailscale Serve (optional)

With Tailscale installed and signed in on this machine and the
phone, and MagicDNS and HTTPS certificates turned on for the tailnet:

```sh
tailscale serve --bg 7727             # HTTPS on https://<machine>.<tailnet>.ts.net, private to the tailnet
tailscale serve status                # the address
```

Turn remote access on and enter that full name in **Tailscale address** (`tailscale_hostname` in
`config.json`), then open the address on the phone and pair it. Serve is private to your tailnet;
Funnel would make the address public and is not recommended. `tailscale serve --https=443 off` or
`tailscale serve reset` stops it.

### Turn on Clawdline Cloud (optional)

Follow [Cloud from a browser or phone](user/cloud-first-look.md) to sign in, connect this machine,
and pair the browser or phone you will use. The guide covers macOS App setup, iPhone and Android;
[remote access](user/remote-access.md) includes the terminal path and recovery steps.

**Check:** the machine reports connected, both ends show matching fingerprints, and the paired
browser or phone shows its Sessions.

## 7. Bring your projects to another machine

A second machine — a Linux server on the same Cloud account, say — can have the repositories and
still lack what git does not carry: the name and icon each project has in the console, and the
skills, commands and agents you keep untracked under `.claude/`, plus `CLAUDE.local.md`. One
machine owns those settings; the others mirror them read-only. Projects are matched by their git
`origin`, so the checkouts may sit at different paths. The full design is
[project-sync.md](project-sync.md).

**Before you start**

- Both machines run this version. An older one is listed with “Clawdline needs an update” and cannot be
  chosen.
- The browser is paired with both machines (section 6), and the receiving machine lets the browser
  act on it: `./bin/clawdline cloud commands on` there.
- Each project has an `origin` remote that the other machine can reach. A project with no origin,
  or a folder that is not a git repository, is listed as skipped with its reason.

**Through Clawdline Cloud**

1. In the hosted console, open the **receiving** machine and go to **Projects**.
2. Open **Project settings sync** and choose the source under **Primary machine**.
3. Press **Read its Projects**, untick anything you do not want, and, if the receiving machine does not
   have some of the repositories yet, tick the line that clones them. It names the directory they
   will go into: the one most of that machine's projects already sit in, or
   `CLAWDLINE_PROJECTS_ROOT` if you set it for the daemon.
4. Press **Sync the selected N projects**. Each project reports what happened: files written, files kept
   because they were edited on this machine, or a clone in progress.

From then on, change a project's icon or skills on the source. Opening **Projects** on the
receiving machine applies whatever changed there. To make a project the receiving machine's own
again, press **Switch back to local settings** on its row.

**Without Cloud**

```sh
# on the source machine
./bin/clawdline project export --out projects.json

# copy projects.json over, then on the receiving machine
./bin/clawdline project import --clone projects.json
```

The file holds the contents of those skills and notes, so move it the way you would move the files
themselves, and delete it afterwards. `--clone` is optional; without it a repository the machine
does not have is reported as missing.

**Check:** on the receiving machine, **Projects** shows each project with the source's name and
icon, and a mirrored skill is in that checkout's `.claude/skills/`. The source
owns later changes to those settings.

Some things are deliberately not copied: `.claude/settings.local.json` (it holds the source's
permission grants), dot files such as `.env` and build caches inside those directories, and your
personal `~/.claude/skills`.

## When something does not come up

| You see | It means |
| --- | --- |
| `501 not_implemented` on a `/v1/` route | This daemon has not taken that route over yet. The body names the route; nothing is wrong with your setup |
| `501 no_web_root` on `/` | `CLAWDLINE_NEXT_WEB` is not set. The daemon's log says `console: NONE` under `listening` |
| `500 no_document` on `/` | `CLAWDLINE_NEXT_WEB` is set and has no `index.html`: the console was not built, or its bundle is being rebuilt. The log says `console: BROKEN` |
| The daemon exits with status 3 | Another process holds the port. `logs/daemon.log` names it — pid, command, since when, and what its `/v1/health` and `/` answer. Stop that process by its pid, or start this one with another `CLAWDLINE_NEXT_PORT` |
| `the daemon did not answer` from the CLI | Nothing is listening on that port, or the CLI and the daemon have different `CLAWDLINE_NEXT_PORT` values |
| A session is missing from the list | It is not running inside tmux (or iTerm2 on a Mac), or tmux is not on `PATH` nor in `/opt/homebrew/bin`, `/usr/local/bin`, `/usr/bin` or `/opt/local/bin` |
| The Cloud line stays off | `./bin/clawdline cloud status` gives the reason. A broken Cloud setting never stops the daemon, it only keeps the line down |
| `mirror_source_mismatch` when syncing a project | Another machine already owns that project's settings here. Choose that machine as the source, or run `project import --replace-source` to hand it over |
| A clone reports `clone_failed` | The receiving machine could not reach the remote with its own credentials; git's own last line is shown. Clone it by hand into the named directory, then sync again |

Everything the daemon does is in `logs/daemon.log` in the state directory.

## Next

- [The user pages](../README.md#documentation): one page per job — sessions, notifications, remote access, schedules,
  the Board, dispatch, projects, usage
- [architecture.md](architecture.md): how the pieces fit
- [project-sync.md](project-sync.md): how project settings move between machines, and what never does
- [README.md](README.md): every document, and which ones are public design notes
